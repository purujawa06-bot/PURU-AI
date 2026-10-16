//go:build !windows

package main

// enableVirtualTerminal is a no-op outside Windows: POSIX terminals process
// ANSI escape sequences natively.
func enableVirtualTerminal() {}
