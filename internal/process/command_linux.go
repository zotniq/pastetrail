package process

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

func ReadCommand(pid int) (Command, error) {
	base := fmt.Sprintf("/proc/%d/", pid)
	path, err := os.Readlink(base + "exe")
	if err != nil {
		return Command{}, err
	}
	f, err := os.Open(base + "cmdline")
	if err != nil {
		return Command{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return Command{}, err
	}
	if len(data) == 0 || len(data) > 1<<20 || data[len(data)-1] != 0 {
		return Command{}, fmt.Errorf("invalid shell argument list")
	}
	command := Command{Path: path}
	for _, arg := range bytes.Split(data[:len(data)-1], []byte{0}) {
		command.Args = append(command.Args, string(arg))
	}
	return command, nil
}
