package bot

import (
	"context"
	"fmt"
	"golang.org/x/sync/errgroup"
	"heckel.io/replbot/util"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type Screen struct {
	id string
	log *os.File
}

func NewScreen() (*Screen, error) {
	return &Screen{
		id: fmt.Sprintf("replbot.%s", util.RandomStringWithCharset(10, charsetRandomID)),
	}, nil
}

func (s *Screen) Start(args ...string) error {
	var err error
	if err = os.WriteFile(s.logFile(), []byte{}, 0600); err != nil{
		return err
	}
	s.log, err = os.Open(s.logFile())
	if err != nil {
		return err
	}
	rcBytes := fmt.Sprintf("deflog on\nlogfile %s\nlogfile flush 0\nlog on", s.logFile())
	if err := os.WriteFile(s.rcFile(), []byte(rcBytes), 0600); err != nil{
		return err
	}
	args = append([]string{"-dmS", s.id, "-c", s.rcFile()}, args...)
	cmd := exec.Command("screen", args...)
	if err := cmd.Run(); err !=nil {
		return err
	}
	return nil
}

func (s *Screen) Read(p []byte) (n int, err error) {
	return s.log.Read(p)
}

func (s *Screen) Write(p []byte) (n int, err error) {
	if err := os.WriteFile(s.regFile(), p, 0600); err != nil{
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

func (s *Screen) Stop() error {
	defer func() {
		os.Remove(s.rcFile())
		os.Remove(s.logFile())
		os.Remove(s.regFile())
	}()
	cmd := exec.Command("screen", "-S", s.id, "-X", "quit")
	return cmd.Run()
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

type repl struct {
	ctx           context.Context
	script     string
	screen     *Screen
	sessionID     string
	sender        Sender
	userInputChan chan string
	outChan       chan []byte
}

func runREPL(ctx context.Context, sessionID string, sender Sender, userInputChan chan string, script string) error {
	r, err := newREPL(ctx, sessionID, sender, userInputChan, script)
	if err != nil {
		return err
	}
	return r.Exec()
}

func newREPL(ctx context.Context, sessionID string, sender Sender, userInputChan chan string, script string) (*repl, error) {
	// screen -dmS replbot123 -c $PWD/screenrc /usr/bin/docker run -it ruby
	// screen -S replbot123 -X readreg x /tmp/replbot123
	// screen -S replbot123 -X paste x

	screen, err := NewScreen()
	if err !=nil {
		return nil, err
	}
	return &repl{
		ctx:           ctx,
		script: script,
		screen: screen,
		sessionID:     sessionID,
		sender:        sender,
		userInputChan: userInputChan,
		outChan:       make(chan []byte, 10),
	}, nil
}

func (r *repl) Exec() error {
	log.Printf("[session %s] Started REPL session", r.sessionID)
	defer log.Printf("[session %s] Closed REPL session", r.sessionID)

	if err := r.screen.Start( r.script, "run", "abc123"); err != nil {
		return err
	}
	if err := r.sender.Send(sessionStartedMessage, Text); err != nil {
		return err
	}

	var g *errgroup.Group
	g, r.ctx = errgroup.WithContext(r.ctx)
	g.Go(r.userInputLoop)
	g.Go(r.commandOutputLoop)
	g.Go(r.commandOutputForwarder)
	g.Go(r.cleanupListener)
	return g.Wait()
}

func (r *repl) userInputLoop() error {
	log.Printf("[session %s] Started user input loop", r.sessionID)
	defer log.Printf("[session %s] Exiting user input loop", r.sessionID)
	for {
		select {
		case line := <-r.userInputChan:
			if err := r.handleUserInput(line); err != nil {
				return err
			}
		case <-r.ctx.Done():
			return errExit
		}
	}
}
func (r *repl) handleUserInput(input string) error {
	switch input {
	case helpCommand:
		return r.sender.Send(availableCommandsMessage, Markdown)
	case exitCommand:
		return errExit
	default:
		// TODO properly handle empty lines
		if strings.HasPrefix(input, commentPrefix) {
			return nil // Ignore comments
		} else if controlChar, ok := controlCharTable[input[1:]]; ok {
			//_, err := outputWriter.Write([]byte{controlChar})
			//return err
			log.Printf("ignoring control char for now %b", controlChar)
			return nil
		}
		_, err := io.WriteString(r.screen, fmt.Sprintf("%s\n", input))
		return err
	}
}

func (r *repl) commandOutputLoop() error {
	log.Printf("[session %s] Started command output loop", r.sessionID)
	defer log.Printf("[session %s] Exiting command output loop", r.sessionID)
	f, err := os.OpenFile("/home/pheckel/Code/replbot/raw-onebyte.log", os.O_CREATE | os.O_WRONLY | os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	for {
		buf := make([]byte, 512) // Allocation in a loop, ahhh ...
		n, err := r.screen.Read(buf)
		select {
		case <-r.ctx.Done():
			return errExit
		default:
			f.Write(buf[:n])
			if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
				// An expected error when the ptmx is closed to break the Read() call.
				// Since we don't want to send this error to the user, we convert it to errExit.
				return errExit
			} else if err == io.EOF {
				if n > 0 {
					select {
					case r.outChan <- buf[:n]:
					case <-r.ctx.Done():
						return errExit
					}
				}
				log.Printf("EOF")
				select {
				case <-time.After(200 * time.Millisecond):
				case <-r.ctx.Done():
					return errExit
				}
				//return nil // FIXME
			} else if err != nil {
				return err
			} else if n > 0 {
				select {
				case r.outChan <- buf[:n]:
				case <-r.ctx.Done():
					return errExit
				}
			}
		}
	}
}

func (r *repl) commandOutputForwarder() error {
	log.Printf("[session %s] Started command output forwarder", r.sessionID)
	defer log.Printf("[session %s] Exiting command output forwarder", r.sessionID)
	var message string
	for {
		select {
		case result := <-r.outChan:
			message += shellEscapeRegex.ReplaceAllString(string(result), "")
			if len(message) > maxMessageLength {
				if err := r.sender.Send(message, Code); err != nil {
					return err
				}
				message = ""
			}
		case <-time.After(300 * time.Millisecond):
			if len(message) > 0 {
				if err := r.sender.Send(message, Code); err != nil {
					return err
				}
				message = ""
			}
		case <-r.ctx.Done():
			if len(message) > 0 {
				if err := r.sender.Send(message, Code); err != nil {
					return err
				}
			}
			return errExit
		}
	}
}

func (r *repl) cleanupListener() error {
	log.Printf("[session %s] Started command cleanup listener", r.sessionID)
	defer log.Printf("[session %s] Command cleanupListener finished", r.sessionID)
	<-r.ctx.Done()
	if err := r.screen.Stop(); err != nil {
		log.Printf("warning: unable to stop screen: %s", err.Error())
	}
	return nil
}
