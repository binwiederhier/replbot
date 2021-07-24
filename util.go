package main

import (
	"bytes"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func killChildren(pid int) error {
	if pid == 0 {
		return errors.New("you seem to be insane, refusing to kill pid 0's kids")
	}
	children, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", pid, pid))
	if err != nil {
		return err
	}
	pids := strings.Split(string(bytes.TrimSpace(children)), " ")
	for _, pid := range pids {
		p, err := strconv.Atoi(pid)
		if err != nil {
			continue
		}
		if err := syscall.Kill(p, unix.SIGTERM); err != nil {
			if err := syscall.Kill(p, unix.SIGKILL); err != nil {
				log.Printf("Failed to kill PID %d", p)
				continue
			}
		}
	}
	return nil
}
