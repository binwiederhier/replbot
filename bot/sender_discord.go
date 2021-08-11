package bot

import (
	"fmt"
	"github.com/bwmarrin/discordgo"
	"strings"
)

type DiscordSender struct {
	session *discordgo.Session
	channel string
}

func NewDiscordSender(session *discordgo.Session, channel string) *DiscordSender {
	return &DiscordSender{
		session: session,
		channel: channel,
	}
}

func (s *DiscordSender) Send(message string, format Format) error {
	_, err := s.SendWithID(message, format)
	return err
}

func (s *DiscordSender) SendWithID(message string, format Format) (string, error) {
	switch format {
	case Markdown:
		return s.send(message)
	case Code:
		return s.send(s.formatCode(message))
	default:
		return "", fmt.Errorf("invalid format: %d", format)
	}
}

func (s *DiscordSender) Update(id string, message string, format Format) error {
	switch format {
	case Markdown:
		return s.update(id, message)
	case Code:
		return s.update(id, s.formatCode(message))
	default:
		return fmt.Errorf("invalid format: %d", format)
	}
}

func (s *DiscordSender) formatCode(message string) string {
	return fmt.Sprintf("```%s```", strings.ReplaceAll(message, "```", "` ` `")) // Hack ...
}

func (s *DiscordSender) send(content string) (string, error) {
	msg, err := s.session.ChannelMessageSend(s.channel, content)
	if err != nil {
		return "", err
	}
	return msg.ID, nil
}

func (s *DiscordSender) update(messageID string, content string) error {
	_, err := s.session.ChannelMessageEdit(s.channel, messageID, content)
	return err
}
