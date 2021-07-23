package main

import (
	"context"
	"fmt"
	"github.com/creack/pty"
	"golang.org/x/sync/errgroup"
	"io"
	"log"
	"os"
	"os/exec"
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
	pty.Open()
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

	log.Printf("ptmx fd: %#v", ptmx.Fd())
	errg, ctx := errgroup.WithContext(context.Background())

	var message string
	readChan := make(chan *result, 10)
	inputChan := make(chan string)
	
	errg.Go(func() error {
		defer log.Printf("Exiting shutdown routine")
		<-ctx.Done()
		ptmx.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		time.Sleep(2*time.Second)
		ptmx.Write([]byte{0x03})
		time.Sleep(2*time.Second)
		close(readChan)
		log.Printf("killing %d", ptmx.Fd())
		syscall.Close(int(ptmx.Fd())) // Force kill the Read()
		time.Sleep(2*time.Second)
		ptmx.Close()
		time.Sleep(2*time.Second)
		c.Process.Kill()
		time.Sleep(2*time.Second)
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
				if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
					// An expected error when the ptmx is closed to break the Read() call.
					// Since we don't want to send this error to the user, we convert it to errExit.
					return errExit
				}
				log.Printf("before readChan<-")
				readChan <- &result{buf[:n], err}
				log.Printf("after readChan<-")
				if err != nil {
					return err
				}
			}
		}
	})
	errg.Go(func() error {
		defer log.Printf("Exiting readChan loop")
		for {
			log.Printf("readChan loop")
			select {
			case result := <-readChan:
				if result.err != nil && result.err != io.EOF {
					log.Printf("readChan error: %s", result.err.Error())
					return result.err
				}
				if len(result.bytes) > 0 {
					message += shellEscapeRegex.ReplaceAllString(string(result.bytes), "")
				}
				if len(message) > maxMessageLength {
					println(message)
					message = ""
				}
				if result.err == io.EOF {
					if len(message) > 0 {
						println(message)
					}
					return errExit
				}
			case <-time.After(300 * time.Millisecond):
				if len(message) > 0 {
					println(message)
					message = ""
				}
			case <-ctx.Done():
				return nil
			}
		}
	})

	errg.Go(func() error {
		defer log.Printf("Exiting inputChan loop")
		for {
			select {
			case <-ctx.Done():
				return nil
			case input := <- inputChan:
				if strings.HasPrefix(input, "!") {
					if input == "!help" {
						println(availableCommandsMessage)
						continue
					} else if input == "!exit" {
						log.Printf("!exit")
						return errExit
					} else {
						controlChar, ok := controlCharTable[input[1:]]
						if ok {
							ptmx.Write([]byte{controlChar})
							continue
						}
					}
					// Fallthrough to underlying REPL
				}
				if _, err := io.WriteString(ptmx, fmt.Sprintf("%s\n", input)); err != nil {
					return err
				}
			}
		}
	})

	time.Sleep(5 * time.Second)
	inputChan <- "!exit"

	time.Sleep(10 * time.Second)
	println("exited main prog")
}
