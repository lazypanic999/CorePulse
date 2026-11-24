package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// row builds one raw telemetry line. Pods are given as name:cpu:memory with an
// optional @sourceTimestamp/window; a pod without @ has unknown metadata.
func row(capture string, pods ...string) string {
	var parts []string
	for _, pod := range pods {
		spec, meta, _ := strings.Cut(pod, "@")
		fields := strings.Split(spec, ":")
		entry := `{"name":"` + fields[0] + `","cpu_millicores":` + fields[1] + `,"memory_bytes":` + fields[2]
		if meta != "" {
			at, window, _ := strings.Cut(meta, "/")
			entry += `,"measurement":{"timestamp":"` + at + `","window":"` + window + `"}`
		}
		parts = append(parts, entry+"}")
	}
	return `{"schema_version":1,"timestamp":"` + capture + `","source":"metrics.k8s.io/v1beta1","context":"open5gs-lab","namespace":"open5gs","cpu_millicores":0,"memory_bytes":0,"pod_count":` +
		string(rune('0'+len(pods))) + `,"pods":[` + strings.Join(parts, ",") + `]}`
}

func writeFixture(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func podByName(t *testing.T, report FrameReport, name string) PodFrames {
	t.Helper()
	for _, pod := range report.Pods {
		if pod.Name == name {
			return pod
		}
	}
	t.Fatalf("pod %s missing from %+v", name, report.Pods)
	return PodFrames{}
}

func TestFrameReportSeparatesRepeatedFramesFromEqualValues(t *testing.T) {
	path := writeFixture(t,
		// Three AMF captures 8 s apart re-read one source frame.
		row("2026-11-09T00:00:10Z", "amf:3:100@2026-11-09T00:00:00Z/1m0s", "legacy:1:1"),
		row("2026-11-09T00:00:18Z", "amf:3:100@2026-11-09T00:00:00Z/1m0s", "legacy:1:1"),
		row("2026-11-09T00:00:26Z", "amf:3:100@2026-11-09T00:00:00Z/1m0s", "legacy:1:1"),
		// A new frame with the same values is still a distinct frame.
		row("2026-11-09T00:01:05Z", "amf:3:100@2026-11-09T00:01:00Z/1m0s", "legacy:1:1"),
	)
	report, err := AnalyzeFrames(path)
	if err != nil {
		t.Fatal(err)
	}
	amf := podByName(t, report, "amf")
	if amf.Captures != 4 || amf.Known != 4 || amf.UniqueFrames != 2 || amf.RepeatedCaptures != 2 || amf.EqualValueFrames != 1 || amf.ConflictingFrames != 0 {
		t.Fatalf("frame counts: %+v", amf)
	}
	if amf.Age == nil || amf.Age.Min != 5 || amf.Age.Max != 26 || amf.Age.Median != 14 {
		t.Fatalf("ages: %+v", amf.Age)
	}
	if !reflect.DeepEqual(amf.Windows, []string{"1m0s"}) {
		t.Fatalf("windows: %v", amf.Windows)
	}
	legacy := podByName(t, report, "legacy")
	if legacy.Known != 0 || legacy.Unknown != 4 || legacy.UniqueFrames != 0 || legacy.Age != nil || len(legacy.Windows) != 0 {
		t.Fatalf("legacy rows were given source metadata: %+v", legacy)
	}
}

func TestFrameReportFlagsConflictsSkewRegressionAndReuse(t *testing.T) {
	path := writeFixture(t,
		row("2026-11-09T00:00:10Z", "upf:5:200@2026-11-09T00:00:00Z/30s"),
		// Same frame, different value: a conflict, counted once.
		row("2026-11-09T00:00:20Z", "upf:6:200@2026-11-09T00:00:00Z/30s"),
		row("2026-11-09T00:00:25Z", "upf:7:200@2026-11-09T00:00:00Z/30s"),
		// Source ahead of capture: clock skew, excluded from ages.
		row("2026-11-09T00:00:30Z", "upf:5:200@2026-11-09T00:00:40Z/30s"),
		// The name is absent for one row, then returns with an older source.
		row("2026-11-09T00:00:40Z", "smf:1:1@2026-11-09T00:00:35Z/30s"),
		row("2026-11-09T00:00:50Z", "upf:5:200@2026-11-09T00:00:20Z/30.5s"),
		// Capture clock regresses between rows.
		row("2026-11-09T00:00:45Z", "upf:5:200@2026-11-09T00:00:20Z/30.5s"),
	)
	report, err := AnalyzeFrames(path)
	if err != nil {
		t.Fatal(err)
	}
	upf := podByName(t, report, "upf")
	if upf.ConflictingFrames != 1 || upf.FutureSource != 1 || upf.RegressingSource != 2 || upf.Reappearances != 1 {
		t.Fatalf("upf: %+v", upf)
	}
	if upf.UniqueFrames != 3 || upf.RepeatedCaptures != 3 {
		t.Fatalf("upf frames: %+v", upf)
	}
	if !reflect.DeepEqual(upf.Windows, []string{"30.5s", "30s"}) {
		t.Fatalf("windows: %v", upf.Windows)
	}
	if report.CaptureOrder.Regressing != 1 || report.Rows != 7 {
		t.Fatalf("capture order: %+v rows %d", report.CaptureOrder, report.Rows)
	}
}

func TestFrameReportIsDeterministic(t *testing.T) {
	lines := []string{
		row("2026-11-09T00:00:10Z", "b:1:1@2026-11-09T00:00:00Z/1m0s", "a:2:2@2026-11-09T00:00:01Z/1m0s"),
		row("2026-11-09T00:00:20Z", "a:2:2@2026-11-09T00:00:01Z/1m0s", "b:1:1@2026-11-09T00:00:00Z/1m0s"),
	}
	first, err := AnalyzeFrames(writeFixture(t, lines...))
	if err != nil {
		t.Fatal(err)
	}
	second, err := AnalyzeFrames(writeFixture(t, lines...))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) || first.Pods[0].Name != "a" || first.InputSHA256 == "" {
		t.Fatalf("report not deterministic:\n%s\n%s", a, b)
	}
}

func TestFrameReportRejectsMalformedInput(t *testing.T) {
	good := row("2026-11-09T00:00:10Z", "amf:1:1@2026-11-09T00:00:00Z/1m0s")
	cases := map[string][]string{
		"not json":            {good, "{"},
		"trailing data":       {good + " {}"},
		"blank line":          {good, "", good},
		"wrong context":       {strings.Replace(good, "open5gs-lab", "production", 1)},
		"wrong source":        {strings.Replace(good, "metrics.k8s.io/v1beta1", "prometheus", 1)},
		"wrong schema":        {strings.Replace(good, `"schema_version":1`, `"schema_version":2`, 1)},
		"zero capture":        {strings.Replace(good, "2026-11-09T00:00:10Z", "0001-01-01T00:00:00Z", 1)},
		"partial measurement": {strings.Replace(good, `,"window":"1m0s"`, "", 1)},
		"zero window":         {strings.Replace(good, "1m0s", "0s", 1)},
		"duplicate pod":       {row("2026-11-09T00:00:10Z", "amf:1:1", "amf:1:1")},
		"unnamed pod":         {row("2026-11-09T00:00:10Z", ":1:1")},
		"empty input":         {},
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "telemetry.jsonl")
			content := strings.Join(lines, "\n")
			if len(lines) > 0 {
				content += "\n"
			}
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := AnalyzeFrames(path); err == nil {
				t.Fatal("malformed input accepted")
			}
		})
	}
}

func TestFailedAnalysisPreservesPriorReport(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "source-frames.json")
	report, err := AnalyzeFrames(writeFixture(t, row("2026-11-09T00:00:10Z", "amf:1:1@2026-11-09T00:00:00Z/1m0s")))
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFrameReport(output, report); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(output)
	if _, err := AnalyzeFrames(writeFixture(t, "{")); err == nil {
		t.Fatal("malformed input accepted")
	}
	after, _ := os.ReadFile(output)
	if string(before) != string(after) || len(before) == 0 {
		t.Fatal("prior report changed")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}
