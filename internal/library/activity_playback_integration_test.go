package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/media"
)

type playbackActivityFact struct {
	action, severity, source, actorKind, actorID, credentialID string
	resourceKind, resourceID, fingerprint, state, raw          string
	revision, previousRevision, count                          int64
	fields                                                     []string
	createdAt                                                  time.Time
}

func playbackActivityFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) []playbackActivityFact {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT action,severity,source,actor_kind,actor_id,actor_credential_id,
		resource_kind,resource_id,observation_fingerprint,state,revision,previous_revision,affected_count,
		changed_fields,created_at,to_jsonb(entry)::text FROM activity_entries entry
		WHERE action IN ('playback.started','playback.stopped') AND resource_id=$1 ORDER BY id`, itemID)
	if err != nil {
		t.Fatalf("query committed playback activity: %v", err)
	}
	defer rows.Close()
	facts := make([]playbackActivityFact, 0)
	for rows.Next() {
		var fact playbackActivityFact
		if err := rows.Scan(&fact.action, &fact.severity, &fact.source, &fact.actorKind, &fact.actorID, &fact.credentialID,
			&fact.resourceKind, &fact.resourceID, &fact.fingerprint, &fact.state, &fact.revision, &fact.previousRevision,
			&fact.count, &fact.fields, &fact.createdAt, &fact.raw); err != nil {
			t.Fatalf("read committed playback activity: %v", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("finish reading playback activity: %v", err)
	}
	return facts
}

func playbackActivityAssertFact(t *testing.T, fact playbackActivityFact, action activity.Action, source activity.Source,
	owner PlaybackOwner, actorID, itemID, canonicalID string) {
	t.Helper()
	fingerprint, err := activity.PlaybackFingerprint(action, canonicalID)
	if err != nil {
		t.Fatalf("derive the canonical playback observation: %v", err)
	}
	kind := activity.ActorUser
	if owner.ApplicationKey {
		kind = activity.ActorApplicationKey
	}
	if fact.action != string(action) || fact.severity != string(activity.SeverityInfo) || fact.source != string(source) ||
		fact.actorKind != string(kind) || fact.actorID != actorID || fact.credentialID != owner.SessionID ||
		fact.resourceKind != string(activity.ResourceItem) || fact.resourceID != itemID || fact.fingerprint != fingerprint ||
		fact.state != "" || fact.revision != 0 || fact.previousRevision != 0 || fact.count != 0 || len(fact.fields) != 0 || fact.createdAt.IsZero() {
		t.Fatalf("playback fact lost its canonical identity or attribution: %+v", fact)
	}
}

// Sequence advancement is excluded because PostgreSQL retains it on rollback.
func playbackActivitySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, play PlaySession) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'playback',to_jsonb(play),
		'data',(SELECT to_jsonb(data) FROM user_item_data data WHERE data.user_id=play.user_id AND data.item_id=play.item_id),
		'credential',(SELECT to_jsonb(authentication) FROM sessions authentication WHERE authentication.id=play.auth_session_id),
		'activity',(SELECT COALESCE(jsonb_agg(to_jsonb(entry) ORDER BY id),'[]') FROM activity_entries entry
			WHERE entry.action IN ('playback.started','playback.stopped') AND entry.resource_id=play.item_id))::text
		FROM play_sessions play WHERE play.id=$1`, play.ID).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot playback, user data, credential and committed activity: %v", err)
	}
	return snapshot
}

func TestStorePlaybackActivityRecordsCommittedTransitionsOnce(t *testing.T) {
	for _, kind := range []string{"emby", "admin"} {
		t.Run(kind, func(t *testing.T) {
			ctx, pool, store, owner, ids := playSessionFixture(t, 1)
			source := activity.SourceEmby
			if kind == "admin" {
				if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator=true WHERE id=$1", owner.UserID); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, "UPDATE sessions SET kind='admin' WHERE id=$1", owner.SessionID); err != nil {
					t.Fatal(err)
				}
				source = activity.SourceNative
			}
			prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
			if reused := playSessionPrepare(t, ctx, store, owner, ids[0], prepared.ID); reused.ID != prepared.ID {
				t.Fatal("preparation did not reuse the existing canonical playback")
			}
			ping, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Ping"})
			if ping.State != "Prepared" || ping.StartedAt != nil || len(playbackActivityFacts(t, ctx, pool, ids[0])) != 0 {
				t.Fatal("preparation or presence-only Ping invented a playback transition")
			}
			for _, phase := range []struct{ event, state string }{{"Started", "Playing"}, {"Stopped", "Stopped"}} {
				results := playSessionParallelReports(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID,
					Event: phase.event, PositionTicks: playSessionPosition(120 * media.TicksPerSecond)}, 8)
				for _, result := range results {
					if result.err != nil || result.session.ID != prepared.ID || result.session.State != phase.state || result.data.PlayCount != 1 {
						t.Fatalf("concurrent %s did not retain one committed playback: %+v", phase.event, result)
					}
				}
				want := 1
				if phase.event == "Stopped" {
					want = 2
				}
				facts := playbackActivityFacts(t, ctx, pool, ids[0])
				if len(facts) != want {
					t.Fatalf("concurrent %s produced %d playback facts, want %d", phase.event, len(facts), want)
				}
				playbackActivityAssertFact(t, facts[0], activity.ActionPlaybackStarted, source, owner, owner.UserID, ids[0], prepared.ID)
				if phase.event == "Started" {
					playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Progress", IsPaused: true,
						PositionTicks: playSessionPosition(150 * media.TicksPerSecond)})
					playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Ping"})
					if after := playbackActivityFacts(t, ctx, pool, ids[0]); !reflect.DeepEqual(facts, after) {
						t.Fatal("progress or Ping added a playback fact after the committed start")
					}
				} else {
					playbackActivityAssertFact(t, facts[1], activity.ActionPlaybackStopped, source, owner, owner.UserID, ids[0], prepared.ID)
				}
			}
			terminalFacts := playbackActivityFacts(t, ctx, pool, ids[0])
			for _, event := range []string{"Started", "Stopped", "Progress", "Ping"} {
				playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: event,
					PositionTicks: playSessionPosition(540 * media.TicksPerSecond)})
			}
			if after := playbackActivityFacts(t, ctx, pool, ids[0]); !reflect.DeepEqual(terminalFacts, after) {
				t.Fatal("a duplicate or late terminal report added a playback fact")
			}
			for index, paused := range []bool{true, false} {
				next := playSessionPrepare(t, ctx, store, owner, ids[0], "")
				if next.ID == prepared.ID || next.StartedAt != nil {
					t.Fatal("a later playback did not acquire a fresh canonical identity")
				}
				progress, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: next.ID, Event: "Progress",
					IsPaused: paused, PositionTicks: playSessionPosition(60 * media.TicksPerSecond)})
				wantState := "Playing"
				if paused {
					wantState = "Paused"
				}
				if progress.StartedAt == nil || progress.State != wantState {
					t.Fatalf("first progress did not start the new playback: %+v", progress)
				}
				playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: next.ID, Event: "Stopped"})
				facts := playbackActivityFacts(t, ctx, pool, ids[0])
				if len(facts) != 4+index*2 {
					t.Fatal("a later canonical playback was deduplicated against an earlier playback of the same item")
				}
				playbackActivityAssertFact(t, facts[len(facts)-2], activity.ActionPlaybackStarted, source, owner, owner.UserID, ids[0], next.ID)
				playbackActivityAssertFact(t, facts[len(facts)-1], activity.ActionPlaybackStopped, source, owner, owner.UserID, ids[0], next.ID)
			}
		})
	}
}

func TestStorePlaybackActivityPreparedStopDoesNotInventStarted(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	stopped, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Stopped",
		PositionTicks: playSessionPosition(120 * media.TicksPerSecond)})
	if stopped.State != "Stopped" {
		t.Fatal("the prepared playback was not stopped")
	}
	facts := playbackActivityFacts(t, ctx, pool, ids[0])
	if len(facts) != 1 {
		t.Fatalf("stopping prepared playback produced %d facts, want only one stopped fact", len(facts))
	}
	playbackActivityAssertFact(t, facts[0], activity.ActionPlaybackStopped, activity.SourceEmby, owner, owner.UserID, ids[0], prepared.ID)
	for _, event := range []string{"Started", "Progress", "Stopped"} {
		playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: event})
	}
	if after := playbackActivityFacts(t, ctx, pool, ids[0]); !reflect.DeepEqual(facts, after) {
		t.Fatal("late reports invented a start for already stopped prepared playback")
	}
}

func TestStorePlaybackActivityExpirationDoesNotInventPlaybackTransitions(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 5)
	for index, test := range []struct{ state, event string }{
		{"Prepared", "Started"}, {"Prepared", "Ping"}, {"Prepared", "Stopped"}, {"Playing", "Stopped"},
	} {
		t.Run(test.state+"/"+test.event, func(t *testing.T) {
			prepared := playSessionPrepare(t, ctx, store, owner, ids[index], "")
			if test.state == "Playing" {
				playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
			}
			before := playbackActivityFacts(t, ctx, pool, ids[index])
			if _, err := pool.Exec(ctx, "UPDATE play_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", prepared.ID); err != nil {
				t.Fatal(err)
			}
			expired, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: test.event,
				PositionTicks: playSessionPosition(120 * media.TicksPerSecond)})
			if expired.State != "Expired" {
				t.Fatalf("a lease-ended report did not remain expired: %+v", expired)
			}
			if after := playbackActivityFacts(t, ctx, pool, ids[index]); !reflect.DeepEqual(before, after) {
				t.Fatal("expiration was projected as a playback start or stop")
			}
		})
	}
	prepared := playSessionPrepare(t, ctx, store, owner, ids[4], "")
	if _, err := pool.Exec(ctx, "UPDATE play_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", prepared.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.cleanupPlayback(ctx, owner); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := pool.QueryRow(ctx, "SELECT state FROM play_sessions WHERE id=$1", prepared.ID).Scan(&state); err != nil || state != "Expired" {
		t.Fatalf("cleanup did not expire the abandoned preparation: state=%s error=%v", state, err)
	}
	if len(playbackActivityFacts(t, ctx, pool, ids[4])) != 0 {
		t.Fatal("background cleanup invented playback activity")
	}
}

func TestStorePlaybackActivityRejectedReportsRemainSilent(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 2)
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	libraryIntegrationUser(t, ctx, pool, "playback-activity-other-user", false, true, nil)
	foreign := playSessionOwnerFixture(t, ctx, pool, "playback-activity-other-user", owner.DeviceID)
	sibling := playSessionOwnerFixture(t, ctx, pool, owner.UserID, owner.DeviceID)
	wrongDevice := owner
	wrongDevice.DeviceID = "playback-activity-untrusted-device"
	reject := func(t *testing.T, caller PlaybackOwner, report PlaybackReport, want error) {
		t.Helper()
		before := playbackActivitySnapshot(t, ctx, pool, prepared)
		if _, _, err := store.ReportPlayback(ctx, caller, report); !errors.Is(err, want) {
			t.Fatalf("rejected playback report returned %v, want %v", err, want)
		}
		if after := playbackActivitySnapshot(t, ctx, pool, prepared); after != before {
			t.Fatal("a rejected playback report changed playback, user data or activity")
		}
	}
	for _, test := range []struct {
		name   string
		caller PlaybackOwner
		report PlaybackReport
		want   error
	}{
		{"foreign owner", foreign, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"}, ErrNotFound},
		{"other authentication session", sibling, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"}, ErrNotFound},
		{"untrusted device", wrongDevice, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"}, ErrForbidden},
		{"mismatched item", owner, PlaybackReport{PlaySessionID: prepared.ID, ItemID: ids[1], Event: "Started"}, ErrNotFound},
		{"mismatched source", owner, PlaybackReport{PlaySessionID: prepared.ID, MediaSourceID: media.SourceID(ids[1]), Event: "Started"}, ErrNotFound},
		{"unknown playback", owner, PlaybackReport{PlaySessionID: "play_unknown-activity", Event: "Started"}, ErrNotFound},
		{"negative position", owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started", PositionTicks: playSessionPosition(-1)}, ErrInvalidInput},
	} {
		t.Run(test.name, func(t *testing.T) { reject(t, test.caller, test.report, test.want) })
	}
	var policy []byte
	if err := pool.QueryRow(ctx, "SELECT policy FROM users WHERE id=$1", owner.UserID).Scan(&policy); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, mutation, restore string
		want                    error
	}{
		{"library access removed", `UPDATE users SET policy=policy||'{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`,
			`UPDATE users SET policy=$2::jsonb WHERE id=$1`, ErrNotFound},
		{"playback permission removed", `UPDATE users SET policy=policy||'{"EnableMediaPlayback":false}'::jsonb WHERE id=$1`,
			`UPDATE users SET policy=$2::jsonb WHERE id=$1`, ErrForbidden},
		{"disabled account", `UPDATE users SET is_disabled=true WHERE id=$1`, `UPDATE users SET is_disabled=false WHERE id=$1`, ErrForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, test.mutation, owner.UserID); err != nil {
				t.Fatal(err)
			}
			arguments := []any{owner.UserID}
			if strings.Contains(test.restore, "$2") {
				arguments = append(arguments, policy)
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(ctx, test.restore, arguments...); err != nil {
					t.Errorf("restore the playback activity account fixture: %v", err)
				}
			})
			reject(t, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"}, test.want)
		})
	}
	for _, test := range []struct{ name, mutation, restore string }{
		{"revoked credential", `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`},
		{"expired credential", `UPDATE sessions SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, `UPDATE sessions SET expires_at=clock_timestamp()+interval '1 day' WHERE id=$1`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, test.mutation, owner.SessionID); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(ctx, test.restore, owner.SessionID); err != nil {
					t.Errorf("restore the playback activity credential fixture: %v", err)
				}
			})
			reject(t, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Stopped", PositionTicks: playSessionPosition(120 * media.TicksPerSecond)}, ErrForbidden)
		})
	}
	if len(playbackActivityFacts(t, ctx, pool, ids[0])) != 0 {
		t.Fatal("an unauthorized report produced playback activity")
	}
}

func TestStorePlaybackActivityRetentionDoesNotRebuildPrunedTransitions(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
	stopped, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Stopped",
		PositionTicks: playSessionPosition(120 * media.TicksPerSecond)})
	if facts := playbackActivityFacts(t, ctx, pool, ids[0]); len(facts) != 2 {
		t.Fatalf("the retention fixture did not commit both playback transitions: %d facts", len(facts))
	}
	if _, err := pool.Exec(ctx, `UPDATE activity_entries SET created_at=clock_timestamp()-interval '31 days'
		WHERE action IN ('playback.started','playback.stopped') AND resource_id=$1`, ids[0]); err != nil {
		t.Fatalf("age the retained playback observations: %v", err)
	}
	if pruned, err := activity.PruneExpired(ctx, pool, activity.DefaultRetention); err != nil || pruned != 2 {
		t.Fatalf("the default retention did not prune both old playback observations: pruned=%d error=%v", pruned, err)
	}
	beforePlay, beforeData := playSessionSnapshot(t, ctx, pool, stopped)
	for _, event := range []string{"Started", "Stopped", "Progress", "Ping"} {
		playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: event,
			PositionTicks: playSessionPosition(540 * media.TicksPerSecond)})
	}
	if len(playbackActivityFacts(t, ctx, pool, ids[0])) != 0 {
		t.Fatal("terminal duplicate reports recreated playback observations removed by retention")
	}
	if afterPlay, afterData := playSessionSnapshot(t, ctx, pool, stopped); afterPlay != beforePlay || afterData != beforeData {
		t.Fatal("terminal reports changed persisted playback or user data after activity retention")
	}
}

func TestStorePlaybackActivityApplicationKeyUsesPersistedKeyAndCanonicalReference(t *testing.T) {
	ctx, pool, store, _, ids := playSessionFixture(t, 1)
	var usersBefore int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&usersBefore); err != nil {
		t.Fatal(err)
	}
	owner := applicationPlaybackOwnerFixture(t, ctx, pool, "activity-private-key")
	var keyID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM application_keys WHERE credential_id=$1", owner.SessionID).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	const reference = "activity-private-client-reference"
	prepared := playReferencePrepare(t, ctx, store, owner, ids[0], reference)
	for _, phase := range []struct{ event, reference string }{{"Started", reference}, {"Stopped", prepared.ID}} {
		for _, result := range playSessionParallelReports(t, ctx, store, owner, PlaybackReport{PlaySessionID: phase.reference,
			Event: phase.event, PositionTicks: playSessionPosition(120 * media.TicksPerSecond)}, 8) {
			if result.err != nil || result.session.ID != prepared.ID || !reflect.DeepEqual(result.data, UserData{}) {
				t.Fatalf("correlated application playback acquired another identity or user data: %+v", result)
			}
		}
	}
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: reference, Event: "Stopped"})
	facts := playbackActivityFacts(t, ctx, pool, ids[0])
	if len(facts) != 2 {
		t.Fatalf("canonical and correlated reports produced %d playback facts, want two", len(facts))
	}
	for index, action := range []activity.Action{activity.ActionPlaybackStarted, activity.ActionPlaybackStopped} {
		playbackActivityAssertFact(t, facts[index], action, activity.SourceEmby, owner, strconv.FormatInt(keyID, 10), ids[0], prepared.ID)
		for _, untrusted := range []string{reference, owner.DeviceID, owner.ApplicationClientID} {
			if strings.Contains(facts[index].raw, untrusted) {
				t.Fatal("a playback fact persisted a client reference, device or application client instead of canonical identities")
			}
		}
	}
	var usersAfter, userData int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM user_item_data)").Scan(&usersAfter, &userData); err != nil {
		t.Fatal(err)
	}
	if usersAfter != usersBefore || userData != 0 || prepared.UserID != "" {
		t.Fatal("application-key activity invented an account or per-user playback data")
	}
}

func playbackActivityInstallTrigger(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action activity.Action, mode string) {
	t.Helper()
	var body string
	switch mode {
	case "insert failure":
		body = `RAISE EXCEPTION USING ERRCODE='P0001',MESSAGE='Injected playback activity failure';`
	case "expired credential":
		body = `UPDATE sessions SET created_at=clock_timestamp()-interval '2 hours',
			expires_at=clock_timestamp()-interval '1 hour' WHERE id=NEW.actor_credential_id;`
	case "revoked credential":
		body = `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=NEW.actor_credential_id;`
	default:
		t.Fatalf("unknown playback activity trigger mode %q", mode)
	}
	statement := fmt.Sprintf(`CREATE SEQUENCE playback_activity_trigger_hits;
		CREATE FUNCTION playback_activity_test_trigger() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.action=TG_ARGV[0] THEN
				PERFORM nextval('playback_activity_trigger_hits');
				%s
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER playback_activity_test_trigger BEFORE INSERT ON activity_entries
		FOR EACH ROW EXECUTE FUNCTION playback_activity_test_trigger('%s')`, body, action)
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatalf("install playback activity transaction failure fixture: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanup, `DROP TRIGGER IF EXISTS playback_activity_test_trigger ON activity_entries;
			DROP FUNCTION IF EXISTS playback_activity_test_trigger(); DROP SEQUENCE IF EXISTS playback_activity_trigger_hits`); err != nil {
			t.Errorf("remove playback activity transaction failure fixture: %v", err)
		}
	})
}

func TestStorePlaybackActivityFailureAndFinalAuthorizationRollBackBusinessState(t *testing.T) {
	for _, mode := range []string{"insert failure", "expired credential", "revoked credential"} {
		for _, event := range []string{"Started", "Stopped"} {
			t.Run(mode+"/"+event, func(t *testing.T) {
				ctx, pool, store, owner, ids := playSessionFixture(t, 1)
				prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
				action := activity.ActionPlaybackStarted
				if event == "Stopped" {
					playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
					action = activity.ActionPlaybackStopped
				}
				before := playbackActivitySnapshot(t, ctx, pool, prepared)
				playbackActivityInstallTrigger(t, ctx, pool, action, mode)
				_, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: event,
					PositionTicks: playSessionPosition(540 * media.TicksPerSecond)})
				if mode == "insert failure" {
					var databaseError *pgconn.PgError
					if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
						t.Fatalf("the activity SQL failure did not fail its playback transaction: %v", err)
					}
				} else if !errors.Is(err, ErrForbidden) {
					t.Fatalf("authorization ending during the activity insert did not reject commit: %v", err)
				}
				var reached bool
				if err := pool.QueryRow(ctx, "SELECT is_called FROM playback_activity_trigger_hits").Scan(&reached); err != nil || !reached {
					t.Fatalf("the failed report did not reach its activity INSERT: reached=%v error=%v", reached, err)
				}
				if after := playbackActivitySnapshot(t, ctx, pool, prepared); after != before {
					t.Fatal("a failed activity insert or final authorization check committed playback, user data, credentials or activity")
				}
			})
		}
	}
}
