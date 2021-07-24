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
	errExit          = errors.New("exited REPL")
	errSessionClosed = errors.New("session closed")
)

const (
	maxMessageLength  = 512
	exitMarkerCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	welcomeMessage    = "REPLbot welcomes you!\n\nYou may start a new session by choosing any one of the " +
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
	launcherScript = "stty -echo; %s; echo; echo %s"
)

type Session struct {
	ID            string
	sender        Sender
	scripts       map[string]string
	started       time.Time
	lastAction    time.Time
	userInputChan chan string
	active        bool
	mu            sync.RWMutex
}

func NewSession(id string, sender Sender, scripts map[string]string) *Session {
	session := &Session{
		ID:            id,
		started:       time.Now(),
		lastAction:    time.Now(), // TODO close stale sessions
		sender:        sender,
		scripts:       scripts,
		userInputChan: make(chan string, 10), // buffered!
		active:        true,
	}
	go func() {
		if err := session.userInputLoop(); err != nil {
			log.Printf("[session %s] fatal error: %s", id, err.Error())
		}
	}()
	return session
}

// Send handles user input by forwarding to the underlying shell
func (s *Session) Send(message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return errSessionClosed
	}
	s.userInputChan <- message
	return nil
}

func (s *Session) Active() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *Session) userInputLoop() error {
	defer s.close()
	if err := s.sayHello(); err != nil {
		return err
	}
	for input := range s.userInputChan {
		switch input {
		case exitCommand:
			_ = s.sender.Send(byeMessage, Text)
			return nil
		case helpCommand:
			if err := s.sender.Send(availableCommandsMessage, Markdown); err != nil {
				return err
			}
		default:
			script, ok := s.scripts[input]
			if ok {
				if err := s.execREPL(script); err != nil && err != errExit {
					return err
				}
				if err := s.sayExited(); err != nil {
					return err
				}
			} else {
				if err := s.sender.Send("Invalid command", Text); err != nil {
					return err
				}
			}
		}
	}
	return nil
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

func (s *Session) execREPL(command string) error {
	log.Printf("[session %s] Started REPL session", s.ID)
	defer log.Printf("[session %s] Closed REPL session", s.ID)

	if err := s.sender.Send(sessionStartedMessage, Text); err != nil {
		return err
	}

	exitMarker := RandomStringWithCharset(10, exitMarkerCharset)
	cmd := exec.Command("sh", "-c", fmt.Sprintf(launcherScript, command, exitMarker))
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("cannot start REPL session: %s", err.Error())
	}

	outChan := make(chan []byte, 10)
	g, ctx := errgroup.WithContext(context.Background())
	g.Go(func() error {
		log.Printf("[session %s] Started command output loop", s.ID)
		defer log.Printf("[session %s] Exiting command output loop", s.ID)
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
				} else if strings.TrimSpace(string(buf[:n])) == exitMarker {
					return errExit
				} else if n > 0 {
					outChan <- buf[:n]
				}
			}
		}
	})
	g.Go(func() error {
		log.Printf("[session %s] Started response loop", s.ID)
		defer log.Printf("[session %s] Exiting response loop", s.ID)
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
		log.Printf("[session %s] Started user input loop", s.ID)
		defer log.Printf("[session %s] Exiting user input loop", s.ID)
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
		defer log.Printf("[session %s] Command cleanup finished", s.ID)
		<-ctx.Done()
		if err := KillChildProcesses(cmd.Process.Pid); err != nil {
			log.Printf("warning: %s", err.Error())
		}
		if err := ptmx.Close(); err != nil {
			log.Printf("warning: %s", err.Error())
		}
		return nil
	})
	return g.Wait()
}

func (s *Session) handleUserInput(input string, outputWriter io.Writer) error {
	switch input {
	case helpCommand:
		return s.sender.Send(availableCommandsMessage, Markdown)
	case exitCommand:
		return errExit
	default:
		// TODO properly handle empty lines
		if controlChar, ok := controlCharTable[input[1:]]; ok {
			_, err := outputWriter.Write([]byte{controlChar})
			return err
		}
		_, err := io.WriteString(outputWriter, fmt.Sprintf("%s\n", input))
		return err
	}
}

func (s *Session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = false
	log.Printf("[session %s] Session closed", s.ID)
}
