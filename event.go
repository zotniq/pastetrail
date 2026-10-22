// Package pastetrail observes paste attempts and reports their available context.
package pastetrail

import "time"

// Evidence distinguishes a direct observation from a guess or missing data.
type Evidence string

const (
	Observed    Evidence = "observed"
	Inferred    Evidence = "inferred"
	Unavailable Evidence = "unavailable"
)

// Field carries the provenance of one destination attribute.
type Field[T any] struct {
	Value    T        `json:"value,omitempty"`
	Evidence Evidence `json:"evidence"`
	Reason   string   `json:"reason,omitempty"`
}

// Known constructs an observed field.
func Known[T any](value T) Field[T] { return Field[T]{Value: value, Evidence: Observed} }

// Missing constructs an explicitly unavailable field.
func Missing[T any](reason string) Field[T] { return Field[T]{Evidence: Unavailable, Reason: reason} }

// Trigger describes the action suggesting a paste. Shortcuts imply intent, not insertion.
type Trigger struct {
	Kind       string      `json:"kind"`
	Action     string      `json:"action,omitempty"`
	Evidence   Evidence    `json:"evidence"`
	Automated  Field[bool] `json:"automated"`
	Source     string      `json:"source"`
	Reason     string      `json:"reason,omitempty"`
	Button     int         `json:"button,omitempty"`
	ClickCount int         `json:"click_count,omitempty"`
	Modifiers  []string    `json:"modifiers,omitempty"`
}

// Representation preserves a text or rich-text clipboard format. Binary formats
// use base64 encoding. File contents are never represented here.
type Representation struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	Encoding  string `json:"encoding"`
	Size      int    `json:"size"`
	Truncated bool   `json:"truncated,omitempty"`
}

// File describes a clipboard file reference without reading its contents.
type File struct {
	Name          string     `json:"name"`
	Path          string     `json:"path,omitempty"`
	URI           string     `json:"uri,omitempty"`
	Size          *int64     `json:"size,omitempty"`
	Directory     *bool      `json:"directory,omitempty"`
	Modified      *time.Time `json:"modified,omitempty"`
	MetadataError string     `json:"metadata_error,omitempty"`
}

// Content is a snapshot of the available clipboard representations at capture time.
type Content struct {
	Representations []Representation `json:"representations,omitempty"`
	Files           []File           `json:"files,omitempty"`
	Evidence        Evidence         `json:"evidence"`
	Reason          string           `json:"reason,omitempty"`
	Incomplete      bool             `json:"incomplete,omitempty"`
}

// Application identifies the foreground destination.
type Application struct {
	Name        Field[string] `json:"name"`
	Executable  Field[string] `json:"executable"`
	PID         Field[int]    `json:"pid"`
	WindowID    Field[string] `json:"window_id"`
	WindowTitle Field[string] `json:"window_title"`
}

// Browser describes a tab independently of the browser process ID.
type Browser struct {
	TabID Field[string] `json:"tab_id"`
	PID   Field[int]    `json:"pid"` // Renderer process, distinct from Application.PID.
	Title Field[string] `json:"title"`
	URL   Field[string] `json:"url"`
	Frame Field[string] `json:"frame"`
}

// Context is one identifiable layer of a terminal, tmux, SSH, or container session.
// PID always belongs to the specified Host and, if present, Container.
type Context struct {
	Kind             string        `json:"kind"`
	Name             Field[string] `json:"name"`
	PID              Field[int]    `json:"pid"`
	Host             Field[string] `json:"host"`
	TTY              Field[string] `json:"tty"`
	Command          Field[string] `json:"command"`
	WorkingDirectory Field[string] `json:"working_directory"`
	Session          Field[string] `json:"session"`
	Pane             Field[string] `json:"pane"`
	Container        Field[string] `json:"container"`
}

// Destination contains the foreground application and any identifiable nested context.
type Destination struct {
	Application Application `json:"application"`
	Browser     *Browser    `json:"browser,omitempty"`
	Contexts    []Context   `json:"contexts,omitempty"`
}

// Event describes an attempt. It never asserts that content was inserted.
type Event struct {
	ID          string      `json:"id"`
	Time        time.Time   `json:"time"`
	CapturedAt  time.Time   `json:"captured_at"`
	OS          string      `json:"os"`
	Trigger     Trigger     `json:"trigger"`
	Content     Content     `json:"content"`
	Destination Destination `json:"destination"`
}

// Capability reports an observable feature or a detection gap.
type Capability struct {
	Feature string `json:"feature"`
	Status  string `json:"status"` // supported, partial, unavailable, permission_required
	Reason  string `json:"reason,omitempty"`
}

// Record is a versioned JSONL envelope. Exactly one of Event or Capabilities is set.
type Record struct {
	SchemaVersion int          `json:"schema_version"`
	Type          string       `json:"type"`
	Time          time.Time    `json:"time"`
	Event         *Event       `json:"event,omitempty"`
	Capabilities  []Capability `json:"capabilities,omitempty"`
}
