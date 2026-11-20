//go:build !unix

package neural

import "os/exec"

// The locked worker environment is Linux-only; elsewhere the default
// CommandContext cancellation (kill the direct child) applies.
func killProcessGroupOnCancel(*exec.Cmd) {}
