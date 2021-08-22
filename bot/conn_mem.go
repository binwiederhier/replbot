package bot

import (
	"context"
	"errors"
	"heckel.io/replbot/config"
	"log"
	"strconv"
	"sync"
)

type MemConn struct {
	config    *config.Config
	messages  map[string]*messageEvent
	currentID int
	mu        sync.RWMutex
}

func NewMemConn(conf *config.Config) *MemConn {
	return &MemConn{
		config:    conf,
		messages:  make(map[string]*messageEvent),
		currentID: 0,
	}
}

func (c *MemConn) Connect(ctx context.Context) (<-chan event, error) {
	return make(chan event), nil
}

func (c *MemConn) Send(target *chatID, message string) error {
	c.currentID++
	c.messages[strconv.Itoa(c.currentID)] = &messageEvent{
		Channel: target.Channel,
		Thread:  target.Thread,
		Message: message,
	}
	return nil
}

func (c *MemConn) SendWithID(target *chatID, message string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentID++
	c.messages[strconv.Itoa(c.currentID)] = &messageEvent{
		Channel: target.Channel,
		Thread:  target.Thread,
		Message: message,
	}
	return strconv.Itoa(c.currentID), nil
}

func (c *MemConn) Update(target *chatID, id string, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages[id] = &messageEvent{
		Channel: target.Channel,
		Thread:  target.Thread,
		Message: message,
	}
	return nil
}

func (c *MemConn) Archive(target *chatID) error {
	return nil
}

func (c *MemConn) Close() error {
	return nil
}

func (c *MemConn) MentionBot() string {
	return ""
}

func (c *MemConn) Mention(user string) string {
	return "@" + user
}

func (c *MemConn) ParseMention(user string) (string, error) {
	return "", errors.New("invalid user")
}

func (c *MemConn) Unescape(s string) string {
	return s
}

func (c *MemConn) Message(id string) messageEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return *c.messages[id] // copy!
}

func (c *MemConn) LogMessages() {
	c.mu.Lock()
	defer c.mu.Unlock()
	log.Printf("Messages:")
	for k, m := range c.messages {
		log.Printf("- %s: %s", k, m.Message)
	}
}
