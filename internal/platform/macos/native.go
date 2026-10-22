//go:build darwin && cgo

// Package macos links the native macos capture implementation.
package macos

// #include "../native/native.h"
import "C"

// The shared C header and implementation are build dependencies of this adapter.
import _ "github.com/zotniq/pastetrail/internal/platform/native"
