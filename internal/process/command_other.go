//go:build (!darwin && !linux) || (darwin && !cgo)

package process

import "github.com/zotniq/pastetrail/internal/model"

func ReadCommand(int) (Command, error) { return Command{}, model.ErrUnsupported }
