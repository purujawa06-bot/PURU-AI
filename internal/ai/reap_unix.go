//go:build !windows

package ai

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var reaperOnce sync.Once

// StartReaper installs a SIGCHLD handler that reaps orphaned grandchildren.
// When a timed-out exec group is killed, the shell dies before its children
// (git, tar, ssl_client) so they are reparented to PID 1. Go's cmd.Wait only
// reaps the direct shell, leaving the orphans as zombies without this loop.
// Idempotent: safe to call from every binary entrypoint.
func StartReaper() {
	reaperOnce.Do(func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGCHLD)
		go func() {
			for range ch {
				reapChildren()
			}
		}()
	})
}

// reapChildren collects every exited child without blocking.
func reapChildren() {
	var status syscall.WaitStatus
	for {
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if pid <= 0 || err != nil {
			return
		}
	}
}
