# Bounded reference public-baseline observer

The [actual observer02](nextup-global-baseline-observer-02.md) completed all 34
requests and independent attestation. Its complete baseline is usable for fresh
preparation, with 88 device rows, exact-token closure and all 129 old roots
preserved. It does not release a matrix fixture or establish client acceptance.

The reference preparation attempt `nextup-global-reference-20260913-02`
completed one administrator login, 30 reads, logout HTTP 204, and an exact-token
HTTP 401. Its actual Devices response contained 86 rows with
`TotalRecordCount: 0`. The generic catalog decoder rejected that count before
reading System/Configuration. The failed attempt therefore cannot supply a
complete new baseline. Its six media copies and its closed authentication
history remain retained; it created no libraries, users, or playback sessions.

The initial real observer then completed its one administrator login but
stopped before any GET or cleanup. The frozen HTTP transport returns ordered
header tuples; the observer incorrectly passed those in-memory tuples to a
decoder requiring JSON-style lists. Its durable raw JSON had already
converted the tuples to lists, so reading the saved response later did not
reproduce the in-memory failure. That consumed observer state and terminal
remain `recovery_required`. A subsequent observation must admit an independent
exact-token recovery; it never rewrites the failed observer as a success.

`scripts/test-env/observe-nextup-global-reference-baseline.py` implements a new,
single-use observation with a complete 31-read snapshot. It never constructs a
preparation runner, copies media, creates a library or user, updates metadata,
reports playback, scans a catalog, restarts a service, or deletes old evidence.
The new observer uses one existing administrator credential and one new device
identity. It preserves its own authentication history and requires independent
attestation before releasing the resulting baseline.

## Frozen implementation and verification

The current observer source is
`5436668b824c3432fe52b6f250faea6626f4fa6dc13fc33955fa152337487228`;
its guard source is
`a7fcffa4f55de3c9dfe04e050daf06384d04b5f38473d6b9506a6babb6ecadf4`.
It reuses the reviewed `HTTPTransport`, `Journal`, and metadata-only
`process_identity` from transport source
`d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1`.

The immutable remote source scope is
`/opt/goby-test/exec-work-m3e/nextup-global-reference-baseline-observer-tool-04`.
All verification ran through `ssh test-env` with `/usr/bin/python3 -I -B`.
These guard, compilation, regression, and proof-chain checks performed no
local testing, business HTTP, original implementation read, or reference
database read. The separately retained real observer and recovery attempts
have their own actual HTTP evidence.

| Evidence | Result | SHA-256 |
| --- | --- | --- |
| `nextup-global-baseline-observer-verification-03.json` | 149 guards, zero failures/errors/skips; includes frozen HTTPTransport with fake sockets and all closed-chain authority guards | `354b3931a2c0df7b87b66a17cfa792b785a11422db1dc8ad2c49c6f4ec517ff6` |
| `nextup-global-baseline-observer-compile-03.json` | Both final owned Python files compiled remotely | `184cfc7d0d58ec520393563bab300d8ddb16dffa47873cb60eaa76b411e40687` |
| `nextup-global-baseline-observer-regression-03.json` | Six negative tests with eight expected failures against the previous chain implementation, including three ControlGroup subcases | `e8f7f2cf8ca9828adeea3584a6cb11e53a7afcd0be749d46c7031d15cfa20481` |
| `nextup-global-baseline-observer-recovery-chain-observation-01.json` | Final source accepted 25 actual predecessor/recovery proof pins, 87 retained devices, two closed token/device windows, and three unit records; no HTTP or process probe | `ff948e8c93585d223c6ec38092cb7cb8bfbb43f626cedf94ad937cd1e9ed5ea9` |
| `nextup-global-baseline-observer-verification-02.json` | Earlier 137 guards passed before the final closed-proof consistency refinements | `33c75716c81a7f0ab6d1f11b80fccec2f0316629d81f8f5509e475d807158ee2` |
| `nextup-global-baseline-observer-compile-02.json` | Both current owned Python files compiled remotely | `99480681011ed366ef7df231df15ef873e18181f4c5589170e1a4d8d4a1668f9` |
| `nextup-global-baseline-observer-regression-02.json` | Two expected tuple-header failures against the retained version-one source; one uses frozen HTTPTransport with fake sockets | `ffbe66083a6d82ac6adac3893e65b9ada937d2b44b42acee6939b317e3f3d901` |
| `nextup-global-baseline-observer-verification-01.json` | Earlier 95 guards passed with list-based fake headers; this did not exercise the real tuple-header boundary | `ef2d70662418efc8c3bc6de3464a569d29810742aadfce7bb749fc1ba571fdb0` |
| `nextup-global-baseline-observer-compile-01.json` | Both owned Python files compiled remotely | `f6d748b2be19b757a66334cee201e869b8c743fe29c16d9d0766512d6a35e98d` |
| `nextup-global-baseline-observer-regression-01.json` | One expected failure against the retained previous source | `82fda0c0213b9c043be8c585fb90a02f9e0d2e596fb7ba94e2b25378486acc14` |
| `nextup-global-baseline-observer-predecessor-observation-01.json` | Actual predecessor chain accepted; 12 actual proof files repinned; no process probe or business HTTP | `87a60954ec80c3785180064960ca29e186b1694be57068ccd59bbe54d717a73d` |
| `nextup-global-baseline-observer-tool01-guards.json` | Earlier 57 guards passed; this source predates the device precision fix | `6b399a2c901b060a1bf88ea3125a3b384d9a68f065465ad031a5dc9e5337490b` |

The earlier source
`794a5bd65b3aa1c76d4ca2df94ee6cafa182d205e51c77f45017ea48d687139a`
incorrectly required the new Device.DateLastActivity to follow the completed
login acknowledgment. The actual preparation response showed a whole-second
device timestamp before that acknowledgment. A fresh regression scope proved
that the earlier source rejects a legitimate same-second timestamp. The new
source accepts it and rejects the preceding second, nonzero fractional times
before the actual request, and values beyond the actual Devices read.

The next source,
`89bc2841a3217cecbdb1815bb74ebef6e79f5e3ad951ed2dbd5d755f1d0fb7ee`,
fixed time precision but still had the tuple-header failure. The current source
normalizes each actual header pair to a list before constructing and decoding
the wire document. Values, duplicate fields, and order remain unchanged.
The default fake transport now uses tuple pairs, and a separate guard sends
all 34 synthetic requests through the actual frozen transport and Python's
HTTPResponse parser. That guard verifies both repeated header positions and
the exact saved wire projection.

The intermediate chain source
`db3b089113d9c705aca91064c449ad91caf5ad3f31a70d6af2f0113c0ca744e5`
retains its successful 137-guard receipt. Independent review then identified
four remaining consistency gaps: omitted live ControlGroup authority,
unbound recovery-manifest evidence, incomplete request/cross-record time
ordering, and seventh-digit activity rollback. Six negative tests reproduced
those gaps against its retained bytes. The final source resolves all four;
the same reviewer confirmed the specific fixes, and all 149 guards passed.

The actual predecessor-chain observation is a read-only schema and evidence
check. It is explicitly not a full Authority admission, a lock acquisition,
a process check, a new login, or a released baseline. No real observer run is
claimed by these tooling receipts.

## Exact request budget

There are at most 34 reserved attempts: 32 normal attempts and two cleanup
attempts. Each is eligible for dispatch once. The counter charges an attempt
before sending; independent wire evidence establishes what actually reached
the server. No automatic resume or retry exists.

| Phase | Requests |
| --- | ---: |
| Existing administrator authentication | 1 POST |
| Public server, complete user roster, complete library definitions | 3 GET |
| Full catalog projection for each of the eight retained libraries | 8 GET |
| Complete subject item projection and preferences for six retained users | 12 GET |
| Six explicitly frozen full item-detail witnesses | 6 GET |
| Actual Devices registry and full System/Configuration | 2 GET |
| Exact-token logout and same-token rejection | 1 POST and 1 GET |

Catalog reads retain the existing explicit fields, `Recursive=true`,
`EnableUserData=true`, `EnableTotalRecordCount=true`, and `Limit=256`. Subject
projections request exactly the item IDs from the pinned full prior catalogs.
Library and catalog envelopes must report the actual complete list count.
Only Devices accepts the observed zero-count convention or an exact count;
its rows still require unique `Id` and `ReportedDeviceId` values and a maximum
of 256 rows.

The only POST routes are `/emby/Users/AuthenticateByName` and
`/emby/Sessions/Logout`. Cleanup GET `/emby/Sessions` is permitted only after
the one actual logout returned HTTP 204. The original application remains in
its existing namespace. Requests use the existing owned host proxy at
`http://127.0.0.1:18197`; the proxy is reused unchanged. The observer does not
read original executable bytes. This does not make a new assertion about
implementation reads performed internally by the existing proxy.

## Private manifest contract

The top-level JSON keys are exactly:

```text
schemaVersion, runId, server, endpoint, process, lock, sources, inputs,
scope, sealedRoots, forbiddenOriginalRoots, admin, preservation, budgets,
closedObservers
```

`schemaVersion` is integer `2`. `runId` and identity references are bounded
ASCII identifiers. The manifest is a root-owned, owner-only JSON file under
the explicit input root. The CLI requires its byte-exact SHA-256.

| Field | Contract |
| --- | --- |
| `server` | `{id, version}` from the retained complete public baseline |
| `endpoint` | Exactly `{scheme: "http", host: "127.0.0.1", port: 18197}` |
| `process` | `{application, endpoint, workerNetworkNamespace}` with the existing metadata-only process/listener shape used by the transport |
| `lock` | `{path, device, inode}` for the existing reference lock; never creates a replacement |
| `sources` | Exact `{observer, transport, proxy}` descriptors, each `{path, sha256}` |
| `inputs` | Exact `{credentials, publicBaseline, predecessor}` descriptors |
| `closedObservers` | Explicit ordered list of zero to eight distinct independent observer-recovery terminal descriptors |
| `scope` | Exact `{fixtureRoot, inputRoot, sourceRoot, proxySourceRoot, outputRoot}` absolute Linux paths |
| `sealedRoots` | Explicit unique historical roots that may be read and must never overlap the new output |
| `forbiddenOriginalRoots` | Explicit exclusions containing both the original executable/package root and its `-programdata` root |
| `admin` | `{userId, username, credentialRef, deviceId}` for the existing administrator and one absent new observer device |
| `preservation` | `{userIds, libraryIds, detailRoutes}` for six users, eight libraries, and six unique `{group, userId, itemId}` detail witnesses |

The output root must be a previously absent direct child of `fixtureRoot`.
Source and input paths must remain inside their declared owned roots and
outside all original implementation/data exclusions. Symlinks, changed
device/inode identity, writable ancestors, non-root ownership, unexpected
hard links, changed file bytes, and non-private input modes are rejected.

The private credential input contains exactly:

```json
{
  "schemaVersion": 1,
  "runId": "the-new-observer-run-id",
  "admin": {
    "userId": "the-existing-administrator-id",
    "username": "the-existing-administrator-name",
    "credentialRef": "the-frozen-secret-reference",
    "password": "the-actual-existing-private-password"
  }
}
```

The actual password must be at least 32 characters and match the manifest's
administrator references. No credential is printed or exported. The
predecessor login's retained actual form must match this same existing
administrator credential. There are no P/Q actor credentials in this observer.

The seven budget keys are `requestSeconds`, `normalSeconds`, `cleanupSeconds`,
`requestBytes`, `responseBytes`, `totalResponseBytes`, and
`cleanupResponseBytes`. Their respective bounds are 1–15 seconds, 1–600 seconds,
1–120 seconds, 1–32 KiB, 1 KiB–1 MiB, 4 KiB–40 MiB, and 1 KiB–3 MiB.
`cleanupResponseBytes` must be at least `2 * (responseBytes + 1)`, and normal
work cannot consume it. A suitable bounded configuration is:

```json
{
  "requestSeconds": 10,
  "normalSeconds": 600,
  "cleanupSeconds": 120,
  "requestBytes": 32768,
  "responseBytes": 1048576,
  "totalResponseBytes": 41943040,
  "cleanupResponseBytes": 2097154
}
```

## Actual predecessor admission

`inputs.predecessor` points to the independent failed-preparation terminal:

```text
/opt/goby-test/exec-work-m3e/reference-nextup-global-preparation-execution-02/independent-terminal.json
SHA-256 588c4f09fc817ed555d7f2711f692f94e35f1f41bd288748626fffca12546111
```

The observer reads and pins the terminal's actual preparation manifest,
producer state, producer terminal, and administrator closure. The closure
links distinct actual intent and raw-response files for login ordinal 1,
Devices ordinal 31, logout ordinal 32, and rejection ordinal 33. It validates
their method, route, request headers, form bytes, plan binding, completion,
HTTP status, token, administrator, server, session, and device identity. It
also checks the retained failed unit's exact invocation, current properties,
and recursively empty cgroup while holding the original exclusive lock.

The baseline is the complete retained v4 `after-public.json`, SHA-256
`48b3acb509b8318481c2d9c4e35ce534a69bb6deaaf31246a1bd78b3cc3bab32`.
The actual predecessor Devices response must preserve all its 85 old rows
exactly and contain exactly one additional device. That row must match the
previous login's `SessionInfo.InternalDeviceId`, the frozen preparation
device ID, administrator, and client metadata. The actual predecessor state
must retain a known administrator token/session, exact cleanup, no pending
ownership, and no created library/user/playback ownership.

Every actual evidence descriptor read during admission is pinned again before
and after each request. The same checks bind the observer input, all owned
source files, the original process metadata, the existing proxy/listener,
worker namespace, and the exclusive existing lock. The observer never reads
the original executable contents or database and never loads predecessor
operator code.

## Closed observer recovery chain

`inputs.predecessor` remains the original failed-preparation anchor. The new
`closedObservers` list separately names each independently closed observer
recovery in chronological order. No recovered device is deleted, hidden, or
folded into an invented baseline. For the next real observation following
the initial failed observer, this list must contain its actual independent
recovery terminal; an empty list cannot justify that additional device.

The actual closed recovery terminal for the initial failed observer is
`/opt/goby-test/exec-work-m3e/reference-nextup-global-baseline-recovery-execution-01/independent-terminal.json`,
SHA-256 `41392c5ff86192dce4e90eee8026b188aa6def14cf90a92aa0920edb4c8eb8f1`.
The final observer source accepted its actual 25-file predecessor/recovery
chain in a separate read-only observation. This established 87 retained
device rows and two closed administrator-token/device windows. The original
failed observer state remained byte-identical at
`3f4df82c27a851973f5578eb19a7ecf164d0840c000e23bb237fd6ab0382729b`.
That check did not construct an observer runner, acquire the live lock,
probe live processes, or release a new full baseline.

Each terminal has `schemaVersion: 1`, kind
`nextup-baseline-observer-recovery-terminal`, and status
`observer_login_independently_recovered_and_closed`. It contains:

| Field | Actual evidence |
| --- | --- |
| `runId`, `observerRunId`, `capturedAt` | Independent recovery run, original failed observer run, and terminal capture time |
| `observer` | Exact `{manifest, state, terminal, login: {intent, response}, unit}` |
| `recovery` | Exact `{manifest, deviceObservation: {intent, response}, logout: {intent, response}, rejection: {intent, response}, unit}` |
| `userId`, `username`, `reportedDeviceId`, `tokenSha256` | The original acknowledged observer administrator, device, and exact token |
| `from`, `through` | Original login intent's real `createdAt`, and actual recovery rejection completion |
| `ownedDevice` | The complete actual row from the recovery Devices response |
| `requestCount`, `observerRequestCount` | Exactly three recovery requests and one original observer request |
| Closure and scope facts | `exactTokenClosed`, `recursiveCgroupsEmpty`, `noNewLogin`, `noMetadataMutation`, `noLibraryCreation`, `noUserCreation`, and `noPlayback` are true; original implementation and database reads are false |

Every nested evidence reference is a byte-exact `{path, sha256}` descriptor.
Each `unit` retains `{name, invocationId, properties, cgroupPath}`. The original
observer unit must still be the failed invocation with MainPID zero and exit
status two; the independent recovery unit must be a distinct successful
invocation with MainPID zero and exit status zero. Current unit properties
and recursively empty cgroups are rechecked with all other authority before
and after subsequent requests. `ControlGroup` is a mandatory retained and
live-queried property, including for the preparation anchor. It must bind
the expected system.slice path or the actual empty value of the exact
completed invocation. The complete unit authority is also frozen in memory;
omitting ControlGroup cannot bypass a different live Slice or residual group.

The recovery manifest binds `runId`, `observerRunId`, `server`, `process`,
`lock`, and the original administrator's user/name/device fields. Its
`observer` and `preparationAnchor` references must exactly match the terminal's
original evidence. Every actual recovery intent must bind the raw manifest
file SHA-256 and the canonical recovery manifest's plan SHA-256. The original
observer manifest still pins the same complete v4 baseline, failed-preparation
anchor, proxy, process, and lock. A recovered version-two observer must name
exactly the already accepted earlier recovery descriptors. Duplicate records,
run IDs, device IDs, unit names, or invocation IDs cannot be substituted.

The original observer state remains unknown: one normal request, zero reads,
zero cleanup requests, no committed token/session, a pending login intent,
and `ownershipPending.stage: response-awaiting-owner`. Its state must bind
the actual original manifest, frozen plan, login intent, and raw login
response. Its terminal remains `recovery_required`, with no released
baseline or complete snapshot. The recovered terminal provides a separate
disposition instead of altering those original records.

The original login's actual principal, server, session, internal device ID,
reported device ID, client metadata, form bytes, and token are revalidated.
The three recovery requests must then be exactly GET `/emby/Devices`, POST
`/emby/Sessions/Logout`, and GET `/emby/Sessions`, using that same token and
original client/device metadata. Distinct complete raw responses must prove
HTTP 200, 204, and 401 in that order. A separate login or a summary flag cannot
substitute for any response.

Every actual intent `createdAt` must precede its own response, and every
following request must start at or after the preceding response completion.
The original failed observer login must also follow the preceding independent
terminal's `capturedAt`. This cursor advances through each accepted recovery
record, so a later chain entry cannot precede the preparation anchor or an
earlier recovered observer. These comparisons preserve all seven fractional
digits instead of discarding submicrosecond ordering.

Each actual recovery Devices snapshot must preserve every previously
observed row and add exactly its own failed-login device. All original 85
device rows remain exact. Already closed devices may advance only
DateLastActivity, within their own proven authentication windows and observed
whole-second precision; activity may not move backwards even at 100ns
precision. Each newly
recovered row must match its original login's internal device ID and all
administrator/client metadata. Its timestamp is bound to the real original
login request through the real recovery Devices response. The resulting
device map, token hashes, session IDs, and closed windows are carried into
the next chain step and final snapshot comparison.

With one recovered observer, the complete new observer must therefore
actually observe 88 Devices rows: 85 original rows, one preparation device,
one recovered observer device, and its own new device. Two recovered
observers require 89 rows. This evidence growth does not add any new business
request: the complete observer remains bounded to 34 attempts.

## Snapshot preservation and authentication responsibility

Every request writes an exclusive intent, a reserved state, and an fsynced
state before its single possible send. It preserves ordered raw response
headers, exact body bytes as base64, timestamps, request context, and body
length. Incomplete, oversized, ambiguous-framing, conflicting Content-Type,
non-UTF-8, and malformed JSON responses stop the run. Only a non-JSON HTTP 401
may use a UTF-8 plaintext body.

A returned login response is persisted with `ownershipPending` and
`uncertain=true` before any nested ownership fields are consumed. The exact
administrator/server/session/client/device and nonempty new token must match.
The new owner is committed durably; a second commit clears uncertainty.
Response loss, malformed ownership, or a failed commit cannot proceed to
another request or automatic cleanup. Known complete GET/status/preservation
failures may perform only the two reserved logout/rejection requests.
Cleanup itself never repeats.

The full new snapshot preserves the existing public-baseline schema and its
actual `credential_context`. It contains all newly observed documents,
including System/Configuration; no old configuration is spliced into it.
The full server, configuration, libraries, catalogs, subject projections,
preferences, and six details must equal the retained complete baseline.
Account profiles must be equal except for bounded administrator
`LastLoginDate` and `LastActivityDate` changes.

All 85 original device rows must still match exactly. The prior preparation
device and each explicitly recovered observer device may change only
`DateLastActivity` inside their own proven closed authentication windows.
Exactly one new observer device must match the acknowledged login's internal
and reported device IDs, administrator, and request client metadata. Thus
success proves the exact actual Devices population derived from the retained
recovery chain plus one new device; it never synthesizes a missing row or
admits an unexplained device.

The actual Devices DTO has observed whole-second activity precision. For an
owned observer or a bounded closed-device update, a timestamp whose fractional digits are all zero
uses the floor of the actual login request's UTC second as its lower bound.
A nonzero fractional timestamp uses the exact request start. Both must be no
later than the actual Devices response completion. Comparisons preserve
Emby's seven fractional digits, avoiding an additional precision tolerance.
The login event retains `requestAt` from its actual durable intent; HTTP
acknowledgment time is not the lower bound.

## Outputs and outer execution

Run `plan` first, inspect its exact 34-route result, then perform a separate
read-only `Authority(manifest, manifest_descriptor)` construction followed by
`acquire()`, `check()`, and `close()`. The fresh output root must remain absent
until the actual run. The root operator is responsible for its own independent
before/after preservation capture, controlled systemd invocation, original
PID/invocation closure, and retained evidence inventory.

```text
/usr/bin/python3 -I -B <tool-root>/observe-nextup-global-reference-baseline.py plan \
  --manifest <private-input.json> --manifest-sha256 <actual-input-sha256>

/usr/bin/python3 -I -B <tool-root>/observe-nextup-global-reference-baseline.py observe \
  --manifest <private-input.json> --manifest-sha256 <actual-input-sha256> \
  --plan-sha256 <separately-reviewed-plan-sha256>
```

The outer invocation must retain `SSH_CONNECTION`, run as root in the existing
host/proxy namespace, use a fresh systemd unit with `RemainAfterExit=yes`, and
preserve its invocation ID and final process/cgroup evidence. A failed or
consumed scope is never replayed.

Private successful outputs include the manifest, frozen plan, per-request
intent/reservation/raw response, full `public-baseline.json`, complete
`public-preservation.json`, `closed-authentication.json`, state, and terminal.
The terminal always has `baselineReleased=false` and
`independentAttestationRequired=true`; success is
`awaiting_independent_attestation`. The exported terminal excludes private
paths, responses, credentials, tokens, and session identifiers.

For each actual logout and same-token rejection, the observer additionally
writes a `controller_api` intent and result. The intent preserves
`channel/method/path`; the result preserves actual
`channel/status/complete/body/token_sha256/completed_at`. Both link the
raw-wire descriptor, and the intent also links the actual wire intent.
They are direct schema projections of that request's retained wire, not
summary-generated responses. An independent reviewer can reconstruct and
hash each projection from the raw files.

`closed-authentication.json` has the existing preparation producer's exact
`closedAuthentication` row shape: `userId`, `reportedDeviceId`, `tokenSha256`,
`from`, `through`, `logout`, and `rejection`. The window begins at the new
full snapshot capture and ends at the actual 401 completion. Both proof rows
contain the actual adapter result/intent descriptors, exact status, token hash,
and completion time. This allows a future preparation release to account for
this observer device's logout-time activity after independently accepting
the new full baseline. No implicit boolean flip or matrix release is performed.
