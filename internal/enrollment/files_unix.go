//go:build cgo && (darwin || linux)

package enrollment

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const maxFileSize = 1 << 20

func readFile(path string) ([]byte, os.FileMode, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, 0, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 || stat.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0022 != 0 {
		return nil, 0, fmt.Errorf("refusing to edit %s: expected a regular, singly linked file owned and writable only by this user", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err == nil && len(data) > maxFileSize {
		err = fmt.Errorf("%s exceeds shell configuration size limit", path)
	}
	return data, info.Mode().Perm(), err
}

func unchanged(path string, before []byte, mode os.FileMode, created bool) error {
	current, currentMode, err := readFile(path)
	if created && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if created || !bytes.Equal(before, current) || mode != currentMode {
		return fmt.Errorf("shell configuration changed during update: %s; preserving the current file", path)
	}
	return nil
}

func replaceConfig(path string, before, after []byte, mode os.FileMode, created bool) error {
	return replaceChecked(path, after, mode, func() error { return unchanged(path, before, mode, created) })
}

func replace(path string, data []byte, mode os.FileMode) error {
	return replaceChecked(path, data, mode, nil)
}

func replaceChecked(path string, data []byte, mode os.FileMode, check func() error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pastetrail-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	return os.Rename(f.Name(), path)
}
