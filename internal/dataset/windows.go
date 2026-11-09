package dataset

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

// WindowSignals is an explicit numeric allowlist. Run identity, target, labels,
// recovery outcomes and timestamps are metadata, never numerical model inputs.
var WindowSignals = []string{"pod_count", "cpu_millicores", "memory_bytes", "amf_cpu_millicores", "amf_memory_bytes", "upf_cpu_millicores", "upf_memory_bytes", "probe_sent", "probe_received", "smf_pfcp_failures", "gnb_n2_terminations", "ue_registrations", "ue_pdu_sessions"}

type WindowOptions struct {
	Duration        time.Duration
	RequireProtocol bool
}

type WindowSummary struct {
	Windows            int            `json:"windows"`
	Samples            int            `json:"samples"`
	ExcludedSamples    int            `json:"excluded_samples"`
	Runs               int            `json:"runs"`
	ExcludedLegacyRuns int            `json:"excluded_legacy_runs"`
	PartialWindows     int            `json:"partial_windows"`
	DurationSeconds    float64        `json:"duration_seconds"`
	RequireProtocol    bool           `json:"require_protocol"`
	ClassWindows       map[string]int `json:"class_windows"`
	Output             string         `json:"output"`
}

type windowSample struct {
	at     time.Time
	values []string
}
type windowBucket struct {
	run        Run
	start, end time.Time
	samples    []windowSample
	partial    bool
}

// BuildWindows creates non-overlapping run-aligned windows. Empty time windows
// are omitted, not filled. Missing ratios count captured rows, not elapsed time.
func BuildWindows(rawPath, runsDir, outputPath string, options WindowOptions) (WindowSummary, error) {
	summary := WindowSummary{DurationSeconds: options.Duration.Seconds(), RequireProtocol: options.RequireProtocol, ClassWindows: map[string]int{}, Output: outputPath}
	if options.Duration < time.Second || options.Duration > time.Hour {
		return summary, fmt.Errorf("window duration must be between 1s and 1h")
	}
	if err := distinctPaths(rawPath, outputPath); err != nil {
		return summary, err
	}
	if rel, err := filepath.Rel(runsDir, outputPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return summary, fmt.Errorf("window output must be outside the run record directory")
	}
	runs, err := loadRuns(runsDir)
	if err != nil {
		return summary, err
	}
	eligible := make([]Run, 0, len(runs))
	eligibleIDs := map[string]bool{}
	for _, run := range runs {
		if options.RequireProtocol && !run.ProtocolVerified {
			summary.ExcludedLegacyRuns++
			continue
		}
		if _, err := scenario(run.Fault, run.Target); err != nil {
			return summary, err
		}
		if eligibleIDs[run.ID] {
			return summary, fmt.Errorf("duplicate run identity %s", run.ID)
		}
		eligibleIDs[run.ID] = true
		eligible = append(eligible, run)
	}
	input, err := os.Open(rawPath)
	if err != nil {
		return summary, err
	}
	defer input.Close()
	buckets := map[string]*windowBucket{}
	seen := map[string]bool{}
	runIDs := map[string]bool{}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var sample telemetry.Sample
		if err := json.Unmarshal(scanner.Bytes(), &sample); err != nil {
			return summary, fmt.Errorf("telemetry row %d: %w", line, err)
		}
		if sample.Context != corepulse.LabContext || sample.Namespace != corepulse.LabNamespace || sample.Source != telemetry.Source || sample.Timestamp.IsZero() {
			return summary, fmt.Errorf("telemetry row %d has invalid provenance or timestamp", line)
		}
		_, run, err := label(sample, eligible)
		if err != nil {
			return summary, err
		}
		if run == nil || !sample.Timestamp.Before(run.Ended) {
			summary.ExcludedSamples++
			continue
		}
		key := run.ID + "/" + sample.Timestamp.Format(time.RFC3339Nano)
		if seen[key] {
			return summary, fmt.Errorf("duplicate telemetry timestamp in run %s: %s", run.ID, sample.Timestamp)
		}
		seen[key] = true
		index := sample.Timestamp.Sub(run.Started) / options.Duration
		key = fmt.Sprintf("%s/%d", run.ID, index)
		bucket := buckets[key]
		if bucket == nil {
			start := run.Started.Add(index * options.Duration)
			end := start.Add(options.Duration)
			partial := end.After(run.Ended)
			if partial {
				end = run.Ended
			}
			bucket = &windowBucket{run: *run, start: start, end: end, partial: partial}
			buckets[key] = bucket
		}
		values := sampleValues(sample, *run)
		for i, value := range values {
			if value == "" {
				continue
			}
			x, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(x) || math.IsInf(x, 0) || x < 0 {
				return summary, fmt.Errorf("invalid %s in telemetry row %d", WindowSignals[i], line)
			}
		}
		bucket.samples = append(bucket.samples, windowSample{at: sample.Timestamp, values: values})
		summary.Samples++
		runIDs[run.ID] = true
	}
	if err := scanner.Err(); err != nil {
		return summary, err
	}
	ordered := make([]*windowBucket, 0, len(buckets))
	for _, b := range buckets {
		ordered = append(ordered, b)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].start.Equal(ordered[j].start) {
			return ordered[i].run.ID < ordered[j].run.ID
		}
		return ordered[i].start.Before(ordered[j].start)
	})
	rows := [][]string{windowHeader()}
	for _, bucket := range ordered {
		sort.Slice(bucket.samples, func(i, j int) bool { return bucket.samples[i].at.Before(bucket.samples[j].at) })
		row, err := bucket.row()
		if err != nil {
			return summary, err
		}
		rows = append(rows, row)
		summary.Windows++
		if bucket.partial {
			summary.PartialWindows++
		}
		class, _ := scenario(bucket.run.Fault, bucket.run.Target)
		summary.ClassWindows[class]++
	}
	summary.Runs = len(runIDs)
	if err := writeCSV(outputPath, rows); err != nil {
		return summary, err
	}
	return summary, nil
}

func sampleValues(sample telemetry.Sample, run Run) []string {
	amfCPU, amfMem := componentFields(sample.Pods, "open5gs-amf")
	upfCPU, upfMem := componentFields(sample.Pods, "open5gs-upf")
	values := []string{strconv.Itoa(sample.PodCount), number(sample.CPUMilli), strconv.FormatInt(sample.MemoryBytes, 10), amfCPU, amfMem, upfCPU, upfMem}
	return append(values, run.evidence.fields(run, sample.Timestamp)[1:]...)
}

func windowHeader() []string {
	columns := []string{"run_id", "label", "target", "window_start", "window_end", "first_sample_at", "last_sample_at", "sample_count", "observed_span_seconds", "partial", "protocol_start_verified"}
	for _, signal := range WindowSignals {
		for _, stat := range []string{"mean", "max", "delta", "missing_ratio"} {
			columns = append(columns, signal+"_"+stat)
		}
	}
	return columns
}

func (b *windowBucket) row() ([]string, error) {
	first, last := b.samples[0].at, b.samples[len(b.samples)-1].at
	row := []string{b.run.ID, b.run.Fault, b.run.Target, b.start.Format(time.RFC3339Nano), b.end.Format(time.RFC3339Nano), first.Format(time.RFC3339Nano), last.Format(time.RFC3339Nano), strconv.Itoa(len(b.samples)), number(last.Sub(first).Seconds()), strconv.FormatBool(b.partial), strconv.FormatBool(b.run.ProtocolVerified)}
	for i := range WindowSignals {
		values := []float64{}
		for _, s := range b.samples {
			if s.values[i] != "" {
				value, _ := strconv.ParseFloat(s.values[i], 64)
				values = append(values, value)
			}
		}
		missing := number(1 - float64(len(values))/float64(len(b.samples)))
		if len(values) == 0 {
			row = append(row, "", "", "", missing)
			continue
		}
		mean, max := 0.0, values[0]
		for _, v := range values {
			mean += v / float64(len(values))
			if v > max {
				max = v
			}
		}
		if math.IsInf(mean, 0) || math.IsNaN(mean) {
			return nil, fmt.Errorf("window aggregate overflow for %s", WindowSignals[i])
		}
		delta := ""
		if len(values) >= 2 {
			delta = number(values[len(values)-1] - values[0])
		}
		row = append(row, number(mean), number(max), delta, missing)
	}
	return row, nil
}

func number(x float64) string { return strconv.FormatFloat(x, 'g', -1, 64) }

func scenario(label, target string) (string, error) {
	if label == "normal" && target == "" {
		return "normal", nil
	}
	if label == "pod_crash" && (target == "amf" || target == "upf") {
		return label + ":" + target, nil
	}
	return "", fmt.Errorf("unsupported scenario label=%q target=%q", label, target)
}

func distinctPaths(input, output string) error {
	a, err := filepath.Abs(input)
	if err != nil {
		return err
	}
	b, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	// Case-insensitive comparison is conservative on Unix and protects Windows.
	if strings.EqualFold(a, b) {
		return fmt.Errorf("output must not overwrite input")
	}
	fi, err1 := os.Stat(input)
	fo, err2 := os.Stat(output)
	if err1 == nil && err2 == nil && os.SameFile(fi, fo) {
		return fmt.Errorf("output aliases input")
	}
	return nil
}

func writeCSV(path string, rows [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".windows-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
