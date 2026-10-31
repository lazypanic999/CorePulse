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
	"path/filepath"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/dashboard"
	"github.com/lazypanic999/CorePulse/internal/dataset"
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
		return 1, errors.New("usage: corepulse <version|discover|pod-crash|dataset|serve> [options]")
	}
	client := corepulse.Kubectl{}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			return 1, errors.New("version takes no arguments")
		}
		fmt.Println(version)
		return 0, nil
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
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		addr := flags.String("addr", "127.0.0.1:8787", "local dashboard address")
		runsDir := flags.String("runs-dir", "runs", "run record directory")
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
		return 1, http.ListenAndServe(*addr, dashboard.NewWithTelemetry(client, *runsDir, store))
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
		result, err := corepulse.RunPodCrash(client, *target)
		if err != nil {
			return 1, err
		}
		path := filepath.Join(*runsDir, result["run_id"].(string)+".json")
		if err := saveJSON(path, result); err != nil {
			return 1, err
		}
		result["run_record"] = path
		if err := printJSON(result); err != nil {
			return 1, err
		}
		if result["status"] != "recovered" {
			return 2, nil
		}
		return 0, nil
	default:
		return 1, fmt.Errorf("unknown command %q; use version, discover, pod-crash, dataset, or serve", args[0])
	}
}

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "corepulse:", err)
	}
	os.Exit(code)
}
