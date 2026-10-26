//go:build cgo && (darwin || linux)

package pty

/*
#cgo linux LDFLAGS: -lutil
#include <errno.h>
#include <fcntl.h>
#include <stdlib.h>
#include <termios.h>
#include <sys/ioctl.h>
#include <unistd.h>
#ifdef __APPLE__
#include <util.h>
#else
#include <pty.h>
#endif

static int pt_open(int *master, int *slave, int *outer, char *name, char *outer_name, struct termios **saved) {
    if (!isatty(STDIN_FILENO) || !isatty(STDOUT_FILENO)) return ENOTTY;
    int result = ttyname_r(STDIN_FILENO, outer_name, 1024);
    if (result != 0) return result;
    *outer = open(outer_name, O_RDWR | O_NOCTTY | O_NONBLOCK | O_CLOEXEC);
    if (*outer < 0) return errno;
    *saved = malloc(sizeof(struct termios));
    if (!*saved) return ENOMEM;
    if (tcgetattr(*outer, *saved) < 0) return errno;
    struct winsize size;
    if (ioctl(*outer, TIOCGWINSZ, &size) < 0) return errno;
    if (openpty(master, slave, name, *saved, &size) < 0) return errno;
    if (fcntl(*master, F_SETFD, FD_CLOEXEC) < 0 || fcntl(*slave, F_SETFD, FD_CLOEXEC) < 0) return errno;
    if (fcntl(*master, F_SETFL, O_NONBLOCK) < 0) return errno;
    return 0;
}

static int pt_raw(int fd, const struct termios *saved) {
    struct termios raw = *saved;
    cfmakeraw(&raw);
    return tcsetattr(fd, TCSANOW, &raw) == 0 ? 0 : errno;
}

static int pt_resize(int outer, int master) {
    struct winsize size;
    if (ioctl(outer, TIOCGWINSZ, &size) < 0) return errno;
    return ioctl(master, TIOCSWINSZ, &size) == 0 ? 0 : errno;
}
*/
import "C"

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

type Terminal struct {
	Master, Slave, Outer *os.File
	Name                 string
	saved                *C.struct_termios
	raw                  bool
	masterFD, outerFD    C.int
}

func CurrentName() string {
	var name [1024]C.char
	if C.ttyname_r(C.STDIN_FILENO, &name[0], C.size_t(len(name))) != 0 {
		return ""
	}
	return C.GoString(&name[0])
}

func Open() (*Terminal, error) {
	master, slave, outer := C.int(-1), C.int(-1), C.int(-1)
	var name, outerName [1024]C.char
	var saved *C.struct_termios
	status := C.pt_open(&master, &slave, &outer, &name[0], &outerName[0], &saved)
	if status != 0 {
		for _, fd := range []C.int{master, slave, outer} {
			if fd >= 0 {
				C.close(fd)
			}
		}
		C.free(unsafe.Pointer(saved))
		return nil, fmt.Errorf("open interactive terminal: %w", syscall.Errno(status))
	}
	return &Terminal{Master: os.NewFile(uintptr(master), "pty-master"), Slave: os.NewFile(uintptr(slave), C.GoString(&name[0])), Outer: os.NewFile(uintptr(outer), C.GoString(&outerName[0])), Name: C.GoString(&name[0]), saved: saved, masterFD: master, outerFD: outer}, nil
}

func (t *Terminal) Raw() error {
	if status := C.pt_raw(t.outerFD, t.saved); status != 0 {
		return syscall.Errno(status)
	}
	t.raw = true
	return nil
}

func (t *Terminal) Resize() { C.pt_resize(t.outerFD, t.masterFD) }

func (t *Terminal) Foreground() int { return int(C.tcgetpgrp(t.masterFD)) }

func (t *Terminal) Restore() {
	if t.raw {
		C.tcsetattr(t.outerFD, C.TCSANOW, t.saved)
		t.raw = false
	}
}

func (t *Terminal) Close() {
	t.Restore()
	t.Master.Close()
	t.Slave.Close()
	t.Outer.Close()
	C.free(unsafe.Pointer(t.saved))
}
