package transcode

import (
	"errors"
	"time"
)

const (
	cacheMaintenanceGlobalBudget = 8192
	cacheMaintenanceJobBudget    = 512
	cacheMaintenanceMaxFacts     = 1 << 18
	cacheMaintenanceAuditLimit   = 5 * time.Minute
)

var errCacheCreationUnaccounted = errors.New("created cache directory could not be rolled back")

// cacheMaintenanceResult reports only descriptor-verified facts. Pending means
// a bounded batch could not close its change accounting; the manager preserves
// its previous charge and fences admission until a later batch closes it.
type cacheMaintenanceResult struct {
	bytes   int64
	ready   bool
	pending bool
	work    int
}
