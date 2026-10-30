package telemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
)

const Source = "metrics.k8s.io/v1beta1"

type PodUsage struct {
	Name        string  `json:"name"`
	CPUMilli    float64 `json:"cpu_millicores"`
	MemoryBytes int64   `json:"memory_bytes"`
}

type Sample struct {
	SchemaVersion int        `json:"schema_version"`
	Timestamp     time.Time  `json:"timestamp"`
	Source        string     `json:"source"`
	Context       string     `json:"context"`
	Namespace     string     `json:"namespace"`
	CPUMilli      float64    `json:"cpu_millicores"`
	MemoryBytes   int64      `json:"memory_bytes"`
	PodCount      int        `json:"pod_count"`
	Pods          []PodUsage `json:"pods"`
}

// Capture reads Kubernetes resource metrics. No synthetic values are emitted.
func Capture(client corepulse.Client) (Sample, error) {
	contextName, err := client.Run(10*time.Second, "config", "current-context")
	if err != nil {
		return Sample{}, err
	}
	if contextName != corepulse.LabContext {
		return Sample{}, fmt.Errorf("metrics require context %q; current context is %q", corepulse.LabContext, contextName)
	}
	raw, err := client.Run(20*time.Second, "get", "--raw", "/apis/metrics.k8s.io/v1beta1/namespaces/open5gs/pods")
	if err != nil {
		return Sample{}, err
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Containers []struct {
				Usage map[string]string `json:"usage"`
			} `json:"containers"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return Sample{}, fmt.Errorf("invalid resource metrics: %w", err)
	}
	if len(list.Items) == 0 {
		return Sample{}, fmt.Errorf("resource metrics returned no pods in %s", corepulse.LabNamespace)
	}
	sample := Sample{SchemaVersion: 1, Timestamp: time.Now().UTC(), Source: Source, Context: contextName, Namespace: corepulse.LabNamespace, Pods: []PodUsage{}}
	for _, item := range list.Items {
		if item.Metadata.Name == "" {
			continue
		}
		pod := PodUsage{Name: item.Metadata.Name}
		for _, container := range item.Containers {
			cpu, err := cpuMilli(container.Usage["cpu"])
			if err != nil {
				return Sample{}, fmt.Errorf("pod %s CPU: %w", pod.Name, err)
			}
			memory, err := memoryBytes(container.Usage["memory"])
			if err != nil {
				return Sample{}, fmt.Errorf("pod %s memory: %w", pod.Name, err)
			}
			pod.CPUMilli += cpu
			pod.MemoryBytes += memory
		}
		sample.CPUMilli += pod.CPUMilli
		sample.MemoryBytes += pod.MemoryBytes
		sample.Pods = append(sample.Pods, pod)
	}
	sort.Slice(sample.Pods, func(i, j int) bool { return sample.Pods[i].Name < sample.Pods[j].Name })
	sample.PodCount = len(sample.Pods)
	if sample.PodCount == 0 {
		return Sample{}, fmt.Errorf("resource metrics contained no named pods")
	}
	return sample, nil
}

func quantity(value string, suffixes map[string]float64) (float64, error) {
	if value == "" {
		return 0, fmt.Errorf("missing quantity")
	}
	for _, suffix := range []string{"Ki", "Mi", "Gi", "Ti", "n", "u", "m", "k", "M", "G", "T"} {
		if strings.HasSuffix(value, suffix) {
			factor, ok := suffixes[suffix]
			if !ok {
				return 0, fmt.Errorf("unsupported quantity %q", value)
			}
			n, err := strconv.ParseFloat(strings.TrimSuffix(value, suffix), 64)
			if err != nil {
				return 0, fmt.Errorf("invalid quantity %q", value)
			}
			return n * factor, nil
		}
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid quantity %q", value)
	}
	return n, nil
}

func cpuMilli(value string) (float64, error) {
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		n, _ := strconv.ParseFloat(value, 64)
		return n * 1000, nil
	}
	return quantity(value, map[string]float64{"n": 0.000001, "u": 0.001, "m": 1, "k": 1_000_000, "M": 1_000_000_000})
}

func memoryBytes(value string) (int64, error) {
	n, err := quantity(value, map[string]float64{"Ki": 1024, "Mi": 1024 * 1024, "Gi": 1024 * 1024 * 1024, "Ti": 1024 * 1024 * 1024 * 1024, "k": 1000, "M": 1_000_000, "G": 1_000_000_000, "T": 1_000_000_000_000})
	if err != nil {
		return 0, err
	}
	return int64(n), nil
}

type Store struct {
	mu        sync.RWMutex
	path      string
	samples   []Sample
	lastError string
}

func (s *Store) Path() string { return s.path }

func NewStore(path string) *Store {
	s := &Store{path: path, samples: []Sample{}}
	file, err := os.Open(path)
	if err != nil {
		return s
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var sample Sample
		if json.Unmarshal(scanner.Bytes(), &sample) == nil {
			s.samples = append(s.samples, sample)
		}
		if len(s.samples) > 120 {
			s.samples = s.samples[1:]
		}
	}
	return s
}

func (s *Store) Capture(client corepulse.Client) error {
	sample, err := Capture(client)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.lastError = err.Error()
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		s.lastError = err.Error()
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		s.lastError = err.Error()
		return err
	}
	data, err := json.Marshal(sample)
	if err == nil {
		_, err = file.Write(append(data, '\n'))
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		s.lastError = err.Error()
		return err
	}
	s.samples = append(s.samples, sample)
	if len(s.samples) > 120 {
		s.samples = s.samples[1:]
	}
	s.lastError = ""
	return nil
}

func (s *Store) Start(ctx context.Context, client corepulse.Client, interval time.Duration) {
	go func() {
		_ = s.Capture(client)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.Capture(client)
			}
		}
	}()
}

func (s *Store) Snapshot() ([]Sample, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copySamples := append([]Sample(nil), s.samples...)
	return copySamples, s.lastError
}
