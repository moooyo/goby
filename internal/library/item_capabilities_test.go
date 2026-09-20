package library

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func TestItemCapabilitySourceEligibilityUsesIndexedSnapshotContract(t *testing.T) {
	allowed := t.TempDir()
	root := filepath.Join(allowed, "library")
	modified := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	// Only the three retained scalar facts are transported by the batch query.
	facts := []byte(`{"ProbeVersion":` + strconv.Itoa(media.CurrentProbeVersion) + `,"FileChangeTimeNs":123,"Size":100}`)
	base := itemCapabilityFact{hasStreams: true, modified: &modified, mediaFacts: facts,
		source: indexedMediaSource{relativePath: "movie.mp4", identity: "device-inode",
			root:      libraryRoot{id: "root", libraryID: "library", path: root, allowedPath: allowed, relativePath: "library"},
			mediaFile: MediaFile{Size: 100, Item: Item{ID: "movie", LibraryID: "library", Type: "Movie", Path: filepath.Join(root, "movie.mp4")}}}}
	for _, test := range []struct {
		name   string
		change func(*itemCapabilityFact)
		want   bool
	}{
		{"current", func(*itemCapabilityFact) {}, true},
		{"folder", func(f *itemCapabilityFact) { f.source.mediaFile.Item.IsFolder = true }, false},
		{"unsupported-kind", func(f *itemCapabilityFact) { f.source.mediaFile.Item.Type = "MusicAlbum" }, false},
		{"missing-streams", func(f *itemCapabilityFact) { f.hasStreams = false }, false},
		{"missing-modified", func(f *itemCapabilityFact) { f.modified = nil }, false},
		{"missing-identity", func(f *itemCapabilityFact) { f.source.identity = "" }, false},
		{"empty-file", func(f *itemCapabilityFact) { f.source.mediaFile.Size = 0 }, false},
		{"stale-probe", func(f *itemCapabilityFact) {
			f.mediaFacts = []byte(`{"ProbeVersion":0,"FileChangeTimeNs":123,"Size":100}`)
		}, false},
		{"wrong-json-type", func(f *itemCapabilityFact) { f.mediaFacts = []byte(`{"ProbeVersion":"8","FileChangeTimeNs":123}`) }, false},
		{"missing-ctime", func(f *itemCapabilityFact) {
			f.mediaFacts = []byte(`{"ProbeVersion":` + strconv.Itoa(media.CurrentProbeVersion) + `}`)
		}, false},
		{"size-mismatch", func(f *itemCapabilityFact) { f.source.mediaFile.Size = 101 }, false},
		{"path-mismatch", func(f *itemCapabilityFact) { f.source.mediaFile.Item.Path = filepath.Join(root, "another.mp4") }, false},
		{"root-mismatch", func(f *itemCapabilityFact) { f.source.root.libraryID = "another-library" }, false},
		{"path-escape", func(f *itemCapabilityFact) { f.source.relativePath = "../outside.mp4" }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fact := base
			test.change(&fact)
			if got := itemCapabilitySourceEligible(fact); got != test.want {
				t.Fatalf("source eligibility = %t, want %t", got, test.want)
			}
		})
	}
}

func TestItemCapabilitiesRejectUnboundedInputBeforeDatabaseAccess(t *testing.T) {
	var store *Store
	for _, ids := range [][]string{{""}, {" "}, {"bad\x00id"}, {strings.Repeat("x", 257)}, make([]string, 1001)} {
		if _, err := store.ItemCapabilitiesFor(context.Background(), identity.Principal{}, ids); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid IDs were not rejected before database access: %v", err)
		}
	}
	if result, err := store.ItemCapabilitiesFor(context.Background(), identity.Principal{}, nil); err != nil || len(result) != 0 {
		t.Fatalf("empty projection performed database work: %#v, %v", result, err)
	}
}
