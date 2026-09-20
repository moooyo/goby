package settings

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/moooyo/goby/internal/library"
)

func TestSortRemoveWordsSQLMatchesGoUnicodeAndRejectsNoncanonicalArrays(t *testing.T) {
	ctx, pool, _, _, _ := settingsRepository(t)
	var source strings.Builder
	for _, entry := range unicode.CaseRanges {
		for r := rune(entry.Lo); r <= rune(entry.Hi); r++ {
			if unicode.ToLower(r) != r {
				source.WriteRune(r)
			}
		}
	}
	var folded string
	if err := pool.QueryRow(ctx, `SELECT goby_sort_fold($1)`, source.String()).Scan(&folded); err != nil || folded != strings.ToLower(source.String()) {
		t.Fatalf("SQL simple lowercase differs from the Go Unicode table: %v", err)
	}
	for _, test := range []struct {
		name  string
		words []string
		want  string
	}{
		{"Ä Σ THE", []string{}, "ä σ the"}, {"Ä Amber", []string{"ä"}, "amber"}, {"Σ Amber", []string{"σ"}, "amber"},
		{"THE\u00a0Amber", []string{"the"}, "amber"}, {"Theatre", []string{"the"}, "theatre"}, {"The", []string{"the"}, "the"},
		{"The The Amber", []string{"the"}, "the amber"}, {"  The Amber  ", []string{}, "  the amber  "}, {"[x] Amber", []string{"[x]"}, "amber"},
	} {
		var got string
		if err := pool.QueryRow(ctx, `SELECT goby_generated_sort_name($1,$2)`, test.name, test.words).Scan(&got); err != nil || got != test.want {
			t.Fatalf("sort %q=%q want%q: %v", test.name, got, test.want, err)
		}
	}
	for _, words := range [][]string{{}, {"The", "Ä"}, {"Ä", "ä"}, {"Σ", "σ"}, {"a\u0080"}, {"a\u2000"}, {strings.Repeat("a", 129)}} {
		var accepted bool
		if err := pool.QueryRow(ctx, `SELECT goby_valid_sort_remove_words($1)`, words).Scan(&accepted); err != nil || accepted != (ValidateStoredSorting(Sorting{SortRemoveWords: words}) == nil) {
			t.Fatalf("SQL/Go rule validation differs: %#v %v", words, err)
		}
	}
	for _, expression := range []string{"ARRAY[['a','b'],['c','d']]::text[]", "'[0:1]={a,b}'::text[]", "ARRAY['a',NULL]::text[]", "NULL::text[]"} {
		var accepted bool
		if err := pool.QueryRow(ctx, `SELECT goby_valid_sort_remove_words(`+expression+`)`).Scan(&accepted); err != nil || accepted {
			t.Fatalf("noncanonical stored array accepted or raised instead of false: %s %v", expression, err)
		}
	}
}

func TestSortingCASRebuildPreservesControlsAndRollsBackAtomically(t *testing.T) {
	ctx, pool, owner, store, actor := settingsRepository(t)
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('sorting-lib','Sorting','movies');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES('sorting-lib','sorting-lib','Sorting','sorting','CollectionFolder',true),
		('sorting-generated','sorting-lib','The Amber','the amber','Movie',false),('sorting-explicit','sorting-lib','The Film','archival','Movie',false),
		('sorting-manual','sorting-lib','The Manual','the manual','Movie',false),('sorting-locked','sorting-lib','The Locked','the locked','Movie',false),
		('sorting-name','sorting-lib','The Original','the original','Movie',false);
		UPDATE item_metadata_state SET overrides='{"SortName":"manual key"}' WHERE item_id='sorting-manual';
		UPDATE item_metadata_state SET locked_values='{"SortName":"locked key"}' WHERE item_id='sorting-locked';
		UPDATE item_metadata_state SET overrides='{"Name":"The Renamed"}' WHERE item_id='sorting-name'`); err != nil {
		t.Fatal(err)
	}
	var events []library.CatalogNotification
	owner.SetCatalogChangeListener(func(event library.CatalogNotification) { events = append(events, event) })
	t.Cleanup(func() { owner.SetCatalogChangeListener(nil) })
	initial := store.Snapshot()
	words := Sorting{SortRemoveWords: []string{"The"}}
	request := UpdateRequest{Revision: initial.Revision, Overrides: initial.Overrides, Sorting: &words}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION sorting_reject_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'sorting rollback'; END $$;
		CREATE TRIGGER sorting_reject_event BEFORE UPDATE ON task_system_events FOR EACH ROW EXECUTE FUNCTION sorting_reject_event()`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(ctx, actor, request); err == nil {
		t.Fatal("injected downstream event failure committed")
	}
	var key string
	if err := pool.QueryRow(ctx, `SELECT sort_name FROM items WHERE id='sorting-generated'`).Scan(&key); err != nil || key != "the amber" || !reflect.DeepEqual(store.Snapshot(), initial) || len(events) != 0 {
		t.Fatalf("rollback leaked settings, keys, publication or catalog notification: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER sorting_reject_event ON task_system_events`); err != nil {
		t.Fatal(err)
	}
	changed, err := store.Update(ctx, actor, request)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"sorting-generated": "amber", "sorting-explicit": "archival", "sorting-manual": "manual key", "sorting-locked": "locked key", "sorting-name": "original"} {
		if err := pool.QueryRow(ctx, `SELECT sort_name FROM items WHERE id=$1`, id).Scan(&key); err != nil || key != want {
			t.Fatalf("%s sort=%q want%q: %v", id, key, want, err)
		}
	}
	var automatic, projection string
	if err := pool.QueryRow(ctx, `SELECT automatic->>'SortName',COALESCE(effective::text,'null') FROM item_metadata_state WHERE item_id='sorting-generated'`).Scan(&automatic, &projection); err != nil || automatic != "amber" || projection != "null" {
		t.Fatalf("derived key polluted raw source projection: %s %s %v", automatic, projection, err)
	}
	if len(events) != 1 || !events[0].Resync || len(events[0].Changes) != 0 {
		t.Fatal("whole-catalog rebuild did not emit one bounded resync")
	}
	words.SortRemoveWords[0] = "changed-input"
	changed.Sorting.SortRemoveWords[0] = "changed-result"
	if store.Snapshot().Sorting.SortRemoveWords[0] != "The" {
		t.Fatal("published sorting leaked mutable data")
	}
	if _, err := store.Update(ctx, actor, request); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale sorting CAS=%v", err)
	}
	reloaded, err := New(ctx, pool, owner, settingsTestDefaults(), "settings-host-alpha")
	if err != nil || !reflect.DeepEqual(reloaded.Snapshot(), store.Snapshot()) {
		t.Fatalf("restart lost sorting: %v", err)
	}
	reset, err := store.Reset(ctx, actor, ResetRequest{Revision: changed.Revision, Fields: []Field{FieldSorting}})
	if err != nil || len(reset.Sorting.SortRemoveWords) != 0 {
		t.Fatalf("sorting reset: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT sort_name FROM items WHERE id='sorting-generated'`).Scan(&key); err != nil || key != "the amber" {
		t.Fatalf("clear failed to restore full source name: %q %v", key, err)
	}
}

func TestSortingConfigurationReplacementPartialOmissionAndFullReset(t *testing.T) {
	ctx, pool, _, store, native := settingsRepository(t)
	actor := configurationTestActor(t, ctx, pool, native, false)
	for _, words := range [][]string{{"The", "a"}, {"The"}, {}} {
		copy := words
		if _, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial, SortRemoveWords: &copy}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(store.Snapshot().Sorting.SortRemoveWords, words) {
			t.Fatal("array was appended instead of replaced")
		}
	}
	words := []string{"The"}
	first, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial, SortRemoveWords: &words})
	if err != nil {
		t.Fatal(err)
	}
	noop, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial})
	if err != nil || !reflect.DeepEqual(first, noop) {
		t.Fatal("partial omission reset sorting")
	}
	reset, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationFull})
	if err != nil || len(reset.Sorting.SortRemoveWords) != 0 {
		t.Fatalf("full omission did not reset sorting: %v", err)
	}
	var invalid []string
	if _, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial, SortRemoveWords: &invalid}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil array normalized into a clear: %v", err)
	}
}

func TestSortingStatementTimeoutRollsBackWithoutLosingCatalogOwner(t *testing.T) {
	ctx, pool, owner, store, actor := settingsRepository(t)
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('sorting-timeout','Timeout','movies');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES('sorting-timeout','sorting-timeout','Timeout','timeout','CollectionFolder',true),('sorting-timeout-film','sorting-timeout','The Film','the film','Movie',false);
		CREATE FUNCTION sorting_slow_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(1); RETURN NEW; END $$;
		CREATE TRIGGER sorting_slow_write BEFORE UPDATE ON items FOR EACH ROW EXECUTE FUNCTION sorting_slow_write()`); err != nil {
		t.Fatal(err)
	}
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error { _, err := tx.Exec(`SET statement_timeout='250ms'`); return err }); err != nil {
		t.Fatal(err)
	}
	before := store.Snapshot()
	sorting := Sorting{SortRemoveWords: []string{"The"}}
	request := UpdateRequest{Revision: before.Revision, Overrides: before.Overrides, Sorting: &sorting}
	if _, err := store.Update(ctx, actor, request); !errors.Is(err, library.ErrUnavailable) {
		t.Fatalf("slow rebuild did not report bounded availability: %v", err)
	}
	if !reflect.DeepEqual(store.Snapshot(), before) {
		t.Fatal("timeout published uncommitted sorting")
	}
	// The same owner session must remain usable after PostgreSQL cancels its
	// statement. LOCAL timeout restoration must also retain the stricter value.
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		var value string
		if err := tx.QueryRow(`SELECT current_setting('statement_timeout')`).Scan(&value); err != nil {
			return err
		}
		if value != "250ms" {
			t.Fatalf("stricter deployment timeout changed: %s", value)
		}
		_, err := tx.Exec(`SET statement_timeout='0'`)
		return err
	}); err != nil {
		t.Fatalf("statement timeout lost the reserved catalog owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER sorting_slow_write ON items`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(ctx, actor, request); err != nil {
		t.Fatalf("same sorting CAS could not retry after a slow statement: %v", err)
	}
}
