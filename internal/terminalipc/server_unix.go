//go:build darwin || linux

package terminalipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/zotniq/pastetrail/internal/terminal"
)

const Supported = true

type Server struct{ Path string }

func listen(path string) (*net.UnixListener, *os.File, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	if len(path) > 100 {
		return nil, nil, errors.New("terminal socket path exceeds 100 bytes")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm()&0077 != 0 || stat.Uid != uint32(os.Getuid()) {
		return nil, nil, errors.New("terminal socket directory must be owned by this user with mode 0700")
	}
	fd, err := syscall.Open(path+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, nil, err
	}
	lock := os.NewFile(uintptr(fd), path+".lock")
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, nil, fmt.Errorf("another PasteTrail daemon owns this socket: %w", err)
	}
	fail := func(err error) (*net.UnixListener, *os.File, error) { lock.Close(); return nil, nil, err }
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fail(errors.New("terminal socket path exists and is not a socket"))
		}
		conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return fail(errors.New("terminal socket is already in use"))
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			return fail(err)
		}
		if err := os.Remove(path); err != nil {
			return fail(err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return fail(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return fail(err)
	}
	return listener, lock, nil
}

func (s Server) Run(ctx context.Context, emit func(terminal.Packet) error) error {
	listener, lock, err := listen(s.Path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); listener.Close(); workers.Wait(); lock.Close() }()
	stopListener := context.AfterFunc(ctx, func() { listener.Close() })
	defer stopListener()
	packets := make(chan terminal.Packet, 64)
	acceptError := make(chan error, 1)
	slots := make(chan struct{}, 32)
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				acceptError <- err
				return
			}
			select {
			case slots <- struct{}{}:
			default:
				conn.Close()
				continue
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-slots }()
				defer conn.Close()
				stop := context.AfterFunc(ctx, func() { conn.Close() })
				defer stop()
				s.read(ctx, conn, packets)
			}()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-acceptError:
			return err
		case packet := <-packets:
			if err := emit(packet); err != nil {
				return err
			}
		}
	}
}

func (s Server) read(ctx context.Context, conn *net.UnixConn, packets chan<- terminal.Packet) {
	scan := bufio.NewScanner(conn)
	scan.Buffer(make([]byte, 4096), 64<<10)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if !scan.Scan() {
		return
	}
	var f terminal.Frame
	if json.Unmarshal(scan.Bytes(), &f) != nil || f.Version != terminal.ProtocolVersion || f.Type != "open" || !validSession(f.Session) {
		return
	}
	session := *f.Session
	send := func(frame terminal.Frame) bool {
		select {
		case packets <- terminal.Packet{Session: session, Frame: frame}:
			return true
		case <-ctx.Done():
			return false
		}
	}
	if !send(f) {
		return
	}
	closed := terminal.Frame{Type: "close"}
	defer func() { send(closed) }()
	conn.SetWriteDeadline(time.Now().Add(time.Second))
	if json.NewEncoder(conn).Encode(terminal.Frame{Version: terminal.ProtocolVersion, Type: "ready"}) != nil {
		return
	}
	conn.SetReadDeadline(time.Time{})
	for scan.Scan() {
		f = terminal.Frame{}
		if json.Unmarshal(scan.Bytes(), &f) != nil || f.Version != terminal.ProtocolVersion {
			return
		}
		if f.Type == "close" {
			closed.Sequence = f.Sequence
			return
		}
		if f.Type == "ping" {
			if writeFrame(conn, terminal.Frame{Type: "pong"}) != nil {
				return
			}
			continue
		}
		if f.Type != "input" || f.Sequence == 0 || len(f.Data) == 0 || len(f.Data) > terminal.MaxInputBytes || f.Time.IsZero() || f.Time.After(time.Now().Add(time.Second)) {
			return
		}
		if !send(f) {
			return
		}
	}
}

func validSession(s *terminal.Session) bool {
	return s != nil && len(s.ID) == 32 && len(s.Ancestors) <= 64 && len(s.TTY) < 256 && strings.HasPrefix(s.TTY, "/dev/") && !strings.Contains(s.TTY, "..") && s.RelayPID > 0 && s.ShellPID > 0 && len(s.Host) < 256
}
