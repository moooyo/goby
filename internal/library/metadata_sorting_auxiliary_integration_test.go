//go:build linux

package library

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestAuxiliarySortingRulesPreserveSourceCaseControlsAndCachedScans(t *testing.T) {
	prober := &metadataMusicScanProber{}
	prober.set("theme.mp3", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "The Theme Song"}, false)
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	libraryIntegrationFile(t, root, "movies/Film/The Main.mp4", "video:sort-owner")
	paths := map[string]string{}
	for name, relative := range map[string]string{"The Trailer": "trailers/The Trailer.mp4", "the trailer": "trailers/the trailer.mp4", "The Clip": "featurettes/The Clip.mp4", "The Backdrop": "backdrops/The Backdrop.mp4", "The Theme Song": "theme.mp3"} {
		content := "video:sort-" + name
		if strings.HasSuffix(relative, ".mp3") {
			content = "audio:sort-song"
		}
		paths[name] = libraryIntegrationFile(t, root, "movies/Film/"+relative, content)
	}
	collection := libraryIntegrationCreate(t, ctx, store, "Auxiliary sorting", "movies", filepath.Join(root, "movies"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	resources := append(extraScanTestResources(t, ctx, pool, collection.ID), themeScanTestResources(t, ctx, pool, collection.ID)...)
	if len(resources) != len(paths) {
		t.Fatal("fixture omitted an independent theme or extra source")
	}
	ids := map[string]string{}
	for name, path := range paths {
		for _, resource := range resources {
			if resource.path == path {
				ids[name] = resource.id
			}
		}
		if ids[name] == "" {
			t.Fatalf("missing source %s", name)
		}
	}
	actor := metadataEditTestActor(t, ctx, pool, "auxiliary-sorting-editor")
	controlled := metadataEditTestDetail(t, ctx, store, actor, ids["The Trailer"])
	controlled = metadataEditTestUpdate(t, ctx, store, actor, controlled, map[string]json.RawMessage{"Name": json.RawMessage(`"Independent Display"`), "SortName": json.RawMessage(`"Pinned Sort"`)}, []string{"SortName"})
	controlled = metadataEditTestUpdate(t, ctx, store, actor, controlled, map[string]json.RawMessage{"Name": json.RawMessage(`"Independent Display"`)}, []string{"SortName"})
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: controlled.ItemID, PlaybackPositionTicks: 9007199254740993, PlayCount: 7, IsFavorite: true})
	userData := forceProbeUserDataSnapshot(t, ctx, pool, controlled.ItemID)
	for step, words := range [][]string{{}, {"The"}, {}} {
		if step != 0 {
			if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
				if _, err := tx.Exec(`UPDATE managed_settings SET sort_remove_words=$1`, words); err != nil {
					return err
				}
				return RebuildGeneratedSortNames(tx)
			}); err != nil {
				t.Fatal(err)
			}
		}
		if step == 1 {
			newPath := libraryIntegrationFile(t, root, "movies/Film/trailers/THE New Trailer.mp4", "video:new-configured-auxiliary")
			libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			for _, resource := range extraScanTestResources(t, ctx, pool, collection.ID) {
				if resource.path == newPath {
					ids["THE New Trailer"] = resource.id
				}
			}
			if ids["THE New Trailer"] == "" {
				t.Fatal("configured first publication did not create its auxiliary identity")
			}
		}
		for name, id := range ids {
			want := name
			if len(words) != 0 {
				want = name[4:]
			}
			detail := metadataEditTestDetail(t, ctx, store, actor, id)
			if detail.Automatic.Name != name || detail.Automatic.SortName != want {
				t.Fatalf("step%d changed source name/case or missed prefix: name=%q automatic=%+v", step, name, detail.Automatic)
			}
			if id == controlled.ItemID {
				if detail.Effective.Name != "Independent Display" || detail.Effective.SortName != "Pinned Sort" || !reflect.DeepEqual(detail.Overrides, controlled.Overrides) || !reflect.DeepEqual(detail.LockedValues, controlled.LockedValues) || detail.LastEditedBy != controlled.LastEditedBy || !reflect.DeepEqual(detail.LastEditedAt, controlled.LastEditedAt) {
					t.Fatal("sorting policy rewrote native controls or editor history")
				}
			}
		}
		themesBefore, extrasBefore := themeScanTestSnapshot(t, ctx, pool, collection.ID), extraScanTestSnapshot(t, ctx, pool, collection.ID)
		calls := len(prober.calls())
		cached := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
		if cached.Error != "" || cached.Added != 0 || cached.Updated != 0 || len(prober.calls()) != calls || themeScanTestSnapshot(t, ctx, pool, collection.ID) != themesBefore || extraScanTestSnapshot(t, ctx, pool, collection.ID) != extrasBefore {
			t.Fatalf("step%d cached scan rewrote or reprobed auxiliary sources: %+v", step, cached)
		}
		beforeForced := map[string]ItemMetadataDetail{}
		for _, id := range ids {
			beforeForced[id] = metadataEditTestDetail(t, ctx, store, actor, id)
		}
		job := themeScanTestForce(t, ctx, store, collection.ID)
		job = libraryIntegrationWaitJob(t, ctx, store, job.ID, "Completed")
		if job.Error != "" {
			t.Fatalf("forced scan: %+v", job)
		}
		for _, id := range ids {
			if after := metadataEditTestDetail(t, ctx, store, actor, id); !reflect.DeepEqual(after, beforeForced[id]) {
				t.Fatalf("step%d forced scan changed unchanged automatic facts, metadata revision or controls", step)
			}
		}
		if forceProbeUserDataSnapshot(t, ctx, pool, controlled.ItemID) != userData {
			t.Fatal("sorting or rescan changed independent playback history")
		}
	}
}
