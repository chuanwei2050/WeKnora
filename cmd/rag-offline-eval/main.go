package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Tencent/WeKnora/internal/rageval"
)

type dataset struct {
	DatasetVersion string         `json:"dataset_version"`
	ConfigSnapshot map[string]any `json:"config_snapshot"`
	K              int            `json:"k"`
	Cases          []rageval.Case `json:"cases"`
}

func main() {
	inputPath := flag.String("input", "", "path to a versioned offline RAG dataset")
	outputPath := flag.String("output", "", "optional path for the JSON report")
	flag.Parse()
	if *inputPath == "" {
		fatalf("-input is required")
	}
	data, err := os.ReadFile(*inputPath)
	if err != nil {
		fatalf("read input: %v", err)
	}
	var input dataset
	if err := json.Unmarshal(data, &input); err != nil {
		fatalf("decode input: %v", err)
	}
	if input.DatasetVersion == "" || input.K < 1 || len(input.Cases) == 0 {
		fatalf("dataset_version, positive k, and cases are required")
	}
	report := rageval.Evaluate(input.DatasetVersion, input.ConfigSnapshot, input.Cases, input.K)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatalf("encode report: %v", err)
	}
	encoded = append(encoded, '\n')
	if *outputPath == "" {
		_, _ = os.Stdout.Write(encoded)
		return
	}
	if err := os.WriteFile(*outputPath, encoded, 0o644); err != nil {
		fatalf("write report: %v", err)
	}
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}
