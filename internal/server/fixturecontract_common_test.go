package server

// Explicit fixture contracts preserve the existing concrete types and optional
// capabilities. They promise joined ProbeFile borrowing, not OS retirement.
// Future source work must finish before return; source-bearing children must
// use and join the actual media gateway rather than an external process.

// ProbeFile checks cancellation and Stat synchronously, then returns facts.
func (apiMediaProber) ProbeFileJoinedContract() bool { return true }

// The synchronous API fixture returns before counter/chapter facts are updated.
func (prober *adminScanCountingProber) ProbeFileJoinedContract() bool { return prober != nil }
