//go:build cgo && (darwin || linux)

package enrollment

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/zotniq/pastetrail/internal/model"
	"github.com/zotniq/pastetrail/internal/shellquote"
	"github.com/zotniq/pastetrail/internal/terminalipc"
)

const marker = "# >>> PasteTrail temporary terminal capture"
const endMarker = "# <<< PasteTrail temporary terminal capture"

type edit struct {
	Path    string
	Block   string
	Created bool
}
type state struct {
	Token  string
	Binary string
	Socket string
	Edits  []edit
}
type Lease struct {
	path  string
	lock  *os.File
	state state
}

func Enable(socket string) (io.Closer, model.Capability, error) {
	if socket == "" {
		var err error
		socket, err = terminalipc.DefaultPath()
		if err != nil {
			return nil, model.Capability{}, err
		}
	}
	socket, err := filepath.Abs(socket)
	if err != nil {
		return nil, model.Capability{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, model.Capability{}, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, model.Capability{}, err
	}
	dir := filepath.Join(cache, "pastetrail", "startup")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, model.Capability{}, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, model.Capability{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm()&0077 != 0 || stat.Uid != uint32(os.Getuid()) {
		return nil, model.Capability{}, errors.New("shell activation directory must be private and owned by this user")
	}
	path := filepath.Join(dir, "state.json")
	fd, err := syscall.Open(path+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, model.Capability{}, err
	}
	lock := os.NewFile(uintptr(fd), path+".lock")
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, model.Capability{}, errors.New("another daemon owns automatic shell activation for this user")
	}
	lease := &Lease{path: path, lock: lock}
	fail := func(err error) (io.Closer, model.Capability, error) {
		return nil, model.Capability{}, errors.Join(err, lease.Close())
	}
	if data, _, readErr := readFile(path); readErr == nil {
		if err = json.Unmarshal(data, &lease.state); err != nil {
			lock.Close()
			return nil, model.Capability{}, fmt.Errorf("invalid shell recovery state: %w", err)
		}
		if err = lease.restore(); err != nil {
			lock.Close()
			return nil, model.Capability{}, err
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		lock.Close()
		return nil, model.Capability{}, readErr
	}
	binary, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return fail(err)
	}
	lease.state = state{Token: hex.EncodeToString(token[:]), Binary: binary, Socket: socket}
	if err = lease.save(); err != nil {
		return fail(err)
	}
	paths, err := startupPaths(home)
	if err != nil {
		return fail(err)
	}
	seen := map[string]bool{}
	for _, p := range paths {
		startup := p
		if _, err := os.Lstat(p); err == nil {
			p, err = filepath.EvalSymlinks(p)
			if err != nil {
				return fail(err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
		if integrationPath(p) {
			return fail(fmt.Errorf("refusing to edit application integration file %s", p))
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		data, mode, err := readFile(p)
		created := errors.Is(err, os.ErrNotExist)
		if err != nil && !created {
			return fail(err)
		}
		if created {
			mode = 0600
		}
		if bytes.Contains(data, []byte(marker)) {
			return fail(fmt.Errorf("unrecognized PasteTrail hook in %s; refusing to overwrite it", p))
		}
		block := lease.block(startup)
		lease.state.Edits = append(lease.state.Edits, edit{Path: p, Block: block, Created: created})
		if err = lease.save(); err != nil {
			return fail(err)
		}
		if err = replaceConfig(p, data, append([]byte(block), data...), mode, created); err != nil {
			return fail(err)
		}
	}
	cap := model.Capability{Feature: "terminal_activation", Status: "partial", Reason: "temporary zsh/bash startup hooks installed; new interactive sessions can join while this daemon runs; existing terminals and shells that skip these files remain uncovered"}
	return lease, cap, nil
}

func (l *Lease) block(startup string) string {
	q := shellquote.Quote
	return marker + "\n" +
		"if [ -n \"${ZSH_VERSION-}${BASH_VERSION-}\" ]; then\n" +
		"  case $- in *i*)\n" +
		"    if [ -t 0 ] && [ -t 1 ] && [ -x " + q(l.state.Binary) + " ] && [ -S " + q(l.state.Socket) + " ]; then\n" +
		"      builtin eval \"$(" + q(l.state.Binary) + " _prepare-shell --state " + q(l.path) + " --token " + q(l.state.Token) + " --pid \"$$\" --startup " + q(startup) + " --zdotdir \"${VSCODE_ZDOTDIR-${ZDOTDIR-}}\")\"\n" +
		"    fi\n" +
		"  ;; esac\n" +
		"fi\n" + endMarker + "\n"
}

func (l *Lease) save() error {
	data, err := json.Marshal(l.state)
	if err != nil {
		return err
	}
	return replace(l.path, data, 0600)
}

func (l *Lease) restore() error {
	var failures []error
	for _, e := range l.state.Edits {
		if !strings.HasPrefix(e.Block, marker+"\n") || !strings.HasSuffix(e.Block, endMarker+"\n") || !filepath.IsAbs(e.Path) {
			failures = append(failures, errors.New("invalid shell recovery entry"))
			continue
		}
		data, mode, err := readFile(e.Path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !bytes.Contains(data, []byte(e.Block)) {
			if bytes.Contains(data, []byte(marker)) {
				failures = append(failures, fmt.Errorf("hook edited in %s; preserving file for manual recovery", e.Path))
			}
			continue
		}
		remaining := bytes.Replace(data, []byte(e.Block), nil, 1)
		if e.Created && len(remaining) == 0 {
			if err = unchanged(e.Path, data, mode, false); err == nil {
				err = os.Remove(e.Path)
			}
		} else {
			err = replaceConfig(e.Path, data, remaining, mode, false)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (l *Lease) Close() error {
	if l.lock == nil {
		return nil
	}
	err := l.restore()
	if err == nil {
		if removeErr := os.Remove(l.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = removeErr
		}
	}
	err = errors.Join(err, l.lock.Close())
	l.lock = nil
	return err
}

// Active requires both the current activation token and a live daemon's lease.
func Active(path, token string) (string, error) {
	data, _, err := readFile(path)
	if err != nil {
		return "", err
	}
	var s state
	if err = json.Unmarshal(data, &s); err != nil {
		return "", err
	}
	if token == "" || s.Token != token {
		return "", errors.New("shell activation has expired")
	}
	fd, err := syscall.Open(path+".lock", syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer syscall.Close(fd)
	err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return "", errors.New("daemon is not running")
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		return "", err
	}
	return s.Socket, nil
}
