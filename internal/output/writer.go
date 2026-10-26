package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/zotniq/pastetrail/internal/model"
)

type Writer struct {
	encoder     *json.Encoder
	diagnostics io.Writer
	detailed    bool
}

func NewWriter(out, diagnostics io.Writer, detailed bool) *Writer {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	return &Writer{encoder: encoder, diagnostics: diagnostics, detailed: detailed}
}

func (w *Writer) Write(r model.Record) error {
	for _, c := range r.Capabilities {
		show := c.Status == "permission_required"
		switch c.Feature {
		case "terminal_activation", "terminal_sessions", "native_monitoring", "monitoring", "event_delivery", "native_event_delivery":
			show = true
		case "terminal_delivery":
			show = len(r.Capabilities) == 1
		}
		if show {
			fmt.Fprintf(w.diagnostics, "pastetrail: %s\n", c.Reason)
		}
	}
	if w.detailed {
		return w.encoder.Encode(r)
	}
	if r.Event != nil {
		return w.encoder.Encode(Project(r))
	}
	return nil
}
