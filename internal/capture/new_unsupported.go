//go:build !cgo || (!darwin && !windows && !linux)

package capture

import "github.com/zotniq/pastetrail/internal/model"

func New(int) model.Source {
	return unavailableSource{reason: "native monitoring requires a supported OS and a build with CGO_ENABLED=1"}
}
