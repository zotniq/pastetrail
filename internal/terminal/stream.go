package terminal

import (
	"bytes"
	"time"
)

var pasteStart = []byte("\x1b[200~")
var pasteEnd = []byte("\x1b[201~")

type span struct {
	data  []byte
	at    time.Time
	group int
}

type receipt struct {
	session  Session
	data     []byte
	size     int
	at, end  time.Time
	group    int
	complete bool
}

type parser struct {
	limit        int
	active       bool
	pending      []byte
	pendingAt    time.Time
	pendingGroup int
	current      receipt
}

func (p *parser) feed(data []byte, at time.Time, group int) ([]receipt, []span) {
	var pastes []receipt
	var plain []span
	appendByte := func(b byte, when time.Time, pgid int) {
		if p.active {
			p.current.size++
			p.current.end = when
			if pgid != p.current.group {
				p.current.group = 0
			}
			if len(p.current.data) < p.limit {
				p.current.data = append(p.current.data, b)
			}
			return
		}
		if len(plain) == 0 || plain[len(plain)-1].group != pgid || !plain[len(plain)-1].at.Equal(when) {
			plain = append(plain, span{at: when, group: pgid})
		}
		i := len(plain) - 1
		plain[i].data = append(plain[i].data, b)
	}
	for _, b := range data {
		if len(p.pending) == 0 {
			p.pendingAt, p.pendingGroup = at, group
		}
		if p.pendingGroup != group {
			p.pendingGroup = 0
		}
		p.pending = append(p.pending, b)
		marker := pasteStart
		if p.active {
			marker = pasteEnd
		}
		for len(p.pending) > 0 && !bytes.HasPrefix(marker, p.pending) {
			appendByte(p.pending[0], p.pendingAt, p.pendingGroup)
			p.pending = p.pending[1:]
		}
		if !bytes.Equal(marker, p.pending) {
			continue
		}
		if p.active {
			p.current.end, p.current.complete = at, true
			if p.current.group != group {
				p.current.group = 0
			}
			pastes = append(pastes, p.current)
			p.current = receipt{}
		} else {
			p.current = receipt{at: p.pendingAt, end: at, group: p.pendingGroup}
		}
		p.active = !p.active
		p.pending = nil
	}
	return pastes, plain
}

func (p *parser) finish() *receipt {
	if !p.active {
		return nil
	}
	for _, b := range p.pending {
		p.current.size++
		if len(p.current.data) < p.limit {
			p.current.data = append(p.current.data, b)
		}
	}
	r := p.current
	*p = parser{limit: p.limit}
	return &r
}
