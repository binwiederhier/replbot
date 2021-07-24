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

func killChildren(pid int) (killed int, err error) {
	fmt.Printf("pid: %d", pid)
	if pid == 0 {
		return 0, errors.New("you seem to be insane, refusing to kill pid 0's kids")
	}
	children, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", pid, pid))
	if err != nil {
		println(err)
		return 0, err
	}
	println(string(children))
	pids := strings.Split(string(bytes.TrimSpace(children)), " ")
	for _, pid := range pids {
		println("pid: " + pid + "|")
		p, err := strconv.Atoi(pid)
		if err != nil {
			continue
		}
		log.Printf("Killing PID %d (SIGTERM)", p)
		if err := syscall.Kill(p, unix.SIGTERM); err != nil {
			log.Printf("Killing PID %d (SIGKILL)", p)
			if err := syscall.Kill(p, unix.SIGKILL); err != nil {
				continue
			}
		}
		log.Printf("Killed PID %d", p)
		killed++
	}
	return killed, nil
}
