package library

// These declarations cover ProbeFile's synchronous borrowing behavior only.
// They are not operating-system retirement certificates. Direct source use
// must finish before return, and every source-bearing child must use the actual
// media gateway and join there. Future fixture changes must keep this contract.

// The fixture reads the whole source synchronously. Its cancellation and
// release gates stay inside ProbeFile, and its deferred active count joins
// before the caller can retire the borrowed descriptor.
func (prober *libraryFixtureProber) ProbeFileJoinedContract() bool { return prober != nil }

// Every current scanProberFunc literal has been reviewed: source reads, Stat,
// fixture delegation, filesystem changes, and gates all finish before return.
// A gate that ignores cancellation still waits for release before returning.
// This named function type is an explicit developer contract for ALL future
// literals too; it must never wrap an unjoined goroutine or external reader.
// A future source-bearing child must use and join the actual media gateway.
func (prober scanProberFunc) ProbeFileJoinedContract() bool { return prober != nil }

// Every current versioned literal performs only synchronous source/fact work
// or waits inside its cancellation gate. All future literals must also join
// every direct source use and every actual media-gateway child before return;
// converting an arbitrary unjoined callback to this type violates the contract.
func (prober forceProbeVersionedFunc) ProbeFileJoinedContract() bool { return prober != nil }

// Keep the dynamic override: the embedded fixture joins its read first, then
// this method copies music facts under its own mutex before returning.
func (prober *metadataMusicScanProber) ProbeFileJoinedContract() bool {
	return prober != nil && prober.libraryFixtureProber.ProbeFileJoinedContract()
}

// Both the delegate and the metadata edit gate finish inside the callback.
func (prober *metadataEditGatedProber) ProbeFileJoinedContract() bool {
	return prober != nil && prober.fixture.ProbeFileJoinedContract()
}

// The fixture's source read and subsequent directory rename are synchronous.
func (prober *nfoRenameDuringProbe) ProbeFileJoinedContract() bool {
	return prober != nil && prober.fixture.ProbeFileJoinedContract()
}
