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

// killGroupTerm asks the whole process group to exit so the shell can reap
// its children (git, tar, ssl_client) before dying. SIGKILL skips that
// chance and leaves the children orphaned under PID 1 as zombies.
func killGroupTerm(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}
