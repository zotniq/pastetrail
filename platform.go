package pastetrail

import "context"

type unavailableSource struct{ reason string }

func (s unavailableSource) Capabilities(context.Context) []Capability {
	return []Capability{{Feature: "monitoring", Status: "unavailable", Reason: s.reason}}
}

func (s unavailableSource) Run(context.Context, func(Observation) error) error { return ErrUnsupported }

func newPlatformSource(Options) Source {
	return unavailableSource{reason: "native monitoring is not included in this build"}
}
