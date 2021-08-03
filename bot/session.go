package bot

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sync/errgroup"
	"heckel.io/replbot/config"
	"heckel.io/replbot/util"
	"log"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
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
		"c":     "^C",
		"d":     "^D",
		"r":     "^M",
		"esc":   "\\033",   // ESC
		"up":    "\\033OA", // Cursor up
		"down":  "\\033OB", // Cursor down
		"right": "\\033OC", // Cursor right
		"left":  "\\033OD", // Cursor left
	}

	// updateMessageUserInputCountLimit is a value that defines when to post a new message as opposed to updating
	// the existing message
	updateMessageUserInputCountLimit = int32(5)
	messageUpdateTimeLimit           = 270 * time.Second // 5 minutes in Slack, minus a little bit of a buffer
	errExit                          = errors.New("exited REPL")
	errSessionClosed                 = errors.New("session closed")
)

const (
	sessionStartedMessage = "🚀 REPL started. Type `!h` to see a list of available commands, or `!q` to forcefully " +
		"exit the REPL. Lines prefixed with `##` are treated as comments."
	sessionExitedMessage     = "👋 REPL exited. See you later!"
	timeoutWarningMessage    = "⏱️ Are you still there? Your session will time out in one minute."
	timeoutReachedMessage    = "👋️ Timeout reached. REPLbot says bye bye!"
	forceCloseMessage        = "🏃 REPLbot has to go. Urgent REPL-related business. Bye!"
	helpCommand              = "!h"
	exitCommand              = "!q"
	commentPrefix            = "## "
	availableCommandsMessage = "Available commands:\n" +
		"  `!r` - Send empty return\n" +
		"  `!c`, `!d`, `!esc` - Send Ctrl-C/Ctrl-D/ESC\n" +
		"  `!up`, `!down`, `!left`, `!right` - Send cursor up, down, left or right\n" +
		"  `!q` - Exit REPL"
)

type Session struct {
	id             string
	config         *config.Config
	sender         Sender
	userInputChan  chan string
	userInputCount int32
	g              *errgroup.Group
	ctx            context.Context
	cancelFn       context.CancelFunc
	active         bool
	warnTimer      *time.Timer
	closeTimer     *time.Timer
	script         string
	mode           string
	screen         *util.Screen
	mu             sync.RWMutex
}

func NewSession(config *config.Config, id string, sender Sender, script string, mode string) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)
	return &Session{
		id:             id,
		config:         config,
		sender:         sender,
		script:         script,
		screen:         util.NewScreen(),
		mode:           mode,
		userInputChan:  make(chan string, 10), // buffered!
		userInputCount: 0,
		g:              g,
		ctx:            ctx,
		cancelFn:       cancel,
		active:         true,
		warnTimer:      time.NewTimer(config.IdleTimeout - time.Minute),
		closeTimer:     time.NewTimer(config.IdleTimeout),
	}
}

func (s *Session) Run() error {
	log.Printf("[session %s] Started REPL session", s.id)
	defer log.Printf("[session %s] Closed REPL session", s.id)
	if err := s.screen.Start(s.script); err != nil {
		return err
	}
	if err := s.sender.Send(sessionStartedMessage, Text); err != nil {
		return err
	}
	s.g.Go(s.userInputLoop)
	s.g.Go(s.commandOutputLoop)
	s.g.Go(s.activityMonitor)
	s.g.Go(s.shutdownHandler)
	if err := s.g.Wait(); err != nil && err != errExit {
		return err
	}
	return nil
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

func (s *Session) ForceClose() error {
	_ = s.sender.Send(forceCloseMessage, Text)
	s.cancelFn()
	if err := s.g.Wait(); err != nil && err != errExit {
		return err
	}
	return nil
}

func (s *Session) shutdownHandler() error {
	log.Printf("[session %s] Starting shutdown handler", s.id)
	defer log.Printf("[session %s] Exiting shutdown handler", s.id)
	<-s.ctx.Done()
	if err := s.screen.Stop(); err != nil {
		log.Printf("warning: unable to stop screen: %s", err.Error())
	}
	if err := s.sender.Send(sessionExitedMessage, Markdown); err != nil {
		return err
	}
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()
	return nil
}

func (s *Session) activityMonitor() error {
	log.Printf("[session %s] Started activity monitor", s.id)
	defer func() {
		s.warnTimer.Stop()
		s.closeTimer.Stop()
		log.Printf("[session %s] Exiting activity monitor", s.id)
	}()
	for {
		select {
		case <-s.ctx.Done():
			return errExit
		case <-s.warnTimer.C:
			_ = s.sender.Send(timeoutWarningMessage, Text)
			log.Printf("[session %s] Session has been idle for a long time. Warning sent to user.", s.id)
		case <-s.closeTimer.C:
			_ = s.sender.Send(timeoutReachedMessage, Text)
			log.Printf("[session %s] Idle timeout reached. Closing session.", s.id)
			return errExit
		}
	}
}

func (s *Session) userInputLoop() error {
	log.Printf("[session %s] Started user input loop", s.id)
	defer log.Printf("[session %s] Exiting user input loop", s.id)
	for {
		select {
		case line := <-s.userInputChan:
			if err := s.handleUserInput(line); err != nil {
				return err
			}
		case <-s.ctx.Done():
			return errExit
		}
	}
}

func (s *Session) handleUserInput(input string) error {
	switch input {
	case helpCommand:
		atomic.AddInt32(&s.userInputCount, updateMessageUserInputCountLimit)
		return s.sender.Send(availableCommandsMessage, Markdown)
	case exitCommand:
		return errExit
	default:
		atomic.AddInt32(&s.userInputCount, 1)
		if strings.HasPrefix(input, commentPrefix) {
			return nil // Ignore comments
		} else if len(input) > 1 {
			if controlChar, ok := controlCharTable[input[1:]]; ok {
				return s.screen.Stuff(controlChar)
			}
		}
		return s.screen.Paste(fmt.Sprintf("%s\n", input))
	}
}

func (s *Session) commandOutputLoop() error {
	log.Printf("[session %s] Started command output loop", s.id)
	defer log.Printf("[session %s] Exiting command output loop", s.id)
	var id, last, lastID string
	var lastTime time.Time
	for {
		select {
		case <-s.ctx.Done():
			return errExit
		case <-time.After(200 * time.Millisecond):
			current, err := s.screen.Hardcopy()
			if err != nil {
				return errExit // Treat this as a failure
			} else if current == last || current == "" {
				continue
			}
			sanitized := consoleCodeRegex.ReplaceAllString(current, "")
			updateMessage := id != "" && atomic.LoadInt32(&s.userInputCount) < updateMessageUserInputCountLimit && time.Since(lastTime) < messageUpdateTimeLimit
			if updateMessage {
				if err := s.sender.Update(lastID, sanitized, Code); err != nil {
					if id, err = s.sender.SendWithID(sanitized, Code); err != nil {
						return err
					}
					atomic.StoreInt32(&s.userInputCount, 0)
				}
			} else {
				if id, err = s.sender.SendWithID(sanitized, Code); err != nil {
					return err
				}
				atomic.StoreInt32(&s.userInputCount, 0)
			}
			last = current
			lastID = id
			lastTime = time.Now()
		}
	}
}
