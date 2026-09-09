package library

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func applicationPlaybackOwnerFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) PlaybackOwner {
	t.Helper()
	owner := PlaybackOwner{ApplicationKey: true, SessionID: "key_" + name, DeviceID: "key-device-" + name}
	digest := sha256.Sum256([]byte("application-playback-fixture-" + name))
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id, token_hash, kind, device_id)
		VALUES ($1, $2, 'application_key', $3)`, owner.SessionID, digest[:], owner.DeviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO application_keys (credential_id, secret_ciphertext)
		VALUES ($1, $2)`, owner.SessionID, []byte("fixture-ciphertext")); err != nil {
		t.Fatal(err)
	}
	return applicationPlaybackClientFixture(t, ctx, pool, owner, name, owner.DeviceID)
}

func applicationPlaybackClientFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, owner PlaybackOwner, name, deviceID string) PlaybackOwner {
	t.Helper()
	owner.ApplicationClientID, owner.DeviceID = "application-client-"+name, deviceID
	if _, err := pool.Exec(ctx, `INSERT INTO application_key_clients
		(id, credential_id, client_name, device_id, device_name, client_version)
		VALUES ($1, $2, $3, $4, 'Fixture device', '1')`, owner.ApplicationClientID, owner.SessionID, name, deviceID); err != nil {
		t.Fatal(err)
	}
	return owner
}

func TestApplicationKeyPlaybackPersistsUserlessLifecycleAndCorrelation(t *testing.T) {
	ctx, pool, store, normal, ids := playSessionFixture(t, 2)
	first := applicationPlaybackOwnerFixture(t, ctx, pool, "first")
	second := applicationPlaybackOwnerFixture(t, ctx, pool, "second")
	var usersBefore, loginsBefore int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users),
		(SELECT count(*) FROM sessions WHERE kind IN ('emby','admin'))`).Scan(&usersBefore, &loginsBefore); err != nil {
		t.Fatal(err)
	}
	const reference = "shared-client-reference"
	prepared := playReferencePrepare(t, ctx, store, first, ids[0], reference)
	other := playReferencePrepare(t, ctx, store, second, ids[0], reference)
	if prepared.ID == other.ID || !prepared.ApplicationKey || prepared.UserID != "" || prepared.ApplicationClientID != first.ApplicationClientID {
		t.Fatalf("application keys did not retain independent userless plays: %+v, %+v", prepared, other)
	}
	if reused := playReferencePrepare(t, ctx, store, first, ids[0], reference); reused.ID != prepared.ID {
		t.Fatalf("same credential and reference did not reuse the canonical play: %+v", reused)
	}
	if _, err := store.GetPlaybackSession(ctx, second, prepared.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another key read the canonical playback: %v", err)
	}
	foreignClient := first
	foreignClient.ApplicationClientID = second.ApplicationClientID
	if _, err := store.GetPlaybackSession(ctx, foreignClient, prepared.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("key accepted another credential's client binding: %v", err)
	}
	if _, err := store.PrepareCorrelatedPlayback(ctx, first, ids[1], media.SourceID(ids[1]), reference); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reference rebound to another source: %v", err)
	}
	invalid := first
	invalid.UserID = normal.UserID
	if _, err := store.PreparePlayback(ctx, invalid, ids[0], "", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("key acquired a target user playback identity: %v", err)
	}
	wrongDevice := first
	wrongDevice.DeviceID = second.DeviceID
	if _, err := store.GetPlaybackSession(ctx, wrongDevice, prepared.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reported device replaced the credential device: %v", err)
	}
	started, data := playSessionReport(t, ctx, store, first, PlaybackReport{PlaySessionID: reference, Event: "Started"})
	if started.State != "Playing" || !reflect.DeepEqual(data, UserData{}) {
		t.Fatalf("key start created user data: %+v, %+v", started, data)
	}
	volume := 37
	progress, data := playSessionReport(t, ctx, store, first, PlaybackReport{PlaySessionID: reference,
		Event: "Progress", PositionTicks: playSessionPosition(120 * media.TicksPerSecond), IsPaused: true,
		PlayerState: &PlayerStateUpdate{VolumeLevel: &volume}})
	if progress.State != "Paused" || progress.PositionTicks != 120*media.TicksPerSecond ||
		progress.PlayerState.VolumeLevel == nil || *progress.PlayerState.VolumeLevel != volume || !reflect.DeepEqual(data, UserData{}) {
		t.Fatalf("key progress lost its state or acquired user data: %+v, %+v", progress, data)
	}
	playSessionReport(t, ctx, store, second, PlaybackReport{PlaySessionID: reference, Event: "Started"})
	nowPlaying, err := store.ListNowPlayingSessionsForSubject(ctx, Subject{ApplicationCredentialID: first.SessionID}, true,
		[]string{first.ApplicationClientID, second.ApplicationClientID})
	if err != nil || len(nowPlaying) != 2 {
		t.Fatalf("userless now playing is unavailable: %+v, %v", nowPlaying, err)
	}
	for _, session := range nowPlaying {
		if !session.ApplicationKey || session.UserID != "" {
			t.Fatalf("now playing invented an account: %+v", session)
		}
	}
	stopped, data := playSessionReport(t, ctx, store, first, PlaybackReport{PlaySessionID: reference,
		Event: "Stopped", PositionTicks: playSessionPosition(playbackTestDuration)})
	if stopped.State != "Stopped" || !reflect.DeepEqual(data, UserData{}) {
		t.Fatalf("key stop created user data: %+v, %+v", stopped, data)
	}
	late, data := playSessionReport(t, ctx, store, first, PlaybackReport{PlaySessionID: reference,
		Event: "Progress", PositionTicks: playSessionPosition(1)})
	if !reflect.DeepEqual(stopped, late) || !reflect.DeepEqual(data, UserData{}) {
		t.Fatalf("terminal key playback was changed by progress: %+v", late)
	}
	var usersAfter, loginsAfter, userData, nullablePlays, nullableReferences int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users),
		(SELECT count(*) FROM sessions WHERE kind IN ('emby','admin')),
		(SELECT count(*) FROM user_item_data), (SELECT count(*) FROM play_sessions WHERE user_id IS NULL),
		(SELECT count(*) FROM client_playback_references WHERE user_id IS NULL)`).
		Scan(&usersAfter, &loginsAfter, &userData, &nullablePlays, &nullableReferences); err != nil {
		t.Fatal(err)
	}
	if usersAfter != usersBefore || loginsAfter != loginsBefore || userData != 0 || nullablePlays != 2 || nullableReferences != 2 {
		t.Fatalf("key lifecycle changed account state: users=%d/%d logins=%d/%d data=%d plays=%d references=%d",
			usersBefore, usersAfter, loginsBefore, loginsAfter, userData, nullablePlays, nullableReferences)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM play_sessions WHERE id = $1", prepared.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareCorrelatedPlayback(ctx, first, ids[0], "", reference); !errors.Is(err, ErrNotFound) {
		t.Fatalf("key rebound a retained reference tombstone: %v", err)
	}
	if counts := playReferenceCounts(t, ctx, pool); counts != (playReferenceRowCounts{sessions: 1, references: 2}) {
		t.Fatalf("key tombstone was removed or rebound: %+v", counts)
	}
}

func TestApplicationKeyPlaybackConcurrentReferenceAndCredentialCapacity(t *testing.T) {
	ctx, pool, store, _, ids := playSessionFixture(t, 1)
	first := applicationPlaybackOwnerFixture(t, ctx, pool, "capacity-first")
	sibling := applicationPlaybackClientFixture(t, ctx, pool, first, "capacity-sibling", first.DeviceID)
	second := applicationPlaybackOwnerFixture(t, ctx, pool, "capacity-second")
	type result struct {
		session PlaySession
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 8)
	for range cap(results) {
		go func() {
			<-start
			session, err := store.PrepareCorrelatedPlayback(ctx, first, ids[0], "", "concurrent-key-reference")
			results <- result{session, err}
		}()
	}
	close(start)
	canonicalID := ""
	for range cap(results) {
		select {
		case value := <-results:
			if value.err != nil {
				t.Fatal(value.err)
			}
			if canonicalID == "" {
				canonicalID = value.session.ID
			}
			if value.session.ID != canonicalID {
				t.Fatalf("same key reference admitted two plays: %s, %s", canonicalID, value.session.ID)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	for index := 1; index < 32; index++ {
		owner := first
		if index%2 == 1 {
			owner = sibling
		}
		playReferencePrepare(t, ctx, store, owner, ids[0], fmt.Sprintf("key-reference-%d", index))
	}
	if _, err := store.PrepareCorrelatedPlayback(ctx, sibling, ids[0], "", "over-capacity"); !errors.Is(err, ErrBusy) {
		t.Fatalf("key exceeded its authentication quota: %v", err)
	}
	playReferencePrepare(t, ctx, store, second, ids[0], "over-capacity")
	if counts := playReferenceCounts(t, ctx, pool); counts != (playReferenceRowCounts{sessions: 33, references: 33}) {
		t.Fatalf("credential quota or reference uniqueness leaked across keys: %+v", counts)
	}
}

func TestApplicationKeyPlaybackCleanupLeavesOtherOwnersUntouched(t *testing.T) {
	ctx, pool, store, normal, ids := playSessionFixture(t, 1)
	first := applicationPlaybackOwnerFixture(t, ctx, pool, "cleanup-first")
	sibling := applicationPlaybackClientFixture(t, ctx, pool, first, "cleanup-sibling", first.DeviceID)
	second := applicationPlaybackOwnerFixture(t, ctx, pool, "cleanup-second")
	one := playSessionPrepare(t, ctx, store, first, ids[0], "")
	two := playSessionPrepare(t, ctx, store, second, ids[0], "")
	siblingPlay := playSessionPrepare(t, ctx, store, sibling, ids[0], "")
	user := playSessionPrepare(t, ctx, store, normal, ids[0], "")
	if _, err := pool.Exec(ctx, `UPDATE play_sessions SET expires_at = clock_timestamp() - interval '1 second'
		WHERE id = ANY($1::text[])`, []string{one.ID, two.ID, user.ID, siblingPlay.ID}); err != nil {
		t.Fatal(err)
	}
	replacement := playSessionPrepare(t, ctx, store, first, ids[0], "")
	if replacement.ID == one.ID {
		t.Fatal("expired key preparation was revived")
	}
	for _, test := range []struct{ id, state string }{{one.ID, "Expired"}, {two.ID, "Prepared"}, {user.ID, "Prepared"}, {siblingPlay.ID, "Prepared"}} {
		var state string
		if err := pool.QueryRow(ctx, "SELECT state FROM play_sessions WHERE id = $1", test.id).Scan(&state); err != nil || state != test.state {
			t.Fatalf("cleanup changed another owner: id=%s state=%s want=%s error=%v", test.id, state, test.state, err)
		}
	}
}

func TestApplicationKeyPlaybackRevocationRollsBackBlockedProgress(t *testing.T) {
	fixtureCtx, pool, store, _, ids := playSessionFixture(t, 1)
	ctx, cancel := context.WithTimeout(fixtureCtx, 15*time.Second)
	defer cancel()
	owner := applicationPlaybackOwnerFixture(t, ctx, pool, "revoke-progress")
	prepared := playReferencePrepare(t, ctx, store, owner, ids[0], "revocation-reference")
	playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
	var before string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(play)::text FROM play_sessions play WHERE id = $1", prepared.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(blocker)
	var blockerPID int32
	if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", owner.SessionID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{PlaySessionID: prepared.ID,
			Event: "Progress", PositionTicks: playSessionPosition(120 * media.TicksPerSecond)})
		result <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE $1::integer = ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("progress did not wait for credential revocation: %v", err)
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked credential wrote progress: %v", err)
	}
	var after string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(play)::text FROM play_sessions play WHERE id = $1", prepared.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after || playReferenceCounts(t, ctx, pool).userData != 0 {
		t.Fatal("rejected progress changed playback or user data")
	}
	if _, err := store.ResolvePlaybackReference(ctx, owner, "revocation-reference"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked credential resolved playback: %v", err)
	}
	reader := applicationPlaybackOwnerFixture(t, ctx, pool, "revocation-reader")
	nowPlaying, err := store.ListNowPlayingSessionsForSubject(ctx, Subject{ApplicationCredentialID: reader.SessionID}, true,
		[]string{owner.ApplicationClientID})
	if err != nil || len(nowPlaying) != 0 {
		t.Fatalf("global now playing exposed revoked application playback: %+v, %v", nowPlaying, err)
	}
}

func TestApplicationKeyPlaybackSeparatesClientsAndRevokesTheirSharedCredential(t *testing.T) {
	ctx, pool, store, _, ids := playSessionFixture(t, 1)
	first := applicationPlaybackOwnerFixture(t, ctx, pool, "client-first")
	// Different client names on the same device still have distinct Sessions IDs.
	second := applicationPlaybackClientFixture(t, ctx, pool, first, "client-second", first.DeviceID)
	const reference = "same-key-same-device-reference"
	one := playReferencePrepare(t, ctx, store, first, ids[0], reference)
	two := playReferencePrepare(t, ctx, store, second, ids[0], reference)
	if one.ID == two.ID || one.ApplicationClientID == two.ApplicationClientID {
		t.Fatalf("same key clients shared a playback identity: %+v, %+v", one, two)
	}
	for _, test := range []struct {
		owner   PlaybackOwner
		foreign PlaySession
	}{{first, two}, {second, one}} {
		if _, err := store.GetPlaybackSession(ctx, test.owner, test.foreign.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("sibling client read canonical playback: %v", err)
		}
		if _, _, err := store.ReportPlayback(ctx, test.owner, PlaybackReport{PlaySessionID: test.foreign.ID, Event: "Progress"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("sibling client changed canonical playback: %v", err)
		}
	}
	preparedFirst := playSessionPrepare(t, ctx, store, first, ids[0], "")
	preparedSecond := playSessionPrepare(t, ctx, store, second, ids[0], "")
	if preparedFirst.ID == preparedSecond.ID {
		t.Fatal("source reuse collapsed distinct client contexts")
	}
	if reused := playSessionPrepare(t, ctx, store, first, ids[0], ""); reused.ID != preparedFirst.ID {
		t.Fatalf("source reuse lost its client context: %+v", reused)
	}
	for index, owner := range []PlaybackOwner{first, second} {
		playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: reference, Event: "Started"})
		playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: reference, Event: "Progress",
			PositionTicks: playSessionPosition(int64(index+1) * 30 * media.TicksPerSecond)})
	}
	clientIDs := []string{first.ApplicationClientID, second.ApplicationClientID}
	playing, err := store.ListNowPlayingSessionsForSubject(ctx, Subject{ApplicationCredentialID: first.SessionID}, true, clientIDs)
	if err != nil || len(playing) != 2 {
		t.Fatalf("same credential lost a now playing client: %+v, %v", playing, err)
	}
	want := map[string]int64{first.ApplicationClientID: 30 * media.TicksPerSecond, second.ApplicationClientID: 60 * media.TicksPerSecond}
	for _, play := range playing {
		if play.AuthSessionID != first.SessionID || play.PositionTicks != want[play.ApplicationClientID] || !play.ApplicationKey || play.UserID != "" {
			t.Fatalf("client now playing crossed ownership: %+v", play)
		}
		delete(want, play.ApplicationClientID)
	}
	if len(want) != 0 {
		t.Fatalf("missing application clients: %+v", want)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", first.SessionID); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []PlaybackOwner{first, second} {
		if _, _, err := store.ReportPlayback(ctx, owner, PlaybackReport{PlaySessionID: reference, Event: "Progress"}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("revoked parent credential left a client active: %v", err)
		}
	}
	reader := applicationPlaybackOwnerFixture(t, ctx, pool, "client-reader")
	playing, err = store.ListNowPlayingSessionsForSubject(ctx, Subject{ApplicationCredentialID: reader.SessionID}, true, clientIDs)
	if err != nil || len(playing) != 0 {
		t.Fatalf("revoked clients remained in now playing: %+v, %v", playing, err)
	}
	if counts := playReferenceCounts(t, ctx, pool); counts.userData != 0 || counts.references != 2 || counts.sessions != 4 {
		t.Fatalf("client playback changed user data or created extra plays: %+v", counts)
	}
}
