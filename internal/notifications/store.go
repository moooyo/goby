// Package notifications delivers the explicitly supported GobyWebhookV1
// protocol. It does not implement vendor push or remote playback commands.
package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/netip"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/notificationjournal"
)

const Transport = "GobyWebhookV1"

var (
	ErrInvalid     = errors.New("invalid notification input")
	ErrConflict    = errors.New("notification revision changed")
	ErrUnavailable = errors.New("notification transport unavailable")
	ErrLimit       = errors.New("notification capacity reached")
)

type Config struct {
	Revision              string
	Enabled               bool
	Endpoint              string
	AllowedNetworks       []string
	HasReceiverCredential bool
	SupportedEvents       []string
	PendingCount          int
}
type ConfigUpdate struct {
	Revision           string
	Enabled            bool
	Endpoint           string
	AllowedNetworks    []string
	ReceiverCredential *string
}
type Registration struct {
	Id             string
	Revision       string
	Transport      string
	Enabled        bool
	EventIds       []string
	HasTargetToken bool
	LastOutcome    string
}
type RegistrationUpdate struct {
	Revision    string
	Transport   string
	TargetToken *string
	EventIds    []string
}
type Store struct {
	pool    *pgxpool.Pool
	users   *identity.Store
	catalog *library.Store
}

func NewStore(pool *pgxpool.Pool, users *identity.Store, catalog *library.Store) *Store {
	return &Store{pool: pool, users: users, catalog: catalog}
}
func revision(value string, zero bool) (int64, error) {
	v, e := strconv.ParseInt(value, 10, 64)
	if e != nil || v < 0 || v == math.MaxInt64 || !zero && v == 0 || strconv.FormatInt(v, 10) != value {
		return 0, ErrInvalid
	}
	return v, nil
}
func validateConfig(update ConfigUpdate) error {
	if _, err := revision(update.Revision, false); err != nil {
		return err
	}
	if update.ReceiverCredential != nil && *update.ReceiverCredential != "" && !identity.ValidNotificationSecret(*update.ReceiverCredential) {
		return ErrInvalid
	}
	if notificationjournal.ValidateTransport(update.Endpoint, update.AllowedNetworks, update.Enabled, true) != nil {
		return ErrInvalid
	}
	return nil
}
func normalizedEvents(values []string) ([]string, error) {
	if len(values) < 1 || len(values) > 2 {
		return nil, ErrInvalid
	}
	result := append([]string{}, values...)
	slices.Sort(result)
	for n, value := range result {
		if value != "CatalogInvalidated" && value != "UserDataInvalidated" || n > 0 && result[n-1] == value {
			return nil, ErrInvalid
		}
	}
	return result, nil
}

func readConfig(ctx context.Context, tx pgx.Tx) (Config, error) {
	result := Config{SupportedEvents: []string{"CatalogInvalidated", "UserDataInvalidated"}, AllowedNetworks: []string{}}
	err := tx.QueryRow(ctx, `SELECT revision::text,enabled,endpoint,allowed_networks,credential_ciphertext IS NOT NULL,(SELECT count(*) FROM notification_deliveries WHERE state IN ('pending','sending')) FROM notification_transport WHERE id=1`).Scan(&result.Revision, &result.Enabled, &result.Endpoint, &result.AllowedNetworks, &result.HasReceiverCredential, &result.PendingCount)
	return result, err
}
func (s *Store) Config(ctx context.Context, actor identity.Principal) (Config, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Config{}, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, true); err != nil {
		return Config{}, err
	}
	result, err := readConfig(ctx, tx)
	if err != nil {
		return Config{}, ErrUnavailable
	}
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return Config{}, err
	}
	if tx.Commit(ctx) != nil {
		return Config{}, ErrUnavailable
	}
	return result, nil
}
func (s *Store) UpdateConfig(ctx context.Context, actor identity.Principal, update ConfigUpdate) (Config, error) {
	if err := validateConfig(update); err != nil {
		return Config{}, err
	}
	expected, _ := revision(update.Revision, false)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Config{}, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if err = identity.LockNotificationMutation(ctx, tx, actor, true); err != nil {
		return Config{}, err
	}
	var current, generation int64
	var sealed []byte
	if tx.QueryRow(ctx, `SELECT revision,credential_generation,credential_ciphertext FROM notification_transport WHERE id=1 FOR UPDATE`).Scan(&current, &generation, &sealed) != nil {
		return Config{}, ErrUnavailable
	}
	if current != expected {
		return Config{}, ErrConflict
	}
	if generation == math.MaxInt64 {
		return Config{}, ErrConflict
	}
	if err = lockJournalControl(ctx, tx); err != nil {
		return Config{}, err
	}
	locked, err := tx.Query(ctx, `SELECT id FROM notification_registrations ORDER BY id FOR UPDATE`)
	if err != nil {
		return Config{}, ErrUnavailable
	}
	for locked.Next() {
		var ignored string
		if locked.Scan(&ignored) != nil {
			locked.Close()
			return Config{}, ErrUnavailable
		}
	}
	err = locked.Err()
	locked.Close()
	if err != nil {
		return Config{}, ErrUnavailable
	}
	if update.ReceiverCredential != nil {
		generation++
		sealed = nil
		if *update.ReceiverCredential != "" {
			sealed, err = s.users.SealNotificationSecret(ctx, tx, identity.NotificationReceiverPurpose, identity.NotificationSecretBinding("receiver", strconv.FormatInt(generation, 10)), *update.ReceiverCredential)
			if err != nil {
				return Config{}, err
			}
		}
	}
	if update.Enabled && len(sealed) == 0 {
		return Config{}, ErrInvalid
	}
	networks := append([]string{}, update.AllowedNetworks...)
	if _, err = tx.Exec(ctx, `UPDATE notification_transport SET revision=revision+1,enabled=$1,endpoint=$2,allowed_networks=$3,credential_ciphertext=$4,credential_generation=$5 WHERE id=1`, update.Enabled, update.Endpoint, networks, sealed, generation); err != nil {
		return Config{}, ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE notification_deliveries SET state='cancelled',refs='[]',outcome='configuration_changed',lease_id='',lease_until=NULL,updated_at=clock_timestamp() WHERE state IN ('pending','sending'); UPDATE notification_registrations SET source_cursor=(SELECT sequence FROM notification_journal_state WHERE id=1)`); err != nil {
		return Config{}, ErrUnavailable
	}
	result, err := readConfig(ctx, tx)
	if err != nil {
		return Config{}, ErrUnavailable
	}
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return Config{}, err
	}
	if tx.Commit(ctx) != nil {
		return Config{}, ErrUnavailable
	}
	return result, nil
}
func readRegistration(ctx context.Context, tx pgx.Tx, session string) (Registration, error) {
	result := Registration{Revision: "0", Transport: Transport, EventIds: []string{}}
	err := tx.QueryRow(ctx, `SELECT id,revision::text,enabled,event_ids,true,last_outcome FROM notification_registrations WHERE session_id=$1`, session).Scan(&result.Id, &result.Revision, &result.Enabled, &result.EventIds, &result.HasTargetToken, &result.LastOutcome)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	return result, err
}
func (s *Store) Registration(ctx context.Context, actor identity.Principal) (Registration, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Registration{}, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if err = identity.CheckNotificationSession(ctx, tx, actor, false); err != nil {
		return Registration{}, err
	}
	result, err := readRegistration(ctx, tx, actor.SessionID)
	if err != nil {
		return Registration{}, ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return Registration{}, ErrUnavailable
	}
	return result, nil
}
func (s *Store) PutRegistration(ctx context.Context, actor identity.Principal, input RegistrationUpdate) (Registration, error) {
	if actor.PeerIP != "" {
		peer, err := netip.ParseAddr(actor.PeerIP)
		if err != nil || peer.Zone() != "" || peer.Unmap().String() != actor.PeerIP {
			return Registration{}, ErrInvalid
		}
	}
	expected, err := revision(input.Revision, true)
	if err != nil || input.Transport != Transport {
		return Registration{}, ErrInvalid
	}
	events, err := normalizedEvents(input.EventIds)
	if err != nil {
		return Registration{}, err
	}
	if input.TargetToken != nil && !identity.ValidNotificationSecret(*input.TargetToken) {
		return Registration{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Registration{}, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if err = identity.LockNotificationMutation(ctx, tx, actor, false); err != nil {
		return Registration{}, err
	}
	var configured bool
	if tx.QueryRow(ctx, `SELECT credential_ciphertext IS NOT NULL AND endpoint<>'' FROM notification_transport WHERE id=1 FOR SHARE`).Scan(&configured) != nil || !configured {
		return Registration{}, ErrUnavailable
	}
	if err = lockJournalControl(ctx, tx); err != nil {
		return Registration{}, err
	}
	var id, device string
	var current, generation int64
	var wasEnabled bool
	var sealed []byte
	err = tx.QueryRow(ctx, `SELECT id,revision,token_generation,token_ciphertext,enabled FROM notification_registrations WHERE session_id=$1 FOR UPDATE`, actor.SessionID).Scan(&id, &current, &generation, &sealed, &wasEnabled)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Registration{}, ErrUnavailable
	}
	if current != expected {
		return Registration{}, ErrConflict
	}
	if generation == math.MaxInt64 {
		return Registration{}, ErrConflict
	}
	if tx.QueryRow(ctx, `SELECT device_id FROM sessions WHERE id=$1`, actor.SessionID).Scan(&device) != nil {
		return Registration{}, ErrUnavailable
	}
	if current == 0 || !wasEnabled {
		var total, perUser, history int
		if tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE enabled),count(*) FILTER(WHERE enabled AND user_id=$1),count(*) FROM notification_registrations`, actor.User.ID).Scan(&total, &perUser, &history) != nil {
			return Registration{}, ErrUnavailable
		}
		if total >= 64 || perUser >= 4 || current == 0 && history >= 512 {
			return Registration{}, ErrLimit
		}
		if current == 0 {
			id = notificationjournal.NewID()
		}
	}
	if input.TargetToken != nil {
		generation++
		sealed, err = s.users.SealNotificationSecret(ctx, tx, identity.NotificationTargetPurpose, identity.NotificationSecretBinding(id, actor.SessionID, actor.User.ID, device, strconv.FormatInt(generation, 10)), *input.TargetToken)
		if err != nil {
			return Registration{}, err
		}
	}
	if len(sealed) == 0 {
		return Registration{}, ErrInvalid
	}
	if _, err = tx.Exec(ctx, `INSERT INTO notification_registrations(id,session_id,user_id,device_id,peer_ip,revision,event_ids,token_ciphertext,token_generation,source_cursor)
	VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,(SELECT sequence FROM notification_journal_state WHERE id=1))
	ON CONFLICT(session_id) DO UPDATE SET revision=EXCLUDED.revision,enabled=true,event_ids=EXCLUDED.event_ids,token_ciphertext=EXCLUDED.token_ciphertext,token_generation=EXCLUDED.token_generation,source_cursor=EXCLUDED.source_cursor,peer_ip=EXCLUDED.peer_ip,last_outcome='',updated_at=clock_timestamp()`, id, actor.SessionID, actor.User.ID, device, actor.PeerIP, current+1, events, sealed, generation); err != nil {
		return Registration{}, ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE notification_deliveries SET state='cancelled',refs='[]',outcome='registration_changed',lease_id='',lease_until=NULL,updated_at=clock_timestamp() WHERE registration_id=$1 AND state IN ('pending','sending')`, id); err != nil {
		return Registration{}, ErrUnavailable
	}
	result, err := readRegistration(ctx, tx, actor.SessionID)
	if err != nil {
		return Registration{}, ErrUnavailable
	}
	if err = identity.CheckNotificationSession(ctx, tx, actor, false); err != nil {
		return Registration{}, err
	}
	if tx.Commit(ctx) != nil {
		return Registration{}, ErrUnavailable
	}
	return result, nil
}
func (s *Store) DeleteRegistration(ctx context.Context, actor identity.Principal, expected string) (Registration, error) {
	v, err := revision(expected, false)
	if err != nil {
		return Registration{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Registration{}, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if err = identity.LockNotificationMutation(ctx, tx, actor, false); err != nil {
		return Registration{}, err
	}
	var id string
	err = tx.QueryRow(ctx, `UPDATE notification_registrations SET enabled=false,revision=revision+1,last_outcome='revoked',updated_at=clock_timestamp() WHERE session_id=$1 AND revision=$2 RETURNING id`, actor.SessionID, v).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Registration{}, ErrConflict
	}
	if err != nil {
		return Registration{}, ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE notification_deliveries SET state='cancelled',refs='[]',outcome='revoked',lease_id='',lease_until=NULL,updated_at=clock_timestamp() WHERE registration_id=$1 AND state IN ('pending','sending')`, id); err != nil {
		return Registration{}, ErrUnavailable
	}
	result, err := readRegistration(ctx, tx, actor.SessionID)
	if err != nil {
		return Registration{}, ErrUnavailable
	}
	if err = identity.CheckNotificationSession(ctx, tx, actor, false); err != nil {
		return Registration{}, err
	}
	if tx.Commit(ctx) != nil {
		return Registration{}, ErrUnavailable
	}
	return result, nil
}

func decodeReferences(raw []byte) ([]notificationjournal.Reference, error) {
	var refs []notificationjournal.Reference
	if len(raw) > 524288 || json.Unmarshal(raw, &refs) != nil || len(refs) > 4096 {
		return nil, ErrInvalid
	}
	for _, ref := range refs {
		if !notificationjournal.ValidID(ref.ID) || ref.Kind != "Item" && ref.Kind != "Entity" && ref.Kind != "Library" {
			return nil, ErrInvalid
		}
	}
	return refs, nil
}
func safeCode(err error) string {
	if errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, identity.ErrClientSessionForbidden) || errors.Is(err, library.ErrForbidden) {
		return "authority_revoked"
	}
	if errors.Is(err, library.ErrNotFound) {
		return "source_unavailable"
	}
	return "unavailable"
}
