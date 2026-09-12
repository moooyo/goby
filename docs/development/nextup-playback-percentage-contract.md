# NextUp playback percentage and stopped-lifecycle cleanup contract

Status: coordinated remote verification passed **379 guards and eight compile
checks**, plus validation of the three actual retained Episode DTOs against the
new shared API. The wrapper exited 0 and performed zero business HTTP requests
and zero actual process probes. This is a shared contract development result;
it does not establish a new live preparation, matrix result, or original-client
acceptance.

The current producer still admits the historical **8 retained users, 10 retained
libraries, 12 explicit detail witnesses, and release schema version 2**. The
fresh observer and release version 3 for **10 users, 12 libraries, and 24 detail
witnesses** are not integrated. The current admission shape must not be used to
start another actual preparation against the expanded reference population.
Cleanup receipt `contractVersion: 3`, introduced here, is a separate contract
from that future producer release version.

## Actual observation that required this change

The first partial calibration in preparation04 reached an acknowledged STOP,
then returned `PlayedPercentage: 20`. The old four-field playback allowance
treated that added field as unrelated UserData drift and stopped before DELETE.
The retained preparation has 172 requests and zero DELETE requests. Its failed
scope was preserved and was not resumed.

The direct own-token episode responses establish these complete UserData values:

```json
{
  "IsFavorite": false,
  "PlayCount": 0,
  "PlaybackPositionTicks": 0,
  "Played": false
}
```

```json
{
  "IsFavorite": false,
  "LastPlayedDate": "2026-09-12T20:52:59.0000000Z",
  "PlayCount": 1,
  "PlaybackPositionTicks": 1200000000,
  "Played": false,
  "PlayedPercentage": 20
}
```

The bound episode runtime is `6000000000` ticks. The partial position is exactly
120 seconds of a 600-second episode. These are direct full episode observations;
the catalog UserData projection had different exposed fields and does not
replace them.

The separate recovery used a new owned token, matched the retained partial
detail, issued one narrow UserData DELETE, and read the full detail again. The
post-DELETE UserData exactly matched the original four-key zero: both
`PlayedPercentage` and `LastPlayedDate` were absent. Six requests completed,
including logout HTTP 204 and same-token HTTP 401. See the
[independent recovery terminal](nextup-preparation04-userdata-recovery-independent-terminal.json)
and [preparation04 record](nextup-global-reference-preparation-04.md).

All remote paths below are relative to `/opt/goby-test/exec-work-m3e`.

| Direct receipt | Remote path | SHA-256 |
| --- | --- | --- |
| Original complete zero | `reference-nextup-global-preparation-04/private/0110-cal-P-partial-before-zero-response.json` | `f8eab2ca15298f393defd49d63e5eaefb26e4c4e8edd5a80b5b714e035b59822` |
| Partial state after acknowledged STOP | `reference-nextup-global-preparation-04/private/0115-cal-P-partial-before-delete-response.json` | `50782901ce61e3d845c0c09cc921eca55f6d821b3f3526ead4c45bffc515120b` |
| Independently accepted full restoration | `reference-nextup-preparation04-userdata-recovery-01/private/0004-after-response.json` | `3f2639ae13fee114943059376b82a9e3595893f412880f744fbb42c8522bfd9c` |

The independent recovery terminal has SHA-256
`0c2380b1fb0ee0f78b2e6fb8ad21995b779cfaad26c9ef318af93215121ce312`.
Its result concerns the exact owned UserData and token closure; it does not turn
the failed preparation into a successful matrix fixture or erase retained
authentication/playback history. Synthetic reset behavior in guards is not an
actual DELETE receipt.

## Shared Episode-only API

[nextup-global-matrix.py](../../scripts/test-env/nextup-global-matrix.py) exposes:

```python
require_episode_percentage(userdata, *, runtime_ticks)
require_playback_userdata_change(before_userdata, after_userdata, *, runtime_ticks)
```

Both functions validate without rewriting their inputs. The caller must first
bind the actual full **Episode** identity and its observed positive integer
runtime. These APIs do not infer an item type from UserData, and must not be
applied to Series or Season summary objects. They do not authorize a DELETE.

`require_episode_percentage` requires actual boolean `Played`, integer
`PlayCount >= 0`, and integer `PlaybackPositionTicks` between zero and the supplied
runtime. An absent `PlayedPercentage` remains absent. A present value must be a
finite built-in integer or float in `[0, 100]`; booleans, null, strings, NaN,
infinities, and out-of-range values are rejected.

For an integer percentage, use `(numerator, denominator) = (value, 1)`. For a
float, use its exact `as_integer_ratio()`. The validator requires:

```text
numerator * runtime_ticks == 100 * PlaybackPositionTicks * denominator
```

There is no epsilon, rounding, clamping, or integer conversion of the observed
value. Both `20` and `20.0` can satisfy this equation for the observed partial
position; their original primitive types remain distinct in retained facts.
`20.000000000000004` does not satisfy that exact observed ratio.

**`Played=true` with any present `PlayedPercentage` is currently rejected.** No
actual completed-state percentage semantics have been established. The code
does not infer 100 from `Played=true`, or infer zero from a reset resume position.
A completed state with percentage absent retains the pre-existing completion
checks. Supporting a present completed-state percentage requires separately
reviewed real full-detail evidence.

`require_playback_userdata_change` validates both observations using that same
runtime. After the caller proves an acknowledged owned STOP, the comparison may
allow changes only to:

- `Played`
- `PlayCount`
- `PlaybackPositionTicks`
- `LastPlayedDate`
- A present `PlayedPercentage` that passed the Episode validator

Every other field must retain its complete value, presence, and JSON primitive
type. An added unknown field is still a failure. Existing count, playback-date,
partial-position, and completion checks at the call sites remain in force.

`userdata_fact` still retains the entire value, sorted field list, and primitive
type map. Initial baseline and post-DELETE checks still compare the complete
facts. Missing percentage is not equivalent to present zero; a missing date is
not equivalent to null. DELETE HTTP 200 alone proves neither of those restoration
conditions.

## Cleanup receipt version 3

[nextup-global-transport.py](../../scripts/test-env/nextup-global-transport.py)
requires `contractVersion: 3` and the same four ordered calibrations:

1. P / A1 / partial
2. P / A1 / complete
3. Q / B1 / partial
4. Q / B1 / complete

Each calibration row contains `actor`, `item`, `mode`, and these eight existing
events, in this order:

| Event | Required operation and response |
| --- | --- |
| `beforeZero` | Own-token full Episode GET, HTTP 200 |
| `playbackInfo` | Exact item PlaybackInfo POST with the actor's UserId, HTTP 200 |
| `started` | Complete acknowledged playback context POST, HTTP 204 |
| `progress` | Same context and exact mode target position, HTTP 204 |
| `stopped` | Same context and exact target position, HTTP 204 |
| `beforeDelete` | Own-token full Episode GET after the acknowledged STOP, HTTP 200 |
| `delete` | Exact actor/item PlayedItems DELETE, HTTP 200 |
| `afterDelete` | Own-token full Episode GET with exact baseline restoration, HTTP 200 |

The two actual login receipts plus four sets of eight events require **34 distinct
response receipt digests**. Event ordinals strictly increase and completed times
cannot regress. P and Q retain independent token fingerprints, login session
IDs, and preparation device IDs.

PlaybackInfo must acknowledge one actual media source with the bound Episode
runtime. Each of the four play-session IDs must be distinct. Started, Progress,
and STOP bodies must bind the exact item, media-source ID, play-session ID, login
session ID, runtime where applicable, and mode target. STOP must acknowledge
`Failed=false` and `IsAutomated=false`. A standalone STOP assertion or a copied
context is insufficient.

Only after those lifecycle facts agree can the shared comparison admit a derived
percentage change. Full baseline and restoration facts must still be identical;
the same actor's partial and complete calibrations must share their measured
baseline. No extra HTTP request is introduced by retaining these events: the
producer already issued them.

## One DELETE per stopped lifecycle

The [producer](../../scripts/test-env/prepare-nextup-global-reference.py) and
matrix recorder persist DELETE attempts by `(actor, item, PlaySessionId)` before
dispatch. The matrix uses the latest acknowledged stopped lifecycle for that
actor/item. Another distinct lifecycle may have its own reset; an already
attempted lifecycle cannot acquire another DELETE through failure cleanup.

An incomplete response or a complete non-200 DELETE response retains pending
responsibility and stops subsequent HTTP. A retained non-200 response is not
misreported by transport as a physically lost response. HTTP 200 followed by a
nonmatching full zero detail also requires independent recovery, with no repeat
DELETE. In particular, the matrix's normal reset-proof failure prevents a new
automatic cleanup pass.

The permission to compare playback-derived fields does not relax the write
boundary: the actor and Episode must be owned, known playback must be stopped,
and cleanup must satisfy its existing full-detail reconciliation requirements.
The request routes are unchanged. Matrix hard limits remain **300 total, 220
normal, and 80 reserved cleanup requests**.

## Source and coordinated verification scope

| Component | Source | Frozen SHA-256 for this increment |
| --- | --- | --- |
| Shared planner | [nextup-global-matrix.py](../../scripts/test-env/nextup-global-matrix.py) | `a69ff17c26934abbf09375a7832d7e11cd898d5e696c4ba5ce8a718efd19e65d` |
| Planner guards | [nextup-global-matrix-guards.py](../../scripts/test-env/nextup-global-matrix-guards.py) | `e18f06dc57b28ab07754cf0cd04de510ca0cdc94e50cc39b64d92cf72deecdf7` |
| Transport | [nextup-global-transport.py](../../scripts/test-env/nextup-global-transport.py) | `4134c66a58a1542fc3c7dc9007bcd9ae289d094bb7db28a95d59a3557be4ceb8` |
| Transport guards | [nextup-global-transport-guards.py](../../scripts/test-env/nextup-global-transport-guards.py) | `f99fd15148cd18cc8166da83ac9eb1612f4319b6bf31593ce8ef100b80535844` |

The coordinated bundle also includes the
[producer](../../scripts/test-env/prepare-nextup-global-reference.py),
[producer guards](../../scripts/test-env/test-prepare-nextup-global-reference.py),
[outer operator](../../scripts/test-env/run-nextup-global-reference.py), and
[operator guards](../../scripts/test-env/test-run-nextup-global-reference.py),
plus the frozen historical Guid worker required by the release fixture. Their
exact source pins and final outcomes belong to the coordinated report.

Remote bundle location:
`/opt/goby-test/exec-work-m3e/nextup-playback-contract-tool-01/revision-01`.
Verification uses `/usr/bin/python3 -I -B` on `test-env`. The synthetic suites
cover finite exact percentages, full field/type preservation, complete v3
lifecycle context, non-200 pending retention, and failed restoration without a
second DELETE. Retained-evidence checks are separate from synthetic fake reset
behavior and do not issue business HTTP.

The [coordinated report](nextup-playback-contract-verification-01.json) has SHA-256
`3f79f264037d657fd4f2dbae0834caf73d049debc077b232a84684145bc85039`.
All four suites completed successfully:

| Verification | Passed guards |
| --- | ---: |
| [Matrix](nextup-playback-contract-matrix-verification-01.json) | 53 |
| [Transport](nextup-playback-contract-transport-verification-01.json) | 100 |
| [Preparation](nextup-playback-contract-preparation-verification-01.json) | 161 |
| [Outer operator](nextup-playback-contract-operator-verification-01.json) | 65 |
| Total | **379** |

Eight source compilation checks also passed. The separate
[actual retained DTO report](nextup-playback-contract-actual-retained-verification-01.json)
passed checks of preparation04 `0110`, preparation04 `0115`, and recovery `0004`
through the new helper. That check uses the actual retained before/partial/
restored observations; it does not substitute a fake automatic reset for the
independently verified DELETE outcome.

Historical failed and recovery scopes remain immutable. A successful contract
development verification does not supply the missing fresh observer/release
integration, authorize actual preparation with stale population facts, or
establish matrix or client acceptance.
