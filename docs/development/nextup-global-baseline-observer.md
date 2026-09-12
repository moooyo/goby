# Bounded reference public-baseline observer

The reference preparation attempt `nextup-global-reference-20260913-02`
completed one administrator login, 30 reads, logout HTTP 204, and an exact-token
HTTP 401. Its actual Devices response contained 86 rows with
`TotalRecordCount: 0`. The generic catalog decoder rejected that count before
reading System/Configuration. The failed attempt therefore cannot supply a
complete new baseline. Its six media copies and its closed authentication
history remain retained; it created no libraries, users, or playback sessions.

`scripts/test-env/observe-nextup-global-reference-baseline.py` implements a new,
single-use observation with a complete 31-read snapshot. It never constructs a
preparation runner, copies media, creates a library or user, updates metadata,
reports playback, scans a catalog, restarts a service, or deletes old evidence.
The new observer uses one existing administrator credential and one new device
identity. It preserves its own authentication history and requires independent
attestation before releasing the resulting baseline.

## Frozen implementation and verification

The current observer source is
`89bc2841a3217cecbdb1815bb74ebef6e79f5e3ad951ed2dbd5d755f1d0fb7ee`;
its guard source is
`9d1de23e8dbd85388cfb5e449d7f5773874447a34fce5aa2376dd8eb6d008ced`.
It reuses the reviewed `HTTPTransport`, `Journal`, and metadata-only
`process_identity` from transport source
`d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1`.

The immutable remote source scope is
`/opt/goby-test/exec-work-m3e/nextup-global-reference-baseline-observer-tool-02`.
All verification ran through `ssh test-env` with `/usr/bin/python3 -I -B`.
No local test, business HTTP, original implementation read, or reference
database read was performed.

| Evidence | Result | SHA-256 |
| --- | --- | --- |
| `nextup-global-baseline-observer-verification-01.json` | 95 guards, zero failures/errors/skips; synthetic HTTP and process observations | `ef2d70662418efc8c3bc6de3464a569d29810742aadfce7bb749fc1ba571fdb0` |
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
scope, sealedRoots, forbiddenOriginalRoots, admin, preservation, budgets
```

`schemaVersion` is integer `1`. `runId` and identity references are bounded
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
device may change only `DateLastActivity` inside its proven logout/401 window.
Exactly one new observer device must match the acknowledged login's internal
and reported device IDs, administrator, and request client metadata. Thus
success proves an actual 87-row Devices observation; it never synthesizes a
missing row or admits an unexplained device.

The actual Devices DTO has observed whole-second activity precision. For the
new observer device only, a timestamp whose fractional digits are all zero
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
