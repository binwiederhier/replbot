// replbot is a Slack bot that provides interactive REPL access through Slack.
package main

import (
	"fmt"
	"github.com/urfave/cli/v2"
	"heckel.io/replbot/cmd"
	"os"
	"runtime"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	cli.AppHelpTemplate += fmt.Sprintf(`
Try 'replbot COMMAND --help' for more information.

replbot %s (%s), runtime %s, built at %s
Copyright (C) 2021 Philipp C. Heckel, distributed under the Apache License 2.0
`, version, commit[:7], runtime.Version(), date)

	app := cmd.New()
	app.Version = version

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
