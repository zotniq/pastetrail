package terminal

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zotniq/pastetrail/internal/content"
	"github.com/zotniq/pastetrail/internal/model"
)

const settleTime = 750 * time.Millisecond
const maxPending = 64
const maxSessions = 32

type sessionState struct {
	session     Session
	parser      parser
	sequence    uint64
	plain       []span
	last        time.Time
	closed      bool
	connections int
}

type pendingNative struct {
	observation model.Observation
	deadline    time.Time
}

type pendingReceipt struct {
	receipt   receipt
	deadline  time.Time
	ambiguous bool
}

// Tracker correlates native attempts with bounded, transient terminal input.
type Tracker struct {
	limit       int
	sessions    map[string]*sessionState
	native      []pendingNative
	pastes      []pendingReceipt
	serial      uint64
	nativeLimit int
}

func NewTracker(limit int) *Tracker {
	return &Tracker{limit: max(1, min(limit, 1<<20)), nativeLimit: max(1, min(maxPending, (32<<20)/max(1, limit))), sessions: make(map[string]*sessionState)}
}

func gap(reason string) model.Observation {
	return model.Observation{Capabilities: []model.Capability{{Feature: "terminal_delivery", Status: "partial", Reason: reason}}}
}

func (t *Tracker) Coverage() model.Observation {
	connected := 0
	for _, s := range t.sessions {
		if s.connections > 0 {
			connected++
		}
	}
	status := "partial"
	reason := fmt.Sprintf("%d relay sessions connected; terminal attribution covers only those sessions", connected)
	if connected == 0 {
		status = "unavailable"
		reason = "no relay sessions connected; open a new zsh/bash terminal after starting the daemon; existing terminals are not attached and native paste attempts have no terminal delivery attribution"
	}
	return model.Observation{Capabilities: []model.Capability{{Feature: "terminal_sessions", Status: status, Reason: reason}}}
}

func (t *Tracker) Native(o model.Observation, now time.Time) []model.Observation {
	if o.Capabilities != nil {
		return []model.Observation{o}
	}
	var out []model.Observation
	if len(t.native) == t.nativeLimit {
		out = append(out, t.native[0].observation, gap("paste correlation queue full; an attempt was emitted without terminal attribution"))
		t.native = t.native[1:]
	}
	t.native = append(t.native, pendingNative{o, now.Add(settleTime)})
	return out
}

func (t *Tracker) Packet(packet Packet, now time.Time) []model.Observation {
	f, session := packet.Frame, packet.Session
	s := t.sessions[session.ID]
	var out []model.Observation
	if f.Type == "open" {
		if s == nil {
			for id, old := range t.sessions {
				if old.closed && now.Sub(old.last) > MatchWindow {
					delete(t.sessions, id)
				}
			}
			if len(t.sessions) >= maxSessions {
				return []model.Observation{gap("terminal session limit reached; this session is not monitored")}
			}
			s = &sessionState{session: session, parser: parser{limit: t.limit}, last: now}
			t.sessions[session.ID] = s
		}
		s.closed = false
		s.connections++
		return []model.Observation{t.Coverage()}
	}
	if s == nil {
		return nil
	}
	s.last = now
	if f.Type == "close" {
		s.connections--
		if s.connections > 0 {
			return nil
		}
		s.closed = true
		out = append(out, t.Coverage())
		if f.Sequence > s.sequence {
			s.parser = parser{limit: t.limit}
			s.plain = nil
			out = append(out, gap("terminal input was lost before the relay disconnected"))
		}
		if r := s.parser.finish(); r != nil {
			r.session = session
			out = append(out, t.addReceipt(*r, now)...)
		}
		s.parser = parser{limit: t.limit}
		s.plain = nil
		return out
	}
	if f.Type != "input" {
		return nil
	}
	if f.Sequence != s.sequence+1 {
		s.parser = parser{limit: t.limit}
		s.plain = nil
		out = append(out, gap("terminal input was lost during congestion or disconnection; paste parsing restarted"))
	}
	s.sequence = f.Sequence
	pastes, plain := s.parser.feed(f.Data, f.Time, f.ProcessGroup)
	if len(pastes) > 0 || s.parser.active {
		s.plain = nil
	} else {
		s.plain = append(s.plain, plain...)
	}
	prunePlain(s, now, min(t.limit, 256<<10))
	for _, r := range pastes {
		r.session = session
		out = append(out, t.addReceipt(r, now)...)
	}
	return out
}

func prunePlain(s *sessionState, now time.Time, limit int) {
	total := 0
	for _, p := range s.plain {
		total += len(p.data)
	}
	for len(s.plain) > 0 && (total > limit || now.Sub(s.plain[0].at) > MatchWindow) {
		total -= len(s.plain[0].data)
		s.plain[0] = span{}
		s.plain = s.plain[1:]
	}
}

func (t *Tracker) addReceipt(r receipt, now time.Time) []model.Observation {
	var out []model.Observation
	if len(t.pastes) == maxPending {
		out = append(out, t.observation(t.pastes[0].receipt), gap("terminal correlation queue full; receipt emitted without a native trigger"))
		t.pastes = t.pastes[1:]
	}
	t.pastes = append(t.pastes, pendingReceipt{receipt: r, deadline: now.Add(settleTime)})
	return out
}

func (t *Tracker) Flush(now time.Time, force bool) []model.Observation {
	var out []model.Observation
	for len(t.native) > 0 && (force || !now.Before(t.native[0].deadline)) {
		n := t.native[0]
		e := &n.observation.Event
		text := clipboardText(e.Content)
		matches, index := 0, -1
		var selected receipt
		if len(text) > 0 {
			for i := range t.pastes {
				p := &t.pastes[i]
				r := p.receipt
				if t.nativeMatches(r) > 1 {
					p.ambiguous = true
				}
				if r.complete && r.size == len(r.data) && related(e, r.session, r.at, r.end) && normalized(text) == normalized(r.data) {
					matches++
					index = i
					selected = r
					if p.ambiguous {
						matches++
					}
				}
			}
			for _, s := range t.sessions {
				for _, r := range plainMatches(s, e, text) {
					matches++
					selected = r
					if t.nativeMatches(r) > 1 {
						matches++
						s.plain = nil
					}
				}
			}
		}
		if matches == 1 {
			applyReceipt(e, selected, "matched clipboard text, input time, and process ancestry")
			if index >= 0 {
				t.pastes = slices.Delete(t.pastes, index, index+1)
			} else if s := t.sessions[selected.session.ID]; s != nil {
				s.plain = nil
			}
		}
		out = append(out, n.observation)
		t.native[0] = pendingNative{}
		t.native = t.native[1:]
	}
	kept := t.pastes[:0]
	for _, p := range t.pastes {
		// A native attempt already waiting for settlement may still match this receipt.
		waiting := false
		for _, n := range t.native {
			if related(&n.observation.Event, p.receipt.session, p.receipt.at, p.receipt.end) {
				waiting = true
				break
			}
		}
		if force || (!waiting && !now.Before(p.deadline)) {
			out = append(out, t.observation(p.receipt))
		} else {
			kept = append(kept, p)
		}
	}
	clear(t.pastes[len(kept):])
	t.pastes = kept
	for id, s := range t.sessions {
		prunePlain(s, now, min(t.limit, 256<<10))
		if s.closed && now.Sub(s.last) > MatchWindow {
			delete(t.sessions, id)
		}
	}
	return out
}

func (t *Tracker) nativeMatches(r receipt) int {
	count := 0
	for _, n := range t.native {
		e := &n.observation.Event
		if related(e, r.session, r.at, r.end) && normalized(clipboardText(e.Content)) == normalized(r.data) {
			count++
		}
	}
	return count
}

func related(e *model.Event, s Session, start, end time.Time) bool {
	pid := e.Destination.Application.PID.Value
	return pid > 0 && slices.Contains(s.Ancestors, pid) &&
		!start.Before(e.Time.Add(-250*time.Millisecond)) && !end.After(e.Time.Add(MatchWindow))
}

func clipboardText(c model.Content) []byte {
	for _, r := range c.Representations {
		if r.MediaType != "text/plain" || r.Truncated {
			continue
		}
		if r.Encoding == "base64" {
			b, _ := base64.StdEncoding.DecodeString(r.Data)
			return b
		}
		return []byte(r.Data)
	}
	return nil
}

func normalized(b []byte) string {
	return strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\r", "\n")
}

func plainMatches(s *sessionState, e *model.Event, text []byte) []receipt {
	var found []receipt
	variants := [][]byte{text, []byte(strings.ReplaceAll(normalized(text), "\n", "\r")), []byte(strings.ReplaceAll(normalized(text), "\n", "\r\n"))}
	for start := 0; start < len(s.plain); {
		end := start + 1
		for end < len(s.plain) && s.plain[end].group == s.plain[start].group {
			end++
		}
		var data []byte
		for _, p := range s.plain[start:end] {
			data = append(data, p.data...)
		}
		seen := make(map[int]bool)
		for _, pattern := range variants {
			for offset := 0; offset < len(data); {
				i := bytes.Index(data[offset:], pattern)
				if i < 0 {
					break
				}
				i += offset
				offset = i + max(1, len(pattern))
				if seen[i] {
					continue
				}
				seen[i] = true
				first, last, pos := start, start, 0
				for j := start; j < end; j++ {
					next := pos + len(s.plain[j].data)
					if i >= pos && i < next {
						first = j
					}
					if i+len(pattern)-1 >= pos && i+len(pattern)-1 < next {
						last = j
					}
					pos = next
				}
				if related(e, s.session, s.plain[first].at, s.plain[last].at) {
					found = append(found, receipt{session: s.session, data: bytes.Clone(data[i : i+len(pattern)]), size: len(pattern), at: s.plain[first].at, end: s.plain[last].at, group: s.plain[first].group, complete: true})
					if len(found) == 2 {
						return found
					}
				}
			}
		}
		start = end
	}
	return found
}

func (t *Tracker) observation(r receipt) model.Observation {
	t.serial++
	e := model.Event{Time: r.at, CapturedAt: r.end, Trigger: model.Trigger{Kind: "terminal", Action: "bracketed_paste", Evidence: model.Observed, Source: "terminal_input", Reason: "bracketed paste protocol observed; input origin is not exposed"}}
	e.Destination.Application = r.session.Application
	applyReceipt(&e, r, "")
	e.Content = e.Delivery.Content
	return model.Observation{Key: fmt.Sprintf("pty:%s:%d", r.session.ID, t.serial), Event: e}
}

func applyReceipt(e *model.Event, r receipt, correlation string) {
	rep := content.TextRepresentation("text/plain", r.data, len(r.data))
	rep.Size, rep.Truncated = r.size, r.size > len(r.data)
	observed := model.Content{Evidence: model.Observed, Representations: []model.Representation{rep}, Incomplete: rep.Truncated || !r.complete}
	if !r.complete {
		observed.Reason = "terminal disconnected before the closing paste marker"
	}
	e.Delivery = &model.Delivery{Transport: "pty", Evidence: model.Observed, Bytes: r.size, Content: observed, Reason: "bytes forwarded to the session PTY; application consumption is not confirmed", Correlation: model.Missing[string]("no unique matching native paste trigger")}
	if correlation != "" {
		e.Delivery.Correlation = model.Field[string]{Value: "native_paste", Evidence: model.Inferred, Reason: correlation}
	}
	e.Destination.Browser = nil
	if e.Destination.Focus == nil {
		e.Destination.Focus = &model.Focus{}
	}
	e.Destination.Focus.Kind = model.Known("terminal")
	e.Destination.Focus.DocumentURI = model.Missing[string]("terminal input has no document URI")
	e.Destination.Focus.Selection = model.Missing[*model.TextRange]("terminal selection is not a document position")
	group := model.Missing[int]("foreground process group changed or could not be read during forwarding")
	if r.group > 0 {
		group = model.Known(r.group)
	}
	e.Destination.Contexts = []model.Context{
		{Kind: "terminal", Name: model.Known("PTY relay"), PID: model.Known(r.session.RelayPID), Host: model.Known(r.session.Host), TTY: model.Known(r.session.TTY), Session: model.Known(r.session.ID), ProcessGroup: group},
		{Kind: "shell", PID: model.Known(r.session.ShellPID), Host: model.Known(r.session.Host), TTY: model.Known(r.session.TTY), Session: model.Known(r.session.ID)},
	}
}
