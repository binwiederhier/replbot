package main

import (
	"bufio"
	"github.com/creack/pty"
	"golang.org/x/term"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"syscall"
	"time"
)


func test() error {
	// Create arbitrary command.
	c := exec.Command("bash")

	// Start the command with a pty.
	ptmx, err := pty.Start(c)
	if err != nil {
		return err
	}
	// Make sure to close the pty at the end.
	defer func() { _ = ptmx.Close() }() // Best effort.

	// Handle pty size.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			if err := pty.InheritSize(os.Stdin, ptmx); err != nil {
				log.Printf("error resizing pty: %s", err)
			}
		}
	}()
	ch <- syscall.SIGWINCH // Initial resize.
	defer func() { signal.Stop(ch); close(ch) }() // Cleanup signals when done.

	// Set stdin in raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }() // Best effort.

	// Copy stdin to the pty and the pty to stdout.
	// NOTE: The goroutine will keep reading until the next keystroke before returning.
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
	_, _ = io.Copy(os.Stdout, ptmx)

	return nil
}

func test2() {
	c := exec.Command("bash")

	// Start the command with a pty.
	ptmx, err := pty.Start(c)
	if err != nil {
		panic(err)
	}
	// Make sure to close the pty at the end.
	defer func() { _ = ptmx.Close() }() // Best effort.

	go func() {
		scanner := bufio.NewScanner(ptmx)
		for scanner.Scan() {
			line := scanner.Text()
			r := regexp.MustCompile(`\x1B\[[0-9;]*[a-zA-Z]`)
			line = r.ReplaceAllString(line, "")

			println("out: " + line)
		}
	}()
	io.WriteString(ptmx, "id\n")
	time.Sleep(time.Second)
	//wait()
}

func wait() {
	for {
		time.Sleep(time.Second)
	}
}

func main() {
	test2()
	//wait()
	/*
	var stdin, stdout, stderr bytes.Buffer

	fmt.Println("Before Python shell:")
	cmd := exec.Command("bash", "-c", "/usr/bin/python3")
	cmd.Stdin = &stdin
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	go func() {
		_ = cmd.Run()
		fmt.Println("After Python shell")
	}()

	for {
		time.Sleep(time.Second)
	}
*/
	/*

		fmt.Println("Before Python shell:")
		cmd := exec.Command("bash", "-c", "/usr/bin/python3")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run() // add error checking
		fmt.Println("After Python shell")
	 */
}
