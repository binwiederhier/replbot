package main

import (
	"fmt"
	"github.com/creack/pty"
	"github.com/slack-go/slack"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	shellEscapeRegex = regexp.MustCompile(`\x1B\[[0-9;]*[a-zA-Z]`)
	controlCharTable = map[string]byte {
		"r": 0x10,
		"ret": 0x10,
		"ctrl-c": 0x03,
		"ctrl-d": 0x04,
	}
	availableREPLs = map[string]string {
		"bash": "docker run -it ubuntu",
		"python": "docker run -it python",
	}
)

const (
	maxMessageLength = 512
)

type Session struct {
	rtm *slack.RTM
	started time.Time
	lastAction time.Time
	channel string
	threadTS string
	inputChan chan string
	pty *os.File
	closed bool
	mu sync.Mutex
}

func NewSession(rtm *slack.RTM, channel string, threadTS string) *Session {
	session := &Session{
		rtm: rtm,
		started: time.Now(),
		lastAction: time.Now(),
		channel: channel,
		threadTS: threadTS,
		inputChan: make(chan string, 10), // buffered!
		pty: nil,
		closed: false,
	}
	go session.welcomeInputLoop()
	return session
}

func (s *Session) welcomeInputLoop() {
	repls := make([]string, 0)
	for name, _ := range availableREPLs {
		repls = append(repls, name)
	}
	s.sendMarkdown("REPLbot welcomes you!")
	s.sendMarkdown(fmt.Sprintf("You may send start a new session by choosing any one of the available REPLs: %s", strings.Join(repls, ", ")))

	for input := range s.inputChan {
		if input == "exit" || input == "!exit" {
			s.closed = true
			return // FIXME
		}
		command, ok := availableREPLs[input]
		if !ok {
			s.sendMarkdown("Invalid command")
			continue
		}
		s.replSession(command)
		s.sendMarkdown("REPL session ended.\n\nYou may start a new session by choosing any one of the available REPLs: %s\",\n\t\tstrings.Join(repls, \", \")")
	}
}

func (s *Session) replSession(command string) {
	c := exec.Command("sh", "-c", command)
	ptmx, err := pty.Start(c)
	if err != nil {
		s.close(fmt.Sprintf("Cannot start REPL session: %s", err.Error()))
		return
	}
	// Make sure to close the pty at the end.
	defer func() { _ = ptmx.Close() }() // Best effort.

	go s.outputLoop(ptmx)

	s.sendMarkdown("Started a REPL session\n")
	for input := range s.inputChan {
		if strings.HasPrefix(input, "!") {
			if input == "!help" {
				s.sendHelp()
				continue
			} else if input == "!exit" {
				return // FIXME
			} else {
				controlChar, ok := controlCharTable[input[1:]]
				if ok {
					ptmx.Write([]byte{controlChar})
					continue
				}
			}
			// Fallthrough to underlying REPL
		}
		log.Printf("dispatching input %s", input)
		if _, err := io.WriteString(ptmx, fmt.Sprintf("%s\n", input)); err != nil {
			s.close(err.Error())
			return
		}
	}
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
	err error
}

func (s *Session) outputLoop(ptmx *os.File) {
	var message string
	readChan := make(chan *result, 10)

	go func() {
		for {
			buf := make([]byte, 4096) // FIXME alloc in a loop!
			n, err := ptmx.Read(buf)
			readChan <- &result{buf[:n], err}
		}
	}()

	for {
		log.Printf("read")
		select {
		case result := <-readChan:
			if result.err != nil && result.err != io.EOF {
				s.close(fmt.Sprintf("Error reading from REPL: %s", result.err.Error()))
				return
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
				s.close("REPL exited. Terminating session")
				return
			}
		case <-time.After(300 * time.Millisecond):
			if len(message) > 0 {
				s.sendCode(message)
				message = ""
			}
		}
	}
}

func (s *Session) sendHelp() {
	s.sendMarkdown("Available commands:```\n  !ret    Send empty return\n  !ctrl-c Send Ctrl-C\n  !ctrl-d Send Ctrl-D\n  !exit   Terminate session```")
}


