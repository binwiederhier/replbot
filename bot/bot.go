package bot

import (
	"context"
	"errors"
	"fmt"
	"github.com/slack-go/slack"
	"heckel.io/replbot/config"
	"heckel.io/replbot/util"
	"log"
	"regexp"
	"strings"
	"sync"
)

const (
	welcomeMessage = "Hi there 👋! "
	helpMessage    = "I'm a robot that you can use to control a REPL from Slack. To start a new session, simply tag me " +
		"and name one of the available REPLs, like so: %s %s\n\nAvailable REPLs: %s. To run the session in a `thread`, " +
		"the main `channel`, or in `split` mode, use the respective keywords. To define the terminal size, use the words " +
		"`tiny`, `small`, `medium`, `large` or `WxH`. To start a private REPL session, just DM me."
	misconfiguredMessage  = "😭 Oh no. It looks like REPLbot is misconfigured. I couldn't find any scripts to run."
	unknownCommandMessage = "I am not quite sure what you mean by \"_%s_\" ⁉️\n\n"
)

var (
	sizeRegex   = regexp.MustCompile(`(\d+)x(\d+)`)
	errNoScript = errors.New("no script defined")
)

type Bot struct {
	config   *config.Config
	userID   string
	sessions map[string]*Session
	ctx      context.Context
	cancelFn context.CancelFunc
	rtm      *slack.RTM
	mu       sync.RWMutex
}

func New(config *config.Config) (*Bot, error) {
	if len(config.Scripts()) == 0 {
		return nil, errors.New("no REPL scripts found in script dir")
	}
	if err := util.TmuxInstalled(); err != nil {
		return nil, fmt.Errorf("tmux check failed: %s", err.Error())
	}
	return &Bot{
		config:   config,
		sessions: make(map[string]*Session),
	}, nil
}

func (b *Bot) Start() error {
	b.rtm = slack.New(b.config.Token, slack.OptionDebug(b.config.Debug)).NewRTM()
	go b.rtm.ManageConnection()
	b.ctx, b.cancelFn = context.WithCancel(context.Background())
	for {
		select {
		case <-b.ctx.Done():
			return nil
		case event := <-b.rtm.IncomingEvents:
			if err := b.handleIncomingEvent(event); err != nil {
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

func (b *Bot) handleIncomingEvent(event slack.RTMEvent) error {
	switch ev := event.Data.(type) {
	case *slack.ConnectedEvent:
		return b.handleConnectedEvent(ev)
	case *slack.ChannelJoinedEvent:
		return b.handleChannelJoinedEvent(ev)
	case *slack.MessageEvent:
		return b.handleMessageEvent(ev)
	case *slack.LatencyReport:
		return b.handleLatencyReportEvent(ev)
	case *slack.RTMError:
		return b.handleErrorEvent(ev)
	case *slack.ConnectionErrorEvent:
		return b.handleErrorEvent(ev)
	case *slack.InvalidAuthEvent:
		return errors.New("invalid credentials")
	default:
		return nil // Ignore other events
	}
}

func (b *Bot) handleConnectedEvent(ev *slack.ConnectedEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ev.Info == nil || ev.Info.User == nil || ev.Info.User.ID == "" {
		return errors.New("missing user info in connected event")
	}
	b.userID = ev.Info.User.ID
	log.Printf("Slack connected as user %s/%s", ev.Info.User.Name, ev.Info.User.ID)
	return nil
}

func (b *Bot) handleMessageEvent(ev *slack.MessageEvent) error {
	if ev.User == "" || ev.SubType == "channel_join" {
		return nil // Ignore my own and join messages
	}
	if b.maybeForwardMessage(ev) {
		return nil // We forwarded the message
	}
	if strings.HasPrefix(ev.Channel, "C") && !strings.Contains(ev.Text, b.me()) {
		return nil
	}
	script, mode, width, height, err := b.parseMessage(ev)
	if err != nil {
		return b.handleHelp(ev.Channel, ev.ThreadTimestamp, err)
	}
	switch mode {
	case config.ModeChannel:
		return b.startSessionChannel(ev, script, width, height)
	case config.ModeThread:
		return b.startSessionThread(ev, script, width, height)
	case config.ModeSplit:
		return b.startSessionSplit(ev, script, width, height)
	default:
		return fmt.Errorf("unexpected mode: %s", mode)
	}
}

func (b *Bot) startSessionChannel(ev *slack.MessageEvent, script string, width, height int) error {
	sessionID := fmt.Sprintf("%s:%s", ev.Channel, "")
	return b.startSession(sessionID, ev.Channel, "", "", script, config.ModeChannel, width, height)
}

func (b *Bot) startSessionThread(ev *slack.MessageEvent, script string, width, height int) error {
	if ev.ThreadTimestamp == "" {
		sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.Timestamp)
		return b.startSession(sessionID, ev.Channel, ev.Timestamp, ev.Timestamp, script, config.ModeThread, width, height)
	}
	sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.ThreadTimestamp)
	return b.startSession(sessionID, ev.Channel, ev.ThreadTimestamp, ev.ThreadTimestamp, script, config.ModeThread, width, height)
}

func (b *Bot) startSessionSplit(ev *slack.MessageEvent, script string, width, height int) error {
	if ev.ThreadTimestamp == "" {
		sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.Timestamp)
		return b.startSession(sessionID, ev.Channel, ev.Timestamp, "", script, config.ModeSplit, width, height)
	}
	sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.ThreadTimestamp)
	return b.startSession(sessionID, ev.Channel, ev.ThreadTimestamp, "", script, config.ModeSplit, width, height)
}

func (b *Bot) startSession(sessionID string, channel string, controlTS string, terminalTS string, script string, mode string, width, height int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	control := NewSlackSender(b.rtm, channel, controlTS)
	terminal := control
	if terminalTS != controlTS {
		terminal = NewSlackSender(b.rtm, channel, terminalTS)
	}
	session := NewSession(b.config, sessionID, control, terminal, script, mode, width, height)
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

func (b *Bot) maybeForwardMessage(ev *slack.MessageEvent) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.ThreadTimestamp) // ThreadTimestamp may be empty, that's ok
	if session, ok := b.sessions[sessionID]; ok && session.Active() {
		session.HandleUserInput(ev.Text)
		return true
	}
	return false
}

func (b *Bot) handleErrorEvent(err error) error {
	log.Printf("Error: %s\n", err.Error())
	return nil
}

func (b *Bot) handleLatencyReportEvent(ev *slack.LatencyReport) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	log.Printf("%d active session(s), current latency: %v\n", len(b.sessions), ev.Value)
	return nil
}

func (b *Bot) parseMessage(ev *slack.MessageEvent) (script string, mode string, width int, height int, err error) {
	fields := strings.Fields(ev.Text)
	for _, field := range fields {
		switch field {
		case b.me():
			// Ignore
		case config.ModeThread, config.ModeChannel, config.ModeSplit:
			mode = field
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
				return "", "", 0, 0, fmt.Errorf(unknownCommandMessage, field)
			}
		}
	}
	if script == "" {
		return "", "", 0, 0, errNoScript
	}
	if mode == "" {
		if ev.ThreadTimestamp != "" {
			mode = config.ModeThread // special handling, cause it'd be weird otherwise
		} else {
			mode = b.config.DefaultMode
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

func (b *Bot) handleChannelJoinedEvent(ev *slack.ChannelJoinedEvent) error {
	return b.handleHelp(ev.Channel.ID, "", nil)
}

func (b *Bot) handleHelp(channel, threadTS string, err error) error {
	sender := NewSlackSender(b.rtm, channel, threadTS)
	scripts := b.config.Scripts()
	if len(scripts) == 0 {
		return sender.Send(misconfiguredMessage, Markdown)
	}
	var messageTemplate string
	if err == nil || err == errNoScript {
		messageTemplate = welcomeMessage + helpMessage
	} else {
		messageTemplate = err.Error() + helpMessage
	}
	replList := fmt.Sprintf("`%s`", strings.Join(b.config.Scripts(), "`, `"))
	return sender.Send(fmt.Sprintf(messageTemplate, b.me(), scripts[0], replList), Markdown)
}

func (b *Bot) me() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return fmt.Sprintf("<@%s>", b.userID)
}
