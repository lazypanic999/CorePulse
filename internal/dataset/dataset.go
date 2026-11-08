package dataset

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

type Run struct {
	ID       string
	Fault    string
	Target   string
	Started  time.Time
	Ended    time.Time
	evidence evidence
}

type Summary struct {
	Samples  int    `json:"samples"`
	Normal   int    `json:"normal"`
	Fault    int    `json:"fault"`
	Excluded int    `json:"excluded"`
	Runs     int    `json:"runs"`
	Output   string `json:"output"`
}

func loadRuns(dir string) ([]Run, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Run{}, nil
	}
	if err != nil {
		return nil, err
	}
	runs := []Run{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := runrecord.Read(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var raw struct {
			ID        string `json:"run_id"`
			Fault     string `json:"fault"`
			Target    string `json:"target"`
			Status    string `json:"status"`
			Quality   string `json:"quality"`
			Context   string `json:"context"`
			Namespace string `json:"namespace"`
			Started   string `json:"started_at"`
			Ended     string `json:"ended_at"`
			// Evidence written by baseline and fault runs. Older records
			// may lack any of these; their columns are then left empty.
			ServiceProbes   []probeJSON                 `json:"service_probes"`
			PreProbe        *probeJSON                  `json:"pre_service_probe"`
			ServiceProbe    *probeJSON                  `json:"service_probe"`
			ServiceRecovery *struct {
				Probes []probeJSON `json:"probes"`
			} `json:"service_recovery"`
			ProtocolEvents []corepulse.ComponentEvents `json:"protocol_events"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("run %s: %w", entry.Name(), err)
		}
		if raw.ID == "" || raw.Fault == "" || raw.Ended == "" || raw.Quality != "clean" || raw.Context != corepulse.LabContext || raw.Namespace != corepulse.LabNamespace {
			continue
		}
		if (raw.Fault == "normal" && raw.Status != "verified") || (raw.Fault != "normal" && raw.Status != "recovered") {
			continue
		}
		started, err := time.Parse(time.RFC3339Nano, raw.Started)
		if err != nil {
			return nil, fmt.Errorf("run %s start: %w", entry.Name(), err)
		}
		ended, err := time.Parse(time.RFC3339Nano, raw.Ended)
		if err != nil {
			return nil, fmt.Errorf("run %s end: %w", entry.Name(), err)
		}
		if !ended.After(started) {
			return nil, fmt.Errorf("run %s has invalid interval", entry.Name())
		}
		probes := [][]probeJSON{raw.ServiceProbes}
		if raw.PreProbe != nil {
			probes = append(probes, []probeJSON{*raw.PreProbe})
		}
		if raw.ServiceRecovery != nil && len(raw.ServiceRecovery.Probes) > 0 {
			probes = append(probes, raw.ServiceRecovery.Probes)
		} else if raw.ServiceProbe != nil {
			probes = append(probes, []probeJSON{*raw.ServiceProbe})
		}
		runs = append(runs, Run{ID: raw.ID, Fault: raw.Fault, Target: raw.Target, Started: started, Ended: ended, evidence: newEvidence(probes, raw.ProtocolEvents)})
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Started.Before(runs[j].Started) })
	return runs, nil
}

func label(sample telemetry.Sample, runs []Run) (string, *Run, error) {
	var match *Run
	for i := range runs {
		run := &runs[i]
		if sample.Timestamp.Before(run.Started) || sample.Timestamp.After(run.Ended) {
			continue
		}
		if match != nil {
			return "", nil, fmt.Errorf("sample %s overlaps multiple runs", sample.Timestamp.Format(time.RFC3339Nano))
		}
		match = run
	}
	if match == nil {
		return "excluded", nil, nil
	}
	return match.Fault, match, nil
}

func componentFields(pods []telemetry.PodUsage, component string) (string, string) {
	var cpu float64
	var memory int64
	present := false
	for _, pod := range pods {
		if strings.HasPrefix(pod.Name, component+"-") {
			present = true
			cpu += pod.CPUMilli
			memory += pod.MemoryBytes
		}
	}
	if !present {
		return "", ""
	}
	return strconv.FormatFloat(cpu, 'f', 3, 64), strconv.FormatInt(memory, 10)
}

// Build joins immutable raw samples with run intervals and writes a compact CSV.
func Build(telemetryPath, runsDir, outputPath string) (Summary, error) {
	runs, err := loadRuns(runsDir)
	if err != nil {
		return Summary{}, err
	}
	input, err := os.Open(telemetryPath)
	if err != nil {
		return Summary{}, err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return Summary{}, err
	}
	temp, err := os.CreateTemp(filepath.Dir(outputPath), "dataset-*.csv")
	if err != nil {
		return Summary{}, err
	}
	defer os.Remove(temp.Name())
	writer := csv.NewWriter(temp)
	columns := append([]string{"timestamp", "run_id", "label", "target", "context", "namespace", "source", "pod_count", "cpu_millicores", "memory_bytes", "amf_cpu_millicores", "amf_memory_bytes", "upf_cpu_millicores", "upf_memory_bytes"}, evidenceColumns...)
	if err := writer.Write(columns); err != nil {
		temp.Close()
		return Summary{}, err
	}
	summary := Summary{Runs: len(runs), Output: outputPath}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var sample telemetry.Sample
		if err := json.Unmarshal(scanner.Bytes(), &sample); err != nil {
			temp.Close()
			return Summary{}, fmt.Errorf("invalid telemetry row: %w", err)
		}
		if sample.Context != corepulse.LabContext || sample.Namespace != corepulse.LabNamespace || sample.Source != telemetry.Source {
			temp.Close()
			return Summary{}, fmt.Errorf("telemetry provenance does not match the isolated lab")
		}
		label, run, err := label(sample, runs)
		if err != nil {
			temp.Close()
			return Summary{}, err
		}
		if label == "excluded" {
			summary.Excluded++
			continue
		}
		amfCPU, amfMem := componentFields(sample.Pods, "open5gs-amf")
		upfCPU, upfMem := componentFields(sample.Pods, "open5gs-upf")
		row := []string{
			sample.Timestamp.UTC().Format(time.RFC3339Nano), run.ID, label, run.Target, sample.Context, sample.Namespace, sample.Source,
			strconv.Itoa(sample.PodCount), strconv.FormatFloat(sample.CPUMilli, 'f', 3, 64), strconv.FormatInt(sample.MemoryBytes, 10),
			amfCPU, amfMem, upfCPU, upfMem,
		}
		row = append(row, run.evidence.fields(*run, sample.Timestamp)...)
		if err := writer.Write(row); err != nil {
			temp.Close()
			return Summary{}, err
		}
		summary.Samples++
		if label == "normal" {
			summary.Normal++
		} else {
			summary.Fault++
		}
	}
	if err := scanner.Err(); err != nil {
		temp.Close()
		return Summary{}, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		temp.Close()
		return Summary{}, err
	}
	if err := temp.Close(); err != nil {
		return Summary{}, err
	}
	if err := os.Rename(temp.Name(), outputPath); err != nil {
		return Summary{}, err
	}
	return summary, nil
}
