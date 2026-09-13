# Bounded reference NextUp client discovery input

This scope observes the original reference client with actor P through Home,
LA TV, Home, and LB TV. It authorizes zero playback attempts, no playback
negotiation, no media requests, no reference implementation reads, and no
reference database reads. The original plan's ceiling remains two attempts and
1,200 seconds; this narrower scope does not spend either playback allowance.

Matrix cleanup has restored P and Q to zero episode history. This discovery
therefore establishes only the client's real parameters, initialization, and
zero-history Home/TV presentation. Empty results do not establish the client's
behavior in states equivalent to watched matrix R1-R4, and do not complete real
playback or playback-driven refresh acceptance. Any new parameter difference
requires a separately frozen public control before further interpretation.

All execution, guards, and syntax checks run through `ssh test-env`. The new
sources are frozen at `/opt/goby-test/exec-work-m3e/nextup-client-discovery-tool-01/revision-01`.
The single fresh output is `/opt/goby-test/exec-work-m3e/reference-nextup-client-discovery-01`.
Neither directory may replace a consumed historical run.

The controller exclusively creates the empty output directory as root-owned
mode 0700 and fsyncs its parent before launch. The driver requires that exact
non-symlink directory to remain completely empty, then exclusively creates
`private` and `export` and fsyncs all new directories. An existing run with any
entry is rejected; no resume is supported. This allows `ProtectSystem=strict`
with only the output directory and existing lock writable, plus `PrivateTmp`
for temporary browser profiles. The driver never writes the work-root parent.

The driver entry is `client-browser-nextup-discovery.mjs`. It imports only its
new dedicated `client-browser-nextup-discovery-runtime.mjs` and the unchanged
`client-browser-session-proof.mjs`. The dedicated runtime derives from the
already verified reference browser transport, with the new P binding and a
narrow NextUp public-response observer. It does not execute the old
LibraryChanged workflow or use its viewer account.

## Private input contract

Write a root-owned, mode 0600 JSON input. All ancestors must be root-owned,
non-symlink directories without group/world write permission. Hash exact file
bytes. Source descriptors must name exactly the frozen directory above.

```json
{
  "kind": "nextup-reference-client-discovery-input",
  "version": 1,
  "runId": "nextup-reference-client-discovery-01",
  "root": "/opt/goby-test/exec-work-m3e/reference-nextup-client-discovery-01",
  "execution": {"path": "ABSOLUTE_PUBLISHED_MATRIX07_EXECUTION", "sha256": "EXACT_SHA256"},
  "release": {"path": "ABSOLUTE_NEW_CLIENT_RELEASE", "sha256": "EXACT_SHA256"},
  "sourceClosure": {
    "client-browser-nextup-discovery.mjs": {"path": "TOOL/client-browser-nextup-discovery.mjs", "sha256": "EXACT_SHA256"},
    "client-browser-nextup-discovery-runtime.mjs": {"path": "TOOL/client-browser-nextup-discovery-runtime.mjs", "sha256": "EXACT_SHA256"},
    "client-browser-session-proof.mjs": {"path": "TOOL/client-browser-session-proof.mjs", "sha256": "EXACT_SHA256"}
  },
  "existingDeviceIds": ["COMPLETE_CURRENT_OWNED_REFERENCE_DEVICE_IDS"],
  "libraries": {
    "LA": {"viewId": "133", "name": "EXACT_PUBLIC_LA_NAME"},
    "LB": {"viewId": "135", "name": "EXACT_PUBLIC_LB_NAME"}
  },
  "budgets": {
    "maximumSeconds": 1200,
    "normalSeconds": 840,
    "maximumPlaybackAttempts": 2,
    "permittedPlaybackAttempts": 0,
    "recorderRequests": 34,
    "recorderBodyBytes": 2097152,
    "browserNavigationActions": 3
  }
}
```

Placeholders are schema documentation, never executable fixture values. Use
the completed matrix07 published execution descriptor. Its credential
descriptor binds the preparation05 private `matrix-credentials.json`; only P
is selected. No password or token goes into the input or exported report. The
driver validates P's credentialRef, userId, username, ordinary policy, exact
library grants, and reference server/origin against that execution.

The new release is a separate controller attestation, not a rewritten matrix
terminal. The controller must independently reconstruct the matrix's complete
wire and cleanup result, verify that its unit has no live worker and its cgroup
is empty, and retain fresh
process/fixture coordination before creating it. Its exact fields are:

```json
{
  "kind": "nextup-reference-client-discovery-release",
  "version": 1,
  "execution": {"path": "SAME_EXECUTION_PATH", "sha256": "SAME_EXECUTION_SHA256"},
  "matrixTerminal": {"path": "EXACT_MATRIX_TERMINAL", "sha256": "EXACT_SHA256"},
  "independentTerminal": {"path": "EXACT_INDEPENDENT_WIRE_TERMINAL", "sha256": "EXACT_SHA256"},
  "independentRuntimeTerminal": {"path": "EXACT_INDEPENDENT_RUNTIME_TERMINAL", "sha256": "EXACT_SHA256"},
  "operatorCommit": {"path": "EXACT_OPERATOR_COMMIT", "sha256": "EXACT_SHA256"},
  "apiClosed": true,
  "revokedActors": ["P", "Q"],
  "zeroBaselineVerified": true,
  "unitInactive": true,
  "originalClientAllowed": true
}
```

The driver binds the exact release and referenced receipt bytes. It does not
replay matrix HTTP or independently reinterpret a generic terminal schema.
Release semantic validation remains the controller's responsibility. The
`unitInactive` field means no live worker, MainPID zero, and an empty cgroup;
the completed systemd unit may correctly remain `active/exited`.
The
existing device list comes from the latest accepted public inventory plus all
acknowledged subsequent owned logins; do not guess it or silently use an old
LibraryChanged list. The driver additionally creates two explicitly named
state-observer devices; these records and the browser's new device are retained.

## State and cleanup sequence

The driver owns the existing exclusive `reference.lock` throughout its run.
It verifies that `/proc/locks` identifies its own PID as that lock's owner.
The launch must therefore use `flock --no-fork`; a background lock held by an
unrelated process is insufficient. Keep the existing lock inode.

1. Revalidate application and existing proxy process metadata, namespaces,
   proxy listener, source closure, and the current lock. Executable metadata is
   inspected without reading original executable bytes. The already documented
   proxy ` (deleted)` display is accepted only for the same executable inode
   with zero links; the first observation freezes the full metadata anchor.
2. A separate P state recorder logs in, reads six full episode details, six
   series/season details, full profile, and preferences, then logs out and
   proves the same token receives 401. This is 17 requests. Every episode must
   have the measured zero state before the browser can start.
3. Create a fresh browser with observers installed before UI login. Journal
   actual login/capability/logout write intents before forwarding. Navigate
   only through visible controls; library IDs and names must also appear in
   the original client's completed Views response. Retain screenshots and
   visible cards, actual NextUp query entries, body, order, actor/token binding,
   and navigation cause. Only completed main-frame NextUp responses outside
   Service Workers are read for a bounded decoded body hash and length. These
   must match the physical response's decoded body hash and length before the
   two channels are accepted as associated. No asset response body is inspected.
   No injected fetch is a client request.
4. Perform the original UI logout, observe 204 and the login view, prove the
   exact UI token receives 401, and close browser/proxy resources.
   Before closure, retain a separate logout-view DOM record and screenshot,
   bound by hashes. The reused session-proof helper retains the observed exact
   token fingerprint and 401 metadata; it does not capture a raw independent
   401 exchange, so byte-for-byte reconstruction of that exchange is not claimed.
5. Only after browser closure and token rejection, a second independent P
   recorder repeats the 14 full reads and exact-token cleanup: another 17
   requests. Compare complete episode/summary UserData, identity, profile
   Id/Name/Policy/Configuration, and preferences. Retain full profiles so
   legitimate activity timestamp differences remain visible. Drift fails the
   scope; no automatic profile restoration or history reset is permitted.

The protocol recorder and browser never overlap. An uncertain login or failed
logout keeps its known private intent/token evidence and a recovery-required
result; it cannot trigger another recorder or a blind retry. Failures retain
their output. All raw response bodies, native paths, private tokens, and UI
screenshots stay in the private directory. Only the sanitized report is copied
back to repository documentation.

## Remote launch shape

After remote guards and syntax checks pass for the exact source closure, the
controller may launch the following command in the new owned systemd scope:

```text
/usr/bin/flock --exclusive --nonblock --no-fork /opt/goby-test/exec-work-m3e/reference.lock /usr/bin/node /opt/goby-test/exec-work-m3e/nextup-client-discovery-tool-01/revision-01/client-browser-nextup-discovery.mjs --input ABSOLUTE_PRIVATE_INPUT --input-sha256 EXACT_INPUT_SHA256
```

The actual unit declares `PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright`
through its `Environment` property; there is no `/usr/bin/env` argv wrapper.
The controller must use the remotely verified Node executable, preserve source
and runtime bindings, allow only this owned output for writes, and independently
verify shutdown after execution. The driver reserves the final 360 seconds of
the 1,200-second limit for cleanup. Browser request/byte/socket ceilings remain
the dedicated runtime's independent limits.

An empty completed client global response produces
`reference_global_positive_unresolved`. A positive one produces
`client_positive_requires_separate_frozen_public_control`. Neither result
reopens R5/R6 or authorizes candidate policy changes. If no completed original
client request with omitted SeriesId can be bound to its response, retain
`actual_client_global_request_unobserved`; do not manufacture a discovery receipt.

## Frozen remote verification

The final source passed five `node --check` invocations and all 21 pure guards
on `test-env`, with zero failures or skips. No browser or business HTTP was
started by verification. The [verification receipt](nextup-client-discovery-verification-02.json)
records the exact source hashes and Node executable hash. Detailed output is
retained under `W/nextup-client-discovery-verification-02`, separate from the
source/input/output scopes. The guard TAP SHA-256 is
`cc39f350958151cf418278c22603348622df974f26f1ac35754bbabed1003c2b`.

The preceding 20-guard receipt remains under `W/nextup-client-discovery-verification-01`
with its source snapshot. The final increment adds logout-view evidence and
bounded decoded NextUp frame-body matching. Pure guards establish the tool's
contract; they establish neither an actual original-client result nor playback
or refresh acceptance.
