package model

import (
	"context"
	"errors"
)

var (
	ErrRunning     = errors.New("pastetrail: monitor is already running")
	ErrUnsupported = errors.New("pastetrail: monitoring is unavailable in this environment")
)

// Observation associates a native attempt with a snapshot. Key is a native event
// identity, not a content hash; different pastes must have different keys.
type Observation struct {
	Key          string
	Event        Event
	Capabilities []Capability
}

// Source supplies observations serially until cancellation or error. emit may
// block; native implementations must buffer independently and report overflows.
type Source interface {
	Capabilities(context.Context) []Capability
	Run(context.Context, func(Observation) error) error
}
