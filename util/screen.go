package util

import (
	"fmt"
	"os"
	"os/exec"
)

type Screen struct {
	id  string
	log *os.File
}

func NewScreen() (*Screen, error) {
	return &Screen{
		id: fmt.Sprintf("replbot.%s", RandomID(10)),
	}, nil
}

func (s *Screen) Start(args ...string) error {
	var err error
	if err = os.WriteFile(s.logFile(), []byte{}, 0600); err != nil {
		return err
	}
	s.log, err = os.Open(s.logFile())
	if err != nil {
		return err
	}
	rcBytes := fmt.Sprintf("deflog on\nlogfile %s\nlogfile flush 0\nlog on", s.logFile())
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

func (s *Screen) Read(p []byte) (n int, err error) {
	return s.log.Read(p)
}

func (s *Screen) Write(p []byte) (n int, err error) {
	if err := os.WriteFile(s.regFile(), p, 0600); err != nil {
		return 0, err
	}
	readRegCmd := exec.Command("screen", "-S", s.id, "-X", "readreg", "x", s.regFile())
	if err := readRegCmd.Run(); err != nil {
		return 0, err
	}
	pasteCmd := exec.Command("screen", "-S", s.id, "-X", "paste", "x")
	if err := pasteCmd.Run(); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *Screen) Active() bool {
	cmd := exec.Command("screen", "-S", s.id, "-Q", "select", ".")
	err := cmd.Run()
	return err == nil
}

func (s *Screen) Stuff(stuff string) error {
	cmd := exec.Command("screen", "-S", s.id, "-X", "stuff", stuff)
	return cmd.Run()
}

func (s *Screen) Stop() error {
	defer func() {
		os.Remove(s.rcFile())
		os.Remove(s.logFile())
		os.Remove(s.regFile())
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
