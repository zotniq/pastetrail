//go:build windows && cgo

// Package windows links the native windows capture implementation.
package windows

// #include "../native/native.h"
import "C"

// The shared C header and implementation are build dependencies of this adapter.
import _ "github.com/zotniq/pastetrail/internal/platform/native"
