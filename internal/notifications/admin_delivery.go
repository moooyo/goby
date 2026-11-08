package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/notificationjournal"
)

type adminDelivery struct {
	id, destination, lease, owner string
	revision                      int64
	attempt                       int
	message                       AdminMessage
}

type adminDestinationAttempt struct {
	id       string
	owner    string
	revision int64
	cancel   context.CancelFunc
	done     chan struct{}
}

// The administrator queue has its own ownership limit. It never consumes or
// alters a personal registration's notification cursor or transport generation.
type adminDestinationRuntime struct {
	store    *Store
	options  RuntimeOptions
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex
	active   map[string]*adminDestinationAttempt
	claiming chan struct{}
}

func newAdminDestinationRuntime(parent context.Context, store *Store, options RuntimeOptions) *adminDestinationRuntime {
	ctx, cancel := context.WithCancel(parent)
	runtime := &adminDestinationRuntime{store: store, options: options, ctx: ctx, cancel: cancel, done: make(chan struct{}), active: map[string]*adminDestinationAttempt{}}
	go runtime.run()
	return runtime
}

func (runtime *adminDestinationRuntime) close() {
	if runtime == nil {
		return
	}
	runtime.cancel()
	<-runtime.done
}

func (runtime *adminDestinationRuntime) fence(ctx context.Context, id string, current int64) error {
	return runtime.fenceMatching(ctx, func(attempt *adminDestinationAttempt) bool {
		return attempt.id == id && (current == 0 || attempt.revision < current)
	})
}

func (runtime *adminDestinationRuntime) fenceUser(ctx context.Context, id string) error {
	return runtime.fenceMatching(ctx, func(attempt *adminDestinationAttempt) bool { return attempt.owner == id })
}

func (runtime *adminDestinationRuntime) fenceMatching(ctx context.Context, match func(*adminDestinationAttempt) bool) error {
	if runtime == nil {
		return nil
	}
	for {
		runtime.mu.Lock()
		claiming := runtime.claiming
		waiting := []<-chan struct{}{}
		for _, attempt := range runtime.active {
			if match(attempt) {
				attempt.cancel()
				waiting = append(waiting, attempt.done)
			}
		}
		runtime.mu.Unlock()
		if claiming != nil {
			select {
			case <-claiming:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		for _, done := range waiting {
			select {
			case <-done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
}

func (runtime *adminDestinationRuntime) run() {
	defer close(runtime.done)
	var workers sync.WaitGroup
	defer workers.Wait()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var nextPrune time.Time
	var nextAuthority time.Time
	for {
		select {
		case <-runtime.ctx.Done():
			return
		case <-ticker.C:
		}
		if time.Now().After(nextPrune) {
			ctx, cancel := context.WithTimeout(runtime.ctx, 3*time.Second)
			if runtime.store.pruneAdminDeliveries(ctx) == nil {
				nextPrune = time.Now().Add(time.Hour)
			}
			cancel()
		}
		if time.Now().After(nextAuthority) {
			ctx, cancel := context.WithTimeout(runtime.ctx, 3*time.Second)
			if runtime.store.retireAdminDestinationOwners(ctx) == nil {
				nextAuthority = time.Now().Add(5 * time.Second)
			}
			cancel()
		}
		for {
			runtime.mu.Lock()
			if len(runtime.active) >= 2 || runtime.ctx.Err() != nil {
				runtime.mu.Unlock()
				break
			}
			occupied := make([]string, 0, len(runtime.active))
			for id := range runtime.active {
				occupied = append(occupied, id)
			}
			runtime.claiming = make(chan struct{})
			runtime.mu.Unlock()
			ctx, cancel := context.WithTimeout(runtime.ctx, 3*time.Second)
			delivery, err := runtime.store.claimAdminDelivery(ctx, occupied)
			cancel()
			runtime.mu.Lock()
			var attempt *adminDestinationAttempt
			var deliveryContext context.Context
			if err == nil {
				var stop context.CancelFunc
				deliveryContext, stop = context.WithTimeout(runtime.ctx, 10*time.Second)
				attempt = &adminDestinationAttempt{id: delivery.destination, owner: delivery.owner, revision: delivery.revision, cancel: stop, done: make(chan struct{})}
				runtime.active[delivery.destination] = attempt
			}
			close(runtime.claiming)
			runtime.claiming = nil
			runtime.mu.Unlock()
			if err != nil {
				break
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() {
					attempt.cancel()
					runtime.mu.Lock()
					delete(runtime.active, delivery.destination)
					close(attempt.done)
					runtime.mu.Unlock()
				}()
				runtime.deliver(deliveryContext, delivery, attempt.cancel)
			}()
		}
	}
}

func (s *Store) retireAdminDestinationOwners(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT d.id FROM admin_notification_destinations d JOIN users u ON u.id=d.owner_user_id WHERE d.enabled AND (NOT u.is_administrator OR u.is_disabled) ORDER BY d.id LIMIT 16`)
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
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return ErrUnavailable
		}
		var locked string
		err = tx.QueryRow(ctx, `SELECT id FROM admin_notification_destinations WHERE id=$1 FOR UPDATE`, id).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			_ = tx.Rollback(ctx)
			continue
		}
		if err == nil {
			var changed bool
			err = tx.QueryRow(ctx, `WITH retired AS (UPDATE admin_notification_destinations d SET enabled=false,revision=CASE WHEN revision<9223372036854775807 THEN revision+1 ELSE revision END,last_outcome='authority_revoked',updated_at=clock_timestamp()
				WHERE d.id=$1 AND d.enabled AND EXISTS(SELECT 1 FROM users u WHERE u.id=d.owner_user_id AND (NOT u.is_administrator OR u.is_disabled)) RETURNING id) SELECT EXISTS(SELECT 1 FROM retired)`, id).Scan(&changed)
			if err == nil && changed {
				_, err = tx.Exec(ctx, `UPDATE admin_notification_deliveries SET state='cancelled',lease_id='',lease_until=NULL,outcome='authority_revoked',updated_at=clock_timestamp() WHERE destination_id=$1 AND state IN ('pending','sending')`, id)
			}
		}
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if err != nil {
			return ErrUnavailable
		}
	}
	return nil
}

func (s *Store) pruneAdminDeliveries(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM admin_notification_deliveries WHERE id IN (
		SELECT id FROM (SELECT id,created_at,row_number() OVER(PARTITION BY destination_id ORDER BY created_at DESC,id DESC) AS position
		FROM admin_notification_deliveries WHERE state NOT IN ('pending','sending')) q
		WHERE position>64 OR created_at<clock_timestamp()-interval '7 days' LIMIT 1024)`)
	return err
}

func (s *Store) claimAdminDelivery(ctx context.Context, occupied []string) (adminDelivery, error) {
	var delivery adminDelivery
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return delivery, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	// Destination-before-delivery lock ordering is shared by edits, triggers,
	// claims and completion. A live lease also fences other server processes.
	err = tx.QueryRow(ctx, `SELECT d.id,d.owner_user_id FROM admin_notification_destinations d JOIN users u ON u.id=d.owner_user_id
		WHERE d.enabled AND u.is_administrator AND NOT u.is_disabled AND d.id<>ALL($1::text[])
		AND NOT EXISTS(SELECT 1 FROM admin_notification_deliveries live WHERE live.destination_id=d.id AND live.state='sending' AND live.lease_until>clock_timestamp())
		AND EXISTS(SELECT 1 FROM admin_notification_deliveries q WHERE q.destination_id=d.id
			AND ((q.state='pending' AND q.due_at<=clock_timestamp()) OR (q.state='sending' AND q.lease_until<=clock_timestamp())))
		ORDER BY d.updated_at,d.id LIMIT 1 FOR UPDATE OF d SKIP LOCKED`, occupied).Scan(&delivery.destination, &delivery.owner)
	if err != nil {
		return delivery, err
	}
	var expired int
	err = tx.QueryRow(ctx, `WITH retired AS (UPDATE admin_notification_deliveries SET state='failed',outcome='delivery_unconfirmed',lease_id='',lease_until=NULL,updated_at=clock_timestamp()
		WHERE destination_id=$1 AND state='sending' AND lease_until<=clock_timestamp() RETURNING id) SELECT count(*) FROM retired`, delivery.destination).Scan(&expired)
	if err != nil {
		return delivery, ErrUnavailable
	}
	if expired > 0 {
		if _, err = tx.Exec(ctx, `UPDATE admin_notification_destinations SET last_outcome='delivery_unconfirmed' WHERE id=$1`, delivery.destination); err != nil {
			return delivery, ErrUnavailable
		}
	}
	var payload []byte
	err = tx.QueryRow(ctx, `SELECT q.id,q.destination_revision,q.attempts,q.payload FROM admin_notification_deliveries q
		JOIN admin_notification_destinations d ON d.id=q.destination_id AND d.revision=q.destination_revision
		WHERE q.destination_id=$1 AND q.state='pending' AND q.due_at<=clock_timestamp()
		AND NOT EXISTS(SELECT 1 FROM admin_notification_deliveries active WHERE active.destination_id=q.destination_id AND active.state='sending')
		ORDER BY q.created_at,q.id LIMIT 1 FOR UPDATE OF q SKIP LOCKED`, delivery.destination).Scan(&delivery.id, &delivery.revision, &delivery.attempt, &payload)
	if errors.Is(err, pgx.ErrNoRows) && expired > 0 {
		if tx.Commit(ctx) != nil {
			return delivery, ErrUnavailable
		}
		return delivery, pgx.ErrNoRows
	}
	if err != nil {
		return delivery, err
	}
	if len(payload) > 4096 || json.Unmarshal(payload, &delivery.message) != nil || delivery.message.EventID != delivery.id {
		return delivery, ErrInvalid
	}
	delivery.lease = notificationjournal.NewID()
	delivery.attempt++
	if _, err = tx.Exec(ctx, `UPDATE admin_notification_deliveries SET state='sending',attempts=attempts+1,lease_id=$2,lease_until=clock_timestamp()+interval '60 seconds',updated_at=clock_timestamp() WHERE id=$1`, delivery.id, delivery.lease); err != nil {
		return delivery, ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE admin_notification_destinations SET last_outcome='sending' WHERE id=$1`, delivery.destination); err != nil {
		return delivery, ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return delivery, ErrUnavailable
	}
	return delivery, nil
}

func (s *Store) currentAdminDelivery(ctx context.Context, delivery adminDelivery) (storedAdminDestination, error) {
	result, err := scanAdminDestination(s.pool.QueryRow(ctx, `SELECT `+adminDestinationProjection+` FROM admin_notification_destinations d
		JOIN users u ON u.id=d.owner_user_id WHERE d.id=$1 AND d.revision=$2 AND d.enabled AND u.is_administrator AND NOT u.is_disabled
		AND EXISTS(SELECT 1 FROM admin_notification_deliveries q WHERE q.id=$3 AND q.destination_id=d.id AND q.destination_revision=d.revision AND q.state='sending' AND q.lease_id=$4 AND q.lease_until>clock_timestamp())`, delivery.destination, delivery.revision, delivery.id, delivery.lease))
	if err != nil {
		return result, adminDestinationQueryError(err)
	}
	return result, nil
}

func (s *Store) checkAdminDelivery(ctx context.Context, delivery adminDelivery) error {
	var permitted bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_notification_destinations d JOIN users u ON u.id=d.owner_user_id
		JOIN admin_notification_deliveries q ON q.destination_id=d.id WHERE d.id=$1 AND d.revision=$2 AND d.enabled
		AND u.is_administrator AND NOT u.is_disabled AND q.id=$3 AND q.destination_revision=d.revision AND q.state='sending' AND q.lease_id=$4 AND q.lease_until>clock_timestamp())`, delivery.destination, delivery.revision, delivery.id, delivery.lease).Scan(&permitted)
	if err != nil {
		return ErrUnavailable
	}
	if !permitted {
		return identity.ErrUnauthorized
	}
	return nil
}

func (runtime *adminDestinationRuntime) deliver(ctx context.Context, delivery adminDelivery, cancel context.CancelFunc) {
	destination, err := runtime.store.currentAdminDelivery(ctx, delivery)
	if err != nil {
		runtime.finish(delivery, adminDestinationSafeError(err))
		return
	}
	secret := ""
	if destination.HasSecret {
		envelope, openErr := runtime.store.users.OpenNotificationSecret(ctx, identity.NotificationReceiverPurpose, adminSecretBinding(destination.Id, destination.secretGeneration), destination.sealed)
		if openErr == nil {
			secret, openErr = decodeAdminSecret(envelope)
		}
		if openErr != nil {
			runtime.finish(delivery, "secret_unavailable")
			return
		}
	}
	watchDone := make(chan struct{})
	var watchErr error
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				check, stop := context.WithTimeout(ctx, time.Second)
				err := runtime.store.checkAdminDelivery(check, delivery)
				stop()
				if err != nil {
					if ctx.Err() == nil {
						watchErr = err
						cancel()
					}
					return
				}
			}
		}
	}()
	authorize := func(check context.Context) error { return runtime.store.checkAdminDelivery(check, delivery) }
	var outcome string
	if destination.Transport == "smtp" {
		outcome = sendAdminSMTP(ctx, destination.SMTP, destination.AllowedNetworks, secret, delivery.message, runtime.options, authorize)
	} else {
		outcome = postAdminWebhook(ctx, destination.AdminDestination, secret, delivery.message, runtime.options, authorize)
	}
	cancel()
	<-watchDone
	if outcome != "delivered" && outcome != "delivery_unconfirmed" && watchErr != nil && errors.Is(watchErr, identity.ErrUnauthorized) {
		outcome = "authority_revoked"
	}
	runtime.finish(delivery, outcome)
}

func (runtime *adminDestinationRuntime) finish(delivery adminDelivery, outcome string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = runtime.store.finishAdminDelivery(ctx, delivery, outcome)
}

func (s *Store) finishAdminDelivery(ctx context.Context, delivery adminDelivery, outcome string) error {
	state := "failed"
	delay := time.Duration(0)
	switch outcome {
	case "delivered":
		state = "delivered"
	case "authority_revoked", "cancelled":
		state = "cancelled"
	case "retry":
		if delivery.attempt < 3 {
			state = "pending"
			delay = time.Duration(1<<uint(delivery.attempt)) * time.Second
		} else {
			outcome = "retry_exhausted"
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var current int64
	if err = tx.QueryRow(ctx, `SELECT revision FROM admin_notification_destinations WHERE id=$1 FOR UPDATE`, delivery.destination).Scan(&current); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return ErrUnavailable
	}
	if current != delivery.revision {
		return nil
	}
	changed, err := tx.Exec(ctx, `UPDATE admin_notification_deliveries SET state=$3,outcome=$4,lease_id='',lease_until=NULL,due_at=clock_timestamp()+($5::bigint*interval '1 millisecond'),updated_at=clock_timestamp() WHERE id=$1 AND lease_id=$2 AND state='sending'`, delivery.id, delivery.lease, state, outcome, delay.Milliseconds())
	if err != nil {
		return ErrUnavailable
	}
	if changed.RowsAffected() == 1 {
		if _, err = tx.Exec(ctx, `UPDATE admin_notification_destinations SET last_outcome=$2,last_delivered_at=CASE WHEN $2='delivered' THEN clock_timestamp() ELSE last_delivered_at END WHERE id=$1`, delivery.destination, outcome); err != nil {
			return ErrUnavailable
		}
	}
	return tx.Commit(ctx)
}

func postAdminWebhook(ctx context.Context, destination AdminDestination, secret string, message AdminMessage, options RuntimeOptions, authorize func(context.Context) error) string {
	transport, closeTransport, err := webhookTransport(target{endpoint: destination.Endpoint, networks: destination.AllowedNetworks}, options, ctx, authorize)
	if err != nil {
		return "endpoint_invalid"
	}
	defer closeTransport()
	raw, err := json.Marshal(message)
	if err != nil || len(raw) > 4096 {
		return "request_rejected"
	}
	var wrote atomic.Bool
	trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { wrote.Store(true) }}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodPost, destination.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return "endpoint_invalid"
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Goby-Notification-Version", "1")
	request.Header.Set("X-Goby-Timestamp", timestamp)
	request.Header.Set("X-Goby-Signature", Signature(secret, timestamp, raw))
	request.Header.Set("Idempotency-Key", message.EventID)
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		if wrote.Load() {
			return "delivery_unconfirmed"
		}
		if errors.Is(err, identity.ErrUnauthorized) {
			return "authority_revoked"
		}
		if ctx.Err() != nil {
			return "cancelled"
		}
		return "retry"
	}
	defer response.Body.Close()
	// A generic webhook acknowledges by its HTTP status. It does not have the
	// personal receiver protocol's JSON acknowledgement or target-token header.
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return "delivered"
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return "retry"
	}
	return "request_rejected"
}
