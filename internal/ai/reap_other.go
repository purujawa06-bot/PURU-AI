//go:build windows

package ai

// StartReaper is a no-op on Windows: process reaping is handled by the OS
// and there is no SIGCHLD / PID 1 zombie problem there.
func StartReaper() {}
