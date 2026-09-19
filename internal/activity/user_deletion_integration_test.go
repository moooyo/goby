package activity_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/database"
)

func TestUserDeletionMigrationPreservesEveryPublishedActionAndHistory(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, false)
	activityMigrationVersion22(t, ctx, pool)
	for offset, name := range []string{"0023_backup_activity.sql", "0024_user_settings.sql", "0025_music_artists.sql",
		"0026_theme_owners.sql", "0027_movie_extras.sql", "0028_storage_root_bindings.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "database", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		tx := beginActivityTransaction(t, ctx, pool)
		if _, err := tx.Exec(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version,name) VALUES ($1,$2)", offset+23, name); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	readActions := func() []string {
		var definition string
		if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conrelid='activity_entries'::regclass AND conname='activity_entries_action_check'`).Scan(&definition); err != nil {
			t.Fatal(err)
		}
		values := regexp.MustCompile(`'([^']+)'`).FindAllString(definition, -1)
		slices.Sort(values)
		return values
	}
	published := readActions()
	insertActivityEvent(t, ctx, pool, activityTestEvent("retained-before-deletion-upgrade"))
	event := activityTestEvent("deleted-user")
	event.Action, event.Count = activity.ActionUserDeleted, 1
	old := beginActivityTransaction(t, ctx, pool)
	var checkError *pgconn.PgError
	if err := activity.Record(ctx, old, event); !errors.As(err, &checkError) || checkError.Code != "23514" {
		t.Fatalf("historical schema 28 accepted a later deletion event: %v", err)
	}
	if err := old.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	before := snapshotActivityRows(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if after := snapshotActivityRows(t, ctx, pool); after != before {
		t.Fatal("user deletion migration rewrote existing activity history")
	}
	// The full upgrade adds user deletion in schema 29, the two media deletion
	// actions in schema 34, and library editing in schema 36. Every earlier
	// action and historical activity row must remain unchanged.
	want := append(slices.Clone(published), "'user.deleted'", "'item.deleted'", "'subtitle.deleted'", "'library.updated'")
	slices.Sort(want)
	if !slices.Equal(readActions(), want) {
		t.Fatal("activity upgrade did not preserve the original actions and add exactly the declared actions")
	}
	insertActivityEvent(t, ctx, pool, event)
	page := queryActivityPage(t, ctx, pool, activity.QueryOptions{Action: activity.ActionUserDeleted, ActorID: event.Actor.ID})
	if page.TotalRecordCount != 1 || len(page.Items) != 1 || page.Items[0].Action != activity.ActionUserDeleted ||
		page.Items[0].Name != "User deleted" || page.Items[0].Resource != event.Resource || page.Items[0].Revision != event.Revision {
		t.Fatal("deletion action filtering or public query metadata is incorrect")
	}
	if legacy := queryActivityPage(t, ctx, pool, activity.QueryOptions{Action: activity.ActionUserUpdated}); legacy.TotalRecordCount != 1 {
		t.Fatal("deletion action filtering changed the existing update action")
	}
	libraryEvent := activityTestEvent("edited-library")
	libraryEvent.Action = activity.ActionLibraryUpdated
	libraryEvent.Resource = activity.Resource{Kind: activity.ResourceLibrary, ID: "edited-library"}
	libraryEvent.Revision, libraryEvent.Count = 8, 2
	insertActivityEvent(t, ctx, pool, libraryEvent)
	libraryPage := queryActivityPage(t, ctx, pool, activity.QueryOptions{Action: activity.ActionLibraryUpdated})
	if libraryPage.TotalRecordCount != 1 || len(libraryPage.Items) != 1 ||
		libraryPage.Items[0].Resource != libraryEvent.Resource || libraryPage.Items[0].Revision != 8 {
		t.Fatal("the declared library edit action did not preserve its resource and revision")
	}
	bad := beginActivityTransaction(t, ctx, pool)
	libraryEvent.Resource.Kind = activity.ResourceUser
	if err := activity.Record(ctx, bad, libraryEvent); err == nil {
		t.Fatal("the library edit action accepted a user resource")
	}
	if err := bad.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}
