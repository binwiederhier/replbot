package bot

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sync/errgroup"
	"heckel.io/replbot/config"
	"heckel.io/replbot/util"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

var (
	// consoleCodeRegex is a regex describing console escape sequences that we're stripping out. This regex
	// only matches ECMA-48 CSI sequences (ESC [ ... <char>), which is enough since, we're using tmux's capture-pane.
	// See https://man7.org/linux/man-pages/man4/console_codes.4.html
	consoleCodeRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

	// sendKeysTable is a translation table that translates input commands "!<command>" to something that can be
	// send via tmux's send-keys command, see https://man7.org/linux/man-pages/man1/tmux.1.html#KEY_BINDINGS
	sendKeysTable = map[string]string{
		"c":     "^C",
		"d":     "^D",
		"ret":   "^M",
		"r":     "^M",
		"t":     "\t",
		"tt":    "\t\t",
		"esc":   "escape", // ESC
		"up":    "up",     // Cursor up
		"down":  "down",   // Cursor down
		"right": "right",  // Cursor right
		"left":  "left",   // Cursor left
		"space": "space",  // Space
		"pd":    "ppage",  // Page up
		"pu":    "npage",  // Page down
	}

	errExit = errors.New("exited REPL")
)

const (
	sessionStartedMessage = "🚀 REPL started. Type `!help` to see a list of available commands, or `!exit` to forcefully " +
		"exit the REPL. Lines prefixed with `!!` are treated as comments.%s"
	splitModeThreadMessage         = "Use this thread to enter your commands. Your output will appear in the main channel."
	sessionExitedMessage           = "👋 REPL exited. See you later!"
	timeoutWarningMessage          = "⏱️ Are you still there? Your session will time out in one minute."
	forceCloseMessage              = "🏃 REPLbot has to go. Urgent REPL-related business. Sorry about that!"
	malformatedTerminalSizeMessage = "🙁 You entered an invalid size. Use `tiny`, `small`, `medium`, `large` or `WxH` instead."
	invalidTerminalSizeMessage     = "🙁 Oh my, you requested a terminal size that is quite unusual. I can't let you do that. " +
		"The minimal supported size is %dx%d, the maximal size is %dx%d.\n\n"
	helpCommand              = "!help"
	helpShortCommand         = "!h"
	exitCommand              = "!exit"
	exitShortCommand         = "!q"
	screenCommand            = "!screen"
	screenShortCommand       = "!s"
	resizePrefix             = "!resize "
	commentPrefix            = "!! "
	rawPrefix                = "!n "
	unquotePrefix            = "!e "
	availableCommandsMessage = "Available commands:\n" +
		"  `!ret`, `!r` - Send empty return\n" +
		"  `!n ...` - Text without a new line\n" +
		"  `!e ...` - Text with escape sequences (`\\n`, `\\t`, ...)\n" +
		"  `!c`, `!d`, `!esc`, `!space` - Send Ctrl-C/Ctrl-D/ESC/Space\n" +
		"  `!t`, `!tt` - Send TAB / double-TAB\n" +
		"  `!up`, `!down`, `!left`, `!right` - Send cursor keys\n" +
		"  `!pu`, `!pd` - Send page up / page down\n" +
		"  `!! ...` - Comment\n" +
		"  `!resize ...` - Resize window\n" +
		"  `!screen`, `!s` - Re-send a new terminal window\n" +
		"  `!help`, `!h` - Show this help screen\n" +
		"  `!exit`, `!q` - Exit REPL"

	// updateMessageUserInputCountLimit is the max number of input messages before re-sending a new screen
	updateMessageUserInputCountLimit = 5

	updateScreenInterval = 200 * time.Millisecond

	scriptRunCommand  = "run"
	scriptKillCommand = "kill"
)

// Session represents a REPL session
//
// Slack:
//   Channels and DMs have an ID (fields: Channel, Timestamp), and may have a ThreadTimestamp field
//   to identify if they belong to a thread.
// Discord:
//   Channels, DMs and Threads are all channels with an ID
type Session struct {
	id             string
	config         *config.Config
	conn           Conn
	control        *Target
	terminal       *Target
	userInputChan  chan string
	userInputCount int32
	forceResend    chan bool
	g              *errgroup.Group
	ctx            context.Context
	cancelFn       context.CancelFunc
	active         bool
	warnTimer      *time.Timer
	closeTimer     *time.Timer
	script         string
	scriptID       string
	mode           config.Mode
	windowMode     config.WindowMode
	tmux           *util.Tmux
	cursorOn       bool
	cursorUpdated  time.Time
	mu             sync.RWMutex
}

func NewSession(config *config.Config, conn Conn, id string, control *Target, terminal *Target, script string, mode config.Mode, windowMode config.WindowMode, width, height int) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)
	return &Session{
		config:         config,
		conn:           conn,
		id:             id,
		control:        control,
		terminal:       terminal,
		script:         script,
		scriptID:       util.SanitizeID(id),
		mode:           mode,
		windowMode:     windowMode,
		tmux:           util.NewTmux(id, width, height),
		userInputChan:  make(chan string, 10), // buffered!
		userInputCount: 0,
		forceResend:    make(chan bool),
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
	if err := s.tmux.Start(s.script, scriptRunCommand, s.scriptID); err != nil {
		log.Printf("[session %s] Failed to start tmux: %s", s.id, err.Error())
		return err
	}
	if err := s.conn.Send(s.control, s.sessionStartedMessage(), Markdown); err != nil {
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
func (s *Session) HandleUserInput(message string) {
	if !s.Active() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Reset timeout timers
	s.warnTimer.Reset(s.config.IdleTimeout - time.Minute)
	s.closeTimer.Reset(s.config.IdleTimeout)

	// Convert message to raw text and forward to input channel
	s.userInputChan <- s.conn.Unescape(message)
}

func (s *Session) Active() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *Session) ForceClose() error {
	_ = s.conn.Send(s.control, forceCloseMessage, Markdown)
	s.cancelFn()
	if err := s.g.Wait(); err != nil && err != errExit {
		return err
	}
	return nil
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
	log.Printf("[session %s] User> %s", s.id, input)
	switch input {
	case helpCommand, helpShortCommand:
		atomic.AddInt32(&s.userInputCount, updateMessageUserInputCountLimit)
		return s.conn.Send(s.control, availableCommandsMessage, Markdown)
	case exitCommand, exitShortCommand:
		return errExit
	case screenCommand, screenShortCommand:
		s.forceResend <- true
		return nil
	default:
		atomic.AddInt32(&s.userInputCount, 1)
		if strings.HasPrefix(input, commentPrefix) {
			return nil // Ignore comments
		} else if strings.HasPrefix(input, rawPrefix) {
			return s.tmux.Paste(strings.TrimPrefix(input, rawPrefix))
		} else if strings.HasPrefix(input, unquotePrefix) {
			return s.tmux.Paste(unquote(strings.TrimPrefix(input, unquotePrefix)))
		} else if strings.HasPrefix(input, resizePrefix) {
			width, height, err := convertSize(strings.TrimPrefix(input, resizePrefix))
			if err != nil {
				return s.conn.Send(s.control, err.Error(), Markdown)
			}
			return s.tmux.Resize(width, height)
		} else if len(input) > 1 && input[0] == '!' {
			if controlChar, ok := sendKeysTable[input[1:]]; ok {
				return s.tmux.SendKeys(controlChar)
			}
		}
		return s.tmux.Paste(fmt.Sprintf("%s\n", input))
	}
}

func (s *Session) commandOutputLoop() error {
	log.Printf("[session %s] Started command output loop", s.id)
	defer log.Printf("[session %s] Exiting command output loop", s.id)
	var last, lastID string
	var err error
	for {
		select {
		case <-s.ctx.Done():
			if lastID != "" {
				_ = s.conn.Update(s.terminal, lastID, addExitedMessage(sanitizeWindow(last)), Code) // Show "(REPL exited.)" in terminal
			}
			return errExit
		case <-s.forceResend:
			last, lastID, err = s.maybeRefreshTerminal("", "") // Force re-send!
			if err != nil {
				return err
			}
		case <-time.After(updateScreenInterval):
			last, lastID, err = s.maybeRefreshTerminal(last, lastID)
			if err != nil {
				return err
			}
		}
	}
}

func (s *Session) maybeRefreshTerminal(last, lastID string) (string, string, error) {
	current, err := s.tmux.Capture()
	if err != nil {
		if lastID != "" {
			_ = s.conn.Update(s.terminal, lastID, addExitedMessage(sanitizeWindow(last)), Code) // Show "(REPL exited.)" in terminal
		}
		return "", "", errExit // The command may have ended, gracefully exit
	}
	current = s.maybeAddCursor(s.maybeTrimWindow(sanitizeWindow(current)))
	if current == last {
		return last, lastID, nil
	}
	if s.shouldUpdateTerminal(lastID) {
		if err := s.conn.Update(s.terminal, lastID, current, Code); err == nil {
			return current, lastID, nil
		}
	}
	if lastID, err = s.conn.SendWithID(s.terminal, current, Code); err != nil {
		return "", "", err
	}
	atomic.StoreInt32(&s.userInputCount, 0)
	return current, lastID, nil
}

func sanitizeWindow(window string) string {
	sanitized := consoleCodeRegex.ReplaceAllString(window, "")
	if strings.TrimSpace(sanitized) == "" {
		sanitized = fmt.Sprintf("(screen is empty) %s", sanitized)
	}
	return sanitized
}

func (s *Session) shouldUpdateTerminal(lastID string) bool {
	if s.mode == config.ModeSplit {
		return lastID != ""
	}
	return lastID != "" && atomic.LoadInt32(&s.userInputCount) < updateMessageUserInputCountLimit
}

func (s *Session) maybeAddCursor(window string) string {
	switch s.config.Cursor {
	case config.CursorOff:
		return window
	case config.CursorOn:
		show, x, y, err := s.tmux.Cursor()
		if !show || err != nil {
			return window
		}
		return addCursor(window, x, y)
	default:
		show, x, y, err := s.tmux.Cursor()
		if !show || err != nil {
			return window
		}
		if time.Since(s.cursorUpdated) > s.config.Cursor {
			s.cursorOn = !s.cursorOn
			s.cursorUpdated = time.Now()
		}
		if !s.cursorOn {
			return window
		}
		return addCursor(window, x, y)
	}
}

func (s *Session) shutdownHandler() error {
	log.Printf("[session %s] Starting shutdown handler", s.id)
	defer log.Printf("[session %s] Exiting shutdown handler", s.id)
	<-s.ctx.Done()
	if err := s.tmux.Stop(); err != nil {
		log.Printf("[session %s] Warning: unable to stop tmux: %s", s.id, err.Error())
	}
	cmd := exec.Command(s.script, scriptKillCommand, s.scriptID)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[session %s] Warning: unable to kill command: %s; command output: %s", s.id, err.Error(), string(output))
	}
	if err := s.conn.Send(s.control, sessionExitedMessage, Markdown); err != nil {
		log.Printf("[session %s] Warning: unable to send exited message: %s", s.id, err.Error())
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
			_ = s.conn.Send(s.control, timeoutWarningMessage, Markdown)
			log.Printf("[session %s] Session has been idle for a long time. Warning sent to user.", s.id)
		case <-s.closeTimer.C:
			log.Printf("[session %s] Idle timeout reached. Closing session.", s.id)
			return errExit
		}
	}
}

func (s *Session) sessionStartedMessage() string {
	if s.mode == config.ModeSplit {
		return fmt.Sprintf(sessionStartedMessage, " "+splitModeThreadMessage)
	}
	return fmt.Sprintf(sessionStartedMessage, "")
}

func (s *Session) maybeTrimWindow(window string) string {
	switch s.windowMode {
	case config.WindowModeFull:
		if s.config.Type() == config.TypeSlack {
			return window
		}
		return expandWindow(window)
	case config.WindowModeTrim:
		return strings.TrimRightFunc(window, unicode.IsSpace)
	default:
		return window
	}
}
