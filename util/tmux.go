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
	commands := make([][]string, 0)
	commands = append(commands, []string{"tmux", "new-session", "-s", s.id, "-d", "-x", terminalWidth, "-y", terminalHeight, fmt.Sprintf(checkMainPaneScript, s.id)})
	commands = append(commands, []string{"tmux", "split-window", "-v", "-t", fmt.Sprintf("%s.0", s.id), fmt.Sprintf(checkMainPaneScript, s.id)})
	for k, v := range env {
		commands = append(commands, []string{"tmux", "set-environment", "-t", s.id, k, v})
	}
	commands = append(commands, append([]string{"tmux", "split-window", "-h", "-t", fmt.Sprintf("%s.1", s.id)}, command...))
	commands = append(commands, []string{"tmux", "resize-pane", "-t", fmt.Sprintf("%s.2", s.id), "-x", strconv.Itoa(s.width), "-y", strconv.Itoa(s.height)})
	commands = append(commands, []string{"tmux", "select-pane", "-t", fmt.Sprintf("%s.2", s.id)})
	return RunAll(commands...)
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
func (s *Tmux) SendKeys(keys string) error {
	return Run("tmux", "send-keys", "-t", fmt.Sprintf("%s.2", s.id), keys)
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
		if err := Run("tmux", "kill-session", "-t", s.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Tmux) bufferFile() string {
	return fmt.Sprintf("/dev/shm/%s.buffer", s.id)
}

func TmuxInstalled() error {
	return Run("tmux", "-V")
}
