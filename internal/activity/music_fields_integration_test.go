package activity_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/activity"
)

func TestMusicActivityDatabaseAllowsOnlyTypedMetadataNames(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, true)
	event := activity.Event{Action: activity.ActionMetadataUpdated, Source: activity.SourceNative,
		Actor: activity.Actor{Kind: activity.ActorUser, ID: "music-editor"}, Resource: activity.Resource{Kind: activity.ResourceItem, ID: "music-track"},
		Revision: 4, ChangedFields: []activity.Field{activity.FieldAlbum, activity.FieldArtists, activity.FieldAlbumArtists}}
	id, _ := insertActivityEvent(t, ctx, pool, event)
	var fields []string
	if err := pool.QueryRow(ctx, "SELECT changed_fields FROM activity_entries WHERE id=$1", id).Scan(&fields); err != nil ||
		!reflect.DeepEqual(fields, []string{"Album", "AlbumArtists", "Artists"}) {
		t.Fatal("music names did not cross the database allowlist", fields, err)
	}
	for _, test := range []struct {
		action, resource string
		fields           []string
	}{
		{"metadata.updated", "item", []string{"Album=private value"}},
		{"metadata.updated", "item", []string{"Private album name"}},
		{"settings.updated", "settings", []string{"Album"}},
		{"user.updated", "user", []string{"Artists"}},
	} {
		_, err := pool.Exec(ctx, `INSERT INTO activity_entries(action,severity,source,actor_kind,resource_kind,resource_id,changed_fields)
			VALUES($1,'Info','system','system',$2,'rejected-music-activity',$3::text[])`, test.action, test.resource, test.fields)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
			t.Fatalf("direct invalid activity bypassed constraint: %+v %v", test, err)
		}
	}
	page := queryActivityPage(t, ctx, pool, activity.QueryOptions{Action: activity.ActionMetadataUpdated, Limit: 10})
	if len(page.Items) != 1 || page.TotalRecordCount != 1 || page.Items[0].Name != "Metadata updated" ||
		page.Items[0].Overview != "Item metadata controls were updated." {
		t.Fatal("music activity lost its fixed public description", page)
	}
}
