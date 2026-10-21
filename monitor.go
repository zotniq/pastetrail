package pastetrail

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
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

// Options controls payload size and duplicate tracking.
type Options struct {
	MaxContentBytes int
	DedupWindow     time.Duration
	MaxDedupEntries int
}

func (o Options) defaults() (Options, error) {
	if o.MaxContentBytes < 0 || o.DedupWindow < 0 || o.MaxDedupEntries < 0 {
		return o, errors.New("pastetrail: options cannot be negative")
	}
	if o.MaxContentBytes == 0 {
		o.MaxContentBytes = 1 << 20
	}
	if o.DedupWindow == 0 {
		o.DedupWindow = 2 * time.Second
	}
	if o.MaxDedupEntries == 0 {
		o.MaxDedupEntries = 4096
	}
	return o, nil
}

// Monitor owns a source. It can be run again after a previous Run returns.
type Monitor struct {
	source  Source
	options Options
	running atomic.Bool
	now     func() time.Time
	newID   func() (string, error)
}

// New creates the native monitor for the current operating system.
func New(options Options) (*Monitor, error) {
	o, err := options.defaults()
	if err != nil {
		return nil, err
	}
	return NewWithSource(newPlatformSource(o), o)
}

// NewWithSource supports embedding an application-provided observation source.
func NewWithSource(source Source, options Options) (*Monitor, error) {
	if source == nil {
		return nil, errors.New("pastetrail: source is required")
	}
	o, err := options.defaults()
	if err != nil {
		return nil, err
	}
	return &Monitor{source: source, options: o, now: time.Now, newID: randomID}, nil
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Capabilities reports detection coverage without starting monitoring or
// prompting for permissions. Call it when Run is not active.
func (m *Monitor) Capabilities(ctx context.Context) []Capability {
	return m.source.Capabilities(ctx)
}

// Run delivers a startup capability record, then observations. The caller owns
// logging and storage. A sink error stops collection and is returned unchanged.
func (m *Monitor) Run(ctx context.Context, sink func(Record) error) error {
	if ctx == nil || sink == nil {
		return errors.New("pastetrail: context and sink are required")
	}
	if !m.running.CompareAndSwap(false, true) {
		return ErrRunning
	}
	defer m.running.Store(false)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := sink(Record{SchemaVersion: 1, Type: "capabilities", Time: m.now().UTC(), Capabilities: m.Capabilities(ctx)}); err != nil {
		return err
	}
	seen := make(map[string]time.Time)
	return m.source.Run(ctx, func(o Observation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := m.now()
		if o.Capabilities != nil {
			return sink(Record{SchemaVersion: 1, Type: "capabilities", Time: now.UTC(), Capabilities: o.Capabilities})
		}
		if o.Key != "" {
			for key, at := range seen {
				if now.Sub(at) >= m.options.DedupWindow {
					delete(seen, key)
				}
			}
			if _, ok := seen[o.Key]; ok {
				return nil
			}
			if len(seen) >= m.options.MaxDedupEntries {
				var oldest string
				var at time.Time
				for key, t := range seen {
					if at.IsZero() || t.Before(at) {
						oldest, at = key, t
					}
				}
				delete(seen, oldest)
			}
			seen[o.Key] = now
		}
		e := o.Event
		id, err := m.newID()
		if err != nil {
			return fmt.Errorf("pastetrail: event ID: %w", err)
		}
		e.ID = id
		if e.Time.IsZero() {
			e.Time = now
		}
		if e.CapturedAt.IsZero() {
			e.CapturedAt = now
		}
		e.Time = e.Time.UTC()
		e.CapturedAt = e.CapturedAt.UTC()
		e.OS = runtime.GOOS
		normalizeEvent(&e, m.options.MaxContentBytes)
		return sink(Record{SchemaVersion: 1, Type: "paste_attempt", Time: e.Time, Event: &e})
	})
}
