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
	"time"
)

var (
	slackLinkWithTextRegex = regexp.MustCompile(`<https?://[^|\s]+\|([^>]+)>`)
	slackRawLinkRegex      = regexp.MustCompile(`<(https?://[^|\s]+)>`)
	slackCodeBlockRegex    = regexp.MustCompile("```([^`]+)```")
	slackCodeRegex         = regexp.MustCompile("`([^`]+)`")
	slackUserLinkRegex     = regexp.MustCompile(`<@(U[^>]+)>`)
	slackMacQuotesRegex    = regexp.MustCompile(`[“”]`)
	slackReplacer          = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">") // see slackutilsx.go, EscapeMessage
)

const (
	additionalRateLimitDuration = 500 * time.Millisecond
)

type slackConn struct {
	rtm    *slack.RTM
	userID string
	config *config.Config
	mu     sync.RWMutex
}

func newSlackConn(conf *config.Config) *slackConn {
	return &slackConn{
		config: conf,
	}
}

func (b *slackConn) Connect(ctx context.Context) (<-chan event, error) {
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

func (s *slackConn) Send(target *chatID, message string) error {
	_, err := s.SendWithID(target, message)
	return err
}

func (s *slackConn) SendWithID(target *chatID, message string) (string, error) {
	options := s.postOptions(target, slack.MsgOptionText(message, false))
	for {
		_, responseTS, err := s.rtm.PostMessage(target.Channel, options...)
		if err == nil {
			return responseTS, nil
		}
		if e, ok := err.(*slack.RateLimitedError); ok {
			log.Printf("error: %s; sleeping before re-sending", err.Error())
			time.Sleep(e.RetryAfter + additionalRateLimitDuration)
			continue
		}
		return "", err
	}
}

func (s *slackConn) Update(target *chatID, id string, message string) error {
	options := s.postOptions(target, slack.MsgOptionText(message, false))
	for {
		_, _, _, err := s.rtm.UpdateMessage(target.Channel, id, options...)
		if err == nil {
			return nil
		}
		if e, ok := err.(*slack.RateLimitedError); ok {
			log.Printf("error: %s; sleeping before re-sending", err.Error())
			time.Sleep(e.RetryAfter + additionalRateLimitDuration)
			continue
		}
		return err
	}
}

func (s *slackConn) Archive(target *chatID) error {
	return nil
}

func (b *slackConn) Close() error {
	return nil
}

func (b *slackConn) MentionBot() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return fmt.Sprintf("<@%s>", b.userID)
}

func (b *slackConn) Mention(user string) string {
	return fmt.Sprintf("<@%s>", user)
}

func (b *slackConn) ParseMention(user string) (string, error) {
	if matches := slackUserLinkRegex.FindStringSubmatch(user); len(matches) > 0 {
		return matches[1], nil
	}
	return "", errors.New("invalid user")
}

func (b *slackConn) Unescape(s string) string {
	s = slackLinkWithTextRegex.ReplaceAllString(s, "$1")
	s = slackRawLinkRegex.ReplaceAllString(s, "$1")
	s = slackCodeBlockRegex.ReplaceAllString(s, "$1")
	s = slackCodeRegex.ReplaceAllString(s, "$1")
	s = slackUserLinkRegex.ReplaceAllString(s, "")   // Remove entirely!
	s = slackMacQuotesRegex.ReplaceAllString(s, `"`) // See issue #14, Mac client sends wrong quotes
	s = slackReplacer.Replace(s)                     // Must happen last!
	return s
}

func (b *slackConn) translateEvent(event slack.RTMEvent) event {
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

func (b *slackConn) handleMessageEvent(ev *slack.MessageEvent) event {
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

func (b *slackConn) handleConnectedEvent(ev *slack.ConnectedEvent) event {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ev.Info == nil || ev.Info.User == nil || ev.Info.User.ID == "" {
		return errorEvent{errors.New("missing user info in connected event")}
	}
	b.userID = ev.Info.User.ID
	log.Printf("Slack connected as user %s/%s", ev.Info.User.Name, ev.Info.User.ID)
	return nil
}

func (b *slackConn) handleChannelJoinedEvent(ev *slack.ChannelJoinedEvent) event {
	return &channelJoinedEvent{ev.Channel.ID}
}

func (b *slackConn) handleErrorEvent(err error) event {
	log.Printf("Error: %s\n", err.Error())
	return nil
}

func (b *slackConn) handleLatencyReportEvent(ev *slack.LatencyReport) event {
	log.Printf("Current latency: %v\n", ev.Value)
	return nil
}

func (b *slackConn) channelType(ch string) channelType {
	if strings.HasPrefix(ch, "C") {
		return channelTypeChannel
	} else if strings.HasPrefix(ch, "D") {
		return channelTypeDM
	}
	return channelTypeUnknown
}

func (s *slackConn) postOptions(target *chatID, msg slack.MsgOption) []slack.MsgOption {
	options := []slack.MsgOption{msg, slack.MsgOptionDisableLinkUnfurl(), slack.MsgOptionDisableMediaUnfurl()}
	if target.Thread != "" {
		options = append(options, slack.MsgOptionTS(target.Thread))
	}
	return options
}
