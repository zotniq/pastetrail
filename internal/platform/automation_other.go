//go:build !darwin || !cgo

package platform

func TerminalTTY(int, bool) (string, int) { return "", -1 }

func BrowserURL(int, int) (string, int) { return "", -1 }
