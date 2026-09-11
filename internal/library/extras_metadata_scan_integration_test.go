//go:build linux

package library

import (
	"encoding/json"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestExtraScanTrailerNativeMetadataControlsSurviveForcedRescan(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, root, firstUserID := libraryIntegrationStore(t, mediaSourceTestProber{inner: prober})
	const secondUserID = "extra-rescan-second-viewer"
	libraryIntegrationUser(t, ctx, pool, secondUserID, false, true, nil)
	actor := metadataEditTestActor(t, ctx, pool, "extra-rescan-metadata-editor")
	const filename = "Delta Local Trailer"
	const trailerContents = "video:independent-original-trailer"
	const manualName = "Manually Named Trailer"
	const manualSort = "Pinned Trailer Sort"
	moviePath := libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:main-movie")
	trailerPath := libraryIntegrationFile(t, root, "movies/Film/trailers/"+filename+".mp4", trailerContents)
	lib := libraryIntegrationCreate(t, ctx, store, "Editable trailer scan", "movies", filepath.Join(root, "movies"))
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("initial trailer scan did not complete cleanly: %+v", job)
	}
	ordinary := libraryIntegrationQuery(t, ctx, store, Query{UserID: firstUserID, ParentID: lib.ID, Recursive: true})
	movie := libraryIntegrationItemByPath(t, ordinary.Items, moviePath)
	trailers, err := store.QueryLocalTrailers(ctx, movie.ID, Subject{UserID: firstUserID})
	if err != nil || len(trailers) != 1 {
		t.Fatalf("initial scan did not publish exactly one real trailer: %+v, %v", trailers, err)
	}
	trailerID := trailers[0].ID
	if trailerID == movie.ID || trailers[0].Path != trailerPath || trailers[0].Name != filename {
		t.Fatalf("initial trailer does not have its own filename and identity: %+v", trailers[0])
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data
		(user_id,item_id,playback_position_ticks,play_count,is_favorite,played,last_played_at,updated_at)
		VALUES($1,$3,9007199254740993,3,true,false,'2025-01-01T00:00:00Z','2025-01-02T00:00:00Z'),
		($2,$3,42000000,7,false,true,'2025-02-01T00:00:00Z','2025-02-02T00:00:00Z')`, firstUserID, secondUserID, trailerID); err != nil {
		t.Fatalf("seed independent trailer user histories: %v", err)
	}
	userSnapshot := func() string {
		t.Helper()
		var snapshot string
		if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(data) ORDER BY user_id)::text
			FROM user_item_data data WHERE item_id=$1`, trailerID).Scan(&snapshot); err != nil {
			t.Fatalf("read exact trailer user histories: %v", err)
		}
		return snapshot
	}
	beforeUserData := userSnapshot()
	initial := metadataEditTestDetail(t, ctx, store, actor, trailerID)
	edited, err := store.UpdateItemMetadata(ctx, actor, trailerID, MetadataEdit{
		Revision: initial.Revision,
		Overrides: map[string]json.RawMessage{
			"Name": metadataEditTestRaw(t, manualName), "SortName": metadataEditTestRaw(t, manualSort),
		},
		LockedFields: []string{"Name", "SortName"},
	})
	if err != nil {
		t.Fatalf("apply real native trailer metadata controls: %v", err)
	}
	// Keep one explicit override and one independently effective lock through
	// the same rescan. Removing SortName's override must retain its saved lock.
	pinned, err := store.UpdateItemMetadata(ctx, actor, trailerID, MetadataEdit{
		Revision:     edited.Revision,
		Overrides:    map[string]json.RawMessage{"Name": metadataEditTestRaw(t, manualName)},
		LockedFields: []string{"Name", "SortName"},
	})
	if err != nil || pinned.Effective.Name != manualName || pinned.Effective.SortName != manualSort ||
		len(pinned.Overrides) != 1 || string(pinned.LockedValues["SortName"]) != string(metadataEditTestRaw(t, manualSort)) {
		t.Fatalf("native edit did not establish independent override and lock state: %+v, %v", pinned, err)
	}
	assertReads := func() {
		t.Helper()
		for _, user := range []struct {
			id               string
			position         int64
			count            int
			favorite, played bool
		}{
			{firstUserID, 9007199254740993, 3, true, false},
			{secondUserID, 42000000, 7, false, true},
		} {
			listed, err := store.QueryLocalTrailers(ctx, movie.ID, Subject{UserID: user.id})
			if err != nil || len(listed) != 1 {
				t.Fatalf("trailer membership changed for %s: %+v, %v", user.id, listed, err)
			}
			direct, err := store.GetItemFor(ctx, Subject{UserID: user.id}, trailerID)
			if err != nil {
				t.Fatalf("read controlled trailer directly for %s: %v", user.id, err)
			}
			selected, err := store.QueryItems(ctx, Query{UserID: user.id, Ids: []string{trailerID}, IncludeItemTypes: []string{"Trailer"}})
			if err != nil || selected.TotalRecordCount != 1 || len(selected.Items) != 1 {
				t.Fatalf("explicit Trailer ID lost controlled resource for %s: %+v, %v", user.id, selected, err)
			}
			for _, item := range []Item{listed[0], direct, selected.Items[0]} {
				if item.ID != trailerID || item.ParentID != movie.ID || item.Type != "Video" || item.ExtraKind != ExtraKindTrailer ||
					item.Name != manualName || item.SortName != manualSort || !item.ExtraNameControlled || !item.ExtraSortNameControlled ||
					item.Path != trailerPath || item.ExtraOwnerName != movie.Name {
					t.Fatalf("trailer read lost stable identity, effective controls, or independent source: %+v", item)
				}
				data := item.UserData
				if data == nil || data.ItemID != trailerID || data.PlaybackPositionTicks != user.position || data.PlayCount != user.count ||
					data.IsFavorite != user.favorite || data.Played != user.played {
					t.Fatalf("trailer projection mixed or changed %s's user history: %+v", user.id, data)
				}
			}
			file, source, err := store.OpenMedia(ctx, user.id, trailerID, media.SourceID(trailerID))
			if err != nil {
				t.Fatalf("open controlled trailer's original source for %s: %v", user.id, err)
			}
			body, readErr := io.ReadAll(file)
			_ = file.Close()
			if readErr != nil || string(body) != trailerContents || source.SourceID != media.SourceID(trailerID) ||
				source.Item.ID != trailerID || source.Item.Path != trailerPath ||
				strings.TrimSuffix(filepath.Base(source.Item.Path), filepath.Ext(source.Item.Path)) != filename {
				t.Fatalf("native display controls changed the original source identity, filename, or bytes: %v", readErr)
			}
		}
		if after := userSnapshot(); after != beforeUserData {
			t.Fatal("native edits, source reads, or rescan changed either independent user history")
		}
	}
	assertReads()
	beforeCalls := len(prober.calls())
	job, err := store.StartScanWithOptions(ctx, lib.ID, ScanOptions{ForceProbe: true})
	if err != nil {
		t.Fatalf("start forced trailer metadata rescan: %v", err)
	}
	job = libraryIntegrationWaitJob(t, ctx, store, job.ID, "Completed")
	if job.Error != "" || !job.ForceProbe || len(prober.calls()) != beforeCalls+2 {
		t.Fatalf("rescan did not reprobe both real main and trailer sources: %+v", job)
	}
	reprobed := make(map[string]int)
	for _, contents := range prober.calls()[beforeCalls:] {
		reprobed[contents]++
	}
	if reprobed[trailerContents] != 1 || reprobed["video:main-movie"] != 1 {
		t.Fatalf("forced rescan did not independently probe the main and trailer bytes: %+v", reprobed)
	}
	rescanned := metadataEditTestDetail(t, ctx, store, actor, trailerID)
	if rescanned.ItemID != trailerID || rescanned.ParentID != movie.ID || rescanned.Path != trailerPath ||
		rescanned.Automatic.Name != filename || rescanned.Automatic.SortName != filename ||
		!reflect.DeepEqual(rescanned.Overrides, pinned.Overrides) || !reflect.DeepEqual(rescanned.LockedValues, pinned.LockedValues) ||
		!reflect.DeepEqual(rescanned.LockedFields, pinned.LockedFields) || !reflect.DeepEqual(rescanned.Effective, pinned.Effective) ||
		rescanned.LastEditedBy != pinned.LastEditedBy || !reflect.DeepEqual(rescanned.LastEditedAt, pinned.LastEditedAt) {
		t.Fatalf("forced trailer rescan replaced native controls, effective values, or editor history: %+v", rescanned)
	}
	resources := extraScanTestResources(t, ctx, pool, lib.ID)
	if len(resources) != 1 || resources[0].id != trailerID || resources[0].ownerID != movie.ID ||
		resources[0].kind != ExtraKindTrailer || !resources[0].active || resources[0].path != trailerPath {
		t.Fatalf("forced metadata rescan changed the persistent trailer association: %+v", resources)
	}
	assertReads()
}
