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

// ReadSplitInput dispatches exact schemas only. The window evaluator continues
// to call ReadWindows, so it cannot accidentally treat masks as window features.
func ReadSplitInput(path string) (WindowData, error) {
	f, err := os.Open(path)
	if err != nil {
		return WindowData{}, err
	}
	header, err := csv.NewReader(f).Read()
	closeErr := f.Close()
	if err != nil {
		return WindowData{}, err
	}
	if closeErr != nil {
		return WindowData{}, closeErr
	}
	if slices.Equal(header, sequenceHeader()) {
		return ReadSequences(path)
	}
	return ReadWindows(path)
}

// ReadSequences preserves absent values in Features, and validates masks and
// complete per-run grids before the shared hash/group/embargo split machinery.
func ReadSequences(path string) (WindowData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return WindowData{}, err
	}
	hash := sha256.Sum256(raw)
	data := WindowData{SHA256: hex.EncodeToString(hash[:]), Rows: []WindowRow{}, InputKind: SequenceKind, FeatureNames: SequenceFeatureNames()}
	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		return data, err
	}
	if len(rows) < 2 || !slices.Equal(rows[0], sequenceHeader()) {
		return data, fmt.Errorf("unsupported or empty sequence CSV")
	}
	if len(rows)-1 > maxSequenceSteps {
		return data, fmt.Errorf("sequence CSV exceeds step limit")
	}
	type gridRow struct {
		index      int
		start, end time.Time
		partial    bool
		step       time.Duration
	}
	grids := map[string][]gridRow{}
	identity := map[string]string{}
	for line, row := range rows[1:] {
		fail := func(message string) (WindowData, error) {
			return data, fmt.Errorf("sequence row %d: %s", line+2, message)
		}
		if strings.TrimSpace(row[0]) == "" {
			return fail("empty run identity")
		}
		class, err := scenario(row[1], row[2])
		if err != nil {
			return data, err
		}
		index, err := strconv.Atoi(row[3])
		if err != nil || index < 0 || index >= maxSequenceSteps {
			return fail("invalid step index")
		}
		start, err := time.Parse(time.RFC3339Nano, row[4])
		if err != nil || start.IsZero() {
			return fail("invalid step start")
		}
		end, err := time.Parse(time.RFC3339Nano, row[5])
		if err != nil || !end.After(start) {
			return fail("invalid step end")
		}
		count, err := strconv.Atoi(row[6])
		if err != nil || count < 0 {
			return fail("invalid sample count")
		}
		partial, err := strconv.ParseBool(row[7])
		if err != nil {
			return fail("invalid partial flag")
		}
		verified, err := strconv.ParseBool(row[8])
		if err != nil {
			return fail("invalid protocol flag")
		}
		if err := runrecord.ValidateSessionID(row[9]); err != nil {
			return data, err
		}
		seconds, err := strconv.ParseFloat(row[10], 64)
		if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 1 || seconds > 3600 {
			return fail("invalid step duration")
		}
		step := time.Duration(seconds * float64(time.Second))
		if step.Seconds() != seconds || end.Sub(start) > step || partial != (end.Sub(start) < step) {
			return fail("inconsistent step duration/partial flag")
		}
		key := class + "/" + row[8] + "/" + row[9] + "/" + row[10]
		if prior, ok := identity[row[0]]; ok && prior != key {
			return fail("inconsistent run label, protocol, session or cadence")
		}
		identity[row[0]] = key
		out := WindowRow{RunID: row[0], Label: row[1], Target: row[2], SessionID: row[9], WindowStart: start, WindowEnd: end, ProtocolVerified: verified, Features: map[string]float64{}}
		observed := 0
		for i, signal := range WindowSignals {
			value, mask := row[11+2*i], row[12+2*i]
			if mask != "0" && mask != "1" || (mask == "0") != (value == "") {
				return fail("inconsistent signal value/mask")
			}
			if mask == "1" {
				x, err := strconv.ParseFloat(value, 64)
				if err != nil || math.IsNaN(x) || math.IsInf(x, 0) || x < 0 {
					return fail("invalid observed signal")
				}
				out.Features[signal+"_value"] = x
				observed++
			}
			m := 0.
			if mask == "1" {
				m = 1
			}
			out.Features[signal+"_mask"] = m
		}
		if count == 0 && observed != 0 || count > 0 && observed == 0 {
			return fail("sample count disagrees with signal observations")
		}
		grids[row[0]] = append(grids[row[0]], gridRow{index, start, end, partial, step})
		data.Rows = append(data.Rows, out)
	}
	for id, grid := range grids {
		sort.Slice(grid, func(i, j int) bool { return grid[i].index < grid[j].index })
		for i, row := range grid {
			if row.index != i || !row.start.Equal(grid[0].start.Add(time.Duration(i)*row.step)) || row.partial && i != len(grid)-1 {
				return data, fmt.Errorf("run %s has an incomplete, overlapping or inconsistent sequence grid", id)
			}
		}
	}
	return data, nil
}
