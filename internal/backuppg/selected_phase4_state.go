package backuppg

import (
	"context"
	"net/netip"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/notificationjournal"
	"github.com/moooyo/goby/internal/settings"
)

func validateSelectedPhase4State(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if version < 47 {
		return nil
	}
	if tx == nil {
		return ErrDatabase
	}
	var runtime []byte
	if err := tx.QueryRow(ctx, `SELECT runtime_overrides FROM managed_settings WHERE id=1`).Scan(&runtime); err != nil {
		return classifyResourceStateError(ctx, err)
	}
	validRuntime := settings.ValidateStoredRuntimeOverrides(runtime)
	if err := ctx.Err(); err != nil {
		return err
	}
	if validRuntime != nil {
		return ErrSchema
	}
	if version < 48 {
		return ctx.Err()
	}
	return validateNotificationState(ctx, tx)
}

// Raw recovery retains revoked/expired sessions and terminal delivery history.
// It checks durable linkage and bounded representations, never current network
// reachability, authority expiry, or availability of a delivery destination.
func validateNotificationState(ctx context.Context, tx pgx.Tx) error {
	var valid bool
	err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM notification_transport)=1 AND (SELECT count(*) FROM notification_journal_state)=1
		AND NOT EXISTS(SELECT 1 FROM notification_registrations registration JOIN sessions session ON session.id=registration.session_id
			WHERE session.kind<>'emby' OR session.user_id IS DISTINCT FROM registration.user_id
			OR session.device_id IS DISTINCT FROM registration.device_id OR registration.id !~ '^[0-9a-f]{32}$'
			OR registration.source_cursor>(SELECT sequence FROM notification_journal_state WHERE id=1))
		AND NOT EXISTS(SELECT 1 FROM notification_source_events event
			WHERE event.id !~ '^[0-9a-f]{32}$' OR event.sequence>(SELECT sequence FROM notification_journal_state WHERE id=1)
			OR (event.kind='CatalogInvalidated' AND event.resync AND jsonb_array_length(event.refs)=0))
		AND NOT EXISTS(SELECT 1 FROM notification_deliveries delivery JOIN notification_registrations registration ON registration.id=delivery.registration_id
			CROSS JOIN notification_transport transport
			WHERE delivery.id !~ '^[0-9a-f]{32}$' OR (delivery.lease_id<>'' AND delivery.lease_id !~ '^[0-9a-f]{32}$')
			OR delivery.registration_revision>registration.revision OR delivery.transport_revision>transport.revision
			OR delivery.source_sequence>(SELECT sequence FROM notification_journal_state WHERE id=1)
			OR (delivery.state IN ('pending','sending') AND (delivery.registration_revision<>registration.revision OR delivery.transport_revision<>transport.revision)))
		AND (SELECT count(*) FROM notification_registrations)<=512
		AND (SELECT count(*) FROM notification_registrations WHERE enabled)<=64
		AND NOT EXISTS(SELECT 1 FROM notification_registrations WHERE enabled GROUP BY user_id HAVING count(*)>4)
		AND (SELECT count(*) FROM notification_source_events)<=512
		AND (SELECT COALESCE(sum(octet_length(refs::text)),0) FROM notification_source_events)<=4194304
		AND (SELECT count(*) FROM notification_deliveries WHERE state IN ('pending','sending'))<=512
		AND (SELECT COALESCE(sum(octet_length(refs::text)),0) FROM notification_deliveries WHERE state IN ('pending','sending'))<=16777216
		AND NOT EXISTS(SELECT 1 FROM notification_deliveries WHERE state IN ('pending','sending') GROUP BY registration_id HAVING count(*)>8)`).Scan(&valid)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	if !valid {
		return ErrSchema
	}
	var endpoint string
	var networks []string
	var enabled, hasSecret bool
	err = tx.QueryRow(ctx, `SELECT endpoint,allowed_networks,enabled,credential_ciphertext IS NOT NULL FROM notification_transport WHERE id=1`).Scan(&endpoint, &networks, &enabled, &hasSecret)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	if notificationjournal.ValidateTransport(endpoint, networks, enabled, hasSecret) != nil {
		return ErrSchema
	}
	rows, err := tx.Query(ctx, `SELECT event_ids,peer_ip FROM notification_registrations ORDER BY id`)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	for rows.Next() {
		var events []string
		var peer string
		if err := rows.Scan(&events, &peer); err != nil {
			rows.Close()
			return classifyResourceStateError(ctx, err)
		}
		if notificationjournal.ValidateEvents(events) != nil {
			rows.Close()
			return ErrSchema
		}
		if peer != "" {
			address, err := netip.ParseAddr(peer)
			if err != nil || address.Is4In6() || address.Zone() != "" || address.String() != peer {
				rows.Close()
				return ErrSchema
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT kind,COALESCE(user_id,''),refs,false FROM notification_source_events
		UNION ALL SELECT kind,'',refs,true FROM notification_deliveries`)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, owner string
		var refs []byte
		var delivery bool
		if err := rows.Scan(&kind, &owner, &refs, &delivery); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		validRefs := notificationjournal.ValidateReferences(kind, owner, refs, delivery)
		if err := ctx.Err(); err != nil {
			return err
		}
		if validRefs != nil {
			return ErrSchema
		}
	}
	return classifyResourceStateError(ctx, rows.Err())
}
