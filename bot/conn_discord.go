package bot

import (
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"heckel.io/replbot/config"
	"log"
	"regexp"
	"strings"
	"sync"
)

var (
	discordUserLinkRegex    = regexp.MustCompile(`<@![^>]+>`)
	discordChannelLinkRegex = regexp.MustCompile(`<#[^>]+>`)
	discordCodeBlockRegex   = regexp.MustCompile("```([^`]+)```")
	discordCodeRegex        = regexp.MustCompile("`([^`]+)`")
)

type DiscordConn struct {
	config   *config.Config
	session  *discordgo.Session
	channels map[string]*discordgo.Channel
	mu       sync.Mutex
}

func NewDiscordConn(conf *config.Config) *DiscordConn {
	return &DiscordConn{
		config:   conf,
		channels: make(map[string]*discordgo.Channel),
	}
}

func (b *DiscordConn) Connect(ctx context.Context) (<-chan event, error) {
	discord, err := discordgo.New(fmt.Sprintf("Bot %s", b.config.Token))
	if err != nil {
		return nil, err
	}
	eventChan := make(chan event)
	discord.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if ev := b.translateMessageEvent(m); ev != nil {
			eventChan <- ev
		}
	})
	discord.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages
	if err := discord.Open(); err != nil {
		return nil, err
	}
	b.session = discord
	log.Printf("Discord connected as user %s/%s", discord.State.User.Username, discord.State.User.ID)
	return eventChan, nil
}

func (s *DiscordConn) Send(target *Target, message string, format Format) error {
	_, err := s.SendWithID(target, message, format)
	return err
}

func (s *DiscordConn) SendWithID(target *Target, message string, format Format) (string, error) {
	switch format {
	case Markdown:
		return s.send(target, message)
	case Code:
		return s.send(target, s.formatCode(message))
	default:
		return "", fmt.Errorf("invalid format: %d", format)
	}
}

func (s *DiscordConn) Update(target *Target, id string, message string, format Format) error {
	switch format {
	case Markdown:
		return s.update(target, id, message)
	case Code:
		return s.update(target, id, s.formatCode(message))
	default:
		return fmt.Errorf("invalid format: %d", format)
	}
}

func (s *DiscordConn) Archive(target *Target) error {
	if target.Thread == "" {
		return nil
	}
	_, err := s.session.ThreadEditComplex(target.Thread, &discordgo.ThreadEditData{
		Name:                "REPLbot session",
		Archived:            true,
		Locked:              false,
		AutoArchiveDuration: discordgo.ArchiveDurationOneHour,
	})
	return err
}

func (b *DiscordConn) Close() error {
	return b.session.Close()
}

func (b *DiscordConn) Mention() string {
	return fmt.Sprintf("<@!%s>", b.session.State.User.ID)
}

func (b *DiscordConn) Unescape(s string) string {
	s = discordCodeBlockRegex.ReplaceAllString(s, "$1")
	s = discordCodeRegex.ReplaceAllString(s, "$1")
	s = discordUserLinkRegex.ReplaceAllString(s, "")    // Remove entirely!
	s = discordChannelLinkRegex.ReplaceAllString(s, "") // Remove entirely!
	return s
}

func (b *DiscordConn) translateMessageEvent(m *discordgo.MessageCreate) event {
	if m.Author.ID == b.session.State.User.ID {
		return nil
	}
	channel, err := b.channel(m.ChannelID)
	if err != nil {
		return &errorEvent{err}
	}
	var thread, channelID string
	if channel.IsThread() {
		channelID = channel.ParentID
		thread = m.ChannelID
	} else {
		channelID = m.ChannelID
	}
	return &messageEvent{
		ID:          m.ID,
		Channel:     channelID,
		ChannelType: b.channelType(channel),
		Thread:      thread,
		User:        m.Author.ID,
		Message:     m.Content,
	}
}

func (b *DiscordConn) channel(channel string) (*discordgo.Channel, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if c, ok := b.channels[channel]; ok {
		return c, nil
	}
	c, err := b.session.Channel(channel)
	if err != nil {
		return nil, err
	}
	b.channels[channel] = c
	return c, nil
}

func (b *DiscordConn) channelType(channel *discordgo.Channel) ChannelType {
	switch channel.Type {
	case discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildPrivateThread, discordgo.ChannelTypeGuildPublicThread:
		return Channel
	case discordgo.ChannelTypeDM:
		return DM
	default:
		return Unknown
	}
}

func (s *DiscordConn) formatCode(message string) string {
	return fmt.Sprintf("```%s```", strings.ReplaceAll(message, "```", "` ` `")) // Hack ...
}

func (s *DiscordConn) send(target *Target, content string) (string, error) {
	channel, err := s.maybeCreateThread(target)
	if err != nil {
		return "", err
	}
	msg, err := s.session.ChannelMessageSend(channel, content)
	if err != nil {
		return "", err
	}
	return msg.ID, nil
}

func (s *DiscordConn) update(target *Target, messageID string, content string) error {
	channel := target.Channel
	if target.Thread != "" {
		channel = target.Thread
	}
	_, err := s.session.ChannelMessageEdit(channel, messageID, content)
	return err
}

func (s *DiscordConn) maybeCreateThread(target *Target) (string, error) {
	channel := target.Channel
	if target.Thread == "" {
		return channel, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.channels[target.Thread]; ok {
		return target.Thread, nil
	}
	c, err := s.session.StartThreadWithMessage(target.Channel, target.Thread, &discordgo.ThreadCreateData{
		Name:                "REPLbot session",
		AutoArchiveDuration: discordgo.ArchiveDurationOneHour,
	})
	if err != nil {
		return "", err
	}
	s.channels[target.Thread] = c
	return target.Thread, nil
}
