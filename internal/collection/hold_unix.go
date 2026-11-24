//go:build unix

package collection

import (
	"os"
	"os/exec"
	"syscall"
)

// isolateHelper puts the helper in its own process group. A terminal Ctrl+C
// then reaches only Hold, which forwards it once, so the helper's own restore
// trap is not interrupted by a second copy of the signal.
func isolateHelper(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func forwardSignal(cmd *exec.Cmd, sig os.Signal) {
	if s, ok := sig.(syscall.Signal); ok && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, s)
	}
}

func helperExitCode(state *os.ProcessState) int {
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	if code := state.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
