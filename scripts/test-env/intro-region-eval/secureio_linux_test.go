//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

func TestRegularReadsRejectSymlinksFIFOAndOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte("1234"), 0600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "link")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatal(err)
	}
	parentLink := filepath.Join(dir, "parent")
	if err := os.Symlink(dir, parentLink); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{symlink, filepath.Join(parentLink, "regular"), fifo, dir, regular} {
		if _, err := readRegular(context.Background(), path, 3); err == nil {
			t.Fatalf("unsafe or oversized input accepted: %s", path)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readRegular(ctx, regular, 4); err != context.Canceled {
		t.Fatalf("cancellation ignored: %v", err)
	}
}

func TestReportPublicationIsPrivateAtomicAndExclusive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	a, err := newReportWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := newReportWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unfinished report became visible")
	}
	var wait sync.WaitGroup
	wait.Add(2)
	results := make(chan error, 2)
	go func() { defer wait.Done(); results <- a.Publish([]byte("first complete report")) }()
	go func() { defer wait.Done(); results <- b.Publish([]byte("second complete report")) }()
	wait.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("exclusive publication had %d successes", success)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first complete report" && string(data) != "second complete report" {
		t.Fatal("report was partial or mixed")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("report mode is %o", info.Mode().Perm())
	}
	if writer, err := newReportWriter(path); err == nil {
		writer.Close()
		t.Fatal("existing evidence accepted for overwrite")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("existing evidence changed")
	}
}

func TestCLIRejectsOldModeAndRetainsFailureReport(t *testing.T) {
	var stderr bytes.Buffer
	never := func(map[string]source, *budget) (map[string]any, error) {
		t.Fatal("runner unexpectedly executed")
		return nil, nil
	}
	if code := runCLI(context.Background(), []string{"-mode", "actions"}, &stderr, never); code != 2 {
		t.Fatalf("legacy mode returned %d", code)
	}
	dir := t.TempDir()
	output := filepath.Join(dir, "failure.json")
	args := []string{"-manifest", filepath.Join(dir, "missing.json"), "-output", output}
	if code := runCLI(context.Background(), args, &stderr, never); code != 1 {
		t.Fatalf("missing input returned %d", code)
	}
	before, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var report evaluationReport
	if err := json.Unmarshal(before, &report); err != nil {
		t.Fatal(err)
	}
	if report.Completed || report.ProductionResult || report.Status != "input-rejected" || report.Implementation.SHA256 == "" {
		t.Fatal("failure report lost its meaning")
	}
	if code := runCLI(context.Background(), args, &stderr, never); code != 1 {
		t.Fatal("existing failure report was not rejected")
	}
	after, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failure evidence was overwritten")
	}
}

func TestCLICancellationPublishesAdmittedSourcesWithoutGroups(t *testing.T) {
	path, _ := cohortFixture(t)
	output := filepath.Join(filepath.Dir(path), "canceled.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stderr bytes.Buffer
	code := runCLI(ctx, []string{"-manifest", path, "-output", output}, &stderr, func(_ map[string]source, work *budget) (map[string]any, error) {
		cancel()
		return map[string]any{"complete": true, "groups": []groupWitness{{AnchorStart: 1, AnchorEnd: 11}}}, work.clock()
	})
	if code != 1 {
		t.Fatalf("canceled CLI returned %d", code)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Completed bool             `json:"completed"`
		Status    string           `json:"status"`
		Admitted  []admittedSource `json:"admittedSources"`
		Prefix    struct {
			Groups []groupWitness `json:"groups"`
		} `json:"prefix"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Completed || report.Status != "canceled" || len(report.Admitted) != 3 || len(report.Prefix.Groups) != 0 {
		t.Fatalf("invalid canceled report: %+v", report)
	}
}

func TestUnpublishedWriteFailureLeavesNoFinalReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	w, err := newReportWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Publish([]byte("complete bytes")); err == nil {
		t.Fatal("failed write unexpectedly published")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("failed write exposed a partial report")
	}
}

func TestCLIOperationalTimeoutHasFixedBounds(t *testing.T) {
	for _, timeout := range []string{"0", "-1s", "10m1s"} {
		var stderr bytes.Buffer
		code := runCLI(context.Background(), []string{"-manifest", "unused.json", "-output", "unused-output.json", "-timeout", timeout}, &stderr, nil)
		if code != 2 {
			t.Fatalf("invalid timeout %q returned %d", timeout, code)
		}
	}
}
