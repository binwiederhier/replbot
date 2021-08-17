package bot

import (
	"context"
)

type ChannelType int

const (
	Unknown ChannelType = iota
	Channel
	DM
)

type Format int

const (
	Markdown = iota
	Code
)

type ChatID struct {
	Channel string
	Thread  string
}

type Conn interface {
	Connect(ctx context.Context) (<-chan event, error)
	Send(chat *ChatID, message string, format Format) error
	SendWithID(chat *ChatID, message string, format Format) (string, error)
	Update(chat *ChatID, id string, message string, format Format) error
	Archive(chat *ChatID) error
	Mention() string
	Unescape(s string) string
	Close() error
}
