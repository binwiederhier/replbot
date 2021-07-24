package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/creack/pty"
	"golang.org/x/sync/errgroup"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	shellEscapeRegex = regexp.MustCompile(`\x1B\[[0-9;]*[a-zA-Z]`)
	controlCharTable = map[string]byte{
		"c":      0x03,
		"ctrl-c": 0x03,
		"d":      0x04,
		"ctrl-d": 0x04,
		"r":      0x10,
		"ret":    0x10,
	}
	welcomeMessage = "REPLbot welcomes you!\n\nYou may start a new session by choosing any one of the " +
		"available REPLs: %s. Type `!help` for help and `!exit` to exit this session."
	sessionStartedMessage = "Started a new REPL session"
	sessionExitedMessage  = "REPL session ended.\n\nYou may start a new session by choosing any one of the " +
		"available REPLs: %s. Type `!help` for help and `!exit` to exit this session."
	byeMessage               = "REPLbot says bye bye!"
	helpCommand              = "!help"
	exitCommand              = "!exit"
	availableCommandsMessage = "Available commands:\n" +
		"  `!ret`, `!r` - Send empty return\n" +
		"  `!ctrl-c`, `!ctrl-d`, ... - Send command sequence\n" +
		"  `!exit` - Exit this session"
	errExit = errors.New("exited REPL session")
)

const (
	maxMessageLength = 512
)

type Session struct {
	id            string
	sender        Sender
	scripts       map[string]string
	started       time.Time
	lastAction    time.Time
	userInputChan chan string
	closed        bool
	mu            sync.Mutex
}

func NewSession(id string, sender Sender, scripts map[string]string) *Session {
	session := &Session{
		id:            id,
		sender:        sender,
		scripts:       scripts,
		started:       time.Now(),
		lastAction:    time.Now(),
		userInputChan: make(chan string, 10), // buffered!
		closed:        false,
	}
	go session.userInputLoop()
	return session
}

func (s *Session) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Session) userInputLoop() {
	s.sayHello()

	for input := range s.userInputChan {
		if input == exitCommand {
			s.close()
			return
		} else if input == helpCommand {
			s.sender.Send(availableCommandsMessage, Markdown)
			continue
		}
		command, ok := s.scripts[input]
		if !ok {
			s.sender.Send("Invalid command", Text)
			continue
		}
		if err := s.replSession(command); err != nil && err != errExit {
			s.sender.Send(err.Error(), Text)
		}
		s.sayExited()
	}
}

func (s *Session) sayHello() error {
	return s.sender.Send(fmt.Sprintf(welcomeMessage, strings.Join(s.replList(), ", ")), Markdown)
}

func (s *Session) sayExited() error {
	return s.sender.Send(fmt.Sprintf(sessionExitedMessage, strings.Join(s.replList(), ", ")), Markdown)
}

func (s *Session) replList() []string {
	repls := make([]string, 0)
	for name, _ := range s.scripts {
		repls = append(repls, fmt.Sprintf("`%s`", name))
	}
	return repls
}

func (s *Session) replSession(command string) error {
	log.Printf("[session %s] Started REPL session", s.id)
	defer log.Printf("[session %s] Closed REPL session", s.id)

	if err := s.sender.Send(sessionStartedMessage, Text); err != nil {
		return err
	}

	c := exec.Command("sh", "-c", "stty -echo; "+command+"; echo; echo exited")
	ptmx, err := pty.Start(c)
	if err != nil {
		return fmt.Errorf("cannot start REPL session: %s", err.Error())
	}

	outChan := make(chan []byte, 10)
	g, ctx := errgroup.WithContext(context.Background())

	g.Go(func() error {
		log.Printf("[session %s] Started command output loop", s.id)
		defer log.Printf("[session %s] Exiting command output loop", s.id)
		for {
			buf := make([]byte, 4096) // Allocation in a loop, ahhh ...
			n, err := ptmx.Read(buf)
			select {
			case <-ctx.Done():
				return nil
			default:
				if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
					// An expected error when the ptmx is closed to break the Read() call.
					// Since we don't want to send this error to the user, we convert it to errExit.
					return errExit
				} else if err == io.EOF {
					if n > 0 {
						outChan <- buf[:n]
					}
					return errExit
				} else if err != nil {
					return err
				} else if strings.TrimSpace(string(buf[:n])) == "exited" {
					return errExit
				} else if n > 0 {
					outChan <- buf[:n]
				}
			}
		}
	})
	g.Go(func() error {
		log.Printf("[session %s] Started response loop", s.id)
		defer log.Printf("[session %s] Exiting response loop", s.id)
		var message string
		for {
			select {
			case result := <-outChan:
				message += shellEscapeRegex.ReplaceAllString(string(result), "")
				if len(message) > maxMessageLength {
					if err := s.sender.Send(message, Code); err != nil {
						return err
					}
					message = ""
				}
			case <-time.After(300 * time.Millisecond):
				if len(message) > 0 {
					if err := s.sender.Send(message, Code); err != nil {
						return err
					}
					message = ""
				}
			case <-ctx.Done():
				if len(message) > 0 {
					if err := s.sender.Send(message, Code); err != nil {
						return err
					}
				}
				return nil
			}
		}
	})

	g.Go(func() error {
		log.Printf("[session %s] Started user input loop", s.id)
		defer log.Printf("[session %s] Exiting user input loop", s.id)
		for {
			select {
			case line := <-s.userInputChan:
				if err := s.handleUserInput(line, ptmx); err != nil {
					return err
				}
			case <-ctx.Done():
				return nil
			}
		}
	})
	g.Go(func() error {
		defer log.Printf("[session %s] Command cleanup finished", s.id)
		<-ctx.Done()
		if err := killChildren(c.Process.Pid); err != nil {
			log.Printf("warning: %s", err.Error())
		}
		if err := ptmx.Close(); err != nil {
			log.Printf("warning: %s", err.Error())
		}
		return nil
	})

	return g.Wait()
}

func (s *Session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.sender.Send(byeMessage, Text)
	s.closed = true
	log.Printf("[session %s] Session closed", s.id)
}

func (s *Session) handleUserInput(input string, outputWriter io.Writer) error {
	switch input {
	case helpCommand:
		return s.sender.Send(availableCommandsMessage, Markdown)
	case exitCommand:
		return errExit
	default:
		if controlChar, ok := controlCharTable[input[1:]]; ok {
			_, err := outputWriter.Write([]byte{controlChar})
			return err
		}
		_, err := io.WriteString(outputWriter, fmt.Sprintf("%s\n", input))
		return err
	}
}
