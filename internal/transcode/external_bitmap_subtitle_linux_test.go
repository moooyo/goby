//go:build linux

package transcode

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func externalBitmapAssetFixture(t *testing.T) (Spec, media.ExternalSubtitleTimelineInput, []byte) {
	t.Helper()
	plan := externalBitmapTestPlan()
	plan.Subtitle.Codec, plan.Subtitle.ExternalStreamIndex = "hdmv_pgs_subtitle", 0
	data := bitmapSubtitlePGSFixture()
	path := filepath.Join(t.TempDir(), "movie.en.sup")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	spec := managerTestSpec(1)
	spec.Plan = plan
	return spec, media.ExternalSubtitleTimelineInput{StreamIndex: plan.Subtitle.StreamIndex, Codec: plan.Subtitle.Codec, Input: file}, data
}

func TestExternalBitmapAssetsBorrowDescriptorsAndBindCompletePlan(t *testing.T) {
	spec, source, contents := externalBitmapAssetFixture(t)
	if _, err := source.Input.Seek(13, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	loads := 0
	ctx := withBitmapSubtitleSource(context.Background(), spec, func(ctx context.Context, got Spec, consume func(context.Context, media.ExternalSubtitleTimelineInput) error) error {
		loads++
		if got != spec {
			t.Fatal("external asset loading lost its source or authorization scope")
		}
		return consume(ctx, source)
	})
	directory := t.TempDir()
	if err := PrepareSubtitleAssets(ctx, "unused", directory, nil, spec.Plan); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "subtitle.sup"))
	if err != nil || !bytes.Equal(data, contents) || loads != 1 {
		t.Fatalf("private SUP snapshot differs from its authorized source: %v", err)
	}
	if position, err := source.Input.Seek(0, io.SeekCurrent); err != nil || position != 13 {
		t.Fatalf("borrowed subtitle descriptor was closed or repositioned: %d, %v", position, err)
	}
	changed := spec.Plan
	changed.Subtitle.OffsetTicks++
	if err := PrepareSubtitleAssets(ctx, "unused", t.TempDir(), nil, changed); !errors.Is(err, ErrInvalidInput) || loads != 1 {
		t.Fatalf("changed plan reused an old source loader: %v", err)
	}
	if err := PrepareSubtitleAssets(context.Background(), "unused", t.TempDir(), nil, spec.Plan); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("external bitmap loaded without bound authority: %v", err)
	}
	if err := PrepareSubtitleAssets(ctx, "unused", directory, nil, spec.Plan); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing private bitmap asset was overwritten: %v", err)
	}
	if preserved, err := os.ReadFile(filepath.Join(directory, "subtitle.sup")); err != nil || !bytes.Equal(preserved, contents) {
		t.Fatal("failed preparation removed another invocation's existing asset")
	}
}

func TestExternalBitmapAssetsFailClosedAndRemoveRejectedSnapshots(t *testing.T) {
	for _, failure := range []string{"index", "ordinal", "codec", "empty callback", "duplicate callback", "swallowed error", "late source failure", "cancelled after copy", "primary alias", "malformed", "oversized"} {
		t.Run(failure, func(t *testing.T) {
			spec, source, _ := externalBitmapAssetFixture(t)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			var primary *os.File
			switch failure {
			case "index":
				source.StreamIndex++
			case "ordinal":
				source.SourceStreamIndex++
			case "codec":
				source.Codec = "dvd_subtitle"
			case "primary alias":
				primary = source.Input
			case "malformed":
				if err := os.WriteFile(source.Input.Name(), []byte("invalid SUP"), 0600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.Truncate(source.Input.Name(), media.MaxExternalBitmapSubtitleBytes+1); err != nil {
					t.Fatal(err)
				}
			}
			ctx := withBitmapSubtitleSource(parent, spec, func(ctx context.Context, _ Spec, consume func(context.Context, media.ExternalSubtitleTimelineInput) error) error {
				if failure == "empty callback" {
					return nil
				}
				if failure == "swallowed error" {
					invalid := source
					invalid.StreamIndex++
					_ = consume(ctx, invalid)
					return nil
				}
				if err := consume(ctx, source); err != nil {
					return err
				}
				switch failure {
				case "duplicate callback":
					return consume(ctx, source)
				case "late source failure":
					return ErrInvalidInput
				case "cancelled after copy":
					cancel()
				}
				return nil
			})
			directory := t.TempDir()
			if err := PrepareSubtitleAssets(ctx, "unused", directory, primary, spec.Plan); err == nil {
				t.Fatal("invalid or cancelled source asset was accepted")
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 {
				t.Fatalf("rejected source left private assets behind: %v, %v", entries, err)
			}
		})
	}
}

func TestManagerExternalBitmapRequiresItsDedicatedSourceLoader(t *testing.T) {
	options := managerTestOptions(t, func(context.Context, string, string, *os.File, Plan, int, func(Progress)) (RunResult, error) {
		t.Error("unbound external bitmap reached the runner")
		return RunResult{}, nil
	})
	options.SubtitleSource = func(context.Context, Spec) ([]byte, error) { return []byte("text"), nil }
	manager := newTestManager(t, options)
	spec := managerTestSpec(1)
	spec.Plan = externalBitmapTestPlan()
	if _, err := manager.Ensure(context.Background(), spec, managerTestInput(t)); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("text loader authorized a bitmap source: %v", err)
	}
}

func TestExternalBitmapVobSubAssetsRetainSecondLanguageAndRollbackPair(t *testing.T) {
	// Reuse the repository's independently authored bilingual sidecar fixture.
	// Its DVD IDs are 3 and 7, distinct from demux ordinals 0 and 1.
	indexData, err := os.ReadFile(filepath.Join("..", "server", "testdata", "external-bitmap", "captions.idx"))
	if err != nil {
		t.Fatal(err)
	}
	subData, err := os.ReadFile(filepath.Join("..", "server", "testdata", "external-bitmap", "captions.sub"))
	if err != nil {
		t.Fatal(err)
	}
	paths := t.TempDir()
	var files []*os.File
	for _, entry := range []struct {
		name string
		data []byte
	}{{"source.idx", indexData}, {"source.sub", subData}} {
		path := filepath.Join(paths, entry.name)
		if err := os.WriteFile(path, entry.data, 0600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		files = append(files, file)
	}
	spec := managerTestSpec(1)
	spec.Plan = externalBitmapTestPlan()
	source := media.ExternalSubtitleTimelineInput{StreamIndex: spec.Plan.Subtitle.StreamIndex, SourceStreamIndex: 1,
		Codec: "dvd_subtitle", Input: files[0], Companion: files[1]}
	ctx := withBitmapSubtitleSource(context.Background(), spec, func(ctx context.Context, _ Spec, consume func(context.Context, media.ExternalSubtitleTimelineInput) error) error {
		return consume(ctx, source)
	})
	for _, blocked := range []bool{false, true} {
		directory := t.TempDir()
		if blocked {
			if err := os.WriteFile(filepath.Join(directory, "subtitle.sub"), []byte("existing asset"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		err := PrepareSubtitleAssets(ctx, "unused", directory, nil, spec.Plan)
		if blocked {
			if !errors.Is(err, os.ErrExist) {
				t.Fatalf("existing SUB did not reject pair creation: %v", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "subtitle.idx")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed pair retained its new IDX asset: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(directory, "subtitle.sub"))
			if err != nil || string(data) != "existing asset" {
				t.Fatal("pair rollback removed an existing SUB asset")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(directory, "subtitle.sub"))
		if err != nil || !bytes.Equal(data, subData) {
			t.Fatal("VobSub preparation changed the paired packet bytes")
		}
		index, err := os.ReadFile(filepath.Join(directory, "subtitle.idx"))
		if err != nil || !strings.Contains(string(index), "id: en, index: 3") || !strings.Contains(string(index), "id: zh, index: 7") {
			t.Fatalf("canonical IDX lost its language order or DVD IDs: %s, %v", index, err)
		}
	}
}
