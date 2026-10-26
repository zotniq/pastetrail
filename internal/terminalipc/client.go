package terminalipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/zotniq/pastetrail/internal/terminal"
)

type queuedInput struct {
	connection net.Conn
	frame      terminal.Frame
}

type Client struct {
	path     string
	initial  net.Conn
	queue    chan queuedInput
	done     chan struct{}
	cancel   context.CancelFunc
	mu       sync.Mutex
	active   net.Conn
	activeAt time.Time
	sequence uint64
	dropped  uint64
}

func Connect(ctx context.Context, path string) (*Client, error) {
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("connect to PasteTrail daemon at %s: %w", path, err)
	}
	return &Client{path: path, initial: conn, queue: make(chan queuedInput, 64), done: make(chan struct{})}, nil
}

func writeFrame(conn net.Conn, frame terminal.Frame) error {
	frame.Version = terminal.ProtocolVersion
	if err := conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		return err
	}
	return json.NewEncoder(conn).Encode(frame)
}

func (c *Client) Start(session terminal.Session, warn func(error)) {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	ready := make(chan struct{})
	go func() {
		defer close(c.done)
		first := true
		defer func() {
			if first {
				close(ready)
			}
		}()
		conn := c.initial
		paused := false
		for ctx.Err() == nil {
			var err error
			if conn == nil {
				conn, err = (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(ctx, "unix", c.path)
			}
			var decoder *json.Decoder
			if err == nil {
				err = writeFrame(conn, terminal.Frame{Type: "open", Session: &session})
				if err == nil {
					conn.SetReadDeadline(time.Now().Add(time.Second))
					decoder = json.NewDecoder(conn)
					var ack terminal.Frame
					err = decoder.Decode(&ack)
					if err == nil && (ack.Version != terminal.ProtocolVersion || ack.Type != "ready") {
						err = fmt.Errorf("invalid terminal receiver acknowledgement")
					}
				}
			}
			if err == nil {
				c.mu.Lock()
				c.active = conn
				c.activeAt = time.Now()
				c.mu.Unlock()
			}
			if first {
				close(ready)
				first = false
			}
			if err == nil {
				paused = false
				finished := c.serve(ctx, conn, decoder, warn)
				conn = nil
				if finished {
					return
				}
			} else if conn != nil {
				conn.Close()
				conn = nil
			}
			if !paused {
				warn(fmt.Errorf("terminal capture paused; shell continues without buffering input"))
				paused = true
			}
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case item, ok := <-c.queue:
				timer.Stop()
				if !ok {
					return
				}
				clear(item.frame.Data)
			case <-timer.C:
			}
		}
	}()
	<-ready
}

func (c *Client) serve(ctx context.Context, conn net.Conn, decoder *json.Decoder, warn func(error)) bool {
	disconnected := make(chan struct{})
	go func() {
		defer close(disconnected)
		defer c.pause(conn)
		for {
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			var frame terminal.Frame
			if decoder.Decode(&frame) != nil || frame.Version != terminal.ProtocolVersion || frame.Type != "pong" {
				return
			}
		}
	}()
	defer func() { c.pause(conn); conn.Close(); <-disconnected }()
	heartbeat := time.NewTicker(time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return true
		case <-disconnected:
			return false
		case <-heartbeat.C:
			if writeFrame(conn, terminal.Frame{Type: "ping"}) != nil {
				return false
			}
		case item, ok := <-c.queue:
			if !ok {
				c.mu.Lock()
				sequence, dropped := c.sequence, c.dropped
				c.dropped = 0
				c.mu.Unlock()
				if dropped > 0 {
					warn(fmt.Errorf("terminal monitor dropped %d input chunks", dropped))
				}
				_ = writeFrame(conn, terminal.Frame{Type: "close", Sequence: sequence})
				return true
			}
			c.mu.Lock()
			active, dropped := c.active == conn, c.dropped
			c.dropped = 0
			c.mu.Unlock()
			if dropped > 0 {
				warn(fmt.Errorf("terminal monitor dropped %d input chunks", dropped))
			}
			if !active || item.connection != conn || time.Since(item.frame.Time) > terminal.MatchWindow {
				clear(item.frame.Data)
				continue
			}
			err := writeFrame(conn, item.frame)
			clear(item.frame.Data)
			if err != nil {
				return false
			}
		}
	}
}

func (c *Client) pause(conn net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != conn {
		return
	}
	c.active = nil
	for {
		select {
		case item, ok := <-c.queue:
			if !ok {
				return
			}
			clear(item.frame.Data)
		default:
			return
		}
	}
}

func (c *Client) Input(at time.Time, group int, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil || at.Before(c.activeAt) {
		return
	}
	c.sequence++
	frame := terminal.Frame{Type: "input", Sequence: c.sequence, Time: at, ProcessGroup: group, Data: append([]byte(nil), data...)}
	select {
	case c.queue <- queuedInput{connection: c.active, frame: frame}:
	default:
		clear(frame.Data)
		c.dropped++
	}
}

func (c *Client) Close() {
	if c.cancel == nil {
		c.initial.Close()
		return
	}
	close(c.queue)
	select {
	case <-c.done:
	case <-time.After(500 * time.Millisecond):
		c.cancel()
		<-c.done
	}
	c.cancel()
}
