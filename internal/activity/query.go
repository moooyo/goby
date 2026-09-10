package activity

import (
	"encoding/json"
	"fmt"
	"time"
	"unicode"
	"unicode/utf8"
)

// Row is the read-only result supplied by an already authorized transaction.
// A function adapter avoids coupling this package to the catalog owner types.
type Row interface {
	Scan(...any) error
}

const queryEntries = `WITH filtered AS (
	SELECT id,created_at,action,severity,source,actor_kind,actor_id,actor_credential_id,
		resource_kind,resource_id,request_id,revision,affected_count,state,changed_fields
	FROM activity_entries
	WHERE ($1::timestamptz IS NULL OR created_at >= $1)
		AND ($2 = '' OR severity = $2)
		AND ($3 = '' OR action = $3)
		AND ($4 = '' OR actor_id = $4)
), page AS (
	SELECT * FROM filtered ORDER BY created_at DESC,id DESC LIMIT $5 OFFSET $6
)
SELECT (SELECT count(*) FROM filtered), COALESCE((
	SELECT jsonb_agg(jsonb_build_object(
		'ID',p.id,'Date',p.created_at,'Action',p.action,'Severity',p.severity,'Source',p.source,
		'Actor',jsonb_build_object('Kind',p.actor_kind,'ID',p.actor_id,'CredentialID',p.actor_credential_id),
		'Resource',jsonb_build_object('Kind',p.resource_kind,'ID',p.resource_id),
		'RequestID',p.request_id,'Revision',p.revision,'Count',p.affected_count,
		'State',p.state,'ChangedFields',p.changed_fields,'ActorName',COALESCE(u.name,''))
		ORDER BY p.created_at DESC,p.id DESC)
	FROM page p LEFT JOIN users u ON p.actor_kind = 'user' AND u.id = p.actor_id
), '[]'::jsonb)`

// QueryOwned returns a bounded page and matching total from one SQL snapshot.
// Its owner supplies authorization and transaction lifetime. MinDate is
// inclusive even between PostgreSQL microseconds: round the lower bound up,
// never down, before pgx encodes it at database precision.
func QueryOwned(queryRow func(string, ...any) Row, options QueryOptions) (Page, error) {
	if queryRow == nil {
		return Page{}, ErrUnavailable
	}
	if options.Limit == 0 {
		options.Limit = DefaultPageLimit
	}
	if options.StartIndex < 0 || options.StartIndex > MaxStartIndex ||
		options.Limit < 1 || options.Limit > MaxPageLimit ||
		(options.Severity != "" && !validSeverity(options.Severity)) ||
		(options.Action != "" && !validAction(options.Action)) || !validIdentifier(options.ActorID, true) {
		return Page{}, ErrInvalidInput
	}
	var minDate any
	if options.MinDate != nil {
		date := options.MinDate.UTC()
		if date.Year() < 1 || date.Year() > 9999 {
			return Page{}, ErrInvalidInput
		}
		if date.Nanosecond()%1000 != 0 {
			date = date.Truncate(time.Microsecond).Add(time.Microsecond)
		}
		if date.Year() > 9999 {
			return Page{}, ErrInvalidInput
		}
		minDate = date
	}
	result := Page{Items: []Entry{}, StartIndex: options.StartIndex, Limit: options.Limit}
	var raw []byte
	row := queryRow(queryEntries, minDate, string(options.Severity), string(options.Action), options.ActorID, options.Limit, options.StartIndex)
	if row == nil {
		return Page{}, ErrUnavailable
	}
	if err := row.Scan(&result.TotalRecordCount, &raw); err != nil {
		return Page{}, fmt.Errorf("query activity entries: %w", err)
	}
	if err := json.Unmarshal(raw, &result.Items); err != nil {
		return Page{}, fmt.Errorf("decode activity entries: %w", err)
	}
	if result.Items == nil {
		result.Items = []Entry{}
	}
	for index := range result.Items {
		entry := &result.Items[index]
		entry.Date = entry.Date.UTC()
		if entry.ChangedFields == nil {
			entry.ChangedFields = []Field{}
		}
		if !validDisplayName(entry.ActorName) {
			entry.ActorName = ""
		}
		entry.Name, entry.Overview = description(entry.Action, entry.State)
	}
	return result, nil
}

func validDisplayName(value string) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 128 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func description(action Action, state State) (string, string) {
	switch action {
	case ActionUserCreated:
		return "User created", "A user account was created."
	case ActionUserUpdated:
		return "User updated", "A user account or its access policy was updated."
	case ActionUserPasswordReset:
		return "User password reset", "A user password was reset and affected credentials were revoked."
	case ActionSessionLogin:
		return "Session issued", "A login completed and its session was issued."
	case ActionSessionRevoked:
		return "Session revoked", "A session was revoked."
	case ActionApplicationKeyCreated:
		return "Application key created", "An application key was created."
	case ActionApplicationKeyRevealed:
		return "Application key revealed", "An application key was decrypted and prepared for authorized display."
	case ActionApplicationKeyRevoked:
		return "Application key revoked", "An application key was revoked."
	case ActionDeviceUpdated:
		return "Device updated", "Device display options were updated."
	case ActionDeviceRemoved:
		return "Device removed", "A device registration was removed and its affected sessions were revoked."
	case ActionLibraryCreated:
		return "Library created", "A media library was registered."
	case ActionLibraryRemoved:
		return "Library removed", "A media library was removed from the catalog; media files were retained."
	case ActionScanRequested:
		return "Library scan requested", "A library scan was durably queued."
	case ActionScanCancelRequested:
		return "Scan cancellation requested", "A library scan cancellation request was persisted."
	case ActionScanFinished:
		return terminalDescription("Library scan", state)
	case ActionMetadataUpdated:
		return "Metadata updated", "Item metadata controls were updated."
	case ActionSettingsUpdated:
		return "Server settings updated", "Supported server settings were updated."
	case ActionTaskAdmitted:
		return "Task admitted", "A new task execution was durably admitted."
	case ActionTaskCancelRequested:
		return "Task cancellation requested", "A task cancellation request was persisted."
	case ActionTaskFinished:
		return terminalDescription("Task", state)
	case ActionTaskScheduleUpdated:
		return "Task schedule updated", "A task schedule was replaced."
	default:
		return "Activity recorded", "A server activity was recorded."
	}
}

func terminalDescription(subject string, state State) (string, string) {
	switch state {
	case StateCompleted:
		return subject + " completed", "The execution completed."
	case StateFailed:
		return subject + " failed", "The execution failed."
	case StateCancelled:
		return subject + " cancelled", "The execution was cancelled."
	case StateInterrupted:
		return subject + " interrupted", "The execution was interrupted."
	default:
		return subject + " finished", "The execution reached a terminal state."
	}
}
