package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/zotniq/pastetrail/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), terminationSignals...)
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, version, cli.DefaultDependencies())
	stop()
	os.Exit(code)
}
