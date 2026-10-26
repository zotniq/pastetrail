//go:build !cgo || (!darwin && !linux)

package relay

import (
	"context"
	"errors"
	"io"
)

func Run(context.Context, []string, string, io.Writer) (int, error) {
	return 1, errors.New("terminal relay requires a macOS or Linux build with CGO enabled")
}

func Managed(ctx context.Context, args []string, argv0, socket string, out io.Writer) (int, error) {
	return Run(ctx, args, socket, out)
}

func Prepare(context.Context, int, string, string, string, string) (string, error) { return "", nil }
