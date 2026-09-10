package library

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	applicationFixtureServerID      = "application-key-fixture-server"
	applicationFixtureServerName    = "Application key fixture server"
	applicationFixtureServerVersion = "1.0"
)

func seedApplicationFixtureDevice(t *testing.T, ctx context.Context, pool *pgxpool.Pool, appName string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO application_key_devices
		(id, reported_device_id, reported_name, app_name, app_version)
		VALUES (1, $1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET app_name = EXCLUDED.app_name`,
		applicationFixtureServerID, applicationFixtureServerName, appName, applicationFixtureServerVersion); err != nil {
		t.Fatalf("insert application server device fixture: %v", err)
	}
}

func seedCatalogApplicationKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string, sidecar bool) Subject {
	t.Helper()
	digest := sha256.Sum256([]byte("catalog-application-key-fixture:" + id))
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id, token_hash, kind, client_name, device_id, device_name, client_version)
		VALUES ($1, $2, 'application_key', $1, $3, $4, $5)`, id, digest[:],
		applicationFixtureServerID, applicationFixtureServerName, applicationFixtureServerVersion); err != nil {
		t.Fatalf("insert application credential fixture: %v", err)
	}
	if sidecar {
		seedApplicationFixtureDevice(t, ctx, pool, id)
		if _, err := pool.Exec(ctx, `INSERT INTO application_keys (credential_id, secret_ciphertext)
			VALUES ($1, $2)`, id, []byte("catalog-fixture-ciphertext")); err != nil {
			t.Fatalf("insert application key fixture: %v", err)
		}
	}
	return Subject{ApplicationCredentialID: id}
}

func TestApplicationKeyCatalogHasIndependentScopeAndOptionalUserData(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryEntityQueryFixture(t, ctx, store.pool)
	subject := seedCatalogApplicationKey(t, ctx, store.pool, "catalog-key", true)
	if _, err := store.pool.Exec(ctx, `UPDATE users SET is_administrator = false,
		policy = '{"EnableAllFolders":true,"EnableMediaPlayback":false}' WHERE id = 'disabled'`); err != nil {
		t.Fatal(err)
	}
	userDataSeed(t, ctx, store.pool, "disabled", UserData{ItemID: "movie-a", IsFavorite: true, Played: true, PlayCount: 3})

	result, err := store.QueryItems(ctx, Query{ApplicationCredentialID: subject.ApplicationCredentialID, Recursive: true, Limit: 2})
	if err != nil || result.TotalRecordCount != 8 || len(result.Items) != 2 {
		t.Fatalf("application catalog page = %+v, %v; want full-catalog count and bounded page", result, err)
	}
	for _, item := range result.Items {
		if item.UserData != nil || !item.CanPlay {
			t.Errorf("userless key item projection = %+v; want no user data and independent playback authority", item)
		}
	}
	libraries, err := store.ListUserLibrariesFor(ctx, subject)
	if err != nil || len(libraries) != 3 {
		t.Fatalf("application libraries = %+v, %v; want all libraries", libraries, err)
	}
	item, err := store.GetItemFor(ctx, subject, "movie-a")
	if err != nil || item.UserData != nil || !item.CanPlay {
		t.Fatalf("userless application item = %+v, %v", item, err)
	}
	latest, err := store.QueryLatest(ctx, Query{ApplicationCredentialID: subject.ApplicationCredentialID}, false)
	if err != nil || len(latest) != 6 {
		t.Fatalf("application latest = %+v, %v; want all catalog leaves", latest, err)
	}
	for _, entry := range latest {
		if entry.Item.UserData != nil {
			t.Errorf("userless latest item invented user data: %+v", entry)
		}
	}
	entity, err := store.GetEntityFor(ctx, subject, "Genre", "Private Genre")
	if err != nil || entity.Count != 1 {
		t.Fatalf("application private entity = %+v, %v", entity, err)
	}
	if byID, err := store.GetEntityByIDFor(ctx, subject, entity.ID); err != nil || byID.ID != entity.ID {
		t.Fatalf("application entity lookup = %+v, %v", byID, err)
	}

	subject.UserID = "disabled"
	item, err = store.GetItemFor(ctx, subject, "movie-a")
	if err != nil || item.UserData == nil || !item.UserData.IsFavorite || item.UserData.PlayCount != 3 || !item.CanPlay {
		t.Fatalf("disabled target projection = %+v, %v; want visible state despite account and playback restrictions", item, err)
	}
	if _, err := store.GetItem(ctx, "restricted", "movie-a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ordinary user escaped library ACL: %v", err)
	}
	if _, err := store.GetItem(ctx, "disabled", "movie-a"); !errors.Is(err, ErrForbidden) {
		t.Errorf("ordinary disabled user was authorized: %v", err)
	}
	ordinary, err := store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true})
	if err != nil || ordinary.TotalRecordCount != 6 {
		t.Fatalf("ordinary restricted page = %+v, %v; want the existing six-item scope", ordinary, err)
	}
	subject.UserID = "missing-target"
	if _, err := store.GetItemFor(ctx, subject, "movie-a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing explicit application target was accepted: %v", err)
	}
}

func TestApplicationKeyUserStateWritesRequireAndNotifyExplicitTarget(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	subject := seedCatalogApplicationKey(t, ctx, store.pool, "state-key", true)
	if _, err := store.SetFavoriteFor(ctx, subject, "movie-a", true); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("userless favorite write = %v; want an explicit-target error", err)
	}
	if _, err := store.SetPlayedFor(ctx, subject, "movie-a", true, nil); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("userless played write = %v; want an explicit-target error", err)
	}
	if _, err := store.GetUserDataBatchFor(ctx, subject, []string{"movie-a"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("userless user-data batch = %v; want an explicit-target error", err)
	}
	subject.UserID = "disabled"
	if _, err := store.pool.Exec(ctx, `UPDATE users SET is_administrator = false,
		policy = '{"EnableAllFolders":true,"EnableMediaPlayback":false}'
		WHERE id = $1`, subject.UserID); err != nil {
		t.Fatal(err)
	}
	favorite, err := store.SetFavoriteFor(ctx, subject, "movie-a", true)
	if err != nil || !favorite.IsFavorite {
		t.Fatalf("application favorite for disabled target = %+v, %v", favorite, err)
	}
	played, err := store.SetPlayedFor(ctx, subject, "movie-a", true, nil)
	if err != nil || !played.Played || !played.IsFavorite || played.PlayCount != 1 {
		t.Fatalf("application played for disabled target = %+v, %v", played, err)
	}
	data, err := store.GetUserDataFor(ctx, subject, "movie-a")
	if err != nil || !data.IsFavorite || !data.Played {
		t.Fatalf("explicit application user data = %+v, %v", data, err)
	}
	page, err := store.UserDataNotificationPage(ctx, UserDataNotificationQuery{
		UserID: subject.UserID, ApplicationCredentialID: subject.ApplicationCredentialID, ItemID: "movie-a",
	})
	if err != nil || len(page.Items) != 1 || !page.Items[0].IsFavorite || !page.Items[0].Played {
		t.Fatalf("application target notification = %+v, %v", page, err)
	}
	var rows, otherRows int
	if err := store.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE user_id <> $1)
		FROM user_item_data`, subject.UserID).Scan(&rows, &otherRows); err != nil || rows != 1 || otherRows != 0 {
		t.Fatalf("user-state ownership = %d total, %d other, %v; want one explicit target row", rows, otherRows, err)
	}
	if _, err := store.SetFavorite(ctx, "restricted", "movie-a", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("ordinary favorite escaped library ACL: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy = policy || '{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb
		WHERE id = $1`, subject.UserID); err != nil {
		t.Fatal(err)
	}
	if data, err := store.SetFavoriteFor(ctx, subject, "movie-a", false); err != nil || data.IsFavorite {
		t.Errorf("application favorite mutation lost independent authority: %+v, %v", data, err)
	}
	if data, err := store.SetPlayedFor(ctx, subject, "movie-a", false, nil); err != nil || data.Played {
		t.Errorf("application played mutation lost independent authority: %+v, %v", data, err)
	}
	if _, err := store.GetUserDataFor(ctx, subject, "movie-a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("application user-data read ignored the target's updated library ACL: %v", err)
	}
}

func TestApplicationKeyExplicitCatalogTargetUsesLibraryACL(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryEntityQueryFixture(t, ctx, store.pool)
	key := seedCatalogApplicationKey(t, ctx, store.pool, "catalog-target-key", true)
	libraryIntegrationUser(t, ctx, store.pool, "enabled-empty-target", false, false, nil)
	libraryIntegrationUser(t, ctx, store.pool, "disabled-empty-target", true, false, nil)
	privateEntityID := libraryQueryEntityID(t, ctx, store.pool, "Genre", "Private Genre")
	for _, test := range []struct {
		userID                  string
		itemCount, libraryCount int
		latestCount, nextCount  int
	}{
		{userID: "restricted", itemCount: 6, libraryCount: 1, latestCount: 4, nextCount: 1},
		{userID: "enabled-empty-target"},
		{userID: "disabled-empty-target"},
	} {
		t.Run(test.userID, func(t *testing.T) {
			subject := Subject{UserID: test.userID, ApplicationCredentialID: key.ApplicationCredentialID}
			userDataSeed(t, ctx, store.pool, subject.UserID, UserData{ItemID: "movie-a", IsFavorite: true})
			userDataSeed(t, ctx, store.pool, subject.UserID, UserData{ItemID: "episode-b1", Played: true, PlayCount: 1})
			query := Query{UserID: subject.UserID, ApplicationCredentialID: key.ApplicationCredentialID, Recursive: true}
			result, err := store.QueryItems(ctx, query)
			if err != nil || result.TotalRecordCount != test.itemCount || len(result.Items) != test.itemCount {
				t.Fatalf("target-scoped application items = %+v, %v; want count %d", result, err, test.itemCount)
			}
			for _, item := range result.Items {
				if item.LibraryID != "library-b" || item.UserData == nil || !item.CanPlay {
					t.Errorf("target-scoped application projection leaked a library or lost state: %+v", item)
				}
			}
			libraries, err := store.ListUserLibrariesFor(ctx, subject)
			if err != nil || len(libraries) != test.libraryCount {
				t.Fatalf("target-scoped application views = %+v, %v; want count %d", libraries, err, test.libraryCount)
			}
			if _, err := store.GetItemFor(ctx, subject, "movie-a"); !errors.Is(err, ErrNotFound) {
				t.Errorf("target-scoped application item leaked a hidden item: %v", err)
			}
			batch, err := store.GetUserDataBatchFor(ctx, subject, []string{"movie-a", "episode-b1"})
			if err != nil || len(batch) != test.libraryCount {
				t.Fatalf("target-scoped application user-data batch = %+v, %v", batch, err)
			}
			if _, exists := batch["movie-a"]; exists {
				t.Error("target-scoped application user-data batch leaked hidden state")
			}
			favorite := true
			favoriteQuery := query
			favoriteQuery.IsFavorite = &favorite
			favorites, err := store.QueryItems(ctx, favoriteQuery)
			if err != nil || favorites.TotalRecordCount != 0 || len(favorites.Items) != 0 {
				t.Errorf("target-scoped favorites leaked hidden state: %+v, %v", favorites, err)
			}
			latest, err := store.QueryLatest(ctx, query, false)
			if err != nil || len(latest) != test.latestCount {
				t.Errorf("target-scoped latest = %+v, %v; want count %d", latest, err, test.latestCount)
			}
			next, err := store.NextUp(ctx, NextUpQuery{UserID: subject.UserID, ApplicationCredentialID: key.ApplicationCredentialID})
			if err != nil || next.TotalRecordCount != test.nextCount || len(next.Items) != test.nextCount {
				t.Errorf("target-scoped next-up = %+v, %v; want count %d", next, err, test.nextCount)
			}
			query.SearchTerm = "Private Genre"
			entities, err := store.ListEntities(ctx, "Genre", query)
			if err != nil || entities.TotalRecordCount != 0 || len(entities.Items) != 0 {
				t.Errorf("target-scoped entities leaked a hidden item count: %+v, %v", entities, err)
			}
			if _, err := store.GetEntityByIDFor(ctx, subject, privateEntityID); !errors.Is(err, ErrNotFound) {
				t.Errorf("target-scoped entity detail leaked a hidden entity: %v", err)
			}
		})
	}
	global, err := store.QueryItems(ctx, Query{ApplicationCredentialID: key.ApplicationCredentialID, Recursive: true})
	if err != nil || global.TotalRecordCount != 8 || len(global.Items) != 8 {
		t.Fatalf("userless application scope inherited a target's ACL: %+v, %v", global, err)
	}
	for _, item := range global.Items {
		if item.UserData != nil {
			t.Errorf("userless application scope inherited target user data: %+v", item)
		}
	}
}

func TestApplicationKeyCatalogRevalidatesCredentialOnEveryOperation(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryEntityQueryFixture(t, ctx, store.pool)
	revoked := seedCatalogApplicationKey(t, ctx, store.pool, "revoked-catalog-key", true)
	if _, err := store.pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", revoked.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	orphan := seedCatalogApplicationKey(t, ctx, store.pool, "orphan-catalog-key", false)
	digest := sha256.Sum256([]byte("ordinary-login-is-not-a-key"))
	if _, err := store.pool.Exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, kind, expires_at)
		VALUES ('ordinary-login', 'admin', $1, 'emby', now() + interval '1 hour')`, digest[:]); err != nil {
		t.Fatal(err)
	}
	for _, subject := range []Subject{revoked, orphan, {ApplicationCredentialID: "ordinary-login"}, {ApplicationCredentialID: "missing-key"}} {
		t.Run(subject.ApplicationCredentialID, func(t *testing.T) {
			subject.UserID = "disabled"
			query := Query{UserID: subject.UserID, ApplicationCredentialID: subject.ApplicationCredentialID, Recursive: true}
			checks := []struct {
				name string
				run  func() error
			}{
				{"items", func() error { _, err := store.QueryItems(ctx, query); return err }},
				{"item", func() error { _, err := store.GetItemFor(ctx, subject, "movie-a"); return err }},
				{"libraries", func() error { _, err := store.ListUserLibrariesFor(ctx, subject); return err }},
				{"latest", func() error { _, err := store.QueryLatest(ctx, query, true); return err }},
				{"entities", func() error { _, err := store.ListEntities(ctx, "Genre", query); return err }},
				{"entity", func() error { _, err := store.GetEntityFor(ctx, subject, "Genre", "Drama"); return err }},
				{"images", func() error { _, err := store.ListImagesFor(ctx, subject, "movie-a"); return err }},
				{"empty images", func() error { _, err := store.ImagesForItemsFor(ctx, subject, nil); return err }},
				{"user data", func() error { _, err := store.GetUserDataFor(ctx, subject, "movie-a"); return err }},
				{"favorite", func() error { _, err := store.SetFavoriteFor(ctx, subject, "movie-a", true); return err }},
				{"played", func() error { _, err := store.SetPlayedFor(ctx, subject, "movie-a", true, nil); return err }},
				{"next up", func() error {
					_, err := store.NextUp(ctx, NextUpQuery{UserID: subject.UserID, ApplicationCredentialID: subject.ApplicationCredentialID})
					return err
				}},
				{"notification", func() error {
					_, err := store.UserDataNotificationPage(ctx, UserDataNotificationQuery{UserID: subject.UserID, ApplicationCredentialID: subject.ApplicationCredentialID, ItemID: "movie-a"})
					return err
				}},
			}
			for _, check := range checks {
				if err := check.run(); !errors.Is(err, ErrForbidden) {
					t.Errorf("%s accepted an invalid application credential: %v", check.name, err)
				}
			}
		})
	}
	var count int
	if err := store.pool.QueryRow(ctx, "SELECT count(*) FROM user_item_data").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected application operations wrote %d user-data rows: %v", count, err)
	}
}

func TestApplicationKeyStateWaitsForUserThenRevalidatesRevocation(t *testing.T) {
	for _, lockedResource := range []string{"target user", "credential"} {
		t.Run(lockedResource, func(t *testing.T) {
			ctx, store := libraryQueryTestStore(t)
			seedLibraryQueryFixture(t, ctx, store.pool)
			subject := seedCatalogApplicationKey(t, ctx, store.pool, "blocked-state-key", true)
			subject.UserID = "disabled"
			owner, err := store.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(owner)
			if lockedResource == "target user" {
				if _, err := owner.Exec(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", subject.UserID); err != nil {
					t.Fatal(err)
				}
			} else if _, err := owner.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", subject.ApplicationCredentialID); err != nil {
				t.Fatal(err)
			}
			operationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				_, err := store.SetFavoriteFor(operationCtx, subject, "movie-a", true)
				finished <- err
			}()
			waitCatalogApplicationBlock(t, operationCtx, store.pool, owner.Conn().PgConn().PID())
			if lockedResource == "target user" {
				// A writer waiting on the target must not already hold the key lock.
				revokeCtx, revokeCancel := context.WithTimeout(ctx, 2*time.Second)
				_, err := store.pool.Exec(revokeCtx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", subject.ApplicationCredentialID)
				revokeCancel()
				if err != nil {
					t.Fatalf("credential revocation blocked behind a target-account wait: %v", err)
				}
			}
			if err := owner.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("write after blocked key revocation = %v; want forbidden", err)
				}
			case <-operationCtx.Done():
				t.Fatal("blocked application state write did not return")
			}
			var count int
			if err := store.pool.QueryRow(ctx, "SELECT count(*) FROM user_item_data").Scan(&count); err != nil || count != 0 {
				t.Fatalf("blocked revoked writer stored %d rows: %v", count, err)
			}
		})
	}
}

func waitCatalogApplicationBlock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, blocker uint32) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(waitCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE $1::integer = ANY(pg_blocking_pids(pid)) AND wait_event_type = 'Lock')`, blocker).Scan(&blocked); err != nil {
			t.Fatalf("observe application state lock wait: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-ticker.C:
		case <-waitCtx.Done():
			t.Fatal("application state operation did not wait for its authorization lock")
		}
	}
}
