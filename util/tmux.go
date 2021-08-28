package util

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Tmux represents a tmux(1) process with one window and three panes, to allow us to resize the terminal of the
// main pane (.2). The main pane is .2, so that if it exits there is no other pane to take its place.
//
// Example: Assuming "htop" is our target process, we're essentially doing this:
//   tmux new-session -s abc -d -x 200 -y 60
//   tmux split-window -t abc.0 -v
//   tmux split-window -t abc.1 -h htop
//   tmux resize-pane -t abc.2 -x 80 -y 24
//   tmux select-pane -t abc.2
//   sleep 1
//   tmux capture-pane -t abc.2 -p
//
type Tmux struct {
	id            string
	width, height int
}

// Must be more than config.MaxSize to give tmux a little room for the other two panes
const (
	terminalWidth       = "200"
	terminalHeight      = "80"
	checkMainPaneScript = "sh -c \"while true; do sleep 10; if ! tmux has-session -t %s.2; then exit; fi; done\""
)

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
	pane0 := fmt.Sprintf("%s.0", s.id)
	pane1 := fmt.Sprintf("%s.1", s.id)
	pane2 := fmt.Sprintf("%s.2", s.id)
	c := make([][]string, 0)
	c = append(c, []string{"tmux", "new-session", "-s", s.id, "-d", "-x", terminalWidth, "-y", terminalHeight, fmt.Sprintf(checkMainPaneScript, s.id)})
	c = append(c, []string{"tmux", "split-window", "-v", "-t", pane0, fmt.Sprintf(checkMainPaneScript, s.id)})
	for k, v := range env {
		c = append(c, []string{"tmux", "set-environment", "-t", s.id, k, v})
	}
	c = append(c, []string{"tmux", "set-option", "-t", s.id, "history-limit", "500000"}) // before split-window!
	c = append(c, append([]string{"tmux", "split-window", "-h", "-t", pane1}, command...))
	c = append(c, []string{"tmux", "resize-pane", "-t", pane2, "-x", strconv.Itoa(s.width), "-y", strconv.Itoa(s.height)})
	c = append(c, []string{"tmux", "select-pane", "-t", pane2})
	c = append(c, []string{"tmux", "set-hook", "-t", pane2, "pane-died", fmt.Sprintf("capture-pane -S- -E-; save-buffer \"%s\"; kill-pane", s.recordingFile())})
	c = append(c, []string{"tmux", "set-option", "-t", pane2, "remain-on-exit"})
	return RunAll(c...)
}

// Active checks if the tmux is still active
func (s *Tmux) Active() bool {
	return Run("tmux", "has-session", "-t", s.id) == nil
}

// Paste pastes the input into the tmux, as if the user entered it
func (s *Tmux) Paste(input string) error {
	defer os.Remove(s.bufferFile())
	if err := os.WriteFile(s.bufferFile(), []byte(input), 0600); err != nil {
		return err
	}
	return RunAll(
		[]string{"tmux", "load-buffer", "-b", s.id, s.bufferFile()},
		[]string{"tmux", "paste-buffer", "-b", s.id, "-t", fmt.Sprintf("%s.2", s.id), "-d"},
	)
}

// SendKeys invokes the tmux send-keys command, which is useful for sending control sequences
func (s *Tmux) SendKeys(keys ...string) error {
	return Run(append([]string{"tmux", "send-keys", "-t", fmt.Sprintf("%s.2", s.id)}, keys...)...)
}

// Resize resizes the active pane (.2) to the given size up to the max size
func (s *Tmux) Resize(width, height int) error {
	return Run("tmux", "resize-pane", "-t", fmt.Sprintf("%s.2", s.id), "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
}

// Capture returns a string representation of the current terminal
func (s *Tmux) Capture() (string, error) {
	var buf bytes.Buffer
	cmd := exec.Command("tmux", "capture-pane", "-t", fmt.Sprintf("%s.2", s.id), "-p")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RecordingFile returns the file name of the recording file. This method can only be called
// after the session has exited. Before that, the file will not exist.
func (s *Tmux) RecordingFile() string {
	return s.recordingFile()
}

// Cursor returns the X and Y position of the cursor
func (s *Tmux) Cursor() (show bool, x int, y int, err error) {
	var buf bytes.Buffer
	cmd := exec.Command("tmux", "display-message", "-t", fmt.Sprintf("%s.2", s.id), "-p", "-F", "#{cursor_flag},#{cursor_x},#{cursor_y}")
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
		if !FileExists(s.recordingFile()) {
			if err := Run("tmux", "capture-pane", "-S-", "-E-", ";", "save-buffer", s.recordingFile()); err != nil {
				return err
			}
		}
		if err := Run("tmux", "kill-session", "-t", s.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Tmux) bufferFile() string {
	return fmt.Sprintf("/dev/shm/%s.buffer", s.id)
}

func (s *Tmux) recordingFile() string {
	return fmt.Sprintf("/tmp/%s.recording", s.id)
}
