//go:build linux

package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestAdditionalPartsRequireExplicitContiguousSameRootSources(t *testing.T) {
	ctx, pool, store, root, user := libraryIntegrationStore(t, mediaSourceTestProber{inner: &libraryFixtureProber{}})
	paths := []string{}
	for _, name := range []string{"Movie - part1.mkv", "Movie - part2.mkv", "Movie - part3.mkv", "Movie Remake - part2.mkv", "Other cd1.mkv", "Other cd3.mkv", "Duplicate.part1.mkv", "Duplicate.part1.mp4", "Duplicate.part2.mkv"} {
		paths = append(paths, libraryIntegrationFile(t, root, "parts/"+name, "video:"+name))
	}
	collection := libraryIntegrationCreate(t, ctx, store, "Parts", "movies", filepath.Join(root, "parts"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: user, ParentID: collection.ID, Recursive: true, Limit: 100}).Items
	first := libraryIntegrationItemByPath(t, items, paths[0])
	second := libraryIntegrationItemByPath(t, items, paths[1])
	third := libraryIntegrationItemByPath(t, items, paths[2])
	result, err := store.AdditionalParts(ctx, Subject{UserID: user}, first.ID)
	if err != nil || result.TotalRecordCount != 2 || len(result.Items) != 2 || result.Items[0].ID != second.ID || result.Items[1].ID != third.ID {
		t.Fatalf("part sequence = %+v, %v", result, err)
	}
	result, err = store.AdditionalParts(ctx, Subject{UserID: user}, second.ID)
	if err != nil || len(result.Items) != 1 || result.Items[0].ID != third.ID {
		t.Fatalf("later part replayed its predecessor: %+v, %v", result, err)
	}
	missingMiddle := libraryIntegrationItemByPath(t, items, paths[4])
	result, err = store.AdditionalParts(ctx, Subject{UserID: user}, missingMiddle.ID)
	if err != nil || len(result.Items) != 0 {
		t.Fatalf("a gap manufactured a continuous program: %+v, %v", result, err)
	}
	duplicate := libraryIntegrationItemByPath(t, items, paths[6])
	if _, err := store.AdditionalParts(ctx, Subject{UserID: user}, duplicate.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate part ordinal accepted: %v", err)
	}
	// Provider conflicts between later parts matter even when the first part
	// itself has no provider ID from which a conflict could be inferred.
	for _, entry := range []struct{ id, metadata string }{{second.ID, `{"ProviderIds":{"Tmdb":"1"}}`}, {third.ID, `{"ProviderIds":{"tmdb":"2"}}`}} {
		if _, err := pool.Exec(ctx, "UPDATE items SET local_metadata=$2::jsonb WHERE id=$1", entry.id, entry.metadata); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.AdditionalParts(ctx, Subject{UserID: user}, first.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("conflicting providers were grouped: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE items SET local_metadata='{}' WHERE id=ANY($1::text[])", []string{second.ID, third.ID}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths[1]); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(paths[2], paths[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdditionalParts(ctx, Subject{UserID: user}, first.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("replaced symlink source accepted: %v", err)
	}
}

func TestAdditionalPartNamesDoNotGuessUnmarkedOrAmbiguousFiles(t *testing.T) {
	for _, name := range []string{"Movie 2.mkv", "Movie.part0.mkv", "Movie.part17.mkv", "../Movie.part1.mkv", "Movie - CD01.mkv", "part1.mkv", "Movie.mkv", `Movie\Name - part1.mkv`} {
		if _, ok := parseAdditionalPart(name); ok {
			t.Fatalf("unsupported filename grouped: %q", name)
		}
	}
	first, ok := parseAdditionalPart("Folder/Movie - Part2.mkv")
	if !ok || first.stem != "Movie" || first.index != 2 || first.kind != "part" || first.directory != "Folder" {
		t.Fatalf("explicit part name = %+v, %v", first, ok)
	}
}

func TestAdditionalPartsLiteralPatternCharactersRetainBoundedCandidateScope(t *testing.T) {
	ctx, _, store, root, user := libraryIntegrationStore(t, mediaSourceTestProber{inner: &libraryFixtureProber{}})
	firstPath := libraryIntegrationFile(t, root, "patterns/Literal%_ - part1.mkv", "video:first")
	secondPath := libraryIntegrationFile(t, root, "patterns/Literal%_ - part2.mkv", "video:second")
	// More than the candidate limit would match an unescaped wildcard prefix.
	// Removing LIKE entirely must not pass this regression either.
	for index := 0; index < 65; index++ {
		name := fmt.Sprintf("patterns/LiteralAB%02d - part1.mkv", index)
		libraryIntegrationFile(t, root, name, "video:unrelated")
	}
	collection := libraryIntegrationCreate(t, ctx, store, "Literal parts", "movies", filepath.Join(root, "patterns"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: user, ParentID: collection.ID, Recursive: true, Limit: 100}).Items
	first := libraryIntegrationItemByPath(t, items, firstPath)
	second := libraryIntegrationItemByPath(t, items, secondPath)
	result, err := store.AdditionalParts(ctx, Subject{UserID: user}, first.ID)
	if err != nil || result.TotalRecordCount != 1 || len(result.Items) != 1 || result.Items[0].ID != second.ID {
		t.Fatalf("literal pattern characters escaped their bounded part scope: %+v, %v", result, err)
	}
}
