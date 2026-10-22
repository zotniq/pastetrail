package capture

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/zotniq/pastetrail/internal/model"
)

func TestUnavailableSource(t *testing.T) {
	s := unavailableSource{reason: "test"}
	if !reflect.DeepEqual(s.Capabilities(context.Background()), []model.Capability{{Feature: "monitoring", Status: "unavailable", Reason: "test"}}) {
		t.Fatal("capabilities")
	}
	if !errors.Is(s.Run(context.Background(), nil), model.ErrUnsupported) {
		t.Fatal("missing unsupported error")
	}
}
