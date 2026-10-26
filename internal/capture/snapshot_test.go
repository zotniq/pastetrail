package capture

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zotniq/pastetrail/internal/content"
	"github.com/zotniq/pastetrail/internal/model"
)

func TestWindowMetadata(t *testing.T) {
	for _, method := range []string{"event_target_pid", "accessibility_focus", "unavailable"} {
		data, _ := json.Marshal(map[string]any{
			"key": "window", "time_ns": 1, "pid": 42, "bundle_id": "com.google.Chrome", "bundle_path": "/Applications/Google Chrome.app",
			"window": "123", "bounds": model.WindowBounds{X: -120, Y: 0, Width: 1000, Height: 800}, "content_bounds": model.WindowBounds{X: -120, Y: 24, Width: 1000, Height: 776}, "memory_usage": 123456,
			"target_method": method, "window_method": "frontmost_window_for_event_pid",
		})
		o, err := decodeSnapshot(data, 1024)
		if err != nil {
			t.Fatal(err)
		}
		content.NormalizeEvent(&o.Event, 1024)
		a := o.Event.Destination.Application
		if method == "unavailable" {
			if a.Bounds.Value != nil || a.MemoryUsage.Value != 0 || a.BundleID.Value != "" || o.Event.Destination.Browser != nil {
				t.Fatal(a)
			}
			continue
		}
		if a.BundleID.Value != "com.google.Chrome" || a.BundlePath.Value == "" || a.Bounds.Value.X != -120 || a.ContentBounds.Value.Height != 776 || a.MemoryUsage.Value != 123456 || a.WindowID.Evidence != model.Inferred || a.Bounds.Evidence != model.Inferred || o.Event.Destination.Browser == nil {
			t.Fatal(a)
		}
		if method == "accessibility_focus" && (a.BundleID.Evidence != model.Inferred || a.MemoryUsage.Evidence != model.Inferred) {
			t.Fatal(a)
		}
		encoded, _ := json.Marshal(o.Event)
		if !strings.Contains(string(encoded), `"memory_usage"`) {
			t.Fatal(string(encoded))
		}
	}
}

func TestFocusedDestination(t *testing.T) {
	for _, method := range []string{"event_target_pid", "accessibility_focus", "unavailable"} {
		data := []byte(`{"key":"focus","time_ns":1,"pid":42,"app":"Code","target_method":"` + method + `","document":"file:///inactive.go","title":"inactive.go - Code","focus":{"path":[{"role":"AXTextArea","description":"Terminal 1, zsh"}],"selection":{"start":0,"length":0}}}`)
		o, err := decodeSnapshot(data, 1024)
		if err != nil {
			t.Fatal(err)
		}
		if method == "unavailable" {
			if o.Event.Destination.Focus != nil {
				t.Fatal("focus attached to unavailable target")
			}
			continue
		}
		f := o.Event.Destination.Focus
		if f == nil || f.Kind.Value != "text_input" || f.DocumentURI.Value != "" || f.Selection.Value == nil {
			t.Fatal(f)
		}
		encoded, err := json.Marshal(o.Event)
		if err != nil || !strings.Contains(string(encoded), `"focus"`) || !strings.Contains(string(encoded), `"start":0,"length":0`) {
			t.Fatalf("focus missing from event: %s %v", encoded, err)
		}
	}
}

func TestJetBrainsGenericToolWindowSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, pane := range []string{"Local Tool Window", "Respond to gre…roject-baseline Tool Window"} {
		data, err := json.Marshal(map[string]any{
			"key": "jetbrains-reworked", "time_ns": 1, "kind": "keyboard", "action": "cmd+v",
			"pid": 14698, "app": "IntelliJ IDEA", "bundle_id": "com.jetbrains.intellij", "target_method": "event_target_pid",
			"title": "project – open-file.sh", "document": "file:///project/open-file.sh",
			"focus": map[string]any{
				"path": []map[string]string{
					{"role": "AXTextArea", "role_description": "text entry area", "description": "Editor"},
					{"role": "AXScrollArea", "role_description": "scroll area"},
					{"role": "AXGroup", "role_description": "group", "description": pane},
					{"role": "AXGroup", "role_description": "group", "description": "Root Pane"},
				},
				"selection": map[string]int{"start": 985, "length": 0},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		o, err := decodeSnapshot(data, 1024)
		if err != nil {
			t.Fatal(err)
		}
		enrichDestination(ctx, &o.Event)
		f := o.Event.Destination.Focus
		if f.Kind.Value != "text_input" || f.DocumentURI.Value != "" {
			t.Fatalf("generic tool-window input falsely identified: %+v", f)
		}
		if len(o.Event.Destination.Contexts) != 0 {
			t.Fatal("ambiguous input attributed to an IDE child process")
		}
	}
}
