// Package bot provides the replbot main functionality
package bot

import (
	"context"
	_ "embed" // go:embed requires this
	"errors"
	"fmt"
	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
	"heckel.io/replbot/config"
	"heckel.io/replbot/util"
	"log"
	"net"
	"os"
	"strings"
	"sync"
)

const (
	welcomeMessage = "Hi there 👋! "
	helpMessage    = "I'm a robot for running interactive REPLs and shells from a right here. To start a new session, simply tag me " +
		"and name one of the available REPLs, like so: %s %s\n\nAvailable REPLs: %s. To run the session in a `thread`, " +
		"the main `channel`, or in `split` mode, use the respective keywords. To define the terminal size, use the words " +
		"`tiny`, `small`, `medium` or `large`. Use `full` or `trim` to set the window mode, and `everyone` or `only-me` to define " +
		"who can send commands. To start a private REPL session, just DM me."
	shareMessage = "Using the word `share` will allow you to share your own terminal here in the chat. Terminal sharing " +
		"sessions are always started in `only-me` mode, unless overridden."
	unknownCommandMessage = "I am not quite sure what you mean by _%s_ ⁉"
	misconfiguredMessage  = "😭 Oh no. It looks like REPLbot is misconfigured. I couldn't find any scripts to run."
	shareCommand          = "share"
	shareServerScriptFile = "/tmp/replbot_share_server.sh"
)

var (
	//go:embed share_server.sh
	shareServerScriptSource string
	errNoScript             = errors.New("no script defined")
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
	} else if err := util.Run("tmux", "-V"); err != nil {
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
	if b.config.ShareHost != "" {
		g.Go(func() error {
			return b.runShareServer(ctx)
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
	} else if ev.ChannelType == Channel && !strings.Contains(ev.Message, b.conn.MentionBot()) {
		return nil
	}
	conf, err := b.parseSessionConfig(ev)
	if err != nil {
		return b.handleHelp(ev.Channel, ev.Thread, err)
	}
	switch conf.ControlMode {
	case config.Channel:
		return b.startSessionChannel(ev, conf)
	case config.Thread:
		return b.startSessionThread(ev, conf)
	case config.Split:
		return b.startSessionSplit(ev, conf)
	default:
		return fmt.Errorf("unexpected mode: %s", conf.ControlMode)
	}
}

func (b *Bot) startSessionChannel(ev *messageEvent, conf *SessionConfig) error {
	conf.ID = util.SanitizeNonAlphanumeric(fmt.Sprintf("%s_%s", ev.Channel, ""))
	conf.Control = &ChatID{Channel: ev.Channel, Thread: ""}
	conf.Terminal = conf.Control
	return b.startSession(conf)
}

func (b *Bot) startSessionThread(ev *messageEvent, conf *SessionConfig) error {
	if ev.Thread == "" {
		conf.ID = util.SanitizeNonAlphanumeric(fmt.Sprintf("%s_%s", ev.Channel, ev.ID))
		conf.Control = &ChatID{Channel: ev.Channel, Thread: ev.ID}
		conf.Terminal = conf.Control
		return b.startSession(conf)
	}
	conf.ID = util.SanitizeNonAlphanumeric(fmt.Sprintf("%s_%s", ev.Channel, ev.Thread))
	conf.Control = &ChatID{Channel: ev.Channel, Thread: ev.Thread}
	conf.Terminal = conf.Control
	return b.startSession(conf)
}

func (b *Bot) startSessionSplit(ev *messageEvent, conf *SessionConfig) error {
	if ev.Thread == "" {
		conf.ID = util.SanitizeNonAlphanumeric(fmt.Sprintf("%s_%s", ev.Channel, ev.ID))
		conf.Control = &ChatID{Channel: ev.Channel, Thread: ev.ID}
		conf.Terminal = &ChatID{Channel: ev.Channel, Thread: ""}
		return b.startSession(conf)
	}
	conf.ID = util.SanitizeNonAlphanumeric(fmt.Sprintf("%s_%s", ev.Channel, ev.Thread))
	conf.Control = &ChatID{Channel: ev.Channel, Thread: ev.Thread}
	conf.Terminal = &ChatID{Channel: ev.Channel, Thread: ""}
	return b.startSession(conf)
}

func (b *Bot) startSession(conf *SessionConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	session := NewSession(b.config, b.conn, conf)
	b.sessions[conf.ID] = session
	log.Printf("[session %s] Starting session", conf.ID)
	go func() {
		if err := session.Run(); err != nil {
			log.Printf("[session %s] Session exited with error: %s", conf.ID, err.Error())
		} else {
			log.Printf("[session %s] Session exited", conf.ID)
		}
		b.mu.Lock()
		delete(b.sessions, conf.ID)
		b.mu.Unlock()
	}()
	return nil
}

func (b *Bot) maybeForwardMessage(ev *messageEvent) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	sessionID := util.SanitizeNonAlphanumeric(fmt.Sprintf("%s_%s", ev.Channel, ev.Thread)) // Thread may be empty, that's ok
	if session, ok := b.sessions[sessionID]; ok && session.Active() {
		session.UserInput(ev.User, ev.Message)
		return true
	}
	return false
}

func (b *Bot) parseSessionConfig(ev *messageEvent) (*SessionConfig, error) {
	conf := &SessionConfig{
		User: ev.User,
	}
	fields := strings.Fields(ev.Message)
	for _, field := range fields {
		switch field {
		case b.conn.MentionBot():
			// Ignore
		case string(config.Thread), string(config.Channel), string(config.Split):
			conf.ControlMode = config.ControlMode(field)
		case string(config.Full), string(config.Trim):
			conf.WindowMode = config.WindowMode(field)
		case string(config.OnlyMe), string(config.Everyone):
			conf.AuthMode = config.AuthMode(field)
		case config.Tiny.Name, config.Small.Name, config.Medium.Name, config.Large.Name:
			conf.Size = config.Sizes[field]
		default:
			if b.config.ShareEnabled() && field == shareCommand {
				relayPort, err := util.RandomPort()
				if err != nil {
					return nil, err
				}
				conf.Script = shareServerScriptFile
				conf.RelayPort = relayPort
				if conf.AuthMode == "" {
					conf.AuthMode = config.OnlyMe
				}
			} else if s := b.config.Script(field); conf.Script == "" && s != "" {
				conf.Script = s
			} else {
				return nil, fmt.Errorf(unknownCommandMessage, field)
			}
		}
	}
	if conf.Script == "" {
		return nil, errNoScript
	}
	return b.applySessionConfigDefaults(ev, conf)
}

func (b *Bot) applySessionConfigDefaults(ev *messageEvent, conf *SessionConfig) (*SessionConfig, error) {
	if conf.ControlMode == "" {
		if ev.Thread != "" {
			conf.ControlMode = config.Thread // special handling, because it'd be weird otherwise
		} else {
			conf.ControlMode = b.config.DefaultControlMode
		}
	}
	if b.config.Type() == config.TypeDiscord && ev.ChannelType == DM && conf.ControlMode != config.Channel {
		conf.ControlMode = config.Channel // special case: Discord does not support threads in direct messages
	}
	if conf.WindowMode == "" {
		if conf.ControlMode == config.Thread {
			conf.WindowMode = config.Trim
		} else {
			conf.WindowMode = b.config.DefaultWindowMode
		}
	}
	if conf.AuthMode == "" {
		conf.AuthMode = b.config.DefaultAuthMode
	}
	if conf.Size == nil {
		if conf.ControlMode == config.Thread {
			conf.Size = config.Tiny // special case: make it tiny in a thread
		} else {
			conf.Size = b.config.DefaultSize
		}
	}
	return conf, nil
}

func (b *Bot) handleChannelJoinedEvent(ev *messageEvent) error {
	return b.handleHelp(ev.Channel, "", nil)
}

func (b *Bot) handleHelp(channel, thread string, err error) error {
	target := &ChatID{Channel: channel, Thread: thread}
	scripts := b.config.Scripts()
	if len(scripts) == 0 {
		return b.conn.Send(target, misconfiguredMessage, Markdown)
	}
	var messageTemplate string
	if err == nil || err == errNoScript {
		messageTemplate = welcomeMessage + helpMessage
	} else {
		messageTemplate = err.Error() + "\n\n" + helpMessage
	}
	if b.config.ShareEnabled() {
		messageTemplate += "\n\n" + shareMessage
		scripts = append(scripts, shareCommand)
	}
	replList := fmt.Sprintf("`%s`", strings.Join(scripts, "`, `"))
	return b.conn.Send(target, fmt.Sprintf(messageTemplate, b.conn.MentionBot(), scripts[0], replList), Markdown)
}

func (b *Bot) runShareServer(ctx context.Context) error {
	if err := os.WriteFile(shareServerScriptFile, []byte(shareServerScriptSource), 0700); err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(b.config.ShareHost)
	if err != nil {
		return err
	}
	forwardHandler := &ssh.ForwardedTCPHandler{}
	server := ssh.Server{
		Addr:                          fmt.Sprintf(":%s", port),
		PasswordHandler:               nil,
		PublicKeyHandler:              nil,
		KeyboardInteractiveHandler:    nil,
		PtyCallback:                   b.sshPtyCallback,
		ReversePortForwardingCallback: b.sshReversePortForwardingCallback,
		Handler:                       b.sshSessionHandler,
		RequestHandlers: map[string]ssh.RequestHandler{
			"tcpip-forward":        forwardHandler.HandleSSHRequest,
			"cancel-tcpip-forward": forwardHandler.HandleSSHRequest,
		},
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"session": ssh.DefaultSessionHandler,
		},
	}
	if err := server.SetOption(ssh.HostKeyFile(b.config.ShareKeyFile)); err != nil {
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

func (b *Bot) sshSessionHandler(s ssh.Session) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	session, ok := b.sessions[s.User()]
	if !ok {
		log.Printf("unknown session: %s", s.User())
		return
	}
	session.WriteShareClientScript(s)
}

func (b *Bot) sshReversePortForwardingCallback(ctx ssh.Context, host string, port uint32) (allow bool) {
	conn, ok := ctx.Value(ssh.ContextKeyConn).(*gossh.ServerConn)
	if !ok {
		return false
	}
	defer func() {
		if !allow {
			log.Printf("rejecting connection %s", conn.RemoteAddr())
			conn.Close()
		}
	}()
	if port < 1024 || (host != "localhost" && host != "127.0.0.1") {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	session, ok := b.sessions[ctx.User()]
	if !ok || int(port) != session.relayPort {
		return
	}
	allow = session.RegisterShareConn(conn)
	return
}

func (b *Bot) sshPtyCallback(ctx ssh.Context, pty ssh.Pty) bool {
	return false
}
