package bot

import (
	"context"
	"errors"
	"fmt"
	"github.com/slack-go/slack"
	"heckel.io/replbot/config"
	"io"
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

func (c *slackConn) Connect(ctx context.Context) (<-chan event, error) {
	eventChan := make(chan event)
	c.rtm = slack.New(c.config.Token, slack.OptionDebug(c.config.Debug)).NewRTM()
	go c.rtm.ManageConnection()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-c.rtm.IncomingEvents:
				if ev := c.translateEvent(e); ev != nil {
					eventChan <- ev
				}
			}
		}
	}()
	return eventChan, nil
}

func (c *slackConn) Send(channel *channelID, message string) error {
	_, err := c.SendWithID(channel, message)
	return err
}

func (c *slackConn) SendWithID(channel *channelID, message string) (string, error) {
	options := c.postOptions(channel, slack.MsgOptionText(message, false))
	for {
		_, responseTS, err := c.rtm.PostMessage(channel.Channel, options...)
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

func (c *slackConn) SendEphemeral(channel *channelID, userID, message string) error {
	options := c.postOptions(channel, slack.MsgOptionText(message, false))
	for {
		_, err := c.rtm.PostEphemeral(channel.Channel, userID, options...)
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

func (c *slackConn) SendDM(userID string, message string) error {
	ch, _, _, err := c.rtm.OpenConversation(&slack.OpenConversationParameters{
		ReturnIM: true,
		Users:    []string{userID},
	})
	if err != nil {
		return err
	}
	channel := &channelID{ch.ID, ""}
	return c.Send(channel, message)
}

func (c *slackConn) UploadFile(channel *channelID, message string, filename string, filetype string, file io.Reader) error {
	_, err := c.rtm.UploadFile(slack.FileUploadParameters{
		InitialComment:  message,
		Filename:        filename,
		Filetype:        filetype,
		Reader:          file,
		Channels:        []string{channel.Channel},
		ThreadTimestamp: channel.Thread,
	})
	return err
}

func (c *slackConn) Update(channel *channelID, id string, message string) error {
	options := c.postOptions(channel, slack.MsgOptionText(message, false))
	for {
		_, _, _, err := c.rtm.UpdateMessage(channel.Channel, id, options...)
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

func (c *slackConn) Archive(_ *channelID) error {
	return nil
}

func (c *slackConn) Close() error {
	return nil
}

func (c *slackConn) MentionBot() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return fmt.Sprintf("<@%s>", c.userID)
}

func (c *slackConn) Mention(user string) string {
	return fmt.Sprintf("<@%s>", user)
}

func (c *slackConn) ParseMention(user string) (string, error) {
	if matches := slackUserLinkRegex.FindStringSubmatch(user); len(matches) > 0 {
		return matches[1], nil
	}
	return "", errors.New("invalid user")
}

func (c *slackConn) Unescape(s string) string {
	s = slackLinkWithTextRegex.ReplaceAllString(s, "$1")
	s = slackRawLinkRegex.ReplaceAllString(s, "$1")
	s = slackCodeBlockRegex.ReplaceAllString(s, "$1")
	s = slackCodeRegex.ReplaceAllString(s, "$1")
	s = slackUserLinkRegex.ReplaceAllString(s, "")   // Remove entirely!
	s = slackMacQuotesRegex.ReplaceAllString(s, `"`) // See issue #14, Mac client sends wrong quotes
	s = slackReplacer.Replace(s)                     // Must happen last!
	return s
}

func (c *slackConn) translateEvent(event slack.RTMEvent) event {
	switch ev := event.Data.(type) {
	case *slack.ConnectedEvent:
		return c.handleConnectedEvent(ev)
	case *slack.ChannelJoinedEvent:
		return c.handleChannelJoinedEvent(ev)
	case *slack.MessageEvent:
		return c.handleMessageEvent(ev)
	case *slack.RTMError:
		return c.handleErrorEvent(ev)
	case *slack.ConnectionErrorEvent:
		return c.handleErrorEvent(ev)
	case *slack.InvalidAuthEvent:
		return &errorEvent{errors.New("invalid credentials")}
	default:
		return nil // Ignore other events
	}
}

func (c *slackConn) handleMessageEvent(ev *slack.MessageEvent) event {
	if ev.User == "" || ev.SubType == "channel_join" {
		return nil // Ignore my own and join messages
	}
	return &messageEvent{
		ID:          ev.Timestamp,
		Channel:     ev.Channel,
		ChannelType: c.channelType(ev.Channel),
		Thread:      ev.ThreadTimestamp,
		User:        ev.User,
		Message:     ev.Text,
	}
}

func (c *slackConn) handleConnectedEvent(ev *slack.ConnectedEvent) event {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ev.Info == nil || ev.Info.User == nil || ev.Info.User.ID == "" {
		return errorEvent{errors.New("missing user info in connected event")}
	}
	c.userID = ev.Info.User.ID
	log.Printf("Slack connected as user %s/%s", ev.Info.User.Name, ev.Info.User.ID)
	return nil
}

func (c *slackConn) handleChannelJoinedEvent(ev *slack.ChannelJoinedEvent) event {
	return &channelJoinedEvent{ev.Channel.ID}
}

func (c *slackConn) handleErrorEvent(err error) event {
	log.Printf("Error: %s\n", err.Error())
	return nil
}

func (c *slackConn) channelType(ch string) channelType {
	if strings.HasPrefix(ch, "C") {
		return channelTypeChannel
	} else if strings.HasPrefix(ch, "D") {
		return channelTypeDM
	}
	return channelTypeUnknown
}

func (c *slackConn) postOptions(channel *channelID, msg slack.MsgOption) []slack.MsgOption {
	options := []slack.MsgOption{msg, slack.MsgOptionDisableLinkUnfurl(), slack.MsgOptionDisableMediaUnfurl()}
	if channel.Thread != "" {
		options = append(options, slack.MsgOptionTS(channel.Thread))
	}
	return options
}
