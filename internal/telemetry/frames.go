package telemetry

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
)

// FrameReport is a read-only diagnostic of Metrics API source metadata in raw
// telemetry. It describes how captures relate to source frames; it does not
// deduplicate, relabel, filter or score samples, and it establishes nothing
// about train/test independence.
type FrameReport struct {
	SchemaVersion int         `json:"schema_version"`
	Input         string      `json:"input"`
	InputSHA256   string      `json:"input_sha256"`
	Rows          int         `json:"rows"`
	FirstCapture  *time.Time  `json:"first_capture"`
	LastCapture   *time.Time  `json:"last_capture"`
	CaptureOrder  OrderCounts `json:"capture_order"`
	Pods          []PodFrames `json:"pods"`
	Policy        FramePolicy `json:"policy"`
}

// OrderCounts counts capture timestamps that did not increase between rows.
type OrderCounts struct {
	Equal      int `json:"equal"`
	Regressing int `json:"regressing"`
}

// PodFrames summarises one pod name. A frame is an exact (source timestamp,
// window) pair; captures are rows in which the pod appears.
type PodFrames struct {
	Name     string `json:"name"`
	Captures int    `json:"captures"`
	// Known captures carry source metadata; unknown ones (legacy rows) do not
	// and are never given an invented age or window.
	Known   int `json:"known"`
	Unknown int `json:"unknown"`
	// UniqueFrames counts distinct frames; RepeatedCaptures are known
	// captures that re-read a frame already seen for this name.
	UniqueFrames     int `json:"unique_frames"`
	RepeatedCaptures int `json:"repeated_captures"`
	// ConflictingFrames are frames read more than once with different CPU or
	// memory values. They are reported, not resolved.
	ConflictingFrames int `json:"conflicting_frames"`
	// EqualValueFrames counts distinct frames whose values equal the previous
	// distinct frame's: identical values are not identical frames.
	EqualValueFrames int `json:"equal_value_frames"`
	// RegressingSource counts known captures whose source timestamp is earlier
	// than one already seen for this name, in capture-row order.
	RegressingSource int `json:"regressing_source"`
	// FutureSource counts known captures whose source timestamp is after the
	// capture time (clock skew between CorePulse and the metrics pipeline).
	FutureSource int `json:"future_source"`
	// Age is capture time minus source timestamp over known captures with a
	// non-negative age; nil when there are none.
	Age *AgeSeconds `json:"age_seconds"`
	// Windows lists the distinct source windows seen, in sorted order.
	Windows []string `json:"windows"`
	// Reappearances counts returns of the name after at least one row in which
	// it was absent.
	Reappearances int `json:"reappearances"`
}

// AgeSeconds is the capture-to-source age distribution in seconds.
type AgeSeconds struct {
	Min    float64 `json:"min"`
	Median float64 `json:"median"`
	Max    float64 `json:"max"`
}

// FramePolicy declares how ambiguous cases are handled.
type FramePolicy struct {
	Identity      string `json:"identity"`
	NameReuse     string `json:"name_reuse"`
	ChangedValues string `json:"changed_values"`
	ClockSkew     string `json:"clock_skew"`
	Unknown       string `json:"unknown"`
	Scope         string `json:"scope"`
}

var framePolicy = FramePolicy{
	Identity:      "A frame is the exact source timestamp and window string reported for one pod name.",
	NameReuse:     "Pods are keyed by name only. A name that disappears and returns is counted in reappearances, not split into separate pods.",
	ChangedValues: "A frame read again with different CPU or memory values is counted in conflicting_frames; neither value is preferred or dropped.",
	ClockSkew:     "Ages compare the CorePulse capture clock with the metrics pipeline clock. Negative ages are counted in future_source and excluded from age_seconds.",
	Unknown:       "Rows without source metadata are counted as unknown and contribute no age, window or frame.",
	Scope:         "Diagnostic only. It does not deduplicate, filter, relabel or establish train/test independence. Repeated frames are expected when polling faster than the metrics source updates.",
}

type frameKey struct {
	at     time.Time
	window string
}

type frameValue struct {
	cpu    float64
	memory int64
}

type podState struct {
	report       PodFrames
	frames       map[frameKey]frameValue
	conflicted   map[frameKey]bool
	windows      map[string]bool
	ages         []float64
	latestSource time.Time
	lastFrame    *frameValue
	lastRow      int
}

// AnalyzeFrames reads raw telemetry JSONL and reports source-frame reuse and
// measurement age per pod name. Any malformed row rejects the whole input.
func AnalyzeFrames(path string) (FrameReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FrameReport{}, err
	}
	digest := sha256.Sum256(data)
	report := FrameReport{SchemaVersion: 1, Input: filepath.Base(path), InputSHA256: hex.EncodeToString(digest[:]), Pods: []PodFrames{}, Policy: framePolicy}
	pods := map[string]*podState{}
	var previous time.Time
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			return FrameReport{}, fmt.Errorf("telemetry row %d is empty", report.Rows+1)
		}
		report.Rows++
		row := report.Rows
		var sample Sample
		decoder := json.NewDecoder(bytes.NewReader(raw))
		if err := decoder.Decode(&sample); err != nil {
			return FrameReport{}, fmt.Errorf("telemetry row %d: %w", row, err)
		}
		if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
			return FrameReport{}, fmt.Errorf("telemetry row %d has trailing data", row)
		}
		if sample.SchemaVersion != 1 || sample.Context != corepulse.LabContext || sample.Namespace != corepulse.LabNamespace || sample.Source != Source || sample.Timestamp.IsZero() {
			return FrameReport{}, fmt.Errorf("telemetry row %d has invalid schema, provenance or capture timestamp", row)
		}
		capture := sample.Timestamp
		if report.FirstCapture == nil {
			first := capture
			report.FirstCapture = &first
		} else if capture.Equal(previous) {
			report.CaptureOrder.Equal++
		} else if capture.Before(previous) {
			report.CaptureOrder.Regressing++
		}
		last := capture
		report.LastCapture, previous = &last, capture
		names := map[string]bool{}
		for _, pod := range sample.Pods {
			if pod.Name == "" {
				return FrameReport{}, fmt.Errorf("telemetry row %d has a pod without a name", row)
			}
			if names[pod.Name] {
				return FrameReport{}, fmt.Errorf("telemetry row %d lists pod %s twice", row, pod.Name)
			}
			names[pod.Name] = true
			state := pods[pod.Name]
			if state == nil {
				state = &podState{report: PodFrames{Name: pod.Name}, frames: map[frameKey]frameValue{}, conflicted: map[frameKey]bool{}, windows: map[string]bool{}}
				pods[pod.Name] = state
			} else if state.lastRow < row-1 {
				state.report.Reappearances++
			}
			state.lastRow = row
			state.observe(pod, capture)
		}
	}
	if err := scanner.Err(); err != nil {
		return FrameReport{}, err
	}
	if report.Rows == 0 {
		return FrameReport{}, errors.New("telemetry input has no rows")
	}
	names := make([]string, 0, len(pods))
	for name := range pods {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		report.Pods = append(report.Pods, pods[name].finish())
	}
	return report, nil
}

func (s *podState) observe(pod PodUsage, capture time.Time) {
	s.report.Captures++
	if pod.Measurement == nil {
		s.report.Unknown++
		return
	}
	s.report.Known++
	m := *pod.Measurement
	key := frameKey{at: m.Timestamp.UTC(), window: m.Window}
	value := frameValue{cpu: pod.CPUMilli, memory: pod.MemoryBytes}
	s.windows[m.Window] = true
	if !s.latestSource.IsZero() && m.Timestamp.Before(s.latestSource) {
		s.report.RegressingSource++
	}
	if m.Timestamp.After(s.latestSource) {
		s.latestSource = m.Timestamp
	}
	if age := capture.Sub(m.Timestamp).Seconds(); age < 0 {
		s.report.FutureSource++
	} else {
		s.ages = append(s.ages, age)
	}
	if seen, ok := s.frames[key]; ok {
		s.report.RepeatedCaptures++
		if seen != value && !s.conflicted[key] {
			s.conflicted[key] = true
			s.report.ConflictingFrames++
		}
		return
	}
	s.frames[key] = value
	s.report.UniqueFrames++
	if s.lastFrame != nil && *s.lastFrame == value {
		s.report.EqualValueFrames++
	}
	s.lastFrame = &value
}

func (s *podState) finish() PodFrames {
	r := s.report
	r.Windows = make([]string, 0, len(s.windows))
	for window := range s.windows {
		r.Windows = append(r.Windows, window)
	}
	sort.Strings(r.Windows)
	if len(s.ages) > 0 {
		sort.Float64s(s.ages)
		n := len(s.ages)
		median := s.ages[n/2]
		if n%2 == 0 {
			median = (s.ages[n/2-1] + s.ages[n/2]) / 2
		}
		r.Age = &AgeSeconds{Min: s.ages[0], Median: median, Max: s.ages[n-1]}
	}
	return r
}

// WriteFrameReport writes the report atomically; a failed write leaves any
// earlier report in place.
func WriteFrameReport(path string, report FrameReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".source-frames-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
