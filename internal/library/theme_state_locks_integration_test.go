package library

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestThemeVisibilityStateRechecksClassificationAfterItemLockWait(t *testing.T) {
	for _, test := range []struct {
		name       string
		lockedItem string
		itemPath   string
		markerPath string
		directory  bool
	}{
		{"direct-favorite", "movie-b", "Owner/theme.mp3", "Owner/theme.mp3", false},
		{"folder-played", "episode-b2", "Show/backdrops/episode.mp4", "Show/backdrops", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, store := libraryQueryTestStore(t)
			seedLibraryQueryFixture(t, ctx, store.pool)
			if _, err := store.pool.Exec(ctx, `UPDATE items SET root_id = 'root-b',
				path = '/media/b/' || $2::text, relative_path = $2 WHERE id = $1`, test.lockedItem, test.itemPath); err != nil {
				t.Fatalf("seed the future reserved item path: %v", err)
			}
			if _, err := store.pool.Exec(ctx, `INSERT INTO user_item_data
				(user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_at, updated_at) VALUES
				('restricted', 'movie-b', 17, 2, false, false, '2026-01-02T03:04:05Z', '2026-01-02T03:04:05Z'),
				('restricted', 'episode-b1', 19, 3, true, false, '2026-02-03T04:05:06Z', '2026-02-03T04:05:06Z'),
				('restricted', 'episode-b2', 23, 4, false, false, '2026-03-04T05:06:07Z', '2026-03-04T05:06:07Z'),
				('default', 'movie-b', 29, 5, true, true, '2026-04-05T06:07:08Z', '2026-04-05T06:07:08Z')`); err != nil {
				t.Fatalf("seed complete user-data preservation witnesses: %v", err)
			}
			snapshot := func() string {
				t.Helper()
				var result string
				if err := store.pool.QueryRow(ctx, `SELECT COALESCE(
					jsonb_agg(to_jsonb(data) ORDER BY user_id, item_id), '[]'::jsonb)::text
					FROM user_item_data data`).Scan(&result); err != nil {
					t.Fatalf("snapshot every user-data row: %v", err)
				}
				return result
			}
			before := snapshot()
			if _, err := store.GetItemFor(ctx, Subject{UserID: "restricted"}, test.lockedItem); err != nil {
				t.Fatalf("the classification race did not start with a visible ordinary item: %v", err)
			}

			operationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			classifier, err := store.pool.Begin(operationCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(classifier)
			var locked string
			// Deliberately acquire a row lock without updating the item tuple.
			// The pending marker lives in another relation, so a locking SELECT
			// that started before this commit cannot rely on tuple rechecking.
			if err := classifier.QueryRow(operationCtx, "SELECT id FROM items WHERE id = $1 FOR UPDATE", test.lockedItem).Scan(&locked); err != nil || locked != test.lockedItem {
				t.Fatalf("lock the exact classification target: %v", err)
			}
			if _, err := classifier.Exec(operationCtx, `INSERT INTO theme_reserved_paths
				(root_id, relative_path, is_directory) VALUES ('root-b', $1, $2)`, test.markerPath, test.directory); err != nil {
				t.Fatalf("stage the permanent classification marker: %v", err)
			}

			finished := make(chan error, 1)
			returned := make(chan struct{})
			go func() {
				defer close(returned)
				var err error
				if test.name == "direct-favorite" {
					_, err = store.SetFavorite(operationCtx, "restricted", "movie-b", true)
				} else {
					_, err = store.SetPlayed(operationCtx, "restricted", "series-b", true, nil)
				}
				finished <- err
			}()
			defer func() {
				cancel()
				rollback(classifier)
				select {
				case <-returned:
				case <-time.After(5 * time.Second):
					t.Error("the bounded state writer did not release its transaction during cleanup")
				}
			}()
			waitCatalogApplicationBlock(t, operationCtx, store.pool, classifier.Conn().PgConn().PID())
			select {
			case err := <-finished:
				t.Fatalf("the state write returned before its item lock was released: %v", err)
			default:
			}
			if err := classifier.Commit(operationCtx); err != nil {
				t.Fatalf("publish the marker after the state statement began waiting: %v", err)
			}
			select {
			case err := <-finished:
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("state write after classification changed = %v, want ErrNotFound", err)
				}
			case <-operationCtx.Done():
				t.Fatal("the state writer did not finish after classification committed")
			}
			if after := snapshot(); after != before {
				t.Fatal("a state statement used its old pre-lock classification to change or create user data")
			}
			var parentRows int
			if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM user_item_data
				WHERE item_id IN ('series-b', 'season-b')`).Scan(&parentRows); err != nil || parentRows != 0 {
				t.Fatalf("a rejected folder write created ancestor state: count=%d, error=%v", parentRows, err)
			}
			if _, err := store.GetItemFor(ctx, Subject{UserID: "restricted"}, test.lockedItem); !errors.Is(err, ErrNotFound) {
				t.Fatalf("the committed reservation was not effective after the rejected state write: %v", err)
			}
		})
	}
}
