package bot

import (
	"context"
	"errors"
	"fmt"
	"heckel.io/replbot/config"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	shellEscapeRegex = regexp.MustCompile(`\x1B\[[0-9;]*[a-zA-Z]`)
	controlCharTable = map[string]byte{
		"c": 0x03,
		"d": 0x04,
		"r": 0x10,
	}
	errExit          = errors.New("exited REPL")
	errSessionClosed = errors.New("session closed")
)

const (
	maxMessageLength = 512
	charsetRandomID  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	welcomeMessage   = "REPLbot welcomes you!\n\nYou may start a new session by choosing any one of the " +
		"available REPLs: %s. Type `!h` for help and `!q` to exit this session."
	sessionStartedMessage = "Started a new REPL session"
	sessionExitedMessage  = "REPL session ended.\n\nYou may start a new session by choosing any one of the " +
		"available REPLs: %s. Type `!h` for help and `!q` to exit this session."
	byeMessage               = "REPLbot says bye bye!"
	timeoutWarningMessage    = "Are you still there? Your session will time out in one minute."
	timeoutReachedMessage    = "Timeout reached. REPLbot says bye bye!"
	helpCommand              = "!h"
	exitCommand              = "!q"
	commentPrefix            = "## "
	availableCommandsMessage = "Available commands:\n" +
		"  `!r` - Send empty return\n" +
		"  `!c`, `!d` - Send Ctrl-C/Ctrl-D command sequence\n" +
		"  `!q` - Exit this session"
	runScript  = "%s run %s; echo; echo %s"
	killScript = "%s kill %s"
)

type session struct {
	ID            string
	config        *config.Config
	sender        Sender
	userInputChan chan string
	cancelFn      context.CancelFunc
	active        bool
	closing       bool
	warnTimer     *time.Timer
	closeTimer    *time.Timer
	mu            sync.RWMutex
}

func NewSession(config *config.Config, id string, sender Sender) *session {
	s := &session{
		ID:            id,
		config:        config,
		sender:        sender,
		userInputChan: make(chan string, 10), // buffered!
		cancelFn:      nil,
		active:        true,
		closing:       false,
		warnTimer:     time.NewTimer(config.IdleTimeout - time.Minute),
		closeTimer:    time.NewTimer(config.IdleTimeout),
	}
	go func() {
		if err := s.userInputLoop(); err != nil {
			log.Printf("[session %s] fatal error: %s", id, err.Error())
		}
	}()
	go s.activityMonitor()
	return s
}

// Send handles user input by forwarding to the underlying shell
func (s *session) Send(message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return errSessionClosed
	}
	s.warnTimer.Reset(s.config.IdleTimeout - time.Minute)
	s.closeTimer.Reset(s.config.IdleTimeout)
	s.userInputChan <- message
	return nil
}

func (s *session) Active() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *session) userInputLoop() error {
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
			if strings.HasPrefix(input, commentPrefix) {
				// Ignore comments
			} else if script := s.config.Script(input); script != "" {
				if err := s.execREPL(script); err != nil && err != errExit {
					return err
				}
				if err := s.maybeSayExited(); err != nil {
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

func (s *session) sayHello() error {
	return s.sender.Send(fmt.Sprintf(welcomeMessage, s.replList()), Markdown)
}

func (s *session) maybeSayExited() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closing {
		return nil
	}
	return s.sender.Send(fmt.Sprintf(sessionExitedMessage, s.replList()), Markdown)
}

func (s *session) replList() string {
	return fmt.Sprintf("`%s`", strings.Join(s.config.Scripts(), "`, `"))
}

func (s *session) execREPL(script string) error {
	s.mu.Lock()
	var ctx context.Context
	ctx, s.cancelFn = context.WithCancel(context.Background())
	s.mu.Unlock()

	err := runREPL(ctx, s.ID, s.sender, s.userInputChan, script)

	s.mu.Lock()
	s.cancelFn = nil
	s.mu.Unlock()
	return err
}

func (s *session) activityMonitor() {
	for {
		select {
		case <-s.warnTimer.C:
			_ = s.sender.Send(timeoutWarningMessage, Text)
			log.Printf("[session %s] Session has been idle for a long time. Warning sent to user.", s.ID)
		case <-s.closeTimer.C:
			_ = s.sender.Send(timeoutReachedMessage, Text)
			log.Printf("[session %s] Idle timeout reached. Closing session.", s.ID)
			s.mu.Lock()
			if s.cancelFn != nil {
				s.cancelFn()
			}
			close(s.userInputChan)
			s.closing = true
			s.mu.Unlock()
			return
		}
	}
}

func (s *session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = false
	s.warnTimer.Stop()
	s.closeTimer.Stop()
	log.Printf("[session %s] Session closed", s.ID)
}
