//go:build !darwin && !linux

package terminalipc

import (
	"context"
	"github.com/zotniq/pastetrail/internal/model"
	"github.com/zotniq/pastetrail/internal/terminal"
)

const Supported = false

type Server struct{ Path string }

func (Server) Run(context.Context, func(terminal.Packet) error) error { return model.ErrUnsupported }
