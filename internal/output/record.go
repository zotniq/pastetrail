package output

import (
	"time"

	"github.com/zotniq/pastetrail/internal/model"
)

type Record struct {
	SchemaVersion int       `json:"schema_version"`
	Type          string    `json:"type"`
	Time          time.Time `json:"time"`
	Event         Event     `json:"event"`
}

type Event struct {
	ID          string      `json:"id"`
	Time        time.Time   `json:"time"`
	CapturedAt  time.Time   `json:"captured_at"`
	OS          string      `json:"os"`
	Trigger     Trigger     `json:"trigger"`
	Content     Content     `json:"content"`
	Destination Destination `json:"destination"`
}

type Trigger struct {
	Kind       string   `json:"kind"`
	Action     string   `json:"action,omitempty"`
	Automated  *bool    `json:"automated,omitempty"`
	Source     string   `json:"source"`
	Button     int      `json:"button,omitempty"`
	ClickCount int      `json:"click_count,omitempty"`
	Modifiers  []string `json:"modifiers,omitempty"`
}

type Content struct {
	Representations []model.Representation `json:"representations,omitempty"`
	Files           []model.File           `json:"files,omitempty"`
	Incomplete      bool                   `json:"incomplete,omitempty"`
}

type Application struct {
	Name          string              `json:"name,omitempty"`
	Executable    string              `json:"executable,omitempty"`
	PID           *int                `json:"pid,omitempty"`
	WindowID      string              `json:"window_id,omitempty"`
	WindowTitle   string              `json:"window_title,omitempty"`
	BundleID      string              `json:"bundle_id,omitempty"`
	BundlePath    string              `json:"bundle_path,omitempty"`
	Bounds        *model.WindowBounds `json:"bounds,omitempty"`
	ContentBounds *model.WindowBounds `json:"content_bounds,omitempty"`
	MemoryUsage   *uint64             `json:"memory_usage,omitempty"`
}

type Browser struct {
	TabID string `json:"tab_id,omitempty"`
	PID   *int   `json:"pid,omitempty"`
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
	Frame string `json:"frame,omitempty"`
}

type Context struct {
	Kind             string `json:"kind"`
	Name             string `json:"name,omitempty"`
	PID              *int   `json:"pid,omitempty"`
	Host             string `json:"host,omitempty"`
	TTY              string `json:"tty,omitempty"`
	Command          string `json:"command,omitempty"`
	WorkingDirectory string `json:"working_directory,omitempty"`
	Session          string `json:"session,omitempty"`
	Pane             string `json:"pane,omitempty"`
	Container        string `json:"container,omitempty"`
	ProcessGroup     *int   `json:"process_group,omitempty"`
}

type Destination struct {
	Application Application      `json:"application"`
	Browser     *Browser         `json:"browser,omitempty"`
	Contexts    []Context        `json:"contexts,omitempty"`
	DocumentURI string           `json:"document_uri,omitempty"`
	Selection   *model.TextRange `json:"selection,omitempty"`
}

func available[T any](f model.Field[T]) bool {
	return f.Evidence == model.Observed || f.Evidence == model.Inferred
}

func value[T any](f model.Field[T]) T {
	if available(f) {
		return f.Value
	}
	var zero T
	return zero
}

func optional[T any](f model.Field[T]) *T {
	if available(f) {
		return &f.Value
	}
	return nil
}

func Project(r model.Record) Record {
	e := r.Event
	t := e.Trigger
	return Record{
		SchemaVersion: r.SchemaVersion, Type: r.Type, Time: r.Time,
		Event: Event{
			ID: e.ID, Time: e.Time, CapturedAt: e.CapturedAt, OS: e.OS,
			Trigger: Trigger{Kind: t.Kind, Action: t.Action, Automated: optional(t.Automated), Source: t.Source, Button: t.Button, ClickCount: t.ClickCount, Modifiers: t.Modifiers},
			Content: projectContent(e), Destination: projectDestination(e.Destination),
		},
	}
}

func projectContent(e *model.Event) Content {
	c := e.Content
	selected := Content{Files: c.Files, Incomplete: c.Incomplete}
	for _, r := range c.Representations {
		if r.MediaType == "text/plain" {
			selected.Representations = []model.Representation{r}
			selected.Incomplete = selected.Incomplete || r.Truncated
			return selected
		}
	}
	if e.Delivery != nil {
		for _, r := range e.Delivery.Content.Representations {
			if r.MediaType == "text/plain" {
				selected.Representations = []model.Representation{r}
				selected.Incomplete = selected.Incomplete || e.Delivery.Content.Incomplete || r.Truncated
				return selected
			}
		}
	}
	if len(c.Representations) > 0 {
		selected.Representations = c.Representations[:1]
		selected.Incomplete = selected.Incomplete || c.Representations[0].Truncated
	}
	return selected
}

func projectDestination(d model.Destination) Destination {
	a := d.Application
	out := Destination{
		Application: Application{
			Name: value(a.Name), Executable: value(a.Executable), PID: optional(a.PID),
			WindowID: value(a.WindowID), WindowTitle: value(a.WindowTitle),
			BundleID: value(a.BundleID), BundlePath: value(a.BundlePath),
			Bounds: value(a.Bounds), ContentBounds: value(a.ContentBounds), MemoryUsage: optional(a.MemoryUsage),
		},
		Contexts: projectContexts(d.Contexts),
	}
	if b := d.Browser; b != nil {
		out.Browser = &Browser{TabID: value(b.TabID), PID: optional(b.PID), Title: value(b.Title), URL: value(b.URL), Frame: value(b.Frame)}
	}
	if f := d.Focus; f != nil {
		out.DocumentURI, out.Selection = value(f.DocumentURI), value(f.Selection)
	}
	return out
}

func projectContexts(contexts []model.Context) []Context {
	var terminal *model.Context
	var commands, candidates []model.Context
	for i := range contexts {
		c := contexts[i]
		switch c.Kind {
		case "terminal":
			terminal = &contexts[i]
		case "command":
			c.Kind = "foreground_process"
			commands = append(commands, c)
		case "foreground_process":
			candidates = append(candidates, c)
		}
	}
	if len(commands) == 0 && len(candidates) == 1 {
		commands = candidates
	}
	if len(commands) == 0 && len(candidates) > 1 {
		commands = []model.Context{{Kind: "foreground_process"}}
	}
	if len(commands) == 0 && terminal != nil {
		commands = []model.Context{{Kind: "terminal"}}
	}
	var out []Context
	for _, c := range commands {
		if terminal != nil {
			inherit(&c.Host, terminal.Host)
			inherit(&c.TTY, terminal.TTY)
			inherit(&c.Session, terminal.Session)
			inherit(&c.Pane, terminal.Pane)
			inherit(&c.Container, terminal.Container)
			inherit(&c.ProcessGroup, terminal.ProcessGroup)
		}
		out = append(out, Context{
			Kind: c.Kind, Name: value(c.Name), PID: optional(c.PID), Host: value(c.Host), TTY: value(c.TTY),
			Command: value(c.Command), WorkingDirectory: value(c.WorkingDirectory), Session: value(c.Session),
			Pane: value(c.Pane), Container: value(c.Container), ProcessGroup: optional(c.ProcessGroup),
		})
	}
	return out
}

func inherit[T any](field *model.Field[T], fallback model.Field[T]) {
	if !available(*field) {
		*field = fallback
	}
}
