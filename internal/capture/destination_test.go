package capture

import (
	"context"
	"testing"

	"github.com/zotniq/pastetrail/internal/model"
)

func TestEnrichDestination(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, name := range []string{"fixture", "Terminal"} {
		e := model.Event{Destination: model.Destination{Application: model.Application{Name: model.Known(name)}}}
		enrichDestination(ctx, &e)
		if (len(e.Destination.Contexts) != 0) != (name == "Terminal") {
			t.Fatal(e.Destination)
		}
	}
}
