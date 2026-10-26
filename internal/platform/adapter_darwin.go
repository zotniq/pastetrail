//go:build darwin && cgo

package platform

import "github.com/zotniq/pastetrail/internal/platform/macos"

func BrowserURL(pid, kind int) (string, int) { return macos.BrowserURL(pid, kind) }
func CWD(pid int) (string, error)            { return macos.CWD(pid) }
