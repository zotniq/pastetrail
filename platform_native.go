//go:build cgo && (darwin || windows || linux)

package pastetrail

import "github.com/zotniq/pastetrail/internal/platform"

func newPlatformSource(o Options) Source { return newNativeSource(platform.Bridge{}, o) }
