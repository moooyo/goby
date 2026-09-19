# Bounded timeshift storage

This package stores complete, already produced media intervals. It does not
connect to upstreams, parse subtitles, estimate source history, or authorize
users. The server must perform current authentication and catalog/playback
policy checks before every operation. The package additionally requires exact
equality of the complete `Scope`, including application-client identity.

The storage implementation requires Linux. `New` walks an absolute canonical
root without following symlinks, requires a private directory owned by the
process user, takes an exclusive directory lock, and verifies ownership markers.
An empty final directory can be created. A nonempty unfamiliar root is rejected.
On restart, all recognized presentation directories and artifacts are inspected
before any recovery deletion. Unknown names, missing markers, links, unexpected
owners/types or excessive entry counts stop recovery. Restart discards verified
temporary media; it does not restore old presentation IDs or manufacture history.

## Integration

```go
store, err := timeshift.New(timeshift.Options{Root: "/var/lib/goby/timeshift"})
if err != nil {
    return err
}

scope := timeshift.Scope{
    UserID: userID, AuthSessionID: authID, DeviceID: deviceID,
    PlaySessionID: playID, ItemID: itemID, SourceID: sourceID,
}
window, err := store.Create(ctx, scope, timeshift.WindowOptions{
    TargetDurationTicks: 5 * timeshift.TicksPerSecond,
    Variants: []timeshift.Variant{{ID: "main", Kind: "video", Format: "fmp4"}},
})
if err != nil {
    return err
}

window, err = store.Publish(ctx, scope, window.PresentationID, timeshift.Publication{
    Generation: 1,
    DurationTicks: observedDurationTicks,
    Initializations: []timeshift.ArtifactInput{{VariantID: "main", File: completeInit}},
    Segments: []timeshift.ArtifactInput{{VariantID: "main", File: completeMedia}},
})
```

Input files are borrowed regular-file descriptors. They must be complete and
unchanged until publication returns. Copying begins at offset zero without
changing the caller's file offset or closing its descriptor. No method accepts
an artifact pathname. Public presentation and artifact identifiers are generated
by the store and resolve only inside its opened directory descriptors.

Every publication supplies exactly one complete segment for every fixed variant.
Supported variants are video/audio `ts` or `fmp4`, and subtitle `vtt`. The store
does not inspect their encoded content: the publisher must establish alignment,
decodability, truthful durations and valid subtitle media. A server generating
subtitle responses from a separate bounded cue store can omit subtitle variants.

The first publication and every higher source generation supply an independent
initialization file for each fMP4 variant. Same-generation publications omit
initializations. Sequence numbers are assigned globally within the presentation
and never reset across generations. Actual durations accumulate its media clock;
network downtime does not become invented media. A new generation has its own
initialization references and discontinuity metadata. Timeline and sequence
overflow are rejected before copying. An advertised presentation fixes
`TargetDurationTicks` to a whole number of seconds at creation; each observed
segment duration rounded to the nearest second must fit that target. A zero
target permits generic storage but cannot advertise an HLS playlist.

`Snapshot` returns detached variant, epoch and segment metadata, including every
artifact's `InitID`, ready for server-owned playlist rendering. `Seek` resolves
only a retained interval and returns its actual start; expired and future
positions return `ErrWindowExpired` and `ErrNotBuffered`. For a fixed HLS target,
`Live` walks actual durations backward to a boundary at least three target
durations before the edge, or the earliest available interval. It never selects
the unpublished frame at `LiveEdgeTicks`.

Before sending each media playlist GET or HEAD response, call
`Advertise(ctx, scope, id, snapshot.Revision)`. If this returns
`ErrSnapshotChanged`, fetch and render a fresh snapshot and retry. Advertisement
rejects a non-ended window shorter than three target durations instead of
promising an unsafe live startup. It records the longest advertised playlist
duration containing each segment. When a segment URI subsequently leaves the
current window, that segment and its initialization remain readable for at least
that longest playlist duration plus the segment's own duration, counted from URI
removal. This implements [RFC 8216 section 6.2.2](https://www.rfc-editor.org/rfc/rfc8216.html#section-6.2.2)
without restoring expired seeks.
An ended presentation may advertise a short or empty terminal playlist.
`Snapshot.NextSequence` preserves the next global sequence for an empty
`EXT-X-ENDLIST` response after all currently listed media has expired.

Artifact endpoints use `ResolveArtifact(ctx, scope, id, artifactID)` followed by
canonical-extension validation and `OpenArtifact`. Resolution includes these
previously advertised grace resources even though they are absent from the
current `Snapshot`; its `Artifact.Initialization` and `Variant.Format` distinguish
initialization files from media. Looking up artifacts only in the newest snapshot
would break clients still consuming an older playlist.
Background cue pruning uses `RetainsArtifact` for the same current/grace
availability check without renewing the consumer lease. A missing artifact
returns `false, nil`; scope or presentation failures remain errors. HTTP artifact
requests continue to use `ResolveArtifact`, which counts as consumer activity.

`SetState` marks a source stalled or ended without inventing media duration.
Successful publication clears stalled state. Ended state is terminal, but
existing buffered artifacts remain readable until ordinary expiration or close.
Changing representation requires a new presentation with fixed variants.

## Bounds and lifetime

Defaults are a 600-second window, 512 MiB per window and 2 GiB globally. Byte
quotas charge conservative filesystem allocation units and any larger observed
allocated-block count, rather than treating tiny files as free metadata. Both
temporary copy reservations, advertised grace and expired-but-open media count
against admission. New media cannot evict protected grace to obtain quota; the
publisher must back off or stop when the retained promises exhaust storage.
After the first advertisement, visible media and its live initialization files
use at most one third of the window byte quota. The remaining capacity provides
headroom for advertised grace, in-flight copies and bounded readers, so a steady
stream can roll its window without routinely consuming its entire hard quota.
The oldest visible intervals enter their full promised grace before new media
commits. Previously advertised playlist durations are never reduced to obtain
space. If the initial private snapshot is too large, `Advertise` trims only
unadvertised history and returns `ErrSnapshotChanged` for a fresh rendering.
Generic unadvertised storage retains its original byte-budget behavior. A single
incoming bundle plus required current initialization that exceeds the active
budget is rejected as `ErrQuota`. Global storage remains an independent hard
limit; concurrent owners or slow readers can still exhaust it truthfully.
Directory/marker overhead is additionally bounded by entry-count limits.

Other default limits are 32 windows, four windows per user or application
credential, 64 readers, eight readers per window, four concurrent publications,
64 MiB per artifact, 2,048 retained segments, 32 retained epochs, 16 variants and
65,536 artifacts globally. Configuration accepts smaller test/deployment limits
and enforces hard upper bounds. One publication must fit its window and global
byte budgets. Capacity pressure may evict older intervals in the publishing
window before copying; it never evicts another owner's window to satisfy that
publication. An incomplete new bundle is never visible.

Retention enforces both accumulated media duration and wall-clock age. Idle
windows close after five minutes without consumer/control activity by default;
publishing and source-state updates do not renew this lease. Server pause/presence
integration can call `Touch` after an authorized client keepalive. Background
production must not substitute its own keepalives for a consumer. Calling
`Snapshot` renews the lease but does not prevent old segments from aging out. The current
epoch's initialization stays available for further segments of that same epoch;
older initialization resources survive only while needed by retained segments
or existing readers.

`OpenArtifact` returns a `ReadHandle` implementing `Read`, `ReadAt`, `Seek` and
`Stat`, suitable for `http.ServeContent`. It exposes no underlying file descriptor.
A handle has a 30-second absolute lifetime by default; reads do not extend it.
Request cancellation, presentation revocation and store close forcibly close it.
A segment outside the current seekable window admits new artifact readers only
through its remaining advertised grace. After that grace, an existing bounded
reader keeps its file and storage charge until its actual handle closes. Files
are not unlinked and forgotten while readers still hold them. Cleanup failures
retain their conservative charges and stop further publication.

`ClosePresentation` cancels in-flight copies and all readers, then removes its
owned files, including grace resources: retained media never bypasses policy
revocation. `Close(ctx)` begins terminal shutdown and waits for all writers and
maintenance before releasing the root lock. A context timeout does not abandon
that cleanup; a later `Close` can wait on the same shutdown. Authorization
revocation should call `ClosePresentation` instead of merely forgetting its ID.

The tests cover observed clocks, reconnect epochs, exact scope checks, atomic
publication, cancellation, mutable input rejection, bounded readers and storage,
overflow, terminal state, root exclusivity and safe recovery. A steady-flow case
publishes and advertises through more than five complete retention periods with
reconnecting epochs, checking active, grace and global byte bounds throughout.
They must be run
only in the task's authorized remote verification environment.
