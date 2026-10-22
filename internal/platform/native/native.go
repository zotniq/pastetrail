//go:build cgo && (darwin || linux || windows)

// Package native links the shared C serialization and event queue implementation.
package native

// #include "native.h"
import "C"
