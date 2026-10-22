// Package pastetrail observes paste attempts and reports their available context.
package pastetrail

import "github.com/zotniq/pastetrail/internal/model"

// Evidence distinguishes a direct observation from a guess or missing data.
type Evidence = model.Evidence

// Field carries the provenance of one destination attribute.
type Field[T any] = model.Field[T]

// Trigger describes the action suggesting a paste. Shortcuts imply intent, not insertion.
type Trigger = model.Trigger

// Representation preserves a text or rich-text clipboard format. Binary formats
// use base64 encoding. File contents are never represented here.
type Representation = model.Representation

// File describes a clipboard file reference without reading its contents.
type File = model.File

// Content is a snapshot of the available clipboard representations at capture time.
type Content = model.Content

// Application identifies the foreground destination.
type Application = model.Application

// WindowBounds uses the OS's desktop coordinates, subject to its display scaling.
type WindowBounds = model.WindowBounds

// Browser describes a tab independently of the browser process ID.
type Browser = model.Browser

// Context is one identifiable layer of a terminal, tmux, SSH, or container session.
// PID always belongs to the specified Host and, if present, Container.
type Context = model.Context

// Destination contains the foreground application and any identifiable nested context.
type Destination = model.Destination

// Event describes an attempt. It never asserts that content was inserted.
type Event = model.Event

// Capability reports an observable feature or a detection gap.
type Capability = model.Capability

// Record is a versioned JSONL envelope. Exactly one of Event or Capabilities is set.
type Record = model.Record

const (
	Observed    = model.Observed
	Inferred    = model.Inferred
	Unavailable = model.Unavailable
)

// Known constructs an observed field.
func Known[T any](value T) Field[T] { return model.Known(value) }

// Missing constructs an explicitly unavailable field.
func Missing[T any](reason string) Field[T] { return model.Missing[T](reason) }
