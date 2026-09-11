package library

import (
	"errors"
	"testing"
)

func TestListMembershipGuardOnlyReturnsProvenEmptyCandidates(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	query := Query{UserID: "restricted", Recursive: true, IncludeItemTypes: []string{"Playlist", "BoxSet"}, ListItemIds: []string{"album-id"}}
	assertEmpty := func(query Query) {
		t.Helper()
		result, err := store.QueryItems(ctx, query)
		if err != nil || result.TotalRecordCount != 0 || len(result.Items) != 0 {
			t.Fatalf("an empty authorized candidate intersection was not retained: %+v, %v", result, err)
		}
	}
	assertEmpty(query)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		VALUES('hidden-list','library-a','library-a','Hidden List','hidden list','Playlist',true)`); err != nil {
		t.Fatal(err)
	}
	assertEmpty(query)
	for _, kind := range []string{"Playlist", "BoxSet"} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
			VALUES($1,'library-b','library-b',$1,$1,$2,true)`, "visible-"+kind, kind); err != nil {
			t.Fatal(err)
		}
		for _, page := range []struct{ offset, limit int }{{0, 1}, {0, 0}, {1000, 1}} {
			candidate := query
			candidate.IncludeItemTypes = []string{kind}
			candidate.StartIndex, candidate.Limit = page.offset, page.limit
			result, err := store.QueryItems(ctx, candidate)
			if !errors.Is(err, ErrUnsupportedFilter) || len(result.Items) != 0 || result.TotalRecordCount != 0 {
				t.Fatalf("unknown list membership was ignored or bypassed by pagination: %+v, %v", result, err)
			}
		}
	}
	query.ExcludeItemIds = []string{"visible-Playlist", "visible-BoxSet"}
	assertEmpty(query)
	query.ExcludeItemIds = nil
	query.SearchTerm = "No candidate has this name"
	assertEmpty(query)
	query.SearchTerm = ""
	query.UserID = "none"
	assertEmpty(query)
}

func TestListMembershipGuardCannotBeDiscardedByAlternateQueries(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	query := Query{UserID: "restricted", ListItemIds: []string{"album-id"}}
	if _, err := store.QueryLatest(ctx, query, true); !errors.Is(err, ErrUnsupportedFilter) {
		t.Fatal("Latest ignored an unevaluated membership predicate")
	}
	if _, err := store.QueryResume(ctx, query); !errors.Is(err, ErrUnsupportedFilter) {
		t.Fatal("Resume ignored an unevaluated membership predicate")
	}
	if _, err := store.ListEntities(ctx, "Genre", query); !errors.Is(err, ErrUnsupportedFilter) {
		t.Fatal("entity listing ignored an unevaluated membership predicate")
	}
}
