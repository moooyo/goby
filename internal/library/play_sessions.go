package library

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

type PlaybackOwner struct {
	UserID, SessionID, DeviceID string
	ApplicationClientID         string
	ApplicationKey              bool
}

type PlaySession struct {
	ID, UserID, AuthSessionID, DeviceID, ItemID, MediaSourceID, State string
	PositionTicks, DurationTicks                                      int64
	CreatedAt, UpdatedAt, ExpiresAt                                   time.Time
	StartedAt, StoppedAt                                              *time.Time
	PlayerState                                                       PlayerState
	ApplicationKey                                                    bool
	ApplicationClientID                                               string
	counted                                                           bool
	live                                                              bool
	clientCorrelated                                                  bool
}

// Event reports use database-lock processing order. Position may move backward for seeks;
// stopped sessions ignore all later events and cannot be revived by progress.
// Authentication identities and client-reported durations are not report fields.
type PlaybackReport struct {
	PlaySessionID, ItemID, MediaSourceID, Event string
	PositionTicks                               *int64
	IsPaused                                    bool
	PlayerState                                 *PlayerStateUpdate
}

const playSessionColumns = `id, user_id, auth_session_id, device_id, item_id, media_source_id, state,
	position_ticks, duration_ticks, created_at, updated_at, expires_at, started_at, stopped_at, player_state, counted, expires_at > clock_timestamp(), application_client_id, client_correlated`

func scanPlaySession(row rowScanner) (PlaySession, error) {
	var session PlaySession
	var rawPlayerState []byte
	var userID *string
	var applicationClientID *string
	err := row.Scan(&session.ID, &userID, &session.AuthSessionID, &session.DeviceID,
		&session.ItemID, &session.MediaSourceID, &session.State, &session.PositionTicks,
		&session.DurationTicks, &session.CreatedAt, &session.UpdatedAt, &session.ExpiresAt,
		&session.StartedAt, &session.StoppedAt, &rawPlayerState, &session.counted, &session.live, &applicationClientID, &session.clientCorrelated)
	if err != nil {
		return session, err
	}
	// Callers establish the credential kind before reading a playback owner.
	session.ApplicationKey = userID == nil
	if userID != nil {
		session.UserID = *userID
	}
	if session.ApplicationKey != (applicationClientID != nil) {
		return PlaySession{}, fmt.Errorf("%w: playback credential and client scopes are inconsistent", ErrUnavailable)
	}
	if applicationClientID != nil {
		session.ApplicationClientID = *applicationClientID
	}
	session.PlayerState, err = decodePlayerState(rawPlayerState)
	if err != nil {
		return PlaySession{}, err
	}
	return session, nil
}

func validPlaybackOwner(owner PlaybackOwner) bool {
	if owner.ApplicationKey && owner.UserID != "" || !owner.ApplicationKey && strings.TrimSpace(owner.UserID) == "" {
		return false
	}
	if !owner.ApplicationKey && owner.ApplicationClientID != "" {
		return false
	}
	identifiers := []string{owner.SessionID}
	if owner.ApplicationKey {
		identifiers = append(identifiers, owner.ApplicationClientID)
	}
	for _, value := range identifiers {
		if strings.TrimSpace(value) == "" || len(value) > 256 || strings.ContainsRune(value, '\x00') || !utf8.ValidString(value) {
			return false
		}
	}
	return len(owner.UserID) <= 256 && !strings.ContainsRune(owner.UserID, '\x00') && utf8.ValidString(owner.UserID) &&
		len(owner.DeviceID) <= 256 && !strings.ContainsRune(owner.DeviceID, '\x00') && utf8.ValidString(owner.DeviceID)
}

func (s *Store) beginPlaybackWrite(ctx context.Context, owner PlaybackOwner) (pgx.Tx, libraryAccess, error) {
	if !validPlaybackOwner(owner) {
		return nil, libraryAccess{}, ErrInvalidInput
	}
	subject := Subject{UserID: owner.UserID}
	if owner.ApplicationKey {
		subject.ApplicationCredentialID = owner.SessionID
	}
	tx, access, err := s.beginSubjectStateWrite(ctx, subject, true)
	if err != nil {
		return nil, libraryAccess{}, err
	}
	var sessionID string
	if owner.ApplicationKey {
		err = tx.QueryRow(ctx, `SELECT authentication.id FROM sessions authentication
			JOIN application_keys application ON application.credential_id = authentication.id
			JOIN application_key_clients client ON client.credential_id = authentication.id
			WHERE authentication.id = $1 AND authentication.user_id IS NULL AND client.id = $2 AND client.device_id = $3
			AND authentication.kind = 'application_key' AND authentication.revoked_at IS NULL
			FOR SHARE OF authentication, application, client`, owner.SessionID, owner.ApplicationClientID, owner.DeviceID).Scan(&sessionID)
	} else {
		err = tx.QueryRow(ctx, `SELECT authentication.id FROM sessions authentication JOIN users account ON account.id = authentication.user_id
		WHERE authentication.id = $1 AND authentication.user_id = $2 AND authentication.device_id = $3
		AND authentication.revoked_at IS NULL AND authentication.expires_at > clock_timestamp()
		AND (authentication.kind <> 'admin' OR account.is_administrator)
		FOR SHARE OF authentication`, owner.SessionID, owner.UserID, owner.DeviceID).Scan(&sessionID)
	}
	if err != nil {
		rollback(tx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, libraryAccess{}, ErrForbidden
		}
		return nil, libraryAccess{}, fmt.Errorf("authorize playback authentication session: %w", err)
	}
	return tx, access, nil
}

// Cleanup has its own short transaction and never locks user_item_data. This
// avoids reversing the report lock order (user data before a playback row).
// At most 256 terminal rows are deleted per call; active sessions are capped.
func (s *Store) cleanupPlayback(ctx context.Context, owner PlaybackOwner) error {
	tx, access, err := s.beginPlaybackWrite(ctx, owner)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `WITH expired AS (
		SELECT play.id FROM play_sessions play WHERE play.user_id IS NOT DISTINCT FROM NULLIF($1, '')
		AND (NOT $4::boolean OR (play.auth_session_id = $5 AND play.application_client_id = $6))
		AND play.state IN ('Prepared','Playing','Paused') AND (play.expires_at <= clock_timestamp() OR NOT EXISTS (
			SELECT 1 FROM sessions authentication LEFT JOIN users account ON account.id = authentication.user_id
			WHERE authentication.id = play.auth_session_id AND authentication.user_id IS NOT DISTINCT FROM play.user_id
			AND authentication.revoked_at IS NULL
			AND ((authentication.kind = 'application_key' AND play.user_id IS NULL
				AND EXISTS (SELECT 1 FROM application_keys application WHERE application.credential_id = authentication.id)
				AND EXISTS (SELECT 1 FROM application_key_clients client WHERE client.id = play.application_client_id
					AND client.credential_id = authentication.id AND client.device_id = play.device_id))
				OR (authentication.kind IN ('emby', 'admin') AND play.user_id IS NOT NULL AND play.application_client_id IS NULL
					AND authentication.device_id = play.device_id
					AND authentication.expires_at > clock_timestamp() AND NOT account.is_disabled
					AND (authentication.kind <> 'admin' OR account.is_administrator)))
		) OR NOT EXISTS (SELECT 1 FROM items i WHERE i.id = play.item_id
			AND ($2::boolean OR i.library_id = ANY($3::text[]))))
		ORDER BY play.expires_at, play.id LIMIT 256 FOR UPDATE OF play SKIP LOCKED
	) UPDATE play_sessions SET state = 'Expired', stopped_at = COALESCE(stopped_at, clock_timestamp()), updated_at = clock_timestamp()
	WHERE id IN (SELECT id FROM expired)`, owner.UserID, access.all, access.folders, owner.ApplicationKey, owner.SessionID, owner.ApplicationClientID); err != nil {
		return fmt.Errorf("expire abandoned playback sessions: %w", err)
	}
	if _, err := tx.Exec(ctx, `WITH excess AS (
		SELECT id FROM play_sessions WHERE user_id IS NOT DISTINCT FROM NULLIF($1, '')
		AND (NOT $2::boolean OR (auth_session_id = $3 AND application_client_id = $4)) AND state IN ('Stopped','Expired')
		ORDER BY created_at DESC, id DESC OFFSET 256
	), removable AS (
		SELECT id FROM play_sessions WHERE user_id IS NOT DISTINCT FROM NULLIF($1, '')
		AND (NOT $2::boolean OR (auth_session_id = $3 AND application_client_id = $4)) AND state IN ('Stopped','Expired')
		AND (expires_at < clock_timestamp() - interval '7 days' OR id IN (SELECT id FROM excess))
		ORDER BY created_at, id LIMIT 256 FOR UPDATE SKIP LOCKED
	) DELETE FROM play_sessions WHERE id IN (SELECT id FROM removable)`, owner.UserID, owner.ApplicationKey, owner.SessionID, owner.ApplicationClientID); err != nil {
		return fmt.Errorf("prune old playback sessions: %w", err)
	}
	if err := pruneInactiveClientPlaybackReferences(ctx, tx, owner); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func sourceForItem(itemID, requested string) (string, error) {
	expected := media.SourceID(itemID)
	if requested != "" && requested != expected {
		return "", ErrNotFound
	}
	return expected, nil
}

func clampPosition(position, duration int64) int64 {
	if position > duration {
		return duration
	}
	return position
}

func readOwnedCanonicalPlaySession(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, id string, lock bool) (PlaySession, error) {
	statement := "SELECT " + playSessionColumns + ` FROM play_sessions
		WHERE id = $1 AND user_id IS NOT DISTINCT FROM NULLIF($2, '') AND auth_session_id = $3 AND device_id = $4
		AND application_client_id IS NOT DISTINCT FROM NULLIF($5, '')`
	if lock {
		statement += " FOR UPDATE"
	}
	session, err := scanPlaySession(tx.QueryRow(ctx, statement, id, owner.UserID, owner.SessionID, owner.DeviceID, owner.ApplicationClientID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PlaySession{}, ErrNotFound
	}
	if err != nil {
		return PlaySession{}, fmt.Errorf("read owned playback session: %w", err)
	}
	return session, nil
}

func currentPlaySession(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, itemID, sourceID string, includeStopped bool) (PlaySession, error) {
	filter := "AND state IN ('Prepared','Playing','Paused')"
	if includeStopped {
		filter = ""
	}
	order := "created_at DESC, id DESC"
	if includeStopped {
		order = "CASE WHEN state IN ('Prepared','Playing','Paused') THEN 0 ELSE 1 END, " + order
	}
	session, err := scanPlaySession(tx.QueryRow(ctx, "SELECT "+playSessionColumns+` FROM play_sessions
		WHERE user_id IS NOT DISTINCT FROM NULLIF($1, '') AND auth_session_id = $2 AND device_id = $3 AND item_id = $4 AND media_source_id = $5
		AND application_client_id IS NOT DISTINCT FROM NULLIF($6, '')
		AND NOT client_correlated `+
		filter+" ORDER BY "+order+" LIMIT 1 FOR UPDATE", owner.UserID, owner.SessionID, owner.DeviceID, itemID, sourceID, owner.ApplicationClientID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PlaySession{}, ErrNotFound
	}
	return session, err
}

func createPlaySession(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, item stateItem, sourceID string, data UserData) (PlaySession, error) {
	return createPlaybackSession(ctx, tx, owner, item, sourceID, data, false)
}

func createPlaybackSession(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, item stateItem, sourceID string, data UserData, correlated bool) (PlaySession, error) {
	var authCount, userCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE auth_session_id = $2),
		count(*) FILTER (WHERE user_id = NULLIF($1, ''))
		FROM play_sessions WHERE (user_id = NULLIF($1, '') OR auth_session_id = $2)
		AND state IN ('Prepared','Playing','Paused') AND expires_at > clock_timestamp()`,
		owner.UserID, owner.SessionID).Scan(&authCount, &userCount); err != nil {
		return PlaySession{}, fmt.Errorf("check playback session capacity: %w", err)
	}
	if authCount >= 32 || userCount >= 128 {
		return PlaySession{}, ErrBusy
	}
	id, err := randomID()
	if err != nil {
		return PlaySession{}, err
	}
	// A purpose prefix makes it impossible to confuse this with an auth session.
	id = "play_" + id
	session, err := scanPlaySession(tx.QueryRow(ctx, `INSERT INTO play_sessions
		(id, user_id, auth_session_id, device_id, item_id, media_source_id, state, position_ticks, duration_ticks, expires_at, client_correlated, application_client_id)
		VALUES ($1,NULLIF($2,''),$3,$4,$5,$6,'Prepared',$7,$8,clock_timestamp() + interval '30 minutes',$9,NULLIF($10,'')) RETURNING `+playSessionColumns,
		id, owner.UserID, owner.SessionID, owner.DeviceID, item.id, sourceID, clampPosition(data.PlaybackPositionTicks, item.duration), item.duration, correlated, owner.ApplicationClientID))
	if err != nil {
		return PlaySession{}, fmt.Errorf("create playback session: %w", err)
	}
	return session, nil
}

func lockPlaybackCapacity(ctx context.Context, tx pgx.Tx, owner PlaybackOwner) error {
	// User quotas cover all of their credentials. Userless credentials have
	// independent buckets and never contend on an empty user identifier.
	capacityID := "user:" + owner.UserID
	if owner.ApplicationKey {
		capacityID = "credential:" + owner.SessionID
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(
		current_schema() || chr(31) || 'goby.playback.capacity' || chr(31) || $1, 0))`, capacityID)
	return err
}

func lockPlaybackUserData(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, itemID string) (UserData, error) {
	if owner.ApplicationKey {
		return UserData{}, nil
	}
	return lockUserData(ctx, tx, owner.UserID, itemID)
}

func activeOrNewPlayback(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, item stateItem, sourceID string, data UserData) (PlaySession, error) {
	// Select by active state without an expiry predicate, then decide while the
	// row is locked. A deadline crossing between SQL statements cannot hide an
	// active-state row that still occupies the unique source key.
	session, err := currentPlaySession(ctx, tx, owner, item.id, sourceID, false)
	if err == nil && session.live {
		return session, nil
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return PlaySession{}, err
	}
	if err == nil {
		if _, err := tx.Exec(ctx, `UPDATE play_sessions SET state = 'Expired',
			stopped_at = COALESCE(stopped_at, clock_timestamp()), updated_at = clock_timestamp() WHERE id = $1`, session.ID); err != nil {
			return PlaySession{}, err
		}
	}
	return createPlaySession(ctx, tx, owner, item, sourceID, data)
}

func (s *Store) PreparePlayback(ctx context.Context, owner PlaybackOwner, itemID, mediaSourceID, currentPlaySessionID string) (PlaySession, error) {
	return s.preparePlayback(ctx, owner, itemID, mediaSourceID, currentPlaySessionID, false)
}

func (s *Store) preparePlayback(ctx context.Context, owner PlaybackOwner, itemID, mediaSourceID, currentPlaySessionID string, createReference bool) (PlaySession, error) {
	if (currentPlaySessionID != "" || createReference) && !validClientPlaybackReference(currentPlaySessionID) {
		return PlaySession{}, ErrInvalidInput
	}
	if err := s.cleanupPlayback(ctx, owner); err != nil {
		return PlaySession{}, err
	}
	tx, access, err := s.beginPlaybackWrite(ctx, owner)
	if err != nil {
		return PlaySession{}, err
	}
	defer rollback(tx)
	if createReference {
		if err := requireCorrelatedPlaybackAuthentication(ctx, tx, owner); err != nil {
			return PlaySession{}, err
		}
	}
	item, err := lockStateItem(ctx, tx, access, itemID, true)
	if err != nil {
		return PlaySession{}, err
	}
	sourceID, err := sourceForItem(itemID, mediaSourceID)
	if err != nil {
		return PlaySession{}, err
	}
	if err := lockPlaybackCapacity(ctx, tx, owner); err != nil {
		return PlaySession{}, fmt.Errorf("lock playback session capacity: %w", err)
	}
	data, err := lockPlaybackUserData(ctx, tx, owner, itemID)
	if err != nil {
		return PlaySession{}, err
	}
	var session PlaySession
	if currentPlaySessionID != "" {
		session, err = readOwnedPlaySession(ctx, tx, owner, currentPlaySessionID, true)
		if errors.Is(err, ErrNotFound) && createReference && !strings.HasPrefix(currentPlaySessionID, "play_") {
			// A missing alias and a retained tombstone are different states. Only
			// the missing alias may reserve a new, independently counted play.
			_, exists, lookupErr := lookupClientPlaybackReference(ctx, tx, owner, currentPlaySessionID)
			if lookupErr != nil {
				return PlaySession{}, lookupErr
			}
			if !exists {
				session, err = createCorrelatedPlayback(ctx, tx, owner, item, sourceID, data, currentPlaySessionID)
			}
		}
		if err != nil || session.ItemID != itemID || session.MediaSourceID != sourceID || session.State == "Stopped" || session.State == "Expired" || !session.live {
			if err != nil {
				return PlaySession{}, err
			}
			return PlaySession{}, ErrNotFound
		}
	} else {
		session, err = activeOrNewPlayback(ctx, tx, owner, item, sourceID, data)
		if err != nil {
			return PlaySession{}, err
		}
	}
	session, err = scanPlaySession(tx.QueryRow(ctx, `UPDATE play_sessions SET updated_at = clock_timestamp(),
		expires_at = clock_timestamp() + interval '30 minutes',
		position_ticks = CASE WHEN state = 'Prepared' THEN LEAST(position_ticks, $2) ELSE position_ticks END,
		duration_ticks = CASE WHEN state = 'Prepared' THEN $2 ELSE duration_ticks END
		WHERE id = $1 RETURNING `+playSessionColumns, session.ID, item.duration))
	if err != nil {
		return PlaySession{}, fmt.Errorf("refresh prepared playback session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PlaySession{}, fmt.Errorf("commit prepared playback session: %w", err)
	}
	return session, nil
}

// GetPlaybackSession authorizes operations that require a live prepared or
// active session, such as future conversion jobs. Original-file reads use
// current token, account, library, and source authorization independently of
// playback state. Terminal sessions still accept idempotent event reports.
func (s *Store) GetPlaybackSession(ctx context.Context, owner PlaybackOwner, id string) (PlaySession, error) {
	tx, access, err := s.beginPlaybackWrite(ctx, owner)
	if err != nil {
		return PlaySession{}, err
	}
	defer rollback(tx)
	session, err := readOwnedPlaySession(ctx, tx, owner, id, false)
	if err != nil {
		return PlaySession{}, err
	}
	if _, err := lockStateItem(ctx, tx, access, session.ItemID, true); err != nil {
		return PlaySession{}, err
	}
	session, err = readOwnedPlaySession(ctx, tx, owner, id, true)
	if err != nil {
		return PlaySession{}, err
	}
	if _, err := sourceForItem(session.ItemID, session.MediaSourceID); err != nil {
		return PlaySession{}, err
	}
	if session.State == "Stopped" || session.State == "Expired" || !session.live {
		return PlaySession{}, ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return PlaySession{}, fmt.Errorf("complete playback session validation: %w", err)
	}
	return session, nil
}

// ListPlaybackSessions returns a bounded active-session view. The HTTP layer
// decides whether to request administrator scope; the database rechecks it.
func (s *Store) ListPlaybackSessions(ctx context.Context, ownerUserID string, administrator bool) ([]PlaySession, error) {
	return s.listPlaybackSessions(ctx, Subject{UserID: ownerUserID}, administrator, nil, false)
}

// ListNowPlayingSessionsForAuth returns the latest Playing/Paused session per
// selected authentication session. Prepared sessions cannot hide active media;
// empty input selects no sessions, not every session.
func (s *Store) ListNowPlayingSessionsForAuth(ctx context.Context, ownerUserID string, administrator bool, authSessionIDs []string) ([]PlaySession, error) {
	return s.ListNowPlayingSessionsForSubject(ctx, Subject{UserID: ownerUserID}, administrator, authSessionIDs)
}

// ListNowPlayingSessionsForSubject accepts normal authentication IDs and real
// application client IDs, preserving each client's independent now playing.
func (s *Store) ListNowPlayingSessionsForSubject(ctx context.Context, subject Subject, administrator bool, clientSessionIDs []string) ([]PlaySession, error) {
	if len(clientSessionIDs) > 256 {
		return nil, ErrInvalidInput
	}
	ids := make([]string, 0, len(clientSessionIDs))
	for _, id := range clientSessionIDs {
		if strings.TrimSpace(id) == "" || len(id) > 256 || strings.ContainsRune(id, '\x00') || !utf8.ValidString(id) {
			return nil, ErrInvalidInput
		}
		ids = append(ids, id)
	}
	return s.listPlaybackSessions(ctx, subject, administrator, ids, true)
}

func (s *Store) listPlaybackSessions(ctx context.Context, subject Subject, administrator bool, authSessionIDs []string, nowPlayingOnly bool) ([]PlaySession, error) {
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	if subject.ApplicationCredentialID != "" {
		administrator = true
	} else if administrator {
		var allowed bool
		if err := tx.QueryRow(ctx, "SELECT is_administrator FROM users WHERE id = $1", subject.UserID).Scan(&allowed); err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrForbidden
		}
	}
	columns := strings.Split(playSessionColumns, ",")
	for index, column := range columns {
		columns[index] = "play." + strings.TrimSpace(column)
	}
	filter := ""
	arguments := []any{administrator, subject.UserID, access.all, access.folders}
	if authSessionIDs != nil {
		filter = " AND COALESCE(play.application_client_id, play.auth_session_id) = ANY($5::text[])"
		arguments = append(arguments, authSessionIDs)
	}
	selection := "SELECT "
	states := "('Prepared','Playing','Paused')"
	if nowPlayingOnly {
		selection = "SELECT DISTINCT ON (play.auth_session_id, play.application_client_id) "
		states = "('Playing','Paused')"
	}
	statement := selection + strings.Join(columns, ",") + ` FROM play_sessions play
		JOIN sessions authentication ON authentication.id = play.auth_session_id AND authentication.user_id IS NOT DISTINCT FROM play.user_id
		LEFT JOIN application_key_clients client ON client.id = play.application_client_id
			AND client.credential_id = authentication.id AND client.device_id = play.device_id
		LEFT JOIN users account ON account.id = play.user_id JOIN items i ON i.id = play.item_id
		WHERE play.state IN ` + states + ` AND play.expires_at > clock_timestamp()
		AND authentication.revoked_at IS NULL
		AND ((authentication.kind = 'application_key' AND play.user_id IS NULL AND client.id IS NOT NULL
			AND EXISTS (SELECT 1 FROM application_keys application WHERE application.credential_id = authentication.id))
		OR (authentication.kind IN ('admin','emby') AND play.user_id IS NOT NULL AND play.application_client_id IS NULL
		AND authentication.device_id = play.device_id
		AND authentication.expires_at > clock_timestamp() AND NOT account.is_disabled
		AND (authentication.kind <> 'admin' OR account.is_administrator)
		AND jsonb_typeof(account.policy) = 'object'
		AND (NOT (account.policy ? 'EnableMediaPlayback') OR account.policy -> 'EnableMediaPlayback' = 'true'::jsonb)
		AND (account.is_administrator OR NOT (account.policy ? 'EnableAllFolders')
			OR account.policy -> 'EnableAllFolders' = 'true'::jsonb OR (
				account.policy -> 'EnableAllFolders' = 'false'::jsonb
				AND jsonb_typeof(account.policy -> 'EnabledFolders') = 'array'
				AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(CASE
					WHEN jsonb_typeof(account.policy -> 'EnabledFolders') = 'array' THEN account.policy -> 'EnabledFolders'
					ELSE '[]'::jsonb END) AS folder(value) WHERE jsonb_typeof(folder.value) <> 'string')
				AND (account.policy -> 'EnabledFolders') ? i.library_id
			))))
		AND ($1::boolean OR play.user_id = $2) AND ($3::boolean OR i.library_id = ANY($4::text[]))` + filter
	if nowPlayingOnly {
		statement += " ORDER BY play.auth_session_id, play.application_client_id, play.updated_at DESC, play.id DESC"
		statement = "SELECT * FROM (" + statement + ") AS current_playback ORDER BY updated_at DESC, id DESC LIMIT 256"
	} else {
		statement += " ORDER BY play.updated_at DESC, play.id LIMIT 256"
	}
	rows, err := tx.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list active playback sessions: %w", err)
	}
	defer rows.Close()
	result := make([]PlaySession, 0)
	for rows.Next() {
		session, err := scanPlaySession(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func canonicalPlaybackEvent(event string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "started":
		return "Started", nil
	case "progress":
		return "Progress", nil
	case "ping":
		return "Ping", nil
	case "stopped":
		return "Stopped", nil
	default:
		return "", ErrInvalidInput
	}
}

// stopPosition is Goby's explicit initial completion policy, using the observed
// reference defaults of 2% minimum, 90% maximum and 120 seconds minimum length.
// Progress reports keep their raw bounded position; normalization occurs on stop.
func stopPosition(position, duration int64) (int64, bool) {
	if duration <= 0 || position <= 0 {
		return 0, false
	}
	minimum := duration/100*2 + (duration%100*2+99)/100
	completion := duration/100*90 + (duration%100*90+99)/100
	if position >= completion {
		return 0, true
	}
	if duration < 120*media.TicksPerSecond || position < minimum {
		return 0, false
	}
	return position, false
}

func (s *Store) ReportPlayback(ctx context.Context, owner PlaybackOwner, report PlaybackReport) (PlaySession, UserData, error) {
	event, err := canonicalPlaybackEvent(report.Event)
	if err != nil || (report.PositionTicks != nil && *report.PositionTicks < 0) ||
		(report.PlaySessionID != "" && !validClientPlaybackReference(report.PlaySessionID)) {
		return PlaySession{}, UserData{}, ErrInvalidInput
	}
	playerStatePatch, err := encodePlayerStateUpdate(report.PlayerState)
	if err != nil {
		return PlaySession{}, UserData{}, err
	}
	if event == "Started" && report.PlaySessionID == "" {
		if err := s.cleanupPlayback(ctx, owner); err != nil {
			return PlaySession{}, UserData{}, err
		}
	}
	tx, access, err := s.beginPlaybackWrite(ctx, owner)
	if err != nil {
		return PlaySession{}, UserData{}, err
	}
	defer rollback(tx)
	var identified PlaySession
	if report.PlaySessionID != "" {
		identified, err = readOwnedPlaySession(ctx, tx, owner, report.PlaySessionID, false)
		if err != nil {
			return PlaySession{}, UserData{}, err
		}
		if report.ItemID != "" && report.ItemID != identified.ItemID {
			return PlaySession{}, UserData{}, ErrNotFound
		}
		report.PlaySessionID, report.ItemID = identified.ID, identified.ItemID
	}
	item, err := lockStateItem(ctx, tx, access, report.ItemID, true)
	if err != nil {
		return PlaySession{}, UserData{}, err
	}
	if report.PlaySessionID != "" {
		// Universal audio clients can report the item ID as their source ID.
		// Accept that alias only for an already owned correlated play and its
		// currently authorized Audio item; keep the stored source authoritative.
		if report.MediaSourceID != "" && report.MediaSourceID != identified.MediaSourceID &&
			!(identified.clientCorrelated && item.itemType == "Audio" && report.MediaSourceID == item.id) {
			return PlaySession{}, UserData{}, ErrNotFound
		}
		report.MediaSourceID = identified.MediaSourceID
	}
	sourceID, err := sourceForItem(report.ItemID, report.MediaSourceID)
	if err != nil {
		return PlaySession{}, UserData{}, err
	}
	if event == "Started" && report.PlaySessionID == "" {
		if err := lockPlaybackCapacity(ctx, tx, owner); err != nil {
			return PlaySession{}, UserData{}, err
		}
	}
	// Every event uses the same data-before-playback-row lock order.
	data, err := lockPlaybackUserData(ctx, tx, owner, report.ItemID)
	if err != nil {
		return PlaySession{}, UserData{}, err
	}
	var session PlaySession
	if report.PlaySessionID != "" {
		session, err = readOwnedPlaySession(ctx, tx, owner, report.PlaySessionID, true)
	} else if event == "Started" {
		session, err = activeOrNewPlayback(ctx, tx, owner, item, sourceID, data)
	} else {
		session, err = currentPlaySession(ctx, tx, owner, report.ItemID, sourceID, true)
	}
	if err != nil {
		return PlaySession{}, UserData{}, err
	}
	if session.State == "Stopped" || session.State == "Expired" {
		if err := tx.Commit(ctx); err != nil {
			return PlaySession{}, UserData{}, err
		}
		return session, data, nil
	}
	if !session.live {
		session, err = scanPlaySession(tx.QueryRow(ctx, `UPDATE play_sessions SET state = 'Expired',
			stopped_at = COALESCE(stopped_at, clock_timestamp()), updated_at = clock_timestamp() WHERE id = $1 RETURNING `+playSessionColumns, session.ID))
		if err != nil {
			return PlaySession{}, UserData{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return PlaySession{}, UserData{}, err
		}
		return session, data, nil
	}
	if event == "Started" && session.StartedAt != nil {
		if err := tx.Commit(ctx); err != nil {
			return PlaySession{}, UserData{}, err
		}
		return session, data, nil
	}
	position := clampPosition(session.PositionTicks, item.duration)
	if report.PositionTicks != nil && event != "Ping" {
		position = clampPosition(*report.PositionTicks, item.duration)
	}
	countNow := !session.counted && (event == "Started" || event == "Progress" || (event == "Stopped" && position > 0))
	counted := session.counted || countNow
	state := session.State
	if event == "Started" || event == "Progress" {
		state = "Playing"
		if report.IsPaused {
			state = "Paused"
		}
	} else if event == "Stopped" {
		state = "Stopped"
	}
	session, err = scanPlaySession(tx.QueryRow(ctx, `UPDATE play_sessions SET state = $2,
		position_ticks = $3, duration_ticks = $4, counted = $5,
		started_at = CASE WHEN $6 THEN COALESCE(started_at, clock_timestamp()) ELSE started_at END,
		stopped_at = CASE WHEN $2 = 'Stopped' THEN clock_timestamp() ELSE stopped_at END,
		expires_at = CASE WHEN $2 = 'Stopped' THEN clock_timestamp() ELSE clock_timestamp() + interval '30 minutes' END,
		player_state = CASE WHEN $7::boolean THEN player_state || $8::jsonb ELSE player_state END,
		updated_at = clock_timestamp() WHERE id = $1 RETURNING `+playSessionColumns,
		session.ID, state, position, item.duration, counted, countNow, report.PlayerState != nil && event != "Ping", playerStatePatch))
	if err != nil {
		return PlaySession{}, UserData{}, fmt.Errorf("persist playback report: %w", err)
	}
	if event != "Ping" && !owner.ApplicationKey {
		if countNow && data.PlayCount < math.MaxInt32 {
			data.PlayCount++
		}
		data.PlaybackPositionTicks = position
		if event == "Stopped" {
			var completed bool
			data.PlaybackPositionTicks, completed = stopPosition(position, item.duration)
			data.Played = data.Played || completed
		}
		data, err = scanUserData(tx.QueryRow(ctx, `UPDATE user_item_data SET playback_position_ticks = $3,
			play_count = $4, played = $5, last_played_at = CASE WHEN $6 THEN clock_timestamp() ELSE last_played_at END,
			updated_at = clock_timestamp() WHERE user_id = $1 AND item_id = $2 RETURNING `+userDataColumns,
			owner.UserID, item.id, data.PlaybackPositionTicks, data.PlayCount, data.Played, countNow))
		if err != nil {
			return PlaySession{}, UserData{}, fmt.Errorf("persist playback user data: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PlaySession{}, UserData{}, fmt.Errorf("commit playback report: %w", err)
	}
	return session, data, nil
}
