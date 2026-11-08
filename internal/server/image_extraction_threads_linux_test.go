//go:build linux

package server

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/media"
)

func TestPreviewBuildExtractionThreadsCaptureResourcesWithoutChangingOutputProfile(t *testing.T) {
	_, _, work, _, _, plan := previewGenerationFixture(t)
	originalWork := work
	originalWork.Execution.PreviewWidths = append([]int(nil), work.Execution.PreviewWidths...)
	runtime := &mediaAnalysisRuntime{server: &Server{cfg: config.Config{Transcoding: config.TranscodingConfig{
		ExtractionThreadCount: 2, ExtractionThreadCountSet: true,
	}}}}
	first, err := runtime.previewBuildExtractor(work, plan)
	if err != nil {
		t.Fatal(err)
	}
	// Saving a later preference may affect the next image build, never a value
	// already shared by this build's audit and three output-width passes.
	runtime.server.cfg.Transcoding.ExtractionThreadCount = 0
	next, err := runtime.previewBuildExtractor(work, plan)
	if err != nil {
		t.Fatal(err)
	}
	if first.ImageExtractionThreads != (media.ImageExtractionThreads{Count: 2, Configured: true}) ||
		next.ImageExtractionThreads != (media.ImageExtractionThreads{Configured: true}) {
		t.Fatal("a settings change rewrote an active build or lost explicit automatic threads")
	}
	if runtime.extractor.ImageExtractionThreads != (media.ImageExtractionThreads{}) || !reflect.DeepEqual(work, originalWork) {
		t.Fatal("image resource selection changed the shared extractor or admitted output profile")
	}
	if first.ExpectedFFmpegSHA256 != next.ExpectedFFmpegSHA256 || first.ExpectedFFprobeSHA256 != next.ExpectedFFprobeSHA256 || first.Limits != next.Limits {
		t.Fatal("changing image threads changed tool identity or derivative resource budgets")
	}
	if threads, err := media.ResolveImageExtractionDecoderThreads(first.ImageExtractionThreads, 8); err != nil || threads != 2 {
		t.Fatalf("the captured positive decoder limit resolved to %d, %v", threads, err)
	}
	if threads, err := media.ResolveImageExtractionDecoderThreads(next.ImageExtractionThreads, 8); err != nil || threads != 0 {
		t.Fatalf("the captured automatic decoder selection resolved to %d, %v", threads, err)
	}
}

func TestPreviewBuildExtractionThreadsPreserveLegacyAndRequestedAutomaticValues(t *testing.T) {
	_, _, work, _, _, plan := previewGenerationFixture(t)
	legacy := &mediaAnalysisRuntime{}
	extractor, err := legacy.previewBuildExtractor(work, plan)
	if err != nil || extractor.ImageExtractionThreads != (media.ImageExtractionThreads{}) {
		t.Fatalf("legacy preview callers lost their fixed decoder policy: %+v, %v", extractor.ImageExtractionThreads, err)
	}
	if threads, err := media.ResolveImageExtractionDecoderThreads(extractor.ImageExtractionThreads, 1); err != nil || threads != 1 {
		t.Fatalf("legacy single-processor preview policy resolved to %d, %v", threads, err)
	}
	managed := &mediaAnalysisRuntime{server: &Server{cfg: config.Config{Transcoding: config.TranscodingConfig{
		ExtractionThreadCount: 8, ExtractionThreadCountSet: true,
	}}}}
	extractor, err = managed.previewBuildExtractor(work, plan)
	if err != nil || extractor.ImageExtractionThreads.Count != 8 || !extractor.ImageExtractionThreads.Configured {
		t.Fatalf("a native automatic boundary lost its requested count: %+v, %v", extractor.ImageExtractionThreads, err)
	}
	if threads, err := media.ResolveImageExtractionDecoderThreads(extractor.ImageExtractionThreads, 8); err != nil || threads != 0 {
		t.Fatalf("a count equal to processor capacity forced a decoder limit: %d, %v", threads, err)
	}
}
