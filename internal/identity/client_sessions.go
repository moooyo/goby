package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// ClientSessionPresenceWindow is an activity filter, not an authentication TTL.
	ClientSessionPresenceWindow = 5 * time.Minute
	ClientSessionTouchInterval  = 15 * time.Second
	MaxClientSessions           = 256
	MaxClientCapabilitiesBytes  = 64 * 1024
	MaxClientCapabilityEntries  = 128
	MaxClientCapabilityText     = 2048
	maxCapabilityDepth          = 8
	maxCapabilityNodes          = 4096
	maxCapabilityObjectFields   = 64
)

var ErrClientSessionForbidden = errors.New("client session access denied")

// ClientCapabilities contains only supported, non-secret client declarations.
// DeviceProfile is a bounded, canonical JSON object containing known official
// fields. Push tokens and unknown fields are never retained.
type ClientCapabilities struct {
	PlayableMediaTypes   []string        `json:"PlayableMediaTypes,omitempty"`
	SupportedCommands    []string        `json:"SupportedCommands,omitempty"`
	SupportsMediaControl bool            `json:"SupportsMediaControl,omitempty"`
	SupportsSync         bool            `json:"SupportsSync,omitempty"`
	DeviceProfile        json.RawMessage `json:"DeviceProfile,omitempty"`
	IconURL              string          `json:"IconUrl,omitempty"`
	AppID                string          `json:"AppId,omitempty"`
}

// ClientSession is a safe projection of an Emby login or application client.
// CredentialID identifies the revocable parent; SessionID identifies the wire
// client context. They are equal only for ordinary logins. A live client session
// does not establish that media is currently playing.
type ClientSession struct {
	SessionID        string
	CredentialID     string
	Kind             string
	ApplicationKeyID int64
	UserID           string
	UserName         string
	Client           Client
	CreatedAt        time.Time
	LastSeenAt       time.Time
	LastUsedAt       *time.Time
	ExpiresAt        time.Time
	Capabilities     ClientCapabilities
}

// ClientSessionFilter constrains an already-authorized session list. An omitted
// activity window defaults to ClientSessionPresenceWindow; an explicit zero
// disables only the presence filter, never expiry or account authorization.
type ClientSessionFilter struct {
	SessionID           string
	DeviceID            string
	ActiveWithinSeconds *int
	Limit               int
}

// ParseClientCapabilities validates the complete input before dropping unknown
// fields for forward compatibility. Known fields must match their official
// types; null optional fields are treated as omitted. All JSON, including
// discarded fields, is bounded. Duplicate keys are rejected rather than merged.
func ParseClientCapabilities(data []byte) (ClientCapabilities, error) {
	if len(data) == 0 || len(data) > MaxClientCapabilitiesBytes || !utf8.Valid(data) {
		return ClientCapabilities{}, capabilityInputError("capabilities must contain at most 64 KiB of UTF-8 JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	nodes := 0
	value, err := readCapabilityJSON(decoder, 0, &nodes)
	if err != nil {
		return ClientCapabilities{}, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ClientCapabilities{}, capabilityInputError("capabilities must contain one JSON object")
	}
	if _, ok := value.(map[string]any); !ok {
		return ClientCapabilities{}, capabilityInputError("capabilities must be a JSON object")
	}
	cleaned, err := sanitizeCapabilityValue(value, clientCapabilityShape, "capabilities")
	if err != nil {
		return ClientCapabilities{}, err
	}
	fields := cleaned.(map[string]any)
	// Push delivery is not implemented, and notification tokens are secrets.
	delete(fields, "PushToken")
	delete(fields, "PushTokenType")
	encoded, err := json.Marshal(fields)
	if err != nil {
		return ClientCapabilities{}, fmt.Errorf("encode validated capabilities: %w", err)
	}
	if len(encoded) > MaxClientCapabilitiesBytes {
		return ClientCapabilities{}, capabilityInputError("encoded capabilities exceed 64 KiB")
	}
	var capabilities ClientCapabilities
	if err := json.Unmarshal(encoded, &capabilities); err != nil {
		return ClientCapabilities{}, fmt.Errorf("decode validated capabilities: %w", err)
	}
	return capabilities, nil
}

// UpdateClientCapabilities replaces the caller's complete capability snapshot.
// A supplied target must be the authenticated session, including for admins.
func (s *Store) UpdateClientCapabilities(ctx context.Context, principal Principal, targetSessionID string, capabilities ClientCapabilities) error {
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		return capabilityInputError("capabilities must contain valid JSON")
	}
	validated, err := ParseClientCapabilities(encoded)
	if err != nil {
		return err
	}
	encoded, err = json.Marshal(validated)
	if err != nil {
		return fmt.Errorf("encode client capabilities: %w", err)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin client capability update: %w", err)
	}
	defer rollback(tx)
	if _, err := lockClientSession(ctx, tx, principal, true); err != nil {
		return err
	}
	if targetSessionID != "" && targetSessionID != clientSessionIdentity(principal) {
		return ErrClientSessionForbidden
	}
	table := "sessions"
	if principal.IsApplicationKey() {
		table = "application_key_clients"
	}
	if _, err := tx.Exec(ctx, `UPDATE `+table+` SET client_capabilities = $2::jsonb,
		last_seen_at = CASE WHEN last_seen_at <= now() - ($3::bigint * interval '1 second')
		THEN now() ELSE last_seen_at END WHERE id = $1`, clientSessionIdentity(principal), encoded,
		int64(ClientSessionTouchInterval/time.Second)); err != nil {
		return fmt.Errorf("update client capabilities: %w", err)
	}
	if err := touchApplicationKeyUsage(ctx, tx, principal); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit client capability update: %w", err)
	}
	return nil
}

// TouchClientSession records authenticated activity at most once per interval.
// It revalidates the account and session without extending the login lifetime.
func (s *Store) TouchClientSession(ctx context.Context, principal Principal) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin client session activity: %w", err)
	}
	defer rollback(tx)
	if _, err := lockClientSession(ctx, tx, principal, true); err != nil {
		return err
	}
	table := "sessions"
	if principal.IsApplicationKey() {
		table = "application_key_clients"
	}
	if _, err := tx.Exec(ctx, `UPDATE `+table+` SET last_seen_at = now()
		WHERE id = $1 AND last_seen_at <= now() - ($2::bigint * interval '1 second')`,
		clientSessionIdentity(principal), int64(ClientSessionTouchInterval/time.Second)); err != nil {
		return fmt.Errorf("record client session activity: %w", err)
	}
	if err := touchApplicationKeyUsage(ctx, tx, principal); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit client session activity: %w", err)
	}
	return nil
}

// ListClientSessions returns bounded active Emby credentials. Ordinary accounts
// see their own sessions; the current stored administrator role permits all
// enabled accounts. Admin cookie sessions are never included or accepted.
func (s *Store) ListClientSessions(ctx context.Context, principal Principal, filter ClientSessionFilter) ([]ClientSession, error) {
	if err := validateClient(Client{DeviceID: filter.DeviceID, Name: filter.SessionID}); err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit == 0 {
		limit = MaxClientSessions
	}
	if limit < 1 || limit > MaxClientSessions {
		return nil, capabilityInputError("session limit must be between 1 and 256")
	}
	activeSeconds := int(ClientSessionPresenceWindow / time.Second)
	if filter.ActiveWithinSeconds != nil {
		activeSeconds = *filter.ActiveWithinSeconds
	}
	if activeSeconds < 0 || activeSeconds > int(embyLifetime/time.Second) {
		return nil, capabilityInputError("session activity window is outside the login lifetime")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin client session list: %w", err)
	}
	defer rollback(tx)
	isAdmin, err := lockClientSession(ctx, tx, principal, false)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `WITH clients AS (
		SELECT a.id, a.id AS credential_id, u.id AS user_id, u.name AS user_name,
			a.client_name, a.device_id, a.device_name, a.client_version, a.created_at,
			a.last_seen_at, a.expires_at, a.client_capabilities, a.kind, 0::bigint AS key_id,
			NULL::timestamptz AS last_used_at
		FROM sessions a JOIN users u ON u.id = a.user_id
		WHERE a.kind = 'emby' AND a.revoked_at IS NULL AND a.expires_at > now()
		AND NOT u.is_disabled AND ($1::boolean OR a.user_id = $2)
		UNION ALL
		SELECT c.id, a.id, '', '', c.client_name, c.device_id, c.device_name,
			c.client_version, c.created_at, c.last_seen_at, NULL::timestamptz,
			c.client_capabilities, a.kind, k.id,
			CASE WHEN k.last_used_at IS NOT NULL THEN c.last_seen_at END
		FROM application_key_clients c JOIN sessions a ON a.id = c.credential_id
		JOIN application_keys k ON k.credential_id = a.id
		WHERE $1::boolean AND a.kind = 'application_key' AND a.user_id IS NULL
		AND a.expires_at IS NULL AND a.revoked_at IS NULL
	)
	SELECT id, credential_id, user_id, user_name, client_name, device_id, device_name,
		client_version, created_at, last_seen_at, expires_at, client_capabilities, kind, key_id, last_used_at
	FROM clients WHERE ($3 = '' OR id = $3) AND ($4 = '' OR device_id = $4)
	AND ($5::bigint = 0 OR last_seen_at >= now() - ($5::bigint * interval '1 second'))
	ORDER BY last_seen_at DESC, id LIMIT $6`, isAdmin, principal.User.ID,
		filter.SessionID, filter.DeviceID, activeSeconds, limit)
	if err != nil {
		return nil, fmt.Errorf("list client sessions: %w", err)
	}
	defer rows.Close()
	sessions := make([]ClientSession, 0)
	for rows.Next() {
		var session ClientSession
		var encoded []byte
		var expiresAt pgtype.Timestamptz
		if err := rows.Scan(&session.SessionID, &session.CredentialID, &session.UserID, &session.UserName,
			&session.Client.Name, &session.Client.DeviceID, &session.Client.Device,
			&session.Client.Version, &session.CreatedAt, &session.LastSeenAt,
			&expiresAt, &encoded, &session.Kind, &session.ApplicationKeyID, &session.LastUsedAt); err != nil {
			return nil, fmt.Errorf("read client session: %w", err)
		}
		if expiresAt.Valid {
			session.ExpiresAt = expiresAt.Time
		}
		// Read-time validation also prevents unsafe fields from a manual database
		// change from reaching the session projection. JSONB adds whitespace.
		var compact bytes.Buffer
		if err := json.Compact(&compact, encoded); err != nil {
			return nil, fmt.Errorf("read stored client capabilities: %w", err)
		}
		session.Capabilities, err = ParseClientCapabilities(compact.Bytes())
		if err != nil {
			return nil, fmt.Errorf("read stored client capabilities: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read client session list: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit client session list: %w", err)
	}
	return sessions, nil
}

// Locks keep concurrent disable, demotion, and revocation from authorizing a
// mutation with stale state. Client metadata and role claims are never trusted.
func lockClientSession(ctx context.Context, tx pgx.Tx, principal Principal, mutate bool) (bool, error) {
	if principal.IsApplicationKey() {
		locking := " FOR SHARE"
		if mutate {
			locking = " FOR UPDATE"
		}
		var credentialID string
		err := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE id = $1`+locking, principal.SessionID).Scan(&credentialID)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrUnauthorized
		}
		if err != nil {
			return false, fmt.Errorf("lock client application credential: %w", err)
		}
		if err := CheckApplicationKey(ctx, tx, principal.SessionID, false); err != nil {
			return false, err
		}
		var keyID int64
		if err := tx.QueryRow(ctx, `SELECT id FROM application_keys WHERE credential_id = $1`, credentialID).Scan(&keyID); err != nil {
			return false, fmt.Errorf("read client application key: %w", err)
		}
		if keyID != principal.ApplicationKeyID {
			return false, ErrUnauthorized
		}
		var clientID string
		err = tx.QueryRow(ctx, `SELECT id FROM application_key_clients
			WHERE id = $1 AND credential_id = $2`+locking, principal.ClientSessionID, principal.SessionID).Scan(&clientID)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrUnauthorized
		}
		if err != nil {
			return false, fmt.Errorf("lock application client context: %w", err)
		}
		return true, nil
	}
	if principal.Kind != "emby" || principal.ApplicationKeyID != 0 || principal.ClientSessionID != "" || principal.SessionID == "" || principal.User.ID == "" {
		return false, ErrUnauthorized
	}
	var isAdmin bool
	// Account mutations and playback state writes lock the account before its
	// sessions. Keep this order explicit instead of relying on a join plan's
	// row-lock order, which could deadlock with administrator changes.
	err := tx.QueryRow(ctx, `SELECT is_administrator FROM users
		WHERE id = $1 AND NOT is_disabled FOR SHARE`, principal.User.ID).Scan(&isAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrUnauthorized
	}
	if err != nil {
		return false, fmt.Errorf("authorize client account: %w", err)
	}
	locking := " FOR SHARE"
	if mutate {
		locking = " FOR UPDATE"
	}
	var sessionID string
	err = tx.QueryRow(ctx, `SELECT id FROM sessions
		WHERE id = $1 AND user_id = $2 AND kind = 'emby'`+locking,
		principal.SessionID, principal.User.ID).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrUnauthorized
	}
	if err != nil {
		return false, fmt.Errorf("lock client session: %w", err)
	}
	// Evaluate expiration after any row-lock wait. The account and session now
	// remain fixed until this transaction finishes.
	var active bool
	if err := tx.QueryRow(ctx, `SELECT revoked_at IS NULL AND expires_at > clock_timestamp()
		FROM sessions WHERE id = $1`, sessionID).Scan(&active); err != nil {
		return false, fmt.Errorf("revalidate locked client session: %w", err)
	}
	if !active {
		return false, ErrUnauthorized
	}
	return isAdmin, nil
}

func touchApplicationKeyUsage(ctx context.Context, tx pgx.Tx, principal Principal) error {
	if !principal.IsApplicationKey() {
		return nil
	}
	// The key record keeps its immutable app label and reported server ID while
	// displaying the most recently used client's version and device name.
	if _, err := tx.Exec(ctx, `UPDATE sessions a SET device_name = c.device_name,
		client_version = c.client_version,
		last_seen_at = CASE WHEN a.last_seen_at <= clock_timestamp() - ($3::bigint * interval '1 second')
			THEN clock_timestamp() ELSE a.last_seen_at END
		FROM application_key_clients c WHERE a.id = $1 AND c.id = $2 AND c.credential_id = a.id
		AND (a.device_name IS DISTINCT FROM c.device_name OR a.client_version IS DISTINCT FROM c.client_version
		OR a.last_seen_at <= clock_timestamp() - ($3::bigint * interval '1 second'))`,
		principal.SessionID, principal.ClientSessionID, int64(ClientSessionTouchInterval/time.Second)); err != nil {
		return fmt.Errorf("record application key client metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE application_keys SET last_used_at = clock_timestamp()
		WHERE credential_id = $1 AND (last_used_at IS NULL
		OR last_used_at <= clock_timestamp() - ($2::bigint * interval '1 second'))`,
		principal.SessionID, int64(ClientSessionTouchInterval/time.Second)); err != nil {
		return fmt.Errorf("record application key activity: %w", err)
	}
	return nil
}

func capabilityInputError(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, message)
}

// A token walk enforces limits before accepting either known or unknown data.
func readCapabilityJSON(decoder *json.Decoder, depth int, nodes *int) (any, error) {
	*nodes = *nodes + 1
	if depth > maxCapabilityDepth || *nodes > maxCapabilityNodes {
		return nil, capabilityInputError("capabilities exceed JSON nesting or value limits")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, capabilityInputError("capabilities contain malformed JSON")
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			object := make(map[string]any)
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, capabilityInputError("capabilities contain malformed object keys")
				}
				key, ok := keyToken.(string)
				if !ok || len(key) > maxClientFieldBytes || !validCapabilityText(key) {
					return nil, capabilityInputError("capabilities contain an invalid object key")
				}
				if _, exists := object[key]; exists || len(object) >= maxCapabilityObjectFields {
					return nil, capabilityInputError("capabilities contain duplicate keys or too many object fields")
				}
				child, err := readCapabilityJSON(decoder, depth+1, nodes)
				if err != nil {
					return nil, err
				}
				object[key] = child
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
				return nil, capabilityInputError("capabilities contain an incomplete object")
			}
			return object, nil
		case '[':
			values := make([]any, 0)
			for decoder.More() {
				if len(values) >= MaxClientCapabilityEntries {
					return nil, capabilityInputError("capabilities contain too many array entries")
				}
				child, err := readCapabilityJSON(decoder, depth+1, nodes)
				if err != nil {
					return nil, err
				}
				values = append(values, child)
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
				return nil, capabilityInputError("capabilities contain an incomplete array")
			}
			return values, nil
		default:
			return nil, capabilityInputError("capabilities contain an unexpected delimiter")
		}
	case string:
		if !validCapabilityText(value) {
			return nil, capabilityInputError("capability text exceeds limits or contains control characters")
		}
		return value, nil
	case json.Number, bool, nil:
		return value, nil
	default:
		return nil, capabilityInputError("capabilities contain an unsupported JSON value")
	}
}

func validCapabilityText(value string) bool {
	return len(value) <= MaxClientCapabilityText && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

type capabilityShape struct {
	kind    byte
	fields  map[string]*capabilityShape
	element *capabilityShape
	maximum int64
	enum    []string
}

func capabilityObject(fields map[string]*capabilityShape) *capabilityShape {
	return &capabilityShape{kind: 'o', fields: fields}
}

func capabilityArray(element *capabilityShape) *capabilityShape {
	return &capabilityShape{kind: 'a', element: element}
}

func capabilityEnum(values ...string) *capabilityShape {
	return &capabilityShape{kind: 's', enum: values}
}

// These shapes follow the pinned DeviceProfile schema. Unknown fields are
// dropped at every object level, preventing identity or secret extensions from
// becoming persisted session state. Optional nulls are normalized to omission.
var (
	capabilityText      = &capabilityShape{kind: 's'}
	capabilityBool      = &capabilityShape{kind: 'b'}
	capabilityInt32     = &capabilityShape{kind: 'n', maximum: math.MaxInt32}
	capabilityInt64     = &capabilityShape{kind: 'n', maximum: math.MaxInt64}
	capabilityKind      = capabilityEnum("Audio", "Video", "Photo")
	capabilityCondition = capabilityObject(map[string]*capabilityShape{
		"Condition": capabilityEnum("Equals", "NotEquals", "LessThanEqual", "GreaterThanEqual", "EqualsAny"),
		"Property": capabilityEnum("AudioChannels", "AudioBitrate", "AudioProfile", "Width", "Height",
			"Has64BitOffsets", "PacketLength", "VideoBitDepth", "VideoBitrate", "VideoFramerate",
			"VideoLevel", "VideoProfile", "VideoTimestamp", "IsAnamorphic", "RefFrames", "NumAudioStreams",
			"NumVideoStreams", "IsSecondaryAudio", "VideoCodecTag", "IsAvc", "IsInterlaced",
			"AudioSampleRate", "AudioBitDepth", "VideoRange", "VideoRotation", "IsExternalAudio"),
		"Value": capabilityText, "IsRequired": capabilityBool,
	})
	capabilityConditions    = capabilityArray(capabilityCondition)
	capabilityDeviceProfile = capabilityObject(map[string]*capabilityShape{
		"Name": capabilityText, "Id": capabilityText, "SupportedMediaTypes": capabilityText,
		"MaxStreamingBitrate": capabilityInt64, "MusicStreamingTranscodingBitrate": capabilityInt32,
		"MaxStaticMusicBitrate": capabilityInt32, "DeclaredFeatures": capabilityArray(capabilityText),
		"DirectPlayProfiles": capabilityArray(capabilityObject(map[string]*capabilityShape{
			"Container": capabilityText, "AudioCodec": capabilityText, "VideoCodec": capabilityText, "Type": capabilityKind,
		})),
		"TranscodingProfiles": capabilityArray(capabilityObject(map[string]*capabilityShape{
			"Container": capabilityText, "Type": capabilityKind, "VideoCodec": capabilityText,
			"AudioCodec": capabilityText, "Protocol": capabilityText, "EstimateContentLength": capabilityBool,
			"EnableMpegtsM2TsMode": capabilityBool, "TranscodeSeekInfo": capabilityEnum("Auto", "Bytes"),
			"CopyTimestamps": capabilityBool, "Context": capabilityEnum("Streaming", "Static"),
			"MaxAudioChannels": capabilityText, "MinSegments": capabilityInt32, "SegmentLength": capabilityInt32,
			"BreakOnNonKeyFrames": capabilityBool, "AllowInterlacedVideoStreamCopy": capabilityBool,
			"ManifestSubtitles": capabilityText, "MaxManifestSubtitles": capabilityInt32,
			"MaxWidth": capabilityInt32, "MaxHeight": capabilityInt32, "FillEmptySubtitleSegments": capabilityBool,
		})),
		"ContainerProfiles": capabilityArray(capabilityObject(map[string]*capabilityShape{
			"Type": capabilityKind, "Conditions": capabilityConditions, "Container": capabilityText,
		})),
		"CodecProfiles": capabilityArray(capabilityObject(map[string]*capabilityShape{
			"Type": capabilityEnum("Video", "VideoAudio", "Audio"), "Conditions": capabilityConditions,
			"ApplyConditions": capabilityConditions, "Codec": capabilityText, "Container": capabilityText,
		})),
		"ResponseProfiles": capabilityArray(capabilityObject(map[string]*capabilityShape{
			"Container": capabilityText, "AudioCodec": capabilityText, "VideoCodec": capabilityText,
			"Type": capabilityKind, "OrgPn": capabilityText, "MimeType": capabilityText, "Conditions": capabilityConditions,
		})),
		"SubtitleProfiles": capabilityArray(capabilityObject(map[string]*capabilityShape{
			"Format": capabilityText, "Method": capabilityEnum("Encode", "Embed", "External", "Hls", "VideoSideData"),
			"DidlMode": capabilityText, "Language": capabilityText, "Container": capabilityText,
			"AllowChunkedResponse": capabilityBool, "Protocol": capabilityText,
		})),
	})
	clientCapabilityShape = capabilityObject(map[string]*capabilityShape{
		"PlayableMediaTypes": capabilityArray(capabilityText), "SupportedCommands": capabilityArray(capabilityText),
		"SupportsMediaControl": capabilityBool, "PushToken": capabilityText, "PushTokenType": capabilityText,
		"SupportsSync": capabilityBool, "DeviceProfile": capabilityDeviceProfile,
		"IconUrl": capabilityText, "AppId": capabilityText,
	})
)

func sanitizeCapabilityValue(value any, shape *capabilityShape, path string) (any, error) {
	if value == nil {
		return nil, nil
	}
	invalid := func() (any, error) {
		return nil, capabilityInputError(path + " has an invalid type or value")
	}
	switch shape.kind {
	case 'o':
		object, ok := value.(map[string]any)
		if !ok {
			return invalid()
		}
		cleaned := make(map[string]any)
		for key, child := range object {
			childShape, known := shape.fields[key]
			if !known || child == nil {
				continue
			}
			sanitized, err := sanitizeCapabilityValue(child, childShape, path+"."+key)
			if err != nil {
				return nil, err
			}
			cleaned[key] = sanitized
		}
		return cleaned, nil
	case 'a':
		values, ok := value.([]any)
		if !ok {
			return invalid()
		}
		cleaned := make([]any, 0, len(values))
		for _, child := range values {
			if child == nil {
				return invalid()
			}
			sanitized, err := sanitizeCapabilityValue(child, shape.element, path+"[]")
			if err != nil {
				return nil, err
			}
			cleaned = append(cleaned, sanitized)
		}
		return cleaned, nil
	case 's':
		text, ok := value.(string)
		if !ok {
			return invalid()
		}
		if len(shape.enum) > 0 {
			for _, choice := range shape.enum {
				if text == choice {
					return text, nil
				}
			}
			return invalid()
		}
		return text, nil
	case 'b':
		if boolean, ok := value.(bool); ok {
			return boolean, nil
		}
	case 'n':
		if number, ok := value.(json.Number); ok {
			integer, err := number.Int64()
			if err == nil && integer >= 0 && integer <= shape.maximum {
				return number, nil
			}
		}
	}
	return invalid()
}
