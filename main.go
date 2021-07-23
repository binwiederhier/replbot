package main

import (
	"github.com/creack/pty"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 {
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
