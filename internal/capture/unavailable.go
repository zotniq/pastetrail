package capture

import (
	"context"

	"github.com/zotniq/pastetrail/internal/model"
)

type unavailableSource struct{ reason string }

func (s unavailableSource) Capabilities(context.Context) []model.Capability {
	return []model.Capability{{Feature: "monitoring", Status: "unavailable", Reason: s.reason}}
}

func (s unavailableSource) Run(context.Context, func(model.Observation) error) error {
	return model.ErrUnsupported
}
