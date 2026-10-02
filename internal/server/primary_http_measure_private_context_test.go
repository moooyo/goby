//go:build linux && primary_io_measure

package server

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// These tests own synthetic files and create no native command. They verify the
// private-context cleanup decision and filesystem evidence, not process-group
// retirement. Actual group/Wait/copier proof remains the measurement runner's
// separate contract and requires its real command to execute.
func TestPrimaryHTTPMeasurePrivateContextRetainsUnjoinedInputsAndEvidence(t *testing.T) {
	private, contents := primaryHTTPMeasurePrivateTestContext(t)
	for range 2 {
		removed, err := private.cleanup(false)
		if err != nil || removed {
			t.Fatalf("unjoined private context was destroyed: removed=%v error=%v", removed, err)
		}
		for path, expected := range contents {
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, expected) {
				t.Fatalf("unjoined private input/evidence changed: basename=%s error=%v", filepath.Base(path), err)
			}
		}
		for _, path := range []string{private.parent, private.directory} {
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
				t.Fatalf("unjoined private directory lost its protected ownership: error=%v", err)
			}
		}
	}
	// This test created no process. It explicitly retires its own synthetic
	// fixture after retention assertions; this is not a late numeric-PID retry.
	if removed, err := private.cleanup(true); err != nil || !removed {
		t.Fatalf("retire synthetic private-context fixture: removed=%v error=%v", removed, err)
	}
}

func TestPrimaryHTTPMeasurePrivateContextRemovesOnlyKnownFilesAfterJoinDecision(t *testing.T) {
	private, contents := primaryHTTPMeasurePrivateTestContext(t)
	unowned := filepath.Join(private.directory, "foreign-evidence.txt")
	if err := os.WriteFile(unowned, []byte("synthetic unowned entry"), 0o600); err != nil {
		t.Fatal(err)
	}
	if removed, err := private.cleanup(true); err == nil || removed {
		t.Fatalf("joined decision removed an unowned directory entry: removed=%v error=%v", removed, err)
	}
	for path, expected := range contents {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("cleanup changed known input before its preflight succeeded: basename=%s error=%v", filepath.Base(path), err)
		}
	}
	if err := os.Remove(unowned); err != nil {
		t.Fatal(err)
	}
	if removed, err := private.cleanup(true); err != nil || !removed {
		t.Fatalf("joined decision did not close the known private context: removed=%v error=%v", removed, err)
	}
	for path := range contents {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("known private file survived verified cleanup decision: basename=%s error=%v", filepath.Base(path), err)
		}
	}
	for _, path := range []string{private.directory, private.parent} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("private ownership directory survived known-file cleanup: error=%v", err)
		}
	}
}

func primaryHTTPMeasurePrivateTestContext(t *testing.T) (*primaryHTTPMeasurePrivateContext, map[string][]byte) {
	t.Helper()
	private, err := newPrimaryHTTPMeasurePrivateContext(filepath.Join(t.TempDir(), "synthetic-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	contents := make(map[string][]byte)
	for _, path := range private.knownFiles() {
		data := []byte("synthetic-private-" + filepath.Base(path))
		if err := primaryHTTPMeasureWritePrivate(path, data); err != nil {
			t.Fatal(err)
		}
		contents[path] = data
	}
	return private, contents
}
