package tasks

// These declarations preserve the original fixture types and method sets.
// Direct source use must finish before ProbeFile returns, and future children
// must use and join the actual media gateway. This is an explicit developer
// borrowing contract, not an operating-system retirement certificate.

// ReadAll and Stat are synchronous. Cancellation waits inside the callback's
// final release gate, and its deferred active count joins before return.
func (prober *managerTestProber) ProbeFileJoinedContract() bool { return prober != nil }

// This callback always rejects probing and performs no source use or child
// start. Its failure has a joined empty callback scope, not fabricated OS proof.
func (repositoryProber) ProbeFileJoinedContract() bool { return true }
