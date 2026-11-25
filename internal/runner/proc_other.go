//go:build !unix

package runner

import (
	"os"
	"os/exec"
	"syscall"
)

// Windows などではプロセスグループを使わず、先頭のプロセスだけを止める（recdth の主な対象は Linux）。
func setProcAttr(cmd *exec.Cmd, pgid int) {}

func signalGroup(pgid int, sig syscall.Signal) {
	if p, err := os.FindProcess(pgid); err == nil {
		_ = p.Kill()
	}
}
