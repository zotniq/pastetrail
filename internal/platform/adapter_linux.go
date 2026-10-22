//go:build linux

package platform

import "github.com/zotniq/pastetrail/internal/platform/linux"

func CWD(pid int) (string, error) { return linux.CWD(pid) }
