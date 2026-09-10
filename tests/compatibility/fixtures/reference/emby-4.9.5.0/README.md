# Emby 4.9.5.0 Reference Fixtures

This directory contains 1794 audited JSON records from official, isolated Emby Server 4.9.5.0 instances on the authorized Linux `test-env` host. The earliest 107 comprise 65 initial baseline files, 24 `artwork-*` files covering local NFO metadata/images, and 18 `entity-*` files covering entity navigation and filtering. Later playback, subtitle, WebSocket, HLS, audio/video, metadata, key/device, and ScheduledTasks studies extend that baseline. These records include supporting observations and audits; they are not endpoint counts or evidence that Goby passes every captured contract. Earlier JSON fixtures are unchanged by each extension.

See [reference-server.md](../../../../../docs/research/reference-server.md) for the official package URL/hash, setup, network isolation, source fixtures, observations, and limitations. The recorder is [reference-capture.py](../../../../../scripts/test-env/reference-capture.py).

HTTP fixtures contain:

- `reference`: product, actual server version, and UTC capture timestamp.
- `request`: exact method, API-relative path, explicit recorder headers, and body.
- `response`: status, ordered header pairs, and body representation. `json` stores parsed JSON and `text` stores exact text. New image responses use `binary-base64` to store the exact wire bytes as base64; empty HEAD/304 responses remain empty text.
- Optional `observation`: context that affects interpretation.

Passwords and access tokens are redacted. Private deployment paths use placeholders. Non-sensitive synthetic instance/item/user IDs are preserved to retain type, relationship, and URL semantics. Response headers, including `Content-Length`, are unchanged from the original wire capture; redacted bodies can have different lengths from those headers.

The initial export was audited remotely against the private originals: all response headers were exact, JSON structure/types/numeric values were preserved, and all recorded credentials were absent. An earlier generic ID-alias export was replaced entirely because it could alter numeric header values.

The initial library/list/Latest captures preceded the movie library's `SampleIgnoreSize=0` adjustment. Files named `items-movies-after-sample-filter`, `item-detail-*`, and `playback-info-*` follow that adjustment. No source media file was changed. One-element arrays must remain arrays, and omitted fields must not become explicit nulls during fixture consumption.

The later artwork captures follow a single-item refresh after adding synthetic NFO, JPEG, and PNG sidecars. They have a different title and richer metadata for the same movie ID. See the report's M2b extension for source hashes, timezone effects, field-selection behavior, image authentication, transforms, and conditional requests. The extension audit verified its 24 exports independently and retained SHA-256 equality of all original raw/exported baseline files.

The 18 entity captures are read-only requests after that refresh. They cover genre/tag/studio/person lists, name and generic-ID details, ID/name filtering, a negative filter, and pagination. Their audit preserved all preceding 89 raw/exported files. Embedded numeric facet IDs and string entity/list/person IDs must retain their original JSON types during comparisons.

Subsequent protocol and media records have their own documented observation or
probe envelopes. Consult the [WebSocket study](../../../../../docs/research/websocket-reference.md),
[HLS study](../../../../../docs/research/hls-reference.md), and
[Universal/progressive audio study](../../../../../docs/research/audio-reference.md)
for experiment scope, source hashes, positive decoding results and preserved
failures. M4c adds 174 audio records to the preceding 436. Large audio/TS bodies
can be represented by bounded binary summaries and hashes rather than embedded
media; exact private wire captures were audited separately on the test host.

The [M4d audio profile study](../../../../../docs/research/audio-profile-reference.md)
adds 126 separately prefixed records to the preceding 610. It contains 87 complete
HTTP captures, supporting observations/probes, and one explicitly incomplete
body-only response whose headers/status were not retained. Do not treat that
body observation as a complete HTTP contract. Reference failures and truncated
successful responses remain recorded; only complete decoded media provides
positive playback evidence. All earlier raw/export pairs and source hashes
were unchanged by the extension.

The [M4e video profile study](../../../../../docs/research/video-progressive-reference.md)
adds 92 separately prefixed records to the preceding 736. It contains 61 complete
HTTP captures, twelve PlaybackInfo requests, a Range control, media probes and
supporting observations. Pure-copy reference responses include preserved server
failures and truncated bodies, and a mixed-copy seek retains the wrong video
preroll. Neither HTTP 200 nor an encoder exit code establishes correct playback.
All earlier raw/export pairs and the source hash were preserved. The separate
Goby copied-video diagnostics under `docs/research/video-copy-seek` do not add to
this official-reference count.

The [M5b metadata study](../../../../../docs/research/metadata-reference.md) adds
130 records: 106 complete HTTP exchanges and 24 observations. It uses a new tiny
owned library and preserves all preceding 828 records and existing media hashes.
The captures distinguish `SortName`/`ForcedSortName`, field locks, NFO refresh,
forced replacement and reset. Native Goby locks must not be described as matching
all observed Emby mutation semantics.

Do not replay setup/library mutations against arbitrary servers. Differential checks should normalize explicitly chosen nondeterministic fields without changing casing, array/object shape, status, content type, null/omission distinctions, or numeric values.

The [application-key study](../../../../../docs/research/api-key-reference.md) adds
48 records under the keys-m5d prefix: 47 complete HTTP exchanges and one audit.
The keys are independent of user logins; creation, carriers, list DTOs, permissions,
pagination anomalies, deletion and logout are recorded. All four newly issued
keys and both dedicated logins were retired; all 958 older records and known
source paths retained their hashes. This study does not verify key playback.

The key playback, client-context and target-scope studies add 122 records:
119 complete HTTP exchanges and three audits. Their prefixes are
keys-playback-m5d, keys-context-m5d and keys-scope-m5d. The corpus reached
1128 records at that checkpoint. The later controls resolve earlier combined-policy
ambiguity: explicit catalog targets obey library ACLs; key profile negotiation
uses conversion flags, while IsDisabled or EnableMediaPlayback alone did not
remove the sampled conversion capability. No conversion URL was followed.
See the separately linked research reports for preservation and cleanup.

The [ordinary-device study](../../../../../docs/research/devices-reference.md)
adds 348 records: 346 complete HTTP exchanges and two audits. Prefixes
`devices-m5e-` and `devices-m5e-2-` preserve a partial first capture and complete
continuation, both with successful cleanup. The corpus reached 1476 at that
checkpoint. Reported-ID grouping, option clearing, ordinary-login revocation,
and later registration remain bounded observations rather than full client
or device-family compatibility claims.

The [key-device study](../../../../../docs/research/key-devices-reference.md)
adds 189 records, bringing the corpus to 1665 at that checkpoint: 61 guarded
HTTP exchanges plus an audit, 16 fresh-setup HTTP exchanges plus one non-HTTP
connection-refused observation, 109 fresh-study HTTP exchanges, and one
independent recovery audit. The original final-audit serialization failure is
retained; offline recovery verifies all 109 exports unchanged. The derived
fixture-teardown report is not an additional corpus record. Shared server-device
deletion revokes both tested keys, while cached Session DTOs remain distinct
from credential authority. Hidden header-device Info/deletion and subsequent
key-device recreation were not sampled.

The [ScheduledTasks read-only study](../../../../../docs/research/scheduled-tasks-reference.md)
adds 129 records under `scheduled-tasks-m5f-`: 128 complete HTTP exchanges and
one [audit](scheduled-tasks-m5f-audit.json), bringing the current total to 1794.
It records 22 task definitions, all sampled states `Idle`, read-permission
responses, and interval/daily/startup/system-event trigger declarations. It
does not start or stop a task, change triggers, restart the service, or make
application-key requests. Existing execution-result DTOs are historical data,
not new executions performed by this recorder.

Its audit preserves the preceding 1665 records/3330 raw-export files, 240 known
sources, 2214 private files, twenty listed devices and hidden server device
`15`, their options, and the original service identities. Both fresh login
credentials finish with independently observed `401`; device rows `31` and
`32` remain as revoked-login history. The
[five pure guard tests](../../../../../docs/development/m5f-scheduled-task-recorder-tests.json)
pass without HTTP or capture writes. This is recorder/reference evidence;
Goby's [M5f task service](../../../../../docs/research/scheduled-tasks-plan.md)
has no new product acceptance in this corpus extension.
