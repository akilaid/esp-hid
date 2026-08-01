//go:build darwin

package main

import "runtime"

// AppKit may only be driven from the process's initial thread. main.main is
// guaranteed to start there, but without this lock the Go runtime is free to
// migrate the main goroutine to another thread, after which [NSApp run]
// misbehaves in ways that are hard to attribute.
func init() {
	runtime.LockOSThread()
}
