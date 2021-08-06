package util

import (
	"fmt"
	"os"
	"os/exec"
)

// Screen represents a screen(1) process
type Screen struct {
	id  string
	log *os.File
}

// NewScreen creates a new Screen instance, but does not start the screen
func NewScreen() *Screen {
	return &Screen{
		id: fmt.Sprintf("replbot.%s", RandomID(10)),
	}
}

// Start starts the screen using the given command and arguments
func (s *Screen) Start(args ...string) error {
	var err error
	if err = os.WriteFile(s.logFile(), []byte{}, 0600); err != nil {
		return err
	}
	s.log, err = os.Open(s.logFile())
	if err != nil {
		return err
	}
	rcBytes := fmt.Sprintf("deflog on\nlogfile %s\nlogfile flush 0\nlog on\n", s.logFile())
	if err := os.WriteFile(s.rcFile(), []byte(rcBytes), 0600); err != nil {
		return err
	}
	args = append([]string{"-dmS", s.id, "-c", s.rcFile()}, args...)
	cmd := exec.Command("screen", args...)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// Active checks if the screen is still active
func (s *Screen) Active() bool {
	cmd := exec.Command("screen", "-S", s.id, "-Q", "select", ".")
	err := cmd.Run()
	return err == nil
}

// Paste pastes the input into the screen, as if the user entered it
func (s *Screen) Paste(input string) error {
	if err := os.WriteFile(s.regFile(), []byte(input), 0600); err != nil {
		return err
	}
	readRegCmd := exec.Command("screen", "-S", s.id, "-X", "readreg", "x", s.regFile())
	if err := readRegCmd.Run(); err != nil {
		return err
	}
	pasteCmd := exec.Command("screen", "-S", s.id, "-X", "paste", "x")
	if err := pasteCmd.Run(); err != nil {
		return err
	}
	return nil
}

// Stuff invokes the screen 'stuff' command, which is useful for sending control sequences
func (s *Screen) Stuff(stuff string) error {
	cmd := exec.Command("screen", "-S", s.id, "-X", "stuff", stuff)
	return cmd.Run()
}

// Hardcopy returns a string representation of the current terminal
func (s *Screen) Hardcopy() (string, error) {
	cmd := exec.Command("screen", "-S", s.id, "-X", "hardcopy", s.hardcopyFile())
	if err := cmd.Run(); err != nil {
		return "", err
	}
	b, err := os.ReadFile(s.hardcopyFile())
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Stop kills the screen and its command using the 'quit' command
func (s *Screen) Stop() error {
	defer func() {
		os.Remove(s.rcFile())
		os.Remove(s.logFile())
		os.Remove(s.regFile())
		os.Remove(s.hardcopyFile())
	}()
	if s.Active() {
		cmd := exec.Command("screen", "-S", s.id, "-X", "quit")
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return s.log.Close()
}

func (s *Screen) logFile() string {
	return fmt.Sprintf("/tmp/%s.screenlog", s.id)
}

func (s *Screen) rcFile() string {
	return fmt.Sprintf("/tmp/%s.screenrc", s.id)
}

func (s *Screen) regFile() string {
	return fmt.Sprintf("/dev/shm/%s.reg", s.id)
}

func (s *Screen) hardcopyFile() string {
	return fmt.Sprintf("/dev/shm/%s.hardcopy", s.id)
}
