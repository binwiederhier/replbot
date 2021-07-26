package bot

import (
	"fmt"
	"github.com/slack-go/slack"
	"log"
	"strings"
	"time"
)

type Format int

const (
	Text = iota
	Markdown
	Code
)

// TODO deal with rate limiting

type Sender interface {
	Send(message string, format Format) error
	SendWithID(message string, format Format) (string, error)
}

type SlackSender struct {
	rtm      *slack.RTM
	channel  string
	threadTS string
}

func NewSlackSender(rtm *slack.RTM, channel string, threadTS string) *SlackSender {
	return &SlackSender{
		rtm:      rtm,
		channel:  channel,
		threadTS: threadTS,
	}
}

func (s *SlackSender) SendWithID(message string, format Format) (string, error) {
	switch format {
	case Text:
		return s.sendText(message)
	case Markdown:
		return s.sendMarkdown(message)
	case Code:
		return s.sendCode(message)
	default:
		return "", fmt.Errorf("invalid format: %d", format)
	}
}

func (s *SlackSender) Send(message string, format Format) error {
	_, err := s.SendWithID(message, format)
	return err
}

func (s *SlackSender) sendText(message string) (string, error) {
	return s.send(slack.MsgOptionText(message, false))
}

func (s *SlackSender) sendCode(message string) (string, error) {
	markdown := fmt.Sprintf("```%s```", strings.ReplaceAll(message, "```", "` ` `")) // Hack ...
	return s.sendMarkdown(markdown)
}

func (s *SlackSender) sendMarkdown(markdown string) (string, error) {
	// TODO break large message into smaller one!
	section := slack.NewSectionBlock(slack.NewTextBlockObject("mrkdwn", markdown, false, true), nil, nil)
	return s.send(slack.MsgOptionBlocks(section))
}

func (s *SlackSender) send(options ...slack.MsgOption) (string, error) {
	if s.threadTS != "" {
		options = append(options, slack.MsgOptionTS(s.threadTS))
	}
	for {
		_, responseTS, err := s.rtm.PostMessage(s.channel, options...)
		if err == nil {
			return responseTS, nil
		}
		if e, ok := err.(*slack.RateLimitedError); ok {
			log.Printf("error: %s; sleeping before re-sending", err.Error())
			time.Sleep(e.RetryAfter + 500*time.Millisecond)
			continue
		}
		return "", err
	}
}
