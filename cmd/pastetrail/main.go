package main

import (
	"context"
	"github.com/zotniq/pastetrail/internal/cli"
	"os"
	"os/signal"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, version, cli.DefaultDependencies())
	stop()
	os.Exit(code)
}
