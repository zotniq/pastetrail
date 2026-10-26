//go:build !windows

package main

import (
	"os"
	"syscall"
)

var terminationSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
