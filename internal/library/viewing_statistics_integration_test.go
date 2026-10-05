package library

import (
	"errors"
	"testing"
)

func TestViewingStatisticsAggregatesContentOnceAndBoundsProgress(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET media='{"DurationTicks":36000000000}';
		UPDATE items SET media='{"DurationTicks":72000000000}' WHERE id='movie-b';
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder,media)
		SELECT 'unknown-'||n,'library-b','library-b','Unknown '||n,'Unknown '||n,'Movie',false,value
		FROM (VALUES (1,NULL::jsonb),(2,'{}'::jsonb),(3,'{"DurationTicks":null}'::jsonb),
			(4,'{"DurationTicks":0}'::jsonb),(5,'{"DurationTicks":-1}'::jsonb),
			(6,'{"DurationTicks":"36000000000"}'::jsonb),(7,'{"DurationTicks":1.5}'::jsonb),
			(8,'{"DurationTicks":9223372036854775808}'::jsonb)) AS invalid(n,value);
		INSERT INTO user_item_data(user_id,item_id,played,play_count,playback_position_ticks) VALUES
		('restricted','movie-b',true,99,80000000000),
		('restricted','episode-b1',false,3,18000000000),
		('restricted','episode-b2',false,7,9223372036854775807),
		('restricted','movie-a',true,1,0),
		('restricted','audio-b',true,1,0),
		('restricted','series-b',true,1,0),
		('default','episode-b1',true,4,0);
		INSERT INTO user_item_data(user_id,item_id,played,play_count,playback_position_ticks)
		SELECT 'restricted',id,true,1,10000000000 FROM items WHERE id LIKE 'unknown-%'`); err != nil {
		t.Fatal(err)
	}
	got, err := store.ViewingStatisticsFor(ctx, Subject{UserID: "restricted"})
	if err != nil || got != (ViewingStatistics{EstimatedContentHours: 4, EstimatedContentTicks: "126000000000", IsEstimate: true}) {
		t.Fatalf("content estimate = %+v, %v; want 3.5 hours rounded to 4", got, err)
	}
	got, err = store.ViewingStatisticsFor(ctx, Subject{UserID: "default"})
	if err != nil || got.EstimatedContentTicks != "36000000000" || got.EstimatedContentHours != 1 {
		t.Fatalf("another user's estimate inherited the first user's state: %+v, %v", got, err)
	}
	got, err = store.ViewingStatisticsFor(ctx, Subject{UserID: "none"})
	if err != nil || got.EstimatedContentTicks != "0" || got.EstimatedContentHours != 0 || !got.IsEstimate {
		t.Fatalf("empty statistics must be a real zero: %+v, %v", got, err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE user_item_data SET played=false,playback_position_ticks=0 WHERE user_id='restricted' AND item_id='movie-b'`); err != nil {
		t.Fatal(err)
	}
	got, err = store.ViewingStatisticsFor(ctx, Subject{UserID: "restricted"})
	if err != nil || got.EstimatedContentTicks != "54000000000" || got.EstimatedContentHours != 2 {
		t.Fatalf("unmarking a watched item retained its historical play count: %+v, %v", got, err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE items SET media='{"DurationTicks":9223372036854775807}' WHERE id IN ('movie-b','episode-b1','episode-b2');
		UPDATE user_item_data SET played=true WHERE user_id='restricted' AND item_id IN ('movie-b','episode-b1','episode-b2')`); err != nil {
		t.Fatal(err)
	}
	got, err = store.ViewingStatisticsFor(ctx, Subject{UserID: "restricted"})
	if err != nil || got.EstimatedContentTicks != "27670116110564327421" || got.EstimatedContentHours != 768614336 {
		t.Fatalf("numeric accumulation overflowed bigint or lost precision: %+v, %v", got, err)
	}
}

func TestViewingStatisticsRechecksLibraryAndInheritedItemPolicy(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET media='{"DurationTicks":36000000000}';
		INSERT INTO user_item_data(user_id,item_id,played)
		SELECT 'restricted',id,true FROM items WHERE type IN ('Movie','Episode')`); err != nil {
		t.Fatal(err)
	}
	key := seedCatalogApplicationKey(t, ctx, store.pool, "viewing-statistics-key", true)
	key.UserID = "restricted"
	for _, subject := range []Subject{{UserID: "restricted"}, key} {
		got, err := store.ViewingStatisticsFor(ctx, subject)
		if err != nil || got.EstimatedContentHours != 3 {
			t.Fatalf("initial visible estimate = %+v, %v", got, err)
		}
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["library-b"],"ExcludedSubFolders":["season-b"]}' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	for _, subject := range []Subject{{UserID: "restricted"}, key} {
		got, err := store.ViewingStatisticsFor(ctx, subject)
		if err != nil || got.EstimatedContentHours != 1 {
			t.Fatalf("excluded ancestor leaked watched episode durations: %+v, %v", got, err)
		}
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false}' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	for _, subject := range []Subject{{UserID: "restricted"}, key} {
		got, err := store.ViewingStatisticsFor(ctx, subject)
		if err != nil || got.EstimatedContentHours != 0 || got.EstimatedContentTicks != "0" {
			t.Fatalf("revoked library access leaked statistics: %+v, %v", got, err)
		}
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM sessions WHERE id=$1`, key.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ViewingStatisticsFor(ctx, key); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked application key retained statistics: %v", err)
	}
	if _, err := store.ViewingStatisticsFor(ctx, Subject{UserID: "disabled"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled user retained statistics: %v", err)
	}
}

func TestViewingStatisticsExcludesReservedBackgroundAndThemeResources(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedThemeVisibilityFixture(t, ctx, store)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET type='Movie',media='{"DurationTicks":36000000000}'
		WHERE NOT is_folder AND id=ANY($1::text[])`, themeVisibilityHiddenIDs()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO user_item_data(user_id,item_id,played)
		SELECT 'restricted',id,true FROM items WHERE id=ANY($1::text[])`, themeVisibilityHiddenIDs()); err != nil {
		t.Fatal(err)
	}
	got, err := store.ViewingStatisticsFor(ctx, Subject{UserID: "restricted"})
	if err != nil || got.EstimatedContentTicks != "0" || got.EstimatedContentHours != 0 {
		t.Fatalf("auxiliary resources were counted as watched content: %+v, %v", got, err)
	}
}
