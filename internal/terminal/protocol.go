package terminal

import (
	"context"
	"time"

	"github.com/zotniq/pastetrail/internal/model"
)

const (
	ProtocolVersion = 1
	MaxInputBytes   = 16 << 10
	MatchWindow     = 2 * time.Second
)

type Session struct {
	ID          string            `json:"id"`
	TTY         string            `json:"tty"`
	ShellPID    int               `json:"shell_pid"`
	RelayPID    int               `json:"relay_pid"`
	Host        string            `json:"host"`
	StartedAt   time.Time         `json:"started_at"`
	Ancestors   []int             `json:"ancestors,omitempty"`
	Application model.Application `json:"application"`
}

type Frame struct {
	Version      int       `json:"version"`
	Type         string    `json:"type"`
	Session      *Session  `json:"session,omitempty"`
	Sequence     uint64    `json:"sequence,omitempty"`
	Time         time.Time `json:"time,omitempty"`
	ProcessGroup int       `json:"process_group,omitempty"`
	Data         []byte    `json:"data,omitempty"`
}

type Packet struct {
	Session Session
	Frame   Frame
}

type Receiver interface {
	Run(context.Context, func(Packet) error) error
}

type Publisher interface {
	Input(time.Time, int, []byte)
	Close()
}
