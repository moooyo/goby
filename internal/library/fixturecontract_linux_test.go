//go:build linux

package library

// These fixture declarations retain each original method set. They cover only
// joined ProbeFile borrowing; the actual callback collector supplies retirement
// receipts. Future source work must finish synchronously or use and join the
// actual media process gateway. Semantic success is no OS retirement proof.

// The counter override delegates to the real prober with the collector's
// context. Its explicit marker prevents a promoted Owned method from skipping
// that dynamic override, while all real children remain gateway-owned.
func (prober *catalogRescanRealProber) ProbeFileJoinedContract() bool { return prober != nil }

// ProbeFile synchronously delegates to the source fixture and appends facts
// under its mutex. The separate artwork executor is outside this declaration.
func (prober *embeddedArtworkFixtureProber) ProbeFileJoinedContract() bool { return prober != nil }

// The source delegate joins before the bounded publication notification.
func (prober *extraPublicationProber) ProbeFileJoinedContract() bool {
	return prober != nil && prober.fixture.ProbeFileJoinedContract()
}

// The source fixture joins before chapter facts are appended.
func (introFixtureProber) ProbeFileJoinedContract() bool { return true }

// The source fixture joins before synthetic subtitle facts are appended.
func (mediaOCRFixtureProber) ProbeFileJoinedContract() bool { return true }

// An arbitrary inner Prober is not implicitly joined. All current inners have
// their own explicit fixture declarations, so retain that dynamic contract and
// reject nil or unknown inners. An Owned method alone does not declare the
// inner's separately dispatched ProbeFile override to be synchronous.
func (prober mediaSourceTestProber) ProbeFileJoinedContract() bool {
	joined, ok := prober.inner.(JoinedProbeCallbacks)
	return ok && joined.ProbeFileJoinedContract()
}

// Preserve the real-prober counting override and its optional version methods;
// the callback collector owns every real child through the inherited context.
func (prober *rootBindingFullScanRealProber) ProbeFileJoinedContract() bool { return prober != nil }

// Profile counters finish in the override's defer. Real source reads and all
// sequential children stay inside the real prober and actual callback cohort.
func (prober *scanProbeProfileProber) ProbeFileJoinedContract() bool { return prober != nil }
