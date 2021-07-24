package bot

import (
	"errors"
	"fmt"
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
	helpCommand              = "!help"
	exitCommand              = "!exit"
	availableCommandsMessage = "Available commands:\n" +
		"  `!ret`, `!r` - Send empty return\n" +
		"  `!ctrl-c`, `!ctrl-d`, ... - Send command sequence\n" +
		"  `!exit` - Exit this session"
	launcherScript = "stty -echo; %s; echo; echo %s"
	warnIdleTime   = 10 * time.Second // 5 * time.Minute
	maxIdleTime    = 15 * time.Second // 6 * time.Minute
)

type session struct {
	ID            string
	sender        Sender
	scripts       map[string]string
	started       time.Time
	lastAction    time.Time
	userInputChan chan string
	active        bool
	warnTimer     *time.Timer
	closeTimer    *time.Timer
	mu            sync.RWMutex
}

func NewSession(id string, sender Sender, scripts map[string]string) *session {
	session := &session{
		ID:            id,
		started:       time.Now(),
		lastAction:    time.Now(), // TODO close stale sessions
		sender:        sender,
		scripts:       scripts,
		userInputChan: make(chan string, 10), // buffered!
		active:        true,
		warnTimer:     time.NewTimer(warnIdleTime),
		closeTimer:    time.NewTimer(maxIdleTime),
	}
	go func() {
		if err := session.userInputLoop(); err != nil {
			log.Printf("[session %s] fatal error: %s", id, err.Error())
		}
	}()
	go session.activityMonitor()
	return session
}

// Send handles user input by forwarding to the underlying shell
func (s *session) Send(message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return errSessionClosed
	}
	s.lastAction = time.Now()
	s.warnTimer.Reset(warnIdleTime)
	s.closeTimer.Reset(maxIdleTime)
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

func (s *session) sayHello() error {
	return s.sender.Send(fmt.Sprintf(welcomeMessage, strings.Join(s.replList(), ", ")), Markdown)
}

func (s *session) sayExited() error {
	return s.sender.Send(fmt.Sprintf(sessionExitedMessage, strings.Join(s.replList(), ", ")), Markdown)
}

func (s *session) replList() []string {
	repls := make([]string, 0)
	for name, _ := range s.scripts {
		repls = append(repls, fmt.Sprintf("`%s`", name))
	}
	return repls
}

func (s *session) execREPL(command string) error {
	log.Printf("[session %s] Started REPL session", s.ID)
	defer log.Printf("[session %s] Closed REPL session", s.ID)

	r := &repl{
		sessionID:     s.ID,
		sender:        s.sender,
		userInputChan: s.userInputChan,
	}
	return r.execREPL(command)
}

func (s *session) activityMonitor() {
	for {
		select {
		case <-s.warnTimer.C:
			_ = s.sender.Send("Are you still there? Your session will time out in one minute.", Text)
			log.Printf("[session %s] Session has been idle for %s. Warning sent to user.", s.ID, warnIdleTime.String())
		case <-s.closeTimer.C:
			_ = s.sender.Send("Timeout reached. Bye!", Text)
			log.Printf("[session %s] Idle timeout reached. Closing session.", s.ID)
			s.close()
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
