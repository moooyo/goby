//go:build linux

package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/analysiscache"
	"github.com/moooyo/goby/internal/library"
)

func previewManifestTestRefs(work library.AnalysisWork, values []library.AnalysisPreviewPublication) []library.AnalysisPreview {
	refs := make([]library.AnalysisPreview, len(values))
	for index, value := range values {
		refs[index] = library.AnalysisPreview{ItemID: value.ItemID, Revision: "1", SourceRevision: work.Sources[0].SourceRevision,
			ProfileFingerprint: work.ConfigurationFingerprint, ProfileRevision: work.ConfigurationRevision, PublicationEpoch: work.PublicationEpoch,
			CacheKey: value.CacheKey, Seal: value.Seal, Width: value.Width, Height: value.Height, SHA256: value.SHA256,
			Bytes: value.Bytes, FrameCount: value.FrameCount, IntervalTicks: value.IntervalTicks,
			NominalTicks: append([]int64(nil), value.NominalTicks...), ActualTicks: append([]int64(nil), value.ActualTicks...),
			UpdatedAt: time.Unix(1, 0).UTC()}
	}
	return refs
}

func previewManifestTestClone(refs []library.AnalysisPreview) []library.AnalysisPreview {
	cloned := append([]library.AnalysisPreview(nil), refs...)
	for index := range cloned {
		cloned[index].NominalTicks = append([]int64(nil), refs[index].NominalTicks...)
		cloned[index].ActualTicks = append([]int64(nil), refs[index].ActualTicks...)
	}
	return cloned
}

func previewManifestTestFixture(t *testing.T) (*mediaAnalysisRuntime, library.AnalysisWork, []library.AnalysisPreview, []byte) {
	t.Helper()
	store, _, work, source, _, plan := previewGenerationFixture(t)
	publication, values, err := generateAnalysisPreview(context.Background(), store, work, source, plan, previewGenerationExtract(work, source, ""))
	if publication != nil {
		t.Cleanup(func() {
			if err := publication.Discard(context.Background()); err != nil {
				t.Errorf("release manifest fixture publication: %v", err)
			}
		})
	}
	if err != nil || publication == nil || len(values) != 3 {
		t.Fatalf("generate actual sealed preview fixture: %v", err)
	}
	if err := publication.Keep(); err != nil {
		t.Fatal(err)
	}
	refs := previewManifestTestRefs(work, values)
	for _, ref := range refs {
		if err := library.ValidateStoredAnalysisPreview(ref, source.DurationTicks); err != nil {
			t.Fatalf("generation fixture does not satisfy the library reference contract: %v", err)
		}
	}
	lease, err := store.Acquire(context.Background(), refs[0].CacheKey, refs[0].Seal, "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(lease.File, analysisPreviewManifestLimit+1))
	closeErr := lease.Close()
	if readErr != nil || closeErr != nil || len(raw) > analysisPreviewManifestLimit {
		t.Fatalf("read actual generated manifest: %v %v", readErr, closeErr)
	}
	return &mediaAnalysisRuntime{cache: store}, work, refs, raw
}

func TestAnalysisPreviewManifestAcquireBindsProviderAndCompleteReuse(t *testing.T) {
	runtime, work, refs, _ := previewManifestTestFixture(t)
	// Delivery may keep serving a sound older generation after the runtime's
	// tool inventory changes. Only task reuse compares admitted execution tools.
	currentExecution := work.Execution
	currentExecution.FFmpegSHA256 = strings.Repeat("d", 64)
	runtime.profiles = map[string]library.AnalysisExecutionProfile{library.TaskPreviewGenerationKey: currentExecution}
	for _, example := range []struct {
		name string
		refs []library.AnalysisPreview
		work *library.AnalysisWork
	}{
		{"provider single variant", refs[1:2], nil},
		{"task complete variants", refs, &work},
	} {
		t.Run(example.name, func(t *testing.T) {
			lease, err := runtime.acquirePreviewManifest(context.Background(), example.refs, example.work)
			if lease != nil {
				defer lease.Close()
			}
			if err != nil || lease == nil || lease.Artifact.Name != "manifest.json" || lease.Seal != refs[0].Seal {
				t.Fatalf("valid sealed metadata could not be acquired: %v", err)
			}
			if runtime.cache.Stats().Readers != 1 {
				t.Fatal("successful manifest acquisition did not retain its caller-owned lease")
			}
			if err := runtime.cache.Delete(context.Background(), refs[0].CacheKey, refs[0].Seal); !errors.Is(err, analysiscache.ErrBusy) {
				t.Fatalf("live manifest lease did not pin its generation: %v", err)
			}
			if _, err := lease.File.Stat(); err != nil {
				t.Fatalf("caller received a closed manifest descriptor: %v", err)
			}
			if err := lease.Close(); err != nil || runtime.cache.Stats().Readers != 0 {
				t.Fatalf("caller close did not release the manifest lease: %v", err)
			}
		})
	}
}

func TestAnalysisPreviewManifestRejectsUnsealedReferenceMetadata(t *testing.T) {
	runtime, _, refs, _ := previewManifestTestFixture(t)
	for _, example := range []struct {
		name   string
		mutate func(*library.AnalysisPreview)
	}{
		{"height", func(r *library.AnalysisPreview) { r.Height++ }},
		{"actual timeline", func(r *library.AnalysisPreview) { r.ActualTicks[1]++ }},
		{"source identity", func(r *library.AnalysisPreview) { r.SourceRevision = "another-source" }},
		{"item identity", func(r *library.AnalysisPreview) { r.ItemID = "another-item" }},
		{"profile fingerprint", func(r *library.AnalysisPreview) { r.ProfileFingerprint = strings.Repeat("d", 64) }},
		{"configuration revision", func(r *library.AnalysisPreview) { r.ProfileRevision = "2" }},
		{"publication epoch", func(r *library.AnalysisPreview) { r.PublicationEpoch++ }},
		{"artifact hash", func(r *library.AnalysisPreview) { r.SHA256 = strings.Repeat("e", 64) }},
		{"artifact extent", func(r *library.AnalysisPreview) { r.Bytes++ }},
	} {
		t.Run(example.name, func(t *testing.T) {
			changed := previewManifestTestClone(refs[1:2])
			example.mutate(&changed[0])
			lease, err := runtime.acquirePreviewManifest(context.Background(), changed, nil)
			if lease != nil {
				_ = lease.Close()
			}
			if !errors.Is(err, analysiscache.ErrUnsafe) || lease != nil || runtime.cache.Stats().Readers != 0 {
				t.Fatalf("unsealed reference metadata was admitted or retained a reader: %v", err)
			}
		})
	}
}

func TestAnalysisPreviewManifestRejectsDifferentWorkPolicyToolsAndGenerations(t *testing.T) {
	runtime, work, refs, _ := previewManifestTestFixture(t)
	for _, example := range []struct {
		name   string
		mutate func(*library.AnalysisWork)
	}{
		{"quality", func(w *library.AnalysisWork) { w.Profile.PreviewQuality-- }},
		{"ffmpeg", func(w *library.AnalysisWork) { w.Execution.FFmpegSHA256 = strings.Repeat("d", 64) }},
		{"ffprobe", func(w *library.AnalysisWork) { w.Execution.FFprobeSHA256 = strings.Repeat("e", 64) }},
		{"preview profile", func(w *library.AnalysisWork) { w.Execution.PreviewProfile = "different-preview-profile" }},
		{"expected widths", func(w *library.AnalysisWork) { w.Execution.PreviewWidths = []int{240, 320} }},
		{"expected source", func(w *library.AnalysisWork) { w.Sources[0].SourceRevision = "different-work-source" }},
	} {
		t.Run(example.name, func(t *testing.T) {
			changed := work
			changed.Sources = append([]library.AnalysisSource(nil), work.Sources...)
			changed.Execution.PreviewWidths = append([]int(nil), work.Execution.PreviewWidths...)
			example.mutate(&changed)
			lease, err := runtime.acquirePreviewManifest(context.Background(), refs, &changed)
			if lease != nil {
				_ = lease.Close()
			}
			if !errors.Is(err, analysiscache.ErrUnsafe) || lease != nil || runtime.cache.Stats().Readers != 0 {
				t.Fatalf("different work execution facts retained reusable metadata: %v", err)
			}
		})
	}
	for _, mutate := range []func([]library.AnalysisPreview){
		func(values []library.AnalysisPreview) { values[1].CacheKey = strings.Repeat("f", 64) },
		func(values []library.AnalysisPreview) { values[1].Seal = strings.Repeat("e", 64) },
		func(values []library.AnalysisPreview) { values[1].Width = values[0].Width },
	} {
		changed := previewManifestTestClone(refs)
		mutate(changed)
		lease, err := runtime.acquirePreviewManifest(context.Background(), changed, &work)
		if lease != nil {
			_ = lease.Close()
		}
		if !errors.Is(err, analysiscache.ErrUnsafe) || lease != nil || runtime.cache.Stats().Readers != 0 {
			t.Fatalf("mixed-generation or duplicate variants were admitted: %v", err)
		}
	}
	if lease, err := runtime.acquirePreviewManifest(context.Background(), refs[:2], &work); !errors.Is(err, analysiscache.ErrUnsafe) || lease != nil {
		if lease != nil {
			_ = lease.Close()
		}
		t.Fatal("partial variant set was treated as complete task reuse")
	}
	if runtime.cache.Stats().Readers != 0 {
		t.Fatal("partial set rejection leaked a manifest lease")
	}
}

func previewManifestTestPublishRaw(t *testing.T, store *analysiscache.Store, raw []byte, number int) analysiscache.Entry {
	t.Helper()
	key := fmt.Sprintf("%064x", number)
	builder, err := store.Begin(context.Background(), key, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := builder.Abort(context.Background()); err != nil {
			t.Errorf("abort raw manifest fixture: %v", err)
		}
	})
	if _, err := builder.WriteFile(context.Background(), "manifest.json", func(ctx context.Context, w io.Writer) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := w.Write(raw)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	publication, err := builder.Publish(context.Background())
	if publication != nil {
		t.Cleanup(func() {
			if err := publication.Discard(context.Background()); err != nil {
				t.Errorf("discard raw manifest fixture: %v", err)
			}
		})
	}
	if err != nil || publication == nil {
		t.Fatalf("seal raw manifest fixture: %v", err)
	}
	return publication.Entry
}

func TestAnalysisPreviewManifestCanonicalJSONFailuresReleaseActualCacheLeases(t *testing.T) {
	runtime, work, refs, raw := previewManifestTestFixture(t)
	for index, example := range []struct {
		name string
		raw  []byte
	}{
		{"duplicate", bytes.Replace(raw, []byte(`"Version":1`), []byte(`"Version":1,"Version":1`), 1)},
		{"unknown", append([]byte(`{"Unknown":true,`), raw[1:]...)},
		{"missing", bytes.Replace(raw, []byte(`"Quality":95,`), nil, 1)},
		{"nested duplicate", bytes.Replace(raw, []byte(`"Width":240`), []byte(`"Width":240,"Width":240`), 1)},
		{"nested unknown", bytes.Replace(raw, []byte(`"Variants":[{`), []byte(`"Variants":[{"Unknown":true,`), 1)},
		{"trailing", append(append([]byte(nil), raw...), []byte(` {}`)...)},
		{"wrong case", bytes.Replace(raw, []byte(`"SourceRevision"`), []byte(`"sourceRevision"`), 1)},
	} {
		t.Run(example.name, func(t *testing.T) {
			entry := previewManifestTestPublishRaw(t, runtime.cache, example.raw, index+1)
			changed := previewManifestTestClone(refs)
			for number := range changed {
				changed[number].CacheKey, changed[number].Seal = entry.Key, entry.Seal
			}
			lease, err := runtime.acquirePreviewManifest(context.Background(), changed, &work)
			if lease != nil {
				_ = lease.Close()
			}
			if !errors.Is(err, analysiscache.ErrUnsafe) || lease != nil || runtime.cache.Stats().Readers != 0 {
				t.Fatalf("sealed but ambiguous manifest JSON was admitted or leaked a reader: %v", err)
			}
		})
	}
}

func TestAnalysisPreviewManifestOversizeFailsBeforePublication(t *testing.T) {
	runtime, work, refs, raw := previewManifestTestFixture(t)
	oversized := append(append([]byte(nil), raw...), bytes.Repeat([]byte(" "), analysisPreviewManifestLimit+1-len(raw))...)
	if err := validatePreviewManifest(oversized, refs, &work); !errors.Is(err, analysiscache.ErrUnsafe) {
		t.Fatal("oversized manifest passed the pure stored contract")
	}
	builder, err := runtime.cache.Begin(context.Background(), fmt.Sprintf("%064x", 100), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer builder.Abort(context.Background())
	if _, err := builder.WriteFile(context.Background(), "manifest.json", func(_ context.Context, w io.Writer) error { _, err := w.Write(oversized); return err }); !errors.Is(err, analysiscache.ErrLimit) {
		t.Fatalf("actual cache accepted an oversized manifest artifact: %v", err)
	}
	if err := builder.Abort(context.Background()); err != nil {
		t.Fatal(err)
	}
	stats := runtime.cache.Stats()
	if stats.Readers != 0 || stats.BuildingEntries != 0 || stats.PendingPublications != 0 {
		t.Fatalf("oversized manifest retained a cache lifetime: %+v", stats)
	}
}
