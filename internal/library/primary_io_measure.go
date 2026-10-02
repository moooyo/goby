//go:build primary_io_measure

package library

import "github.com/moooyo/goby/internal/primaryio"

// PrimaryReadMeasurementSnapshot contains process-wide scalar observations from
// the existing original-media and download read runtimes. Its independently
// locked fields are not an atomic snapshot and do not establish actual peaks.
type PrimaryReadMeasurementSnapshot struct {
	IO           primaryio.Stats
	Owners       primaryio.RuntimeStats
	DomainClaims int
}

// PrimaryReadMeasurementStats returns read-only values without exposing routes,
// runtime controls, or mutable state. Zero observations establish idle counts
// only after the caller has joined actual consumers and Store cleanup; polling
// cannot prove exact retirement time or substitute for those lifetime joins.
func PrimaryReadMeasurementStats() PrimaryReadMeasurementSnapshot {
	io := originalMediaReadGovernor.Stats()
	owners := originalMediaReadOwners.Stats()
	originalMediaReadDomains.mu.Lock()
	claims := len(originalMediaReadDomains.claims)
	originalMediaReadDomains.mu.Unlock()
	return PrimaryReadMeasurementSnapshot{IO: io, Owners: owners, DomainClaims: claims}
}
