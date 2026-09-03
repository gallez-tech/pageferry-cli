package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/gallez-tech/pageferry/cli/internal/command"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	app, err := command.New(version, os.Stdin, os.Stdout, os.Stderr)
	if err == nil {
		err = app.Run(ctx, os.Args[1:])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "pageferry: %v\n", err)
		os.Exit(1)
	}
}
