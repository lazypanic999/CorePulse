package collection

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"syscall"
)

// Environment passed to a helper running under Hold. The token lets the helper
// confirm, through CheckHeld, that the configured marker is really its own.
const (
	HeldTokenEnv     = "COREPULSE_OWNERSHIP_TOKEN"
	HeldOperationEnv = "COREPULSE_OWNERSHIP_OPERATION"
)

var helperOperation = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// Hold acquires shared collection ownership, runs one helper command and
// releases ownership only after that command has exited. Contention refuses
// before the command starts. Interrupt, terminate and hangup signals are
// forwarded to the helper, never treated as a reason to release early. If Hold
// itself is killed, the marker stays for conservative manual inspection.
func Hold(operation string, argv []string, stdout, stderr io.Writer) (code int, runErr error) {
	if !helperOperation.MatchString(operation) {
		return 1, fmt.Errorf("helper operation must be lowercase letters, digits and dashes")
	}
	if len(argv) == 0 || argv[0] == "" {
		return 1, errors.New("hold requires a helper command after --")
	}
	if os.Getenv(HeldTokenEnv) != "" {
		return 1, errors.New("already running under collection ownership; run the helper directly instead of nesting hold")
	}
	owner, err := AcquireOwnership(operation)
	if err != nil {
		return 1, err
	}
	// Register before starting the helper so no signal can slip through and
	// terminate this process while the helper keeps mutating.
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	defer func() {
		runErr = errors.Join(runErr, owner.Release())
		if runErr != nil && code == 0 {
			code = 1
		}
	}()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.Env = append(os.Environ(), HeldTokenEnv+"="+owner.token, HeldOperationEnv+"="+operation)
	isolateHelper(cmd)
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("start helper: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case sig := <-signals:
			forwardSignal(cmd, sig)
		case err := <-done:
			if cmd.ProcessState == nil {
				return 1, fmt.Errorf("helper did not run: %w", err)
			}
			if exit := helperExitCode(cmd.ProcessState); exit != 0 {
				return exit, nil
			}
			return 0, nil
		}
	}
}

// CheckHeld confirms that the configured marker holds the given token. Helpers
// call it before mutating so a stale or copied environment cannot pass.
func CheckHeld(token string) (Owner, error) {
	if token == "" {
		return Owner{}, fmt.Errorf("%s is not set; run the helper through corepulse hold", HeldTokenEnv)
	}
	dir, err := ownershipDir()
	if err != nil {
		return Owner{}, err
	}
	owner, err := readOwner(dir)
	if err != nil {
		return Owner{}, fmt.Errorf("collection ownership at %s cannot be confirmed: %w", dir, err)
	}
	if owner.Token != token {
		return Owner{}, fmt.Errorf("collection ownership at %s belongs to %s, not this helper", dir, owner.Operation)
	}
	return owner, nil
}
