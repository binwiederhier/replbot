package bot

import (
	"context"
	"fmt"
	"github.com/creack/pty"
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

type repl struct {
	sessionID     string
	sender        Sender
	userInputChan chan string
}

func (r *repl) Exec(ctx context.Context, command string) error {
	log.Printf("[session %s] Started REPL session", r.sessionID)
	defer log.Printf("[session %s] Closed REPL session", r.sessionID)

	if err := r.sender.Send(sessionStartedMessage, Text); err != nil {
		return err
	}

	exitMarker := util.RandomStringWithCharset(10, exitMarkerCharset)
	cmd := exec.Command("sh", "-c", fmt.Sprintf(launcherScript, command, exitMarker))
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("cannot start REPL session: %s", err.Error())
	}

	outChan := make(chan []byte, 10)
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		log.Printf("[session %s] Started command output loop", r.sessionID)
		defer log.Printf("[session %s] Exiting command output loop", r.sessionID)
		for {
			buf := make([]byte, 4096) // Allocation in a loop, ahhh ...
			n, err := ptmx.Read(buf)
			select {
			case <-ctx.Done():
				return errExit
			default:
				if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
					// An expected error when the ptmx is closed to break the Read() call.
					// Since we don't want to send this error to the user, we convert it to errExit.
					return errExit
				} else if err == io.EOF {
					if n > 0 {
						outChan <- buf[:n]
					}
					return errExit
				} else if err != nil {
					return err
				} else if strings.TrimSpace(string(buf[:n])) == exitMarker {
					return errExit
				} else if n > 0 {
					outChan <- buf[:n]
				}
			}
		}
	})
	g.Go(func() error {
		log.Printf("[session %s] Started response loop", r.sessionID)
		defer log.Printf("[session %s] Exiting response loop", r.sessionID)
		var message string
		for {
			select {
			case result := <-outChan:
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
			case <-ctx.Done():
				if len(message) > 0 {
					if err := r.sender.Send(message, Code); err != nil {
						return err
					}
				}
				return errExit
			}
		}
	})
	g.Go(func() error {
		log.Printf("[session %s] Started user input loop", r.sessionID)
		defer log.Printf("[session %s] Exiting user input loop", r.sessionID)
		for {
			select {
			case line := <-r.userInputChan:
				if err := r.handleUserInput(line, ptmx); err != nil {
					return err
				}
			case <-ctx.Done():
				return errExit
			}
		}
	})
	g.Go(func() error {
		defer log.Printf("[session %s] Command cleanup finished", r.sessionID)
		<-ctx.Done()
		if err := util.KillChildProcesses(cmd.Process.Pid); err != nil {
			log.Printf("warning: %s", err.Error())
		}
		if err := ptmx.Close(); err != nil {
			log.Printf("warning: %s", err.Error())
		}
		return nil
	})
	return g.Wait()
}

func (r *repl) handleUserInput(input string, outputWriter io.Writer) error {
	switch input {
	case helpCommand:
		return r.sender.Send(availableCommandsMessage, Markdown)
	case exitCommand:
		return errExit
	default:
		// TODO properly handle empty lines
		if controlChar, ok := controlCharTable[input[1:]]; ok {
			_, err := outputWriter.Write([]byte{controlChar})
			return err
		}
		_, err := io.WriteString(outputWriter, fmt.Sprintf("%s\n", input))
		return err
	}
}
