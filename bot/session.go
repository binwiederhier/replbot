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

	// controlCharTable is a translation table that translates Slack input commands "!<command>" to
	// screen key bindings, see https://www.gnu.org/software/screen/manual/html_node/Input-Translation.html#Input-Translation
	controlCharTable = map[string]string{
		"c":   "^C",
		"d":   "^D",
		"r":   "^M",
		"esc": "\\033",   // ESC
		"ku":  "\\033OA", // Cursor up
		"kd":  "\\033OB", // Cursor down
		"kr":  "\\033OC", // Cursor right
		"kl":  "\\033OD", // Cursor left
	}

	// updateMessageUserInputCountLimit is a value that defines when to post a new message as opposed to updating
	// the existing message
	updateMessageUserInputCountLimit = int32(5)
	messageUpdateTimeLimit           = 270 * time.Second // 5 minutes in Slack, minus a little bit of a buffer
	errExit                          = errors.New("exited REPL")
	errSessionClosed                 = errors.New("session closed")
)

const (
	welcomeMessage = "👋 REPLbot says hello!\n\nYou may start a new session by choosing any one of the " +
		"available REPLs: %s. Type `!q` to exit this session."
	helpMessage = "You may start a new session by choosing any one of the " +
		"available REPLs: %s. Type `!q` to exit this session."
	sessionStartedMessage = "🚀 REPL started. Type `!h` to see a list of available commands, or `!q` to forcefully " +
		"exit the REPL. Lines prefixed with `##` are treated as comments."
	sessionExitedMessage  = "👋 REPL exited.\n\nYou may start a new session by choosing any one of the " +
		"available REPLs: %s. Type `!q` to exit this session."
	byeMessage               = "👋 REPLbot says bye bye!"
	timeoutWarningMessage    = "⏱️ Are you still there? Your session will time out in one minute."
	timeoutReachedMessage    = "👋️ Timeout reached. REPLbot says bye bye!"
	forceCloseMessage        = "🏃 REPLbot has to go. Urgent REPL-related business. Bye!"
	invalidCommandMessage    = "I don't understand. Type `!h` for help."
	helpCommand              = "!h"
	exitCommand              = "!q"
	commentPrefix            = "## "
	availableCommandsMessage = "Available commands:\n" +
		"  `!r` - Send empty return\n" +
		"  `!c`, `!d`, `!esc` - Send Ctrl-C/Ctrl-D/ESC\n" +
		"  `!ku`, `!kd`, `!kl`, `!kr` - Send cursor up, down, left or right\n" +
		"  `!q` - Exit REPL"
)

type Session struct {
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

func NewSession(config *config.Config, id string, sender Sender) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	closeGroup, ctx := errgroup.WithContext(ctx)
	s := &Session{
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

// HandleUserInput handles user input by forwarding to the underlying shell
func (s *Session) HandleUserInput(message string) error {
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

func (s *Session) Active() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *Session) CloseWithMessageAndWait() {
	_ = s.sender.Send(forceCloseMessage, Text)
	s.closeNoWait()
	if err := s.closeGroup.Wait(); err != nil && err != errExit {
		log.Printf("[session %s] Warning: %s", s.ID, err.Error())
	}
}

func (s *Session) closeNoWait() {
	log.Printf("[session %s] Closing session", s.ID)
	defer log.Printf("[session %s] Session closed", s.ID)
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.cancelFn()
	close(s.userInputChan)
	s.active = false
	s.mu.Unlock()
}

func (s *Session) userInputLoop() error {
	defer s.closeNoWait()
	if err := s.sayHello(); err != nil {
		return err
	}
	for input := range s.userInputChan {
		switch input {
		case exitCommand:
			_ = s.sender.Send(byeMessage, Text)
			return nil
		case helpCommand:
			if err := s.sender.Send(fmt.Sprintf(helpMessage, s.replList()), Markdown); err != nil {
				return err
			}
		default:
			if strings.HasPrefix(input, commentPrefix) {
				// Ignore comments
			} else if script := s.config.Script(input); script != "" {
				if err := s.execREPL(script); err != nil {
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

func (s *Session) sayHello() error {
	return s.sender.Send(fmt.Sprintf(welcomeMessage, s.replList()), Markdown)
}

func (s *Session) maybeSayExited() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.active {
		return nil
	}
	return s.sender.Send(fmt.Sprintf(sessionExitedMessage, s.replList()), Markdown)
}

func (s *Session) replList() string {
	return fmt.Sprintf("`%s`", strings.Join(s.config.Scripts(), "`, `"))
}

func (s *Session) execREPL(script string) error {
	return runREPL(s.ctx, s.ID, s.sender, s.userInputChan, script)
}

func (s *Session) activityMonitor() error {
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
			s.closeNoWait()
			return nil
		}
	}
}
