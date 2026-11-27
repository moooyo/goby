//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSeriesNFOAuditAdmissionWaitsForWorkerRetirement(t *testing.T) {
	retired, released := make(chan struct{}), make(chan struct{})
	releaseSeriesNFOAuditAfterRetirement(retired, func() { close(released) })
	select {
	case <-released:
		t.Fatal("audit released its admission before the worker retired")
	default:
	}
	close(retired)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("audit did not release admission when the worker retired")
	}
}

func TestSeriesNFOAuditCandidateBudgetAndRecheck(t *testing.T) {
	ctx := context.Background()
	const relative = "First Show (2004)"
	const nfo = `<tvshow><title>Another Show</title><year>2019</year></tvshow>`
	base := t.TempDir()
	path := filepath.Join(base, relative, "tvshow.nfo")
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(nfo), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sidecar := filepath.ToSlash(filepath.Join(relative, "tvshow.nfo"))
	accepted, err := parseObservedNFO(localNFOObservation{path: sidecar, data: []byte(nfo)}, "tvshow", nil)
	if err != nil {
		t.Fatal(err)
	}
	target := seriesNFOAuditTarget{id: "series-a", relative: relative, nfoPath: sidecar, digest: accepted.hash}
	remaining := int64(len(nfo))
	if _, err := readSeriesNFOAuditCandidate(ctx, root, target, relative, &remaining); !errors.Is(err, errSeriesNFOAuditBytes) {
		t.Fatalf("sidecar with insufficient budget returned %v", err)
	}
	remaining++
	observation, err := readSeriesNFOAuditCandidate(ctx, root, target, relative, &remaining)
	if err != nil || observation.file == nil || remaining != 0 {
		t.Fatalf("read the sidecar with its exact byte budget: %+v, %v, remaining %d", observation.claim, err, remaining)
	}
	if matches, err := observation.verify(ctx, root); err != nil || !matches {
		t.Fatalf("stable sidecar did not recheck: %v, %v", matches, err)
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(nfo), 0o600); err != nil {
		t.Fatal(err)
	}
	if matches, err := observation.verify(ctx, root); err != nil || matches {
		t.Fatalf("a different file with identical bytes stood in for the observation: %v, %v", matches, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("tvshow.nfo.old", path); err != nil {
		t.Fatal(err)
	}
	remaining = 1000
	if seen, err := readSeriesNFOAuditCandidate(ctx, root, target, relative, &remaining); err != nil || seen.file != nil || remaining != 1000 {
		t.Fatalf("linked sidecar contributed: %+v, %v, remaining %d", seen.claim, err, remaining)
	}
}

func TestSeriesNFOAuditRootWitnessRejectsSameNameReplacement(t *testing.T) {
	ctx := context.Background()
	anchor := t.TempDir()
	registered := filepath.Join(anchor, "registered")
	if err := os.Mkdir(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	plan := seriesNFOAuditRootPlan{root: libraryRoot{id: "root", libraryID: "library", allowedPath: anchor, path: registered, relativePath: "registered"}}
	witness, err := withSeriesNFOAuditRoot(ctx, plan, func(_ context.Context, _ *os.Root, witness seriesNFOAuditRootWitness) (seriesNFOAuditRootWitness, error) {
		return witness, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(registered, registered+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := withSeriesNFOAuditRoot(ctx, plan, (seriesNFOAuditRootEvidence{witness: witness}).verify); !errors.Is(err, errSeriesNFOAuditRoot) {
		t.Fatalf("same-name replacement root reused previous witness: %v", err)
	}
}
