//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func generatedAVSourceTestExecutable(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bounded-projection")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func generatedAVSourceTestShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func TestGeneratedAVSourceBorrowedDescriptorAndChangeTimeFence(t *testing.T) {
	candidate, fixture := generatedAVSourceTestCandidate(t, generatedAVSourceTestTracks(0, 2), 0, 1)
	projection := generatedAVSourceTestProjection(candidate, "0.000000")
	executable := generatedAVSourceTestExecutable(t, "printf '%s' "+generatedAVSourceTestShellQuote(string(projection)))
	path := filepath.Join(t.TempDir(), "structural.mp4")
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := source.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := MeasureGeneratedMP4AVSourceEndpoint(context.Background(), executable, source, 0, 1)
	if err != nil || got.SourceIdentity == "" || got.DurationTicks != 2*ticksPerSecond {
		t.Fatalf("borrowed descriptor lost its candidate: %+v %v", got, err)
	}
	if offset, err := source.Seek(0, io.SeekCurrent); err != nil || offset != 7 {
		t.Fatalf("borrowed file offset changed: %d %v", offset, err)
	}
	if err := ValidateGeneratedMP4AVSourceEndpointIdentity(source, got); err != nil {
		t.Fatal(err)
	}
	before, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}
	beforeStat, ok := before.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("source has no Linux ctime fact")
	}
	writer, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	deadline := time.Now().Add(2 * time.Second)
	for attempt := 0; ; attempt++ {
		if _, err := writer.WriteAt([]byte{byte(attempt)}, int64(generatedAVSourceTestBoxNth(fixture, "mdat", 0)+8)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
			t.Fatal(err)
		}
		after, err := source.Stat()
		if err != nil {
			t.Fatal(err)
		}
		afterStat, ok := after.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatal("mutated source has no Linux ctime fact")
		}
		if afterStat.Ctim != beforeStat.Ctim {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("controlled same-size mutation did not advance ctime")
		}
		time.Sleep(time.Millisecond)
	}
	if err := ValidateGeneratedMP4AVSourceEndpointIdentity(source, got); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("restored size and mtime hid payload mutation: %v", err)
	}
	copyPath := filepath.Join(t.TempDir(), "copy.mp4")
	if err := os.WriteFile(copyPath, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	copySource, err := os.Open(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer copySource.Close()
	if err := ValidateGeneratedMP4AVSourceEndpointIdentity(copySource, got); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("another inode reused candidate identity: %v", err)
	}
}

func TestGeneratedAVSourceFreshDemuxMustFinishCleanlyAndWithinBounds(t *testing.T) {
	candidate, fixture := generatedAVSourceTestCandidate(t, generatedAVSourceTestTracks(0, 2), 0, 1)
	projection := generatedAVSourceTestProjection(candidate, "0.000000")
	path := filepath.Join(t.TempDir(), "structural.mp4")
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"stderr":                      "printf diagnostic >&2\nprintf '%s' " + generatedAVSourceTestShellQuote(string(projection)),
		"failed exit":                 "printf '%s' " + generatedAVSourceTestShellQuote(string(projection)) + "\nexit 1",
		"oversized stdout":            "head -c 65537 /dev/zero",
		"source changes during demux": "printf x >> " + generatedAVSourceTestShellQuote(path) + "\nprintf '%s' " + generatedAVSourceTestShellQuote(string(projection)),
		"missing independent format":  "printf '%s' '{\"streams\":[]}'",
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, fixture, 0o600); err != nil {
				t.Fatal(err)
			}
			source, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			got, err := MeasureGeneratedMP4AVSourceEndpoint(context.Background(), generatedAVSourceTestExecutable(t, script), source, 0, 1)
			if err == nil || got != (GeneratedAVSourceCertificate{}) {
				t.Fatalf("incomplete demux observation returned candidate: %+v %v", got, err)
			}
		})
	}
}

func TestGeneratedAVSourceRejectsInvalidBorrowers(t *testing.T) {
	if _, err := MeasureGeneratedMP4AVSourceEndpoint(nil, "", nil, 0, 1); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("nil context was admitted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MeasureGeneratedMP4AVSourceEndpoint(ctx, "", nil, 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled probe lost cancellation: %v", err)
	}
	if _, err := MeasureGeneratedMP4AVSourceEndpoint(context.Background(), "", nil, 0, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing source was admitted: %v", err)
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	if _, err := MeasureGeneratedMP4AVSourceEndpoint(context.Background(), "", read, 0, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nonregular descriptor was admitted: %v", err)
	}
	if err := ValidateGeneratedMP4AVSourceEndpointIdentity(nil, GeneratedAVSourceCertificate{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing identity was admitted: %v", err)
	}
}
