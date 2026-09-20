package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/analysiscache"
	"github.com/moooyo/goby/internal/bif"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func previewGenerationFixture(t *testing.T) (*analysiscache.Store, config.MediaAnalysisConfig, library.AnalysisWork, library.AnalysisSource, media.Info, analysisPreviewBuildPlan) {
	t.Helper()
	configuration := config.MediaAnalysisConfig{Enabled: true, CacheDirectory: filepath.Join(t.TempDir(), "cache"), CacheMaxBytes: 32 << 20,
		CacheMaxEntries: 8, MaxEntryBytes: 8 << 20, MaxFileBytes: 1 << 20}
	store, err := analysiscache.Open(context.Background(), configuration.CacheOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Errorf("close generation cache: %v", err)
		}
	})
	profile := library.DefaultAnalysisProfile()
	profile.PreviewIntervalSeconds, profile.PreviewQuality = 2, 95
	source := library.AnalysisSource{ItemID: "preview-item", SourceRevision: "source-revision", DurationTicks: 5 * media.TicksPerSecond, Size: 4096, Target: true}
	work := library.AnalysisWork{TaskKey: library.TaskPreviewGenerationKey, ConfigurationFingerprint: strings.Repeat("a", 64), ConfigurationRevision: "1", PublicationEpoch: 1,
		Profile: profile, Sources: []library.AnalysisSource{source}, Execution: library.AnalysisExecutionProfile{Version: library.AnalysisProfileVersion, Available: true,
			FFmpegSHA256: strings.Repeat("b", 64), FFprobeSHA256: strings.Repeat("c", 64), PreviewProfile: media.PreviewAnalysisProfile, PreviewWidths: []int{240, 320, 400}}}
	info := media.Info{DurationTicks: source.DurationTicks, Size: source.Size}
	plan, err := planAnalysisPreviewBuild(configuration, work, source, info)
	if err != nil {
		t.Fatal(err)
	}
	return store, configuration, work, source, info, plan
}

func previewGenerationJPEG(width, quality int) []byte {
	raster := image.NewRGBA(image.Rect(0, 0, width, width/2))
	for y := 0; y < width/2; y++ {
		for x := 0; x < width; x++ {
			raster.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 11), B: uint8(x + y), A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, raster, &jpeg.Options{Quality: quality}); err != nil {
		panic(err)
	}
	return encoded.Bytes()
}

func previewGenerationExtract(work library.AnalysisWork, source library.AnalysisSource, mode string) analysisPreviewExtract {
	return func(ctx context.Context, options media.PreviewAnalysisOptions, emit func(media.PreviewFrame) error) (media.PreviewAnalysisSummary, error) {
		data := previewGenerationJPEG(options.Width, options.Quality)
		if mode == "invalid_jpeg" {
			data = []byte("not a JPEG image")
		}
		defer clear(data) // A retained callback JPEG slice would now be invalid.
		count := int((source.DurationTicks-1)/options.IntervalTicks + 1)
		summary := media.PreviewAnalysisSummary{Profile: work.Execution.PreviewProfile, FFmpegSHA256: work.Execution.FFmpegSHA256, FFprobeSHA256: work.Execution.FFprobeSHA256,
			Width: options.Width, Height: options.Width / 2, Quality: options.Quality, IntervalTicks: options.IntervalTicks}
		if mode == "wrong_jpeg_dimensions" {
			summary.Height++
		}
		for index := 0; index < count; index++ {
			if err := ctx.Err(); err != nil {
				return media.PreviewAnalysisSummary{}, err
			}
			nominal := int64(index) * options.IntervalTicks
			actual := nominal - media.TicksPerSecond/10
			if index == 0 {
				actual = media.TicksPerSecond / 10
			}
			frame := media.PreviewFrame{Width: options.Width, Height: options.Width / 2, NominalTicks: nominal, ActualTicks: actual, JPEG: data, SHA256: sha256.Sum256(data)}
			switch mode {
			case "held_frame":
				if index == 1 {
					frame.ActualTicks = media.TicksPerSecond / 10
				}
			case "late_first_frame":
				if index < 2 {
					frame.ActualTicks = 3 * media.TicksPerSecond
				}
			case "backward_actual":
				if index == 2 {
					frame.ActualTicks = 0
				}
			case "future_actual":
				if index == 1 {
					frame.ActualTicks = nominal + media.TicksPerSecond/10
				}
			case "negative_actual":
				frame.ActualTicks = -1
			case "outside_duration":
				frame.ActualTicks = source.DurationTicks
			}
			if mode == "wrong_jpeg_dimensions" {
				frame.Height = summary.Height
			}
			if mode == "variant_clock" && options.Width == 320 && index == 1 {
				frame.ActualTicks++
			}
			if mode == "bad_hash" {
				frame.SHA256[0] ^= 1
			}
			if mode == "wrong_nominal" {
				frame.NominalTicks++
			}
			if mode == "oversize_edge" {
				frame.Height = 2049
			}
			if err := emit(frame); err != nil {
				return media.PreviewAnalysisSummary{}, err
			}
			summary.FrameCount++
			summary.JPEGBytes += int64(len(data))
			if mode == "extract_failure" {
				return media.PreviewAnalysisSummary{}, io.ErrUnexpectedEOF
			}
		}
		if mode == "summary_tool" {
			summary.FFmpegSHA256 = strings.Repeat("d", 64)
		}
		if mode == "summary_quality" {
			summary.Quality = 80
		}
		if mode == "summary_count" {
			summary.FrameCount--
		}
		return summary, nil
	}
}

func previewGenerationAssertEmpty(t *testing.T, store *analysiscache.Store) {
	t.Helper()
	stats := store.Stats()
	if stats.BuildingEntries != 0 || stats.PendingPublications != 0 || stats.ReadyEntries != 0 || stats.Readers != 0 || stats.ReservedBytes != 0 || stats.ReadyBytes != 0 || stats.TotalBytes != stats.ControlBytes {
		t.Fatalf("function returned before owned workspace cleanup ended: %+v", stats)
	}
}

func TestAnalysisPreviewGenerationStreamsThreeSealedBIFsWithExactTimelines(t *testing.T) {
	store, configuration, work, source, _, plan := previewGenerationFixture(t)
	var widths []int
	extract := previewGenerationExtract(work, source, "")
	publication, values, err := generateAnalysisPreview(context.Background(), store, work, source, plan,
		func(ctx context.Context, options media.PreviewAnalysisOptions, emit func(media.PreviewFrame) error) (media.PreviewAnalysisSummary, error) {
			widths = append(widths, options.Width)
			stats := store.Stats()
			if stats.BuildingEntries != 1 || stats.ReservedBytes != plan.reservation || stats.TotalBytes < plan.reservation+stats.ControlBytes {
				return media.PreviewAnalysisSummary{}, fmt.Errorf("assembly scratch and completed variants lost their reservation: %+v", stats)
			}
			if options.Quality != 95 || options.IntervalTicks != plan.interval {
				return media.PreviewAnalysisSummary{}, errors.New("execution did not consume the admitted preview policy")
			}
			return extract(ctx, options, emit)
		})
	if err != nil || publication == nil || len(values) != 3 {
		t.Fatalf("generation = %+v %+v %v", publication, values, err)
	}
	defer func() {
		if err := publication.Discard(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	if fmt.Sprint(widths) != "[240 320 400]" || !analysisPreviewHash(publication.Entry.Key) || !analysisPreviewHash(publication.Entry.Seal) {
		t.Fatalf("wrong generation inventory: %v %+v", widths, publication.Entry)
	}
	stats := store.Stats()
	if stats.BuildingEntries != 0 || stats.ReservedBytes != 0 || stats.PendingPublications != 1 || stats.ReadyEntries != 1 {
		t.Fatalf("publication pin ownership = %+v", stats)
	}
	for _, value := range values {
		if value.CacheKey != publication.Entry.Key || value.Seal != publication.Entry.Seal || value.IntervalTicks != plan.interval || value.FrameCount != plan.frames || len(value.NominalTicks) != plan.frames {
			t.Fatalf("unbound database candidate: %+v", value)
		}
		lease, err := store.Acquire(context.Background(), value.CacheKey, value.Seal, fmt.Sprintf("%d.bif", value.Width))
		if err != nil {
			t.Fatal(err)
		}
		archive, openErr := bif.Open(lease.File, value.Bytes, bif.DefaultLimits())
		if openErr != nil {
			lease.Close()
			t.Fatal(openErr)
		}
		if archive.Len() != plan.frames || archive.MultiplierMillis() != uint32(plan.interval/(media.TicksPerSecond/1000)) {
			lease.Close()
			t.Fatal("BIF changed its timestamp multiplier or count")
		}
		for index := 0; index < archive.Len(); index++ {
			entry, entryErr := archive.Entry(index)
			if entryErr != nil {
				lease.Close()
				t.Fatal(entryErr)
			}
			wantActual := value.NominalTicks[index] - media.TicksPerSecond/10
			if index == 0 {
				wantActual = media.TicksPerSecond / 10
			}
			if entry.Timestamp != uint32(index) || entry.TimestampMillis != uint64(value.NominalTicks[index]/(media.TicksPerSecond/1000)) || value.ActualTicks[index] != wantActual {
				lease.Close()
				t.Fatal("BIF or database lost exact nominal/actual ticks")
			}
			data, err := archive.JPEG(context.Background(), index)
			if err != nil {
				lease.Close()
				t.Fatal(err)
			}
			geometry, err := jpeg.DecodeConfig(bytes.NewReader(data))
			if err != nil || geometry.Width != value.Width || geometry.Height != value.Height {
				lease.Close()
				t.Fatalf("BIF source buffer was not independently retained: %+v %v", geometry, err)
			}
		}
		if lease.Artifact.SHA256 != value.SHA256 || lease.Artifact.Size != value.Bytes {
			lease.Close()
			t.Fatal("publication hash or extent is unbound")
		}
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	}
	manifestLease, err := store.Acquire(context.Background(), publication.Entry.Key, publication.Entry.Seal, "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := io.ReadAll(manifestLease.File)
	closeErr := manifestLease.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("manifest read: %v %v", err, closeErr)
	}
	var manifest analysisPreviewBuildManifest
	if err := json.Unmarshal(encoded, &manifest); err != nil || len(manifest.Variants) != 3 || manifest.Quality != 95 || manifest.SourceRevision != source.SourceRevision || manifest.ProfileFingerprint != work.ConfigurationFingerprint {
		t.Fatalf("manifest: %+v %v", manifest, err)
	}
	if bytes.Contains(encoded, []byte(configuration.CacheDirectory)) || len(encoded) > analysisPreviewManifestLimit {
		t.Fatal("manifest contains a path or exceeds its bound")
	}
	entries, err := os.ReadDir(filepath.Join(configuration.CacheDirectory, "entry-"+publication.Entry.Key))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "frame-") {
			t.Fatal("scratch JPEG survived publication")
		}
	}
}

func TestAnalysisPreviewGenerationPreservesHeldSourceFrameTicks(t *testing.T) {
	for _, fixture := range []struct{ mode, actual string }{
		{"held_frame", "[1000000 1000000 39000000]"},
		{"late_first_frame", "[30000000 30000000 39000000]"},
	} {
		t.Run(fixture.mode, func(t *testing.T) {
			store, _, work, source, _, plan := previewGenerationFixture(t)
			publication, values, err := generateAnalysisPreview(context.Background(), store, work, source, plan, previewGenerationExtract(work, source, fixture.mode))
			if err != nil || publication == nil || len(values) != 3 {
				t.Fatalf("held-frame generation = %+v %+v %v", publication, values, err)
			}
			defer func() {
				if err := publication.Discard(context.Background()); err != nil {
					t.Error(err)
				}
			}()
			for _, value := range values {
				if fmt.Sprint(value.NominalTicks) != "[0 20000000 40000000]" || fmt.Sprint(value.ActualTicks) != fixture.actual {
					t.Fatalf("held source PTS was replaced by a synthetic slot clock: %+v", value)
				}
			}
		})
	}
}

func TestAnalysisPreviewGenerationRejectsOutputBudgetWithoutPublishing(t *testing.T) {
	store, _, work, source, _, plan := previewGenerationFixture(t)
	publication, values, err := generateAnalysisPreview(context.Background(), store, work, source, plan,
		func(ctx context.Context, options media.PreviewAnalysisOptions, emit func(media.PreviewFrame) error) (media.PreviewAnalysisSummary, error) {
			data := make([]byte, plan.jpegBytes+1)
			return media.PreviewAnalysisSummary{}, emit(media.PreviewFrame{Width: options.Width, Height: 1, JPEG: data, SHA256: sha256.Sum256(data)})
		})
	if publication != nil || values != nil || !errors.Is(err, media.ErrAnalysisBudget) {
		t.Fatalf("output budget became a partial publication: %+v %+v %v", publication, values, err)
	}
	previewGenerationAssertEmpty(t, store)
}

func TestAnalysisPreviewGenerationRejectsPartialOrUnprovenOutputAndJoinsAbort(t *testing.T) {
	for _, mode := range []string{"extract_failure", "variant_clock", "bad_hash", "wrong_nominal", "oversize_edge", "summary_tool", "summary_quality", "summary_count", "invalid_jpeg", "wrong_jpeg_dimensions", "backward_actual", "future_actual", "negative_actual", "outside_duration"} {
		t.Run(mode, func(t *testing.T) {
			store, _, work, source, _, plan := previewGenerationFixture(t)
			publication, values, err := generateAnalysisPreview(context.Background(), store, work, source, plan, previewGenerationExtract(work, source, mode))
			if err == nil || publication != nil || values != nil {
				t.Fatalf("invalid output became publishable: %+v %+v %v", publication, values, err)
			}
			previewGenerationAssertEmpty(t, store)
		})
	}
}

func TestAnalysisPreviewGenerationAdaptsLongSourcesAndReservesAssemblyPeak(t *testing.T) {
	_, configuration, work, source, info, _ := previewGenerationFixture(t)
	source.DurationTicks, info.DurationTicks = media.MaxAnalysisDurationTicks, media.MaxAnalysisDurationTicks
	work.Sources[0] = source
	plan, err := planAnalysisPreviewBuild(configuration, work, source, info)
	if err != nil || plan.frames > 4096 || plan.interval != 11*media.TicksPerSecond {
		t.Fatalf("long-source plan: %+v %v", plan, err)
	}
	if plan.reservation > configuration.MaxEntryBytes || plan.reservation != 4*plan.variantBytes+analysisPreviewWorkspaceOverhead || plan.jpegBytes+64+8*int64(plan.frames+1) != plan.variantBytes {
		t.Fatalf("workspace peak does not include scratch, index and controls: %+v", plan)
	}
	for _, mutate := range []func(*config.MediaAnalysisConfig){
		func(c *config.MediaAnalysisConfig) { c.MaxEntryBytes = analysisPreviewWorkspaceOverhead },
		func(c *config.MediaAnalysisConfig) { c.MaxFileBytes = 72 },
	} {
		bad := configuration
		mutate(&bad)
		if _, err := planAnalysisPreviewBuild(bad, work, source, info); !errors.Is(err, media.ErrAnalysisBudget) {
			t.Fatalf("too-small cache configuration was not a resource failure: %v", err)
		}
	}
	extractor, err := bindAnalysisPreviewExtractor(media.AnalysisExtractor{}, work, plan)
	if err != nil || extractor.ExpectedFFmpegSHA256 != work.Execution.FFmpegSHA256 || extractor.ExpectedFFprobeSHA256 != work.Execution.FFprobeSHA256 || extractor.Limits.MaxPreviewFrames != 4096 || extractor.Limits.MaxOutputBytes != plan.jpegBytes || extractor.Limits.MaxJPEGBytes > 2<<20 {
		t.Fatalf("media limits/admission binding: %+v %v", extractor, err)
	}
	wrong := extractor
	wrong.ExpectedFFmpegSHA256 = strings.Repeat("d", 64)
	if _, err := bindAnalysisPreviewExtractor(wrong, work, plan); !errors.Is(err, media.ErrAnalysisUnavailable) {
		t.Fatalf("different admitted tool bytes were accepted: %v", err)
	}
}

func previewGenerationPending(t *testing.T, store *analysiscache.Store) *analysiscache.Publication {
	t.Helper()
	var seed [32]byte
	seed[0] = 1
	key := hex.EncodeToString(seed[:])
	builder, err := store.Begin(context.Background(), key, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.WriteFile(context.Background(), "240.bif", func(_ context.Context, w io.Writer) error { _, err := w.Write([]byte("sealed test bytes")); return err }); err != nil {
		builder.Abort(context.Background())
		t.Fatal(err)
	}
	publication, err := builder.Publish(context.Background())
	if err != nil {
		if publication != nil {
			discardAnalysisPreviewPublication(publication, err)
		} else {
			builder.Abort(context.Background())
		}
		t.Fatal(err)
	}
	return publication
}

func TestAnalysisPreviewGenerationResolvesPostRenameFailurePin(t *testing.T) {
	store, _, _, _, _, _ := previewGenerationFixture(t)
	publication := previewGenerationPending(t, store)
	cause := errors.New("directory sync failed after rename")
	if err := discardAnalysisPreviewPublication(publication, cause); !errors.Is(err, cause) {
		t.Fatalf("post-rename failure was hidden: %v", err)
	}
	previewGenerationAssertEmpty(t, store)
}

func TestAnalysisPreviewGenerationFailedDiscardKeepsChargedBytesAndReleasesPin(t *testing.T) {
	store, _, _, _, _, _ := previewGenerationFixture(t)
	publication := previewGenerationPending(t, store)
	lease, err := store.Acquire(context.Background(), publication.Entry.Key, publication.Entry.Seal, "240.bif")
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("post-rename publication failed")
	err = discardAnalysisPreviewPublication(publication, cause)
	if !errors.Is(err, cause) || !errors.Is(err, analysiscache.ErrBusy) {
		lease.Close()
		t.Fatalf("discard/keep cause was hidden: %v", err)
	}
	stats := store.Stats()
	if stats.PendingPublications != 0 || stats.Readers != 1 || stats.ReadyEntries != 1 || stats.ReadyBytes <= 0 {
		lease.Close()
		t.Fatalf("fallback lost bytes or an unresolved pin: %+v", stats)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), publication.Entry.Key, publication.Entry.Seal); err != nil {
		t.Fatal(err)
	}
	previewGenerationAssertEmpty(t, store)
}

func TestAnalysisPreviewGenerationCancellationWaitsForActualProducerExit(t *testing.T) {
	store, _, work, source, _, plan := previewGenerationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, _, err := generateAnalysisPreview(ctx, store, work, source, plan, func(callCtx context.Context, options media.PreviewAnalysisOptions, emit func(media.PreviewFrame) error) (media.PreviewAnalysisSummary, error) {
			data := previewGenerationJPEG(options.Width, options.Quality)
			if err := emit(media.PreviewFrame{Width: options.Width, Height: options.Width / 2, NominalTicks: 0, ActualTicks: media.TicksPerSecond / 10, JPEG: data, SHA256: sha256.Sum256(data)}); err != nil {
				return media.PreviewAnalysisSummary{}, err
			}
			close(started)
			<-release
			return media.PreviewAnalysisSummary{}, callCtx.Err()
		})
		done <- err
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("producer failed before its blocking point: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("producer did not start")
	}
	cancel()
	select {
	case err := <-done:
		close(release)
		t.Fatalf("cancellation pretended the producer had exited: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel cause: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("producer cleanup did not complete")
	}
	previewGenerationAssertEmpty(t, store)
}
