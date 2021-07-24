package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/creack/pty"
	"github.com/slack-go/slack"
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
		"r":      0x10,
		"ret":    0x10,
		"ctrl-c": 0x03,
		"ctrl-d": 0x04,
	}
	welcomeMessage = "REPLbot welcomes you!\n\nYou may start a new session by choosing any one of the " +
		"available REPLs: %s. Type `!help` for help and `!exit` to exit this session."
	sessionExitedMessage = "REPL session ended.\n\nYou may start a new session by choosing any one of the " +
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
	scripts       map[string]string
	rtm           *slack.RTM
	started       time.Time
	lastAction    time.Time
	channel       string
	threadTS      string
	userInputChan chan string
	closed        bool
	mu            sync.Mutex
}

func NewSession(scripts map[string]string, rtm *slack.RTM, channel string, threadTS string) *Session {
	session := &Session{
		scripts:       scripts,
		rtm:           rtm,
		started:       time.Now(),
		lastAction:    time.Now(),
		channel:       channel,
		threadTS:      threadTS,
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
			s.close(byeMessage)
			return
		} else if input == helpCommand {
			s.sendMarkdown(availableCommandsMessage)
			continue
		}
		command, ok := s.scripts[input]
		if !ok {
			s.sendMarkdown("Invalid command")
			continue
		}
		if err := s.replSession(command); err != nil && err != errExit {
			s.sendMarkdown(err.Error())
		}
		s.sayExited()
	}
}

func (s *Session) sayHello() error {
	_, err := s.sendMarkdown(fmt.Sprintf(welcomeMessage, strings.Join(s.replList(), ", ")))
	return err
}

func (s *Session) sayExited() error {
	_, err := s.sendMarkdown(fmt.Sprintf(sessionExitedMessage, strings.Join(s.replList(), ", ")))
	return err
}

func (s *Session) replList() []string {
	repls := make([]string, 0)
	for name, _ := range s.scripts {
		repls = append(repls, fmt.Sprintf("`%s`", name))
	}
	return repls
}

func (s *Session) replSession(command string) error {
	log.Printf("[session %s] Started REPL session", s.threadTS)
	defer log.Printf("[session %s] Closed REPL session", s.threadTS)

	if _, err := s.sendMarkdown("Started a new REPL session"); err != nil {
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
		log.Printf("[session %s] Started command output loop", s.threadTS)
		defer log.Printf("[session %s] Exiting command output loop", s.threadTS)
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
		log.Printf("[session %s] Started response loop", s.threadTS)
		defer log.Printf("[session %s] Exiting response loop", s.threadTS)
		var message string
		for {
			select {
			case result := <-outChan:
				message += shellEscapeRegex.ReplaceAllString(string(result), "")
				if len(message) > maxMessageLength {
					s.sendCode(message)
					message = ""
				}
			case <-time.After(300 * time.Millisecond):
				if len(message) > 0 {
					s.sendCode(message)
					message = ""
				}
			case <-ctx.Done():
				if len(message) > 0 {
					s.sendCode(message)
				}
				return nil
			}
		}
	})

	g.Go(func() error {
		log.Printf("[session %s] Started user input loop", s.threadTS)
		defer log.Printf("[session %s] Exiting user input loop", s.threadTS)
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
		defer log.Printf("[session %s] Command cleanup finished", s.threadTS)
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

func (s *Session) sendText(message string) (string, error) {
	return s.send(slack.MsgOptionText(message, false))
}

func (s *Session) sendCode(message string) (string, error) {
	markdown := fmt.Sprintf("```%s```", strings.ReplaceAll(message, "```", "` ` `")) // Hack ...
	return s.sendMarkdown(markdown)
}

func (s *Session) sendMarkdown(markdown string) (string, error) {
	textBlock := slack.NewTextBlockObject("mrkdwn", markdown, false, true)
	sectionBlock := slack.NewSectionBlock(textBlock, nil, nil)
	return s.send(slack.MsgOptionBlocks(sectionBlock))
}

func (s *Session) send(options ...slack.MsgOption) (string, error) {
	options = append(options, slack.MsgOptionTS(s.threadTS))
	_, responseTS, err := s.rtm.PostMessage(s.channel, options...)
	if err != nil {
		log.Printf("Cannot send message: %s", err.Error())
		return "", err
	}
	return responseTS, nil
}

func (s *Session) close(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	log.Printf(message)
	s.sendText(message)
	s.closed = true
}

func (s *Session) handleUserInput(input string, outputWriter io.Writer) error {
	switch input {
	case helpCommand:
		_, err := s.sendMarkdown(availableCommandsMessage)
		return err
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

type result struct {
	bytes []byte
	err   error
}
