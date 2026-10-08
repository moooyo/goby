package library

import (
	"errors"
	"testing"
)

func TestItemPermissionsMatchDirectReadsIncludingExpectedEpisodes(t *testing.T) {
	f := newEpisodeRosterFixture(t)
	f.replace(t, f.edit)
	ids := []string{"movie-a", "library-b", "series-b", "season-b", "episode-b1", "movie-b", "missing"}
	rows, err := f.pool.Query(f.ctx, "SELECT id FROM expected_episodes ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	ids = append(ids, "movie-b")
	for _, user := range []string{"admin", "default", "restricted", "none"} {
		t.Run(user, func(t *testing.T) {
			permissions, err := f.store.ItemPermissionsFor(f.ctx, Subject{UserID: user}, ids)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range ids {
				item, err := f.store.GetItem(f.ctx, user, id)
				permission, visible := permissions[id]
				if errors.Is(err, ErrNotFound) {
					if visible {
						t.Fatalf("permission projection exposed inaccessible item %s", id)
					}
					continue
				}
				if err != nil || !visible || permission.CanPlay != item.CanPlay {
					t.Fatalf("permission projection differs for %s: visible=%v canPlay=%v direct=%v error=%v", id, visible, permission.CanPlay, item.CanPlay, err)
				}
			}
		})
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy=policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	permissions, err := f.store.ItemPermissionsFor(f.ctx, Subject{UserID: "restricted"}, []string{"movie-b", "series-b"})
	if err != nil || len(permissions) != 2 || permissions["movie-b"].CanPlay || permissions["series-b"].CanPlay {
		t.Fatalf("fresh playback denial changed visibility or was ignored: %+v %v", permissions, err)
	}
}
