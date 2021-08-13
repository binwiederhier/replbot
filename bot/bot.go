package bot

import (
	"context"
	"errors"
	"fmt"
	"heckel.io/replbot/config"
	"heckel.io/replbot/util"
	"log"
	"regexp"
	"strings"
	"sync"
)

const (
	welcomeMessage = "Hi there 👋! "
	helpMessage    = "I'm a robot for running interactive REPLs and shells from a right here. To start a new session, simply tag me " +
		"and name one of the available REPLs, like so: %s %s\n\nAvailable REPLs: %s. To run the session in a `thread`, " +
		"the main `channel`, or in `split` mode, use the respective keywords. To define the terminal size, use the words " +
		"`tiny`, `small`, `medium`, `large` or `WxH`. Use `full` or `trim` to set the window mode. To start a private REPL " +
		"session, just DM me."
	misconfiguredMessage  = "😭 Oh no. It looks like REPLbot is misconfigured. I couldn't find any scripts to run."
	unknownCommandMessage = "I am not quite sure what you mean by _%s_ ⁉️\n\n"
)

var (
	sizeRegex   = regexp.MustCompile(`(\d+)x(\d+)`)
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
	eventChan, err := b.conn.Connect(ctx)
	if err != nil {
		return err
	}
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
	case config.ModeChannel:
		return b.startSessionChannel(ev, script, windowMode, width, height)
	case config.ModeThread:
		return b.startSessionThread(ev, script, windowMode, width, height)
	case config.ModeSplit:
		return b.startSessionSplit(ev, script, windowMode, width, height)
	default:
		return fmt.Errorf("unexpected mode: %s", mode)
	}
}

func (b *Bot) startSessionChannel(ev *messageEvent, script string, windowMode config.WindowMode, width, height int) error {
	sessionID := fmt.Sprintf("%s:%s", ev.Channel, "")
	target := &Target{Channel: ev.Channel, Thread: ""}
	return b.startSession(sessionID, target, target, script, config.ModeChannel, windowMode, width, height)
}

func (b *Bot) startSessionThread(ev *messageEvent, script string, windowMode config.WindowMode, width, height int) error {
	if ev.Thread == "" {
		sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.ID)
		target := &Target{Channel: ev.Channel, Thread: ev.ID}
		return b.startSession(sessionID, target, target, script, config.ModeThread, windowMode, width, height)
	}
	sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.Thread)
	target := &Target{Channel: ev.Channel, Thread: ev.Thread}
	return b.startSession(sessionID, target, target, script, config.ModeThread, windowMode, width, height)
}

func (b *Bot) startSessionSplit(ev *messageEvent, script string, windowMode config.WindowMode, width, height int) error {
	if ev.Thread == "" {
		sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.ID)
		control := &Target{Channel: ev.Channel, Thread: ev.ID}
		terminal := &Target{Channel: ev.Channel, Thread: ""}
		return b.startSession(sessionID, control, terminal, script, config.ModeSplit, windowMode, width, height)
	}
	sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.Thread)
	control := &Target{Channel: ev.Channel, Thread: ev.Thread}
	terminal := &Target{Channel: ev.Channel, Thread: ""}
	return b.startSession(sessionID, control, terminal, script, config.ModeSplit, windowMode, width, height)
}

func (b *Bot) startSession(sessionID string, control *Target, terminal *Target, script string, mode config.Mode, windowMode config.WindowMode, width, height int) error {
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
	sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.Thread) // Thread may be empty, that's ok
	if session, ok := b.sessions[sessionID]; ok && session.Active() {
		session.HandleUserInput(ev.Message)
		return true
	}
	return false
}

func (b *Bot) parseMessage(ev *messageEvent) (script string, mode config.Mode, windowMode config.WindowMode, width int, height int, err error) {
	fields := strings.Fields(ev.Message)
	for _, field := range fields {
		switch field {
		case b.conn.Mention():
			// Ignore
		case string(config.ModeThread), string(config.ModeChannel), string(config.ModeSplit):
			mode = config.Mode(field)
		case string(config.WindowModeFull), string(config.WindowModeTrim):
			windowMode = config.WindowMode(field)
		case config.SizeTiny, config.SizeSmall, config.SizeMedium, config.SizeLarge:
			width, height = config.Sizes[field][0], config.Sizes[field][1]
		default:
			if s := b.config.Script(field); s != "" {
				script = s
			} else if sizeRegex.MatchString(field) {
				width, height, err = convertSize(field)
				if err != nil {
					return
				}
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
			mode = config.ModeThread // special handling, because it'd be weird otherwise
		} else {
			mode = b.config.DefaultMode
		}
	}
	if b.config.Type() == config.TypeDiscord && ev.ChannelType == DM && mode != config.ModeChannel {
		mode = config.ModeChannel // special case: Discord does not support threads in direct messages
	}
	if windowMode == "" {
		if mode == config.ModeThread {
			windowMode = config.WindowModeTrim
		} else {
			windowMode = b.config.DefaultWindowMode
		}
	}
	if width == 0 || height == 0 {
		if mode == config.ModeThread {
			width, height = config.Sizes[config.SizeTiny][0], config.Sizes[config.SizeTiny][1] // special case: make it tiny in a thread
		} else {
			width, height = config.Sizes[b.config.DefaultSize][0], config.Sizes[b.config.DefaultSize][1]
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
