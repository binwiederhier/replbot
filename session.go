package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/creack/pty"
	"github.com/slack-go/slack"
	"golang.org/x/sync/errgroup"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	shellEscapeRegex = regexp.MustCompile(`\x1B\[[0-9;]*[a-zA-Z]`)
	controlCharTable = map[string]byte{
		"r":      0x10,
		"ret":    0x10,
		"ctrl-c": 0x03,
		"ctrl-d": 0x04,
	}
	availableREPLs = map[string]string{
		"bash":   "docker run -it ubuntu",
		"python": "docker run -it python",
		"node": "docker run -it node",
		"php": "docker run -it php",
		"scala": "docker run -it bigtruedata/scala",
		"phil": "/home/pheckel/Code/philbot/phil",
	}
	welcomeMessage = "REPLbot welcomes you!\n\nYou may start a new session by choosing any one of the " +
		"available REPLs: %s. Type `!help` for help and `!exit` to exit this session."
	sessionExitedMessage = "REPL session ended.\n\nYou may start a new session by choosing any one of the " +
		"available REPLs: %s. Type `!help` for help and `!exit` to exit this session."
	byeMessage               = "REPLbot says bye bye!"
	availableCommandsMessage = "Available commands:\n" +
		"  `!ret`, `!r` - Send empty return\n" +
		"  `!ctrl-c`, `!ctrl-d`, ... - Send command sequence\n" +
		"  `!exit` - Exit this session"
	errExit = errors.New("exited REPL session")
)

const (
	maxMessageLength = 512
)

type Session struct {
	rtm        *slack.RTM
	started    time.Time
	lastAction time.Time
	channel    string
	threadTS   string
	inputChan  chan string
	closed     bool
	mu         sync.Mutex
}

func NewSession(rtm *slack.RTM, channel string, threadTS string) *Session {
	session := &Session{
		rtm:        rtm,
		started:    time.Now(),
		lastAction: time.Now(),
		channel:    channel,
		threadTS:   threadTS,
		inputChan:  make(chan string, 10), // buffered!
		closed:     false,
	}
	go session.inputLoop()
	return session
}

func (s *Session) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Session) inputLoop() {
	s.sayHello()

	for input := range s.inputChan {
		if input == "!exit" {
			s.close(byeMessage)
			return
		} else if input == "!help" {
			s.sendMarkdown(availableCommandsMessage)
			continue
		}
		command, ok := availableREPLs[input]
		if !ok {
			s.sendMarkdown("Invalid command")
			continue
		}
		if err := s.replSession(command); err != nil && err != errExit {
			s.sendMarkdown(err.Error())
		}
		s.sayExited()
	}
}

func (s *Session) sayHello() error {
	_, err := s.sendMarkdown(fmt.Sprintf(welcomeMessage, strings.Join(s.replList(), ", ")))
	return err
}

func (s *Session) sayExited() error {
	_, err := s.sendMarkdown(fmt.Sprintf(sessionExitedMessage, strings.Join(s.replList(), ", ")))
	return err
}

func (s *Session) replList() []string {
	repls := make([]string, 0)
	for name, _ := range availableREPLs {
		repls = append(repls, fmt.Sprintf("`%s`", name))
	}
	return repls
}

func (s *Session) replSession(command string) error {
	defer log.Printf("Closed REPL session")

	c := exec.Command("sh", "-c", command)
	ptmx, err := pty.Start(c)
	if err != nil {
		return fmt.Errorf("cannot start REPL session: %s", err.Error())
	}

	log.Printf("ptmx fd: %#v", ptmx.Fd())
	errg, ctx := errgroup.WithContext(context.Background())

	var message string
	readChan := make(chan *result, 10)


	errg.Go(func() error {
		defer log.Printf("Exiting shutdown routine")
		<-ctx.Done()
		time.Sleep(2*time.Second)
		close(readChan)
		time.Sleep(2*time.Second)
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
					s.sendCode(message)
					message = ""
				}
				if result.err == io.EOF {
					if len(message) > 0 {
						s.sendCode(message)
					}
					return errExit
				}
			case <-time.After(300 * time.Millisecond):
				if len(message) > 0 {
					s.sendCode(message)
					message = ""
				}
			case <-ctx.Done():
				return nil
			}
		}
	})

	s.sendMarkdown("Started a new REPL session")
	errg.Go(func() error {
		defer log.Printf("Exiting inputChan loop")
		for {
			select {
			case <-ctx.Done():
				return nil
			case input := <- s.inputChan:
				if strings.HasPrefix(input, "!") {
					if input == "!help" {
						s.sendMarkdown(availableCommandsMessage)
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

	return errg.Wait()
}

func (s *Session) sendText(message string) (string, error) {
	return s.send(slack.MsgOptionText(message, false))
}

func (s *Session) sendCode(message string) (string, error) {
	markdown := fmt.Sprintf("```%s```", strings.ReplaceAll(message, "```", "` ` `")) // Hack ...
	return s.sendMarkdown(markdown)
}

func (s *Session) sendMarkdown(markdown string) (string, error) {
	textBlock := slack.NewTextBlockObject("mrkdwn", markdown, false, true)
	sectionBlock := slack.NewSectionBlock(textBlock, nil, nil)
	return s.send(slack.MsgOptionBlocks(sectionBlock))
}

func (s *Session) send(options ...slack.MsgOption) (string, error) {
	options = append(options, slack.MsgOptionTS(s.threadTS))
	_, responseTS, err := s.rtm.PostMessage(s.channel, options...)
	if err != nil {
		log.Printf("Cannot send message: %s", err.Error())
		return "", err
	}
	return responseTS, nil
}

func (s *Session) close(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	log.Printf(message)
	s.sendText(message)
	s.closed = true
}

type result struct {
	bytes []byte
	err   error
}
