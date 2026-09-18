//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerHLSArtifactsRequireTheCompleteScope(t *testing.T) {
	artifacts := map[string]string{
		"v0.m3u8":               "variant playlist",
		"init.mp4":              "primary initialization",
		"v0-init.mp4":           "variant initialization",
		"segment-000000.m4s":    "primary fragmented media",
		"v0-segment-000000.m4s": "variant fragmented media",
		"segment-000000.aac":    "packed AAC",
		"segment-000000.mp3":    "packed MP3",
		"segment-000000.vtt":    "WEBVTT",
	}
	run := func(_ context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		if err := publishManagerTestOutput(dir, 188); err != nil {
			return RunResult{}, err
		}
		for name, body := range artifacts {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
				return RunResult{}, err
			}
		}
		return RunResult{}, nil
	}
	m := newTestManager(t, managerAccessOptions(t, run))
	spec := managerTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if final := managerTestWaitFinished(t, m, record.ID); final.State != "completed" {
		t.Fatalf("HLS artifact job failed: %+v", final)
	}
	for name, want := range artifacts {
		t.Run(name, func(t *testing.T) {
			for _, change := range []func(*Scope){
				func(scope *Scope) { scope.UserID = "other-user" },
				func(scope *Scope) { scope.AuthSessionID = "other-auth" },
				func(scope *Scope) { scope.DeviceID = "other-device" },
				func(scope *Scope) { scope.PlaySessionID = "other-play" },
				func(scope *Scope) { scope.ItemID = "other-item" },
				func(scope *Scope) { scope.SourceID = "other-source" },
			} {
				foreign := spec.Scope
				change(&foreign)
				if handle, err := m.TryOpen(foreign, record.ID, name); handle != nil || !errors.Is(err, ErrJobNotFound) {
					if handle != nil {
						_ = handle.Close()
					}
					t.Fatalf("foreign scope read %s: %v", name, err)
				}
			}
			handle, err := m.TryOpen(spec.Scope, record.ID, name)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(handle)
			closeErr := handle.Close()
			if readErr != nil || closeErr != nil || string(body) != want {
				t.Fatalf("authorized artifact = %q, %v, %v", body, readErr, closeErr)
			}
		})
	}
	for _, name := range []string{"../init.mp4", "v0/init.mp4", `v0\init.mp4`, "v4.m3u8", "init.mp4.tmp", "segment-list.m3u8", "main.m3u8.publish.tmp", "stream.bin", "subtitle.ass", "font-0.ttf"} {
		if handle, err := m.TryOpen(spec.Scope, record.ID, name); handle != nil || !errors.Is(err, ErrOutputUnavailable) {
			if handle != nil {
				_ = handle.Close()
			}
			t.Fatalf("private or external name became public: %s, %v", name, err)
		}
	}
	managerAccessAssertReaders(t, m, record.ID, 0, 0)
}

func TestManagerHLSExtendedArtifactsCountTowardJobQuota(t *testing.T) {
	for _, name := range []string{"v0.m3u8", "init.mp4", "v0-init.mp4", "segment-000000.m4s", "segment-000000.aac", "segment-000000.mp3", "segment-000000.vtt", "v0-segment-000000.m4s.tmp", "subtitle.ass", "font-15.ttf"} {
		t.Run(name, func(t *testing.T) {
			run := func(_ context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
				if err := publishManagerTestOutput(dir, 188); err != nil {
					return RunResult{}, err
				}
				return RunResult{}, os.WriteFile(filepath.Join(dir, name), make([]byte, 2048), 0o600)
			}
			options := managerAccessOptions(t, run)
			options.MaxJobBytes = 1024
			m := newTestManager(t, options)
			spec := managerTestSpec(1)
			record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
			if err != nil {
				t.Fatal(err)
			}
			final := managerTestWaitFinished(t, m, record.ID)
			if final.State != "failed" || final.ErrorCode != "job_quota" {
				t.Fatalf("extended artifact bypassed the storage quota: %+v", final)
			}
			if handle, err := m.TryOpen(spec.Scope, record.ID, "main.m3u8"); handle != nil || !errors.Is(err, ErrQuota) {
				if handle != nil {
					_ = handle.Close()
				}
				t.Fatalf("quota failure still exposed output: %v", err)
			}
		})
	}
}

func TestManagerHLSVariantPlaylistsHaveTheSameReadLimit(t *testing.T) {
	run := func(_ context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		if err := publishManagerTestOutput(dir, 188); err != nil {
			return RunResult{}, err
		}
		return RunResult{}, os.WriteFile(filepath.Join(dir, "v0.m3u8"), make([]byte, MaxPlaylistBytes+1), 0o600)
	}
	options := managerAccessOptions(t, run)
	options.MaxJobBytes = 2 << 20
	m := newTestManager(t, options)
	spec := managerTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if final := managerTestWaitFinished(t, m, record.ID); final.State != "completed" {
		t.Fatalf("fixture exceeded an unrelated limit: %+v", final)
	}
	if handle, err := m.TryOpen(spec.Scope, record.ID, "v0.m3u8"); handle != nil || !errors.Is(err, ErrOutputUnavailable) {
		if handle != nil {
			_ = handle.Close()
		}
		t.Fatalf("oversized variant playlist was exposed: %v", err)
	}
	managerAccessAssertReaders(t, m, record.ID, 0, 0)
}
