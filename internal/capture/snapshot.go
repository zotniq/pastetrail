package capture

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zotniq/pastetrail/internal/application"
	"github.com/zotniq/pastetrail/internal/browser"
	"github.com/zotniq/pastetrail/internal/content"
	"github.com/zotniq/pastetrail/internal/focus"
	"github.com/zotniq/pastetrail/internal/model"
)

type nativeSnapshot struct {
	Key           string   `json:"key"`
	TimeNS        int64    `json:"time_ns"`
	CapturedNS    int64    `json:"captured_ns"`
	Action        string   `json:"action"`
	Kind          string   `json:"kind"`
	Automated     *bool    `json:"automated"`
	Source        string   `json:"source"`
	TriggerReason string   `json:"trigger_reason"`
	Button        int      `json:"button"`
	Clicks        int      `json:"clicks"`
	Modifiers     []string `json:"modifiers"`
	application.Snapshot
	Focus      *focus.Snapshot `json:"focus"`
	Document   string          `json:"document"`
	BrowserURL string          `json:"browser_url"`
	Formats    []struct {
		Type      string `json:"type"`
		Data      []byte `json:"data"`
		Size      int    `json:"size"`
		Truncated bool   `json:"truncated"`
	} `json:"formats"`
	Files   []string `json:"files"`
	Issue   string   `json:"issue"`
	Dropped uint64   `json:"dropped"`
}

type decodedObservation struct {
	model.Observation
	dropped uint64
}

func decodeSnapshot(data []byte, limit int) (decodedObservation, error) {
	var n nativeSnapshot
	if err := json.Unmarshal(data, &n); err != nil {
		return decodedObservation{}, fmt.Errorf("decode native event: %w", err)
	}
	if n.Key == "" || n.TimeNS <= 0 {
		return decodedObservation{}, errors.New("native event is missing identity or capture time")
	}
	e := model.Event{Time: time.Unix(0, n.TimeNS), CapturedAt: time.Unix(0, n.CapturedNS), Trigger: model.Trigger{Kind: n.Kind, Action: n.Action, Evidence: model.Inferred, Source: n.Source}, Content: model.Content{Evidence: model.Observed}}
	e.Trigger.Reason, e.Trigger.Button, e.Trigger.ClickCount, e.Trigger.Modifiers = n.TriggerReason, n.Button, n.Clicks, n.Modifiers
	if n.CapturedNS <= 0 {
		e.CapturedAt = e.Time
	}
	if n.Automated != nil {
		e.Trigger.Automated = model.Known(*n.Automated)
	}
	if n.Issue != "" {
		e.Content.Incomplete = true
		e.Content.Reason = n.Issue
	}
	for _, r := range n.Formats {
		media := content.RichMediaType(r.Type)
		if media == "" {
			continue
		}
		rep := content.TextRepresentation(media, r.Data, limit)
		if r.Size > rep.Size {
			rep.Size = r.Size
		}
		rep.Truncated = rep.Truncated || r.Truncated
		e.Content.Representations = append(e.Content.Representations, rep)
	}
	for _, f := range n.Files {
		e.Content.Files = append(e.Content.Files, content.FileReference(f))
	}
	if len(e.Content.Files) == 0 && len(e.Content.Representations) == 0 {
		e.Content.Evidence = model.Unavailable
		if e.Content.Reason == "" {
			e.Content.Reason = "no supported clipboard representation was available"
		}
	}
	e.Destination.Application = application.FromSnapshot(n.Snapshot)
	if n.TargetMethod != "unavailable" {
		e.Destination.Focus = focus.FromSnapshot(n.Focus)
	}
	e.Destination.Browser = browser.FromApplication(e.Destination.Application, n.Title, n.Document, n.BrowserURL)
	return decodedObservation{Observation: model.Observation{Key: n.Key, Event: e}, dropped: n.Dropped}, nil
}
