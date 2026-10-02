package library

import (
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestScanProbeSourceAllowsConcurrentDisplayOverlay(t *testing.T) {
	for _, kind := range []string{"Movie", "Episode", "Audio"} {
		t.Run(kind, func(t *testing.T) {
			prepared := storedFile{id: "item", itemType: kind, name: "Automatic", sortName: "automatic",
				overview: "Automatic overview", automatic: &MetadataValues{Name: "Automatic"}}
			current := prepared
			current.name, current.sortName, current.overview = "Manual", "manual", "Manual overview"
			if kind == "Episode" || kind == "Audio" {
				current.indexNumber = 9
			}
			if kind == "Audio" {
				current.parentIndexNumber = 2
			}
			if !sameScanProbeSource(current, prepared) {
				t.Fatal("a committed display overlay invalidated unchanged source facts")
			}
		})
	}
}

func TestScanProbeSourceRetainsIdentityAndStructuralFences(t *testing.T) {
	prepared := storedFile{id: "item", rootID: "root", relativePath: "Feature.mp4", identity: "inode",
		parentID: "parent", path: "/root/Feature.mp4", itemType: "Movie", size: 4,
		automatic: &MetadataValues{Name: "Automatic"}}
	for _, scenario := range []struct {
		name   string
		change func(*storedFile)
	}{
		{"item", func(value *storedFile) { value.id = "other" }},
		{"root", func(value *storedFile) { value.rootID = "other" }},
		{"relative-path", func(value *storedFile) { value.relativePath = "Other.mp4" }},
		{"file-identity", func(value *storedFile) { value.identity = "replacement" }},
		{"parent", func(value *storedFile) { value.parentID = "other" }},
		{"path", func(value *storedFile) { value.path = "/root/Other.mp4" }},
		{"role", func(value *storedFile) { value.itemType = "Audio" }},
		{"size", func(value *storedFile) { value.size++ }},
		{"mtime", func(value *storedFile) { changed := time.Unix(1, 0); value.modified = &changed }},
		{"probe", func(value *storedFile) { value.media = &media.Info{Size: 5} }},
		{"automatic-source", func(value *storedFile) { value.automatic = &MetadataValues{Name: "Changed source"} }},
		{"local-source-hash", func(value *storedFile) { value.local.hash = "changed" }},
		{"local-source-path", func(value *storedFile) { value.local.path = "Other.nfo" }},
		{"local-source-bytes", func(value *storedFile) { value.local.raw = []byte("changed") }},
		{"scan-sort-source", func(value *storedFile) { changed := "changed"; value.scanSortName = &changed }},
		{"image-facts", func(value *storedFile) { value.hasLocalImages = true }},
		{"structural-number", func(value *storedFile) { value.indexNumber++ }},
		{"structural-parent-number", func(value *storedFile) { value.parentIndexNumber++ }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			current := prepared
			scenario.change(&current)
			if sameScanProbeSource(current, prepared) {
				t.Fatal("a changed source or structural fact bypassed probe publication fencing")
			}
		})
	}
}

func TestScanProbeSourceWithoutAutomaticProjectionKeepsDisplayFence(t *testing.T) {
	prepared := storedFile{id: "item", itemType: "Movie", name: "Original"}
	current := prepared
	current.name = "Changed"
	if sameScanProbeSource(current, prepared) {
		t.Fatal("legacy display source changed without an independent automatic projection")
	}
	prepared.automatic = &MetadataValues{Name: "Original"}
	if sameScanProbeSource(current, prepared) {
		t.Fatal("a missing current source projection bypassed the legacy display fence")
	}
	prepared.automatic, current.automatic = nil, &MetadataValues{Name: "Original"}
	if sameScanProbeSource(current, prepared) {
		t.Fatal("a missing prepared source projection bypassed the legacy display fence")
	}
	for _, kind := range []string{"Episode", "Audio"} {
		prepared = storedFile{id: "item", itemType: kind, indexNumber: 1}
		current = prepared
		current.indexNumber = 2
		if sameScanProbeSource(current, prepared) {
			t.Fatalf("%s numbering changed without an automatic source projection", kind)
		}
	}
}

func TestScanProbeSourceEpisodeParentNumberRemainsStructural(t *testing.T) {
	prepared := storedFile{id: "item", itemType: "Episode", parentIndexNumber: 1,
		automatic: &MetadataValues{Name: "Episode"}}
	current := prepared
	current.parentIndexNumber = 2
	if sameScanProbeSource(current, prepared) {
		t.Fatal("episode season identity changed during a display-only edit")
	}
}
