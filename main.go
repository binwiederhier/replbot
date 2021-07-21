package main

import (
	"errors"
	"github.com/slack-go/slack"
	"log"
	"os"
	"strings"
)

func rtm() error {
	token := os.Getenv("SLACK_BOT_TOKEN")
	if token == "" {
		return errors.New("SLACK_BOT_TOKEN must be set")
	} else if !strings.HasPrefix(token, "xoxb-") {
		return errors.New("SLACK_BOT_TOKEN must have the prefix \"xoxb-\"")
	}
	api := slack.New(token, slack.OptionLog(log.New(os.Stdout, "slack-bot: ", log.Lshortfile|log.LstdFlags)))

	sessions := make(map[string]*Session)
	rtm := api.NewRTM()
	go rtm.ManageConnection()

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
				sessions[ev.Timestamp] = NewSession(rtm, ev.Channel, ev.Timestamp)
			} else if ev.ThreadTimestamp != "" {
				if session, ok := sessions[ev.ThreadTimestamp]; ok {
					session.inputChan <- ev.Text
				}
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

func main() {
	if err := rtm(); err != nil {
		panic(err)
	}
}
