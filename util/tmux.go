package util

import (
	"bytes"
	_ "embed" // required by go:embed
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

const (
	requiredVersion = 2.6 // see issue #
)

var (
	tmuxVersionRegex = regexp.MustCompile(`tmux (\d+\.\d+)`)

	//go:embed tmux.sh.gotmpl
	scriptSource   string
	scriptTemplate = template.Must(template.New("tmux_script").Parse(scriptSource))
)

// CheckTmuxVersion checks the version of tmux and returns an error if it's not supported
func CheckTmuxVersion() error {
	cmd := exec.Command("tmux", "-V")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	matches := tmuxVersionRegex.FindStringSubmatch(string(output))
	if len(matches) <= 1 {
		return errors.New("unexpected tmux version output")
	}
	version, err := strconv.ParseFloat(matches[1], 32)
	if err != nil {
		return err
	}
	if version < requiredVersion-0.01 { // floats are fun
		return fmt.Errorf("tmux version too low: tmux %.1f required, but found tmux %.1f", requiredVersion, version)
	}
	return nil
}

// Tmux represents a very special tmux(1) setup, specifially used for REPLbot. It consists of
// two tmux sessions:
// - session "replbot_$id_frame": session with one window and three panes to allow us to resize the terminal of the
//   main pane (.2). The main pane is .2, so that if it exits there is no other pane to take its place, which is easily
//   detectable by the other panes. The main pane (.2) connects to the main session (see below).
// - session "replbot_$id_main": main session running the actual shell/REPL.
type Tmux struct {
	id            string
	width, height int
}

type tmuxScriptParams struct {
	MainID, FrameID  string
	Width, Height    int
	Env              map[string]string
	Command          string
	ConfigFile       string
	CaptureFile      string
	LaunchScriptFile string
}

// NewTmux creates a new Tmux instance, but does not start the tmux
func NewTmux(id string, width, height int) *Tmux {
	return &Tmux{
		id:     fmt.Sprintf("replbot_%s", id),
		width:  width,
		height: height,
	}
}

// Start starts the tmux using the given command and arguments
func (s *Tmux) Start(env map[string]string, command ...string) error {
	defer os.Remove(s.scriptFile())
	defer os.Remove(s.launchScriptFile())
	script, err := os.OpenFile(s.scriptFile(), os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	defer script.Close()
	params := &tmuxScriptParams{
		MainID:           s.mainID(),
		FrameID:          s.frameID(),
		Width:            s.width,
		Height:           s.height,
		Env:              env,
		Command:          QuoteCommand(command),
		ConfigFile:       s.configFile(),
		CaptureFile:      s.captureFile(),
		LaunchScriptFile: s.launchScriptFile(),
	}
	if err := scriptTemplate.Execute(script, params); err != nil {
		return err
	}
	if err := script.Close(); err != nil {
		return err
	}
	return Run(s.scriptFile())
}

// Active checks if the tmux is still active
func (s *Tmux) Active() bool {
	return Run("tmux", "has-session", "-t", s.mainID()) == nil
}

// Paste pastes the input into the tmux, as if the user entered it
func (s *Tmux) Paste(input string) error {
	defer os.Remove(s.bufferFile())
	if err := os.WriteFile(s.bufferFile(), []byte(input), 0600); err != nil {
		return err
	}
	return RunAll(
		[]string{"tmux", "load-buffer", "-b", s.id, s.bufferFile()},
		[]string{"tmux", "paste-buffer", "-b", s.id, "-t", s.mainID(), "-d"},
	)
}

// SendKeys invokes the tmux send-keys command, which is useful for sending control sequences
func (s *Tmux) SendKeys(keys ...string) error {
	return Run(append([]string{"tmux", "send-keys", "-t", s.mainID()}, keys...)...)
}

// Resize resizes the active pane (.2) to the given size up to the max size
func (s *Tmux) Resize(width, height int) error {
	return Run("tmux", "resize-pane", "-t", s.frameMainPaneID(), "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
}

// Capture returns a string representation of the current terminal
func (s *Tmux) Capture() (string, error) {
	var buf bytes.Buffer
	cmd := exec.Command("tmux", "capture-pane", "-t", s.mainID(), "-p")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RecordingFile returns the file name of the recording file. This method can only be called
// after the session has exited. Before that, the file will not exist.
func (s *Tmux) RecordingFile() string {
	return s.captureFile()
}

// Cursor returns the X and Y position of the cursor
func (s *Tmux) Cursor() (show bool, x int, y int, err error) {
	var buf bytes.Buffer
	cmd := exec.Command("tmux", "display-message", "-t", s.mainID(), "-p", "-F", "#{cursor_flag},#{cursor_x},#{cursor_y}")
	cmd.Stdout = &buf
	if err = cmd.Run(); err != nil {
		return
	}
	cursor := strings.Split(strings.TrimSpace(buf.String()), ",")
	if len(cursor) != 3 {
		return false, 0, 0, errors.New("unexpected response from display-message")
	}
	show = cursor[0] == "1"
	if x, err = strconv.Atoi(cursor[1]); err != nil {
		return
	}
	if y, err = strconv.Atoi(cursor[2]); err != nil {
		return
	}
	return
}

// Stop kills the tmux and its command using the 'quit' command
func (s *Tmux) Stop() error {
	if s.Active() {
		if !FileExists(s.captureFile()) {
			_ = Run("tmux", "capture-pane", "-t", s.mainID(), "-S-", "-E-", ";", "save-buffer", s.captureFile())
		}
		_ = Run("tmux", "kill-session", "-t", s.mainID())
		_ = Run("tmux", "kill-session", "-t", s.frameID())
	}
	return nil
}

// MainID returns the session identifier for the main tmux session
func (s *Tmux) MainID() string {
	return s.mainID()
}

func (s *Tmux) bufferFile() string {
	return fmt.Sprintf("/dev/shm/%s.tmux.buffer", s.id)
}

func (s *Tmux) scriptFile() string {
	return fmt.Sprintf("/tmp/%s.tmux.script", s.id)
}

func (s *Tmux) launchScriptFile() string {
	return fmt.Sprintf("/tmp/%s.tmux.lauch-script", s.id)
}

func (s *Tmux) configFile() string {
	return fmt.Sprintf("/tmp/%s.tmux.conf", s.id)
}

func (s *Tmux) captureFile() string {
	return fmt.Sprintf("/tmp/%s.tmux.capture", s.id)
}

func (s *Tmux) frameID() string {
	return fmt.Sprintf("%s_frame", s.id)
}

func (s *Tmux) frameMainPaneID() string {
	return fmt.Sprintf("%s.2", s.frameID())
}

func (s *Tmux) mainID() string {
	return fmt.Sprintf("%s_main", s.id)
}
