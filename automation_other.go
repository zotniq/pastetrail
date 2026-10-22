//go:build !darwin || !cgo

package pastetrail

func nativeTerminalTTY(int, bool) (string, int) { return "", -1 }

func nativeBrowserURL(int, int) (string, int) { return "", -1 }
