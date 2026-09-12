# Controlled library change observation

The separate controlled continuation completed metadata editing, directory
removal and re-addition, with one LibraryChanged event in each isolated window.
The [implementation and evidence](../development/library-change-notifications.md)
record the exact arrays and authorization boundaries. The original attempt
below remains failed. Product scan-triggered delivery and full client acceptance
are still incomplete.

The first controlled M3e attempt indexed the new movie, then stopped because
an NFO edit and acknowledged refresh did not change the requested Name and
Overview. Removal did not run. Keep this attempt as `retained_for_review`;
neither the full event contract nor product compatibility is complete.

## Initial controlled attempt

The [initial report](../development/m3e-library-changed-reference-initial.json)
records 334 HTTP exchanges. The original seven libraries and five users retained
their captured public catalog, extra membership/detail, UserData, configuration,
policy and preference projections. The three old media trees retained their
bytes and file identities. Both new recorder sessions completed logout204 and
same-token401. The controller exited1 with an empty worker cgroup.

| Stage | Catalog result | WebSocket observation |
| --- | --- | --- |
| Setup | New library93, root folder94, anchor folder95/movie96, and one new ordinary user | A delayed setup event listed `ItemsAdded=[93,94,95,96]`, `FoldersAddedTo=[94,95]`, and `CollectionFolders=[93]` during the pre-action quiet period |
| Add | Folder97/movie98 appeared with complete stable requested DTOs | No LibraryChanged event in the controlled window ending 35 seconds after convergence; the earlier setup frame is not an add-stage result |
| NFO update plus FullRefresh | The five requested catalog DTOs remained exactly equal; the requested Name/Overview change was unproven | A frame listed `ItemsUpdated=[93,94,97,98,95,96]`, with the other ID arrays empty; it does not establish successful metadata editing |
| Remove | Not executed | No removal claim |

The [add transcript](../development/m3e-library-changed-reference-add-window.json)
retains all quiet-period annotations and the earlier setup frame. The
[update transcript](../development/m3e-library-changed-reference-update-window.json)
retains its failed catalog gate and observed refresh notification. A transcript
containing an event is not sufficient to attribute that event to its named action.
The [exact DTO comparison](../development/m3e-library-changed-reference-dto-comparison.json)
independently confirms equality for IDs94–98. CollectionFolder93 was not part of
that recursive query and is outside the equality claim.

The separate [240-second tail](../development/m3e-library-changed-reference-tail.json)
used another fresh session for the new viewer and made zero catalog mutations.
It observed no LibraryChanged messages and completed logout204/same-token401,
exit0 and an empty worker cgroup. Its unit started 48.879 seconds after the
initial controller exited; the report's generic wording about concurrency must
not be read as evidence of overlap with a catalog mutation. This tail is another
bounded negative observation and does not repair the failed update/removal gate.

The reference now has eight libraries and six users. The owned new user is
`c5f36699a54f4971a891682cd9de410f`; its credentials and both synthetic movies remain
in the private reference scope. The observed movie's NFO contains the requested
update, while its last recorded public metadata remains unchanged. Preserve the
failed scope at `/opt/goby-test/exec-work-m3e/reference-library-changed-v1`.
Any continuation must use new output paths and fresh sessions against this
acknowledged library; it must not repeat setup or relabel the failure.

The completed continuation used the already captured administrator metadata
contract: a fresh full item detail, the `reference-metadata.py` `EDIT_FIELDS`
whitelist with current values, `Id`, and only Name/Overview replacements, then
`POST /emby/Items/98` and its observed204 result. It then moved the owned directory
out and back, preserving the media bytes and the initial failed evidence. Current
reference identities are folder99/movie100 at the original owned paths; IDs97/98
were absent from the scoped catalog after removal. Do not replay either consumed
operator against this new state.

## Existing evidence

The original Emby 4.9.5.0 recording
`tests/compatibility/fixtures/reference/emby-4.9.5.0/websocket-m3c-playback-report-progress.json`
contains one incidental `LibraryChanged` event during independent subtitle
library creation. Its `Data` is:

```json
{
  "FoldersAddedTo": ["38", "39"],
  "FoldersRemovedFrom": [],
  "ItemsAdded": ["37", "38", "39", "40"],
  "ItemsRemoved": [],
  "ItemsUpdated": [],
  "CollectionFolders": ["37"],
  "IsEmpty": false
}
```

The envelope also contains `MessageType` and `MessageId`. The recording does
not establish the meanings of each identifier group, aggregation boundaries,
update/removal behavior, or authorization filtering. It is not evidence that
playback reports cause library changes. An accepted WebSocket upgrade alone
does not establish application-level authentication.

## Controlled scope

The initial attempt used `goby-emby-client-m3e.service` in its private network
namespace, with fresh recorder sessions and a new ordinary user and synthetic
Movies library. Preserve the seven original libraries, five original users, their public
configuration and item/UserData projections, and the three existing media
trees. Read the reference only through public HTTP/WebSocket interfaces and
owned operational receipts; do not inspect its implementation or database.

Separate setup from observation. With the same fresh viewer credentials,
record independent stages against the owned library using a separate connection
per stage. Each stage needs its controlled action, any scoped refresh, completed
catalog observations, and a bounded WebSocket window. Keep unmatched messages
and negative windows as observations instead of guessing missing behavior.
No global refresh, existing-account policy mutation or service restart belongs
to this experiment. Save private raw exchanges and export only sanitized data.

## Transport verification

[`bounded_websocket_capture.py`](../../scripts/test-env/bounded_websocket_capture.py)
is an independent standard-library transport with no implicit login, reference
setup, imports from the old recorder, or output/persistence behavior. Its raw
record includes the token-bearing request target and therefore stays private.
It validates the handshake, retains server frames and assembled messages,
responds to control frames, and bounds message bytes, total bytes, event count
and observation deadlines. Times describe client receipt and send times;
frames received after an HTTP operation can have been queued earlier.

The [remote verification](../development/m3e-library-changed-transport-verification.json)
passed two syntax checks and 14 adversarial transport tests on `test-env` with
zero HTTP requests or reference mutations. Coverage includes fragmented UTF-8
with interleaved Ping, masked/reserved frames, invalid close codes, retained
invalid-text evidence, segmented handshakes, slow handshake deadlines, capture
limits and incomplete-message EOF. This establishes recorder behavior only.

The [recorder verification](../development/m3e-library-changed-recorder-verification.json)
passed two syntax checks, nine focused guards and a separate zero-HTTP preflight
before the actual attempt. Guards cover old-user and old-library write exclusion,
viewer scope, secret-free public projection, retained array order, stable-ID
removal and rejecting a closed observer before media mutation. A Python regex
escape SyntaxWarning was retained in its logs; it did not fail these checks.

## Product integration requirements

Catalog facts must originate from successfully committed transactions. A scan
can fail or be cancelled after earlier changes committed, and owned catalog
transactions can commit after the request context is cancelled. Neither a
successful whole scan nor a later progress write is the notification boundary.

Job counters, forced probes and maintenance timestamps do not establish a
visible metadata change. The initial attempt nevertheless observed an update
notification after explicit FullRefresh while all five requested DTOs remained
equal. Track actual catalog changes separately from explicit refresh
invalidation; do not assume that equal DTOs require no reference notification.
Preserve old and new item, library, parent and role information. Delivery must revalidate the current
session and library policy using trusted publication metadata, including the
old scope of a genuinely deleted item or library.

Ordinary scans currently do not prune missing files. Event delivery cannot
invent removal while the item remains in the catalog. Catalog reconciliation
requires its own stable-directory and cross-root move handling; inaccessible,
changing or incomplete roots must preserve existing entries.
