//go:build !cgo || (!darwin && !linux)

package enrollment

import (
	"io"

	"github.com/zotniq/pastetrail/internal/model"
)

func Enable(string) (io.Closer, model.Capability, error) {
	return nil, model.Capability{Feature: "terminal_activation", Status: "unavailable", Reason: "automatic shell activation requires macOS or Linux with cgo"}, nil
}
