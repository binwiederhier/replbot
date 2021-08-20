package bot

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"heckel.io/replbot/config"
	"heckel.io/replbot/util"
	"io"
	"log"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
)

var sockUserLinkRegex = regexp.MustCompile(`@(\S+)`)

const sockConnMessageID = "1"

type SockConn struct {
	config *config.Config
	conns  map[string]net.Conn
	sock   string
	mu     sync.RWMutex
}

func NewSockConn(conf *config.Config) *SockConn {
	return &SockConn{
		config: conf,
		conns:  make(map[string]net.Conn),
	}
}

func (b *SockConn) Connect(ctx context.Context) (<-chan event, error) {
	if strings.HasPrefix(b.config.Token, "sock:") {
		b.sock = strings.TrimPrefix(b.config.Token, "sock:")
	} else {
		b.sock = fmt.Sprintf("%s/replbot%s.sock", os.TempDir(), util.RandomString(5))
	}
	eventChan := make(chan event)
	listener, err := net.Listen("unix", b.sock)
	if err != nil {
		return nil, err
	}
	go b.shutdownListener(ctx, listener)
	go b.handleConns(listener, eventChan)
	log.Printf("Test socket connection ready. Run 'nc -U %s' to connect locally.", b.sock)
	return eventChan, nil
}

func (s *SockConn) Send(target *ChatID, message string, format Format) error {
	_, err := s.SendWithID(target, message, format)
	return err
}

func (s *SockConn) SendWithID(target *ChatID, message string, format Format) (string, error) {
	return sockConnMessageID, s.send(message)
}

func (s *SockConn) Update(target *ChatID, id string, message string, format Format) error {
	return s.send(message)
}

func (s *SockConn) Archive(target *ChatID) error {
	return nil
}

func (b *SockConn) Close() error {
	return nil
}

func (b *SockConn) MentionBot() string {
	return ""
}

func (b *SockConn) Mention(user string) string {
	return "@" + user
}

func (b *SockConn) ParseMention(user string) (string, error) {
	if matches := sockUserLinkRegex.FindStringSubmatch(user); len(matches) > 0 {
		return matches[1], nil
	}
	return "", errors.New("invalid user")
}

func (b *SockConn) Unescape(s string) string {
	return s
}

func (b *SockConn) shutdownListener(ctx context.Context, listener net.Listener) {
	<-ctx.Done()
	b.mu.Lock()
	if listener != nil {
		listener.Close()
	}
	b.mu.Unlock()
	os.Remove(b.sock)
}

func (b *SockConn) handleConns(listener net.Listener, eventChan chan event) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("err accepting: %s", err.Error())
			continue
		}
		go func() {
			if err := b.handleConn(conn, eventChan); err != nil {
				log.Printf("connection error: %s", err)
			}
		}()
	}
}

func (b *SockConn) handleConn(conn net.Conn, eventChan chan event) error {
	defer conn.Close()
	rd := bufio.NewReader(conn)
	if _, err := io.WriteString(conn, "Username (default)> "); err != nil {
		return err
	}
	line, err := rd.ReadString('\n')
	if err != nil {
		return err
	}
	user := util.SanitizeNonAlphanumeric(strings.TrimSpace(line))
	if user == "" {
		user = "default"
	}
	b.mu.Lock()
	if _, ok := b.conns[user]; ok {
		b.mu.Unlock()
		io.WriteString(conn, "User already connected, choose different name.\n")
		return nil
	}
	b.conns[user] = conn
	b.mu.Unlock()
	log.Printf("User %s connected", user)

	defer func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.conns, user)
		log.Printf("User %s disconnected", user)
	}()

	b.send("")

	for {
		line, err := rd.ReadString('\n')
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		eventChan <- &messageEvent{
			ID:          sockConnMessageID,
			Channel:     "main",
			ChannelType: DM,
			Thread:      "",
			User:        user,
			Message:     strings.TrimSpace(line),
		}
	}
}

func (b *SockConn) send(message string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for user, conn := range b.conns {
		_, err := io.WriteString(conn, fmt.Sprintf("\x1b[H\x1b[J%s\n%s> ", message, user))
		if err != nil {
			log.Printf("cannot send to user %s", user)
		}
	}
	return nil
}
