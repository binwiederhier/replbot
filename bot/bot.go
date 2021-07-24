package bot

import (
	"errors"
	"github.com/slack-go/slack"
	"heckel.io/replbot/config"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Bot struct {
	config   *config.Config
	scripts  map[string]string
	sessions map[string]*session
	mu       sync.Mutex
}

func New(config *config.Config) (*Bot, error) {
	scripts := make(map[string]string, 0)
	entries, err := os.ReadDir(config.ScriptDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		scripts[entry.Name()] = filepath.Join(config.ScriptDir, entry.Name())
	}
	return &Bot{
		config:   config,
		scripts:  scripts,
		sessions: make(map[string]*session),
	}, nil
}

func (b *Bot) Start() error {
	api := slack.New(b.config.Token, slack.OptionLog(log.New(os.Stdout, "slack-bot: ", log.Lshortfile|log.LstdFlags)))
	rtm := api.NewRTM()
	go rtm.ManageConnection()
	go b.manageSessions() // FIXME

	for msg := range rtm.IncomingEvents {
		switch ev := msg.Data.(type) {
		case *slack.ConnectedEvent:
			log.Print("Slack connected")
		case *slack.LatencyReport:
			log.Printf("Current latency: %v\n", ev.Value)
		case *slack.RTMError:
			log.Printf("Error: %s\n", ev.Error())
		case *slack.InvalidAuthEvent:
			return errors.New("invalid credentials")
		case *slack.MessageEvent:
			if ev.User == "" {
				continue // Ignore my own messages
			}
			if ev.ThreadTimestamp == "" && strings.TrimSpace(ev.Text) == "repl" { // TODO react on name instead
				log.Printf("[session %s] Starting session requested by user %s\n", ev.Timestamp, ev.User)
				b.mu.Lock()
				sender := NewSlackSender(rtm, ev.Channel, ev.Timestamp)
				b.sessions[ev.Timestamp] = NewSession(ev.Timestamp, sender, b.scripts)
				b.mu.Unlock()
			} else if ev.ThreadTimestamp != "" {
				b.mu.Lock()
				if session, ok := b.sessions[ev.ThreadTimestamp]; ok && session.Active() {
					_ = session.Send(ev.Text) // TODO deal with URLs
				}
				b.mu.Unlock()
			}
		default:
			// Ignore other events
		}
	}

	return errors.New("unexpected end of incoming events stream")
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
