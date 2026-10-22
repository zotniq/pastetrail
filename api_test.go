package pastetrail_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zotniq/pastetrail"
)

type publicSource struct{}

func (publicSource) Capabilities(context.Context) []pastetrail.Capability { return nil }
func (publicSource) Run(_ context.Context, emit func(pastetrail.Observation) error) error {
	return emit(pastetrail.Observation{Event: pastetrail.Event{
		Destination: pastetrail.Destination{Application: pastetrail.Application{
			Name: pastetrail.Known("fixture"), PID: pastetrail.Missing[int]("fixture PID unavailable"),
		}},
	}})
}

func TestPublicAPI(t *testing.T) {
	var source pastetrail.Source = publicSource{}
	var field pastetrail.Field[string] = pastetrail.Known("fixture")
	if field.Evidence != pastetrail.Observed || pastetrail.Missing[int]("missing").Evidence != pastetrail.Unavailable {
		t.Fatal("field constructors changed")
	}
	file := pastetrail.FileReference("https://example.com/fixture.txt")
	if file.Name != "fixture.txt" || file.URI != "https://example.com/fixture.txt" || file.Size != nil {
		t.Fatal(file)
	}
	monitor, err := pastetrail.NewWithSource(source, pastetrail.Options{})
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	if err := monitor.Run(context.Background(), func(record pastetrail.Record) error {
		if record.Event == nil {
			return nil
		}
		seen = true
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		for _, want := range []string{`"schema_version":1`, `"type":"paste_attempt"`, `"value":"fixture","evidence":"observed"`, `"reason":"fixture PID unavailable"`} {
			if !strings.Contains(string(data), want) {
				t.Errorf("public JSON is missing %s", want)
			}
		}
		// Public callers must still be able to compare sentinel errors.
		if err := monitor.Run(context.Background(), func(pastetrail.Record) error { return nil }); !errors.Is(err, pastetrail.ErrRunning) {
			t.Errorf("running error identity changed: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("custom source did not deliver an event")
	}
}
