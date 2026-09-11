package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func playReferencePrepare(t *testing.T, ctx context.Context, store *Store, owner PlaybackOwner, itemID, reference string) PlaySession {
	t.Helper()
	session, err := store.PrepareCorrelatedPlayback(ctx, owner, itemID, media.SourceID(itemID), reference)
	if err != nil {
		t.Fatalf("prepare correlated playback: %v", err)
	}
	if !strings.HasPrefix(session.ID, "play_") || session.ID == reference || session.UserID != owner.UserID ||
		session.AuthSessionID != owner.SessionID || session.DeviceID != owner.DeviceID || session.ItemID != itemID ||
		session.MediaSourceID != media.SourceID(itemID) {
		t.Fatalf("correlated playback has incorrect canonical identity: %+v", session)
	}
	return session
}

type playReferenceRowCounts struct {
	sessions, references, userData int
}

func playReferenceCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) playReferenceRowCounts {
	t.Helper()
	var counts playReferenceRowCounts
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM play_sessions),
		(SELECT count(*) FROM client_playback_references), (SELECT count(*) FROM user_item_data)`).
		Scan(&counts.sessions, &counts.references, &counts.userData); err != nil {
		t.Fatalf("count playback reference state: %v", err)
	}
	return counts
}

func playReferenceBinding(t *testing.T, ctx context.Context, pool *pgxpool.Pool, owner PlaybackOwner, reference string) *string {
	t.Helper()
	var id *string
	if err := pool.QueryRow(ctx, `SELECT play_session_id FROM client_playback_references
		WHERE user_id = $1 AND auth_session_id = $2 AND device_id = $3 AND client_nonce = $4`,
		owner.UserID, owner.SessionID, owner.DeviceID, reference).Scan(&id); err != nil {
		t.Fatalf("read persisted playback reference: %v", err)
	}
	return id
}

func TestStoreCorrelatedPlaybackConcurrentPreparationBindsOneCanonicalSession(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	const reference = "concurrent-client-playback"
	type result struct {
		session PlaySession
		err     error
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan result, 12)
	for index := 0; index < cap(results); index++ {
		go func() {
			<-start
			session, err := store.PrepareCorrelatedPlayback(callCtx, owner, ids[0], media.SourceID(ids[0]), reference)
			results <- result{session: session, err: err}
		}()
	}
	close(start)
	canonicalID := ""
	for index := 0; index < cap(results); index++ {
		select {
		case prepared := <-results:
			if prepared.err != nil {
				t.Errorf("concurrent correlated preparation %d: %v", index, prepared.err)
				continue
			}
			if canonicalID == "" {
				canonicalID = prepared.session.ID
			}
			if prepared.session.ID != canonicalID || prepared.session.ID == reference || !strings.HasPrefix(prepared.session.ID, "play_") {
				t.Errorf("same nonce produced another canonical session: %+v, want %q", prepared.session, canonicalID)
			}
		case <-callCtx.Done():
			t.Fatalf("concurrent correlated preparations did not finish: %v", callCtx.Err())
		}
	}
	if canonicalID == "" {
		t.Fatal("no correlated preparation succeeded")
	}
	if counts := playReferenceCounts(t, ctx, pool); counts != (playReferenceRowCounts{sessions: 1, references: 1, userData: 1}) {
		t.Errorf("same nonce created duplicate state: %+v", counts)
	}
	if bound := playReferenceBinding(t, ctx, pool, owner, reference); bound == nil || *bound != canonicalID {
		t.Errorf("nonce did not retain its canonical binding: %v, want %q", bound, canonicalID)
	}
	for _, supplied := range []string{reference, canonicalID} {
		if resolved, err := store.ResolvePlaybackReference(ctx, owner, supplied); err != nil || resolved != canonicalID {
			t.Errorf("resolve %q: got %q, error = %v", supplied, resolved, err)
		}
		if prepared := playSessionPrepare(t, ctx, store, owner, ids[0], supplied); prepared.ID != canonicalID {
			t.Errorf("explicit preparation changed the canonical identity: %+v", prepared)
		}
		if active, err := store.GetPlaybackSession(ctx, owner, supplied); err != nil || active.ID != canonicalID {
			t.Errorf("get %q: session = %+v, error = %v", supplied, active, err)
		}
	}
	started, data := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: reference, Event: "Started"})
	if started.ID != canonicalID || started.State != "Playing" || data.PlayCount != 1 {
		t.Fatalf("nonce start lost the canonical identity or counted incorrectly: session = %+v, data = %+v", started, data)
	}
	pinged, pingData := playSessionReport(t, ctx, store, owner, PlaybackReport{
		PlaySessionID: reference, Event: "Ping", PositionTicks: playSessionPosition(120 * media.TicksPerSecond),
	})
	if pinged.ID != canonicalID || pinged.State != "Playing" || pinged.PositionTicks != started.PositionTicks || !reflect.DeepEqual(pingData, data) {
		t.Errorf("nonce ping changed position or user data: session = %+v, data = %+v", pinged, pingData)
	}
}

func TestStoreCorrelatedPlaybackKeepsIndependentNoncesOutsideDefaultPlayback(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	first := playReferencePrepare(t, ctx, store, owner, ids[0], "first-client-playback")
	second := playReferencePrepare(t, ctx, store, owner, ids[0], "second-client-playback")
	if first.ID == second.ID {
		t.Fatalf("different client nonces shared an active playback session: %q", first.ID)
	}
	for _, event := range []string{"Progress", "Stopped", "Ping"} {
		if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{ItemID: ids[0], Event: event}); !errors.Is(err, ErrNotFound) {
			t.Errorf("default %s selected a correlated session: got %v, want ErrNotFound", event, err)
		}
	}
	defaultSession := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	if defaultSession.ID == first.ID || defaultSession.ID == second.ID {
		t.Fatalf("default preparation borrowed a correlated session: %+v", defaultSession)
	}
	if reused := playSessionPrepare(t, ctx, store, owner, ids[0], ""); reused.ID != defaultSession.ID {
		t.Errorf("default preparation did not retain its own active session: %+v", reused)
	}
	started, data := playSessionReport(t, ctx, store, owner, PlaybackReport{ItemID: ids[0], Event: "Started"})
	if started.ID != defaultSession.ID || data.PlayCount != 1 {
		t.Fatalf("default start selected the wrong session: session = %+v, data = %+v", started, data)
	}
	for _, event := range []string{"Progress", "Stopped", "Ping"} {
		reported, reportData := playSessionReport(t, ctx, store, owner, PlaybackReport{
			ItemID: ids[0], Event: event, PositionTicks: playSessionPosition(120 * media.TicksPerSecond),
		})
		if reported.ID != defaultSession.ID || reportData.PlayCount != 1 {
			t.Errorf("default %s escaped its own playback history: session = %+v, data = %+v", event, reported, reportData)
		}
	}
	restarted, restartData := playSessionReport(t, ctx, store, owner, PlaybackReport{ItemID: ids[0], Event: "Started"})
	if restarted.ID == defaultSession.ID || restarted.ID == first.ID || restarted.ID == second.ID || restartData.PlayCount != 2 {
		t.Errorf("default restart selected terminal or correlated playback: session = %+v, data = %+v", restarted, restartData)
	}
	for _, correlated := range []PlaySession{first, second} {
		if retained, err := store.GetPlaybackSession(ctx, owner, correlated.ID); err != nil || retained.State != "Prepared" || retained.PositionTicks != 0 {
			t.Errorf("default reports changed correlated playback: session = %+v, error = %v", retained, err)
		}
	}
	var correlatedCount, defaultCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE client_correlated),
		count(*) FILTER (WHERE NOT client_correlated) FROM play_sessions`).Scan(&correlatedCount, &defaultCount); err != nil || correlatedCount != 2 || defaultCount != 2 {
		t.Errorf("playback classification is incorrect: correlated = %d, default = %d, error = %v", correlatedCount, defaultCount, err)
	}
}

func TestStorePlaybackReferencesRespectOwnerScopeAndImmutableItemBinding(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 2)
	const reference = "shared-client-playback"
	prepared := playReferencePrepare(t, ctx, store, owner, ids[0], reference)
	otherAuth := playSessionOwnerFixture(t, ctx, pool, owner.UserID, owner.DeviceID)
	otherUserID := "other-playback-reference-user"
	libraryIntegrationUser(t, ctx, pool, otherUserID, false, true, nil)
	otherUser := playSessionOwnerFixture(t, ctx, pool, otherUserID, owner.DeviceID)
	otherDevice := owner
	otherDevice.DeviceID = "unrecognized-reference-device"
	for _, fixture := range []struct {
		name  string
		owner PlaybackOwner
		want  error
	}{
		{"authentication", otherAuth, ErrNotFound},
		{"user", otherUser, ErrNotFound},
		{"device", otherDevice, ErrForbidden},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, supplied := range []string{reference, prepared.ID} {
				if _, err := store.ResolvePlaybackReference(ctx, fixture.owner, supplied); !errors.Is(err, fixture.want) {
					t.Errorf("foreign reference resolution: got %v, want %v", err, fixture.want)
				}
				if _, err := store.GetPlaybackSession(ctx, fixture.owner, supplied); !errors.Is(err, fixture.want) {
					t.Errorf("foreign playback lookup: got %v, want %v", err, fixture.want)
				}
				if _, err := store.PreparePlayback(ctx, fixture.owner, ids[0], media.SourceID(ids[0]), supplied); !errors.Is(err, fixture.want) {
					t.Errorf("foreign explicit preparation: got %v, want %v", err, fixture.want)
				}
				for _, event := range []string{"Started", "Progress", "Stopped", "Ping"} {
					if _, _, err := store.ReportPlayback(ctx, fixture.owner, PlaybackReport{PlaySessionID: supplied, Event: event}); !errors.Is(err, fixture.want) {
						t.Errorf("foreign %s report: got %v, want %v", event, err, fixture.want)
					}
				}
			}
			if _, err := store.PrepareCorrelatedPlayback(ctx, fixture.owner, ids[0], media.SourceID(ids[0]), prepared.ID); !errors.Is(err, fixture.want) {
				t.Errorf("foreign canonical correlated preparation: got %v, want %v", err, fixture.want)
			}
		})
	}
	for _, fixture := range []struct{ itemID, sourceID string }{
		{ids[1], media.SourceID(ids[1])},
		{ids[0], media.SourceID(ids[1])},
	} {
		if _, err := store.PrepareCorrelatedPlayback(ctx, owner, fixture.itemID, fixture.sourceID, reference); !errors.Is(err, ErrNotFound) {
			t.Errorf("correlated preparation changed the bound item or source: got %v, want ErrNotFound", err)
		}
		if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{
			PlaySessionID: reference, ItemID: fixture.itemID, MediaSourceID: fixture.sourceID, Event: "Progress",
		}); !errors.Is(err, ErrNotFound) {
			t.Errorf("report changed the bound item or source: got %v, want ErrNotFound", err)
		}
	}
	if counts := playReferenceCounts(t, ctx, pool); counts != (playReferenceRowCounts{sessions: 1, references: 1, userData: 1}) {
		t.Errorf("rejected ownership or binding changes created state: %+v", counts)
	}
	seen := map[string]bool{prepared.ID: true}
	for _, independentOwner := range []PlaybackOwner{otherAuth, otherUser} {
		independent := playReferencePrepare(t, ctx, store, independentOwner, ids[0], reference)
		if seen[independent.ID] {
			t.Errorf("same nonce crossed an owner boundary: %+v", independent)
		}
		seen[independent.ID] = true
		if resolved, err := store.ResolvePlaybackReference(ctx, independentOwner, reference); err != nil || resolved != independent.ID {
			t.Errorf("scoped nonce did not resolve independently: id = %q, error = %v", resolved, err)
		}
	}
	if resolved, err := store.ResolvePlaybackReference(ctx, owner, reference); err != nil || resolved != prepared.ID {
		t.Errorf("another owner's same nonce replaced the original binding: id = %q, error = %v", resolved, err)
	}
}

func TestStorePlaybackReferencesRecheckAuthorizationAndEmbyAuthenticationKind(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 2)
	const reference = "authorized-client-playback"
	prepared := playReferencePrepare(t, ctx, store, owner, ids[0], reference)
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: reference, Event: "Started"})
	if _, err := pool.Exec(ctx, "UPDATE items SET type = 'Audio' WHERE id = $1", ids[1]); err != nil {
		t.Fatal("set owned audio reference fixture type")
	}
	const audioReference = "authorized-audio-client-playback"
	audio := playReferencePrepare(t, ctx, store, owner, ids[1], audioReference)
	for _, fixture := range []struct {
		name, block, restore, id string
		want                     error
	}{
		{"revoked authentication", "UPDATE sessions SET revoked_at = now() WHERE id = $1", "UPDATE sessions SET revoked_at = NULL WHERE id = $1", owner.SessionID, ErrForbidden},
		{"disabled user", "UPDATE users SET is_disabled = true WHERE id = $1", "UPDATE users SET is_disabled = false WHERE id = $1", owner.UserID, ErrForbidden},
		{"playback permission", `UPDATE users SET policy = policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id = $1`, "UPDATE users SET policy = policy - 'EnableMediaPlayback' WHERE id = $1", owner.UserID, ErrForbidden},
		{"library access", `UPDATE users SET policy = policy || '{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id = $1`, `UPDATE users SET policy = policy || '{"EnableAllFolders":true}'::jsonb WHERE id = $1`, owner.UserID, ErrNotFound},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, fixture.block, fixture.id); err != nil {
				t.Fatalf("remove reference authorization: %v", err)
			}
			t.Cleanup(func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if _, err := pool.Exec(cleanupCtx, fixture.restore, fixture.id); err != nil {
					t.Errorf("restore reference authorization: %v", err)
				}
			})
			before := playReferenceCounts(t, ctx, pool)
			beforeAudio, beforeAudioData := playSessionSnapshot(t, ctx, pool, audio)
			if _, err := store.ResolvePlaybackReference(ctx, owner, reference); !errors.Is(err, fixture.want) {
				t.Errorf("resolve after authorization removal: got %v, want %v", err, fixture.want)
			}
			if _, err := store.GetPlaybackSession(ctx, owner, reference); !errors.Is(err, fixture.want) {
				t.Errorf("get after authorization removal: got %v, want %v", err, fixture.want)
			}
			for _, event := range []string{"Started", "Progress", "Stopped", "Ping"} {
				if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{PlaySessionID: reference, Event: event}); !errors.Is(err, fixture.want) {
					t.Errorf("%s after authorization removal: got %v, want %v", event, err, fixture.want)
				}
				if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{
					PlaySessionID: audioReference, ItemID: ids[1], MediaSourceID: ids[1], Event: event,
				}); !errors.Is(err, fixture.want) {
					t.Errorf("audio source alias %s after authorization removal: got %v, want %v", event, err, fixture.want)
				}
			}
			if afterAudio, afterAudioData := playSessionSnapshot(t, ctx, pool, audio); afterAudio != beforeAudio || afterAudioData != beforeAudioData {
				t.Error("unauthorized audio source alias reports changed stored playback or user data")
			}
			for _, supplied := range []string{reference, "unauthorized-new-client-playback"} {
				if _, err := store.PrepareCorrelatedPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), supplied); !errors.Is(err, fixture.want) {
					t.Errorf("correlated preparation after authorization removal: got %v, want %v", err, fixture.want)
				}
			}
			if after := playReferenceCounts(t, ctx, pool); after != before {
				t.Errorf("unauthorized reference requests created state: before = %+v, after = %+v", before, after)
			}
			afterAudio, afterAudioData := playSessionSnapshot(t, ctx, pool, audio)
			if afterAudioData != beforeAudioData {
				t.Error("rejected preparation changed audio user data")
			}
			if fixture.name == "library access" {
				// Preparation first commits bounded cleanup of plays whose library
				// access was revoked. Reports above must remain entirely inert;
				// this separate phase may only retire the existing prepared play.
				var retiredOnly bool
				if err := pool.QueryRow(ctx, `SELECT state = 'Expired' AND stopped_at IS NOT NULL
					AND (to_jsonb(p) - '{state,stopped_at,updated_at}'::text[]) =
					($2::jsonb - '{state,stopped_at,updated_at}'::text[])
					FROM play_sessions p WHERE id = $1`, audio.ID, beforeAudio).Scan(&retiredOnly); err != nil || !retiredOnly {
					t.Errorf("library-revoked preparation did not exclusively retire its existing audio play: %v", err)
				}
			} else if afterAudio != beforeAudio {
				t.Error("preparation with revoked authentication or playback permission changed audio playback")
			}
		})
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = true WHERE id = $1", owner.UserID); err != nil {
		t.Fatalf("promote authentication kind fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET kind = 'admin' WHERE id = $1", owner.SessionID); err != nil {
		t.Fatalf("change authentication kind fixture: %v", err)
	}
	if _, err := store.ResolvePlaybackReference(ctx, owner, reference); !errors.Is(err, ErrNotFound) {
		t.Errorf("administrator authentication resolved an Emby nonce: got %v, want ErrNotFound", err)
	}
	if _, err := store.GetPlaybackSession(ctx, owner, reference); !errors.Is(err, ErrNotFound) {
		t.Errorf("administrator authentication looked up an Emby nonce: got %v, want ErrNotFound", err)
	}
	if _, err := store.PreparePlayback(ctx, owner, ids[0], media.SourceID(ids[0]), reference); !errors.Is(err, ErrNotFound) {
		t.Errorf("administrator authentication reused an Emby nonce: got %v, want ErrNotFound", err)
	}
	for _, supplied := range []string{reference, "administrator-new-client-playback", prepared.ID} {
		if _, err := store.PrepareCorrelatedPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), supplied); !errors.Is(err, ErrForbidden) {
			t.Errorf("administrator authentication prepared correlated playback: got %v, want ErrForbidden", err)
		}
	}
	for _, event := range []string{"Started", "Progress", "Stopped", "Ping"} {
		if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{PlaySessionID: reference, Event: event}); !errors.Is(err, ErrNotFound) {
			t.Errorf("administrator authentication reported %s through an Emby nonce: got %v, want ErrNotFound", event, err)
		}
	}
	if resolved, err := store.ResolvePlaybackReference(ctx, owner, prepared.ID); err != nil || resolved != prepared.ID {
		t.Errorf("administrator canonical resolution lost its existing semantics: id = %q, error = %v", resolved, err)
	}
}

func TestStoreCorrelatedAudioReportsRestrictBareSourceAlias(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 3)
	if _, err := pool.Exec(ctx, "UPDATE items SET type = 'Audio' WHERE id = ANY($1::text[])", ids[:2]); err != nil {
		t.Fatal("set owned audio source alias fixture types")
	}
	const reference = "audio-source-alias"
	prepared := playReferencePrepare(t, ctx, store, owner, ids[0], reference)
	legacy := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	movie := playReferencePrepare(t, ctx, store, owner, ids[2], "movie-source-alias")
	otherAuth := playSessionOwnerFixture(t, ctx, pool, owner.UserID, owner.DeviceID)
	const otherUserID = "other-audio-source-user"
	libraryIntegrationUser(t, ctx, pool, otherUserID, false, true, nil)
	otherUser := playSessionOwnerFixture(t, ctx, pool, otherUserID, owner.DeviceID)
	otherDevice := owner
	otherDevice.DeviceID = "unknown-audio-source-device"
	snapshot := func(t *testing.T) string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
			'plays', (SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM play_sessions p),
			'references', (SELECT jsonb_agg(to_jsonb(r) ORDER BY client_nonce) FROM client_playback_references r),
			'data', (SELECT jsonb_agg(to_jsonb(d) ORDER BY user_id, item_id) FROM user_item_data d))::text`).Scan(&value); err != nil {
			t.Fatal("snapshot owned audio source alias state")
		}
		return value
	}
	before := snapshot(t)
	for _, fixture := range []struct {
		name, reference, itemID, sourceID string
		owner                             PlaybackOwner
		want                              error
	}{
		{"unknown nonce", "unknown-audio-reference", ids[0], ids[0], owner, ErrNotFound},
		{"different item", reference, ids[1], ids[1], owner, ErrNotFound},
		{"different bare source", reference, ids[0], ids[1], owner, ErrNotFound},
		{"different canonical source", reference, ids[0], media.SourceID(ids[1]), owner, ErrNotFound},
		{"legacy explicit", legacy.ID, ids[0], ids[0], owner, ErrNotFound},
		{"legacy implicit", "", ids[0], ids[0], owner, ErrNotFound},
		{"correlated video", movie.ID, ids[2], ids[2], owner, ErrNotFound},
		{"other authentication", reference, ids[0], ids[0], otherAuth, ErrNotFound},
		{"other user", reference, ids[0], ids[0], otherUser, ErrNotFound},
		{"other device", reference, ids[0], ids[0], otherDevice, ErrForbidden},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, event := range []string{"Started", "Progress", "Stopped"} {
				if _, _, err := store.ReportPlayback(ctx, fixture.owner, PlaybackReport{
					PlaySessionID: fixture.reference, ItemID: fixture.itemID, MediaSourceID: fixture.sourceID,
					Event: event, PositionTicks: playSessionPosition(120 * media.TicksPerSecond),
				}); !errors.Is(err, fixture.want) {
					t.Errorf("rejected audio alias %s: got %v, want %v", event, err, fixture.want)
				}
			}
			if snapshot(t) != before {
				t.Fatal("rejected audio source alias created or changed playback state")
			}
		})
	}
	// Both owned reference forms select the same correlated play; the canonical
	// source remains authoritative even when the client uses the bare item ID.
	for _, fixture := range []struct {
		reference, sourceID, event, state string
		position                          int64
	}{
		{reference, ids[0], "Started", "Playing", 0},
		{prepared.ID, ids[0], "Progress", "Playing", 180 * media.TicksPerSecond},
		{reference, media.SourceID(ids[0]), "Progress", "Playing", 120 * media.TicksPerSecond},
		{reference, ids[0], "Stopped", "Stopped", 120 * media.TicksPerSecond},
	} {
		play, data := playSessionReport(t, ctx, store, owner, PlaybackReport{
			PlaySessionID: fixture.reference, ItemID: ids[0], MediaSourceID: fixture.sourceID,
			Event: fixture.event, PositionTicks: &fixture.position,
		})
		if play.ID != prepared.ID || play.MediaSourceID != media.SourceID(ids[0]) || !play.clientCorrelated ||
			play.State != fixture.state || play.PositionTicks != fixture.position || data.PlayCount != 1 ||
			data.PlaybackPositionTicks != fixture.position || data.LastPlayedDate == nil || data.Played {
			t.Fatalf("audio source alias lost canonical state or durable progress: play = %+v, data = %+v", play, data)
		}
	}
	terminal := snapshot(t)
	for _, event := range []string{"Started", "Progress", "Stopped"} {
		playSessionReport(t, ctx, store, owner, PlaybackReport{
			PlaySessionID: reference, ItemID: ids[0], MediaSourceID: ids[0], Event: event,
			PositionTicks: playSessionPosition(500 * media.TicksPerSecond),
		})
	}
	if snapshot(t) != terminal {
		t.Fatal("late audio source aliases revived or recounted a stopped play")
	}
}

func TestStorePlaybackReferenceTerminalReportsNeverReviveOrRecount(t *testing.T) {
	for _, terminal := range []string{"Stopped", "Expired"} {
		t.Run(terminal, func(t *testing.T) {
			ctx, pool, store, owner, ids := playSessionFixture(t, 1)
			const reference = "terminal-client-playback"
			prepared := playReferencePrepare(t, ctx, store, owner, ids[0], reference)
			playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: reference, Event: "Started"})
			var stopped PlaySession
			var stoppedData UserData
			if terminal == "Stopped" {
				stopped, stoppedData = playSessionReport(t, ctx, store, owner, PlaybackReport{
					PlaySessionID: reference, Event: "Stopped", PositionTicks: playSessionPosition(120 * media.TicksPerSecond),
				})
			} else {
				if _, err := pool.Exec(ctx, "UPDATE play_sessions SET expires_at = clock_timestamp() - interval '1 second' WHERE id = $1", prepared.ID); err != nil {
					t.Fatalf("expire correlated playback: %v", err)
				}
				stopped, stoppedData = playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: reference, Event: "Ping"})
			}
			if stopped.State != terminal || stopped.StoppedAt == nil || stoppedData.PlayCount != 1 {
				t.Fatalf("correlated playback did not become terminal: session = %+v, data = %+v", stopped, stoppedData)
			}
			beforeSession, beforeData := playSessionSnapshot(t, ctx, pool, stopped)
			for _, supplied := range []string{reference, prepared.ID} {
				if resolved, err := store.ResolvePlaybackReference(ctx, owner, supplied); err != nil || resolved != prepared.ID {
					t.Errorf("terminal reference was not resolvable: id = %q, error = %v", resolved, err)
				}
				if _, err := store.GetPlaybackSession(ctx, owner, supplied); !errors.Is(err, ErrNotFound) {
					t.Errorf("terminal reference was returned as live playback: got %v, want ErrNotFound", err)
				}
				if _, err := store.PreparePlayback(ctx, owner, ids[0], media.SourceID(ids[0]), supplied); !errors.Is(err, ErrNotFound) {
					t.Errorf("explicit preparation revived terminal playback: got %v, want ErrNotFound", err)
				}
				if _, err := store.PrepareCorrelatedPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), supplied); !errors.Is(err, ErrNotFound) {
					t.Errorf("correlated preparation revived terminal playback: got %v, want ErrNotFound", err)
				}
				for _, event := range []string{"Started", "Progress", "Stopped", "Ping"} {
					late, data := playSessionReport(t, ctx, store, owner, PlaybackReport{
						PlaySessionID: supplied, Event: event, PositionTicks: playSessionPosition(540 * media.TicksPerSecond),
					})
					if !reflect.DeepEqual(late, stopped) || !reflect.DeepEqual(data, stoppedData) {
						t.Errorf("late %s changed terminal playback: session = %+v, data = %+v", event, late, data)
					}
				}
			}
			if afterSession, afterData := playSessionSnapshot(t, ctx, pool, stopped); afterSession != beforeSession || afterData != beforeData {
				t.Error("terminal reference requests changed persisted playback or user data")
			}
			if counts := playReferenceCounts(t, ctx, pool); counts != (playReferenceRowCounts{sessions: 1, references: 1, userData: 1}) {
				t.Errorf("terminal reference requests created new state: %+v", counts)
			}
		})
	}
}

func TestStorePlaybackReferenceTombstonesSurviveHistoryPruningUntilAuthenticationEnds(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	const reference = "retained-client-playback"
	prepared := playReferencePrepare(t, ctx, store, owner, ids[0], reference)
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: reference, Event: "Stopped"})
	if _, err := pool.Exec(ctx, "UPDATE play_sessions SET created_at = now() - interval '2 days' WHERE id = $1", prepared.ID); err != nil {
		t.Fatalf("age correlated playback history: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO play_sessions
		(id, user_id, auth_session_id, device_id, item_id, media_source_id, state,
		 position_ticks, duration_ticks, created_at, updated_at, expires_at, stopped_at)
		SELECT 'play_reference_history_' || entry::text, $1, $2, $3, $4, $5, 'Stopped',
			0, $6, now() - interval '1 day' + entry * interval '1 second',
			now() - interval '1 hour', now() - interval '1 hour', now() - interval '1 hour'
		FROM generate_series(1, 256) AS entry`,
		owner.UserID, owner.SessionID, owner.DeviceID, ids[0], media.SourceID(ids[0]), playbackTestDuration); err != nil {
		t.Fatalf("seed bounded terminal history: %v", err)
	}
	playSessionPrepare(t, ctx, store, owner, ids[0], "")
	if bound := playReferenceBinding(t, ctx, pool, owner, reference); bound != nil {
		t.Fatalf("pruned playback did not leave a reference tombstone: %v", bound)
	}
	before := playReferenceCounts(t, ctx, pool)
	for _, supplied := range []string{reference, prepared.ID} {
		if _, err := store.ResolvePlaybackReference(ctx, owner, supplied); !errors.Is(err, ErrNotFound) {
			t.Errorf("pruned reference resolution: got %v, want ErrNotFound", err)
		}
		if _, err := store.GetPlaybackSession(ctx, owner, supplied); !errors.Is(err, ErrNotFound) {
			t.Errorf("pruned reference lookup: got %v, want ErrNotFound", err)
		}
		if _, err := store.PrepareCorrelatedPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), supplied); !errors.Is(err, ErrNotFound) {
			t.Errorf("pruned reference rebound to new playback: got %v, want ErrNotFound", err)
		}
		for _, event := range []string{"Started", "Progress", "Stopped", "Ping"} {
			if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{PlaySessionID: supplied, Event: event}); !errors.Is(err, ErrNotFound) {
				t.Errorf("pruned reference %s: got %v, want ErrNotFound", event, err)
			}
		}
	}
	if after := playReferenceCounts(t, ctx, pool); after != before {
		t.Errorf("pruned reference requests created state: before = %+v, after = %+v", before, after)
	}
	for _, reason := range []string{"revoked", "expired"} {
		retiredOwner := playSessionOwnerFixture(t, ctx, pool, owner.UserID, owner.DeviceID)
		retired := playReferencePrepare(t, ctx, store, retiredOwner, ids[0], reason+"-client-playback")
		if _, err := pool.Exec(ctx, "DELETE FROM play_sessions WHERE id = $1", retired.ID); err != nil {
			t.Fatalf("delete playback while retaining its reference: %v", err)
		}
		if bound := playReferenceBinding(t, ctx, pool, retiredOwner, reason+"-client-playback"); bound != nil {
			t.Errorf("deleted playback retained a nonnull binding: %v", bound)
		}
		statement := "UPDATE sessions SET revoked_at = now() WHERE id = $1"
		if reason == "expired" {
			statement = "UPDATE sessions SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE id = $1"
		}
		if _, err := pool.Exec(ctx, statement, retiredOwner.SessionID); err != nil {
			t.Fatalf("end tombstone authentication: %v", err)
		}
		playSessionPrepare(t, ctx, store, owner, ids[0], "")
		var retained int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM client_playback_references WHERE auth_session_id = $1", retiredOwner.SessionID).Scan(&retained); err != nil || retained != 0 {
			t.Errorf("%s authentication retained reference tombstones: count = %d, error = %v", reason, retained, err)
		}
		if bound := playReferenceBinding(t, ctx, pool, owner, reference); bound != nil {
			t.Errorf("cleanup rebound the live authentication tombstone: %v", bound)
		}
	}
}

func TestStorePlaybackReferencesRejectMalformedAndUnknownInputsWithoutStateGrowth(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	for _, reference := range []string{strings.Repeat("a", 256), strings.Repeat("\u754c", 85) + "a"} {
		playReferencePrepare(t, ctx, store, owner, ids[0], reference)
	}
	before := playReferenceCounts(t, ctx, pool)
	for _, fixture := range []struct{ name, reference string }{
		{"missing", ""},
		{"blank", "   "},
		{"ASCII overflow", strings.Repeat("a", 257)},
		{"UTF-8 byte overflow", strings.Repeat("\u754c", 86)},
		{"NUL", "client\x00playback"},
		{"newline", "client\nplayback"},
		{"tab", "client\tplayback"},
		{"carriage return", "client\rplayback"},
		{"DEL", "client\x7fplayback"},
		{"Unicode control", "client\u0085playback"},
		{"invalid UTF-8", string([]byte{'c', 0xff})},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if _, err := store.PrepareCorrelatedPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), fixture.reference); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("invalid correlated preparation: got %v, want ErrInvalidInput", err)
			}
			if _, err := store.ResolvePlaybackReference(ctx, owner, fixture.reference); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("invalid reference resolution: got %v, want ErrInvalidInput", err)
			}
			if _, err := store.GetPlaybackSession(ctx, owner, fixture.reference); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("invalid reference lookup: got %v, want ErrInvalidInput", err)
			}
			if fixture.reference != "" {
				if _, err := store.PreparePlayback(ctx, owner, ids[0], media.SourceID(ids[0]), fixture.reference); !errors.Is(err, ErrInvalidInput) {
					t.Errorf("invalid explicit preparation: got %v, want ErrInvalidInput", err)
				}
				for _, event := range []string{"Started", "Progress", "Stopped", "Ping"} {
					if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{PlaySessionID: fixture.reference, Event: event}); !errors.Is(err, ErrInvalidInput) {
						t.Errorf("invalid reference %s: got %v, want ErrInvalidInput", event, err)
					}
				}
			}
			if after := playReferenceCounts(t, ctx, pool); after != before {
				t.Errorf("malformed reference grew persisted state: before = %+v, after = %+v", before, after)
			}
		})
	}
	for _, reference := range []string{"unknown-client-playback", "play_unknown-client-playback", owner.SessionID} {
		if _, err := store.ResolvePlaybackReference(ctx, owner, reference); !errors.Is(err, ErrNotFound) {
			t.Errorf("unknown reference resolution: got %v, want ErrNotFound", err)
		}
		if _, err := store.GetPlaybackSession(ctx, owner, reference); !errors.Is(err, ErrNotFound) {
			t.Errorf("unknown reference lookup: got %v, want ErrNotFound", err)
		}
		if _, err := store.PreparePlayback(ctx, owner, ids[0], media.SourceID(ids[0]), reference); !errors.Is(err, ErrNotFound) {
			t.Errorf("ordinary preparation bound an unknown reference: got %v, want ErrNotFound", err)
		}
		for _, event := range []string{"Started", "Progress", "Stopped", "Ping"} {
			if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{PlaySessionID: reference, ItemID: ids[0], Event: event}); !errors.Is(err, ErrNotFound) {
				t.Errorf("report %s bound an unknown reference: got %v, want ErrNotFound", event, err)
			}
		}
	}
	if _, err := store.PrepareCorrelatedPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), "play_unknown-client-playback"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown canonical ID created a nonce binding: got %v, want ErrNotFound", err)
	}
	if after := playReferenceCounts(t, ctx, pool); after != before {
		t.Errorf("unknown references grew persisted state: before = %+v, after = %+v", before, after)
	}
}

func TestStorePlaybackReferencesReserveCanonicalNamespace(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	const reference = "canonical-namespace-client-playback"
	prepared := playReferencePrepare(t, ctx, store, owner, ids[0], reference)
	otherOwner := playSessionOwnerFixture(t, ctx, pool, owner.UserID, owner.DeviceID)
	foreign := playReferencePrepare(t, ctx, store, otherOwner, ids[0], reference)
	_, err := pool.Exec(ctx, `UPDATE client_playback_references SET client_nonce = $1
		WHERE user_id = $2 AND auth_session_id = $3 AND device_id = $4 AND client_nonce = $5`,
		foreign.ID, owner.UserID, owner.SessionID, owner.DeviceID, reference)
	var constraintError *pgconn.PgError
	if !errors.As(err, &constraintError) || constraintError.Code != "23514" {
		t.Errorf("reference schema accepted the reserved canonical namespace: %v", err)
	}
	before := playReferenceCounts(t, ctx, pool)
	for _, supplied := range []string{foreign.ID, "play_unknown-reserved-reference"} {
		if _, err := store.ResolvePlaybackReference(ctx, owner, supplied); !errors.Is(err, ErrNotFound) {
			t.Errorf("reserved reference resolved through another binding: got %v, want ErrNotFound", err)
		}
		if _, err := store.PrepareCorrelatedPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), supplied); !errors.Is(err, ErrNotFound) {
			t.Errorf("reserved reference created a client binding: got %v, want ErrNotFound", err)
		}
	}
	if reused, err := store.PrepareCorrelatedPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), prepared.ID); err != nil || reused.ID != prepared.ID {
		t.Errorf("owned canonical ID was not reused directly: session = %+v, error = %v", reused, err)
	}
	if after := playReferenceCounts(t, ctx, pool); after != before {
		t.Errorf("canonical references created alias state: before = %+v, after = %+v", before, after)
	}
	if bound := playReferenceBinding(t, ctx, pool, owner, reference); bound == nil || *bound != prepared.ID {
		t.Errorf("canonical collision changed the original binding: %v", bound)
	}
}

func TestStoreCorrelatedPlaybackActiveCapacityIsAtomicAcrossNewNonces(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	for index := 0; index < 31; index++ {
		playReferencePrepare(t, ctx, store, owner, ids[0], fmt.Sprintf("capacity-client-%02d", index))
	}
	type result struct {
		reference string
		session   PlaySession
		err       error
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, reference := range []string{"capacity-client-31", "capacity-client-32"} {
		go func(reference string) {
			<-start
			session, err := store.PrepareCorrelatedPlayback(callCtx, owner, ids[0], media.SourceID(ids[0]), reference)
			results <- result{reference: reference, session: session, err: err}
		}(reference)
	}
	close(start)
	succeeded, busy := 0, 0
	blockedReference := ""
	for index := 0; index < cap(results); index++ {
		select {
		case prepared := <-results:
			if prepared.err == nil {
				succeeded++
			} else if errors.Is(prepared.err, ErrBusy) {
				busy++
				blockedReference = prepared.reference
			} else {
				t.Errorf("correlated capacity contender: %v", prepared.err)
			}
		case <-callCtx.Done():
			t.Fatalf("correlated capacity contenders did not finish: %v", callCtx.Err())
		}
	}
	if succeeded != 1 || busy != 1 {
		t.Fatalf("new nonce capacity was not atomic: successes = %d, busy = %d", succeeded, busy)
	}
	if counts := playReferenceCounts(t, ctx, pool); counts != (playReferenceRowCounts{sessions: 32, references: 32, userData: 1}) {
		t.Errorf("capacity rejection persisted a partial client binding: %+v", counts)
	}
	if _, err := store.PrepareCorrelatedPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), blockedReference); !errors.Is(err, ErrBusy) {
		t.Errorf("additional nonce exceeded authentication capacity: got %v, want ErrBusy", err)
	}
	if _, err := store.PreparePlayback(ctx, owner, ids[0], media.SourceID(ids[0]), ""); !errors.Is(err, ErrBusy) {
		t.Errorf("default preparation bypassed correlated capacity: got %v, want ErrBusy", err)
	}
	retained := playReferencePrepare(t, ctx, store, owner, ids[0], "capacity-client-00")
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: "capacity-client-00", Event: "Stopped"})
	if _, err := store.PrepareCorrelatedPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), "capacity-client-00"); !errors.Is(err, ErrNotFound) {
		t.Errorf("capacity release revived a stopped nonce: got %v, want ErrNotFound", err)
	}
	replacement := playReferencePrepare(t, ctx, store, owner, ids[0], blockedReference)
	if replacement.ID == retained.ID {
		t.Errorf("new nonce reused the stopped session identity: %q", replacement.ID)
	}
	var active int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM play_sessions WHERE auth_session_id = $1
		AND state IN ('Prepared','Playing','Paused') AND expires_at > clock_timestamp()`, owner.SessionID).Scan(&active); err != nil || active != 32 {
		t.Errorf("replacing a stopped correlated session changed the capacity bound: count = %d, error = %v", active, err)
	}
	if counts := playReferenceCounts(t, ctx, pool); counts != (playReferenceRowCounts{sessions: 33, references: 33, userData: 1}) {
		t.Errorf("capacity replacement lost a terminal nonce or created duplicate state: %+v", counts)
	}
}

func TestStoreCorrelatedPlaybackReferenceCapacityIncludesTombstonesAndAllowsReuse(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	const reference = "reference-capacity-live-playback"
	const referenceLimit = 65_536
	prepared := playReferencePrepare(t, ctx, store, owner, ids[0], reference)
	playSessionReport(t, ctx, store, owner, PlaybackReport{
		PlaySessionID: reference, Event: "Started", PositionTicks: playSessionPosition(120 * media.TicksPerSecond),
	})
	if _, err := pool.Exec(ctx, `INSERT INTO client_playback_references
		(user_id, auth_session_id, device_id, client_nonce, play_session_id)
		SELECT $1, $2, $3, 'reference-capacity-tombstone-' || entry::text, NULL
		FROM generate_series(1, $4::integer - 1) AS entry`,
		owner.UserID, owner.SessionID, owner.DeviceID, referenceLimit); err != nil {
		t.Fatalf("fill authentication reference capacity with tombstones: %v", err)
	}
	beforeSession, beforeData := playSessionSnapshot(t, ctx, pool, prepared)
	beforeCounts := playReferenceCounts(t, ctx, pool)
	if beforeCounts != (playReferenceRowCounts{sessions: 1, references: referenceLimit, userData: 1}) {
		t.Fatalf("reference capacity fixture has unexpected state: %+v", beforeCounts)
	}
	if _, err := store.PrepareCorrelatedPlayback(ctx, owner, ids[0], media.SourceID(ids[0]), "reference-capacity-rejected-playback"); !errors.Is(err, ErrBusy) {
		t.Errorf("new nonce exceeded authentication reference capacity: got %v, want ErrBusy", err)
	}
	if afterCounts := playReferenceCounts(t, ctx, pool); afterCounts != beforeCounts {
		t.Errorf("reference capacity rejection created state: before = %+v, after = %+v", beforeCounts, afterCounts)
	}
	if afterSession, afterData := playSessionSnapshot(t, ctx, pool, prepared); afterSession != beforeSession || afterData != beforeData {
		t.Error("reference capacity rejection changed existing playback or user data")
	}
	if retained := playReferencePrepare(t, ctx, store, owner, ids[0], reference); retained.ID != prepared.ID || retained.State != "Playing" || retained.PositionTicks != 120*media.TicksPerSecond {
		t.Errorf("reference capacity prevented reusing live playback: %+v", retained)
	}
	var active int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM play_sessions WHERE auth_session_id = $1
		AND state IN ('Prepared','Playing','Paused') AND expires_at > clock_timestamp()`, owner.SessionID).Scan(&active); err != nil || active != 1 {
		t.Errorf("reference capacity checks changed active playback count: count = %d, error = %v", active, err)
	}
	if data, err := store.GetUserData(ctx, owner.UserID, ids[0]); err != nil || data.PlayCount != 1 || data.PlaybackPositionTicks != 120*media.TicksPerSecond {
		t.Errorf("reference capacity checks changed user playback data: data = %+v, error = %v", data, err)
	}
	if afterCounts := playReferenceCounts(t, ctx, pool); afterCounts != beforeCounts {
		t.Errorf("reference capacity reuse created state: before = %+v, after = %+v", beforeCounts, afterCounts)
	}
}
