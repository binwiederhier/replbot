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
	"sort"
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
	resizeCommandHelpMessage          = "Use the `!resize` command to resize the terminal, like so: !resize medium.\n\nAllowed sizes are `tiny`, `small`, `medium` or `large`."
	messageLimitWarningMessage        = "Note that Discord has a message size limit of 2000 characters, so your messages may be truncated if they get to large."
	usersAddedToAllowList             = "👍 Okay, I added the user(s) to the allow list."
	usersAddedToDenyList              = "👍 Okay, I added the user(s) to the deny list."
	cannotAddOwnerToDenyList          = "🙁 I don't think adding the session owner to the deny list is a good idea. I must protest."
	recordingTooLargeMessage          = "🙁 I'm sorry, but you've produced too much output in this session. You may want to run a session with `norecord` to avoid this problem."
	shareStartCommandMessage          = "To start your terminal sharing session, please run the following command from your terminal:\n\n```bash -c \"$(ssh -T -p %s %s@%s $USER)\"```"
	allowCommandHelpMessage           = "To allow other users to interact with this session, use the `!allow` command like so: !allow %s\n\nYou may tag multiple users, or use the words " +
		"`everyone`/`all` to allow all users, or `nobody`/`only-me` to only yourself access."
	denyCommandHelpMessage = "To deny users from interacting with this session, use the `!deny` command like so: !deny %s\n\nYou may tag multiple users, or use the words " +
		"`everyone`/`all` to deny everyone (except yourself), like so: !deny all"
	noNewlineHelpMessage = "Use the `!n` command to send text without a newline character (`\\n`) at the end of the line, e.g. sending `!n ls`, will send `ls` and not `ls\\n`. " +
		"This is similar `echo -n` in a shell."
	escapeHelpMessage = "Use the `!e` command to interpret the escape sequences `\\n` (new line), `\\r` (carriage return), `\\t` (tab), `\\b` (backspace) and `\\x..` (hex " +
		"representation of any byte), e.g. `Hi\\bI` will show up as `HI`. This is is similar to `echo -e` in a shell."
	sendKeysHelpMessage = "Use any of the send-key commands (`!c`, `!esc`, ...) to send common keyboard shortcuts, e.g. `!d` to send Ctrl-D, or `!up` to send the up key.\n\n" +
		"You may also combine them in a sequence, like so: `!c-b d` (Ctrl-B + d), or `!up !up !down !down !left !right !left !right b a`."
	authModeChangeMessage = "👍 Okay, I updated the auth mode: "
	helpMessage           = "Alright, buckle up. Here's a list of all the things you can do in this REPL session.\n\n" +
		"Sending text:\n" +
		"  `TEXT` - Sends _TEXT\\n_\n" +
		"  `!n TEXT` - Sends _TEXT_ (no new line)\n" +
		"  `!e TEXT` - Sends _TEXT_ (interprets _\\n_, _\\r_, _\\t_, _\\b_ & _\\x.._)\n\n" +
		"Sending keys (can be combined):\n" +
		"  `!r` - Return key\n" +
		"  `!t`, `!tt` - Tab / double-tab\n" +
		"  `!up`, `!down`, `!left`, `!right` - Cursor\n" +
		"  `!pu`, `!pd` - Page up / page down\n" +
		"  `!c`, `!d`, `!c-..` - Ctrl-C/Ctrl-D/Ctrl-..\n" +
		"  `!esc`, `!space` - Escape/Space\n\n" +
		"Other commands:\n" +
		"  `!! ..` - Comment, ignored entirely\n" +
		"  `!allow ..`, `!deny ..` - Allow/deny users\n" +
		"  `!resize ..` - Resize window\n" +
		"  `!screen`, `!s` - Re-send terminal\n" +
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

	// sendKeysMapping is a translation table that translates input commands "!<command>" to something that can be
	// send via tmux's send-keys command, see https://man7.org/linux/man-pages/man1/tmux.1.html#KEY_BINDINGS
	sendKeysMapping = map[string]string{
		"!r":     "^M",
		"!c":     "^C",
		"!d":     "^D",
		"!t":     "\t",
		"!tt":    "\t\t",
		"!esc":   "escape", // ESC
		"!up":    "up",     // Cursor up
		"!down":  "down",   // Cursor down
		"!right": "right",  // Cursor right
		"!left":  "left",   // Cursor left
		"!space": "space",  // Space
		"!pu":    "ppage",  // Page up
		"!pd":    "npage",  // Page down
	}
	ctrlCommandRegex  = regexp.MustCompile(`^!c-([a-z])$`)
	alphanumericRegex = regexp.MustCompile(`^([a-zA-Z0-9])$`)
	errExit           = errors.New("exited REPL")

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
	conf           *sessionConfig
	conn           conn
	commands       []*sessionCommand
	userInputChan  chan string
	userInputCount int32
	forceResend    chan bool
	g              *errgroup.Group
	ctx            context.Context
	cancelFn       context.CancelFunc
	active         bool
	warnTimer      *time.Timer
	closeTimer     *time.Timer
	scriptID       string
	authUsers      map[string]bool // true = allow, false = deny, n/a = default
	tmux           *util.Tmux
	cursorOn       bool
	cursorUpdated  time.Time
	shareConn      io.Closer
	mu             sync.RWMutex
}

type sessionConfig struct {
	global      *config.Config
	id          string
	user        string
	control     *channelID
	terminal    *channelID
	script      string
	controlMode config.ControlMode
	windowMode  config.WindowMode
	authMode    config.AuthMode
	size        *config.Size
	share       *shareConfig
	record      bool
}

type shareConfig struct {
	user          string
	relayPort     int
	hostKeyPair   *util.SSHKeyPair
	clientKeyPair *util.SSHKeyPair
}

type sessionCommand struct {
	prefix  string
	execute func(input string) error
}

type sshSession struct {
	SessionID     string
	ServerHost    string
	ServerPort    string
	User          string
	HostKeyPair   *util.SSHKeyPair
	ClientKeyPair *util.SSHKeyPair
	RelayPort     int
}

func newSession(conf *sessionConfig, conn conn) *session {
	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)
	s := &session{
		conf:           conf,
		conn:           conn,
		scriptID:       fmt.Sprintf("replbot_%s", conf.id),
		authUsers:      make(map[string]bool),
		tmux:           util.NewTmux(conf.id, conf.size.Width, conf.size.Height),
		userInputChan:  make(chan string, 10), // buffered!
		userInputCount: 0,
		forceResend:    make(chan bool),
		g:              g,
		ctx:            ctx,
		cancelFn:       cancel,
		active:         true,
		warnTimer:      time.NewTimer(conf.global.IdleTimeout - time.Minute),
		closeTimer:     time.NewTimer(conf.global.IdleTimeout),
	}
	return initSessionCommands(s)
}

func initSessionCommands(s *session) *session {
	s.commands = []*sessionCommand{
		{"!h", s.handleHelpCommand},
		{"!help", s.handleHelpCommand},
		{"!n", s.handleNoNewlineCommand},
		{"!e", s.handleEscapeCommand},
		{"!allow", s.handleAllowCommand},
		{"!deny", s.handleDenyCommand},
		{"!!", s.handleCommentCommand},
		{"!screen", s.handleScreenCommand},
		{"!s", s.handleScreenCommand},
		{"!resize", s.handleResizeCommand},
		{"!c-", s.handleSendKeysCommand}, // more see below!
		{"!q", s.handleExitCommand},
		{"!exit", s.handleExitCommand},
	}
	for prefix := range sendKeysMapping {
		s.commands = append(s.commands, &sessionCommand{prefix, s.handleSendKeysCommand})
	}
	sort.Slice(s.commands, func(i, j int) bool {
		return len(s.commands[i].prefix) > len(s.commands[j].prefix)
	})
	return s
}

// Run executes a REPL session. This function only returns on error or when gracefully exiting the session.
func (s *session) Run() error {
	log.Printf("[session %s] Started REPL session", s.conf.id)
	defer log.Printf("[session %s] Closed REPL session", s.conf.id)
	env, err := s.getEnv()
	if err != nil {
		return err
	}
	if err := s.maybeSendStartShareMessage(); err != nil {
		return err
	}
	command := s.createCommand()
	if err := s.tmux.Start(env, command...); err != nil {
		log.Printf("[session %s] Failed to start tmux: %s", s.conf.id, err.Error())
		return err
	}
	if err := s.conn.Send(s.conf.control, s.sessionStartedMessage()); err != nil {
		return err
	}
	s.g.Go(s.userInputLoop)
	s.g.Go(s.commandOutputLoop)
	s.g.Go(s.activityMonitor)
	s.g.Go(s.shutdownHandler)
	if s.conf.record {
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
	s.warnTimer.Reset(s.conf.global.IdleTimeout - time.Minute)
	s.closeTimer.Reset(s.conf.global.IdleTimeout)

	// Forward to input channel
	s.userInputChan <- message
}

func (s *session) Active() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *session) ForceClose() error {
	_ = s.conn.Send(s.conf.control, forceCloseMessage)
	s.cancelFn()
	if err := s.g.Wait(); err != nil && err != errExit {
		return err
	}
	return nil
}

func (s *session) WriteShareClientScript(w io.Writer) error {
	if s.conf.share == nil {
		return errors.New("not a share session")
	}
	host, port, err := net.SplitHostPort(s.conf.global.ShareHost)
	if err != nil {
		return fmt.Errorf("invalid config: %s", err.Error())
	}
	shareConf := s.conf.share
	sessionInfo := &sshSession{
		SessionID:     s.conf.id,
		ServerHost:    host,
		ServerPort:    port,
		User:          shareConf.user,
		RelayPort:     shareConf.relayPort,
		HostKeyPair:   shareConf.hostKeyPair,
		ClientKeyPair: shareConf.clientKeyPair,
	}
	return shareClientScriptTemplate.Execute(w, sessionInfo)
}

func (s *session) WriteShareUserFile(user string) error {
	return os.WriteFile(s.sshUserFile(), []byte(user), 0600)
}

func (s *session) RegisterShareConn(conn io.Closer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shareConn != nil {
		_ = s.shareConn.Close()
	}
	s.shareConn = conn
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
	log.Printf("[session %s] User> %s", s.conf.id, input)
	atomic.AddInt32(&s.userInputCount, 1)
	for _, c := range s.commands {
		if strings.HasPrefix(input, c.prefix) {
			return c.execute(input)
		}
	}
	return s.handlePassthrough(input)
}

func (s *session) commandOutputLoop() error {
	var last, lastID string
	var err error
	for {
		select {
		case <-s.ctx.Done():
			if lastID != "" {
				_ = s.conn.Update(s.conf.terminal, lastID, util.FormatMarkdownCode(addExitedMessage(sanitizeWindow(last)))) // Show "(REPL exited.)" in terminal
			}
			return errExit
		case <-s.forceResend:
			last, lastID, err = s.maybeRefreshTerminal("", "") // Force re-send!
			if err != nil {
				return err
			}
		case <-time.After(s.conf.global.RefreshInterval):
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
			_ = s.conn.Update(s.conf.terminal, lastID, util.FormatMarkdownCode(addExitedMessage(sanitizeWindow(last)))) // Show "(REPL exited.)" in terminal
		}
		return "", "", errExit // The command may have ended, gracefully exit
	}
	current = s.maybeAddCursor(s.maybeTrimWindow(sanitizeWindow(current)))
	if current == last {
		return last, lastID, nil
	}
	if s.shouldUpdateTerminal(lastID) {
		if err := s.conn.Update(s.conf.terminal, lastID, util.FormatMarkdownCode(current)); err == nil {
			return current, lastID, nil
		}
	}
	if lastID, err = s.conn.SendWithID(s.conf.terminal, util.FormatMarkdownCode(current)); err != nil {
		return "", "", err
	}
	atomic.StoreInt32(&s.userInputCount, 0)
	return current, lastID, nil
}

func (s *session) shouldUpdateTerminal(lastID string) bool {
	if s.conf.controlMode == config.Split {
		return lastID != ""
	}
	return lastID != "" && atomic.LoadInt32(&s.userInputCount) < updateMessageUserInputCountLimit
}

func (s *session) maybeAddCursor(window string) string {
	switch s.conf.global.Cursor {
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
		if time.Since(s.cursorUpdated) > s.conf.global.Cursor {
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
		log.Printf("[session %s] Warning: unable to stop tmux: %s", s.conf.id, err.Error())
	}
	cmd := exec.Command(s.conf.script, scriptKillCommand, s.scriptID)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[session %s] Warning: unable to kill command: %s; command output: %s", s.conf.id, err.Error(), string(output))
	}
	if err := s.sendExitedMessage(); err != nil {
		log.Printf("[session %s] Warning: unable to exit message: %s", s.conf.id, err.Error())
	}
	if err := s.conn.Archive(s.conf.control); err != nil {
		log.Printf("[session %s] Warning: unable to archive thread: %s", s.conf.id, err.Error())
	}
	os.Remove(s.sshUserFile())
	os.Remove(s.sshClientKeyFile())
	os.Remove(s.tmux.RecordingFile())
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
			_ = s.conn.Send(s.conf.control, fmt.Sprintf(timeoutWarningMessage, s.conn.Mention(s.conf.user)))
			log.Printf("[session %s] Session has been idle for a long time. Warning sent to user.", s.conf.id)
		case <-s.closeTimer.C:
			log.Printf("[session %s] Idle timeout reached. Closing session.", s.conf.id)
			return errExit
		}
	}
}

func (s *session) sessionStartedMessage() string {
	message := fmt.Sprintf(sessionStartedMessage, s.conn.Mention(s.conf.user))
	if s.conf.controlMode == config.Split {
		message += "\n\n" + splitModeThreadMessage
	}
	switch s.conf.authMode {
	case config.OnlyMe:
		message += "\n\n" + onlyMeModeMessage
	case config.Everyone:
		message += "\n\n" + everyoneModeMessage
	}
	if s.shouldWarnMessageLength(s.conf.size) {
		message += "\n\n" + messageLimitWarningMessage
	}
	return message
}

func (s *session) maybeTrimWindow(window string) string {
	switch s.conf.windowMode {
	case config.Full:
		if s.conf.global.Platform() == config.Discord {
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
	var sshUserFile, sshKeyFile, relayPort string
	if s.conf.share != nil {
		shareConf := s.conf.share
		relayPort = strconv.Itoa(shareConf.relayPort)
		sshUserFile = s.sshUserFile()
		sshKeyFile = s.sshClientKeyFile()
		if err := os.WriteFile(sshKeyFile, []byte(shareConf.clientKeyPair.PrivateKey), 0600); err != nil {
			return nil, err
		}
	}
	return map[string]string{
		"REPLBOT_SSH_KEY_FILE":   sshKeyFile,
		"REPLBOT_SSH_USER_FILE":  sshUserFile,
		"REPLBOT_SSH_RELAY_PORT": relayPort,
	}, nil
}

func (s *session) parseUsers(usersList []string) ([]string, error) {
	users := make([]string, 0)
	for _, field := range usersList {
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
	if user == s.conf.user {
		return true // Always allow session owner!
	}
	if allow, ok := s.authUsers[user]; ok {
		return allow
	}
	return s.conf.authMode == config.Everyone
}

func (s *session) createCommand() []string {
	if s.conf.record {
		if err := util.Run("script", "-V"); err == nil {
			scriptFile, timingFile := s.replayFiles()
			command := fmt.Sprintf("%s %s %s", s.conf.script, scriptRunCommand, s.scriptID) // a little icky, but fine, since we trust all arguments
			return []string{"script", "--flush", "--quiet", "--timing=" + timingFile, "--command", command, scriptFile}
		}
		log.Printf("[session %s] Cannot record session, 'script' command is missing.", s.conf.id)
		s.conf.record = false
	}
	return []string{s.conf.script, scriptRunCommand, s.scriptID}
}

func (s *session) replayFiles() (replayFile string, timingFile string) {
	return filepath.Join(os.TempDir(), "replbot_"+s.conf.id+".replay-script"), filepath.Join(os.TempDir(), "replbot_"+s.conf.id+".replay-timing")
}

func (s *session) sshClientKeyFile() string {
	return filepath.Join(os.TempDir(), "replbot_"+s.conf.id+".ssh-client-key")
}

func (s *session) sshUserFile() string {
	return filepath.Join(os.TempDir(), "replbot_"+s.conf.id+".ssh-user")
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
				if err := s.conn.Send(s.conf.control, recordingTooLargeMessage); err != nil {
					return err
				}
				return errExit
			}
		}
	}
}

func (s *session) sendExitedMessage() error {
	if s.conf.record {
		if err := s.sendExitedMessageWithRecording(); err != nil {
			log.Printf("[session %s] Warning: unable to upload recording: %s", s.conf.id, err.Error())
			return s.sendExitedMessageWithoutRecording()
		}
		return nil
	}
	return s.sendExitedMessageWithoutRecording()
}

func (s *session) sendExitedMessageWithoutRecording() error {
	return s.conn.Send(s.conf.control, sessionExitedMessage)
}

func (s *session) sendExitedMessageWithRecording() error {
	filename := filepath.Join(os.TempDir(), "replbot_"+s.conf.id+".recording.zip")
	file, err := s.createRecordingArchive(filename)
	if err != nil {
		return err
	}
	defer func() {
		file.Close()
		os.Remove(filename)
	}()
	return s.conn.UploadFile(s.conf.control, sessionExitedWithRecordingMessage, recordingFileName, recordingFileType, file)
}

func (s *session) maybeSendStartShareMessage() error {
	if s.conf.share == nil {
		return nil
	}
	host, port, err := net.SplitHostPort(s.conf.global.ShareHost)
	if err != nil {
		return err
	}
	message := fmt.Sprintf(shareStartCommandMessage, port, s.conf.share.user, host)
	if err := s.conn.SendDM(s.conf.user, message); err != nil {
		return err
	}
	return nil
}

func (s *session) maybeSendMessageLengthWarning(size *config.Size) error {
	if s.shouldWarnMessageLength(size) {
		return s.conn.Send(s.conf.control, messageLimitWarningMessage)
	}
	return nil
}

func (s *session) shouldWarnMessageLength(size *config.Size) bool {
	return s.conf.global.Platform() == config.Discord && (size == config.Medium || size == config.Large)
}

func (s *session) handlePassthrough(input string) error {
	return s.tmux.Paste(fmt.Sprintf("%s\n", s.conn.Unescape(input)))
}

func (s *session) handleHelpCommand(_ string) error {
	atomic.AddInt32(&s.userInputCount, updateMessageUserInputCountLimit)
	return s.conn.Send(s.conf.control, helpMessage)
}

func (s *session) handleNoNewlineCommand(input string) error {
	input = s.conn.Unescape(strings.TrimSpace(strings.TrimPrefix(input, "!n")))
	if input == "" {
		return s.conn.Send(s.conf.control, noNewlineHelpMessage)
	}
	return s.tmux.Paste(input)
}

func (s *session) handleEscapeCommand(input string) error {
	input = unquote(s.conn.Unescape(strings.TrimSpace(strings.TrimPrefix(input, "!e"))))
	if input == "" {
		return s.conn.Send(s.conf.control, escapeHelpMessage)
	}
	return s.tmux.Paste(input)
}

func (s *session) handleAllowCommand(input string) error {
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(input, "!allow")))
	if util.InStringList(fields, "all") || util.InStringList(fields, "everyone") {
		return s.resetAuthMode(config.Everyone)
	}
	if util.InStringList(fields, "nobody") || util.InStringList(fields, "only-me") {
		return s.resetAuthMode(config.OnlyMe)
	}
	users, err := s.parseUsers(fields)
	if err != nil || len(users) == 0 {
		return s.conn.Send(s.conf.control, fmt.Sprintf(allowCommandHelpMessage, s.conn.MentionBot()))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range users {
		s.authUsers[user] = true
	}
	return s.conn.Send(s.conf.control, usersAddedToAllowList)
}

func (s *session) handleDenyCommand(input string) error {
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(input, "!deny")))
	if util.InStringList(fields, "all") || util.InStringList(fields, "everyone") {
		return s.resetAuthMode(config.OnlyMe)
	}
	users, err := s.parseUsers(fields)
	if err != nil || len(users) == 0 {
		return s.conn.Send(s.conf.control, fmt.Sprintf(denyCommandHelpMessage, s.conn.MentionBot()))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range users {
		if s.conf.user == user {
			return s.conn.Send(s.conf.control, cannotAddOwnerToDenyList)
		}
		s.authUsers[user] = false
	}
	return s.conn.Send(s.conf.control, usersAddedToDenyList)
}

func (s *session) resetAuthMode(authMode config.AuthMode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conf.authMode = authMode
	s.authUsers = make(map[string]bool)
	if authMode == config.Everyone {
		return s.conn.Send(s.conf.control, authModeChangeMessage+everyoneModeMessage)
	}
	return s.conn.Send(s.conf.control, authModeChangeMessage+onlyMeModeMessage)
}

func (s *session) handleSendKeysCommand(input string) error {
	fields := strings.Fields(strings.TrimSpace(input))
	keys := make([]string, 0)
	for _, field := range fields {
		if matches := ctrlCommandRegex.FindStringSubmatch(field); len(matches) > 0 {
			keys = append(keys, "^"+strings.ToUpper(matches[1]))
		} else if matches := alphanumericRegex.FindStringSubmatch(field); len(matches) > 0 {
			keys = append(keys, matches[1])
		} else if controlChar, ok := sendKeysMapping[field]; ok {
			keys = append(keys, controlChar)
		} else {
			return s.conn.Send(s.conf.control, sendKeysHelpMessage)
		}
	}
	return s.tmux.SendKeys(keys...)
}

func (s *session) handleCommentCommand(_ string) error {
	return nil // Ignore comments
}

func (s *session) handleScreenCommand(_ string) error {
	s.forceResend <- true
	return nil
}

func (s *session) handleResizeCommand(input string) error {
	size, err := config.ParseSize(strings.TrimSpace(strings.TrimPrefix(input, "!resize")))
	if err != nil {
		return s.conn.Send(s.conf.control, resizeCommandHelpMessage)
	}
	if err := s.maybeSendMessageLengthWarning(size); err != nil {
		return err
	}
	return s.tmux.Resize(size.Width, size.Height)
}

func (s *session) handleExitCommand(_ string) error {
	return errExit
}
