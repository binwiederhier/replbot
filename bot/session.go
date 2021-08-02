package bot

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sync/errgroup"
	"heckel.io/replbot/config"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	// consoleCodeRegex is a regex describing console escape sequences that we're stripping out.
	// This regex matches
	// - ECMA-48 CSI sequences: ESC [ ... <char>
	// - DEC Private Mode (DECSET/DECRST) sequences: ESC [ ? ... <char>
	// - Other escape sequences: ESC [N O P X P X ^ ...]
	// See https://man7.org/linux/man-pages/man4/console_codes.4.html
	consoleCodeRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\[\?[0-9;]*[a-zA-Z]|\x1b[clmnoDEFHMNOPXZ78^\\*+<=|}~]`)
	controlCharTable = map[string]string{
		"c": "^C",
		"d": "^D",
		"r": "^M",
	}
	errExit          = errors.New("exited REPL")
	errSessionClosed = errors.New("session closed")
)

const (
	maxMessageLength = 512
	welcomeMessage   = "REPLbot welcomes you!\n\nYou may start a new session by choosing any one of the " +
		"available REPLs: %s. Type `!h` for help and `!q` to exit this session."
	sessionStartedMessage = "Started a new REPL session"
	sessionExitedMessage  = "REPL session ended.\n\nYou may start a new session by choosing any one of the " +
		"available REPLs: %s. Type `!h` for help and `!q` to exit this session."
	byeMessage               = "REPLbot says bye bye!"
	timeoutWarningMessage    = "Are you still there? Your session will time out in one minute."
	timeoutReachedMessage    = "Timeout reached. REPLbot says bye bye!"
	forceCloseMessage        = "REPLbot has to go. Urgent REPL-related business. Bye!"
	invalidCommandMessage    = "I don't understand. Type `!h` for help."
	helpCommand              = "!h"
	exitCommand              = "!q"
	commentPrefix            = "## "
	availableCommandsMessage = "Available commands:\n" +
		"  `!r` - Send empty return\n" +
		"  `!c`, `!d` - Send Ctrl-C/Ctrl-D command sequence\n" +
		"  `!q` - Exit this session"
)

type session struct {
	ID            string
	config        *config.Config
	sender        Sender
	userInputChan chan string
	ctx           context.Context
	cancelFn      context.CancelFunc
	active        bool
	warnTimer     *time.Timer
	closeTimer    *time.Timer
	closeGroup    *errgroup.Group
	mu            sync.RWMutex
}

func NewSession(config *config.Config, id string, sender Sender) *session {
	ctx, cancel := context.WithCancel(context.Background())
	closeGroup, ctx := errgroup.WithContext(ctx)
	s := &session{
		ID:            id,
		config:        config,
		sender:        sender,
		userInputChan: make(chan string, 10), // buffered!
		ctx:           ctx,
		cancelFn:      cancel,
		active:        true,
		warnTimer:     time.NewTimer(config.IdleTimeout - time.Minute),
		closeTimer:    time.NewTimer(config.IdleTimeout),
		closeGroup:    closeGroup,
	}
	closeGroup.Go(func() error {
		err := s.userInputLoop()
		if err != nil {
			log.Printf("[session %s] FATAL ERROR: %s", id, err.Error())
		}
		return err
	})
	closeGroup.Go(s.activityMonitor)
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

func (s *session) Close() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.cancelFn()
	close(s.userInputChan)
	s.active = false
	s.mu.Unlock()
	if err := s.closeGroup.Wait(); err != nil && err != errExit {
		log.Printf("[session %s] Warning: %s", s.ID, err.Error())
	}
	log.Printf("[session %s] Session closed", s.ID)
}

func (s *session) CloseWithMessage() {
	_ = s.sender.Send(forceCloseMessage, Text)
	s.Close()
}

func (s *session) userInputLoop() error {
	defer s.Close()
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
				if err := s.sender.Send(invalidCommandMessage, Markdown); err != nil {
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
	if !s.active {
		return nil
	}
	return s.sender.Send(fmt.Sprintf(sessionExitedMessage, s.replList()), Markdown)
}

func (s *session) replList() string {
	return fmt.Sprintf("`%s`", strings.Join(s.config.Scripts(), "`, `"))
}

func (s *session) execREPL(script string) error {
	return runREPL(s.ctx, s.ID, s.sender, s.userInputChan, script)
}

func (s *session) activityMonitor() error {
	defer s.warnTimer.Stop()
	defer s.closeTimer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return errExit
		case <-s.warnTimer.C:
			_ = s.sender.Send(timeoutWarningMessage, Text)
			log.Printf("[session %s] Session has been idle for a long time. Warning sent to user.", s.ID)
		case <-s.closeTimer.C:
			_ = s.sender.Send(timeoutReachedMessage, Text)
			log.Printf("[session %s] Idle timeout reached. Closing session.", s.ID)
			s.Close()
			return nil
		}
	}
}
