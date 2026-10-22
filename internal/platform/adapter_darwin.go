//go:build darwin && cgo

package platform

import "github.com/zotniq/pastetrail/internal/platform/macos"

func TerminalTTY(pid int, iterm bool) (string, int) { return macos.TerminalTTY(pid, iterm) }
func BrowserURL(pid, kind int) (string, int)        { return macos.BrowserURL(pid, kind) }
func CWD(pid int) (string, error)                   { return macos.CWD(pid) }
