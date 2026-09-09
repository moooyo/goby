package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestStoreMetadataInactiveNumberSurvivesReclassificationAndCanBeCleared(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("stable identity across a rename uses Linux inode information")
	}
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "metadata-type-editor")
	original := libraryIntegrationFile(t, allowedRoot, "mixed/Show.S01E02.mp4", "video:metadata-reclassification")
	plain := filepath.Join(filepath.Dir(original), "Film.mp4")
	library := libraryIntegrationCreate(t, ctx, store, "Reclassified metadata", "mixed", filepath.Dir(original))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, original)
	detail := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail,
		map[string]json.RawMessage{"IndexNumber": json.RawMessage(`9`)}, []string{"IndexNumber"})
	moveAndScan := func(from, to string) ItemMetadataDetail {
		t.Helper()
		if err := os.Rename(from, to); err != nil {
			t.Fatal(err)
		}
		libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		result := metadataEditTestDetail(t, ctx, store, actor, item.ID)
		if result.Path != to {
			t.Fatalf("metadata kept an obsolete physical path: %q, want %q", result.Path, to)
		}
		return result
	}
	detail = moveAndScan(original, plain)
	if detail.Type != "Movie" || detail.Effective.IndexNumber != nil ||
		!reflect.DeepEqual(detail.InactiveFields, []string{"IndexNumber"}) ||
		string(detail.Overrides["IndexNumber"]) != "9" || string(detail.LockedValues["IndexNumber"]) != "9" {
		t.Fatalf("reclassification discarded or applied inactive metadata: %+v", detail)
	}
	mediaItem, err := store.GetItem(ctx, userID, item.ID)
	if err != nil || mediaItem.Type != "Movie" || mediaItem.IndexNumber != 0 || mediaItem.Metadata != nil {
		t.Fatalf("inactive metadata leaked into the item projection: %+v, %v", mediaItem, err)
	}
	unchanged, err := store.UpdateItemMetadata(ctx, actor, item.ID, MetadataEdit{
		Revision: detail.Revision, Overrides: metadataEditTestCopy(detail.Overrides),
		LockedFields: append([]string{}, detail.LockedFields...),
	})
	if err != nil || unchanged.Revision != detail.Revision {
		t.Fatalf("round-tripping inactive controls failed or changed the revision: %+v, %v", unchanged, err)
	}
	values := metadataEditTestCopy(detail.Overrides)
	values["Name"] = json.RawMessage(`"Manual name across types"`)
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail, values, detail.LockedFields)
	values["IndexNumber"] = json.RawMessage(`10`)
	if _, err := store.UpdateItemMetadata(ctx, actor, item.ID, MetadataEdit{
		Revision: detail.Revision, Overrides: values, LockedFields: detail.LockedFields,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an inactive override could be changed: %v", err)
	}
	detail = moveAndScan(plain, original)
	if detail.Type != "Episode" || detail.Effective.IndexNumber == nil || *detail.Effective.IndexNumber != 9 ||
		len(detail.InactiveFields) != 0 || detail.Effective.Name != "Manual name across types" {
		t.Fatalf("saved controls did not reactivate for the original type: %+v", detail)
	}
	detail = moveAndScan(original, plain)
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail,
		map[string]json.RawMessage{"Name": json.RawMessage(`"Manual name across types"`)}, []string{})
	if len(detail.InactiveFields) != 0 || len(detail.LockedValues) != 0 {
		t.Fatalf("inactive controls could not be explicitly cleared: %+v", detail)
	}
	detail = moveAndScan(plain, original)
	if detail.Effective.IndexNumber == nil || *detail.Effective.IndexNumber != 2 {
		t.Fatalf("cleared inactive controls returned after a later type change: %+v", detail)
	}
}
