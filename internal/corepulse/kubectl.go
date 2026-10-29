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

type Kubectl struct{ Binary string }

func (k Kubectl) Run(timeout time.Duration, args ...string) (string, error) {
	binary := k.Binary
	if binary == "" {
		binary = "kubectl"
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
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
		return "", fmt.Errorf("kubectl failed: %s: %w", detail, err)
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
