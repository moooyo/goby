# Configuration mutation reference

Research date: 2026-09-10 (Asia/Shanghai).
Status: **bounded upstream mutation study and owned-fixture teardown complete**.

This study observes official Emby Server `4.9.5.0` on a fresh disposable
instance. Complete configuration clones, selected Partial writes, and named
encoding writes return empty `204` responses. One malformed Partial request
returns `500` after changing its earlier valid property in subsequent reads.
These observations inform a later Goby compatibility adapter. They do not
establish that Goby's native settings implementation already supports the
ConfigurationService write routes.

## Evidence and isolation

The `configuration-fresh-m5g-` dataset contains **237 records: 236 complete
HTTP exchanges and one audit**. The separate
`configuration-fresh-setup-m5g-` dataset contains **17 setup records**. Together
they add 254 records to the preceding 2051-record corpus, producing **2305
retained reference records** at this checkpoint. The capture transferred
**145,347 response-body bytes in 3.238 seconds**, with no incomplete responses,
capture failure, persistence failure, or cleanup error. Its
[audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-audit.json)
records twelve successful checks and twenty exact restoration observations,
including the final cleanup check.

The [preparation operator](../../scripts/test-env/prepare-configuration-fresh.py)
created an independent program-data root and private network namespace. The
[READY report](../development/m5g-fresh-configuration-setup.json) identifies
unit `goby-emby-configuration-fresh-m5g-20260910-01.service`, PID `3613232`,
server ID `f7fb7ac911e44a42a34c201d786376a5`, and loopback port `18099` inside
that namespace. The existing reference process `3131777` and maintained Goby
process `3570491` were protected authorities, never HTTP mutation targets.
The shared official package remained read-only.

The [mutation recorder](../../scripts/test-env/reference-configuration-fresh.py)
rechecked the attested fresh process, namespace, program-data ownership, and
service sandbox before requests. It issued two new ordinary login credentials
and, after proving the complete application-key list empty, one key with a
reserved application label. It never reused bootstrap or older credentials.
No media, library, task, device, or user-policy mutation was permitted.

Each experiment began with the previously restored baseline. After each
configuration POST, the recorder read the complete administrator total and
encoding configurations and public system information. It accepted changes
only in the fields explicitly selected for that experiment. Before the next
experiment, it restored complete baseline objects and compared the original
unredacted parsed JSON values, including types and property omission. This
is a configuration-value comparison, not a claim that independently serialized
HTTP bodies had identical byte ordering.

The bounds were 240 main-phase requests, 280 total requests, 32 KiB per request
body, 256 KiB per response, and 8 MiB total response bytes. Main and cleanup
phases had separate 360-second and 180-second deadlines. Raw wire bodies,
headers, and credentials remain private; exported fixtures use the pinned
deep secret and URL sanitizer. The policy covers compound fields such as
`CertificatePassword`, nested headers, dictionary keys, and encoded URL secrets.
Redaction markers are not wire configuration values.

## Complete configuration clones and server names

The fresh administrator baseline contains 60 total-configuration fields and
17 encoding fields. The initial configured name is
`Goby Configuration Fresh M5g 20260910 01`.

`POST /emby/System/Configuration` received a complete copy of that total
object with only `ServerName` changed to
`Goby Configuration M5g Owned First`. It returned empty `204`. Subsequent total
configuration and `GET /emby/System/Info/Public` reads showed the new name;
the public server ID remained unchanged. The encoding object and every other
total-configuration property remained equal to the baseline. A complete
baseline POST restored the original values and public name.

Evidence: [full-clone write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-full-name.json),
[total readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-full-name-after-total.json),
[public readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-full-name-after-public.json),
and [restored total](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-full-name-restored-total.json).

This proves the tested complete-clone update and restoration. No empty `{}`
or minimal object was sent to the full endpoint. The result therefore does
not establish its general behavior for omitted fields, default reconstruction,
or partial full-endpoint submissions.

## Partial merge, arrays, and empty values

All following writes used `POST /emby/System/Configuration/Partial` with
`Content-Type: application/json` and returned empty `204`.

| Experiment | Observed readback |
| --- | --- |
| Set `ServerName` and `MaintenanceModeMessage`, then update only `ServerName` | The second name appeared and the previously written maintenance message remained |
| Set `SortRemoveWords` to two reserved strings, then one, then `[]` | Each read returned exactly the submitted array; the old elements were not appended or retained |
| Set `ServerName` to `""` | Total configuration retained `ServerName: ""`; public system information used `ServerName: "test-env"` |
| Set `ServerName` to JSON `null` | Total configuration omitted the `ServerName` property; public system information again used `"test-env"` |

The flat experiment's maintenance message was
`Goby Configuration M5g Owned Inactive Maintenance Message`.
`IsInMaintenanceMode` remained `false` throughout. A complete baseline restore
also returned the initially absent message property to absence. This is a
bounded flat-property observation; nested-object merge rules were not sampled.

The array sequence was
`["goby-m5g-owned-first","goby-m5g-owned-second"]`,
`["goby-m5g-owned-first"]`, and `[]`. It establishes replacement for the tested
string-array property, not the behavior of every collection or object array.

Evidence: [first flat write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-flat-both.json),
[name-only readback retaining the message](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-flat-name-only-after-total.json),
[two words](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-words-two-after-total.json),
[one word](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-words-one-after-total.json),
and [empty array](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-words-empty-after-total.json).

The empty and null cases separately preserve their different total-object
representations. Both public responses show the observed host-name fallback
`test-env`, with the same server ID. The configured and publicly effective
names must therefore be treated separately. Each case was restored before
the following experiment.

Evidence: [empty-string total](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-name-empty-after-total.json),
[empty-string public name](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-name-empty-after-public.json),
[null total](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-name-null-after-total.json),
and [null public name](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-name-null-after-public.json).

## A failed Partial request changes visible state

The atomicity case sent these properties in this order:

```json
{
  "ServerName": "Goby Configuration M5g Owned First",
  "ImageExtractionTimeoutMs": "goby-m5g-invalid-integer"
}
```

The response was `500`, `text/plain`, with the 41-byte message
`Input string was not in a correct format.` Nevertheless, the following
administrator total read showed the new `ServerName`, and public system
information showed that name too. `ImageExtractionTimeoutMs` remained its
baseline integer value `0`. The encoding configuration and other total fields
remained unchanged.

This request was **not atomic with respect to the same process's subsequently
visible configuration**: an error did not imply that the valid earlier field
had been rolled back. The study did not inspect durable configuration storage
or restart the instance while the failed patch was in effect. It does not
prove that the partial change survived a restart, nor that all malformed
patches are applied in the same order. A complete baseline restore then
returned the visible configuration and public name to their original values.

Evidence: [failed mixed write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-invalid-atomicity.json),
[total readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-invalid-atomicity-after-total.json),
[public readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-invalid-atomicity-after-public.json),
and [restoration](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-partial-invalid-restored-total.json).

## Named encoding writes and MIME handling

The administrator submitted a complete 17-field encoding clone to
`POST /emby/System/Configuration/encoding`, changing only
`TranscodingMaxWidth` from `0` to `1280`. The response was empty `204`.
The next encoding read showed `1280`; every other encoding field and the
total configuration remained unchanged. The complete original encoding object
restored the width to `0`.

Two additional no-op writes sent the full baseline encoding JSON, once with
`Content-Type: application/json` and once with
`Content-Type: application/octet-stream`. Both returned empty `204` and left
the configuration equal to the baseline. This establishes that the named
endpoint accepted those JSON bytes under both tested MIME types. It does not
establish arbitrary binary formats, XML support, or omitted-field behavior.

Evidence: [width write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-encoding-width.json),
[width readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-encoding-width-after-encoding.json),
[restored width](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-encoding-width-restored-encoding.json),
[JSON no-op](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-encoding-mime-json.json),
and [octet-stream no-op](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-encoding-mime-octet-stream.json).

No playback or encoder operation ran. The observed stored `0` does not prove
an unlimited-width execution rule. Hardware flags, codec selection, thread
behavior, and GPU execution were not exercised. `RemoteClientBitrateLimit`
was unchanged; this study supplies no basis to equate it with Goby's output
planning `MaxBitrate` setting.

## Write authorization and the unknown named configuration

For each of the total, Partial, and named encoding POST routes, anonymous and
ordinary-viewer requests submitted complete baseline no-op objects. A newly
owned application key later submitted the same three kinds of no-op request.

| POST route | Anonymous | Ordinary viewer | Token-only application key |
| --- | --- | --- | --- |
| `/emby/System/Configuration` | `401` | `403` | Empty `204` |
| `/emby/System/Configuration/Partial` | `401` | `403` | Empty `204` |
| `/emby/System/Configuration/encoding` | `401` | `403` | Empty `204` |

The viewer errors name the `ManageServer` feature. Anonymous errors say
`Access token is invalid or expired.` The denied attempts changed neither
configuration object. The previously documented ordinary-user GET behavior
remains separate: see the [read-only study](configuration-reference.md).

The key's requests supplied only `X-Emby-Token`, with no client authorization
metadata, target user, or playback request. Its total GET returned the full
60-field object and its encoding GET returned the 17-field object. The three
POST successes establish access for the tested complete no-op bodies; they
do not establish every possible configuration-field mutation under a key.
Each was followed by baseline comparisons and explicit restoration.

Evidence: [anonymous total POST](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-anonymous-permission-total.json),
[viewer total POST](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-viewer-permission-total.json),
[key total GET](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-application-read-total.json),
[key encoding GET](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-application-read-encoding.json),
[key total POST](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-application-permission-total.json),
[key Partial POST](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-application-permission-partial.json),
and [key named POST](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-application-permission-encoding.json).

A reserved unknown named key received a complete encoding-shaped object from
the administrator. Its POST returned `500`, `text/plain`, with the 37-byte
message `Sequence contains no matching element`. Subsequent GETs of that
fixed unknown name returned the same error, and both known configurations
remained unchanged. No deletion or filesystem cleanup of a guessed named
configuration was attempted. This observation does not prescribe Goby's error
status for unsupported configuration keys.

Evidence: [unknown POST](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-unknown-named-post.json)
and [subsequent unknown GET](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-fresh-m5g-unknown-named-after.json).

## Restoration, credential cleanup, and teardown

The recorder completed its first live capture attempt. It restored the total
and encoding objects exactly before credential cleanup. The owned key DELETE
returned `204`; a separate protected request with that token returned `401`,
and the complete key list was empty afterward. Both fresh ordinary logouts
returned `204` and their separate protected requests returned `401`.
The fresh library list remained empty, task definitions were unchanged, and
the two users retained their structural and policy fields. Participant
login/activity timestamps were excluded from that user-field comparison.

The audit preserved all preceding **2051 records**, **240 surviving source
files**, and **2854 private baseline files**, the latter including fresh setup
evidence. Its record-file comparison includes 4102 preceding raw/export files
plus 34 setup files, totaling 4136. The original reference and maintained Goby
services remained unchanged. All 236 private wire records matched their stored
hashes, and the new raw/export pairs passed the deep redaction comparison.

The [separate operator cleanup](../development/m5g-fresh-configuration-cleanup.json)
then passed on its first cleanup attempt. It proved PID `3613232` gone and
removed only the attested fresh program-data root, which contained 3,862,841
bytes before removal. Evidence was retained and old-service/file preservation
passed. Stopping and deleting this restored fixture is not a restart test of
the experimental values.

The [recorder's 37 memory-only guards](../development/m5g-fresh-configuration-recorder-tests.json)
and [operator's nine memory-only guards](../development/m5g-fresh-operator-tests.json)
passed remotely. Their earlier fixture-expectation failures remain recorded
separately; the corrected diagnostic field names did not weaken secret
redaction. Those synthetic suites made zero real HTTP requests or fixture
writes and are not substituted for the successful live capture.

| Executed source | SHA-256 |
| --- | --- |
| Mutation recorder | `86a2bbf25843a9b07893b82bc98008c906167a60dd6c339acdde8a9737253402` |
| Preparation/teardown operator | `6aa2f03e2db5fd0028159c55bf6fc2cd22bb33597a9c4cb7166e40b2b14329d8` |
| Pinned deep configuration sanitizer | `baa425eb9680d3e924c4d18d75ad99cc6d0a10ac438ba402501955bb96a359cd` |

## Limits of the result

This matrix does not establish minimal or empty full-POST semantics, general
nested merge rules, mutation durability across restart, settings reload effects
on existing playback, encoding execution at width zero, GPU operation, or full
client interoperability. Network, certificate, path, database, hardware,
automatic-update, and automatic-restart settings were never selected for change.
Goby's native settings and a later compatibility adapter require their own
implementation and acceptance evidence. Neither the native settings work nor
this upstream study should be described as completed compatibility writes.
