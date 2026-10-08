package library

import (
	"runtime"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func subtitleTimelineExternalTestTrack(index int) BitmapSubtitle {
	track := BitmapSubtitle{Index: index, Codec: "hdmv_pgs_subtitle", Format: "sup", Filename: "movie.en.sup", Language: "en",
		Components: []BitmapSubtitleComponent{{Name: "movie.en.sup", Identity: "10:30", Size: 100, ModifiedNS: 1001, ChangeTimeNS: 1002, SHA256: strings.Repeat("c", 64)}}}
	track.Tag = BitmapSubtitleSourceHash(track.Format, track.SourceStreamIndex, track.Components)
	return track
}

func TestSubtitleTimelineExternalStampBindsEntireSetAndKeepsLegacyVersion(t *testing.T) {
	base := subtitleTimelineTestSnapshot()
	legacy, err := subtitleTimelineSourceStamp(base)
	if err != nil || !strings.HasPrefix(legacy, "subtitle-timeline-source-v1-") {
		t.Fatalf("legacy stamp changed version: %q %v", legacy, err)
	}
	base.mediaFile.Item.BitmapSubtitles = []BitmapSubtitle{subtitleTimelineExternalTestTrack(1000001)}
	current, err := subtitleTimelineSourceStamp(base)
	if err != nil || !strings.HasPrefix(current, "subtitle-timeline-source-v2-") || current == legacy {
		t.Fatalf("sidecar did not select the external source stamp: %q %v", current, err)
	}
	mutations := []func(*BitmapSubtitle){
		func(track *BitmapSubtitle) { track.Index++ },
		func(track *BitmapSubtitle) { track.Title = "Replacement title" },
		func(track *BitmapSubtitle) { track.Language = "fr" },
		func(track *BitmapSubtitle) { track.IsDefault = true },
		func(track *BitmapSubtitle) { track.IsForced = true },
		func(track *BitmapSubtitle) { track.IsHearingImpaired = true },
		func(track *BitmapSubtitle) { track.Components[0].Identity = "10:31" },
		func(track *BitmapSubtitle) { track.Components[0].Size++ },
		func(track *BitmapSubtitle) { track.Components[0].ModifiedNS++ },
		func(track *BitmapSubtitle) { track.Components[0].ChangeTimeNS++ },
		func(track *BitmapSubtitle) {
			track.Components[0].SHA256 = strings.Repeat("d", 64)
			track.Tag = BitmapSubtitleSourceHash(track.Format, track.SourceStreamIndex, track.Components)
		},
	}
	for index, mutate := range mutations {
		changed := subtitleTimelineTestSnapshot()
		track := subtitleTimelineExternalTestTrack(1000001)
		mutate(&track)
		changed.mediaFile.Item.BitmapSubtitles = []BitmapSubtitle{track}
		stamp, err := subtitleTimelineSourceStamp(changed)
		if err != nil || stamp == current {
			t.Fatalf("mutation %d did not fence its sidecar snapshot: %q %v", index, stamp, err)
		}
	}
	second := subtitleTimelineExternalTestTrack(1000002)
	second.Filename, second.Components[0].Name = "movie.fr.sup", "movie.fr.sup"
	base.mediaFile.Item.BitmapSubtitles = append(base.mediaFile.Item.BitmapSubtitles, second)
	two, err := subtitleTimelineSourceStamp(base)
	if err != nil || two == current {
		t.Fatalf("newly indexed sidecar did not invalidate the bundle: %q %v", two, err)
	}
	base.mediaFile.Item.BitmapSubtitles[0], base.mediaFile.Item.BitmapSubtitles[1] = base.mediaFile.Item.BitmapSubtitles[1], base.mediaFile.Item.BitmapSubtitles[0]
	reordered, err := subtitleTimelineSourceStamp(base)
	if err != nil || reordered != two {
		t.Fatalf("query ordering changed a canonical source stamp: %q %q %v", two, reordered, err)
	}
	base.mediaFile.Item.BitmapSubtitles = nil
	restored, err := subtitleTimelineSourceStamp(base)
	if err != nil || restored != legacy {
		t.Fatalf("empty external set changed legacy stamp bytes: %q %q %v", legacy, restored, err)
	}
}

func TestSubtitleTimelineRawBitmapFactsFenceHiddenIndexAndParentBinding(t *testing.T) {
	snapshot := subtitleTimelineTestSnapshot()
	relative := "Movie/movie.en.sup"
	if runtime.GOOS == "windows" {
		// A root-level fixture keeps the catalog path valid without native
		// separators, while retaining the hidden-index and parent checks.
		snapshot.relativePath = "movie.mkv"
		relative = "movie.en.sup"
	}
	legacy, _ := subtitleTimelineSourceStamp(snapshot)
	track := subtitleTimelineExternalTestTrack(1)
	snapshot.mediaFile.Item.bitmapSubtitleFacts = []storedBitmapSubtitle{{BitmapSubtitle: track, relativePath: relative, rootID: snapshot.root.id}}
	stamp, err := subtitleTimelineSourceStamp(snapshot)
	if err != nil || stamp == legacy || !strings.HasPrefix(stamp, "subtitle-timeline-source-v2-") {
		t.Fatalf("invisible index collision lost the raw catalog set: %q %v", stamp, err)
	}
	summary := subtitleTimelineTestData(1).Summary(100)
	if subtitleTimelineSummaryMatchesSource(summary, snapshot.mediaFile.Item.Media, subtitleTimelineSnapshotBitmap(snapshot)) {
		t.Fatal("colliding external index could publish a timeline")
	}
	snapshot.relativePath = "Other/movie.mkv"
	if _, err := subtitleTimelineSourceStamp(snapshot); err == nil {
		t.Fatal("another directory was accepted as the media sidecar parent")
	}
}

func TestSubtitleTimelineMixedSummaryRequiresEveryEmbeddedAndExternalTrack(t *testing.T) {
	snapshot := subtitleTimelineTestSnapshot()
	track := subtitleTimelineExternalTestTrack(1000001)
	summary := subtitleTimelineTestData(1).Summary(100)
	summary.Profile = media.SubtitleTimelineExternalProfile
	summary.Tracks = append(summary.Tracks, media.SubtitleTimelineTrackSummary{StreamIndex: track.Index, Codec: track.Codec, IntervalCount: 1})
	if !subtitleTimelineSummaryMatchesSource(summary, snapshot.mediaFile.Item.Media, []BitmapSubtitle{track}) {
		t.Fatal("complete mixed embedded and external summary rejected")
	}
	for _, mutate := range []func(*media.SubtitleTimelineSummary){
		func(summary *media.SubtitleTimelineSummary) { summary.Tracks = summary.Tracks[:1] },
		func(summary *media.SubtitleTimelineSummary) { summary.Tracks[1].StreamIndex++ },
		func(summary *media.SubtitleTimelineSummary) { summary.Tracks[1].Codec = "dvd_subtitle" },
		func(summary *media.SubtitleTimelineSummary) { summary.Profile = media.SubtitleTimelineProfile },
	} {
		changed := summary
		changed.Tracks = cloneSubtitleTimelineTracks(summary.Tracks)
		mutate(&changed)
		if subtitleTimelineSummaryMatchesSource(changed, snapshot.mediaFile.Item.Media, []BitmapSubtitle{track}) {
			t.Fatal("incomplete or mismatched mixed summary accepted")
		}
	}
}

func TestSubtitleTimelineJobRevisionPreservesLegacyAndBindsExternalSet(t *testing.T) {
	job := SubtitleTimelineJob{SourceRevision: "video-revision"}
	if subtitleTimelineJobRevision(job) != job.SourceRevision {
		t.Fatal("legacy claim revision changed")
	}
	job.BitmapRevision = "external-revision"
	first := subtitleTimelineJobRevision(job)
	job.BitmapRevision = "replacement-revision"
	if first == job.SourceRevision || first == subtitleTimelineJobRevision(job) {
		t.Fatal("external inventory did not bind the persistent claim")
	}
}
