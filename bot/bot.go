package bot

import (
	"errors"
	"fmt"
	"github.com/slack-go/slack"
	"heckel.io/replbot/config"
	"log"
	"strings"
	"sync"
	"time"
)

type Bot struct {
	config   *config.Config
	userID   string
	sessions map[string]*session
	rtm      *slack.RTM
	mu       sync.RWMutex
}

func New(config *config.Config) (*Bot, error) {
	return &Bot{
		config:   config,
		sessions: make(map[string]*session),
	}, nil
}

func (b *Bot) Start() error {
	b.rtm = slack.New(b.config.Token).NewRTM()
	go b.rtm.ManageConnection()
	go b.manageSessions()

	for msg := range b.rtm.IncomingEvents {
		switch ev := msg.Data.(type) {
		case *slack.ConnectedEvent:
			if ev.Info == nil || ev.Info.User == nil || ev.Info.User.ID == "" {
				return errors.New("missing user info in connected event")
			}
			b.mu.Lock()
			b.userID = ev.Info.User.ID
			b.mu.Unlock()
			log.Printf("Slack connected as user %s/%s", ev.Info.User.Name, ev.Info.User.ID)
		case *slack.LatencyReport:
			log.Printf("Current latency: %v\n", ev.Value)
		case *slack.RTMError:
			log.Printf("Error: %s\n", ev.Error())
		case *slack.ConnectionErrorEvent:
			log.Printf("Error: %s\n", ev.Error())
		case *slack.InvalidAuthEvent:
			return errors.New("invalid credentials")
		case *slack.MessageEvent:
			b.dispatchMessage(ev)
		default:
			// Ignore other events
		}
	}

	return errors.New("unexpected end of incoming events stream")
}

func (b *Bot) dispatchMessage(ev *slack.MessageEvent) {
	if ev.User == "" {
		return // Ignore my own messages
	}
	if strings.HasPrefix(ev.Channel, "D") {
		sessionID := ev.Channel
		if !b.maybeForwardMessage(sessionID, ev.Text) {
			b.startSession(sessionID, ev.Channel, "")
		}
	} else if strings.HasPrefix(ev.Channel, "C") {
		if ev.ThreadTimestamp == "" && b.mentioned(ev.Text) {
			sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.Timestamp)
			b.startSession(sessionID, ev.Channel, ev.Timestamp)
		} else if ev.ThreadTimestamp != "" {
			sessionID := fmt.Sprintf("%s:%s", ev.Channel, ev.ThreadTimestamp)
			if !b.maybeForwardMessage(sessionID, ev.Text) && b.mentioned(ev.Text) {
				b.startSession(sessionID, ev.Channel, ev.ThreadTimestamp)
			}
		}
	}
}

func (b *Bot) startSession(sessionID string, channel string, threadTS string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sender := NewSlackSender(b.rtm, channel, threadTS)
	b.sessions[sessionID] = NewSession(b.config, sessionID, sender)
	log.Printf("[session %s] Starting new session\n", sessionID)
}

func (b *Bot) maybeForwardMessage(sessionID string, message string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if session, ok := b.sessions[sessionID]; ok && session.Active() {
		_ = session.Send(message) // TODO deal with URLs
		return true
	}
	return false
}

func (b *Bot) manageSessions() {
	for {
		b.mu.Lock()
		for id, session := range b.sessions {
			if !session.Active() {
				log.Printf("[session %s] Removing stale session", session.ID)
				delete(b.sessions, id)
			}
		}
		b.mu.Unlock()
		time.Sleep(15 * time.Second)
	}
}

func (b *Bot) mentioned(message string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return strings.Contains(message, fmt.Sprintf("<@%s>", b.userID))
}
