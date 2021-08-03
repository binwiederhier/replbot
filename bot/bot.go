package bot

import (
	"context"
	"errors"
	"fmt"
	"github.com/slack-go/slack"
	"heckel.io/replbot/config"
	"heckel.io/replbot/util"
	"log"
	"strings"
	"sync"
)

const (
	welcomeMessage = "Hi there 👋! I'm a robot that you can use to control a REPL from Slack. " +
		"To start a new session, simply tag me and name one of the available REPLs, like so:\n\n> %s %s\n\n" +
		"Available REPLs: %s. You can also use the words `thread` or `channel` to control where the session, " +
		"is started, or DM me for a private REPL."
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
	return &Bot{
		config:   config,
		sessions: make(map[string]*Session),
	}, nil
}

func (b *Bot) Start() error {
	b.rtm = slack.New(b.config.Token).NewRTM()
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
	if ev.User == "" {
		return nil // Ignore my own messages
	}
	sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.ThreadTimestamp) // ThreadTimestamp may be empty, that's ok
	mentioned, script, mode := b.parseMessage(ev.Text)

	// Direct message with REPLbot
	if strings.HasPrefix(ev.Channel, "D") {
		if !b.maybeForwardMessage(sessionID, ev.Text) {
			if script == "" {
				return b.handleHelpCommand(ev)
			}
			return b.startSession(sessionID, ev.Channel, ev.ThreadTimestamp, script, config.ModeChannel)
		}
	}

	// Message in a channel
	if strings.HasPrefix(ev.Channel, "C") {
		// First try to find a matching session based on the channel (and potentially thread),
		// and forward the message to the existing session.
		if !b.maybeForwardMessage(sessionID, ev.Text) {
			if mentioned && script != "" {
				var threadTS string
				if mode == config.ModeThread {
					threadTS = ev.Timestamp
					sessionID = fmt.Sprintf("%s:%s", ev.Channel, ev.Timestamp) // Override!
				}
				return b.startSession(sessionID, ev.Channel, threadTS, script, mode)
			}
		}
	}

	// Fall back to help if mentioned
	if mentioned {
		return b.handleHelpCommand(ev)
	}

	return nil
}

func (b *Bot) startSession(sessionID string, channel string, threadTS string, script string, mode string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	sender := NewSlackSender(b.rtm, channel, threadTS)
	session := NewSession(b.config, sessionID, sender, script, mode)
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

func (b *Bot) maybeForwardMessage(sessionID string, message string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if session, ok := b.sessions[sessionID]; ok && session.Active() {
		_ = session.HandleUserInput(util.RemoveSlackLinks(message))
		return true
	}
	return false
}

func (b *Bot) handleErrorEvent(err error) error {
	log.Printf("Error: %s\n", err.Error())
	return nil
}

func (b *Bot) handleLatencyReportEvent(ev *slack.LatencyReport) error {
	log.Printf("Current latency: %v\n", ev.Value)
	return nil
}

func (b *Bot) parseMessage(message string) (mentioned bool, script string, mode string) {
	fields := strings.Fields(message)
	mentioned = util.StringContains(fields, b.me())
	for _, f := range fields {
		if script = b.config.Script(f); script != "" {
			break
		}
	}
	if util.StringContains(fields, config.ModeThread) {
		mode = config.ModeThread
	} else if util.StringContains(fields, config.ModeChannel) {
		mode = config.ModeChannel
	} else {
		mode = b.config.DefaultMode
	}
	return
}

func (b *Bot) handleHelpCommand(ev *slack.MessageEvent) error {
	sender := NewSlackSender(b.rtm, ev.Channel, ev.ThreadTimestamp)
	scripts := b.config.Scripts()
	return sender.Send(fmt.Sprintf(welcomeMessage, b.me(), scripts[1], b.replList()), Markdown)
}

func (b *Bot) replList() string {
	return fmt.Sprintf("`%s`", strings.Join(b.config.Scripts(), "`, `"))
}

func (b *Bot) me() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return fmt.Sprintf("<@%s>", b.userID)
}
