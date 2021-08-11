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

type Target struct {
	Channel string
	Thread  string
}

type Conn interface {
	Connect(ctx context.Context) (<-chan event, error)
	Send(target *Target, message string, format Format) error
	SendWithID(target *Target, message string, format Format) (string, error)
	Update(target *Target, id string, message string, format Format) error
	Mention() string
	Unescape(s string) string
	Close() error
}
