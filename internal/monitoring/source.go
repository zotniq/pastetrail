package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/zotniq/pastetrail/internal/model"
	"github.com/zotniq/pastetrail/internal/terminal"
)

type Source struct {
	Native   model.Source
	Receiver terminal.Receiver
	Limit    int
	Enrich   func(context.Context, *model.Event)
}

func (s *Source) Capabilities(ctx context.Context) []model.Capability {
	caps := s.Native.Capabilities(ctx)
	return append(caps, model.Capability{Feature: "terminal_delivery", Status: "partial", Reason: "relay sessions enrolled by daemon startup hooks or pastetrail shell; bracketed pastes observed directly, other text matched to native clipboard attempts; terminal payloads capped at 1 MiB; foreground metadata is sampled"})
}

type message struct {
	observation *model.Observation
	packet      *terminal.Packet
}
type stopped struct {
	native bool
	err    error
}

func (s *Source) Run(ctx context.Context, emit func(model.Observation) error) error {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer workers.Wait()
	defer cancel()
	incoming := make(chan message, 64)
	done := make(chan stopped, 2)
	workers.Add(2)
	go func() {
		defer workers.Done()
		err := s.Native.Run(ctx, func(o model.Observation) error {
			select {
			case incoming <- message{observation: &o}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		done <- stopped{native: true, err: err}
	}()
	go func() {
		defer workers.Done()
		err := s.Receiver.Run(ctx, func(p terminal.Packet) error {
			select {
			case incoming <- message{packet: &p}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		done <- stopped{err: err}
	}()
	tracker := terminal.NewTracker(s.Limit)
	if err := emit(tracker.Coverage()); err != nil {
		return err
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	deliver := func(observations []model.Observation) error {
		for _, o := range observations {
			if o.Event.Delivery != nil && s.Enrich != nil {
				s.Enrich(ctx, &o.Event)
			}
			if err := emit(o); err != nil {
				return err
			}
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case stop := <-done:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !stop.native {
				return fmt.Errorf("terminal receiver stopped: %w", stop.err)
			}
			if err := emit(model.Observation{Capabilities: []model.Capability{{Feature: "native_monitoring", Status: "unavailable", Reason: fmt.Sprintf("%v; terminal relay remains active", stop.err)}}}); err != nil {
				return err
			}
		case m := <-incoming:
			var observations []model.Observation
			now := time.Now()
			if m.observation != nil {
				observations = tracker.Native(*m.observation, now)
			} else {
				observations = tracker.Packet(*m.packet, now)
			}
			if err := deliver(observations); err != nil {
				return err
			}
		case now := <-tick.C:
			if err := deliver(tracker.Flush(now, false)); err != nil {
				return err
			}
		}
	}
}
