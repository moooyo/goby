//go:build linux

package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

type subtitleTimelineStorageProber struct{}

func (subtitleTimelineStorageProber) ProbeFileJoinedContract() bool { return true }
func (subtitleTimelineStorageProber) ProbeFile(_ context.Context, file *os.File) (media.Info, error) {
	stat, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	return media.Info{Container: "matroska,webm", Size: stat.Size(), DurationTicks: 20 * media.TicksPerSecond,
		FormatStartKnown: true, Streams: []media.Stream{
			{Index: 0, CodecType: "video", Codec: "h264", Width: 1920, Height: 1080},
			{Index: 1, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle", TimeBase: "1/90000"},
			{Index: 3, CodecType: "subtitle", Codec: "dvd_subtitle", TimeBase: "1/1000"},
			{Index: 5, CodecType: "subtitle", Codec: "subrip", IsTextSubtitleStream: true},
			{Index: 7, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle", IsExternal: true},
		}}, nil
}

func TestSubtitleTimelineStorageGenerationRetentionAndReadFences(t *testing.T) {
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, mediaSourceTestProber{inner: subtitleTimelineStorageProber{}})
	path := libraryIntegrationFile(t, root, "subtitle-timelines/Film.mkv", "video:subtitle-timeline-storage")
	collection := libraryIntegrationCreate(t, ctx, store, "Subtitle timeline storage", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1`, path).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "subtitle-timeline-storage-admin")
	viewerSubject := Subject{UserID: viewer}
	fence := AnalysisFence(func(OwnedTx) error { return nil })
	if artifact, err := store.GetSubtitleTimelineFor(ctx, viewerSubject, itemID); !errors.Is(err, ErrNotFound) || artifact.Available {
		t.Fatalf("missing generation was not absent: %+v %v", artifact, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "backdrops")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reading ungenerated material created sidecar storage: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM subtitle_timeline_queue`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("read queued generation: %d %v", count, err)
	}
	request := 0
	claim := func(force bool) SubtitleTimelineJob {
		t.Helper()
		request++
		id := fmt.Sprintf("subtitle-storage-%d", request)
		if _, err := store.QueueSubtitleTimelines(ctx, actor, AnalysisSelection{ItemIDs: []string{itemID}, Force: force}, id); err != nil {
			t.Fatal(err)
		}
		job, err := store.ClaimSubtitleTimeline(ctx, fence, collection.ID, id+"-run", id+"-child")
		if err != nil || job == nil || job.SourceRevision == "" {
			t.Fatalf("claim failed: %+v %v", job, err)
		}
		return *job
	}
	complete := func(job SubtitleTimelineJob, result SubtitleTimelineResult) {
		t.Helper()
		if err := store.CompleteSubtitleTimeline(ctx, fence, job, result); err != nil {
			t.Fatal(err)
		}
	}
	encodedCalls := 0
	encode := func(work context.Context, input *os.File, source MediaFile, job SubtitleTimelineJob, output io.Writer) (media.SubtitleTimelineSummary, error) {
		encodedCalls++
		if err := work.Err(); err != nil {
			return media.SubtitleTimelineSummary{}, err
		}
		if source.Item.ID != job.ItemID || source.SourceID != job.MediaSourceID {
			return media.SubtitleTimelineSummary{}, errors.New("encoder received another admitted source")
		}
		if _, err := io.ReadAll(input); err != nil {
			return media.SubtitleTimelineSummary{}, err
		}
		data := subtitleTimelineTestData(2)
		data.Tracks[1].Codec = "dvd_subtitle"
		data.Tracks[1].Warnings = []string{"dvd_palette_missing_monochrome_review"}
		encoded, err := media.MarshalSubtitleTimelines(data)
		if err != nil {
			return media.SubtitleTimelineSummary{}, err
		}
		_, err = output.Write(encoded)
		return data.Summary(int64(len(encoded))), err
	}
	job := claim(false)
	first, err := store.GenerateSubtitleTimeline(ctx, job, fence, encode)
	if err != nil || !first.Available || first.Stale || first.Reused || encodedCalls != 1 || len(first.Tracks) != 2 {
		t.Fatalf("first generation failed: %+v calls=%d %v", first, encodedCalls, err)
	}
	complete(job, SubtitleTimelineResult{})
	readCurrent := func(expected SubtitleTimelineArtifact) []byte {
		t.Helper()
		file, artifact, err := store.OpenSubtitleTimelineFor(ctx, viewerSubject, itemID)
		if err != nil || file == nil || !artifact.Available || artifact.Stale || artifact.Generation != expected.Generation {
			t.Fatalf("current material unavailable: %+v %v", artifact, err)
		}
		data, readErr := io.ReadAll(file)
		if err := errors.Join(readErr, file.Close()); err != nil {
			t.Fatal(err)
		}
		parsed, err := media.ParseSubtitleTimelines(data)
		if err != nil || len(parsed.Tracks) != 2 || parsed.Tracks[0].StreamIndex != 1 || parsed.Tracks[1].StreamIndex != 3 || parsed.Tracks[1].Codec != "dvd_subtitle" {
			t.Fatalf("opened bundle lost absolute stream identity: %+v %v", parsed, err)
		}
		return data
	}
	firstBytes := readCurrent(first)
	storagePath := filepath.Join(filepath.Dir(path), "backdrops", "goby-subtitle-timelines", backgroundClipDirectoryName(filepath.Base(path)))
	manifestBefore, err := os.ReadFile(filepath.Join(storagePath, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	job = claim(false)
	reused, err := store.GenerateSubtitleTimeline(ctx, job, fence, encode)
	if err != nil || !reused.Reused || reused.Generation != first.Generation || encodedCalls != 1 {
		t.Fatalf("ordinary request regenerated persistent data: %+v calls=%d %v", reused, encodedCalls, err)
	}
	complete(job, SubtitleTimelineResult{Reused: reused.Reused})
	for _, failure := range []string{"encoder", "cancel", "partial_inventory"} {
		job = claim(true)
		work, cancel := context.WithCancel(ctx)
		fault := errors.New("injected encoder failure")
		_, err = store.GenerateSubtitleTimeline(work, job, fence, func(work context.Context, input *os.File, source MediaFile, job SubtitleTimelineJob, output io.Writer) (media.SubtitleTimelineSummary, error) {
			if failure == "partial_inventory" {
				data := subtitleTimelineTestData(1)
				encoded, err := media.MarshalSubtitleTimelines(data)
				if err != nil {
					return media.SubtitleTimelineSummary{}, err
				}
				_, err = output.Write(encoded)
				return data.Summary(int64(len(encoded))), err
			}
			if _, err := output.Write([]byte("incomplete")); err != nil {
				return media.SubtitleTimelineSummary{}, err
			}
			if failure == "cancel" {
				cancel()
				return media.SubtitleTimelineSummary{}, work.Err()
			}
			return media.SubtitleTimelineSummary{}, fault
		})
		cancel()
		if err == nil {
			t.Fatalf("%s published a failed or incomplete generation", failure)
		}
		complete(job, SubtitleTimelineResult{ErrorCode: "fixture_" + failure})
		if got := readCurrent(first); !bytes.Equal(got, firstBytes) {
			t.Fatalf("%s discarded previously generated material", failure)
		}
		manifestAfter, err := os.ReadFile(filepath.Join(storagePath, "manifest.json"))
		if err != nil || !bytes.Equal(manifestBefore, manifestAfter) {
			t.Fatalf("%s altered the prior manifest: %v", failure, err)
		}
	}
	job = claim(true)
	replaced, err := store.GenerateSubtitleTimeline(ctx, job, fence, encode)
	if err != nil || !replaced.Available || replaced.Reused || replaced.Generation == first.Generation || encodedCalls != 2 {
		t.Fatalf("explicit replacement failed: %+v calls=%d %v", replaced, encodedCalls, err)
	}
	replayed, err := store.GenerateSubtitleTimeline(ctx, job, fence, encode)
	if err != nil || !replayed.Reused || replayed.Generation != replaced.Generation || encodedCalls != 2 {
		t.Fatalf("lost acknowledgement repeated Force encoding: %+v calls=%d %v", replayed, encodedCalls, err)
	}
	complete(job, SubtitleTimelineResult{Reused: replayed.Reused})
	if _, err := os.Stat(filepath.Join(storagePath, first.Generation)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful explicit replacement retained obsolete generation: %v", err)
	}
	readCurrent(replaced)
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stat.ModTime(), stat.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	stale, err := store.GetSubtitleTimelineFor(ctx, viewerSubject, itemID)
	if err != nil || !stale.Available || !stale.Stale || stale.Generation != replaced.Generation {
		t.Fatalf("unscanned physical mutation was not retained as stale: %+v %v", stale, err)
	}
	if file, _, err := store.OpenSubtitleTimelineFor(ctx, viewerSubject, itemID); file != nil || !errors.Is(err, ErrSubtitleTimelineStale) {
		if file != nil {
			file.Close()
		}
		t.Fatalf("old intervals leaked to playback after source mutation: %v", err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	job = claim(false)
	retained, err := store.GenerateSubtitleTimeline(ctx, job, fence, encode)
	if err != nil || !retained.Reused || !retained.Stale || retained.Generation != replaced.Generation || encodedCalls != 2 {
		t.Fatalf("rescan caused implicit regeneration: %+v calls=%d %v", retained, encodedCalls, err)
	}
	complete(job, SubtitleTimelineResult{Reused: true})
	if _, err := os.Stat(filepath.Join(storagePath, replaced.Generation)); err != nil {
		t.Fatal("stale material was cleared without explicit replacement", err)
	}
	libraryIntegrationUser(t, ctx, pool, "subtitle-timeline-hidden", false, false, nil)
	if file, _, err := store.OpenSubtitleTimelineFor(ctx, Subject{UserID: "subtitle-timeline-hidden"}, itemID); file != nil || !errors.Is(err, ErrNotFound) {
		if file != nil {
			file.Close()
		}
		t.Fatalf("hidden reader observed generated material: %v", err)
	}
}
