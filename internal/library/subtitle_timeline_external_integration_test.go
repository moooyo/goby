//go:build linux

package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func subtitleTimelineComponentFixture(t *testing.T, path string) BitmapSubtitleComponent {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return BitmapSubtitleComponent{Name: filepath.Base(path), Identity: fileIdentity(stat), Size: stat.Size(),
		ModifiedNS: stat.ModTime().UnixNano(), ChangeTimeNS: media.FileChangeTime(stat), SHA256: hex.EncodeToString(digest[:])}
}

func TestSubtitleTimelineExternalComponentRejectsReplacementSymlinkAndHashMismatch(t *testing.T) {
	for _, mutation := range []string{"replacement", "symlink", "directory", "hash"} {
		t.Run(mutation, func(t *testing.T) {
			root := backgroundClipTestRoot(t)
			path := filepath.Join(root.Name(), "movie.en.sup")
			if err := os.WriteFile(path, []byte("source-bitmap-bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			facts := subtitleTimelineComponentFixture(t, path)
			file, err := openScanFile(root, facts.Name)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := recheckSubtitleTimelineComponent(context.Background(), root, subtitleTimelineExternalFile{facts, file}); err != nil {
				t.Fatal(err)
			}
			if mutation == "hash" {
				facts.SHA256 = strings.Repeat("a", 64)
			} else {
				if err := os.Rename(path, path+".saved"); err != nil {
					t.Fatal(err)
				}
				switch mutation {
				case "replacement":
					err = os.WriteFile(path, []byte("source-bitmap-bytes"), 0600)
				case "symlink":
					err = os.Symlink(filepath.Base(path)+".saved", path)
				case "directory":
					err = os.Mkdir(path, 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := recheckSubtitleTimelineComponent(context.Background(), root, subtitleTimelineExternalFile{facts, file}); !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("%s returned old source content: %v", mutation, err)
			}
		})
	}
}

func TestSubtitleTimelineOnlyExternalGenerationPersistenceAndCatalogFences(t *testing.T) {
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, mediaSourceTestProber{inner: subtitleTimelineFixtureProber{}})
	path := libraryIntegrationFile(t, root, "external-timeline/Film.mkv", "text:source")
	collection := libraryIntegrationCreate(t, ctx, store, "External subtitle timeline", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var itemID, rootID string
	if err := pool.QueryRow(ctx, `SELECT id,root_id FROM items WHERE path=$1`, path).Scan(&itemID, &rootID); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "external-timeline-admin")
	subject := Subject{UserID: viewer}
	directory := filepath.Dir(path)
	for _, name := range []string{"Film.en.sup", "Film.idx", "Film.sub"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("fixture-"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tracks := []BitmapSubtitle{
		{Index: 1000001, Codec: "hdmv_pgs_subtitle", Format: "sup", Filename: "Film.en.sup", Language: "en",
			Components: []BitmapSubtitleComponent{subtitleTimelineComponentFixture(t, filepath.Join(directory, "Film.en.sup"))}},
		{Index: 1000003, Codec: "dvd_subtitle", Format: "vobsub", Filename: "Film.idx", Language: "zh",
			Components: []BitmapSubtitleComponent{subtitleTimelineComponentFixture(t, filepath.Join(directory, "Film.idx")), subtitleTimelineComponentFixture(t, filepath.Join(directory, "Film.sub"))}},
		{Index: 1000005, SourceStreamIndex: 1, Codec: "dvd_subtitle", Format: "vobsub", Filename: "Film.idx", Language: "en",
			Components: []BitmapSubtitleComponent{subtitleTimelineComponentFixture(t, filepath.Join(directory, "Film.idx")), subtitleTimelineComponentFixture(t, filepath.Join(directory, "Film.sub"))}},
	}
	persist := func() {
		t.Helper()
		for index := range tracks {
			track := &tracks[index]
			track.Tag = BitmapSubtitleSourceHash(track.Format, track.SourceStreamIndex, track.Components)
			components, err := json.Marshal(track.Components)
			if err != nil {
				t.Fatal(err)
			}
			_, err = pool.Exec(ctx, `INSERT INTO item_bitmap_subtitles(item_id,root_id,stream_index,relative_path,source_stream_index,format,codec,source_hash,language,components)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(item_id,stream_index) DO UPDATE SET components=EXCLUDED.components,source_hash=EXCLUDED.source_hash`,
				itemID, rootID, track.Index, track.Filename, track.SourceStreamIndex, track.Format, track.Codec, track.Tag, track.Language, components)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	persist()
	detail, err := store.GetSubtitleTimelineItem(ctx, actor, itemID)
	if err != nil || detail.SubtitleStreamCount != 3 {
		t.Fatalf("only-external inventory omitted sidecars: %+v %v", detail, err)
	}
	fence := AnalysisFence(func(OwnedTx) error { return nil })
	request := 0
	claim := func(force bool) SubtitleTimelineJob {
		t.Helper()
		request++
		id := fmt.Sprintf("external-timeline-%d", request)
		queued, err := store.QueueSubtitleTimelines(ctx, actor, AnalysisSelection{ItemIDs: []string{itemID}, Force: force}, id)
		if err != nil || queued.Queued != 1 {
			t.Fatalf("external-only task was not eligible: %+v %v", queued, err)
		}
		job, err := store.ClaimSubtitleTimeline(ctx, fence, collection.ID, id+"-run", id+"-child")
		if err != nil || job == nil || job.SourceRevision == "" || job.BitmapRevision == "" {
			t.Fatalf("external claim omitted its inventory fence: %+v %v", job, err)
		}
		return *job
	}
	complete := func(job SubtitleTimelineJob, code string) {
		t.Helper()
		if err := store.CompleteSubtitleTimeline(ctx, fence, job, SubtitleTimelineResult{ErrorCode: code}); err != nil {
			t.Fatal(err)
		}
	}
	encodeCalls := 0
	encode := func(work context.Context, input *os.File, source MediaFile, job SubtitleTimelineJob, inputs []media.ExternalSubtitleTimelineInput, output io.Writer) (media.SubtitleTimelineSummary, error) {
		encodeCalls++
		var dvd []media.ExternalSubtitleTimelineInput
		var pgs []media.ExternalSubtitleTimelineInput
		for _, input := range inputs {
			if input.Codec == "dvd_subtitle" {
				dvd = append(dvd, input)
			} else {
				pgs = append(pgs, input)
			}
		}
		if len(dvd) != 2 || len(pgs) != 1 || dvd[0].Input != dvd[1].Input || dvd[0].Companion != dvd[1].Companion ||
			pgs[0].Companion != nil || dvd[0].Companion == nil || dvd[0].SourceStreamIndex != 0 || dvd[1].SourceStreamIndex != 1 {
			return media.SubtitleTimelineSummary{}, errors.New("external descriptor mapping or pair deduplication changed")
		}
		data := media.SubtitleTimelineData{Profile: media.SubtitleTimelineExternalProfile, FFprobeSHA256: strings.Repeat("a", 64), DurationTicks: source.Item.Media.DurationTicks}
		for _, input := range inputs {
			if _, err := input.Input.ReadAt(make([]byte, 1), 0); err != nil {
				return media.SubtitleTimelineSummary{}, err
			}
			data.Tracks = append(data.Tracks, media.SubtitleTimelineTrack{SubtitleTimelineTrackSummary: media.SubtitleTimelineTrackSummary{
				StreamIndex: input.StreamIndex, Codec: input.Codec, IntervalCount: 1}, Intervals: []media.SubtitleTimelineInterval{{StartTicks: media.TicksPerSecond, EndTicks: 2 * media.TicksPerSecond}}})
		}
		sort.Slice(data.Tracks, func(i, j int) bool { return data.Tracks[i].StreamIndex < data.Tracks[j].StreamIndex })
		encoded, err := media.MarshalSubtitleTimelines(data)
		if err != nil {
			return media.SubtitleTimelineSummary{}, err
		}
		_, err = output.Write(encoded)
		return data.Summary(int64(len(encoded))), err
	}
	job := claim(false)
	first, err := store.GenerateSubtitleTimelineWithExternal(ctx, job, fence, encode)
	if err != nil || !first.Available || first.Stale || len(first.Tracks) != 3 || first.Profile != media.SubtitleTimelineExternalProfile || encodeCalls != 1 {
		t.Fatalf("external generation failed: %+v calls=%d %v", first, encodeCalls, err)
	}
	complete(job, "")
	file, current, err := store.OpenSubtitleTimelineFor(ctx, subject, itemID)
	if err != nil || file == nil || current.Stale || current.Generation != first.Generation {
		t.Fatalf("current external bundle was not readable: %+v %v", current, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	storage := filepath.Join(directory, "backdrops", "goby-subtitle-timelines", backgroundClipDirectoryName(filepath.Base(path)))
	manifestBefore, err := os.ReadFile(filepath.Join(storage, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertPreserved := func() {
		t.Helper()
		manifest, err := os.ReadFile(filepath.Join(storage, "manifest.json"))
		if err != nil || !bytes.Equal(manifest, manifestBefore) {
			t.Fatalf("failed replacement modified the old manifest: %v", err)
		}
		if _, err := os.Stat(filepath.Join(storage, first.Generation)); err != nil {
			t.Fatal("failed replacement deleted the old generation", err)
		}
	}
	assertStale := func() {
		t.Helper()
		artifact, err := store.GetSubtitleTimelineFor(ctx, subject, itemID)
		if err != nil || !artifact.Available || !artifact.Stale || artifact.Generation != first.Generation {
			t.Fatalf("changed sidecar was not retained as stale: %+v %v", artifact, err)
		}
		file, _, err := store.OpenSubtitleTimelineFor(ctx, subject, itemID)
		if file != nil {
			file.Close()
		}
		if file != nil || !errors.Is(err, ErrSubtitleTimelineStale) {
			t.Fatalf("old external intervals remained playable: %v", err)
		}
	}
	if err := os.Rename(filepath.Join(directory, "Film.sub"), filepath.Join(directory, "Film.sub.saved")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "Film.sub"), []byte("fixture-Film.sub"), 0600); err != nil {
		t.Fatal(err)
	}
	assertStale()
	job = claim(true)
	if _, err := store.GenerateSubtitleTimelineWithExternal(ctx, job, fence, encode); !errors.Is(err, ErrSourceChanged) || encodeCalls != 1 {
		t.Fatalf("replaced companion was passed to the encoder: calls=%d %v", encodeCalls, err)
	}
	complete(job, "source_changed")
	assertPreserved()
	for index := 1; index < len(tracks); index++ {
		tracks[index].Components[1] = subtitleTimelineComponentFixture(t, filepath.Join(directory, "Film.sub"))
	}
	persist()
	job = claim(false)
	retained, err := store.GenerateSubtitleTimelineWithExternal(ctx, job, fence, encode)
	if err != nil || !retained.Reused || !retained.Stale || retained.Generation != first.Generation || encodeCalls != 1 {
		t.Fatalf("catalog refresh regenerated persistent material without Force: %+v %v", retained, err)
	}
	complete(job, "")
	job = claim(true)
	if _, err := pool.Exec(ctx, `UPDATE item_bitmap_subtitles SET stream_index=1000021 WHERE item_id=$1 AND stream_index=1000001`, itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateSubtitleTimelineJob(ctx, fence, job); !errors.Is(err, ErrAnalysisSourceChanged) {
		t.Fatalf("index mutation retained its task authority: %v", err)
	}
	published := false
	if err := store.WithSubtitleTimelinePublication(ctx, fence, job, func() error { published = true; return nil }); !errors.Is(err, ErrAnalysisSourceChanged) || published {
		t.Fatalf("index mutation passed the publication fence: published=%v %v", published, err)
	}
	complete(job, "index_changed")
	assertPreserved()
	assertStale()
	job = claim(true)
	_, err = store.GenerateSubtitleTimelineWithExternal(ctx, job, fence, func(work context.Context, input *os.File, source MediaFile, job SubtitleTimelineJob, inputs []media.ExternalSubtitleTimelineInput, output io.Writer) (media.SubtitleTimelineSummary, error) {
		summary, err := encode(work, input, source, job, inputs, output)
		if err != nil {
			return summary, err
		}
		_, err = pool.Exec(ctx, `UPDATE item_bitmap_subtitles SET title='Changed during generation' WHERE item_id=$1 AND stream_index=1000021`, itemID)
		return summary, err
	})
	if !errors.Is(err, ErrAnalysisSourceChanged) {
		t.Fatalf("concurrent external metadata mutation published: %v", err)
	}
	complete(job, "catalog_changed")
	assertPreserved()
	job = claim(true)
	work, cancel := context.WithCancel(ctx)
	var borrowed []*os.File
	_, err = store.GenerateSubtitleTimelineWithExternal(work, job, fence, func(work context.Context, input *os.File, source MediaFile, job SubtitleTimelineJob, inputs []media.ExternalSubtitleTimelineInput, output io.Writer) (media.SubtitleTimelineSummary, error) {
		borrowed = append(borrowed, input)
		for _, input := range inputs {
			borrowed = append(borrowed, input.Input)
			if input.Companion != nil {
				borrowed = append(borrowed, input.Companion)
			}
		}
		cancel()
		return media.SubtitleTimelineSummary{}, work.Err()
	})
	cancel()
	if !errors.Is(err, context.Canceled) || len(borrowed) == 0 {
		t.Fatalf("canceled external generation did not reach its reader: %v", err)
	}
	for _, file := range borrowed {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("canceled external generation returned before descriptor retirement: %v", err)
		}
	}
	complete(job, "cancelled")
	assertPreserved()
	job = claim(true)
	replaced, err := store.GenerateSubtitleTimelineWithExternal(ctx, job, fence, encode)
	if err != nil || !replaced.Available || replaced.Stale || replaced.Generation == first.Generation || replaced.Reused {
		t.Fatalf("explicit external replacement failed: %+v %v", replaced, err)
	}
	replayed, err := store.GenerateSubtitleTimelineWithExternal(ctx, job, fence, encode)
	if err != nil || !replayed.Reused || replayed.Generation != replaced.Generation {
		t.Fatalf("external Force replay encoded again: %+v %v", replayed, err)
	}
	complete(job, "")
	if _, err := os.Stat(filepath.Join(storage, first.Generation)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful Force did not replace the previous generation: %v", err)
	}
	current, err = store.GetSubtitleTimelineFor(ctx, subject, itemID)
	if err != nil || current.Stale || current.Generation != replaced.Generation {
		t.Fatalf("replacement failed its public readback: %+v %v", current, err)
	}
}
