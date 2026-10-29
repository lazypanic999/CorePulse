package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
)

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
		return 1, errors.New("usage: corepulse <discover|pod-crash> [options]")
	}
	client := corepulse.Kubectl{}
	switch args[0] {
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
		return 1, fmt.Errorf("unknown command %q; use discover or pod-crash", args[0])
	}
}

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "corepulse:", err)
	}
	os.Exit(code)
}
