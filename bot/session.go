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
	timeoutWarningMessage    = "Are you still there? Your session will time out in one minute."
	timeoutReachedMessage    = "Timeout reached. REPLbot says bye bye!"
	helpCommand              = "!help"
	exitCommand              = "!exit"
	availableCommandsMessage = "Available commands:\n" +
		"  `!ret`, `!r` - Send empty return\n" +
		"  `!ctrl-c`, `!ctrl-d`, ... - Send command sequence\n" +
		"  `!exit` - Exit this session"
	launcherScript = "stty -echo; %s run %s; echo; echo %s"
	killScript     = "%s kill %s"
)

type session struct {
	ID            string
	config        *config.Config
	sender        Sender
	scripts       map[string]string
	userInputChan chan string
	cancelFn      context.CancelFunc
	active        bool
	closing       bool
	warnTimer     *time.Timer
	closeTimer    *time.Timer
	mu            sync.RWMutex
}

func NewSession(config *config.Config, id string, sender Sender, scripts map[string]string) *session {
	s := &session{
		ID:            id,
		config:        config,
		sender:        sender,
		scripts:       scripts,
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
			script, ok := s.scripts[input]
			if ok {
				if err := s.execREPL(input, script); err != nil && err != errExit {
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
	return s.sender.Send(fmt.Sprintf(welcomeMessage, strings.Join(s.replList(), ", ")), Markdown)
}

func (s *session) maybeSayExited() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closing {
		return nil
	}
	return s.sender.Send(fmt.Sprintf(sessionExitedMessage, strings.Join(s.replList(), ", ")), Markdown)
}

func (s *session) replList() []string {
	repls := make([]string, 0)
	for name, _ := range s.scripts {
		repls = append(repls, fmt.Sprintf("`%s`", name))
	}
	return repls
}

func (s *session) execREPL(name string, command string) error {
	log.Printf("[session %s] Started REPL %s", s.ID, name)
	defer log.Printf("[session %s] Closed REPL %s", s.ID, name)

	r := &repl{
		sessionID:     s.ID,
		sender:        s.sender,
		userInputChan: s.userInputChan,
	}
	s.mu.Lock()
	var ctx context.Context
	ctx, s.cancelFn = context.WithCancel(context.Background())
	s.mu.Unlock()

	err := r.Exec(ctx, command)

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
