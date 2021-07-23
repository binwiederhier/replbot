package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/creack/pty"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "1" {
		test1()
		return
	} else if len(os.Args) > 1 && os.Args[1] == "2" {
		test2()
		return
	} else if len(os.Args) > 1 && os.Args[1] == "launcher" {
		launcher()
		return
	}

	config := &Config{
		Token: os.Getenv("SLACK_BOT_TOKEN"),
		ScriptDir: "repls.d",
	}
	bot, err := NewBot(config)
	if err != nil {
		panic(err)
	}
	if err := bot.Start(); err != nil {
		panic(err)
	}
}


func test1() {
	c := exec.Command("sh", "-c", "docker run -it node")
	ptmx, err := pty.Start(c)
	if err != nil {
		panic(err)
	}
	go func() {
		defer log.Printf("exiting read loop routine")
		for {
			buf := make([]byte, 4096)
			log.Printf("before read")
			n, err := ptmx.Read(buf)
			log.Printf("after read")
			if err != nil {
				log.Printf("read loop err: %s", err.Error())
				return
			}
			log.Printf("read: %s", string(buf[:n]))
		}

	}()

	//time.Sleep(2 * time.Second)
	//log.Printf("sending ctrl-d")
	//ptmx.Write([]byte{0x04})

	time.Sleep(10 * time.Second)
	log.Printf("killing")
	syscall.Close(int(ptmx.Fd()))
	c.Process.Kill()
	ptmx.Close()

	time.Sleep(30 * time.Second)
	log.Printf("exiting main prog")
}

func test2() {
	c := exec.Command("sh", "-c", "docker run -it node")
	ptmx, err := pty.Start(c)
	if err != nil {
		panic(err)
	}
	fd := int(ptmx.Fd())
	//log.Printf("ptmx fd: %#v", ptmx.Fd())
	errg, ctx := errgroup.WithContext(context.Background())

	inputChan := make(chan string)

	errg.Go(func() error {
		defer log.Printf("Exiting shutdown routine")
		<-ctx.Done()
		println("killing kids")
		killChildren(c.Process.Pid)
		// c.Process.Kill()
		//ptmx.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		//time.Sleep(2*time.Second)
		// ptmx.Write([]byte{0x03})
		time.Sleep(2*time.Second)
		log.Printf("killing %d", fd)

		syscall.Close(fd) // Force kill the Read()
		ptmx.Close()

		return nil
	})
	errg.Go(func() error {
		defer log.Printf("Exiting read loop")
		for {
			buf := make([]byte, 4096) // FIXME alloc in a loop!
			log.Printf("before read")
			n, err := ptmx.Read(buf)
			log.Printf("read something: %d %v", n, err)
			select {
			case <-ctx.Done():
				return nil
			default:
				if err != nil {
					return err
				}
				if strings.TrimSpace(string(buf[:n])) == "exited" {
					return nil
				}
				println(string(buf[:n]))
			}
		}
	})

	errg.Go(func() error {
		defer log.Printf("Exiting inputChan loop")
		for {
			select {
			case <-ctx.Done():
				return nil
			case <- inputChan:
				return errExit
			}
		}
	})

	//time.Sleep(5 * time.Second)
	//println("killing FD")
	//syscall.Close(int(ptmx.Fd())) // Force kill the Read()

	time.Sleep(5 * time.Second)
	println("sending exit")
	inputChan <- "!exit"

	time.Sleep(10 * time.Second)
	println("exited main prog")
}

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

func launcher() {

}
