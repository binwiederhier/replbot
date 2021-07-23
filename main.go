package main

import (
	"errors"
	"github.com/creack/pty"
	"github.com/slack-go/slack"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	Token string
	ScriptDir string
}

type Bot struct {
	config   *Config
	scripts map[string]string
	sessions map[string]*Session
	mu       sync.Mutex
}

func NewBot(config *Config) (*Bot, error) {
	scripts := make(map[string]string, 0)
	entries, err := os.ReadDir(config.ScriptDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		scripts[entry.Name()] = filepath.Join(config.ScriptDir, entry.Name())
	}
	return &Bot{
		config:   config,
		scripts: scripts,
		sessions: make(map[string]*Session),
	}, nil
}

func (b *Bot) Start() error {
	api := slack.New(b.config.Token, slack.OptionLog(log.New(os.Stdout, "slack-bot: ", log.Lshortfile|log.LstdFlags)))
	rtm := api.NewRTM()
	go rtm.ManageConnection()
	go b.manageSessions() // FIXME

	for msg := range rtm.IncomingEvents {
		switch ev := msg.Data.(type) {
		case *slack.ConnectedEvent:
			log.Print("Slack connected")
		case *slack.MessageEvent:
			if ev.User == "" {
				continue // Ignore my own messages
			}
			if ev.ThreadTimestamp == "" && strings.TrimSpace(ev.Text) == "repl" {
				log.Printf("Starting new session %s, requested by user %s\n", ev.Timestamp, ev.User)
				b.mu.Lock()
				b.sessions[ev.Timestamp] = NewSession(b.scripts, rtm, ev.Channel, ev.Timestamp)
				b.mu.Unlock()
			} else if ev.ThreadTimestamp != "" {
				b.mu.Lock()
				if session, ok := b.sessions[ev.ThreadTimestamp]; ok {
					session.inputChan <- ev.Text
				}
				b.mu.Unlock()
			}
		case *slack.LatencyReport:
			log.Printf("Current latency: %v\n", ev.Value)
		case *slack.RTMError:
			log.Printf("Error: %s\n", ev.Error())
		case *slack.InvalidAuthEvent:
			return errors.New("invalid credentials")
		default:
			// Ignore other events
		}
	}

	return errors.New("unexpected end of incoming events stream")
}

func (b *Bot) manageSessions() {
	for {
		b.mu.Lock()
		for id, session := range b.sessions {
			if session.IsClosed() {
				log.Printf("Removing session %s", session.threadTS)
				delete(b.sessions, id)
			}
		}
		b.mu.Unlock()
		time.Sleep(5 * time.Second)
	}
}

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

	/*
		c := exec.Command("sh", "-c", "rm /tmp/date; while true; do date >> /tmp/date; sleep 1; done")
		ptmx, err := pty.Start(c)
		if err != nil {
			panic(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer func() {
			cancel()
			ptmx.Close()
			log.Printf("Closed REPL session")
		}()

		go func() {
			for {
				select {
				case <-ctx.Done():
					log.Printf("Exiting read loop")
					return
				default:
				}
				buf := make([]byte, 4096) // FIXME alloc in a loop!
				n, err := ptmx.Read(buf)
				log.Printf("read loop: %s %#v", string(buf[:n]), err)
			}
		}()

		println("sleep")
		time.Sleep(5 * time.Second)

		println("cancel")
		cancel()
		time.Sleep(5 * time.Second)
		println("close")

		c.Process.Kill()
		ptmx.Close()

		time.Sleep(5 * time.Second)
		println("exit")

		os.Exit(0)*/
/*
	var ErrExit = errors.New("exit")

	for i := 1; ; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		errg, ctx := errgroup.WithContext(ctx)
		errg.Go(func() error {
			defer log.Printf("[%d] EXIT fn 1 ", i)
			for {
				time.Sleep(time.Second)
				select {
				case <-ctx.Done():
					return nil
				default:
					log.Printf("[%d] fn 1", i)
				}
			}
		})

		errg.Go(func() error {
			defer log.Printf("[%d] EXIT fn 2", i)
			rd := bufio.NewReader(os.Stdin)
			for {
				s, _ := rd.ReadString('\n')
				select {
				case <-ctx.Done():
					return nil
				default:
					if strings.TrimSpace(s) == "exit" {
						return ErrExit
					}
					if strings.TrimSpace(s) == "err" {
						return errors.New("ERROR happened oh no")
					}
					log.Printf("[%d] fn 2: %s", i, s)
				}
			}
		})

		go func() {
			time.Sleep(3 * time.Second)
			log.Printf("[%d] canceling stuff", i)
			cancel()
		}()
		if err := errg.Wait(); err != nil && err != ErrExit {
			log.Printf("[%d] ERR received in main session: %s", i, err.Error())
		}

		log.Printf("[%d] EXIT main session", i)
	}

	os.Exit(0)*/

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
