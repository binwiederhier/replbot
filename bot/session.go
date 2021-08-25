package bot

import (
	"archive/zip"
	"context"
	_ "embed" // go:embed requires this
	"errors"
	"fmt"
	"golang.org/x/sync/errgroup"
	"heckel.io/replbot/config"
	"heckel.io/replbot/util"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"
	"unicode"
)

const (
	sessionStartedMessage = "🚀 REPL session started, %s. Type `!help` to see a list of available commands, or `!exit` to forcefully " +
		"exit the REPL."
	splitModeThreadMessage            = "Use this thread to enter your commands. Your output will appear in the main channel."
	onlyMeModeMessage                 = "Only you as the session owner can send commands. Use the `!allow` command to let other users control the session."
	everyoneModeMessage               = "*Everyone in this channel* can send commands. Use the `!deny` command specifically revoke access from users."
	sessionExitedMessage              = "👋 REPL exited. See you later!"
	sessionExitedWithRecordingMessage = "👋 REPL exited. You can find a recording of the session in the file below. See you later!"
	timeoutWarningMessage             = "⏱️ Are you still there, %s? Your session will time out in one minute."
	forceCloseMessage                 = "🏃 REPLbot has to go. Urgent REPL-related business. Sorry about that!"
	malformatedTerminalSizeMessage    = "🙁 You entered an invalid size. Use `tiny`, `small`, `medium` or `large` instead."
	unknownUserMessage                = "⁉ I'm not sure what you mean. Please only list users."
	usersAddedToAllowList             = "👍 Okay, I added the user(s) to the allow list."
	usersAddedToDenyList              = "👍 Okay, I added the user(s) to the deny list."
	cannotAddOwnerToDenyList          = "🙁 I don't think adding the session owner to the deny list is a good idea. I must protest."
	recordingTooLargeMessage          = "🙁 I'm sorry, but you've produced too much output in this session. You may want to run a session with `norecord` to avoid this problem."
	helpCommand                       = "!help"
	helpShortCommand                  = "!h"
	exitCommand                       = "!exit"
	exitShortCommand                  = "!q"
	screenCommand                     = "!screen"
	screenShortCommand                = "!s"
	resizePrefix                      = "!resize "
	commentPrefix                     = "!! "
	noNewLinePrefix                   = "!n "
	unquotePrefix                     = "!e "
	allowPrefix                       = "!allow "
	denyPrefix                        = "!deny "
	availableCommandsMessage          = "Available commands:\n" +
		"  `!ret`, `!r` - Send empty return\n" +
		"  `!n ...` - Text without a new line\n" +
		"  `!e ...` - Text with escape sequences (`\\n`, `\\t`, ...)\n" +
		"  `!c`, `!d`, `!c-...` - Send Ctrl-C/Ctrl-D/Ctrl-...\n" +
		"  `!t`, `!tt` - Send TAB / double-TAB\n" +
		"  `!up`, `!down`, `!left`, `!right` - Send cursor keys\n" +
		"  `!pu`, `!pd` - Send page up / page down\n" +
		"  `!esc`, `!space` - Send ESC/Space\n" +
		"  `!allow ...`, `!deny ...` - Allow/deny users to interact\n" +
		"  `!! ...` - Comment\n" +
		"  `!resize ...` - Resize window\n" +
		"  `!screen`, `!s` - Re-send a new terminal window\n" +
		"  `!help`, `!h` - Show this help screen\n" +
		"  `!exit`, `!q` - Exit REPL"

	// updateMessageUserInputCountLimit is the max number of input messages before re-sending a new screen
	updateMessageUserInputCountLimit = 5

	recordingFileName    = "REPLbot session.zip"
	recordingFileType    = "application/zip"
	recordingFileSizeMax = 50 * 1024 * 1024

	scriptRunCommand  = "run"
	scriptKillCommand = "kill"
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
	ctrlCommandRegex = regexp.MustCompile(`^!c-([a-z])$`)
	errExit          = errors.New("exited REPL")

	//go:embed share_client.sh.gotmpl
	shareClientScriptSource   string
	shareClientScriptTemplate = template.Must(template.New("share_client").Parse(shareClientScriptSource))

	//go:embed recording.md
	recordingReadmeSource string
)

// session represents a REPL session
//
// Slack:
//   Channels and DMs have an ID (fields: Channel, Timestamp), and may have a ThreadTimestamp field
//   to identify if they belong to a thread.
// Discord:
//   Channels, DMs and Threads are all channels with an ID
type session struct {
	id             string
	user           string
	config         *config.Config
	conn           conn
	control        *chatID
	terminal       *chatID
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
	controlMode    config.ControlMode
	windowMode     config.WindowMode
	authMode       config.AuthMode
	authUsers      map[string]bool // true = allow, false = deny, n/a = default
	tmux           *util.Tmux
	cursorOn       bool
	cursorUpdated  time.Time
	relayPort      int
	record         bool
	shareConn      io.Closer
	mu             sync.RWMutex
}

type sessionConfig struct {
	ID          string
	User        string
	Control     *chatID
	Terminal    *chatID
	Script      string
	ControlMode config.ControlMode
	WindowMode  config.WindowMode
	AuthMode    config.AuthMode
	Size        *config.Size
	RelayPort   int
	Record      bool
}

type sshSession struct {
	SessionID  string
	ServerHost string
	ServerPort string
	RelayPort  int
}

func newSession(config *config.Config, conn conn, sconfig *sessionConfig) *session {
	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)
	return &session{
		config:         config,
		conn:           conn,
		id:             sconfig.ID,
		user:           sconfig.User,
		control:        sconfig.Control,
		terminal:       sconfig.Terminal,
		script:         sconfig.Script,
		scriptID:       fmt.Sprintf("replbot_%s", sconfig.ID),
		controlMode:    sconfig.ControlMode,
		windowMode:     sconfig.WindowMode,
		authMode:       sconfig.AuthMode,
		authUsers:      make(map[string]bool),
		relayPort:      sconfig.RelayPort,
		record:         sconfig.Record,
		tmux:           util.NewTmux(sconfig.ID, sconfig.Size.Width, sconfig.Size.Height),
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

// Run executes a REPL session. This function only returns on error or when gracefully exiting the session.
func (s *session) Run() error {
	log.Printf("[session %s] Started REPL session", s.id)
	defer log.Printf("[session %s] Closed REPL session", s.id)
	env, err := s.getEnv()
	if err != nil {
		return err
	}
	command := s.createCommand()
	if err := s.tmux.Start(env, command...); err != nil {
		log.Printf("[session %s] Failed to start tmux: %s", s.id, err.Error())
		return err
	}
	if err := s.conn.Send(s.control, s.sessionStartedMessage()); err != nil {
		return err
	}
	s.g.Go(s.userInputLoop)
	s.g.Go(s.commandOutputLoop)
	s.g.Go(s.activityMonitor)
	s.g.Go(s.shutdownHandler)
	if s.record {
		s.g.Go(s.monitorRecording)
	}
	if err := s.g.Wait(); err != nil && err != errExit {
		return err
	}
	return nil
}

// UserInput handles user input by forwarding to the underlying shell
func (s *session) UserInput(user, message string) {
	if !s.Active() || !s.allowUser(user) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Reset timeout timers
	s.warnTimer.Reset(s.config.IdleTimeout - time.Minute)
	s.closeTimer.Reset(s.config.IdleTimeout)

	// Forward to input channel
	s.userInputChan <- message
}

func (s *session) Active() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *session) ForceClose() error {
	_ = s.conn.Send(s.control, forceCloseMessage)
	s.cancelFn()
	if err := s.g.Wait(); err != nil && err != errExit {
		return err
	}
	return nil
}

func (s *session) WriteShareClientScript(w io.Writer) {
	if err := s.writeShareClientScript(w); err != nil {
		log.Printf("cannot write share script: %s", err.Error())
		io.WriteString(w, "echo 'Oh my, something went wrong ...'")
	}
}

func (s *session) RegisterShareConn(conn io.Closer) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shareConn != nil {
		return false
	}
	s.shareConn = conn
	return true
}

func (s *session) userInputLoop() error {
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

func (s *session) handleUserInput(input string) error {
	log.Printf("[session %s] User> %s", s.id, input)
	switch input {
	case helpCommand, helpShortCommand:
		atomic.AddInt32(&s.userInputCount, updateMessageUserInputCountLimit)
		return s.conn.Send(s.control, availableCommandsMessage)
	case exitCommand, exitShortCommand:
		return errExit
	case screenCommand, screenShortCommand:
		s.forceResend <- true
		return nil
	default:
		atomic.AddInt32(&s.userInputCount, 1)
		if strings.HasPrefix(input, commentPrefix) {
			return nil // Ignore comments
		} else if strings.HasPrefix(input, noNewLinePrefix) {
			return s.tmux.Paste(s.conn.Unescape(strings.TrimPrefix(input, noNewLinePrefix)))
		} else if strings.HasPrefix(input, unquotePrefix) {
			return s.tmux.Paste(unquote(s.conn.Unescape(strings.TrimPrefix(input, unquotePrefix))))
		} else if strings.HasPrefix(input, allowPrefix) {
			return s.handleAllow(strings.TrimPrefix(input, allowPrefix))
		} else if strings.HasPrefix(input, denyPrefix) {
			return s.handleDeny(strings.TrimPrefix(input, denyPrefix))
		} else if matches := ctrlCommandRegex.FindStringSubmatch(input); len(matches) > 0 {
			return s.tmux.SendKeys("^" + strings.ToUpper(matches[1]))
		} else if strings.HasPrefix(input, resizePrefix) {
			size, err := config.ConvertSize(strings.TrimPrefix(input, resizePrefix))
			if err != nil {
				return s.conn.Send(s.control, malformatedTerminalSizeMessage)
			}
			return s.tmux.Resize(size.Width, size.Height)
		} else if len(input) > 1 && input[0] == '!' {
			if controlChar, ok := sendKeysTable[input[1:]]; ok {
				return s.tmux.SendKeys(controlChar)
			}
		}
		return s.tmux.Paste(fmt.Sprintf("%s\n", s.conn.Unescape(input)))
	}
}

func (s *session) commandOutputLoop() error {
	var last, lastID string
	var err error
	for {
		select {
		case <-s.ctx.Done():
			if lastID != "" {
				_ = s.conn.Update(s.terminal, lastID, util.FormatMarkdownCode(addExitedMessage(sanitizeWindow(last)))) // Show "(REPL exited.)" in terminal
			}
			return errExit
		case <-s.forceResend:
			last, lastID, err = s.maybeRefreshTerminal("", "") // Force re-send!
			if err != nil {
				return err
			}
		case <-time.After(s.config.RefreshInterval):
			last, lastID, err = s.maybeRefreshTerminal(last, lastID)
			if err != nil {
				return err
			}
		}
	}
}

func (s *session) maybeRefreshTerminal(last, lastID string) (string, string, error) {
	current, err := s.tmux.Capture()
	if err != nil {
		if lastID != "" {
			_ = s.conn.Update(s.terminal, lastID, util.FormatMarkdownCode(addExitedMessage(sanitizeWindow(last)))) // Show "(REPL exited.)" in terminal
		}
		return "", "", errExit // The command may have ended, gracefully exit
	}
	current = s.maybeAddCursor(s.maybeTrimWindow(sanitizeWindow(current)))
	if current == last {
		return last, lastID, nil
	}
	if s.shouldUpdateTerminal(lastID) {
		if err := s.conn.Update(s.terminal, lastID, util.FormatMarkdownCode(current)); err == nil {
			return current, lastID, nil
		}
	}
	if lastID, err = s.conn.SendWithID(s.terminal, util.FormatMarkdownCode(current)); err != nil {
		return "", "", err
	}
	atomic.StoreInt32(&s.userInputCount, 0)
	return current, lastID, nil
}

func (s *session) shouldUpdateTerminal(lastID string) bool {
	if s.controlMode == config.Split {
		return lastID != ""
	}
	return lastID != "" && atomic.LoadInt32(&s.userInputCount) < updateMessageUserInputCountLimit
}

func (s *session) maybeAddCursor(window string) string {
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

func (s *session) shutdownHandler() error {
	<-s.ctx.Done()
	if err := s.tmux.Stop(); err != nil {
		log.Printf("[session %s] Warning: unable to stop tmux: %s", s.id, err.Error())
	}
	cmd := exec.Command(s.script, scriptKillCommand, s.scriptID)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[session %s] Warning: unable to kill command: %s; command output: %s", s.id, err.Error(), string(output))
	}
	if err := s.sendExitedMessage(); err != nil {
		log.Printf("[session %s] Warning: unable to exit message: %s", s.id, err.Error())
	}
	if err := s.conn.Archive(s.control); err != nil {
		log.Printf("[session %s] Warning: unable to archive thread: %s", s.id, err.Error())
	}
	s.mu.Lock()
	s.active = false
	if s.shareConn != nil {
		s.shareConn.Close()
	}
	s.mu.Unlock()
	return nil
}

func (s *session) activityMonitor() error {
	defer func() {
		s.warnTimer.Stop()
		s.closeTimer.Stop()
	}()
	for {
		select {
		case <-s.ctx.Done():
			return errExit
		case <-s.warnTimer.C:
			_ = s.conn.Send(s.control, fmt.Sprintf(timeoutWarningMessage, s.conn.Mention(s.user)))
			log.Printf("[session %s] Session has been idle for a long time. Warning sent to user.", s.id)
		case <-s.closeTimer.C:
			log.Printf("[session %s] Idle timeout reached. Closing session.", s.id)
			return errExit
		}
	}
}

func (s *session) sessionStartedMessage() string {
	message := fmt.Sprintf(sessionStartedMessage, s.conn.Mention(s.user))
	if s.controlMode == config.Split {
		message += "\n\n" + splitModeThreadMessage
	}
	switch s.authMode {
	case config.OnlyMe:
		message += "\n\n" + onlyMeModeMessage
	case config.Everyone:
		message += "\n\n" + everyoneModeMessage
	}
	return message
}

func (s *session) maybeTrimWindow(window string) string {
	switch s.windowMode {
	case config.Full:
		if s.config.Platform() == config.Discord {
			return expandWindow(window)
		}
		return window
	case config.Trim:
		return strings.TrimRightFunc(window, unicode.IsSpace)
	default:
		return window
	}
}

func (s *session) getEnv() (map[string]string, error) {
	var host, port, relayPort string
	var err error
	if s.config.ShareHost != "" {
		host, port, err = net.SplitHostPort(s.config.ShareHost)
		if err != nil {
			return nil, err
		}
	}
	if s.relayPort > 0 {
		relayPort = strconv.Itoa(s.relayPort)
	}
	return map[string]string{
		"REPLBOT_SESSION_ID": s.id,
		"REPLBOT_SSH_HOST":   host,
		"REPLBOT_SSH_PORT":   port,
		"REPLBOT_RELAY_PORT": relayPort,
	}, nil
}

func (s *session) writeShareClientScript(w io.Writer) error {
	if s.relayPort == 0 {
		return errors.New("not a share session")
	}
	host, port, err := net.SplitHostPort(s.config.ShareHost)
	if err != nil {
		return fmt.Errorf("invalid config: %s", err.Error())
	}
	sessionInfo := &sshSession{
		SessionID:  s.id,
		ServerHost: host,
		ServerPort: port,
		RelayPort:  s.relayPort,
	}
	return shareClientScriptTemplate.Execute(w, sessionInfo)
}

func (s *session) handleAllow(allow string) error {
	users, err := s.parseUsers(allow)
	if err != nil {
		return s.conn.Send(s.control, unknownUserMessage)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range users {
		s.authUsers[user] = true
	}
	return s.conn.Send(s.control, usersAddedToAllowList)
}

func (s *session) handleDeny(deny string) error {
	users, err := s.parseUsers(deny)
	if err != nil {
		return s.conn.Send(s.control, unknownUserMessage)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range users {
		if s.user == user {
			return s.conn.Send(s.control, cannotAddOwnerToDenyList)
		}
		s.authUsers[user] = false
	}
	return s.conn.Send(s.control, usersAddedToDenyList)
}

func (s *session) parseUsers(usersList string) ([]string, error) {
	users := make([]string, 0)
	for _, field := range strings.Fields(usersList) {
		user, err := s.conn.ParseMention(field)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

func (s *session) allowUser(user string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if user == s.user {
		return true // Always allow session owner!
	}
	if allow, ok := s.authUsers[user]; ok {
		return allow
	}
	return s.authMode == config.Everyone
}

func (s *session) createCommand() []string {
	if s.record {
		if err := util.Run("script", "-V"); err == nil {
			scriptFile, timingFile := s.replayFiles()
			command := fmt.Sprintf("%s %s %s", s.script, scriptRunCommand, s.scriptID) // a little icky, but fine, since we trust all arguments
			return []string{"script", "--flush", "--quiet", "--timing=" + timingFile, "--command", command, scriptFile}
		}
		log.Printf("[session %s] Cannot record session, 'script' command is missing.")
		s.record = false
	}
	return []string{s.script, scriptRunCommand, s.scriptID}
}

func (s *session) replayFiles() (replayFile string, timingFile string) {
	return filepath.Join(os.TempDir(), "replbot_"+s.id+".replay-script"), filepath.Join(os.TempDir(), "replbot_"+s.id+".replay-timing")
}

func (s *session) createRecordingArchive(filename string) (*os.File, error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	zw := zip.NewWriter(file)
	if err := zipAppendEntry(zw, "REPLbot session/README.md", recordingReadmeSource); err != nil {
		return nil, err
	}
	recordingFile := s.tmux.RecordingFile()
	defer os.Remove(recordingFile)
	if err := zipAppendFile(zw, "REPLbot session/terminal.txt", recordingFile); err != nil {
		return nil, err
	}
	replayFile, timingFile := s.replayFiles()
	defer func() {
		os.Remove(replayFile)
		os.Remove(timingFile)
	}()
	if err := zipAppendFile(zw, "REPLbot session/replay.script", replayFile); err != nil {
		return nil, err
	}
	if err := zipAppendFile(zw, "REPLbot session/replay.timing", timingFile); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return nil, err
	}
	return file, nil
}

func (s *session) monitorRecording() error {
	for {
		select {
		case <-s.ctx.Done():
			return nil
		case <-time.After(time.Second):
			scriptFile, _ := s.replayFiles()
			stat, err := os.Stat(scriptFile)
			if err != nil {
				continue
			} else if stat.Size() > recordingFileSizeMax {
				if err := s.conn.Send(s.control, recordingTooLargeMessage); err != nil {
					return err
				}
				return errExit
			}
		}
	}
}

func (s *session) sendExitedMessage() error {
	if s.record {
		if err := s.sendExitedMessageWithRecording(); err != nil {
			log.Printf("[session %s] Warning: unable to upload recording: %s", s.id, err.Error())
			return s.sendExitedMessageWithoutRecording()
		}
		return nil
	}
	return s.sendExitedMessageWithoutRecording()
}

func (s *session) sendExitedMessageWithoutRecording() error {
	return s.conn.Send(s.control, sessionExitedMessage)
}

func (s *session) sendExitedMessageWithRecording() error {
	filename := filepath.Join(os.TempDir(), "replbot_"+s.id+".recording.zip")
	file, err := s.createRecordingArchive(filename)
	if err != nil {
		return err
	}
	defer func() {
		file.Close()
		os.Remove(filename)
	}()
	return s.conn.UploadFile(s.control, sessionExitedWithRecordingMessage, recordingFileName, recordingFileType, file)
}

func zipAppendFile(zw *zip.Writer, name string, filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func zipAppendEntry(zw *zip.Writer, name string, content string) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(content)); err != nil {
		return err
	}
	return nil
}
