package util

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

// Tmux represents a tmux(1) process
type Tmux struct {
	ID            string
	Width, Height int
}

// NewTmux creates a new Tmux instance, but does not start the tmux
func NewTmux(id string, width, height int) *Tmux {
	return &Tmux{
		ID:     SanitizeID(id),
		Width:  width,
		Height: height,
	}
}

// Start starts the tmux using the given command and arguments
func (s *Tmux) Start(args ...string) error {
	args = append([]string{"new-session", "-d", "-s", s.ID, "-x", fmt.Sprintf("%d", s.Width), "-y", fmt.Sprintf("%d", s.Height)}, args...)
	cmd := exec.Command("tmux", args...)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// Active checks if the tmux is still active
func (s *Tmux) Active() bool {
	cmd := exec.Command("tmux", "has-session", "-t", s.ID)
	return cmd.Run() == nil
}

// Paste pastes the input into the tmux, as if the user entered it
func (s *Tmux) Paste(input string) error {
	if err := os.WriteFile(s.bufferFile(), []byte(input), 0600); err != nil {
		return err
	}
	cmd := exec.Command("tmux", "load-buffer", "-b", s.ID, s.bufferFile())
	if err := cmd.Run(); err != nil {
		return err
	}
	cmd = exec.Command("tmux", "paste-buffer", "-b", s.ID, "-t", s.ID, "-d")
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// SendKeys invokes the tmux send-keys command, which is useful for sending control sequences
func (s *Tmux) SendKeys(keys string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", s.ID, keys)
	return cmd.Run()
}

// CapturePane returns a string representation of the current terminal
func (s *Tmux) CapturePane() (string, error) {
	var buf bytes.Buffer
	cmd := exec.Command("tmux", "capture-pane", "-t", s.ID, "-p")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Stop kills the tmux and its command using the 'quit' command
func (s *Tmux) Stop() error {
	defer os.Remove(s.bufferFile())
	if s.Active() {
		cmd := exec.Command("tmux", "kill-session", "-t", s.ID)
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Tmux) bufferFile() string {
	return fmt.Sprintf("/dev/shm/%s.buffer", s.ID)
}
