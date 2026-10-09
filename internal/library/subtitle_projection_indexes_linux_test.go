//go:build linux

package library

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestSubtitleProjectionUsesEachPositionsMediaSnapshot(t *testing.T) {
	fixture, sidecar, _ := subtitleTestCatalog(t)
	writeBitmapCatalogFiles(t, fixture)
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	owned := ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
	item, err := fixture.store.GetItem(fixture.ctx, fixture.userID, fixture.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Subtitles) != 2 || len(item.BitmapSubtitles) != 3 || owned.Index <= sidecar.Index {
		t.Fatalf("mixed subtitle fixture is incomplete: text=%d bitmap=%d", len(item.Subtitles), len(item.BitmapSubtitles))
	}
	maximum := owned.Index
	for _, track := range item.BitmapSubtitles {
		maximum = max(maximum, track.Index)
	}
	items := []Item{item, item, item, item, item, {ID: "no-subtitle-rows", Media: item.Media}}
	for position, highest := range map[int]int{1: sidecar.Index, 2: maximum} {
		info := *item.Media
		info.Streams = append(append([]media.Stream(nil), item.Media.Streams...), media.Stream{Index: highest})
		items[position].Media = &info
	}
	items[3].Media = &media.Info{}
	items[4].Media = nil
	tx, _, err := fixture.store.beginUserRead(fixture.ctx, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(fixture.ctx)
	if err := attachSubtitles(fixture.ctx, tx, items); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(items[0].Subtitles, item.Subtitles) || !reflect.DeepEqual(items[0].BitmapSubtitles, item.BitmapSubtitles) {
		t.Fatal("the unchanged authorized projection lost mixed subtitle tracks")
	}
	if !reflect.DeepEqual(items[3].Subtitles, item.Subtitles) || !reflect.DeepEqual(items[3].BitmapSubtitles, item.BitmapSubtitles) {
		t.Fatal("the empty embedded namespace discarded external subtitle tracks")
	}
	for _, position := range []int{1, 2} {
		highest := highestEmbeddedStreamIndex(items[position].Media)
		wantText, wantBitmap := []Subtitle{}, []BitmapSubtitle{}
		for _, track := range item.Subtitles {
			if track.Index > highest {
				wantText = append(wantText, track)
			}
		}
		for _, track := range item.BitmapSubtitles {
			if track.Index > highest {
				wantBitmap = append(wantBitmap, track)
			}
		}
		if !reflect.DeepEqual(items[position].Subtitles, wantText) || !reflect.DeepEqual(items[position].BitmapSubtitles, wantBitmap) {
			t.Fatalf("duplicate item position %d used another position's embedded namespace", position)
		}
		if !reflect.DeepEqual(items[position].bitmapSubtitleFacts, items[0].bitmapSubtitleFacts) {
			t.Fatalf("collision filtering discarded private bitmap facts for position %d", position)
		}
	}
	for _, position := range []int{4, 5} {
		if len(items[position].Subtitles) != 0 || len(items[position].BitmapSubtitles) != 0 || len(items[position].bitmapSubtitleFacts) != 0 {
			t.Fatalf("position %d without eligible tracks acquired another item's subtitles", position)
		}
	}
	if len(items[2].bitmapSubtitleFacts) == 0 {
		t.Fatal("fully colliding bitmap tracks lost their private source facts")
	}
	items[2].bitmapSubtitleFacts[0].Components[0].Identity = "changed-private-copy"
	if items[0].bitmapSubtitleFacts[0].Components[0].Identity == "changed-private-copy" ||
		items[0].BitmapSubtitles[0].Components[0].Identity == "changed-private-copy" {
		t.Fatal("duplicate projections share mutable bitmap component facts")
	}
}

var subtitleProjectionBenchmarkResult int

func BenchmarkSubtitleProjectionEmbeddedIndexes(b *testing.B) {
	for _, streamCount := range []int{2, 32, 128} {
		for _, trackCount := range []int{0, 1, 8, 32} {
			b.Run(fmt.Sprintf("streams_%d/tracks_%d", streamCount, trackCount), func(b *testing.B) {
				items := make([]Item, 64)
				ids := make([]string, len(items))
				for position := range items {
					info := &media.Info{Streams: make([]media.Stream, streamCount)}
					for index := range info.Streams {
						info.Streams[index].Index = index
					}
					items[position].ID = fmt.Sprintf("item-%d", position)
					items[position].Media = info
					ids[position] = items[position].ID
				}
				b.Run("loop/repeated", func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						result := 0
						for position := range items {
							for track := 0; track < trackCount; track++ {
								result += highestEmbeddedStreamIndex(items[position].Media)
							}
						}
						subtitleProjectionBenchmarkResult = result
					}
				})
				b.Run("loop/cached", func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						result := 0
						for position := range items {
							projection := subtitleProjectionPosition{index: position, embedded: -2}
							for track := 0; track < trackCount; track++ {
								result += projection.highestEmbeddedIndex(items)
							}
						}
						subtitleProjectionBenchmarkResult = result
					}
				})
				// Include the existing position-map construction in both variants so
				// the cached entry's additional integer storage is measured as well.
				b.Run("positions/repeated", func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						positions := make(map[string][]int, len(items))
						for position := range items {
							positions[items[position].ID] = append(positions[items[position].ID], position)
						}
						result := 0
						for _, id := range ids {
							for track := 0; track < trackCount; track++ {
								for _, position := range positions[id] {
									result += highestEmbeddedStreamIndex(items[position].Media)
								}
							}
						}
						subtitleProjectionBenchmarkResult = result
					}
				})
				b.Run("positions/cached", func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						positions := make(map[string][]subtitleProjectionPosition, len(items))
						for position := range items {
							positions[items[position].ID] = append(positions[items[position].ID], subtitleProjectionPosition{index: position, embedded: -2})
						}
						result := 0
						for _, id := range ids {
							for track := 0; track < trackCount; track++ {
								itemPositions := positions[id]
								for offset := range itemPositions {
									result += itemPositions[offset].highestEmbeddedIndex(items)
								}
							}
						}
						subtitleProjectionBenchmarkResult = result
					}
				})
			})
		}
	}
}
