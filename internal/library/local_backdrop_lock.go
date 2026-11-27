package library

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/storagebinding"
)

const (
	localBackdropLockKey        = "maintenance.local_backdrop_lock.v1"
	localBackdropLockWindow     = 2 * time.Minute
	localBackdropLockMaxBytes   = 64 << 10
	maxLocalBackdropLockAliases = 8
)

var (
	ErrLocalBackdropLocked      = errors.New("the local backdrop is protected by an execution lock")
	ErrLocalBackdropLockExpired = errors.New("the local backdrop lock expired and requires explicit release")
	ErrLocalBackdropLockChanged = errors.New("the local backdrop lock does not match the request")
)

type LocalBackdropLockInfo struct {
	ItemID    string    `json:"ItemId"`
	LockID    string    `json:"LockId"`
	ExpiresAt time.Time `json:"ExpiresAt"`
}

// One durable maintenance record per catalog is sufficient. It stays in the
// exact-key server_settings namespace (like recovery control), not the managed
// settings API. Restarts and restores retain the hold, even when it has expired.
type localBackdropLock struct {
	Info           LocalBackdropLockInfo `json:"info"`
	SessionID      string                `json:"session"`
	LibraryID      string                `json:"library"`
	RootID         string                `json:"root"`
	Binding        int64                 `json:"binding"`
	Revision       int64                 `json:"revision"`
	ScanID         string                `json:"scan"`
	SourceKey      string                `json:"source"`
	Directory      string                `json:"directory"`
	DirectoryInode string                `json:"directory_identity"`
	Aliases        []string              `json:"aliases,omitempty"`
}

func newLocalBackdropLock(target localBackdropTarget, session, token string, expires time.Time) *localBackdropLock {
	scope := target.workScope()
	return &localBackdropLock{
		Info:      LocalBackdropLockInfo{ItemID: target.item.id, LockID: token, ExpiresAt: expires.UTC()},
		SessionID: session, LibraryID: target.item.libraryID, RootID: target.item.rootID,
		Binding: target.root.revision, Revision: target.generation.revision,
		ScanID: target.queue.jobID, SourceKey: target.queue.sourceKey, Directory: scope.directory, DirectoryInode: scope.inode,
	}
}

func validLocalBackdropLockID(value string) bool {
	return len(value) == 32 && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f')
	}) < 0
}

func (lock *localBackdropLock) valid() bool {
	if len(lock.Aliases) > maxLocalBackdropLockAliases {
		return false
	}
	for i, alias := range lock.Aliases {
		if !storagebinding.ValidAbsolutePath(alias) || alias == lock.Directory || i > 0 && lock.Aliases[i-1] >= alias {
			return false
		}
	}
	return validCatalogLibraryIdentifier(lock.Info.ItemID) && validLocalBackdropLockID(lock.Info.LockID) && !lock.Info.ExpiresAt.IsZero() &&
		validCatalogLibraryIdentifier(lock.SessionID) && validCatalogLibraryIdentifier(lock.LibraryID) && validCatalogLibraryIdentifier(lock.RootID) &&
		validCatalogLibraryIdentifier(lock.ScanID) && lock.Binding > 0 && lock.Revision > 0 &&
		len(lock.SourceKey) > 0 && len(lock.SourceKey) <= 32768 && len(lock.DirectoryInode) > 0 && len(lock.DirectoryInode) <= 128 &&
		storagebinding.ValidAbsolutePath(lock.Directory)
}

func readLocalBackdropLock(row rowScanner) (*localBackdropLock, error) {
	var raw *string
	err := row.Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lock localBackdropLock
	if raw == nil || json.Unmarshal([]byte(*raw), &lock) != nil || !lock.valid() {
		return nil, ErrUnavailable
	}
	return &lock, nil
}

const localBackdropLockReadSQL = `SELECT CASE WHEN octet_length(value)<=$2 THEN value END FROM server_settings WHERE key=$1`

func readDurableLocalBackdropLock(ctx context.Context, tx pgx.Tx) (*localBackdropLock, error) {
	return readLocalBackdropLock(tx.QueryRow(ctx, localBackdropLockReadSQL, localBackdropLockKey, localBackdropLockMaxBytes))
}

func (lock *localBackdropLock) same(other *localBackdropLock) bool {
	return other != nil && lock.Info.ItemID == other.Info.ItemID && lock.Info.LockID == other.Info.LockID &&
		lock.Info.ExpiresAt.Equal(other.Info.ExpiresAt) && lock.SessionID == other.SessionID && lock.LibraryID == other.LibraryID &&
		lock.RootID == other.RootID && lock.Binding == other.Binding && lock.Revision == other.Revision && lock.ScanID == other.ScanID &&
		lock.SourceKey == other.SourceKey && lock.Directory == other.Directory && lock.DirectoryInode == other.DirectoryInode &&
		lock.sameAliases(other.Aliases)
}

func (lock *localBackdropLock) sameAliases(aliases []string) bool {
	return slices.Equal(lock.Aliases, aliases)
}

func (s *Store) restoreLocalBackdropLock(ctx context.Context) error {
	lock, err := readLocalBackdropLock(s.ownership.conn.QueryRow(ctx, localBackdropLockReadSQL, localBackdropLockKey, localBackdropLockMaxBytes))
	if err == nil {
		s.backdropExecution.lock = lock
	}
	return err
}

func (lock *localBackdropLock) matches(target localBackdropTarget) bool {
	scope := target.workScope()
	return lock.Info.ItemID == scope.itemID && lock.LibraryID == target.item.libraryID && lock.RootID == target.root.root.id &&
		lock.Binding == target.root.revision && lock.Revision == target.generation.revision && lock.ScanID == target.queue.jobID &&
		lock.SourceKey == target.queue.sourceKey && lock.DirectoryInode == scope.inode && lock.Directory == scope.directory
}

type localBackdropWorkScope struct{ itemID, directory, inode string }

func (target localBackdropTarget) workScope() localBackdropWorkScope {
	return localBackdropWorkScope{target.item.id, filepath.Join(target.root.root.path, target.item.directory(target.root.root)), target.queue.directoryIdentity}
}

func (lock *localBackdropLock) covers(scope localBackdropWorkScope) bool {
	return lock.Info.ItemID == scope.itemID || lock.Directory == scope.directory || lock.DirectoryInode == scope.inode || slices.Contains(lock.Aliases, scope.directory)
}

// Scanner admission and record changes serialize on the catalog owner. Local
// importers are finer grained: pin their scoped source use through retirement,
// including a worker whose already-read queue page predates the lock. Never
// wait for SQL, filesystem work, or an external operator under this mutex.
type localBackdropExecution struct {
	mu       sync.Mutex
	lock     *localBackdropLock
	changing bool
	reread   bool
	workers  map[localBackdropWorkScope]int
}

func (gate *localBackdropExecution) current() *localBackdropLock {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.lock
}

func (gate *localBackdropExecution) beginWork(scope localBackdropWorkScope) (func(), bool) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.lock != nil && gate.lock.covers(scope) {
		return nil, false
	}
	if gate.workers == nil {
		gate.workers = make(map[localBackdropWorkScope]int)
	}
	gate.workers[scope]++
	return func() {
		gate.mu.Lock()
		defer gate.mu.Unlock()
		if gate.workers[scope] == 1 {
			delete(gate.workers, scope)
		} else {
			gate.workers[scope]--
		}
	}, true
}

func (gate *localBackdropExecution) reserve(lock *localBackdropLock) error {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.lock != nil {
		return ErrLocalBackdropLocked
	}
	for scope := range gate.workers {
		if lock.covers(scope) {
			return ErrBusy
		}
	}
	gate.lock, gate.changing = lock, true
	return nil
}

func (gate *localBackdropExecution) pin(itemID, lockID, sessionID string) (*localBackdropLock, func(), error) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	lock := gate.lock
	if lock == nil || lock.Info.ItemID != itemID || lock.Info.LockID != lockID || lock.SessionID != sessionID {
		return nil, nil, ErrLocalBackdropLockChanged
	}
	if gate.changing || gate.reread {
		return nil, nil, ErrBusy
	}
	if !time.Now().Before(lock.Info.ExpiresAt) {
		return nil, nil, ErrLocalBackdropLockExpired
	}
	gate.reread = true
	return lock, func() {
		gate.mu.Lock()
		gate.reread = false
		gate.mu.Unlock()
	}, nil
}

func (gate *localBackdropExecution) startRelease(itemID, lockID string) error {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.lock == nil || gate.lock.Info.ItemID != itemID || gate.lock.Info.LockID != lockID {
		return ErrLocalBackdropLockChanged
	}
	if gate.changing || gate.reread {
		return ErrBusy
	}
	gate.changing = true
	return nil
}

func (gate *localBackdropExecution) finishChange(keep bool) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if !keep {
		gate.lock = nil
	}
	gate.changing = false
}

// Compare canonical paths by component. A full scan of an overlapping root is
// indivisible: even admitting its job could supersede the accepted generation.
// A different library without an overlapping root keeps normal admission.
const localBackdropScanOverlapsDirectorySQL = `EXISTS(SELECT 1 FROM unnest($2::text[]) held(path)
	WHERE r.path COLLATE "C"=held.path OR r.path='/' OR held.path='/'
	OR starts_with(held.path COLLATE "C",r.path||'/') OR starts_with(r.path COLLATE "C",held.path||'/'))`

func (lock *localBackdropLock) directories() []string {
	return append([]string{lock.Directory}, lock.Aliases...)
}

func (s *Store) localBackdropBlocksScan(tx OwnedTx, libraryID string) (bool, error) {
	lock := s.backdropExecution.current()
	if lock == nil {
		return false, nil
	}
	if lock.LibraryID == libraryID {
		return true, nil
	}
	var contains bool
	err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM library_roots r WHERE r.library_id=$1 AND `+
		localBackdropScanOverlapsDirectorySQL+`)`, libraryID, lock.directories()).Scan(&contains)
	return contains, err
}

func (s *Store) GetLocalBackdropLock(ctx context.Context, actor identity.Principal, itemID string) (LocalBackdropLockInfo, error) {
	if ctx == nil || !validCatalogLibraryIdentifier(itemID) {
		return LocalBackdropLockInfo{}, ErrInvalidInput
	}
	tx, err := s.beginAnalysisAdminRead(ctx, actor)
	if err != nil {
		return LocalBackdropLockInfo{}, err
	}
	defer rollback(tx)
	lock, err := readDurableLocalBackdropLock(ctx, tx)
	if err != nil {
		return LocalBackdropLockInfo{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return LocalBackdropLockInfo{}, err
	}
	if err := s.finishAnalysisAdminRead(ctx, actor); err != nil {
		return LocalBackdropLockInfo{}, err
	}
	if lock == nil || lock.Info.ItemID != itemID {
		return LocalBackdropLockInfo{}, ErrNotFound
	}
	return lock.Info, nil
}
