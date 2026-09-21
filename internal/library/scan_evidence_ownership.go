package library

import (
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"
)

type retainedScanEvidenceOwner struct {
	connection *pgx.Conn
	manager    *scanEvidenceManager
}

// A cleanup failure is not a successful ownership transfer. Strong process-wide
// references preserve the raw PostgreSQL session and failed filesystem owner
// even when New returns no Store or a failed generation is otherwise discarded.
// No production path removes these entries or releases their retained fences.
// PostgreSQL losing the session does not release the manager's filesystem lock.
var retainedScanEvidenceOwners = struct {
	sync.Mutex
	owners map[*scanOwnership]retainedScanEvidenceOwner
}{owners: make(map[*scanOwnership]retainedScanEvidenceOwner)}

// The ordinary Close caller has joined scan workers and file deletions; startup
// failure calls this before workers exist. The manager itself joins every
// constructor and actual retirement before this function can detach ownership.
func (s *Store) retireScanEvidenceOwnership() error {
	cleanupErr := s.scanEvidence.close()
	if cleanupErr == nil {
		return s.ownership.release()
	}
	s.ownership.retainFailedScanEvidence(s.scanEvidence)
	return errors.Join(ErrUnavailable, cleanupErr)
}

func (ownership *scanOwnership) retainFailedScanEvidence(manager *scanEvidenceManager) {
	if ownership == nil {
		return
	}
	// Owned transactions and private staging use this same mutex. Waiting for
	// it completes their current operation before subsequent use is fenced.
	ownership.mu.Lock()
	defer ownership.mu.Unlock()
	ownership.lost.Store(true)
	var connection *pgx.Conn
	if ownership.conn != nil {
		// Hijack only changes pool bookkeeping. It sends no SQL, releases no
		// advisory lock, and never returns dirty temporary state to a borrower.
		connection = ownership.conn.Hijack()
		ownership.conn = nil
	}
	retainedScanEvidenceOwners.Lock()
	defer retainedScanEvidenceOwners.Unlock()
	if _, exists := retainedScanEvidenceOwners.owners[ownership]; !exists {
		retainedScanEvidenceOwners.owners[ownership] = retainedScanEvidenceOwner{connection: connection, manager: manager}
	}
}
