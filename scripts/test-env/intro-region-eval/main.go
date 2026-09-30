// Command intro-region-eval evaluates one frozen three-source research cohort.
// It has no database, player, detector-output, or network integration.
package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const reportProtocol = "intro-region-research-v1"
const executionDeadline = 2 * time.Minute
const maximumDeadline = 10 * time.Minute
const maxReportBytes = 32 << 20

// Embed the implementation itself, including the wrapper, so every report
// binds the exact source bytes used at build time without trusting a Git state.
//
//go:embed *.go
var implementationFiles embed.FS

type implementationBinding struct {
	SHA256       string            `json:"sha256"`
	Files        map[string]string `json:"files"`
	HashEncoding string            `json:"hashEncoding"`
}

func implementationIdentity() implementationBinding {
	entries, _ := implementationFiles.ReadDir(".")
	names := []string{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "_test.go") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	h := sha256.New()
	identity := implementationBinding{Files: map[string]string{}, HashEncoding: "Sorted non-test Go filenames and content; each field is prefixed by its decimal byte length and a colon."}
	for _, name := range names {
		data, _ := implementationFiles.ReadFile(name)
		fmt.Fprintf(h, "%d:%s%d:", len(name), name, len(data))
		h.Write(data)
		identity.Files[name] = digest(data)
	}
	identity.SHA256 = hex.EncodeToString(h.Sum(nil))
	return identity
}

type evaluationReport struct {
	Protocol           string                `json:"protocol"`
	SchemaVersion      int                   `json:"schemaVersion"`
	Mode               string                `json:"mode"`
	ResultKind         string                `json:"resultKind"`
	ProductionResult   bool                  `json:"productionResult"`
	IndependentHeldout bool                  `json:"independentHeldout"`
	Completed          bool                  `json:"completed"`
	Status             string                `json:"status"`
	Stage              string                `json:"stage"`
	Error              string                `json:"error,omitempty"`
	Ambiguous          bool                  `json:"ambiguous"`
	InputPath          string                `json:"inputPath"`
	InputSHA256        string                `json:"inputSha256,omitempty"`
	DerivedFrom        *manifestOrigin       `json:"derivedFrom,omitempty"`
	Implementation     implementationBinding `json:"implementation"`
	AdmittedSources    []admittedSource      `json:"admittedSources"`
	Prefix             map[string]any        `json:"prefix"`
	Work               *budget               `json:"work"`
	Limits             map[string]any        `json:"limits"`
	StartedUTC         time.Time             `json:"startedUTC"`
	FinishedUTC        time.Time             `json:"finishedUTC"`
}

type prefixRunner func(map[string]source, *budget) (map[string]any, error)

func isBudgetFailure(err error) bool {
	// These exact errors are emitted by the frozen engine. Input path/error
	// text must never be interpreted as computational budget evidence.
	switch err.Error() {
	case "cohort hypothesis budget exceeded", "patch comparison budget exceeded", "clock lookup budget exceeded", "render pixel budget exceeded":
		return true
	}
	return false
}

func evaluate(ctx context.Context, path string, runner prefixRunner) evaluationReport {
	work := &budget{ctx: ctx, stage: "input-admission"}
	r := evaluationReport{Protocol: reportProtocol, SchemaVersion: 1, Mode: "full-prefix", Status: "failed", Stage: work.stage, InputPath: path, Implementation: implementationIdentity(), AdmittedSources: []admittedSource{}, Prefix: map[string]any{"complete": false, "productionResult": false, "groups": []groupWitness{}}, Work: work, StartedUTC: time.Now().UTC(), Limits: map[string]any{"deadlineSeconds": int(executionDeadline.Seconds()), "patchComparisons": maxPatchComparisons, "clockLookups": maxClockLookups, "renderPixels": maxRenderPixels, "retainedHypotheses": maxHypotheses, "inputManifestBytes": maxInputManifestBytes, "extractionManifestBytes": maxExtractionBytes, "gzipBytesPerSource": maxGzipBytes, "rawBytesPerSource": expectedFrames * frameBytes}}
	r.ResultKind = "research-candidates-with-observed-bounds"
	finish := func(err error) evaluationReport {
		r.FinishedUTC = time.Now().UTC()
		r.Stage = work.stage
		if err != nil {
			r.Error = err.Error()
			r.Completed = false
			r.Prefix["groups"] = []groupWitness{}
			r.Prefix["complete"] = false
			if summary, ok := r.Prefix["closure"].(closureSummary); ok {
				summary.RejectedAmbiguousGroups = nil
				r.Prefix["closure"] = summary
			}
			switch {
			case errors.Is(err, context.Canceled):
				r.Status = "canceled"
			case errors.Is(err, context.DeadlineExceeded):
				r.Status = "deadline-exceeded"
			case work.stage == "input-admission":
				r.Status = "input-rejected"
			case isBudgetFailure(err):
				r.Status = "budget-exhausted"
			default:
				r.Status = "evaluation-failed"
			}
		}
		return r
	}
	data, err := readRegular(ctx, path, maxInputManifestBytes)
	if err != nil {
		return finish(err)
	}
	r.InputSHA256 = digest(data)
	manifest, err := parseInput(data, filepath.Dir(path))
	if err != nil {
		return finish(err)
	}
	if manifest.DerivedFrom != nil {
		upstream, err := readRegular(ctx, manifest.DerivedFrom.ManifestPath, maxInputManifestBytes)
		if err != nil {
			return finish(fmt.Errorf("upstream manifest: %w", err))
		}
		if digest(upstream) != manifest.DerivedFrom.SHA256 {
			return finish(errors.New("upstream manifest SHA256 mismatch"))
		}
		r.DerivedFrom = manifest.DerivedFrom
	}
	sources := map[string]source{}
	for _, info := range manifest.Sources {
		s, admitted, err := loadSource(ctx, info)
		if err != nil {
			return finish(fmt.Errorf("source %s: %w", info.ID, err))
		}
		sources[info.ID] = s
		r.AdmittedSources = append(r.AdmittedSources, admitted)
	}
	work.stage = "full-prefix"
	prefix, err := runner(sources, work)
	if prefix != nil {
		r.Prefix = prefix
	}
	r.Prefix["productionResult"] = false
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return finish(err)
	}
	complete, _ := r.Prefix["complete"].(bool)
	if !complete {
		return finish(errors.New("matcher returned without complete evidence"))
	}
	if stage, ok := r.Prefix["stopStage"].(string); ok {
		work.stage = stage
	}
	if summary, ok := r.Prefix["closure"].(closureSummary); ok {
		r.Ambiguous = summary.CohortBoundaryAmbiguous
	}
	r.Completed = true
	groups, _ := r.Prefix["groups"].([]groupWitness)
	if len(groups) > 0 {
		r.Status = "completed-with-groups"
	} else if r.Ambiguous {
		r.Status = "completed-ambiguous-abstention"
	} else {
		r.Status = "completed-abstention"
	}
	return finish(nil)
}

func runCLI(ctx context.Context, args []string, stderr io.Writer, runner prefixRunner) int {
	flags := flag.NewFlagSet("intro-region-eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("manifest", "", "frozen three-source JSON manifest")
	output := flags.String("output", "", "new private research report path (must not exist)")
	timeout := flags.Duration("timeout", executionDeadline, "operational timeout, greater than zero and at most 10m; detection budgets remain fixed")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *manifest == "" || *output == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "exactly -manifest and -output are required")
		return 2
	}
	if *timeout <= 0 || *timeout > maximumDeadline {
		fmt.Fprintln(stderr, "timeout must be greater than zero and at most 10m")
		return 2
	}
	inputPath, err := canonicalPath(".", *manifest)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	outputPath, err := canonicalPath(".", *output)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	writer, err := newReportWriter(outputPath)
	if err != nil {
		fmt.Fprintln(stderr, "cannot reserve a new report:", err)
		return 1
	}
	defer writer.Close()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	report := evaluate(ctx, inputPath, runner)
	report.Limits["deadlineSeconds"] = timeout.Seconds()
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "cannot encode report:", err)
		return 1
	}
	if len(data) >= maxReportBytes {
		fmt.Fprintln(stderr, "report exceeds fixed output bound")
		return 1
	}
	if err := writer.Publish(append(data, '\n')); err != nil {
		fmt.Fprintln(stderr, "cannot publish report:", err)
		return 1
	}
	fmt.Fprintf(stderr, "%s: %s; report=%s\n", report.Status, report.Stage, outputPath)
	if !report.Completed {
		return 1
	}
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runCLI(ctx, os.Args[1:], os.Stderr, completePrefix))
}
