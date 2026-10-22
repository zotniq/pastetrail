//go:build windows

package main

import "os"

var terminationSignals = []os.Signal{os.Interrupt}
