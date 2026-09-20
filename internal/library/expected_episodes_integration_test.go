package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type episodeRosterFixture struct {
	ctx   context.Context
	pool  *pgxpool.Pool
	store *Store
	actor identity.Principal
	edit  EpisodeRosterEdit
}

func newEpisodeRosterFixture(t *testing.T) episodeRosterFixture {
	t.Helper()
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	seedLibraryQueryFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE items SET index_number=1 WHERE id='season-b';
		UPDATE items SET root_id='root-b',relative_path=id,path='/media/b/'||id||'.mkv',parent_index_number=1,
			media='{"DurationTicks":15000000,"Container":"mkv"}'::jsonb WHERE id IN ('episode-b1','episode-b2');
		UPDATE items SET index_number=3 WHERE id='episode-b2'`); err != nil {
		t.Fatal(err)
	}
	edit := episodeRosterTestEdit()
	edit.Entries[1].Key, edit.Entries[1].EpisodeNumber = "episode-four", 4
	edit.Entries = append(edit.Entries, EpisodeRosterEntryInput{Key: "episode-five", SeasonNumber: 1, EpisodeNumber: 5, Name: "Future episode", PremiereDate: "9999-12-31"})
	return episodeRosterFixture{ctx: ctx, pool: pool, store: store, actor: metadataEditTestActor(t, ctx, pool, "episode-roster-admin"), edit: edit}
}

func (f episodeRosterFixture) replace(t *testing.T, edit EpisodeRosterEdit) EpisodeRosterDetail {
	t.Helper()
	result, err := f.store.ReplaceEpisodeRoster(f.ctx, f.actor, "series-b", edit)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEpisodeRosterLifecycleRetainsSourceHistoryAndPhysicalIdentity(t *testing.T) {
	f := newEpisodeRosterFixture(t)
	initial, err := f.store.GetEpisodeRoster(f.ctx, f.actor, "series-b")
	if err != nil || initial.Revision != "0" || initial.State != "absent" || initial.Source != nil || len(initial.Entries) != 0 {
		t.Fatalf("absent state: %+v %v", initial, err)
	}
	if _, err := f.store.SetFavorite(f.ctx, "restricted", "episode-b1", true); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, f.store)
	accepted := f.replace(t, f.edit)
	if accepted.Revision != "1" || accepted.State != "active" || len(accepted.Entries) != 3 || accepted.Source.Kind != "admin_import" || len(accepted.Source.SHA256) != 64 {
		t.Fatalf("accepted state: %+v", accepted)
	}
	notification := nextCatalogTestNotification(t, notifications)
	if len(notification.Changes) != 1 || notification.Changes[0].ItemID != "series-b" || !notification.Changes[0].ChildrenAdded || !notification.Changes[0].ChildrenRemoved {
		t.Fatal("roster import did not invalidate declared series membership")
	}
	for index, airing := range []string{"aired", "unknown", "unaired"} {
		entry := accepted.Entries[index]
		if entry.Airing != airing || entry.Availability != "missing" || entry.AvailableItemCount != 0 || len(entry.AvailableItemIDs) != 0 {
			t.Fatalf("entry source classification: %+v", entry)
		}
	}
	if _, err := f.store.ReplaceEpisodeRoster(f.ctx, f.actor, "series-b", f.edit); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale import: %v", err)
	}
	noOp := f.edit
	noOp.Revision = accepted.Revision
	noOp.Entries = append([]EpisodeRosterEntryInput{}, f.edit.Entries...)
	noOp.Entries[0], noOp.Entries[2] = noOp.Entries[2], noOp.Entries[0]
	if result := f.replace(t, noOp); result.Revision != accepted.Revision {
		t.Fatal("source entry ordering advanced revision")
	}
	assertNoCatalogTestNotification(t, notifications)
	conflict := f.edit
	conflict.Revision = accepted.Revision
	conflict.Entries = append([]EpisodeRosterEntryInput{}, f.edit.Entries...)
	conflict.Entries[0].Name = "Changed source content"
	if _, err := f.store.ReplaceEpisodeRoster(f.ctx, f.actor, "series-b", conflict); !errors.Is(err, ErrEpisodeRosterSourceRevision) {
		t.Fatalf("source version changed content: %v", err)
	}
	invalid := conflict
	invalid.Entries = append(invalid.Entries, invalid.Entries[0])
	if _, err := f.store.ReplaceEpisodeRoster(f.ctx, f.actor, "series-b", invalid); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid import changed source: %v", err)
	}
	conflict.Source.Revision = "v2"
	conflict.Entries = conflict.Entries[:1]
	replaced := f.replace(t, conflict)
	if replaced.Revision != "2" || replaced.RetiredCount != 2 || len(replaced.Entries) != 1 || replaced.Entries[0].ID != accepted.Entries[0].ID {
		t.Fatalf("replacement lost stable facts: %+v", replaced)
	}
	withdrawn, err := f.store.WithdrawEpisodeRoster(f.ctx, f.actor, "series-b", replaced.Revision)
	if err != nil || withdrawn.State != "withdrawn" || withdrawn.Revision != "3" || withdrawn.RetiredCount != 3 || len(withdrawn.Entries) != 0 || withdrawn.Source.SHA256 != replaced.Source.SHA256 {
		t.Fatalf("withdrawal: %+v %v", withdrawn, err)
	}
	conflict.Revision = withdrawn.Revision
	reactivated := f.replace(t, conflict)
	if reactivated.Revision != "4" || reactivated.Entries[0].ID != accepted.Entries[0].ID {
		t.Fatal("same source reactivation replaced stable identity")
	}
	conflict.Revision = reactivated.Revision
	conflict.Source.Key = "another-declared-source"
	other := f.replace(t, conflict)
	if other.Entries[0].ID == reactivated.Entries[0].ID || other.RetiredCount != 3 {
		t.Fatal("changing source authority failed to retire its old identity")
	}
	var imports, physical int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM episode_roster_imports WHERE series_id='series-b'`).Scan(&imports); err != nil || imports != 5 {
		t.Fatalf("immutable history: %d %v", imports, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM items WHERE id LIKE 'missing-%'`).Scan(&physical); err != nil || physical != 0 {
		t.Fatal("roster inserted physical catalog items")
	}
	item, err := f.store.GetItem(f.ctx, "restricted", "episode-b1")
	if err != nil || item.UserData == nil || !item.UserData.IsFavorite {
		t.Fatal("roster mutation altered physical user state")
	}
}

func TestEpisodeRosterMissingQueriesSharePredicatesCountsPagesAndAuthority(t *testing.T) {
	f := newEpisodeRosterFixture(t)
	base := Query{UserID: "restricted", ParentID: "series-b", Recursive: true, IncludeItemTypes: []string{"Episode"}, SortBy: "IndexNumber", Limit: 100, DisplayMissingEpisodes: true}
	before, err := f.store.QueryItems(f.ctx, base)
	if err != nil || before.TotalRecordCount != 2 {
		t.Fatalf("a numbered gap supplied evidence: %+v %v", before, err)
	}
	detail := f.replace(t, f.edit)
	missing := map[int]string{}
	for _, entry := range detail.Entries {
		missing[entry.EpisodeNumber] = entry.ID
	}
	yes, no := true, false
	for _, test := range []struct {
		name    string
		change  func(*Query)
		numbers []int
	}{
		{"preference", func(*Query) {}, []int{1, 2, 3, 4, 5}},
		{"default_physical", func(q *Query) { q.DisplayMissingEpisodes = false }, []int{1, 3}},
		{"missing", func(q *Query) { q.DisplayMissingEpisodes = false; q.IsMissing = &yes }, []int{2, 4, 5}},
		{"not_missing_overrides_preference", func(q *Query) { q.IsMissing = &no }, []int{1, 3}},
		{"placeholder", func(q *Query) { q.IsPlaceHolder = &yes }, []int{2, 4, 5}},
		{"not_placeholder", func(q *Query) { q.IsPlaceHolder = &no }, []int{1, 3}},
		{"virtual_future", func(q *Query) { q.DisplayMissingEpisodes = false; q.IsVirtualUnaired = &yes }, []int{5}},
		{"not_virtual_future", func(q *Query) { q.IsVirtualUnaired = &no }, []int{1, 2, 3, 4}},
		{"future", func(q *Query) { q.IsUnaired = &yes }, []int{5}},
		{"not_future_includes_unknown", func(q *Query) { q.IsUnaired = &no }, []int{1, 2, 3, 4}},
		{"missing_nonfuture", func(q *Query) { q.IsMissing = &yes; q.IsVirtualUnaired = &no }, []int{2, 4}},
		{"contradictory", func(q *Query) { q.IsMissing = &no; q.IsPlaceHolder = &yes }, []int{}},
		{"season_parent", func(q *Query) { q.ParentID = "season-b"; q.Recursive = false }, []int{1, 2, 3, 4, 5}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := base
			test.change(&query)
			result, err := f.store.QueryItems(f.ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			got := []int{}
			for _, item := range result.Items {
				got = append(got, item.IndexNumber)
				if IsExpectedEpisodeID(item.ID) {
					if item.ExpectedEpisode == nil || item.CanPlay || item.Media != nil || item.Path != "" || item.UserData != nil {
						t.Fatalf("virtual item acquired physical capabilities: %+v", item)
					}
				}
			}
			if !reflect.DeepEqual(got, test.numbers) || result.TotalRecordCount != len(test.numbers) {
				t.Fatalf("numbers/count: %v/%d want %v", got, result.TotalRecordCount, test.numbers)
			}
		})
	}
	page := base
	page.StartIndex = 1
	page.Limit = 2
	result, err := f.store.QueryItems(f.ctx, page)
	if err != nil || result.TotalRecordCount != 5 || len(result.Items) != 2 || result.Items[0].ID != missing[2] || result.Items[1].ID != "episode-b2" {
		t.Fatalf("mixed paging: %+v %v", result, err)
	}
	item, err := f.store.GetItem(f.ctx, "restricted", missing[4])
	if err != nil || item.ExpectedEpisode == nil || item.ExpectedEpisode.PremiereDateKnown || item.ExpectedEpisode.IsUnaired || item.Series == nil || item.Series.ID != "series-b" {
		t.Fatalf("unknown date projection: %+v %v", item, err)
	}
	if _, err := f.store.SetFavorite(f.ctx, "restricted", missing[2], true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("virtual favorite write: %v", err)
	}
	if _, err := f.store.SetPlayed(f.ctx, "restricted", missing[2], true, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("virtual played write: %v", err)
	}
	if items, err := f.store.GetItemsByIDFor(f.ctx, Subject{UserID: "restricted"}, []string{missing[2]}); err != nil || len(items) != 0 {
		t.Fatal("playback identity projection admitted a missing fact")
	}
	for _, consumer := range []func() (ItemResult, error){
		func() (ItemResult, error) {
			return f.store.NextUp(f.ctx, NextUpQuery{UserID: "restricted", SeriesID: "series-b", Limit: 100})
		},
		func() (ItemResult, error) {
			return f.store.EpisodePlaybackQueue(f.ctx, Subject{UserID: "restricted"}, "series-b")
		},
		func() (ItemResult, error) { q := base; q.Resumable = true; return f.store.QueryItems(f.ctx, q) },
	} {
		result, err := consumer()
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range result.Items {
			if IsExpectedEpisodeID(item.ID) {
				t.Fatal("missing fact entered a playable consumer")
			}
		}
	}
	if _, err := f.store.GetItem(f.ctx, "none", missing[2]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("library ACL leaked a missing episode: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["library-b"],"ExcludedSubFolders":["season-b"]}' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetItem(f.ctx, "restricted", missing[2]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("season ACL leaked a missing episode: %v", err)
	}
	denied, err := f.store.QueryItems(f.ctx, base)
	if err != nil || denied.TotalRecordCount != 0 {
		t.Fatalf("season ACL leaked counts: %+v %v", denied, err)
	}
}

func TestEpisodeRosterCurrentAdministratorAndConcurrentCAS(t *testing.T) {
	f := newEpisodeRosterFixture(t)
	var group sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := f.store.ReplaceEpisodeRoster(f.ctx, f.actor, "series-b", f.edit)
			results <- err
		}()
	}
	group.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("initial CAS winners: %d conflicts: %d", success, conflicts)
	}
	actor := f.actor
	actor.Kind = "emby"
	if _, err := f.store.GetEpisodeRoster(f.ctx, actor, "series-b"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("consumer session admitted as native administrator: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.actor.SessionID); err != nil {
		t.Fatal(err)
	}
	edit := f.edit
	edit.Revision = "1"
	edit.Source.Revision = "v2"
	if _, err := f.store.ReplaceEpisodeRoster(f.ctx, f.actor, "series-b", edit); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked administrator changed roster: %v", err)
	}
	if _, err := f.store.WithdrawEpisodeRoster(f.ctx, f.actor, "series-b", "1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked administrator withdrew roster: %v", err)
	}
	var revision int64
	if err := f.pool.QueryRow(f.ctx, `SELECT revision FROM series_episode_rosters WHERE series_id='series-b'`).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("rejected mutation advanced revision: %d %v", revision, err)
	}
}

func TestEpisodeRosterScanArrivalAndRemovalPreserveSourceAndPhysicalState(t *testing.T) {
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, &libraryFixtureProber{})
	firstPath := libraryIntegrationFile(t, root, "tv/Declared Show/Season 01/Declared Show S01E01.mkv", "first episode")
	libraryIntegrationFile(t, root, "tv/Declared Show/Season 01/Declared Show S01E03.mkv", "third episode")
	collection := libraryIntegrationCreate(t, ctx, store, "Declared TV", "tvshows", filepath.Join(root, "tv"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var seriesID, firstID string
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE library_id=$1 AND type='Series'`, collection.ID).Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1`, firstPath).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "scan-roster-admin")
	edit := episodeRosterTestEdit()
	edit.Entries = edit.Entries[:1]
	accepted, err := store.ReplaceEpisodeRoster(ctx, actor, seriesID, edit)
	if err != nil {
		t.Fatal(err)
	}
	missingID := accepted.Entries[0].ID
	if _, err := store.SetFavorite(ctx, viewer, firstID, true); err != nil {
		t.Fatal(err)
	}
	arrivedPath := libraryIntegrationFile(t, root, "tv/Declared Show/Season 01/Declared Show S01E02.mkv", "arriving episode")
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var arrivedID string
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1`, arrivedPath).Scan(&arrivedID); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetEpisodeRoster(ctx, actor, seriesID)
	if err != nil || after.Revision != accepted.Revision || after.Entries[0].Availability != "available" || !reflect.DeepEqual(after.Entries[0].AvailableItemIDs, []string{arrivedID}) {
		t.Fatalf("arrival facts: %+v %v", after, err)
	}
	if _, err := store.GetItem(ctx, viewer, missingID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("arrival retained missing projection: %v", err)
	}
	first, err := store.GetItem(ctx, viewer, firstID)
	if err != nil || first.UserData == nil || !first.UserData.IsFavorite {
		t.Fatal("arrival changed existing physical state")
	}
	if err := os.Remove(arrivedPath); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	after, err = store.GetEpisodeRoster(ctx, actor, seriesID)
	if err != nil || after.Revision != accepted.Revision || after.Entries[0].Availability != "missing" || after.Entries[0].ID != missingID {
		t.Fatalf("removal lost explicit fact: %+v %v", after, err)
	}
	if _, err := store.WithdrawEpisodeRoster(ctx, actor, seriesID, after.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItem(ctx, viewer, missingID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdrawn fact remained visible: %v", err)
	}
}

func TestEpisodeRosterReadInsideOwnedWriteSurvivesCallerCancellation(t *testing.T) {
	f := newEpisodeRosterFixture(t)
	f.replace(t, f.edit)
	request, cancel := context.WithCancel(f.ctx)
	tx, err := f.store.beginOwnedTx(request)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	cancel()
	detail, _, err := readEpisodeRoster(request, tx, "series-b", false)
	if err != nil || len(detail.Entries) != 3 {
		t.Fatalf("protected roster cursor failed after request cancellation: %v", err)
	}
	if err := tx.Commit(request); err != nil {
		t.Fatal(err)
	}
	if f.store.ownership.lost.Load() {
		t.Fatal("cancelled roster read lost catalog ownership")
	}
	edit := f.edit
	edit.Revision = "1"
	edit.Source.Revision = "v2"
	if after := f.replace(t, edit); after.Revision != "2" {
		t.Fatal("catalog did not remain writable")
	}
}

func TestEpisodeRosterAvailableReferencesAreBoundedAndCountsRemainExact(t *testing.T) {
	f := newEpisodeRosterFixture(t)
	f.replace(t, f.edit)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path,index_number,parent_index_number,media)
		SELECT 'roster-copy-'||n,'library-b','root-b','season-b','Copy '||n,'Copy '||n,'Episode',
			'/media/b/copy-'||n,'copy-'||n,2,1,'{"DurationTicks":15000000}'::jsonb FROM generate_series(1,12) AS n`); err != nil {
		t.Fatal(err)
	}
	detail, err := f.store.GetEpisodeRoster(f.ctx, f.actor, "series-b")
	if err != nil {
		t.Fatal(err)
	}
	entry := detail.Entries[0]
	if entry.Availability != "available" || entry.AvailableItemCount != 12 || len(entry.AvailableItemIDs) != 8 {
		t.Fatalf("unbounded or inaccurate availability: %+v", entry)
	}
	if _, err := f.store.GetItem(f.ctx, "restricted", entry.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("physical copies did not suppress missing fact: %v", err)
	}
	// SQL JSON null is not a retained media object and cannot establish physical
	// availability, even if a malformed catalog row has a path and root.
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET media='null'::jsonb WHERE id LIKE 'roster-copy-%'`); err != nil {
		t.Fatal(err)
	}
	detail, err = f.store.GetEpisodeRoster(f.ctx, f.actor, "series-b")
	if err != nil || detail.Entries[0].Availability != "missing" || detail.Entries[0].AvailableItemCount != 0 {
		t.Fatal("JSON null media established availability")
	}
}

func TestEpisodeRosterAmbiguousSeasonDoesNotBypassParentAuthority(t *testing.T) {
	f := newEpisodeRosterFixture(t)
	detail := f.replace(t, f.edit)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder,index_number)
		VALUES ('other-season-b','library-b','series-b','Other Season','Other Season','Season',true,1)`); err != nil {
		t.Fatal(err)
	}
	for _, entry := range detail.Entries {
		if _, err := f.store.GetItem(f.ctx, "restricted", entry.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ambiguous parent permitted a missing fact: %v", err)
		}
	}
	current, err := f.store.GetEpisodeRoster(f.ctx, f.actor, "series-b")
	if err != nil || current.Revision != detail.Revision || len(current.Entries) != len(detail.Entries) {
		t.Fatal("projection ambiguity changed imported evidence")
	}
}

func TestEpisodeRosterScannerReclassificationKeepsDormantEvidence(t *testing.T) {
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, &libraryFixtureProber{})
	libraryIntegrationFile(t, root, "reclassified/Declared Show/Season 01/Declared Show S01E01.mkv", "original episode")
	collection := libraryIntegrationCreate(t, ctx, store, "Reclassified roster", "tvshows", filepath.Join(root, "reclassified"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var seriesID string
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE library_id=$1 AND type='Series'`, collection.ID).Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "dormant-roster-admin")
	edit := episodeRosterTestEdit()
	edit.Entries = edit.Entries[:1]
	detail, err := store.ReplaceEpisodeRoster(ctx, actor, seriesID, edit)
	if err != nil {
		t.Fatal(err)
	}
	missingID := detail.Entries[0].ID
	for _, kind := range []string{"movies", "tvshows"} {
		// Change the fixture's configured collection role, then let the real
		// scanner update the same physical folder ID through its normal upsert.
		if _, err := pool.Exec(ctx, `UPDATE libraries SET collection_type=$2 WHERE id=$1`, collection.ID, kind); err != nil {
			t.Fatal(err)
		}
		libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
		var itemType, revision, digest string
		var imports, facts int
		if err := pool.QueryRow(ctx, `SELECT i.type,r.revision::text,r.payload_sha256,
			(SELECT count(*) FROM episode_roster_imports h WHERE h.series_id=r.series_id),
			(SELECT count(*) FROM expected_episodes e WHERE e.series_id=r.series_id AND e.id=$2)
			FROM items i JOIN series_episode_rosters r ON r.series_id=i.id WHERE i.id=$1`, seriesID, missingID).
			Scan(&itemType, &revision, &digest, &imports, &facts); err != nil {
			t.Fatal(err)
		}
		if revision != detail.Revision || digest != detail.Source.SHA256 || imports != 1 || facts != 1 {
			t.Fatal("scanner altered dormant roster evidence")
		}
		if kind == "movies" {
			if itemType != "Folder" {
				t.Fatalf("scanner did not reclassify original owner: %s", itemType)
			}
			if _, err := store.GetEpisodeRoster(ctx, actor, seriesID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("dormant source admitted administration: %v", err)
			}
			edit.Revision = detail.Revision
			if _, err := store.ReplaceEpisodeRoster(ctx, actor, seriesID, edit); !errors.Is(err, ErrNotFound) {
				t.Fatalf("dormant source admitted import: %v", err)
			}
			if _, err := store.WithdrawEpisodeRoster(ctx, actor, seriesID, detail.Revision); !errors.Is(err, ErrNotFound) {
				t.Fatalf("dormant source admitted withdrawal: %v", err)
			}
			if _, err := store.GetItem(ctx, viewer, missingID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("dormant fact remained visible: %v", err)
			}
			result, err := store.QueryItems(ctx, Query{UserID: viewer, Ids: []string{missingID}, Recursive: true, DisplayMissingEpisodes: true, Limit: 100})
			if err != nil || result.TotalRecordCount != 0 {
				t.Fatalf("dormant fact remained in counts: %+v %v", result, err)
			}
		} else {
			if itemType != "Series" {
				t.Fatalf("scanner did not restore original owner: %s", itemType)
			}
			restored, err := store.GetEpisodeRoster(ctx, actor, seriesID)
			if err != nil || restored.Source.SHA256 != detail.Source.SHA256 || restored.Entries[0].ID != missingID {
				t.Fatalf("reactivated evidence changed: %+v %v", restored, err)
			}
			item, err := store.GetItem(ctx, viewer, missingID)
			if err != nil || item.ExpectedEpisode == nil {
				t.Fatalf("reactivated source missing projection: %v", err)
			}
		}
	}
}
