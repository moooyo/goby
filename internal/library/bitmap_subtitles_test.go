package library

import (
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func bitmapSubtitleTestSnapshot() BitmapSubtitle {
	track := BitmapSubtitle{Index: 100001, SourceStreamIndex: 1, Codec: "dvd_subtitle", Format: "vobsub", Language: "zh", Title: "zh", Filename: "Feature.idx",
		Components: []BitmapSubtitleComponent{
			{Name: "Feature.idx", Identity: "index-file", SHA256: strings.Repeat("a", 64), Size: 100, ModifiedNS: 1, ChangeTimeNS: 2},
			{Name: "Feature.SUB", Identity: "sub-file", SHA256: strings.Repeat("b", 64), Size: 1000, ModifiedNS: 3, ChangeTimeNS: 4},
		}}
	track.Tag = BitmapSubtitleSourceHash(track.Format, track.SourceStreamIndex, track.Components)
	return track
}

func TestBitmapSubtitleSnapshotBindsPairContentWithoutPrivateStatNotifications(t *testing.T) {
	track := bitmapSubtitleTestSnapshot()
	if err := ValidateBitmapSubtitle(track); err != nil {
		t.Fatal(err)
	}
	previous := track.Tag
	track.Components[1].ModifiedNS++
	track.Components[1].ChangeTimeNS++
	track.Components[1].Identity = "replaced-inode"
	if actual := BitmapSubtitleSourceHash(track.Format, track.SourceStreamIndex, track.Components); actual != previous {
		t.Fatal("private filesystem facts changed the semantic content hash")
	}
	track.Components[1].SHA256 = strings.Repeat("c", 64)
	if ValidateBitmapSubtitle(track) == nil || BitmapSubtitleSourceHash(track.Format, track.SourceStreamIndex, track.Components) == previous {
		t.Fatal("a replaced SUB payload retained the indexed content identity")
	}
	track = bitmapSubtitleTestSnapshot()
	if BitmapSubtitleSourceHash(track.Format, 0, track.Components) == track.Tag {
		t.Fatal("two languages of the same pair share a content identity")
	}
}

func TestBitmapSubtitleSnapshotRejectsUnsafeAndAmbiguousComponents(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*BitmapSubtitle)
	}{
		{"negative index", func(v *BitmapSubtitle) { v.Index = -1 }},
		{"public index overflow", func(v *BitmapSubtitle) { v.Index = 1 << 31 }},
		{"source index overflow", func(v *BitmapSubtitle) { v.SourceStreamIndex = 32 }},
		{"missing companion", func(v *BitmapSubtitle) { v.Components = v.Components[:1] }},
		{"wrong stem", func(v *BitmapSubtitle) { v.Components[1].Name = "Another.sub" }},
		{"wrong extension", func(v *BitmapSubtitle) { v.Components[1].Name = "Feature.sup" }},
		{"path traversal", func(v *BitmapSubtitle) { v.Components[1].Name = "../Feature.sub" }},
		{"windows path", func(v *BitmapSubtitle) { v.Components[1].Name = `nested\Feature.sub` }},
		{"primary mismatch", func(v *BitmapSubtitle) { v.Filename = "Feature.en.idx" }},
		{"same inode", func(v *BitmapSubtitle) { v.Components[1].Identity = v.Components[0].Identity }},
		{"changed hash", func(v *BitmapSubtitle) { v.Components[1].SHA256 = strings.Repeat("c", 64) }},
		{"uppercase digest", func(v *BitmapSubtitle) { v.Components[1].SHA256 = strings.Repeat("B", 64) }},
		{"bad digest", func(v *BitmapSubtitle) { v.Components[1].SHA256 = strings.Repeat("z", 64) }},
		{"unknown format", func(v *BitmapSubtitle) { v.Format = "idx" }},
		{"wrong codec", func(v *BitmapSubtitle) { v.Codec = "srt" }},
		{"excessive pair", func(v *BitmapSubtitle) { v.Components[1].Size = media.MaxExternalBitmapSubtitleBytes }},
		{"missing identity", func(v *BitmapSubtitle) { v.Components[1].Identity = "" }},
		{"unknown modification", func(v *BitmapSubtitle) { v.Components[1].ModifiedNS = 0 }},
		{"unknown change time", func(v *BitmapSubtitle) { v.Components[1].ChangeTimeNS = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			track := bitmapSubtitleTestSnapshot()
			test.edit(&track)
			if ValidateBitmapSubtitle(track) == nil {
				t.Fatal("invalid component snapshot was accepted")
			}
		})
	}
}

func TestBitmapSubtitleDirectoryIndexPairsExactNamesAndLongestMediaOwner(t *testing.T) {
	index := newBitmapSubtitleDirectoryIndex(subtitleTestDirectoryEntries("Feature.mkv", "Feature.en.mkv",
		"Feature.sup", "Feature.en.SUP", "Feature.en.zh.idx", "Feature.en.zh.SUB", "Feature.en.zh.sup",
		"Feature.fr.idx", "Feature.fr.sub", "Feature.de.idx", "Feature.es.sub", "Feature2.sup"), "movies")
	first := index.selection("Feature.mkv")
	var names []string
	for _, track := range first.candidates {
		names = append(names, track.filename)
	}
	if first.overflow || !reflect.DeepEqual(names, []string{"Feature.sup", "Feature.fr.idx"}) ||
		!first.present["feature.de.idx"] || !first.present["feature.es.idx"] {
		t.Fatalf("incorrect base-owner candidates or incomplete-pair protection: %+v", first)
	}
	other := index.selection("Feature.en.mkv")
	if len(other.candidates) != 3 || other.candidates[0].filename != "Feature.en.SUP" ||
		other.candidates[0].metadata.Language != "" || other.candidates[2].metadata.Language != "zh" ||
		other.candidates[2].companion != "Feature.en.zh.SUB" {
		t.Fatalf("longest owner or paired metadata was lost: %+v", other)
	}
}

func TestBitmapSubtitleDirectoryIndexRejectsAmbiguousPairsWithoutRetiringThem(t *testing.T) {
	for _, names := range [][]string{
		{"Feature.mkv", "Feature.idx", "Feature.sub", "FEATURE.SUB"},
		{"Feature.mkv", "Feature.idx", "FEATURE.IDX", "Feature.sub"},
		{"Feature.mkv", "Feature.sup", "FEATURE.SUP"},
		{"Feature.mkv", "Feature.mp4", "Feature.idx", "Feature.sub", "Feature.sup"},
	} {
		selected := newBitmapSubtitleDirectoryIndex(subtitleTestDirectoryEntries(names...), "movies").selection("Feature.mkv")
		if len(selected.candidates) != 0 || len(selected.present) == 0 {
			t.Fatalf("ambiguous source was admitted or lost snapshot protection: %+v", selected)
		}
	}
}

func TestBitmapSubtitleProjectionLimitIsSharedWithTextTracks(t *testing.T) {
	item := Item{}
	for index := 0; index < 40; index++ {
		if index%2 == 0 {
			item.Subtitles = append(item.Subtitles, Subtitle{Index: index})
		} else {
			item.BitmapSubtitles = append(item.BitmapSubtitles, BitmapSubtitle{Index: index})
		}
	}
	trimSubtitleProjection(&item)
	if len(item.Subtitles) != 16 || len(item.BitmapSubtitles) != 16 || item.Subtitles[15].Index != 30 || item.BitmapSubtitles[15].Index != 31 {
		t.Fatalf("public projection did not share one ordered limit: %+v", item)
	}
}
