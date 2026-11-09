package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/lazypanic999/CorePulse/internal/collection"
	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/dashboard"
	"github.com/lazypanic999/CorePulse/internal/dataset"
	"github.com/lazypanic999/CorePulse/internal/evaluation"
	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

// Set by release builds; source builds report dev.
var version = "dev"

func printJSON(value any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func saveJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func run(args []string) (int, error) {
	if len(args) == 0 {
		return 1, errors.New("usage: corepulse <version|discover|signals|verify-service|pod-crash|baseline|dataset|windows|split|evaluate|serve> [options]")
	}
	client := corepulse.Kubectl{}
	switch args[0] {
	case "signals":
		if len(args) != 1 {
			return 1, errors.New("signals takes no arguments")
		}
		result, err := corepulse.InspectSignals(client)
		if err != nil {
			return 1, err
		}
		return 0, printJSON(result)
	case "verify-service":
		if len(args) != 1 {
			return 1, errors.New("verify-service takes no arguments")
		}
		probe, err := corepulse.ProbeService(client)
		if err != nil {
			return 1, err
		}
		if err := printJSON(probe); err != nil {
			return 1, err
		}
		if probe.Status != "healthy" {
			return 2, nil
		}
		return 0, nil
	case "version":
		if len(args) != 1 {
			return 1, errors.New("version takes no arguments")
		}
		fmt.Println(version)
		return 0, nil
	case "baseline":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		client := corepulse.Kubectl{Context: ctx}
		flags := flag.NewFlagSet("baseline", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		runs := flags.Int("runs", 3, "number of separate verified baseline runs")
		samples := flags.Int("samples", 4, "samples per run")
		interval := flags.Duration("interval", 10*time.Second, "interval between samples")
		metricsWait := flags.Duration("metrics-wait", 0, "optional pre-run wait for current pod metric coverage (up to 5m)")
		telemetryPath := flags.String("telemetry", filepath.Join("data", "telemetry.jsonl"), "raw telemetry JSONL")
		runsDir := flags.String("runs-dir", "runs", "run record directory")
		output := flags.String("output", filepath.Join("data", "dataset.csv"), "curated CSV output")
		if err := flags.Parse(args[1:]); err != nil {
			return 1, err
		}
		if flags.NArg() != 0 || *runs < 1 || *runs > 100 {
			return 1, errors.New("baseline needs 1 to 100 runs and no positional arguments")
		}
		if *samples < 2 || *interval < time.Second || *metricsWait < 0 || *metricsWait > 5*time.Minute {
			return 1, errors.New("baseline needs at least two samples, interval >= 1s, and metrics-wait between 0 and 5m")
		}
		return collection.WithOwnership("baseline", func() (int, error) {
			store := telemetry.NewStore(*telemetryPath)
			for i := 0; i < *runs; i++ {
				if *metricsWait > 0 {
					ready, err := collection.WaitForMetrics(ctx, client, *metricsWait)
					if err != nil {
						if errors.Is(err, context.Canceled) {
							return 130, err
						}
						return 1, err
					}
					fmt.Fprintf(os.Stderr, "Resource metrics cover %d essential pods after %d checks.\n", len(ready.Pods), ready.Checks)
				}
				record, err := collection.BaselineContext(ctx, client, store, *runsDir, *samples, *interval)
				if record.RunID != "" {
					_ = printJSON(record)
				}
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return 130, err
					}
					return 1, err
				}
			}
			summary, err := dataset.Build(*telemetryPath, *runsDir, *output)
			if err != nil {
				return 1, err
			}
			return 0, printJSON(summary)
		})
	case "dataset":
		flags := flag.NewFlagSet("dataset", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		telemetryPath := flags.String("telemetry", filepath.Join("data", "telemetry.jsonl"), "raw telemetry JSONL")
		runsDir := flags.String("runs-dir", "runs", "run record directory")
		output := flags.String("output", filepath.Join("data", "dataset.csv"), "labeled CSV output")
		if err := flags.Parse(args[1:]); err != nil {
			return 1, err
		}
		if flags.NArg() != 0 {
			return 1, errors.New("unexpected positional arguments")
		}
		summary, err := dataset.Build(*telemetryPath, *runsDir, *output)
		if err != nil {
			return 1, err
		}
		return 0, printJSON(summary)
	case "windows":
		flags := flag.NewFlagSet("windows", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		raw := flags.String("telemetry", filepath.Join("data", "telemetry.jsonl"), "raw telemetry JSONL")
		runs := flags.String("runs-dir", "runs", "run record directory")
		output := flags.String("output", filepath.Join("data", "windows.csv"), "run-aligned feature windows")
		duration := flags.Duration("window", 30*time.Second, "non-overlapping window duration (1s to 1h)")
		legacy := flags.Bool("allow-legacy", false, "include clean runs without protocol starting evidence (exploration only)")
		if err := flags.Parse(args[1:]); err != nil {
			return 1, err
		}
		if flags.NArg() != 0 {
			return 1, errors.New("unexpected positional arguments")
		}
		summary, err := dataset.BuildWindows(*raw, *runs, *output, dataset.WindowOptions{Duration: *duration, RequireProtocol: !*legacy})
		if err != nil {
			return 1, err
		}
		return 0, printJSON(summary)
	case "split":
		flags := flag.NewFlagSet("split", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		input := flags.String("input", filepath.Join("data", "windows.csv"), "window CSV input")
		output := flags.String("output", filepath.Join("data", "split.json"), "whole-run split manifest")
		fraction := flags.Float64("test-fraction", 0.25, "held-out fraction per experiment scenario")
		seed := flags.Int64("seed", 42, "deterministic run split seed")
		embargo := flags.Duration("embargo", 0, "purge training runs near held-out capture ranges (up to 24h)")
		legacy := flags.Bool("allow-legacy", false, "allow unverified protocol starts (exploration only)")
		if err := flags.Parse(args[1:]); err != nil {
			return 1, err
		}
		if flags.NArg() != 0 {
			return 1, errors.New("unexpected positional arguments")
		}
		manifest, err := dataset.Split(*input, *output, dataset.SplitOptions{TestFraction: *fraction, Seed: *seed, RequireProtocol: !*legacy, Embargo: *embargo})
		if err != nil {
			return 1, err
		}
		return 0, printJSON(manifest)
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		addr := flags.String("addr", "127.0.0.1:8787", "local dashboard address")
		runsDir := flags.String("runs-dir", "runs", "run record directory")
		evaluationPath := flags.String("evaluation", filepath.Join("reports", "evaluation.json"), "saved offline evaluation report to display")
		if err := flags.Parse(args[1:]); err != nil {
			return 1, err
		}
		if flags.NArg() != 0 {
			return 1, errors.New("unexpected positional arguments")
		}
		host, _, err := net.SplitHostPort(*addr)
		if err != nil {
			return 1, fmt.Errorf("invalid dashboard address: %w", err)
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return 1, errors.New("dashboard must bind to a loopback address")
		}
		fmt.Fprintf(os.Stderr, "CorePulse dashboard: http://%s\n", *addr)
		store := telemetry.NewStore(filepath.Join("data", "telemetry.jsonl"))
		store.Start(context.Background(), client, 10*time.Second)
		return 1, http.ListenAndServe(*addr, dashboard.NewWithEvaluation(client, *runsDir, store, *evaluationPath))
	case "evaluate":
		flags := flag.NewFlagSet("evaluate", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		input := flags.String("input", filepath.Join("data", "windows.csv"), "window CSV input")
		split := flags.String("split", filepath.Join("data", "split.json"), "whole-run split manifest")
		output := flags.String("output", filepath.Join("reports", "evaluation.json"), "offline baseline evaluation report")
		threshold := flags.Float64("z-threshold", 3, "normal-only maximum absolute z-score threshold")
		legacy := flags.Bool("allow-legacy", false, "allow explicitly exploratory legacy splits")
		if err := flags.Parse(args[1:]); err != nil {
			return 1, err
		}
		if flags.NArg() != 0 {
			return 1, errors.New("unexpected positional arguments")
		}
		report, err := evaluation.Evaluate(*input, *split, *output, evaluation.Options{Threshold: *threshold, AllowLegacy: *legacy})
		if err != nil {
			return 1, err
		}
		return 0, printJSON(report)
	case "discover":
		flags := flag.NewFlagSet("discover", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		snapshot := flags.String("snapshot", "", "analyze a saved Kubernetes List")
		output := flags.String("output", "", "save the JSON report")
		if err := flags.Parse(args[1:]); err != nil {
			return 1, err
		}
		if flags.NArg() != 0 {
			return 1, errors.New("unexpected positional arguments")
		}
		var result map[string]any
		var err error
		if *snapshot == "" {
			result, err = corepulse.Discover(client)
		} else {
			data, readErr := os.ReadFile(*snapshot)
			if readErr != nil {
				return 1, readErr
			}
			var source map[string]any
			if err = json.Unmarshal(data, &source); err == nil {
				result, err = corepulse.AnalyzeSnapshot(source, "offline-snapshot")
			}
		}
		if err != nil {
			return 1, err
		}
		if *output != "" {
			if err := saveJSON(*output, result); err != nil {
				return 1, err
			}
		}
		return 0, printJSON(result)
	case "pod-crash":
		flags := flag.NewFlagSet("pod-crash", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		target := flags.String("target", "", "amf or upf")
		apply := flags.Bool("apply", false, "delete one lab pod")
		runsDir := flags.String("runs-dir", "runs", "run record directory")
		if err := flags.Parse(args[1:]); err != nil {
			return 1, err
		}
		if flags.NArg() != 0 {
			return 1, errors.New("unexpected positional arguments")
		}
		if !*apply {
			plan, err := corepulse.PlanPodCrash(*target)
			if err != nil {
				return 1, err
			}
			plan["mode"] = "dry-run"
			return 0, printJSON(plan)
		}
		return collection.WithOwnership("pod-crash:"+*target, func() (int, error) {
			preProbe, err := corepulse.ProbeService(client)
			if err != nil {
				return 1, err
			}
			if preProbe.Status != "healthy" {
				return 1, fmt.Errorf("fault run requires a healthy UE-to-UPF path before injection: %s", preProbe.Detail)
			}
			if _, err := corepulse.StableLabPods(client); err != nil {
				return 1, fmt.Errorf("fault run requires ready essential workloads: %w", err)
			}
			store := telemetry.NewStore(filepath.Join("data", "telemetry.jsonl"))
			result, err := collection.RunFault(client, *target, *runsDir, preProbe, func() error { return store.Capture(client) })
			if err != nil {
				return 1, err
			}
			path := filepath.Join(*runsDir, result["run_id"].(string)+".json")
			result["run_record"] = path
			if err := printJSON(result); err != nil {
				return 1, err
			}
			if result["status"] != "recovered" || result["service_recovery"].(collection.Recovery).Status != "service_recovered" || collection.ControlPlaneLost(result) {
				return 2, nil
			}
			return 0, nil
		})
	default:
		return 1, fmt.Errorf("unknown command %q; use version, discover, signals, verify-service, pod-crash, baseline, dataset, windows, split, evaluate, or serve", args[0])
	}
}

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "corepulse:", err)
	}
	os.Exit(code)
}
