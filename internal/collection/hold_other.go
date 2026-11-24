//go:build !unix

package collection

import (
	"os"
	"os/exec"
)

// On Windows the console delivers Ctrl+C to the helper directly; Hold only
// keeps waiting so it cannot release while the helper still runs.
func isolateHelper(*exec.Cmd) {}

func forwardSignal(*exec.Cmd, os.Signal) {}

func helperExitCode(state *os.ProcessState) int {
	if code := state.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
