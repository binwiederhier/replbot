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

type ChatWindow struct {
	Channel string
	Thread  string
}

type Conn interface {
	Connect(ctx context.Context) (<-chan event, error)
	Send(window *ChatWindow, message string, format Format) error
	SendWithID(window *ChatWindow, message string, format Format) (string, error)
	Update(window *ChatWindow, id string, message string, format Format) error
	Archive(window *ChatWindow) error
	Mention() string
	Unescape(s string) string
	Close() error
}
