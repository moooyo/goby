package notifications

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/notificationjournal"
)

const AdminDestinationLimit = 16
const adminDestinationSecretPrefix = "admin-destination/v1:"

var ErrDestinationNotFound = errors.New("notification destination was not found")
var ErrDestinationInactive = errors.New("notification destination owner is unavailable")

type AdminNotificationEvent struct {
	Id      string
	Name    string
	Actions []string
}

// Only committed actions that the server actually records can be subscribed to.
var adminNotificationEvents = []AdminNotificationEvent{
	{Id: "UserChanges", Name: "User account changes", Actions: []string{"user.created", "user.updated", "user.deleted", "user.password_reset"}},
	{Id: "SignIn", Name: "User sign-ins", Actions: []string{"session.login"}},
	{Id: "LibraryChanges", Name: "Library changes", Actions: []string{"library.created", "library.updated", "library.removed", "library.root_binding.updated"}},
	{Id: "ScanFinished", Name: "Library scans finished", Actions: []string{"scan.finished"}},
	{Id: "TaskFinished", Name: "Scheduled tasks finished", Actions: []string{"task.finished"}},
	{Id: "BackupFinished", Name: "Backups finished", Actions: []string{"backup.published"}},
	{Id: "MetadataChanges", Name: "Metadata changes", Actions: []string{"metadata.updated"}},
}

type AdminSMTPConfig struct {
	Host     string
	Port     int
	Security string
	Username string
	From     string
	To       []string
}

type AdminDestination struct {
	Id              string
	Revision        string
	Name            string
	Transport       string
	Enabled         bool
	EventIds        []string
	AllowedNetworks []string
	Endpoint        string
	SMTP            AdminSMTPConfig
	HasSecret       bool
	LastOutcome     string
	LastDeliveredAt *time.Time `json:",omitempty"`
	PendingCount    int
	DroppedCount    int
}

type AdminDestinationList struct {
	Items           []AdminDestination
	SupportedEvents []AdminNotificationEvent
	Limit           int
}

type AdminDestinationInput struct {
	Revision        string
	Name            string
	Transport       string
	Enabled         bool
	EventIds        []string
	AllowedNetworks []string
	Endpoint        string
	SMTP            AdminSMTPConfig
	Secret          *string
}

func (AdminDestinationInput) String() string   { return "<notification destination input>" }
func (AdminDestinationInput) GoString() string { return "<notification destination input>" }

type AdminMessage struct {
	EventID    string    `json:"EventId"`
	Kind       string    `json:"Kind"`
	Title      string    `json:"Title"`
	Summary    string    `json:"Summary"`
	OccurredAt time.Time `json:"OccurredAt"`
}

type storedAdminDestination struct {
	AdminDestination
	owner            string
	secretGeneration int64
	sealed           []byte
}

func defaultAdminSMTP() AdminSMTPConfig {
	return AdminSMTPConfig{Port: 587, Security: "starttls", To: []string{}}
}

func validAdminDestinationID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func validAdminText(value string, maximum int) bool {
	if !utf8.ValidString(value) || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func validAdminMailAddress(value string) bool {
	if value == "" || len(value) > 254 || !validAdminText(value, 254) {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value && !strings.ContainsAny(value, "<>\r\n")
}

func validAdminSMTPHost(value string) bool {
	if value == "" || len(value) > 253 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "/\\@?#[]%") {
		return false
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Zone() == "" && address.Unmap().String() == value
	}
	if strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	return true
}

func normalizeAdminEvents(values []string) ([]string, error) {
	if len(values) < 1 || len(values) > len(adminNotificationEvents) {
		return nil, ErrInvalid
	}
	result := slices.Clone(values)
	slices.Sort(result)
	for index, value := range result {
		if index > 0 && result[index-1] == value || !slices.ContainsFunc(adminNotificationEvents, func(event AdminNotificationEvent) bool { return event.Id == value }) {
			return nil, ErrInvalid
		}
	}
	return result, nil
}

func validateAdminDestination(input AdminDestinationInput, creating bool) (AdminDestinationInput, error) {
	if _, err := revision(input.Revision, creating); err != nil || creating && input.Revision != "0" || !creating && input.Revision == "0" {
		return input, ErrInvalid
	}
	if strings.TrimSpace(input.Name) != input.Name || input.Name == "" || !validAdminText(input.Name, 120) || input.AllowedNetworks == nil {
		return input, ErrInvalid
	}
	events, err := normalizeAdminEvents(input.EventIds)
	if err != nil {
		return input, err
	}
	input.EventIds = events
	if notificationjournal.ValidateTransport("", input.AllowedNetworks, false, true) != nil {
		return input, ErrInvalid
	}
	if input.Secret != nil && !validAdminText(*input.Secret, 1024) {
		return input, ErrInvalid
	}
	switch input.Transport {
	case "webhook":
		if input.Endpoint == "" || strings.TrimSpace(input.Endpoint) != input.Endpoint || !validAdminText(input.Endpoint, 2048) || notificationjournal.ValidateTransport(input.Endpoint, input.AllowedNetworks, true, true) != nil {
			return input, ErrInvalid
		}
		if input.Secret != nil && *input.Secret != "" && (!identity.ValidNotificationSecret(*input.Secret) || len(*input.Secret) > 1024) {
			return input, ErrInvalid
		}
		input.SMTP = defaultAdminSMTP()
	case "smtp":
		if !validAdminSMTPHost(input.SMTP.Host) || input.SMTP.Port < 1 || input.SMTP.Port > 65535 || input.SMTP.Security != "tls" && input.SMTP.Security != "starttls" || !validAdminText(input.SMTP.Username, 256) || !validAdminMailAddress(input.SMTP.From) || len(input.SMTP.To) < 1 || len(input.SMTP.To) > 16 {
			return input, ErrInvalid
		}
		seen := map[string]bool{}
		for _, recipient := range input.SMTP.To {
			if !validAdminMailAddress(recipient) || seen[strings.ToLower(recipient)] {
				return input, ErrInvalid
			}
			seen[strings.ToLower(recipient)] = true
		}
		input.Endpoint = ""
		if input.SMTP.Username == "" && input.Secret != nil && *input.Secret != "" {
			return input, ErrInvalid
		}
	default:
		return input, ErrInvalid
	}
	return input, nil
}

func adminSecretBinding(id string, generation int64) string {
	return identity.NotificationSecretBinding("admin-destination", id, strconv.FormatInt(generation, 10))
}

func encodeAdminSecret(value string) string {
	return adminDestinationSecretPrefix + base64.RawStdEncoding.EncodeToString([]byte(value))
}

func decodeAdminSecret(value string) (string, error) {
	if !strings.HasPrefix(value, adminDestinationSecretPrefix) {
		return "", ErrUnavailable
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, adminDestinationSecretPrefix))
	if err != nil || len(raw) == 0 || !validAdminText(string(raw), 1024) {
		return "", ErrUnavailable
	}
	return string(raw), nil
}

const adminDestinationProjection = `d.id,d.revision::text,d.name,d.transport,d.enabled,d.event_ids,d.allowed_networks,d.endpoint,d.smtp_config,d.secret_ciphertext IS NOT NULL,d.last_outcome,d.last_delivered_at,(SELECT count(*) FROM admin_notification_deliveries q WHERE q.destination_id=d.id AND q.state IN ('pending','sending')),d.dropped_count,d.owner_user_id,d.secret_generation,d.secret_ciphertext`

func scanAdminDestination(row pgx.Row) (storedAdminDestination, error) {
	var result storedAdminDestination
	var raw []byte
	err := row.Scan(&result.Id, &result.Revision, &result.Name, &result.Transport, &result.Enabled, &result.EventIds, &result.AllowedNetworks, &result.Endpoint, &raw, &result.HasSecret, &result.LastOutcome, &result.LastDeliveredAt, &result.PendingCount, &result.DroppedCount, &result.owner, &result.secretGeneration, &result.sealed)
	if err == nil && json.Unmarshal(raw, &result.SMTP) != nil {
		err = ErrUnavailable
	}
	return result, err
}

func readAdminDestination(ctx context.Context, tx pgx.Tx, id string) (storedAdminDestination, error) {
	return scanAdminDestination(tx.QueryRow(ctx, `SELECT `+adminDestinationProjection+` FROM admin_notification_destinations d WHERE d.id=$1`, id))
}

func (s *Store) AdminDestinations(ctx context.Context, actor identity.Principal) (AdminDestinationList, error) {
	result := AdminDestinationList{Items: []AdminDestination{}, SupportedEvents: slices.Clone(adminNotificationEvents), Limit: AdminDestinationLimit}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, true); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+adminDestinationProjection+` FROM admin_notification_destinations d ORDER BY d.created_at,d.id LIMIT 17`)
	if err != nil {
		return result, ErrUnavailable
	}
	for rows.Next() {
		value, scanErr := scanAdminDestination(rows)
		if scanErr != nil {
			rows.Close()
			return result, ErrUnavailable
		}
		result.Items = append(result.Items, value.AdminDestination)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(result.Items) > AdminDestinationLimit {
		return result, ErrUnavailable
	}
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return result, err
	}
	if tx.Commit(ctx) != nil {
		return result, ErrUnavailable
	}
	return result, nil
}

func adminDestinationTargetChanged(current AdminDestination, input AdminDestinationInput) bool {
	return current.Transport != input.Transport || input.Transport == "webhook" && current.Endpoint != input.Endpoint || input.Transport == "smtp" && (current.SMTP.Host != input.SMTP.Host || current.SMTP.Username != input.SMTP.Username)
}

func (s *Store) PutAdminDestination(ctx context.Context, actor identity.Principal, id string, input AdminDestinationInput) (AdminDestination, error) {
	creating := id == ""
	input, err := validateAdminDestination(input, creating)
	if err != nil || !creating && !validAdminDestinationID(id) {
		return AdminDestination{}, ErrInvalid
	}
	if creating {
		id = notificationjournal.NewID()
	}
	expected, _ := revision(input.Revision, creating)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return AdminDestination{}, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if err = identity.LockNotificationMutation(ctx, tx, actor, true); err != nil {
		return AdminDestination{}, err
	}
	var current storedAdminDestination
	if creating {
		var count int
		if tx.QueryRow(ctx, `SELECT count(*) FROM admin_notification_destinations`).Scan(&count) != nil {
			return AdminDestination{}, ErrUnavailable
		}
		if count >= AdminDestinationLimit {
			return AdminDestination{}, ErrLimit
		}
		current.secretGeneration = 1
	} else {
		var actual int64
		if err = tx.QueryRow(ctx, `SELECT revision FROM admin_notification_destinations WHERE id=$1 FOR UPDATE`, id).Scan(&actual); errors.Is(err, pgx.ErrNoRows) {
			return AdminDestination{}, ErrDestinationNotFound
		} else if err != nil {
			return AdminDestination{}, ErrUnavailable
		}
		if actual != expected || actual == math.MaxInt64 {
			return AdminDestination{}, ErrConflict
		}
		current, err = readAdminDestination(ctx, tx, id)
		if err != nil {
			return AdminDestination{}, ErrUnavailable
		}
		if input.Secret == nil && current.HasSecret && adminDestinationTargetChanged(current.AdminDestination, input) {
			return AdminDestination{}, ErrInvalid
		}
	}
	sealed, generation := current.sealed, current.secretGeneration
	if input.Secret != nil {
		if generation == math.MaxInt64 {
			return AdminDestination{}, ErrConflict
		}
		generation++
		sealed = nil
		if *input.Secret != "" {
			sealed, err = s.users.SealNotificationSecret(ctx, tx, identity.NotificationReceiverPurpose, adminSecretBinding(id, generation), encodeAdminSecret(*input.Secret))
			if err != nil {
				return AdminDestination{}, err
			}
		}
	}
	if input.Enabled && (input.Transport == "webhook" || input.SMTP.Username != "") && len(sealed) == 0 {
		return AdminDestination{}, ErrInvalid
	}
	raw, _ := json.Marshal(input.SMTP)
	if creating {
		_, err = tx.Exec(ctx, `INSERT INTO admin_notification_destinations(id,name,transport,enabled,event_ids,allowed_networks,endpoint,smtp_config,owner_user_id,secret_generation,secret_ciphertext) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, input.Name, input.Transport, input.Enabled, input.EventIds, input.AllowedNetworks, input.Endpoint, raw, actor.User.ID, generation, sealed)
	} else {
		_, err = tx.Exec(ctx, `UPDATE admin_notification_destinations SET revision=revision+1,name=$2,transport=$3,enabled=$4,event_ids=$5,allowed_networks=$6,endpoint=$7,smtp_config=$8,owner_user_id=$9,secret_generation=$10,secret_ciphertext=$11,last_outcome='configuration_changed',updated_at=clock_timestamp() WHERE id=$1`, id, input.Name, input.Transport, input.Enabled, input.EventIds, input.AllowedNetworks, input.Endpoint, raw, actor.User.ID, generation, sealed)
	}
	if err != nil {
		return AdminDestination{}, ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE admin_notification_deliveries SET state='cancelled',lease_id='',lease_until=NULL,outcome='configuration_changed',updated_at=clock_timestamp() WHERE destination_id=$1 AND state IN ('pending','sending')`, id); err != nil {
		return AdminDestination{}, ErrUnavailable
	}
	result, err := readAdminDestination(ctx, tx, id)
	if err != nil {
		return AdminDestination{}, ErrUnavailable
	}
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return AdminDestination{}, err
	}
	if tx.Commit(ctx) != nil {
		return AdminDestination{}, ErrUnavailable
	}
	return result.AdminDestination, nil
}

func (s *Store) DeleteAdminDestination(ctx context.Context, actor identity.Principal, id, expected string) error {
	if !validAdminDestinationID(id) {
		return ErrInvalid
	}
	want, err := revision(expected, false)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if err = identity.LockNotificationMutation(ctx, tx, actor, true); err != nil {
		return err
	}
	var actual int64
	if err = tx.QueryRow(ctx, `SELECT revision FROM admin_notification_destinations WHERE id=$1 FOR UPDATE`, id).Scan(&actual); errors.Is(err, pgx.ErrNoRows) {
		return ErrDestinationNotFound
	} else if err != nil {
		return ErrUnavailable
	}
	if actual != want {
		return ErrConflict
	}
	if _, err = tx.Exec(ctx, `DELETE FROM admin_notification_destinations WHERE id=$1`, id); err != nil {
		return ErrUnavailable
	}
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return err
	}
	if tx.Commit(ctx) != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) TestAdminDestination(ctx context.Context, actor identity.Principal, id, expected string) (string, error) {
	if !validAdminDestinationID(id) {
		return "", ErrInvalid
	}
	want, err := revision(expected, false)
	if err != nil {
		return "", err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if err = identity.LockNotificationMutation(ctx, tx, actor, true); err != nil {
		return "", err
	}
	var actual int64
	var enabled, ownerActive bool
	if err = tx.QueryRow(ctx, `SELECT d.revision,d.enabled,u.is_administrator AND NOT u.is_disabled FROM admin_notification_destinations d JOIN users u ON u.id=d.owner_user_id WHERE d.id=$1 FOR UPDATE OF d`, id).Scan(&actual, &enabled, &ownerActive); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDestinationNotFound
	} else if err != nil {
		return "", ErrUnavailable
	}
	if actual != want {
		return "", ErrConflict
	}
	if !enabled {
		return "", ErrInvalid
	}
	if !ownerActive {
		return "", ErrDestinationInactive
	}
	var count int
	if tx.QueryRow(ctx, `SELECT count(*) FROM admin_notification_deliveries WHERE destination_id=$1 AND state IN ('pending','sending')`, id).Scan(&count) != nil {
		return "", ErrUnavailable
	}
	if count >= 256 {
		return "", ErrLimit
	}
	deliveryID := notificationjournal.NewID()
	message := AdminMessage{EventID: deliveryID, Kind: "Test", Title: "Goby test notification", Summary: "This destination accepted a test requested by a server administrator.", OccurredAt: time.Now().UTC()}
	raw, _ := json.Marshal(message)
	if _, err = tx.Exec(ctx, `INSERT INTO admin_notification_deliveries(id,destination_id,destination_revision,source_key,event_kind,payload) VALUES($1,$2,$3,$4,'Test',$5)`, deliveryID, id, actual, "test:"+deliveryID, raw); err != nil {
		return "", ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE admin_notification_destinations SET last_outcome='pending' WHERE id=$1`, id); err != nil {
		return "", ErrUnavailable
	}
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return "", err
	}
	if tx.Commit(ctx) != nil {
		return "", ErrUnavailable
	}
	return deliveryID, nil
}

func adminEventKind(action string) string {
	for _, event := range adminNotificationEvents {
		if slices.Contains(event.Actions, action) {
			return event.Id
		}
	}
	return ""
}

func adminDestinationSafeError(err error) string {
	if errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, ErrDestinationNotFound) {
		return "authority_revoked"
	}
	return "retry"
}

func adminDestinationQueryError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDestinationNotFound
	}
	return fmt.Errorf("%w: notification destination query", ErrUnavailable)
}
