package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const MaxUserActivityIDs = 200

// UserActivity contains recorded account activity and current watched-item
// state. PlayedItemCount counts completed movies and episodes once; it is not
// a count of individual playback sessions or a measured viewing duration.
type UserActivity struct {
	ID               string     `json:"Id"`
	LastLoginDate    *time.Time `json:"LastLoginDate"`
	LastActivityDate *time.Time `json:"LastActivityDate"`
	LastPlayedDate   *time.Time `json:"LastPlayedDate"`
	PlayedItemCount  int64      `json:"PlayedItemCount"`
}

type UserActivityPage struct {
	Items      []UserActivity `json:"Items"`
	ObservedAt time.Time      `json:"ObservedAt"`
}

// ValidateUserActivityIDs bounds the batch independently of HTTP parsing.
func ValidateUserActivityIDs(ids []string) error {
	if len(ids) == 0 || len(ids) > MaxUserActivityIDs {
		return ErrInvalidInput
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !validRevalidationID(id) || strings.Contains(id, ",") || seen[id] {
			return ErrInvalidInput
		}
		seen[id] = true
	}
	return nil
}

// ListUserActivity reads one administrator-authorized batch from one statement
// snapshot. Missing users are omitted; unknown recorded times remain null.
func (s *Store) ListUserActivity(ctx context.Context, actor Principal, ids []string) (UserActivityPage, error) {
	if err := ValidateUserActivityIDs(ids); err != nil {
		return UserActivityPage{}, err
	}
	if !validDeviceActor(actor, true) {
		return UserActivityPage{}, ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return UserActivityPage{}, fmt.Errorf("begin user activity read: %w", err)
	}
	defer rollback(tx)
	if err := CheckAdministrator(ctx, tx, actor, AdministratorNative, true); err != nil {
		return UserActivityPage{}, err
	}
	rows, err := tx.Query(ctx, `WITH observation AS MATERIALIZED (
		SELECT clock_timestamp() AS observed_at
	), playback AS (
		SELECT state.user_id, max(state.last_played_at) AS last_played_at,
			count(*) FILTER (WHERE state.played AND NOT i.is_folder
				AND i.type IN ('Movie', 'Episode')) AS played_item_count
		FROM user_item_data state JOIN items i ON i.id=state.item_id
		WHERE state.user_id=ANY($1::text[]) GROUP BY state.user_id
	)
	SELECT observation.observed_at, u.id, activity.last_login_at,
		activity.last_activity_at, playback.last_played_at,
		COALESCE(playback.played_item_count,0)
	FROM observation LEFT JOIN users u ON u.id=ANY($1::text[])
	LEFT JOIN user_activity activity ON activity.user_id=u.id
	LEFT JOIN playback ON playback.user_id=u.id ORDER BY u.id`, ids)
	if err != nil {
		return UserActivityPage{}, fmt.Errorf("query user activity: %w", err)
	}
	page := UserActivityPage{Items: make([]UserActivity, 0, len(ids))}
	for rows.Next() {
		var id pgtype.Text
		var login, active, played pgtype.Timestamptz
		var count int64
		if err := rows.Scan(&page.ObservedAt, &id, &login, &active, &played, &count); err != nil {
			rows.Close()
			return UserActivityPage{}, fmt.Errorf("read user activity: %w", err)
		}
		if id.Valid {
			page.Items = append(page.Items, UserActivity{ID: id.String,
				LastLoginDate: userActivityTime(login), LastActivityDate: userActivityTime(active),
				LastPlayedDate: userActivityTime(played), PlayedItemCount: count})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return UserActivityPage{}, fmt.Errorf("read user activity batch: %w", err)
	}
	if err := CheckAdministrator(ctx, tx, actor, AdministratorNative, false); err != nil {
		return UserActivityPage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserActivityPage{}, fmt.Errorf("complete user activity read: %w", err)
	}
	page.ObservedAt = page.ObservedAt.UTC()
	return page, nil
}

func userActivityTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	instant := value.Time.UTC()
	return &instant
}

// recordUserLoginActivity is called while the login issuer holds the account
// lock. Its write commits with the credential and audit record.
func recordUserLoginActivity(ctx context.Context, tx pgx.Tx, userID string, issuedAt time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO user_activity(user_id,last_login_at,last_activity_at)
		VALUES($1,$2,$2) ON CONFLICT(user_id) DO UPDATE SET
		last_login_at=GREATEST(user_activity.last_login_at,EXCLUDED.last_login_at),
		last_activity_at=GREATEST(user_activity.last_activity_at,EXCLUDED.last_activity_at)`, userID, issuedAt)
	if err != nil {
		return fmt.Errorf("record user login activity: %w", err)
	}
	return nil
}

// recordUserSessionActivity follows account and credential locks already held
// by the caller. The persisted credential supplies the timestamp; the caller's
// projected LastSeenAt and client metadata are never written back.
func recordUserSessionActivity(ctx context.Context, tx pgx.Tx, principal Principal) error {
	if principal.IsApplicationKey() || (principal.Kind != "admin" && principal.Kind != "emby") {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO user_activity(user_id,last_activity_at)
		SELECT user_id,last_seen_at FROM sessions
		WHERE id=$1 AND user_id=$2 AND kind=$3 AND revoked_at IS NULL
			AND expires_at>clock_timestamp()
		ON CONFLICT(user_id) DO UPDATE SET
		last_activity_at=GREATEST(user_activity.last_activity_at,EXCLUDED.last_activity_at)
		WHERE user_activity.last_activity_at IS NULL
			OR user_activity.last_activity_at<EXCLUDED.last_activity_at`, principal.SessionID, principal.User.ID, principal.Kind)
	if err != nil {
		return fmt.Errorf("record user session activity: %w", err)
	}
	return nil
}

// TouchAdministratorActivity samples an authenticated native request without
// extending credential lifetime. Lock ordering matches other identity writes:
// account, credential, then the independent activity summary.
func (s *Store) TouchAdministratorActivity(ctx context.Context, actor Principal) error {
	if !validDeviceActor(actor, true) {
		return ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin administrator activity: %w", err)
	}
	defer rollback(tx)
	if err := lockAdministratorActivity(ctx, tx, actor); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET last_seen_at=GREATEST(last_seen_at,clock_timestamp())
		WHERE id=$1 AND last_seen_at<=clock_timestamp()-($2::bigint*interval '1 second')`,
		actor.SessionID, int64(ClientSessionTouchInterval/time.Second)); err != nil {
		return fmt.Errorf("record administrator session activity: %w", err)
	}
	if err := recordUserSessionActivity(ctx, tx, actor); err != nil {
		return err
	}
	if err := CheckAdministrator(ctx, tx, actor, AdministratorNative, false); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit administrator activity: %w", err)
	}
	return nil
}

func lockAdministratorActivity(ctx context.Context, tx pgx.Tx, actor Principal) error {
	if err := CheckAdministrator(ctx, tx, actor, AdministratorNative, false); err != nil {
		return err
	}
	var id string
	if err := tx.QueryRow(ctx, "SELECT id FROM users WHERE id=$1 FOR SHARE", actor.User.ID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		return fmt.Errorf("lock administrator activity account: %w", err)
	}
	// Take the write lock immediately. Concurrent sampled requests must not
	// both retain a shared credential lock before attempting to upgrade it.
	if err := tx.QueryRow(ctx, `SELECT id FROM sessions
		WHERE id=$1 AND user_id=$2 AND kind='admin' FOR UPDATE`, actor.SessionID, actor.User.ID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		return fmt.Errorf("lock administrator activity credential: %w", err)
	}
	return CheckAdministrator(ctx, tx, actor, AdministratorNative, false)
}
