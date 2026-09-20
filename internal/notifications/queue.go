package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/notificationjournal"
)

const queueLock int64 = 4919415424202458298

func lockJournalControl(ctx context.Context, tx pgx.Tx) error {
	var id int
	if tx.QueryRow(ctx, `SELECT id FROM notification_journal_state WHERE id=1 FOR UPDATE`).Scan(&id) != nil {
		return ErrUnavailable
	}
	return nil
}

type target struct {
	id, session, user, device, peer, endpoint                          string
	regRevision, configRevision, tokenGeneration, credentialGeneration int64
	networks                                                           []string
	token, credential                                                  []byte
}

func (t target) principal() identity.Principal {
	return identity.Principal{Kind: "emby", SessionID: t.session, User: identity.User{ID: t.user}, PeerIP: t.peer}
}
func readTarget(ctx context.Context, tx pgx.Tx, id string) (target, error) {
	var t target
	err := tx.QueryRow(ctx, `SELECT r.id,r.session_id,r.user_id,r.device_id,r.peer_ip,r.revision,c.revision,r.token_generation,c.credential_generation,c.endpoint,c.allowed_networks,r.token_ciphertext,c.credential_ciphertext
	FROM notification_registrations r CROSS JOIN notification_transport c JOIN sessions s ON true JOIN users u ON u.id=s.user_id
	WHERE r.id=$1 AND c.id=1 AND r.enabled AND c.enabled AND s.id=r.session_id AND s.user_id=r.user_id AND s.device_id=r.device_id AND s.kind='emby' AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND NOT u.is_disabled`, id).Scan(&t.id, &t.session, &t.user, &t.device, &t.peer, &t.regRevision, &t.configRevision, &t.tokenGeneration, &t.credentialGeneration, &t.endpoint, &t.networks, &t.token, &t.credential)
	return t, err
}
func (s *Store) currentTarget(ctx context.Context, id string) (target, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return target{}, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	t, err := readTarget(ctx, tx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return target{}, identity.ErrUnauthorized
		}
		return target{}, ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return target{}, ErrUnavailable
	}
	return t, nil
}
func (s *Store) authorizedRefs(ctx context.Context, t target, refs []notificationjournal.Reference) ([]notificationjournal.Reference, error) {
	fresh, err := s.users.RevalidateSession(ctx, t.principal())
	if err != nil {
		return nil, err
	}
	return s.catalog.FilterNotificationReferences(ctx, library.Subject{UserID: t.user, Actor: &fresh}, refs)
}

// Fanout advances one registration cursor and its delivery rows atomically.
// Hidden-only changes update only the private cursor, never public status.
func (s *Store) fanout(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT r.id FROM notification_registrations r CROSS JOIN notification_transport c WHERE r.enabled AND c.enabled ORDER BY r.id LIMIT 64`)
	if err != nil {
		return ErrUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return ErrUnavailable
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrUnavailable
	}
	for _, id := range ids {
		if err := s.fanoutRegistration(ctx, id); err != nil && !errors.Is(err, identity.ErrUnauthorized) {
			return err
		}
	}
	return nil
}
func (s *Store) fanoutRegistration(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var cursor int64
	var events []string
	if err = tx.QueryRow(ctx, `SELECT source_cursor,event_ids FROM notification_registrations WHERE id=$1 AND enabled`, id).Scan(&cursor, &events); err != nil {
		return nil
	}
	t, err := readTarget(ctx, tx, id)
	if err != nil {
		return nil
	}
	if _, err = s.users.RevalidateSession(ctx, t.principal()); err != nil {
		if errors.Is(err, identity.ErrUnauthorized) {
			if _, err = tx.Exec(ctx, `UPDATE notification_registrations SET enabled=false WHERE id=$1`, id); err != nil {
				return ErrUnavailable
			}
			return tx.Commit(ctx)
		}
		return ErrUnavailable
	}
	rows, err := tx.Query(ctx, `SELECT sequence,kind,COALESCE(user_id,''),refs,recursive,resync FROM notification_source_events WHERE sequence>$1 ORDER BY sequence LIMIT 16`, cursor)
	if err != nil {
		return ErrUnavailable
	}
	type source struct {
		seq               int64
		kind, user        string
		raw               []byte
		recursive, resync bool
	}
	sources := []source{}
	for rows.Next() {
		var v source
		if rows.Scan(&v.seq, &v.kind, &v.user, &v.raw, &v.recursive, &v.resync) != nil {
			rows.Close()
			return ErrUnavailable
		}
		sources = append(sources, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return ErrUnavailable
	}
	beforeCursor := cursor
	type projected struct {
		sequence  int64
		kind      string
		refs      []notificationjournal.Reference
		recursive bool
	}
	ready := []projected{}
	for _, v := range sources {
		cursor = v.seq
		selected := false
		for _, e := range events {
			selected = selected || e == v.kind
		}
		if !selected || v.user != "" && v.user != t.user {
			continue
		}
		if notificationjournal.ValidateReferences(v.kind, v.user, v.raw, false) != nil {
			return ErrInvalid
		}
		refs, err := decodeReferences(v.raw)
		if err != nil {
			return err
		}
		refs, err = s.authorizedRefs(ctx, t, refs)
		if err != nil {
			return err
		}
		if len(refs) == 0 && !(v.kind == "UserDataInvalidated" && v.resync && v.user == t.user) {
			continue
		}
		kind := v.kind
		if v.resync {
			kind = "ResyncRequired"
		}
		ready = append(ready, projected{v.seq, kind, refs, v.recursive})
	}
	tx, err = s.pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var currentCursor, currentRevision int64
	if err = tx.QueryRow(ctx, `SELECT source_cursor,revision FROM notification_registrations WHERE id=$1 AND enabled FOR UPDATE`, id).Scan(&currentCursor, &currentRevision); err != nil {
		return nil
	}
	if currentCursor != beforeCursor || currentRevision != t.regRevision {
		return nil
	}
	current, err := readTarget(ctx, tx, id)
	if err != nil || current.configRevision != t.configRevision {
		return nil
	}
	for _, item := range ready {
		if err = s.enqueue(ctx, tx, t, item.sequence, item.kind, item.refs, item.recursive); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE notification_registrations SET source_cursor=$2 WHERE id=$1`, id, cursor); err != nil {
		return ErrUnavailable
	}
	return tx.Commit(ctx)
}
func (s *Store) enqueue(ctx context.Context, tx pgx.Tx, t target, sequence int64, kind string, refs []notificationjournal.Reference, recursive bool) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, queueLock); err != nil {
		return ErrUnavailable
	}
	var count, total, bytes int
	if tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE registration_id=$1),count(*),COALESCE(sum(octet_length(refs::text)),0) FROM notification_deliveries WHERE state IN ('pending','sending')`, t.id).Scan(&count, &total, &bytes) != nil {
		return ErrUnavailable
	}
	if count >= 8 {
		if kind == "Test" {
			return ErrLimit
		}
		rows, err := tx.Query(ctx, `SELECT refs FROM notification_deliveries WHERE registration_id=$1 AND state='pending' ORDER BY created_at,id FOR UPDATE`, t.id)
		if err != nil {
			return ErrUnavailable
		}
		combined := append([]notificationjournal.Reference{}, refs...)
		for rows.Next() {
			var raw []byte
			if rows.Scan(&raw) != nil {
				rows.Close()
				return ErrUnavailable
			}
			prior, err := decodeReferences(raw)
			if err != nil {
				rows.Close()
				return err
			}
			combined = append(combined, prior...)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ErrUnavailable
		}
		seen := map[notificationjournal.Reference]bool{}
		refs = []notificationjournal.Reference{}
		for _, ref := range combined {
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
		if len(refs) > 4096 {
			return ErrLimit
		}
		kind = "ResyncRequired"
		recursive = true
		if _, err = tx.Exec(ctx, `UPDATE notification_deliveries SET state='cancelled',outcome='coalesced',refs='[]',updated_at=clock_timestamp() WHERE registration_id=$1 AND state='pending'`, t.id); err != nil {
			return ErrUnavailable
		}
		if tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum(octet_length(refs::text)),0) FROM notification_deliveries WHERE state IN ('pending','sending')`).Scan(&total, &bytes) != nil {
			return ErrUnavailable
		}
	}
	raw, err := json.Marshal(refs)
	if err != nil || len(raw) > 524288 {
		return ErrLimit
	}
	var incomingBytes int
	if tx.QueryRow(ctx, `SELECT octet_length($1::jsonb::text)`, raw).Scan(&incomingBytes) != nil {
		return ErrUnavailable
	}
	if total >= 512 || incomingBytes > 524288 || bytes+incomingBytes > 16*1024*1024 {
		return ErrLimit
	}
	_, err = tx.Exec(ctx, `INSERT INTO notification_deliveries(id,registration_id,registration_revision,transport_revision,source_sequence,kind,refs,recursive) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8) ON CONFLICT(registration_id,registration_revision,source_sequence,kind) DO NOTHING`, notificationjournal.NewID(), t.id, t.regRevision, t.configRevision, sequence, kind, raw, recursive)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *Store) EnqueueTest(ctx context.Context, actor identity.Principal) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if err = identity.CheckNotificationSession(ctx, tx, actor, true); err != nil {
		return err
	}
	if err = lockJournalControl(ctx, tx); err != nil {
		return err
	}
	var id string
	if tx.QueryRow(ctx, `SELECT id FROM notification_registrations WHERE session_id=$1 AND enabled FOR UPDATE`, actor.SessionID).Scan(&id) != nil {
		return ErrUnavailable
	}
	t, err := readTarget(ctx, tx, id)
	if err != nil {
		return ErrUnavailable
	}
	var pending bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notification_deliveries WHERE registration_id=$1 AND kind='Test' AND created_at>clock_timestamp()-interval '1 minute')`, id).Scan(&pending) != nil {
		return ErrUnavailable
	}
	if pending {
		return ErrLimit
	}
	var seq int64
	if tx.QueryRow(ctx, `UPDATE notification_journal_state SET sequence=sequence+1 WHERE id=1 RETURNING sequence`).Scan(&seq) != nil {
		return ErrUnavailable
	}
	if err = s.enqueue(ctx, tx, t, seq, "Test", []notificationjournal.Reference{}, false); err != nil {
		return err
	}
	if err = identity.CheckNotificationSession(ctx, tx, actor, false); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type delivery struct {
	id, registration, kind, lease string
	regRevision, configRevision   int64
	refs                          []notificationjournal.Reference
	recursive                     bool
	attempt                       int
	created                       time.Time
}

func (s *Store) claim(ctx context.Context) (delivery, error) {
	var d delivery
	var raw []byte
	d.lease = notificationjournal.NewID()
	err := s.pool.QueryRow(ctx, `WITH chosen AS(SELECT d.id FROM notification_deliveries d JOIN notification_registrations r ON r.id=d.registration_id JOIN notification_transport c ON c.id=1 WHERE d.state='pending' AND d.due_at<=clock_timestamp() AND r.enabled AND c.enabled AND d.registration_revision=r.revision AND d.transport_revision=c.revision AND NOT EXISTS(SELECT 1 FROM notification_deliveries earlier WHERE earlier.registration_id=d.registration_id AND earlier.state IN ('pending','sending') AND (earlier.source_sequence,earlier.id)<(d.source_sequence,d.id)) ORDER BY d.source_sequence,d.id LIMIT 1 FOR UPDATE OF d SKIP LOCKED)
	UPDATE notification_deliveries d SET state='sending',attempts=attempts+1,lease_id=$1,lease_until=clock_timestamp()+interval '30 seconds',updated_at=clock_timestamp() FROM chosen WHERE d.id=chosen.id AND d.attempts<5 RETURNING d.id,d.registration_id,d.kind,d.registration_revision,d.transport_revision,d.refs,d.recursive,d.attempts,d.created_at`, d.lease).Scan(&d.id, &d.registration, &d.kind, &d.regRevision, &d.configRevision, &raw, &d.recursive, &d.attempt, &d.created)
	if err != nil {
		return d, err
	}
	if notificationjournal.ValidateReferences(d.kind, "", raw, true) != nil {
		return d, ErrInvalid
	}
	d.refs, err = decodeReferences(raw)
	return d, err
}
func (s *Store) finish(ctx context.Context, d delivery, state, code string, delay time.Duration) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var owner string
	if err = tx.QueryRow(ctx, `SELECT id FROM notification_registrations WHERE id=$1 FOR UPDATE`, d.registration).Scan(&owner); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE notification_deliveries SET state=$3,outcome=$4,lease_id='',lease_until=NULL,due_at=clock_timestamp()+$5::bigint*interval '1 millisecond',refs=CASE WHEN $3='pending' THEN refs ELSE '[]'::jsonb END,updated_at=clock_timestamp() WHERE id=$1 AND lease_id=$2 AND state='sending'`, d.id, d.lease, state, code, delay.Milliseconds())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return tx.Commit(ctx)
	}
	if state != "suppressed" && code != "authority_revoked" && code != "source_unavailable" {
		if _, err = tx.Exec(ctx, `UPDATE notification_registrations SET last_outcome=$2,updated_at=clock_timestamp() WHERE id=$1 AND revision=$3`, d.registration, code, d.regRevision); err != nil {
			return err
		}
	}
	if code == "target_invalid" {
		if _, err = tx.Exec(ctx, `UPDATE notification_registrations SET enabled=false,revision=revision+1 WHERE id=$1 AND revision=$2`, d.registration, d.regRevision); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE notification_deliveries SET state='cancelled',outcome='target_invalid',refs='[]',lease_id='',lease_until=NULL,updated_at=clock_timestamp() WHERE registration_id=$1 AND state IN ('pending','sending')`, d.registration); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) maintain(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE notification_registrations r SET enabled=false WHERE r.enabled AND NOT EXISTS(SELECT 1 FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id=r.session_id AND s.kind='emby' AND s.user_id=r.user_id AND s.device_id=r.device_id AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND NOT u.is_disabled);
	UPDATE notification_deliveries d SET state='cancelled',lease_id='',lease_until=NULL,refs='[]',outcome='authority_revoked',updated_at=clock_timestamp() WHERE state IN ('pending','sending') AND NOT EXISTS(SELECT 1 FROM notification_registrations r CROSS JOIN notification_transport c WHERE r.id=d.registration_id AND r.enabled AND c.enabled AND r.revision=d.registration_revision AND c.revision=d.transport_revision);
	UPDATE notification_deliveries SET state='failed',lease_id='',lease_until=NULL,refs='[]',outcome='retry_exhausted',updated_at=clock_timestamp() WHERE (state='pending' AND (attempts>=5 OR created_at<clock_timestamp()-interval '24 hours')) OR (state='sending' AND lease_until<clock_timestamp() AND (attempts>=5 OR created_at<clock_timestamp()-interval '24 hours'));
	UPDATE notification_deliveries SET state='pending',lease_id='',lease_until=NULL WHERE state='sending' AND lease_until<clock_timestamp();
	DELETE FROM notification_deliveries WHERE id IN(SELECT id FROM(SELECT id,row_number() OVER(PARTITION BY registration_id ORDER BY created_at DESC,id DESC) AS ordinal FROM notification_deliveries WHERE state NOT IN('pending','sending')) history WHERE ordinal>32);
	DELETE FROM notification_registrations WHERE NOT enabled AND updated_at<clock_timestamp()-interval '7 days';
	DELETE FROM notification_source_events WHERE sequence<=COALESCE((SELECT min(source_cursor) FROM notification_registrations WHERE enabled),9223372036854775807)`)
	return err
}

func (t target) targetBinding() string {
	return identity.NotificationSecretBinding(t.id, t.session, t.user, t.device, strconv.FormatInt(t.tokenGeneration, 10))
}
