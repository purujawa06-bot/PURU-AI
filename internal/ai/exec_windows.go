//go:build windows

package ai

import "os/exec"

// setupKillGroup is a no-op on Windows: CommandContext already kills the
// process on timeout; no process-group primitive available.
func setupKillGroup(cmd *exec.Cmd) {}

func killGroup(pid int) {}
