package util

import (
	"bytes"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	random = rand.New(rand.NewSource(time.Now().UnixNano()))
)

func KillChildProcesses(pid int) error {
	if pid == 0 {
		return errors.New("refusing to kill child processes of PID 0")
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

func KillTree(pid int) error {
	if pid == 0 {
		return errors.New("refusing to kill child processes of PID 0")
	}
	pids := []int{pid}
	for i := 0; i < 5; i++ {
		pid = pids[i]
		children, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", pid, pid))
		if err != nil {
			return err
		}
		childPIDs := strings.Split(string(bytes.TrimSpace(children)), " ")
		for _, childPID := range childPIDs {
			p, err := strconv.Atoi(childPID)
			if err != nil {
				return err
			}
			pids = append(pids, p)
		}
		fmt.Printf("%v\n", pids)
	}
	return nil
}

// RandomStringWithCharset returns a random string with a given length, using the defined charset
func RandomStringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[random.Intn(len(charset))]
	}
	return string(b)
}
