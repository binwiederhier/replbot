package main

import (
	"errors"
	"github.com/slack-go/slack"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Token string
}

type Bot struct {
	config *Config
	sessions map[string]*Session
	mu sync.Mutex
}

func NewBot(config *Config) (*Bot, error) {
	return &Bot{
		config: config,
		sessions: make(map[string]*Session),
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
		case *slack.MessageEvent:
			if ev.User == "" {
				continue // Ignore my own messages
			}
			if ev.ThreadTimestamp == "" && strings.TrimSpace(ev.Text) == "repl" {
				log.Printf("Starting new session %s, requested by user %s\n", ev.Timestamp, ev.User)
				b.mu.Lock()
				b.sessions[ev.Timestamp] = NewSession(rtm, ev.Channel, ev.Timestamp)
				b.mu.Unlock()
			} else if ev.ThreadTimestamp != "" {
				b.mu.Lock()
				if session, ok := b.sessions[ev.ThreadTimestamp]; ok {
					session.inputChan <- ev.Text
				}
				b.mu.Unlock()
			}
		case *slack.LatencyReport:
			log.Printf("Current latency: %v\n", ev.Value)
		case *slack.RTMError:
			log.Printf("Error: %s\n", ev.Error())
		case *slack.InvalidAuthEvent:
			return errors.New("invalid credentials")
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
			if session.IsClosed() {
				log.Printf("Removing session %s", session.threadTS)
				delete(b.sessions, id)
			}
		}
		b.mu.Unlock()
		time.Sleep(5 * time.Second)
	}
}

func main() {
	/*
	c := exec.Command("sh", "-c", "rm /tmp/date; while true; do date >> /tmp/date; sleep 1; done")
	ptmx, err := pty.Start(c)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		ptmx.Close()
		log.Printf("Closed REPL session")
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Printf("Exiting read loop")
				return
			default:
			}
			buf := make([]byte, 4096) // FIXME alloc in a loop!
			n, err := ptmx.Read(buf)
			log.Printf("read loop: %s %#v", string(buf[:n]), err)
		}
	}()

	println("sleep")
	time.Sleep(5 * time.Second)

	println("cancel")
	cancel()
	time.Sleep(5 * time.Second)
	println("close")

	c.Process.Kill()
	ptmx.Close()

	time.Sleep(5 * time.Second)
	println("exit")

	os.Exit(0)*/

	config := &Config{
		Token: os.Getenv("SLACK_BOT_TOKEN"),
	}
	bot, err := NewBot(config)
	if err != nil {
		panic(err)
	}
	if err := bot.Start(); err != nil {
		panic(err)
	}
}
