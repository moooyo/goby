//go:build linux

package server

// Every declaration covers the dynamic ProbeFile callback on the original
// fixture type, retaining its existing version and executor capabilities.
// Direct source use must join before return. Any future source-bearing child
// must use and join the actual media gateway; nil error is no OS certificate.

// The API delegate and source Stat finish before version facts are returned.
func (analysisHTTPProber) ProbeFileJoinedContract() bool { return true }

// The intro delegate joins before synthetic subtitle facts are appended.
func (adminMediaOperationHTTPProber) ProbeFileJoinedContract() bool { return true }

// Explicitly declare this override instead of inheriting its embedded marker:
// the scheduled-task gate joins first, then source Stat/version facts finish.
func (prober adminRefreshMediaHTTPProber) ProbeFileJoinedContract() bool {
	return prober.scheduledTaskHTTPProber != nil && prober.scheduledTaskHTTPProber.ProbeFileJoinedContract()
}

// The analysis delegate and bounded ReadAt both finish inside ProbeFile.
func (analysisProjectionProber) ProbeFileJoinedContract() bool { return true }

// Cancellation and source Stat are synchronous; the remaining facts are local.
func (applicationKeyCatalogProber) ProbeFileJoinedContract() bool { return true }

// ProbeFile checks cancellation and Stat before constructing local AV facts.
func (applicationMediaProber) ProbeFileJoinedContract() bool { return true }

// Source Stat and chapter facts finish before this callback returns.
func (introHTTPProber) ProbeFileJoinedContract() bool { return true }

// ProbeFile checks cancellation and Stat synchronously before returning facts.
func (playbackHTTPProber) ProbeFileJoinedContract() bool { return true }

// Cancellation remains inside the gate until the test explicitly releases its
// final cleanup phase. The API source delegate also joins before return.
func (prober *scheduledTaskHTTPProber) ProbeFileJoinedContract() bool { return prober != nil }

// Cancellation, source Stat, and local stream facts finish synchronously.
func (streamHTTPProber) ProbeFileJoinedContract() bool { return true }
