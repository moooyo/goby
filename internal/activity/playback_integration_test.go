package activity_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/activity"
)

func playbackSchemaEvent(t *testing.T) activity.Event {
	t.Helper()
	fingerprint, err := activity.PlaybackFingerprint(activity.ActionPlaybackStopped, "play_schema-observation")
	if err != nil {
		t.Fatal(err)
	}
	return activity.Event{Action: activity.ActionPlaybackStopped, Severity: activity.SeverityInfo, Source: activity.SourceEmby,
		Actor:    activity.Actor{Kind: activity.ActorUser, ID: "retained-playback-user", CredentialID: "existing-playback-credential"},
		Resource: activity.Resource{Kind: activity.ResourceItem, ID: "retained-playback-item"}, ObservationFingerprint: fingerprint}
}

func insertRawPlaybackSchemaFact(ctx context.Context, tx pgx.Tx, event activity.Event) error {
	fields := make([]string, len(event.ChangedFields))
	for index, field := range event.ChangedFields {
		fields[index] = string(field)
	}
	_, err := tx.Exec(ctx, `INSERT INTO activity_entries
		(action,severity,source,actor_kind,actor_id,actor_credential_id,resource_kind,resource_id,
		 revision,previous_revision,observation_fingerprint,affected_count,state,changed_fields)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		string(event.Action), string(event.Severity), string(event.Source), string(event.Actor.Kind), event.Actor.ID,
		event.Actor.CredentialID, string(event.Resource.Kind), event.Resource.ID, event.Revision, event.PreviousRevision,
		event.ObservationFingerprint, event.Count, string(event.State), fields)
	return err
}

func TestPlaybackActivityMigrationRejectsUntypedFactsWithoutTheWriter(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, true)
	for _, test := range []struct {
		name string
		edit func(*activity.Event)
	}{
		{"system source", func(event *activity.Event) { event.Source = activity.SourceSystem }},
		{"system actor", func(event *activity.Event) { event.Actor = activity.Actor{Kind: activity.ActorSystem} }},
		{"native application", func(event *activity.Event) {
			event.Source = activity.SourceNative
			event.Actor.Kind = activity.ActorApplicationKey
		}},
		{"missing authentication", func(event *activity.Event) { event.Actor.CredentialID = "" }},
		{"warning severity", func(event *activity.Event) { event.Severity = activity.SeverityWarning }},
		{"wrong resource", func(event *activity.Event) { event.Resource.Kind = activity.ResourceSession }},
		{"revision", func(event *activity.Event) { event.Revision = 1 }},
		{"previous revision", func(event *activity.Event) { event.PreviousRevision = 1 }},
		{"affected count", func(event *activity.Event) { event.Count = 1 }},
		{"completion claim", func(event *activity.Event) { event.State = activity.StateCompleted }},
		{"changed fields", func(event *activity.Event) { event.ChangedFields = []activity.Field{activity.FieldName} }},
		{"missing fingerprint", func(event *activity.Event) { event.ObservationFingerprint = "" }},
		{"uppercase fingerprint", func(event *activity.Event) { event.ObservationFingerprint = strings.Repeat("A", 64) }},
		{"arbitrary fingerprint", func(event *activity.Event) { event.ObservationFingerprint = strings.Repeat("g", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := playbackSchemaEvent(t)
			test.edit(&event)
			tx := beginActivityTransaction(t, ctx, pool)
			err := insertRawPlaybackSchemaFact(ctx, tx, event)
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
				t.Fatalf("untyped playback fact bypassed the database contract: %v", err)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPlaybackActivityMigrationDeduplicatesOneCanonicalTransition(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, true)
	event := playbackSchemaEvent(t)
	insertActivityEvent(t, ctx, pool, event)
	duplicate := beginActivityTransaction(t, ctx, pool)
	// Even a different actor cannot reuse another canonical transition's
	// fingerprint; authorization remains the business owner's responsibility.
	event.Actor.ID = "different-retained-user"
	err := insertRawPlaybackSchemaFact(ctx, duplicate, event)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
		t.Fatalf("duplicate canonical observation escaped its partial unique index: %v", err)
	}
	if err := duplicate.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM activity_entries WHERE action=$1 AND observation_fingerprint=$2",
		string(event.Action), event.ObservationFingerprint).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate observation changed committed history: count=%d, error=%v", count, err)
	}
}
