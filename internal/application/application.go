package application

import "github.com/zotniq/pastetrail/internal/model"

// Snapshot contains native application and window attributes before provenance is applied.
type Snapshot struct {
	PID           int                 `json:"pid"`
	App           string              `json:"app"`
	Executable    string              `json:"executable"`
	Window        string              `json:"window"`
	Title         string              `json:"title"`
	TargetMethod  string              `json:"target_method"`
	BundleID      string              `json:"bundle_id"`
	BundlePath    string              `json:"bundle_path"`
	Bounds        *model.WindowBounds `json:"bounds"`
	ContentBounds *model.WindowBounds `json:"content_bounds"`
	MemoryUsage   *uint64             `json:"memory_usage"`
	WindowMethod  string              `json:"window_method"`
}

// FromSnapshot preserves the evidence for the process attributed to this input event.
func FromSnapshot(n Snapshot) model.Application {
	a := model.Application{}
	if n.PID > 0 {
		a.PID = model.Known(n.PID)
	}
	if n.App != "" {
		a.Name = model.Known(n.App)
	}
	if n.Executable != "" {
		a.Executable = model.Known(n.Executable)
	}
	if n.Window != "" {
		a.WindowID = model.Known(n.Window)
	}
	if n.Title != "" {
		a.WindowTitle = model.Known(n.Title)
	}
	if n.BundleID != "" {
		a.BundleID = model.Known(n.BundleID)
	}
	if n.BundlePath != "" {
		a.BundlePath = model.Known(n.BundlePath)
	}
	if n.Bounds != nil {
		a.Bounds = model.Known(n.Bounds)
	}
	if n.ContentBounds != nil {
		a.ContentBounds = model.Known(n.ContentBounds)
	}
	if n.MemoryUsage != nil {
		a.MemoryUsage = model.Known(*n.MemoryUsage)
	}
	if n.WindowMethod == "frontmost_window_for_event_pid" {
		reason := "frontmost visible window belonging to the input event target PID"
		a.WindowID.Evidence, a.WindowID.Reason = model.Inferred, reason
		a.Bounds.Evidence, a.Bounds.Reason = model.Inferred, reason
	}
	applyTargetEvidence(&a, n.TargetMethod)
	return a
}

func applyTargetEvidence(a *model.Application, method string) {
	switch method {
	case "event_target_pid":
		a.PID.Reason = "Core Graphics event target process ID"
	case "accessibility_focus":
		reason := "event target PID unavailable; focused application sampled through Accessibility after input"
		a.PID.Evidence, a.PID.Reason = model.Inferred, reason
		for _, f := range []*model.Field[string]{&a.Name, &a.Executable, &a.WindowID, &a.WindowTitle, &a.BundleID, &a.BundlePath} {
			if f.Evidence != "" {
				f.Evidence, f.Reason = model.Inferred, reason
			}
		}
		if a.Bounds.Value != nil {
			a.Bounds.Evidence, a.Bounds.Reason = model.Inferred, reason
		}
		if a.ContentBounds.Value != nil {
			a.ContentBounds.Evidence, a.ContentBounds.Reason = model.Inferred, reason
		}
		if a.MemoryUsage.Evidence != "" {
			a.MemoryUsage.Evidence, a.MemoryUsage.Reason = model.Inferred, reason
		}
	case "unavailable":
		reason := "event target PID and live Accessibility focus are unavailable; no cached application was used"
		*a = model.Application{
			PID: model.Missing[int](reason), Name: model.Missing[string](reason),
			Executable: model.Missing[string](reason), WindowID: model.Missing[string](reason), WindowTitle: model.Missing[string](reason),
		}
	}
}
