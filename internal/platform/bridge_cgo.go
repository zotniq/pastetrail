//go:build cgo && (darwin || windows || linux)

package platform

/*
#cgo darwin LDFLAGS: -framework AppKit -framework ApplicationServices
#cgo linux LDFLAGS: -lxcb -lxcb-record
#cgo windows LDFLAGS: -luser32 -lshell32 -lkernel32 -lpsapi
#include "native.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

type Bridge struct{}

func (Bridge) Probe() string { return C.GoString(C.pt_probe()) }
func (Bridge) Open(limit int) error {
	if C.pt_open(C.int(limit)) == 0 {
		return errors.New(C.GoString(C.pt_error()))
	}
	return nil
}
func (Bridge) Next() ([]byte, error) {
	p := C.pt_next(25)
	if p != nil {
		defer C.free(unsafe.Pointer(p))
		return []byte(C.GoString(p)), nil
	}
	if msg := C.GoString(C.pt_error()); msg != "" {
		return nil, errors.New(msg)
	}
	return nil, nil
}
func (Bridge) Close() { C.pt_close() }
