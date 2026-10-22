//go:build linux && cgo

// Package linux links the native linux capture implementation.
package linux

// #include "../native/native.h"
import "C"

// The shared C header and implementation are build dependencies of this adapter.
import _ "github.com/zotniq/pastetrail/internal/platform/native"
