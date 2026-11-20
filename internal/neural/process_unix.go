//go:build unix

package neural

import (
	"os/exec"
	"syscall"
)

// killProcessGroupOnCancel starts the worker in its own process group so a
// timeout also stops the CLI processes it spawned, not only the Python parent.
func killProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
