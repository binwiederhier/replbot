package bot

import (
	"context"
	"errors"
	"fmt"
	"github.com/slack-go/slack"
	"heckel.io/replbot/config"
	"log"
	"regexp"
	"strings"
	"sync"
)

var (
	slackLinkWithTextRegex = regexp.MustCompile(`<https?://[^|\s]+\|([^>]+)>`)
	slackRawLinkRegex      = regexp.MustCompile(`<(https?://[^|\s]+)>`)
	slackCodeBlockRegex    = regexp.MustCompile("```([^`]+)```")
	slackCodeRegex         = regexp.MustCompile("`([^`]+)`")
	slackUserLinkRegex     = regexp.MustCompile(`<@U[^>]+>`)
	slackMacQuotesRegex    = regexp.MustCompile(`[“”]`)
	slackReplacer          = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">") // see slackutilsx.go, EscapeMessage
)

type SlackConn struct {
	rtm    *slack.RTM
	userID string
	config *config.Config
	mu     sync.RWMutex
}

func NewSlackConn(conf *config.Config) *SlackConn {
	return &SlackConn{
		config: conf,
	}
}

func (b *SlackConn) Connect(ctx context.Context) (<-chan event, error) {
	eventChan := make(chan event)
	b.rtm = slack.New(b.config.Token, slack.OptionDebug(b.config.Debug)).NewRTM()
	go b.rtm.ManageConnection()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-b.rtm.IncomingEvents:
				if ev := b.translateEvent(e); ev != nil {
					eventChan <- ev
				}
			}
		}
	}()
	return eventChan, nil
}

func (b *SlackConn) Close() error {
	return nil
}

func (b *SlackConn) Sender(channel, threadTS string) Sender {
	return NewSlackSender(b.rtm, channel, threadTS)
}

func (b *SlackConn) Mention() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return fmt.Sprintf("<@%s>", b.userID)
}

func (b *SlackConn) Unescape(s string) string {
	s = slackLinkWithTextRegex.ReplaceAllString(s, "$1")
	s = slackRawLinkRegex.ReplaceAllString(s, "$1")
	s = slackCodeBlockRegex.ReplaceAllString(s, "$1")
	s = slackCodeRegex.ReplaceAllString(s, "$1")
	s = slackUserLinkRegex.ReplaceAllString(s, "")   // Remove entirely!
	s = slackMacQuotesRegex.ReplaceAllString(s, `"`) // See issue #14, Mac client sends wrong quotes
	s = slackReplacer.Replace(s)                     // Must happen last!
	return s
}

func (b *SlackConn) ModeSupported(mode config.Mode) bool {
	return true
}

func (b *SlackConn) translateEvent(event slack.RTMEvent) event {
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
		return &errorEvent{errors.New("invalid credentials")}
	default:
		return nil // Ignore other events
	}
}

func (b *SlackConn) handleMessageEvent(ev *slack.MessageEvent) event {
	if ev.User == "" || ev.SubType == "channel_join" {
		return nil // Ignore my own and join messages
	}
	return &messageEvent{
		ID:          ev.Timestamp,
		Channel:     ev.Channel,
		ChannelType: b.channelType(ev.Channel),
		Thread:      ev.ThreadTimestamp,
		User:        ev.User,
		Message:     ev.Text,
	}
}

func (b *SlackConn) handleConnectedEvent(ev *slack.ConnectedEvent) event {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ev.Info == nil || ev.Info.User == nil || ev.Info.User.ID == "" {
		return errorEvent{errors.New("missing user info in connected event")}
	}
	b.userID = ev.Info.User.ID
	log.Printf("Slack connected as user %s/%s", ev.Info.User.Name, ev.Info.User.ID)
	return nil
}

func (b *SlackConn) handleChannelJoinedEvent(ev *slack.ChannelJoinedEvent) event {
	return &channelJoined{ev.Channel.ID}
}

func (b *SlackConn) handleErrorEvent(err error) event {
	log.Printf("Error: %s\n", err.Error())
	return nil
}

func (b *SlackConn) handleLatencyReportEvent(ev *slack.LatencyReport) event {
	log.Printf("Current latency: %v\n", ev.Value)
	return nil
}

func (b *SlackConn) channelType(channel string) ChannelType {
	if strings.HasPrefix(channel, "C") {
		return Channel
	} else if strings.HasPrefix(channel, "D") {
		return DM
	}
	return Unknown
}
