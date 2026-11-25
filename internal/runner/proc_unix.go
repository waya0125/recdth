//go:build unix && !linux

package runner

import (
	"os/exec"
	"syscall"
)

func setProcAttr(cmd *exec.Cmd, pgid int) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: pgid}
}

func signalGroup(pgid int, sig syscall.Signal) {
	_ = syscall.Kill(-pgid, sig)
}
