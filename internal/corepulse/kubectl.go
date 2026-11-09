package corepulse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Client runs kubectl with argument slices, never through a shell.
type Client interface {
	Run(timeout time.Duration, args ...string) (string, error)
	GetJSON(args ...string) (map[string]any, error)
}

type Kubectl struct {
	Binary  string
	Context context.Context
}

func (k Kubectl) Run(timeout time.Duration, args ...string) (string, error) {
	binary := k.Binary
	if binary == "" {
		binary = "kubectl"
	}
	parent := k.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	// Observe the ambient context for the existing fail-closed preflight, but
	// bind every actual lab operation to the allowlisted context. A concurrent
	// `kubectl config use-context` must not redirect an already-checked run.
	commandArgs := args
	if !(len(args) == 2 && args[0] == "config" && args[1] == "current-context") {
		commandArgs = append([]string{"--context", LabContext}, args...)
	}
	cmd := exec.CommandContext(ctx, binary, commandArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if parent.Err() != nil {
		return "", parent.Err()
	}
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("kubectl timed out: %s", strings.Join(args, " "))
	}
	if err != nil {
		if _, ok := err.(*exec.Error); ok {
			return "", fmt.Errorf("kubectl is not installed or is not on PATH: %w", err)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return strings.TrimSpace(stdout.String()), fmt.Errorf("kubectl failed: %s: %w", detail, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (k Kubectl) GetJSON(args ...string) (map[string]any, error) {
	output, err := k.Run(30*time.Second, append(args, "-o", "json")...)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		return nil, fmt.Errorf("kubectl returned invalid JSON: %w", err)
	}
	if data == nil {
		return nil, fmt.Errorf("kubectl returned an unexpected JSON shape")
	}
	return data, nil
}
