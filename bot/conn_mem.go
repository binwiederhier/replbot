package bot

import (
	"context"
	"errors"
	"heckel.io/replbot/config"
	"heckel.io/replbot/util"
	"log"
	"strconv"
	"sync"
	"time"
)

type memConn struct {
	config    *config.Config
	messages  map[string]*messageEvent
	currentID int
	mu        sync.RWMutex
}

func newMemConn(conf *config.Config) *memConn {
	return &memConn{
		config:    conf,
		messages:  make(map[string]*messageEvent),
		currentID: 0,
	}
}

func (c *memConn) Connect(ctx context.Context) (<-chan event, error) {
	return make(chan event), nil
}

func (c *memConn) Send(target *chatID, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentID++
	c.messages[strconv.Itoa(c.currentID)] = &messageEvent{
		Channel: target.Channel,
		Thread:  target.Thread,
		Message: message,
	}
	return nil
}

func (c *memConn) SendWithID(target *chatID, message string) (string, error) {
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

func (c *memConn) Update(target *chatID, id string, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages[id] = &messageEvent{
		Channel: target.Channel,
		Thread:  target.Thread,
		Message: message,
	}
	return nil
}

func (c *memConn) Archive(target *chatID) error {
	return nil
}

func (c *memConn) Close() error {
	return nil
}

func (c *memConn) MentionBot() string {
	return ""
}

func (c *memConn) Mention(user string) string {
	return "@" + user
}

func (c *memConn) ParseMention(user string) (string, error) {
	return "", errors.New("invalid user")
}

func (c *memConn) Unescape(s string) string {
	return s
}

func (c *memConn) Message(id string) messageEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return *c.messages[id] // copy!
}

func (c *memConn) MessageContainsWait(id string, needle string) (contains bool) {
	haystackFn := func() string {
		c.mu.Lock()
		defer c.mu.Unlock()
		m, ok := c.messages[id]
		if !ok {
			return ""
		}
		return m.Message
	}
	return util.StringContainsWait(haystackFn, needle, time.Second)
}

func (c *memConn) LogMessages() {
	c.mu.Lock()
	defer c.mu.Unlock()
	log.Printf("Messages:")
	for k, m := range c.messages {
		log.Printf("- %s: %s", k, m.Message)
	}
}
