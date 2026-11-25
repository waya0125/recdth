//go:build linux

package runner

import (
	"os/exec"
	"syscall"
)

// setProcAttr は子を pgid のプロセスグループに入れる（0 なら自分が新しいグループの先頭）。
// Pdeathsig で recdth が落ちたときにも子へ SIGTERM が届くようにする。
func setProcAttr(cmd *exec.Cmd, pgid int) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: pgid, Pdeathsig: syscall.SIGTERM}
}

func signalGroup(pgid int, sig syscall.Signal) {
	_ = syscall.Kill(-pgid, sig)
}
