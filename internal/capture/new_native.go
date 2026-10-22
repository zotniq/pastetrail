//go:build cgo && (darwin || windows || linux)

package capture

import (
	"github.com/zotniq/pastetrail/internal/model"
	"github.com/zotniq/pastetrail/internal/platform"
)

func New(maxContentBytes int) model.Source {
	return newNativeSource(platform.Bridge{}, maxContentBytes)
}
