package bot

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"github.com/gliderlabs/ssh"
	"golang.org/x/sync/errgroup"
	"heckel.io/replbot/config"
	"heckel.io/replbot/util"
	"log"
	"net"
	"strings"
	"sync"
	"text/template"
)

const (
	welcomeMessage = "Hi there 👋! "
	helpMessage    = "I'm a robot for running interactive REPLs and shells from a right here. To start a new session, simply tag me " +
		"and name one of the available REPLs, like so: %s %s\n\nAvailable REPLs: %s. To run the session in a `thread`, " +
		"the main `channel`, or in `split` mode, use the respective keywords. To define the terminal size, use the words " +
		"`tiny`, `small`, `medium` or `large`. Use `full` or `trim` to set the window mode. To start a private REPL " +
		"session, just DM me."
	misconfiguredMessage  = "😭 Oh no. It looks like REPLbot is misconfigured. I couldn't find any scripts to run."
	unknownCommandMessage = "I am not quite sure what you mean by _%s_ ⁉️\n\n"
)

var (
	errNoScript = errors.New("no script defined")
)

type Bot struct {
	config   *config.Config
	conn     Conn
	sessions map[string]*Session
	cancelFn context.CancelFunc
	mu       sync.RWMutex
}

func New(conf *config.Config) (*Bot, error) {
	if len(conf.Scripts()) == 0 {
		return nil, errors.New("no REPL scripts found in script dir")
	}
	if err := util.TmuxInstalled(); err != nil {
		return nil, fmt.Errorf("tmux check failed: %s", err.Error())
	}
	var conn Conn
	switch conf.Type() {
	case config.TypeSlack:
		conn = NewSlackConn(conf)
	case config.TypeDiscord:
		conn = NewDiscordConn(conf)
	default:
		return nil, fmt.Errorf("invalid type: %s", conf.Type())
	}
	return &Bot{
		config:   conf,
		conn:     conn,
		sessions: make(map[string]*Session),
	}, nil
}

func (b *Bot) Run() error {
	var ctx context.Context
	ctx, b.cancelFn = context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)
	eventChan, err := b.conn.Connect(ctx)
	if err != nil {
		return err
	}
	g.Go(func() error {
		return b.handleEvents(ctx, eventChan)
	})
	if b.config.SSHServerListen != "" && b.config.SSHServerHost != "" {
		g.Go(func() error {
			return b.runSSHd(ctx)
		})
	}
	return g.Wait()
}

func (b *Bot) handleEvents(ctx context.Context, eventChan <-chan event) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-eventChan:
			if err := b.handleEvent(ev); err != nil {
				return err
			}
		}
	}
}

func (b *Bot) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sessionID, session := range b.sessions {
		log.Printf("[session %s] Force-closing session", sessionID)
		if err := session.ForceClose(); err != nil {
			log.Printf("[session %s] Force-closing failed: %s", sessionID, err.Error())
		}
		delete(b.sessions, sessionID)
	}
	b.cancelFn() // This must be at the end, see app.go
}

func (b *Bot) handleEvent(e event) error {
	switch ev := e.(type) {
	case *messageEvent:
		return b.handleMessageEvent(ev)
	case *errorEvent:
		return ev.Error
	default:
		return nil // Ignore other events
	}
}
func (b *Bot) handleMessageEvent(ev *messageEvent) error {
	if b.maybeForwardMessage(ev) {
		return nil // We forwarded the message
	} else if ev.ChannelType == Unknown {
		return nil
	} else if ev.ChannelType == Channel && !strings.Contains(ev.Message, b.conn.Mention()) {
		return nil
	}
	script, mode, windowMode, width, height, err := b.parseMessage(ev)
	if err != nil {
		return b.handleHelp(ev.Channel, ev.Thread, err)
	}
	switch mode {
	case config.Channel:
		return b.startSessionChannel(ev, script, windowMode, width, height)
	case config.Thread:
		return b.startSessionThread(ev, script, windowMode, width, height)
	case config.Split:
		return b.startSessionSplit(ev, script, windowMode, width, height)
	default:
		return fmt.Errorf("unexpected mode: %s", mode)
	}
}

func (b *Bot) startSessionChannel(ev *messageEvent, script string, windowMode config.WindowMode, width, height int) error {
	sessionID := util.SanitizeID(fmt.Sprintf("%s_%s", ev.Channel, ""))
	target := &Target{Channel: ev.Channel, Thread: ""}
	return b.startSession(sessionID, target, target, script, config.Channel, windowMode, width, height)
}

func (b *Bot) startSessionThread(ev *messageEvent, script string, windowMode config.WindowMode, width, height int) error {
	if ev.Thread == "" {
		sessionID := util.SanitizeID(fmt.Sprintf("%s_%s", ev.Channel, ev.ID))
		target := &Target{Channel: ev.Channel, Thread: ev.ID}
		return b.startSession(sessionID, target, target, script, config.Thread, windowMode, width, height)
	}
	sessionID := util.SanitizeID(fmt.Sprintf("%s_%s", ev.Channel, ev.Thread))
	target := &Target{Channel: ev.Channel, Thread: ev.Thread}
	return b.startSession(sessionID, target, target, script, config.Thread, windowMode, width, height)
}

func (b *Bot) startSessionSplit(ev *messageEvent, script string, windowMode config.WindowMode, width, height int) error {
	if ev.Thread == "" {
		sessionID := util.SanitizeID(fmt.Sprintf("%s_%s", ev.Channel, ev.ID))
		control := &Target{Channel: ev.Channel, Thread: ev.ID}
		terminal := &Target{Channel: ev.Channel, Thread: ""}
		return b.startSession(sessionID, control, terminal, script, config.Split, windowMode, width, height)
	}
	sessionID := util.SanitizeID(fmt.Sprintf("%s_%s", ev.Channel, ev.Thread))
	control := &Target{Channel: ev.Channel, Thread: ev.Thread}
	terminal := &Target{Channel: ev.Channel, Thread: ""}
	return b.startSession(sessionID, control, terminal, script, config.Split, windowMode, width, height)
}

func (b *Bot) startSession(sessionID string, control *Target, terminal *Target, script string, mode config.ControlMode, windowMode config.WindowMode, width, height int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	session := NewSession(b.config, b.conn, sessionID, control, terminal, script, mode, windowMode, width, height)
	b.sessions[sessionID] = session
	log.Printf("[session %s] Starting session", sessionID)
	go func() {
		if err := session.Run(); err != nil {
			log.Printf("[session %s] Session exited with error: %s", sessionID, err.Error())
		} else {
			log.Printf("[session %s] Session exited", sessionID)
		}
		b.mu.Lock()
		delete(b.sessions, sessionID)
		b.mu.Unlock()
	}()
	return nil
}

func (b *Bot) maybeForwardMessage(ev *messageEvent) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	sessionID := util.SanitizeID(fmt.Sprintf("%s_%s", ev.Channel, ev.Thread)) // Thread may be empty, that's ok
	if session, ok := b.sessions[sessionID]; ok && session.Active() {
		session.HandleUserInput(ev.Message)
		return true
	}
	return false
}

func (b *Bot) parseMessage(ev *messageEvent) (script string, mode config.ControlMode, windowMode config.WindowMode, width int, height int, err error) {
	fields := strings.Fields(ev.Message)
	for _, field := range fields {
		switch field {
		case b.conn.Mention():
			// Ignore
		case string(config.Thread), string(config.Channel), string(config.Split):
			mode = config.ControlMode(field)
		case string(config.Full), string(config.Trim):
			windowMode = config.WindowMode(field)
		case config.Tiny.Name, config.Small.Name, config.Medium.Name, config.Large.Name:
			width, height = config.Sizes[field].Width, config.Sizes[field].Height
		default:
			if s := b.config.Script(field); s != "" {
				script = s
			} else {
				return "", "", "", 0, 0, fmt.Errorf(unknownCommandMessage, field)
			}
		}
	}
	if script == "" {
		return "", "", "", 0, 0, errNoScript
	}
	if mode == "" {
		if ev.Thread != "" {
			mode = config.Thread // special handling, because it'd be weird otherwise
		} else {
			mode = b.config.DefaultControlMode
		}
	}
	if b.config.Type() == config.TypeDiscord && ev.ChannelType == DM && mode != config.Channel {
		mode = config.Channel // special case: Discord does not support threads in direct messages
	}
	if windowMode == "" {
		if mode == config.Thread {
			windowMode = config.Trim
		} else {
			windowMode = b.config.DefaultWindowMode
		}
	}
	if width == 0 || height == 0 {
		if mode == config.Thread {
			width, height = config.Tiny.Width, config.Tiny.Height // special case: make it tiny in a thread
		} else {
			width, height = b.config.DefaultSize.Width, b.config.DefaultSize.Height
		}
	}
	return
}

func (b *Bot) handleChannelJoinedEvent(ev *messageEvent) error {
	return b.handleHelp(ev.Channel, "", nil)
}

func (b *Bot) handleHelp(channel, thread string, err error) error {
	target := &Target{Channel: channel, Thread: thread}
	scripts := b.config.Scripts()
	if len(scripts) == 0 {
		return b.conn.Send(target, misconfiguredMessage, Markdown)
	}
	var messageTemplate string
	if err == nil || err == errNoScript {
		messageTemplate = welcomeMessage + helpMessage
	} else {
		messageTemplate = err.Error() + helpMessage
	}
	replList := fmt.Sprintf("`%s`", strings.Join(b.config.Scripts(), "`, `"))
	return b.conn.Send(target, fmt.Sprintf(messageTemplate, b.conn.Mention(), scripts[0], replList), Markdown)
}

var (
	//go:embed screenshare_client.gotmpl
	screenshareClient         string
	screenshareClientTemplate = template.Must(template.New("screenshare-client").Parse(screenshareClient))
)

type sshSession struct {
	SessionID  string
	ServerHost string
	ServerPort string
	RelayPort  int
}

func (b *Bot) runSSHd(ctx context.Context) error {
	serverHost, serverPort, err := net.SplitHostPort(b.config.SSHServerHost)
	if err != nil {
		return err
	}

	forwardHandler := &ssh.ForwardedTCPHandler{}

	server := ssh.Server{
		Addr:                       b.config.SSHServerListen,
		PasswordHandler:            nil,
		KeyboardInteractiveHandler: nil,
		PublicKeyHandler: func(ctx ssh.Context, key ssh.PublicKey) bool {
			return true
		},
		ReversePortForwardingCallback: func(ctx ssh.Context, host string, port uint32) bool {
			if port < 1024 || (host != "localhost" && host != "127.0.0.1") {
				return false
			}
			b.mu.RLock()
			defer b.mu.RUnlock()
			session, ok := b.sessions[ctx.User()]
			if !ok {
				log.Printf("unknown session: %s", ctx.User())
				return false
			}
			log.Printf("host: %v, port: %v", host, port)
			return int(port) == session.relayPort
		},
		Handler: func(s ssh.Session) {
			b.mu.RLock()
			defer b.mu.RUnlock()
			session, ok := b.sessions[s.User()]
			if !ok {
				log.Printf("unknown session: %s", s.User())
				return
			}
			sessionInfo := &sshSession{
				SessionID:  session.id,
				ServerHost: serverHost,
				ServerPort: serverPort,
				RelayPort:  10000,
			}
			if err := screenshareClientTemplate.Execute(s, sessionInfo); err != nil {
				log.Printf(err.Error())
				return
			}
			// When exiting this method, the connection is closed!
		},
		RequestHandlers: map[string]ssh.RequestHandler{
			"tcpip-forward":        forwardHandler.HandleSSHRequest,
			"cancel-tcpip-forward": forwardHandler.HandleSSHRequest,
		},
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"session":      ssh.DefaultSessionHandler,
			"direct-tcpip": ssh.DirectTCPIPHandler,
		},
	}

	if err := server.SetOption(ssh.HostKeyFile("tmp/hostkey")); err != nil {
		return err
	}
	errChan := make(chan error)
	go func() {
		errChan <- server.ListenAndServe()
	}()
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return nil
	}
}
