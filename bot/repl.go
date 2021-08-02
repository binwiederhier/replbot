package bot

import (
	"context"
	"fmt"
	"golang.org/x/sync/errgroup"
	"heckel.io/replbot/util"
	"io"
	"log"
	"strings"
	"time"
)

type repl struct {
	ctx           context.Context
	script        string
	screen        *util.Screen
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
	screen, err := util.NewScreen()
	if err != nil {
		return nil, err
	}
	return &repl{
		ctx:           ctx,
		script:        script,
		screen:        screen,
		sessionID:     sessionID,
		sender:        sender,
		userInputChan: userInputChan,
		outChan:       make(chan []byte, 10),
	}, nil
}

func (r *repl) Exec() error {
	log.Printf("[session %s] Started REPL session", r.sessionID)
	defer log.Printf("[session %s] Closed REPL session", r.sessionID)

	if err := r.screen.Start(r.script); err != nil {
		return err
	}
	if err := r.sender.Send(sessionStartedMessage, Text); err != nil {
		return err
	}

	var g *errgroup.Group
	g, r.ctx = errgroup.WithContext(r.ctx)
	g.Go(r.userInputLoop)
	g.Go(r.commandOutputLoop)
	g.Go(r.screenWatchLoop)
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
		} else if len(input) > 1 {
			if controlChar, ok := controlCharTable[input[1:]]; ok {
				return r.screen.Stuff(controlChar)
			}
		}
		_, err := io.WriteString(r.screen, fmt.Sprintf("%s\n", input))
		return err
	}
}

func (r *repl) commandOutputLoop() error {
	log.Printf("[session %s] Started command output loop", r.sessionID)
	defer log.Printf("[session %s] Exiting command output loop", r.sessionID)
	var id, last, lastID string
	var lastTime time.Time
	for {
		select {
		case <-r.ctx.Done():
			return errExit
		case <-time.After(200 * time.Millisecond):
			current, err := r.screen.Hardcopy()
			if err != nil {
				return err
			} else if current == last || current == "" {
				continue
			}
			sanitized := consoleCodeRegex.ReplaceAllString(current, "")
			if time.Since(lastTime) < 270*time.Second {
				log.Printf("updating with id %s", lastID)
				if err := r.sender.Update(lastID, sanitized, Code); err != nil {
					log.Printf("updating failed: %s", err.Error())
					if id, err = r.sender.SendWithID(sanitized, Code); err != nil {
						return err
					}
				}
			} else if id, err = r.sender.SendWithID(sanitized, Code); err != nil {
				return err
			}
			last = current
			lastID = id
			lastTime = time.Now()
		}
	}
}

func (r *repl) screenWatchLoop() error {
	log.Printf("[session %s] Started screen watch loop", r.sessionID)
	defer log.Printf("[session %s] Exiting screen watch loop", r.sessionID)
	for {
		select {
		case <-r.ctx.Done():
			return errExit
		case <-time.After(500 * time.Millisecond):
			if !r.screen.Active() {
				return errExit
			}
		}
	}
}

func (r *repl) cleanupListener() error {
	log.Printf("[session %s] Started cleanup listener", r.sessionID)
	defer log.Printf("[session %s] Exited cleanup finished", r.sessionID)
	<-r.ctx.Done()
	if err := r.screen.Stop(); err != nil {
		log.Printf("warning: unable to stop screen: %s", err.Error())
	}
	return nil
}
