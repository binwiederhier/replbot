package bot

import (
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"heckel.io/replbot/config"
	"regexp"
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
	channels map[string]ChannelType
	mu       sync.Mutex
}

func NewDiscordConn(conf *config.Config) *DiscordConn {
	return &DiscordConn{
		config:   conf,
		channels: make(map[string]ChannelType),
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
	err = discord.Open()
	if err != nil {
		return nil, err
	}
	b.session = discord
	return eventChan, nil
}

func (b *DiscordConn) Close() error {
	return b.session.Close()
}

func (b *DiscordConn) Sender(channel, threadTS string) Sender {
	return NewDiscordSender(b.session, channel)
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

func (b *DiscordConn) ModeSupported(mode config.Mode) bool {
	return mode == config.ModeChannel
}

func (b *DiscordConn) translateMessageEvent(m *discordgo.MessageCreate) event {
	if m.Author.ID == b.session.State.User.ID {
		return nil
	}
	channelType, err := b.channelType(m.ChannelID)
	if err != nil {
		return &errorEvent{err}
	}
	return &messageEvent{
		ID:          m.ID,
		Channel:     m.ChannelID,
		ChannelType: channelType,
		Thread:      "", // Not supported yet
		User:        m.Author.ID,
		Message:     m.Content,
	}
}

func (b *DiscordConn) channelType(channel string) (ChannelType, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if channelType, ok := b.channels[channel]; ok {
		return channelType, nil
	}
	c, err := b.session.Channel(channel)
	if err != nil {
		return Unknown, err
	}
	switch c.Type {
	case discordgo.ChannelTypeGuildText:
		b.channels[channel] = Channel
	case discordgo.ChannelTypeDM:
		b.channels[channel] = DM
	default:
		b.channels[channel] = Unknown
	}
	return b.channels[channel], nil
}
