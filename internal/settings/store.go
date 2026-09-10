package settings

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// Store is one server's settings publisher. Its defaults never change during
// that server lifetime. Direct SQL edits are not a supported hot-reload path.
type Store struct {
	owner         library.OwnedTransactions
	defaults      Values
	hostName      string
	publicationMu sync.Mutex
	current       atomic.Pointer[Snapshot]
}

// New loads the migration-created singleton after catalog ownership has been
// acquired. It never copies startup defaults into persisted overrides.
func New(ctx context.Context, pool *pgxpool.Pool, owner library.OwnedTransactions, defaults Values, hostName string) (*Store, error) {
	if pool == nil || owner == nil {
		return nil, fmt.Errorf("%w: settings pool and transaction owner are required", ErrInvalidInput)
	}
	if err := validateValues(defaults); err != nil {
		return nil, err
	}
	if err := validateName(ServerNameCustom, &hostName); err != nil {
		return nil, &ValidationError{Fields: map[string]string{"HostName": "supply a valid public host-name fallback"}}
	}
	store := &Store{owner: owner, defaults: defaults, hostName: hostName}
	var initial Snapshot
	err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		record, err := readRecord(tx.QueryRow("SELECT " + recordColumns + " FROM managed_settings WHERE id = 1"))
		if err != nil {
			return err
		}
		initial, err = store.materialize(record)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("load managed settings: %w", err)
	}
	store.publish(initial)
	return store, nil
}

// Snapshot performs no I/O. Successful construction guarantees a snapshot;
// every returned override pointer is a private copy owned by the caller.
func (s *Store) Snapshot() Snapshot {
	return cloneSnapshot(*s.current.Load())
}

// Get checks current administrator authority and returns the same published
// state consumed by runtime readers, not an independently read database view.
func (s *Store) Get(ctx context.Context, actor Actor) (Snapshot, error) {
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	var result Snapshot
	err := s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if err := checkActor(tx, actor, true); err != nil {
			return err
		}
		result = s.Snapshot()
		return checkActor(tx, actor, false)
	})
	if err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

// Update replaces the five native overrides with the selected name mode.
// Omitted NameMode keeps the schema-20 name contract, and omitted Encoding
// preserves its independent value. Explicit defaults remain explicit overrides.
func (s *Store) Update(ctx context.Context, actor Actor, request UpdateRequest) (Snapshot, error) {
	if err := validateRevision(request.Revision); err != nil {
		return Snapshot{}, err
	}
	overrides := cloneOverrides(request.Overrides)
	mode := inferredNameMode(overrides.ServerName)
	if request.NameMode != nil {
		mode = *request.NameMode
	}
	if err := validateName(mode, overrides.ServerName); err != nil {
		return Snapshot{}, err
	}
	if err := validateValues(effectiveValues(s.defaults, overrides, mode, s.hostName)); err != nil {
		return Snapshot{}, err
	}
	encoding := clonePointer(request.Encoding)
	if encoding != nil {
		if err := validateEncoding(*encoding); err != nil {
			return Snapshot{}, err
		}
	}
	return s.change(ctx, actor, &request.Revision, func(_ library.OwnedTx, previous settingsRecord) (settingsRecord, error) {
		previous.Overrides, previous.ServerNameMode = overrides, mode
		if encoding != nil {
			previous.Encoding = *encoding
		}
		return previous, nil
	})
}

// Reset restores only the named fields. ServerName returns to Deployment and
// the compatibility width returns to zero. Empty or ambiguous selections fail.
func (s *Store) Reset(ctx context.Context, actor Actor, request ResetRequest) (Snapshot, error) {
	if err := validateRevision(request.Revision); err != nil {
		return Snapshot{}, err
	}
	fields := append([]Field(nil), request.Fields...)
	if err := validateReset(fields); err != nil {
		return Snapshot{}, err
	}
	return s.change(ctx, actor, &request.Revision, func(_ library.OwnedTx, previous settingsRecord) (settingsRecord, error) {
		previous.Overrides = clearFields(previous.Overrides, fields)
		for _, field := range fields {
			if field == FieldServerName {
				previous.ServerNameMode = ServerNameDeployment
			} else if field == FieldTranscodingMaxWidth {
				previous.Encoding = Encoding{}
			}
		}
		return previous, nil
	})
}

func (s *Store) change(ctx context.Context, actor Actor, revision *int64, replacement func(library.OwnedTx, settingsRecord) (settingsRecord, error)) (Snapshot, error) {
	// The lock extends beyond COMMIT through publication. An older committed
	// writer cannot resume later and overwrite a newer runtime snapshot.
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	var committed Snapshot
	err := s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if err := checkActor(tx, actor, true); err != nil {
			return err
		}
		previous, err := readRecord(tx.QueryRow("SELECT " + recordColumns + " FROM managed_settings WHERE id = 1 FOR UPDATE"))
		if err != nil {
			return err
		}
		if err := checkActor(tx, actor, false); err != nil {
			return err
		}
		if revision != nil && previous.Revision != *revision {
			return ErrRevisionConflict
		}
		current := s.current.Load()
		if current == nil || previous.Revision != current.Revision || !equalOverrides(previous.Overrides, current.Overrides) ||
			previous.ServerNameMode != current.ServerNameMode || previous.Encoding != current.Encoding {
			return fmt.Errorf("%w: persisted state differs from the published application state", ErrStoredSettings)
		}
		next := previous
		next.Overrides = cloneOverrides(previous.Overrides)
		next, err = replacement(tx, next)
		if err != nil {
			return err
		}
		next.Overrides = cloneOverrides(next.Overrides)
		if err := s.validateRecordValues(next); err != nil {
			return err
		}
		if equalOverrides(previous.Overrides, next.Overrides) && previous.ServerNameMode == next.ServerNameMode && previous.Encoding == next.Encoding {
			// A no-op still validates the caller's revision and live authority.
			committed, err = s.materialize(previous)
			if err != nil {
				return err
			}
		} else {
			if err := validateRevision(previous.Revision); err != nil {
				return err
			}
			changed, err := readRecord(tx.QueryRow(`UPDATE managed_settings SET
				revision = revision + 1, server_name = $2, max_bitrate = $3,
				max_width = $4, max_height = $5, max_audio_channels = $6,
				server_name_mode = $7, compatibility_max_width = $8,
				updated_at = clock_timestamp() WHERE id = 1 AND revision = $1
				RETURNING `+recordColumns, previous.Revision, next.Overrides.ServerName, next.Overrides.MaxBitrate,
				next.Overrides.MaxWidth, next.Overrides.MaxHeight, next.Overrides.MaxAudioChannels,
				next.ServerNameMode, next.Encoding.TranscodingMaxWidth))
			if err != nil {
				return err
			}
			committed, err = s.materialize(changed)
			if err != nil {
				return err
			}
		}
		// This must remain the final database operation before owner COMMIT.
		return checkActor(tx, actor, false)
	})
	if err != nil {
		return Snapshot{}, err
	}
	// A cancelled HTTP caller cannot cancel this synchronous post-commit step.
	// A process crash here is recovered by New loading committed overrides.
	return s.publish(committed), nil
}

// publish is also monotonic independently of the writer mutex. It returns the
// actually published value and never shares its stored override pointers.
func (s *Store) publish(value Snapshot) Snapshot {
	copy := cloneSnapshot(value)
	for {
		previous := s.current.Load()
		if previous != nil && previous.Revision >= copy.Revision {
			return cloneSnapshot(*previous)
		}
		if s.current.CompareAndSwap(previous, &copy) {
			return cloneSnapshot(copy)
		}
	}
}

const recordColumns = "revision, server_name, max_bitrate, max_width, max_height, max_audio_channels, updated_at, server_name_mode, compatibility_max_width"

type settingsRecord struct {
	Revision       int64
	Overrides      Overrides
	UpdatedAt      time.Time
	ServerNameMode ServerNameMode
	Encoding       Encoding
}

type rowScanner interface{ Scan(...any) error }

func readRecord(row rowScanner) (settingsRecord, error) {
	var record settingsRecord
	err := row.Scan(&record.Revision, &record.Overrides.ServerName, &record.Overrides.MaxBitrate,
		&record.Overrides.MaxWidth, &record.Overrides.MaxHeight, &record.Overrides.MaxAudioChannels, &record.UpdatedAt,
		&record.ServerNameMode, &record.Encoding.TranscodingMaxWidth)
	if errors.Is(err, pgx.ErrNoRows) {
		return settingsRecord{}, fmt.Errorf("%w: managed settings singleton is missing", ErrStoredSettings)
	}
	if err != nil {
		return settingsRecord{}, fmt.Errorf("read managed settings: %w", err)
	}
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record, nil
}

func (s *Store) materialize(record settingsRecord) (Snapshot, error) {
	if record.Revision < 1 {
		return Snapshot{}, ErrStoredSettings
	}
	if err := s.validateRecordValues(record); err != nil {
		// Invalid persisted state is an operational fault, not a bad current
		// HTTP request. Do not make it match ErrInvalidInput through wrapping.
		return Snapshot{}, fmt.Errorf("%w: %v", ErrStoredSettings, err)
	}
	return Snapshot{Revision: record.Revision, Defaults: s.defaults,
		Overrides: cloneOverrides(record.Overrides), Effective: effectiveValues(s.defaults, record.Overrides, record.ServerNameMode, s.hostName),
		ServerNameMode: record.ServerNameMode, HostName: s.hostName, Encoding: record.Encoding, UpdatedAt: record.UpdatedAt.UTC()}, nil
}

func (s *Store) validateRecordValues(record settingsRecord) error {
	if err := validateName(record.ServerNameMode, record.Overrides.ServerName); err != nil {
		return err
	}
	if err := validateEncoding(record.Encoding); err != nil {
		return err
	}
	return validateValues(effectiveValues(s.defaults, record.Overrides, record.ServerNameMode, s.hostName))
}

// The adapter preserves the owner's protected query context and exposes no
// connection or transaction-control capability to the identity helper.
type authorizationTx struct{ tx library.OwnedTx }

func (adapter authorizationTx) QueryRow(_ context.Context, statement string, args ...any) pgx.Row {
	return adapter.tx.QueryRow(statement, args...)
}

var _ identity.AuthorizationTx = authorizationTx{}

func checkActor(tx library.OwnedTx, actor Actor, lock bool) error {
	return identity.CheckAdministrator(context.Background(), authorizationTx{tx: tx}, actor.Principal, actor.Audience, lock)
}
