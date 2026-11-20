package dataset

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Sequence values are within-step observed means, never interpolated samples.
// A mask of one means at least one capture observed that signal in this step.
const SequenceKind = "sequences_v1"
const maxSequenceSteps = 100000

type SequenceOptions struct {
	Step            time.Duration
	RequireProtocol bool
}
type SequenceSummary struct {
	SchemaVersion      int     `json:"schema_version"`
	Steps              int     `json:"steps"`
	Samples            int     `json:"samples"`
	EmptySteps         int     `json:"empty_steps"`
	PartialSteps       int     `json:"partial_steps"`
	Runs               int     `json:"runs"`
	ExcludedSamples    int     `json:"excluded_samples"`
	ExcludedLegacyRuns int     `json:"excluded_legacy_runs"`
	StepSeconds        float64 `json:"step_seconds"`
	Output             string  `json:"output"`
}

func sequenceHeader() []string {
	h := []string{"run_id", "label", "target", "step_index", "step_start", "step_end", "sample_count", "partial", "protocol_start_verified", "session_id", "step_seconds"}
	for _, signal := range WindowSignals {
		h = append(h, signal+"_value", signal+"_mask")
	}
	return h
}

func SequenceFeatureNames() []string { return append([]string(nil), sequenceHeader()[11:]...) }

// BuildSequences deliberately delegates raw attribution, source validation,
// quality/protocol gates and aggregation to the unchanged window exporter.
// It then restores empty run-aligned bins with explicit missing-value masks.
func BuildSequences(rawPath, runsDir, outputPath string, options SequenceOptions) (SequenceSummary, error) {
	summary := SequenceSummary{SchemaVersion: 1, StepSeconds: options.Step.Seconds(), Output: outputPath}
	if options.Step < time.Second || options.Step > time.Hour {
		return summary, fmt.Errorf("sequence step must be between 1s and 1h")
	}
	if err := distinctPaths(rawPath, outputPath); err != nil {
		return summary, err
	}
	if rel, err := filepath.Rel(runsDir, outputPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return summary, fmt.Errorf("sequence output must be outside the run record directory")
	}
	runs, err := loadRuns(runsDir)
	if err != nil {
		return summary, err
	}
	eligible := []Run{}
	ids := map[string]bool{}
	for _, r := range runs {
		if options.RequireProtocol && !r.ProtocolVerified {
			continue
		}
		if _, err := scenario(r.Fault, r.Target); err != nil {
			return summary, err
		}
		if ids[r.ID] {
			return summary, fmt.Errorf("duplicate run identity %s", r.ID)
		}
		ids[r.ID] = true
		n := int64((r.Ended.Sub(r.Started)-1)/options.Step) + 1
		if n < 1 || n > maxSequenceSteps-int64(summary.Steps) {
			return summary, fmt.Errorf("sequence export exceeds %d steps", maxSequenceSteps)
		}
		summary.Steps += int(n)
		eligible = append(eligible, r)
	}
	if len(eligible) == 0 {
		return summary, fmt.Errorf("no eligible runs for sequence export")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return summary, err
	}
	temp, err := os.CreateTemp(filepath.Dir(outputPath), ".sequence-window-*.csv")
	if err != nil {
		return summary, err
	}
	temporary := temp.Name()
	defer os.Remove(temporary)
	if err := temp.Close(); err != nil {
		return summary, err
	}
	w, err := BuildWindows(rawPath, runsDir, temporary, WindowOptions{Duration: options.Step, RequireProtocol: options.RequireProtocol})
	if err != nil {
		return summary, err
	}
	after, err := loadRuns(runsDir)
	if err != nil {
		return summary, err
	}
	if !reflect.DeepEqual(runs, after) {
		return summary, fmt.Errorf("run records changed during sequence export")
	}
	f, err := os.Open(temporary)
	if err != nil {
		return summary, err
	}
	windowRows, err := csv.NewReader(f).ReadAll()
	closeErr := f.Close()
	if err != nil {
		return summary, err
	}
	if closeErr != nil {
		return summary, closeErr
	}
	offset := 11
	if len(windowRows[0]) == len(sessionWindowHeader()) {
		offset = 12
	}
	buckets := map[string][]string{}
	for _, row := range windowRows[1:] {
		buckets[row[0]+"/"+row[3]] = row
	}
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].Started.Equal(eligible[j].Started) {
			return eligible[i].ID < eligible[j].ID
		}
		return eligible[i].Started.Before(eligible[j].Started)
	})
	rows := [][]string{sequenceHeader()}
	for _, r := range eligible {
		for index, start := 0, r.Started; start.Before(r.Ended); index, start = index+1, start.Add(options.Step) {
			end := start.Add(options.Step)
			partial := end.After(r.Ended)
			if partial {
				end = r.Ended
				summary.PartialSteps++
			}
			window := buckets[r.ID+"/"+start.Format(time.RFC3339Nano)]
			count := "0"
			if window != nil {
				count = window[7]
			} else {
				summary.EmptySteps++
			}
			row := []string{r.ID, r.Fault, r.Target, strconv.Itoa(index), start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano), count, strconv.FormatBool(partial), strconv.FormatBool(r.ProtocolVerified), r.SessionID, number(options.Step.Seconds())}
			for i := range WindowSignals {
				value, mask := "", "0"
				if window != nil {
					value = window[offset+4*i]
					if value != "" {
						mask = "1"
					}
				}
				row = append(row, value, mask)
			}
			rows = append(rows, row)
		}
	}
	summary.Runs, summary.Samples, summary.ExcludedSamples, summary.ExcludedLegacyRuns = len(eligible), w.Samples, w.ExcludedSamples, w.ExcludedLegacyRuns
	if err := writeCSV(outputPath, rows); err != nil {
		return summary, err
	}
	return summary, nil
}
