package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

// This opt-in measurement exercises the actual fingerprint stage with manager
// capabilities and source I/O admission. It excludes matching and visual work.
// The admitted tool is the actual binary, so each measured extraction or reuse
// includes reopening and hashing its complete bytes. Process counts below are
// expected, not instrumented; counting-wrapper integration tests cover them.
// Force supplies the extraction baseline; alternating the order reduces simple
// page-cache/order bias. No throughput or detection-accuracy claim is implied by
// the generated clip. Set GOBY_CREDITS_CACHE_SOURCE to repeat on representative
// media, and TMPDIR to select the fixture filesystem.
func TestCreditsFingerprintCacheNativeMeasurement(t *testing.T) {
	if os.Getenv("GOBY_CREDITS_CACHE_MEASURE") != "1" {
		t.Skip("set GOBY_CREDITS_CACHE_MEASURE=1 for the native fingerprint-stage measurement")
	}
	ffmpeg, ffprobe, introFFmpeg := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE"), os.Getenv("GOBY_INTRO_SKIPPER_FFMPEG")
	if ffmpeg == "" || ffprobe == "" || introFFmpeg == "" || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("native cache measurement requires PostgreSQL, FFmpeg, FFprobe and Chromaprint FFmpeg")
	}
	sourcePath := os.Getenv("GOBY_CREDITS_CACHE_SOURCE")
	if sourcePath == "" {
		sourcePath = filepath.Join(t.TempDir(), "generated-credits-tail.mkv")
		hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
			"-f", "lavfi", "-i", "color=c=black:size=16x16:rate=1:duration=475.125",
			"-f", "lavfi", "-i", "aevalsrc=0.3*sin(2*PI*(180*t+0.75*t*t)):s=16000:d=475.125",
			"-map", "0:v:0", "-map", "1:a:0", "-c:v", "ffv1", "-threads:v", "1", "-c:a", "pcm_s16le", "-ac", "2", "-threads:a", "1", "-t", "475.125", sourcePath)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	sourceBytes, readErr := io.Copy(digest, source)
	sourceInfo, probeErr := (media.Prober{FFprobePath: ffprobe, Timeout: time.Minute}).ProbeFile(context.Background(), source)
	if err := errors.Join(readErr, probeErr, source.Close()); err != nil {
		t.Fatal(err)
	}
	audioIndex, found := media.SelectIntroSkipperAudioStream(sourceInfo, "", true)
	if !found {
		t.Fatal("native measurement requires an eligible audio stream")
	}
	sourcePath, err = filepath.Abs(sourcePath)
	if err == nil {
		sourcePath, err = filepath.EvalSymlinks(sourcePath)
	}
	if err != nil {
		t.Fatal(err)
	}
	expectedHash := hex.EncodeToString(digest.Sum(nil))
	writeSource := func(path string) error {
		input, err := os.Open(sourcePath)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return errors.Join(err, input.Close())
		}
		_, copyErr := io.Copy(output, input)
		return errors.Join(copyErr, output.Close(), input.Close())
	}
	for _, sources := range []int{16, 24, 32} {
		t.Run(fmt.Sprintf("sources-%d", sources), func(t *testing.T) {
			f := newCreditsCacheFixture(t, creditsCacheFixtureOptions{SourceCount: sources,
				Prober: media.Prober{FFprobePath: ffprobe, Timeout: time.Minute}, WriteSource: writeSource, ToolPath: introFFmpeg, DirectTool: true})
			toolInfo, err := os.Stat(f.tool)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("credits-cache-native-input mode=direct source_path=%q stage_source_path=%q source_bytes=%d source_sha256=%s tool_path=%q tool_bytes=%d tool_sha256=%s",
				sourcePath, f.paths[0], sourceBytes, expectedHash, f.tool, toolInfo.Size(), f.executor.runtime.extractor.ExpectedIntroFFmpegSHA256)
			var stageTotals [2]time.Duration
			for round := 0; round < 2; round++ {
				for _, force := range []bool{round == 0, round != 0} {
					startedAt := time.Now()
					admission := f.start(t, force)
					children := int(admission.Run.TotalChildren)
					if children != (sources+15)/16 {
						t.Fatalf("unexpected admitted overlap: sources=%d children=%d", sources, children)
					}
					var elapsed time.Duration
					cacheHits := 0
					for child := 0; child < children; child++ {
						started := f.next(t)
						if started.task.RunID != admission.Run.ID || len(started.work.Sources) != sources {
							t.Fatal("measurement lost the admitted overlapping source cohort")
						}
						// The worker remains gated while this verifies the exact keys
						// that its complete digest and selected stream will produce.
						hits := 0
						for _, source := range started.work.Sources {
							key, ok := creditsFingerprintKey(started.work, source, expectedHash, f.executor.runtime.creditsAudioProfile, audioIndex)
							if !ok {
								t.Fatal("native source could not form its complete cache key")
							}
							if len(f.executor.runtime.creditsFingerprints.get(key)) != 0 {
								hits++
							}
						}
						wantHits := 0
						if !force && child != 0 {
							wantHits = sources
						}
						if hits != wantHits {
							t.Fatalf("unexpected native cache reuse: hits=%d want=%d", hits, wantHits)
						}
						cacheHits += hits
						entriesBefore := f.executor.runtime.creditsFingerprints.count
						outcome := f.finish(t, started)
						if outcome.err != nil || len(outcome.hashes) != sources {
							t.Fatalf("native fingerprint stage failed: %v", outcome.err)
						}
						for _, hash := range outcome.hashes {
							if hash != expectedHash {
								t.Fatal("native reuse lost the complete source digest")
							}
						}
						wantEntries := entriesBefore
						if !force && child == 0 {
							wantEntries += sources
						}
						if f.executor.runtime.creditsFingerprints.count != wantEntries {
							t.Fatal("native cache did not retain exactly the successful non-Force source fingerprints")
						}
						elapsed += outcome.elapsed
					}
					f.waitRun(t, admission.Run.ID, tasks.RunCompleted)
					wall := time.Since(startedAt)
					wantExtracts, variant := sources, 0
					if force {
						wantExtracts, variant = sources*children, 1
					}
					stageTotals[variant] += elapsed
					t.Logf("credits-cache-native mode=direct sources=%d children=%d round=%d force=%t source_bytes=%d verified_full_digests=%d verified_cache_hits=%d expected_capability_probes=%d expected_extractions=%d stage_ms=%.3f wall_ms=%.3f",
						sources, children, round, force, sourceBytes, sources*children, cacheHits, 3*sources*children, wantExtracts, float64(elapsed)/float64(time.Millisecond), float64(wall)/float64(time.Millisecond))
				}
			}
			t.Logf("credits-cache-native-summary mode=direct sources=%d reuse_mean_ms=%.3f force_mean_ms=%.3f reuse_over_force=%.5f", sources,
				float64(stageTotals[0])/float64(2*time.Millisecond), float64(stageTotals[1])/float64(2*time.Millisecond), float64(stageTotals[0])/float64(stageTotals[1]))
		})
	}
}
