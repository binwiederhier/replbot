package bot

import (
	"context"
	"errors"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"heckel.io/replbot/config"
	"io"
	"log"
	"regexp"
	"sync"
)

var (
	discordUserLinkRegex    = regexp.MustCompile(`<@!([^>]+)>`)
	discordChannelLinkRegex = regexp.MustCompile(`<#[^>]+>`)
	discordCodeBlockRegex   = regexp.MustCompile("```([^`]+)```")
	discordCodeRegex        = regexp.MustCompile("`([^`]+)`")
)

type discordConn struct {
	config   *config.Config
	session  *discordgo.Session
	channels map[string]*discordgo.Channel
	mu       sync.Mutex
}

func newDiscordConn(conf *config.Config) *discordConn {
	return &discordConn{
		config:   conf,
		channels: make(map[string]*discordgo.Channel),
	}
}

func (c *discordConn) Connect(ctx context.Context) (<-chan event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	discord, err := discordgo.New(fmt.Sprintf("Bot %s", c.config.Token))
	if err != nil {
		return nil, err
	}
	eventChan := make(chan event)
	discord.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if ev := c.translateMessageEvent(m); ev != nil {
			eventChan <- ev
		}
	})
	discord.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages
	if err := discord.Open(); err != nil {
		return nil, err
	}
	c.session = discord
	if discord.State == nil || discord.State.User == nil {
		return nil, errors.New("unexpected internal state")
	}
	log.Printf("Discord connected as user %s/%s", discord.State.User.Username, discord.State.User.ID)
	return eventChan, nil
}

func (c *discordConn) Send(channel *channelID, message string) error {
	_, err := c.SendWithID(channel, message)
	return err
}

func (c *discordConn) SendWithID(channel *channelID, message string) (string, error) {
	ch, err := c.maybeCreateThread(channel)
	if err != nil {
		return "", err
	}
	msg, err := c.session.ChannelMessageSend(ch, message)
	if err != nil {
		return "", err
	}
	return msg.ID, nil
}

func (c *discordConn) UploadFile(channel *channelID, message string, filename string, filetype string, file io.Reader) error {
	ch := channel.Channel
	if channel.Thread != "" {
		ch = channel.Thread
	}
	_, err := c.session.ChannelMessageSendComplex(ch, &discordgo.MessageSend{
		Content: message,
		File: &discordgo.File{
			Name:        filename,
			ContentType: filetype,
			Reader:      file,
		},
	})
	return err
}

func (c *discordConn) Update(channel *channelID, id string, message string) error {
	ch := channel.Channel
	if channel.Thread != "" {
		ch = channel.Thread
	}
	_, err := c.session.ChannelMessageEdit(ch, id, message)
	return err
}

func (c *discordConn) Archive(channel *channelID) error {
	if channel.Thread == "" {
		return nil
	}
	_, err := c.session.ThreadEdit(channel.Thread, "REPLbot session", true, false, discordgo.ArchiveDurationOneHour)
	return err
}

func (c *discordConn) Close() error {
	return c.session.Close()
}

func (c *discordConn) MentionBot() string {
	return fmt.Sprintf("<@!%s>", c.session.State.User.ID)
}

func (c *discordConn) Mention(user string) string {
	return fmt.Sprintf("<@!%s>", user)
}

func (c *discordConn) ParseMention(user string) (string, error) {
	if matches := discordUserLinkRegex.FindStringSubmatch(user); len(matches) > 0 {
		return matches[1], nil
	}
	return "", errors.New("invalid user")
}

func (c *discordConn) Unescape(s string) string {
	s = discordCodeBlockRegex.ReplaceAllString(s, "$1")
	s = discordCodeRegex.ReplaceAllString(s, "$1")
	s = discordUserLinkRegex.ReplaceAllString(s, "")    // Remove entirely!
	s = discordChannelLinkRegex.ReplaceAllString(s, "") // Remove entirely!
	return s
}

func (c *discordConn) translateMessageEvent(m *discordgo.MessageCreate) event {
	if m.Author.ID == c.session.State.User.ID {
		return nil
	}
	channel, err := c.channel(m.ChannelID)
	if err != nil {
		return &errorEvent{err}
	}
	var thread, channelID string
	if channel.ThreadMetadata != nil {
		channelID = channel.ParentID
		thread = m.ChannelID
	} else {
		channelID = m.ChannelID
	}
	return &messageEvent{
		ID:          m.ID,
		Channel:     channelID,
		ChannelType: c.channelType(channel),
		Thread:      thread,
		User:        m.Author.ID,
		Message:     m.Content,
	}
}

func (c *discordConn) channel(channel string) (*discordgo.Channel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ch, ok := c.channels[channel]; ok {
		return ch, nil
	}
	ch, err := c.session.Channel(channel)
	if err != nil {
		return nil, err
	}
	c.channels[channel] = ch
	return ch, nil
}

func (c *discordConn) channelType(ch *discordgo.Channel) channelType {
	switch ch.Type {
	case discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildPrivateThread, discordgo.ChannelTypeGuildPublicThread:
		return channelTypeChannel
	case discordgo.ChannelTypeDM:
		return channelTypeDM
	default:
		return channelTypeUnknown
	}
}

func (c *discordConn) maybeCreateThread(target *channelID) (string, error) {
	channel := target.Channel
	if target.Thread == "" {
		return channel, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.channels[target.Thread]; ok {
		return target.Thread, nil
	}
	ch, err := c.session.ThreadStartWithMessage(target.Channel, target.Thread, "REPLbot session", discordgo.ArchiveDurationOneHour)
	if err != nil {
		return "", err
	}
	c.channels[target.Thread] = ch
	return target.Thread, nil
}
