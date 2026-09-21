//go:build !windows

package ai

import (
	"os/exec"
	"syscall"
)

// setupKillGroup puts the child in its own process group so killGroup can
// wipe the whole tree on timeout (no leaked RAM from orphaned children).
func setupKillGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killGroup(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
