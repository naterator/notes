package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/naterator/notes/internal/cli"
)

func main() {
	// A closed downstream pipe should return EPIPE so the command can exit
	// quietly, including when the shell has pipefail enabled.
	signal.Ignore(syscall.SIGPIPE)
	app := cli.New(os.Stdin, os.Stdout, os.Stderr)
	if err := app.Execute(context.Background(), os.Args[1:]); err != nil {
		if errors.Is(err, syscall.EPIPE) {
			return
		}
		fmt.Fprintln(os.Stderr, "notes:", err)
		os.Exit(cli.ExitCode(err))
	}
}
