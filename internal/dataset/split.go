package dataset

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

type WindowRow struct {
	SessionID        string
	WindowEnd        time.Time
	RunID            string
	WindowStart      time.Time
	Label            string
	Target           string
	ProtocolVerified bool
	Features         map[string]float64
}

type WindowData struct {
	SHA256 string
	Rows   []WindowRow
}

func WindowFeatureNames() []string { return append([]string(nil), windowHeader()[11:]...) }

// ReadWindows validates legacy and session-aware headers and keeps metadata
// separate from numerical inputs. Empty numeric fields stay absent, never zero.
func ReadWindows(path string) (WindowData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return WindowData{}, err
	}
	hash := sha256.Sum256(data)
	result := WindowData{SHA256: hex.EncodeToString(hash[:]), Rows: []WindowRow{}}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		return result, err
	}
	if len(rows) == 0 || (!slices.Equal(rows[0], windowHeader()) && !slices.Equal(rows[0], sessionWindowHeader())) {
		return result, fmt.Errorf("unsupported window CSV schema")
	}
	featureOffset := 11
	if slices.Equal(rows[0], sessionWindowHeader()) {
		featureOffset = 12
	}
	classes := map[string]string{}
	sessions := map[string]string{}
	seen := map[string]bool{}
	for index, row := range rows[1:] {
		line := index + 2
		if strings.TrimSpace(row[0]) == "" {
			return result, fmt.Errorf("window row %d has no run_id", line)
		}
		class, err := scenario(row[1], row[2])
		if err != nil {
			return result, err
		}
		if previous, ok := classes[row[0]]; ok && previous != class {
			return result, fmt.Errorf("run %s has conflicting labels", row[0])
		}
		classes[row[0]] = class
		sessionID := ""
		if featureOffset == 12 {
			sessionID = row[11]
		}
		if err := runrecord.ValidateSessionID(sessionID); err != nil {
			return result, fmt.Errorf("window row %d: %w", line, err)
		}
		if previous, ok := sessions[row[0]]; ok && previous != sessionID {
			return result, fmt.Errorf("run %s has conflicting session declarations", row[0])
		}
		sessions[row[0]] = sessionID
		times := make([]time.Time, 4)
		for i := range times {
			times[i], err = time.Parse(time.RFC3339Nano, row[3+i])
			if err != nil {
				return result, fmt.Errorf("window row %d timestamp: %w", line, err)
			}
		}
		if !times[1].After(times[0]) || times[2].Before(times[0]) || times[3].Before(times[2]) || !times[3].Before(times[1]) {
			return result, fmt.Errorf("window row %d has inconsistent time bounds", line)
		}
		key := row[0] + "/" + times[0].Format(time.RFC3339Nano)
		if seen[key] {
			return result, fmt.Errorf("duplicate window for run %s", row[0])
		}
		seen[key] = true
		count, err := strconv.Atoi(row[7])
		if err != nil || count < 1 {
			return result, fmt.Errorf("window row %d has invalid sample count", line)
		}
		span, err := strconv.ParseFloat(row[8], 64)
		if err != nil || math.IsNaN(span) || math.IsInf(span, 0) || span < 0 {
			return result, fmt.Errorf("window row %d has invalid observed span", line)
		}
		if _, err := strconv.ParseBool(row[9]); err != nil {
			return result, err
		}
		verified, err := strconv.ParseBool(row[10])
		if err != nil {
			return result, err
		}
		out := WindowRow{RunID: row[0], SessionID: sessionID, WindowStart: times[0], WindowEnd: times[1], Label: row[1], Target: row[2], ProtocolVerified: verified, Features: map[string]float64{}}
		for i, name := range rows[0][featureOffset:] {
			value := row[featureOffset+i]
			if value == "" {
				if strings.HasSuffix(name, "_missing_ratio") {
					return result, fmt.Errorf("window row %d missing coverage ratio", line)
				}
				continue
			}
			x, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
				return result, fmt.Errorf("window row %d has invalid %s", line, name)
			}
			if x < 0 && !strings.HasSuffix(name, "_delta") {
				return result, fmt.Errorf("window row %d has negative %s", line, name)
			}
			if strings.HasSuffix(name, "_missing_ratio") && (x < 0 || x > 1) {
				return result, fmt.Errorf("window row %d has invalid missing ratio", line)
			}
			out.Features[name] = x
		}
		result.Rows = append(result.Rows, out)
	}
	return result, nil
}

type SplitOptions struct {
	GroupBy         string
	Embargo         time.Duration
	TestFraction    float64
	Seed            int64
	RequireProtocol bool
}
type ClassSplit struct {
	TrainRuns int `json:"train_runs"`
	TestRuns  int `json:"test_runs"`
}
type SplitManifest struct {
	Grouping           string                `json:"grouping,omitempty"`
	SessionAssignments map[string]string     `json:"session_assignments,omitempty"`
	TrainSessions      []string              `json:"train_sessions,omitempty"`
	TestSessions       []string              `json:"test_sessions,omitempty"`
	PurgedSessions     []string              `json:"purged_sessions,omitempty"`
	EmbargoSeconds     float64               `json:"embargo_seconds"`
	PurgedRuns         []string              `json:"purged_runs"`
	SchemaVersion      int                   `json:"schema_version"`
	WindowsSHA256      string                `json:"windows_sha256"`
	Seed               int64                 `json:"seed"`
	TestFraction       float64               `json:"test_fraction"`
	RequireProtocol    bool                  `json:"require_protocol"`
	FeatureNames       []string              `json:"feature_names"`
	TrainRuns          []string              `json:"train_runs"`
	TestRuns           []string              `json:"test_runs"`
	Classes            map[string]ClassSplit `json:"classes"`
}

// Split assigns whole runs, stratified by known experiment scenario. A class
// with fewer than two distinct runs is an error, not a misleading score.
func Split(inputPath, outputPath string, options SplitOptions) (SplitManifest, error) {
	m := SplitManifest{SchemaVersion: 1, Seed: options.Seed, TestFraction: options.TestFraction, RequireProtocol: options.RequireProtocol, FeatureNames: WindowFeatureNames(), TrainRuns: []string{}, TestRuns: []string{}, Classes: map[string]ClassSplit{}}
	if options.GroupBy != "" && options.GroupBy != "run" && options.GroupBy != "session" {
		return m, fmt.Errorf("group-by must be run or session")
	}
	if math.IsNaN(options.TestFraction) || options.TestFraction <= 0 || options.TestFraction >= 1 {
		return m, fmt.Errorf("test fraction must be greater than zero and less than one")
	}
	if err := distinctPaths(inputPath, outputPath); err != nil {
		return m, err
	}
	if options.Embargo < 0 || options.Embargo > 24*time.Hour {
		return m, fmt.Errorf("embargo must be between 0 and 24h")
	}
	m.EmbargoSeconds = options.Embargo.Seconds()
	m.PurgedRuns = []string{}
	data, err := ReadWindows(inputPath)
	if err != nil {
		return m, err
	}
	m.WindowsSHA256 = data.SHA256
	if options.GroupBy == "session" {
		m, err = splitSessions(data, m, options)
		if err != nil {
			return m, err
		}
		if err := runrecord.Save(outputPath, m); err != nil {
			return m, err
		}
		return m, nil
	}
	groups := map[string][]string{}
	seen := map[string]bool{}
	for _, row := range data.Rows {
		if options.RequireProtocol && !row.ProtocolVerified {
			return m, fmt.Errorf("run %s lacks verified protocol starting conditions; rebuild windows without legacy runs", row.RunID)
		}
		if seen[row.RunID] {
			continue
		}
		seen[row.RunID] = true
		class, _ := scenario(row.Label, row.Target)
		groups[class] = append(groups[class], row.RunID)
	}
	if len(groups) == 0 {
		return m, fmt.Errorf("cannot split an empty dataset")
	}
	for class, ids := range groups {
		if len(ids) < 2 {
			return m, fmt.Errorf("class %s needs at least two distinct runs, found %d", class, len(ids))
		}
		sort.Slice(ids, func(i, j int) bool { return splitKey(options.Seed, ids[i]) < splitKey(options.Seed, ids[j]) })
		n := int(math.Ceil(float64(len(ids)) * options.TestFraction))
		if n >= len(ids) {
			n = len(ids) - 1
		}
		m.TestRuns = append(m.TestRuns, ids[:n]...)
		m.TrainRuns = append(m.TrainRuns, ids[n:]...)
		m.Classes[class] = ClassSplit{TrainRuns: len(ids) - n, TestRuns: n}
	}
	sort.Strings(m.TrainRuns)
	sort.Strings(m.TestRuns)
	if options.Embargo > 0 {
		ranges := windowRanges(data)
		kept := []string{}
		for _, id := range m.TrainRuns {
			purge := false
			for _, testID := range m.TestRuns {
				if near(ranges[id], ranges[testID], options.Embargo) {
					purge = true
					break
				}
			}
			if purge {
				m.PurgedRuns = append(m.PurgedRuns, id)
			} else {
				kept = append(kept, id)
			}
		}
		m.TrainRuns = kept
		purged := map[string]bool{}
		for _, id := range m.PurgedRuns {
			purged[id] = true
		}
		counted := map[string]bool{}
		for _, row := range data.Rows {
			if purged[row.RunID] && !counted[row.RunID] {
				class, _ := scenario(row.Label, row.Target)
				c := m.Classes[class]
				c.TrainRuns--
				m.Classes[class] = c
				counted[row.RunID] = true
			}
		}
		for class, c := range m.Classes {
			if c.TrainRuns < 1 {
				return m, fmt.Errorf("time embargo leaves class %s without a training run; collect more separated runs without tuning against test scores", class)
			}
		}
	}
	if err := runrecord.Save(outputPath, m); err != nil {
		return m, err
	}
	return m, nil
}

func splitKey(seed int64, id string) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", seed, id)))
	return hex.EncodeToString(hash[:])
}
