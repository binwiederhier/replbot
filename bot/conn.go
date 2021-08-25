package bot

import (
	"context"
	"io"
)

type chatID struct {
	Channel string
	Thread  string
}

type conn interface {
	Connect(ctx context.Context) (<-chan event, error)
	Send(chat *chatID, message string) error
	SendWithID(chat *chatID, message string) (string, error)
	SendWithAttachment(chat *chatID, message string, filename string, filetype string, file io.Reader) error
	Update(chat *chatID, id string, message string) error
	Archive(chat *chatID) error
	MentionBot() string
	Mention(user string) string
	ParseMention(user string) (string, error)
	Unescape(s string) string
	Close() error
}
