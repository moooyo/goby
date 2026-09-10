# Configuration compatibility implementation

**M5h increment complete and deployed: schema 21/probe 6.** The accepted source
implements the [five ConfigurationService routes](../api/configuration.md),
schema-21 name/encoding state, and the corresponding native Settings UI changes.
The complete race suite, browser/restarts, bounded official-reference 4K
execution study, protected deployment and main workflow passed. See the
[verification summary](verification-m5h-configuration.md) and evidence below.
[M5g native settings](verification-m5g-settings.md), schema 20/probe 6, is the
earlier checkpoint; its 1222-test and browser evidence remain historical.
This completion covers the declared M5h increment, not M4, M5, M6, broader
configuration consumers or all Emby compatibility.

## One state model for both surfaces

Migration [0021_configuration_compatibility.sql](../../internal/database/migrations/0021_configuration_compatibility.sql)
extends the existing `managed_settings` singleton with `server_name_mode` and
`compatibility_max_width`. It adds no table or fake user/credential. Existing
null names become `deployment`; non-null names become `custom`. Existing five
override values, revision, and timestamps are retained. The independent width
starts at `0`, constrained to `0..8192`. Database checks require a consistent
mode/raw-name pair, while domain validation also enforces lossless, nonblank
custom text and the existing byte/NUL policy. Probe cache version remains 6.

The four modes are `deployment`, `custom`, `empty`, and `unset`. Deployment
uses the startup default with a null raw name. Custom uses its nonempty raw
name. Empty stores `""`; unset stores null. Empty and unset both resolve the
effective/public name to the actual host name, but total compatibility GET
retains the empty property or omits it respectively. The wire-mode field is
`ServerNameMode`; the domain's optional request member is named `NameMode`.

Server initialization calls `os.Hostname()` once and validates the result,
then freezes it alongside resolved deployment defaults in the store. Failure
to obtain a usable host name prevents settings initialization. Host name,
deployment default, and custom name are separate concepts; no fixed fixture
name or guessed server identity supplies fallback. New requests and new key
name snapshots use the effective value. Existing key/client snapshots retain
their recorded names and authority.

Restart reloads the stored modes and overrides. A changed deployment name can
affect deployment-mode effective values, and a changed host name can affect
empty/unset effective values. Neither startup input is copied into raw name
storage or advances the stored revision merely because a process restarted.
The [M5h browser acceptance](m5h-configuration-browser.json) verifies these
schema-21 behaviors across two isolated restarts, each preserving all 28 tables
exactly, including a restart with changed deployment defaults.

## Section writes, authorization, and publication

The HTTP adapter authenticates before checking sections, queries, or bodies.
Named reads require administrator/key authority under the
`ReadServerConfiguration` denial label; POSTs use `ManageServer`. These labels
describe the compatibility permission response, not new independent writable
user-policy flags. A valid viewer's total GET is precisely `{}` without the
administrator DTO or initialization state. Invalid or expired credentials
remain unauthorized.

`GetConfiguration` and mutations use catalog-owner transactions with current
Emby administrator/key authority. Credential/account locks precede the settings
row lock. Authority is checked after waits and as the final database operation
before commit. Application keys retain parent/key/client-context checks;
display metadata is not authority. Native operations continue to require their
separate administrator-cookie audience and CSRF boundary.

Compatibility mutations do not require a client revision. They transform only
their selected section of the latest locked settings record. Full server POST
sets the name to custom/empty/unset, with absence meaning unset; Partial only
touches a present name; complete named encoding POST replaces the independent
width, with absence meaning zero. Unrelated native bitrate, width, height, and
channel overrides remain intact. Server-name operations preserve compatibility
width, and encoding operations preserve the name. Native Update still uses
revision CAS and replaces the complete native override set.

`IsStartupWizardCompleted` is the sole read-only total-configuration field.
Its predicate matches identity initialization: the true setup marker or any
surviving account closes setup. A supplied value is compared inside the same
transaction and cannot change that predicate. A false/mismatched or invalid
value causes the entire write to fail without publishing a proposed name.
Unsupported fields likewise cause whole-request rejection; there is no store
for arbitrary options without a consumer.

All writers share the existing publication mutex across commit and atomic
snapshot publication. Changed raw overrides, name mode, or encoding width
advance one common revision; a true persisted no-op does not. Native stale
revisions therefore detect intervening compatibility changes. Posting a full
GET projection can change deployment mode into custom mode, so equal visible
text alone is not a no-op test. Direct SQL divergence is rejected rather than
silently published.

Runtime requests capture native effective values and compatibility width once
at entry. Publication after commit is synchronous and cannot be skipped merely
because the HTTP caller cancelled. A process failure after commit is recovered
by loading the committed row at startup. Ordinary runtime reads use immutable
snapshots without settings I/O; management GET waits for publication. The same
state supplies both DTO surfaces and planning without separate configuration
caches that could lose each other's writes.

## Native API and dashboard extension

The three native route paths remain unchanged. The accepted
[native API](../api/settings.md) extends the earlier M5g contract as follows;
the older acceptance report
continues to describe its schema-20 checkpoint.

| Surface | Accepted schema-21 extension |
| --- | --- |
| Native GET/PUT/reset response | Nine top-level fields: `Revision`, `ServerNameMode`, `Defaults`, `Overrides`, `Effective`, `Sources`, `Encoding`, `UpdatedAt`, `Deployment` |
| `Deployment` | Adds read-only `HostName` to the preceding seven safe startup fields; credentials and internal paths remain excluded |
| `Sources.ServerName` | `deployment` only for deployment mode; `database` for custom, empty, and unset |
| `Encoding` | Exact object `{TranscodingMaxWidth: integer}`; zero is explicit, not null |
| Native PUT | Still requires `Revision` and all five `Overrides`; optionally accepts `ServerNameMode` and `Encoding` |
| Native reset | Select one to six unique fields: the five native names or `TranscodingMaxWidth` |

When `ServerNameMode` is supplied, raw `Overrides.ServerName` must match the
mode: null for deployment/unset, `""` for empty, and a valid nonempty string for
custom. If mode is omitted, the schema-20 rule remains: null means deployment,
and a non-null name means custom. Therefore an empty string without explicit
empty mode is invalid, and an old full-native update does not preserve an
unset mode automatically. A mode-aware client should carry the current mode.

Omitting native `Encoding` preserves the current compatibility width. Supplying
it requires its sole width member, an integer `0..8192`; null, `{}`, or extra
members fail. Resetting `ServerName` returns it to deployment mode. Resetting
`TranscodingMaxWidth` sets only the extra ceiling to zero, while resetting native
`MaxWidth` leaves the extra ceiling intact. Native wire fields remain exact-case,
revision remains a canonical decimal string, and the existing 16 KiB strict
JSON, no-query, cookie/CSRF, and conflict rules remain in force.
The reset selector is `TranscodingMaxWidth`, not `Encoding.TranscodingMaxWidth`;
`ServerNameMode` is not itself a reset selector.

The dashboard presents three administrator choices: Deployment default, Host
name, and Custom name. Host name represents either stored empty or unset;
loading and editing an unrelated setting preserves the exact saved mode.
Explicitly selecting Host name chooses empty mode. The UI sends explicit mode
and encoding state, displays actual host/deployment names separately, and shows
the additional width alongside the resulting combined ceiling. It retains
selective reset, drafts for unselected fields, conflict/uncertain-result reload,
and dirty-navigation protection. It remains an administrator interface.

## Independent width ceiling

`Encoding.TranscodingMaxWidth` never replaces native `MaxWidth`. Planning copies
startup execution configuration and applies the native effective ceilings,
then combines widths as:

```text
combinedMaxWidth = nativeEffectiveMaxWidth
if compatibilityTranscodingMaxWidth > 0:
    combinedMaxWidth = min(nativeEffectiveMaxWidth, compatibilityTranscodingMaxWidth)
```

Native `Effective.MaxWidth` continues to report the native value, not this
combined runtime width; the dashboard derives the combined value separately.

For example, native width 1920 with extra width 1280 yields a 1280 ceiling;
extra width 0 yields 1920, not unlimited output. Native width 960 with extra
width 1280 still yields 960. These are ceilings, not a promise that every
source, codec/profile, aspect ratio, or user policy produces that width.
Other native limits remain unchanged. Hardware/resource selections stay
startup-only; this field does not enable conversion or select a GPU.

New PlaybackInfo and media planning use the request snapshot. Already
registered HLS/conversion plans retain their concrete output settings and
current authorization/source revalidation. A progressive PlaybackInfo URL
does not reserve a producer: its subsequent GET/HEAD plans again and can reject
exact URL choices that exceed a newly tightened ceiling. A changed revision
alone does not promise a new encoder if the concrete plan remains reusable.

This composition and missing-field reset meaning are explicit Goby policies.
The earlier [reference reads](../research/configuration-reference.md) and
[mutation study](../research/configuration-mutation-reference.md) observed
encoding values and bounded writes. The later
[Emby 4.9.5.0 4K experiment](m5h-encoding-width-reference.json) produced
1280 by 720 H.264/AAC at configured width 1280 and 3840 by 2160 at zero, with
eight fully decoded frames in each output and actual software-command proof.
That zero-valued case had no extra width clamp. It does not establish universal
unlimited output, an Emby missing-field reset contract, or GPU coverage.

## Reference differences and remaining consumers

The M5g mutation study's invalid mixed Partial write returned `500` while a
valid earlier name remained visible in that process. It did not establish
durable state after restart. Goby uses atomic validation/rejection instead.
Its unknown named configuration is `404`, known unimplemented devices/DLNA
sections are `501`, and the body is JSON-only despite broader upstream schema
declarations. The emitted fields are intentionally backed by current behavior;
case-sensitive-ID flags, port projections, and other values with unproven
semantic equivalence are not synthesized.

The current field set is an increment, not a reduction of the
[implementation scope](../api/implementation-scope.md). Remaining consumers and
their evidence obligations include:

| Configuration area | Work before exposing and accepting it |
| --- | --- |
| Sorting options such as `SortRemoveWords` | Apply consistent catalog/query ordering and establish update/reindex effects |
| Maintenance mode and message | Define and enforce admission/client behavior, lifecycle, and authorized recovery |
| `RemoteClientBitrateLimit` | Establish client/network classification and apply the intended negotiation/execution budget |
| CRF/codec-specific encoding options | Map values to supported encoder plans and verify resulting commands and media |
| Software/hardware tone mapping | Implement pixel/color and output-metadata behavior with actual supported execution evidence |
| Metadata language/country and provider settings | Connect preferences to provider/scanner consumers and document refresh/provenance behavior |
| Network, certificate, startup and restart settings | Define supervisor/deployment ownership and real reload/restart behavior |
| Logs, audit, preferences and operational settings | Complete typed contracts, access rules, consumers, and lifecycle evidence from the wider scope |

DLNA, camera-upload configuration, and other named domains need their own
capability decisions and implementations. Additional task executors, product
backup/restore, hardware coverage, and full client acceptance remain open.

## Verification and deployed acceptance

The [settings/database run](m5h-settings-database.json) passed 41 top-level tests;
the [native and compatibility HTTP/DTO/media run](m5h-configuration-http.json)
passed 37. Both used the race detector with zero test skips, no race warning,
and Go exit 0. Real HLS output was produced and decoded at widths 160, 96, and
160 as the extra ceiling changed from zero to 96 and back. Native width stayed
160, and the old registered plan/output was preserved. This supports Goby's
ceiling policy, not Emby's zero-width execution semantics.

The [first HTTP attempt](m5h-configuration-http-attempt-1.json) retains 12 passes
and one failure: the test helper's noncanonical CSRF header produced a correct
`403`. Using `Header.Add` and the expected Origin corrected the fixture;
production behavior was unchanged. The [verification summary](verification-m5h-configuration.md)
keeps that failure separate from the later passing run. These targeted results
are subsets of the later full run, not additional unique tests.

The [complete race run](m5h-full-race.json) passed **1252 top-level tests across
fourteen tested packages**, with no failed or skipped top-level tests and no
race warnings; `cmd/goby` has no test files. The [final source gate](m5h-final-go-source-gate.json)
binds those tests, the browser and the accepted binary to 429 Go/module/SQL
inputs, ten test-data files and 50 current dashboard assets.

The [browser workflow](m5h-configuration-browser.json) passed 16 checks in
**7.808112 seconds** without skips or retries. Two isolated restarts preserved
all 28 tables, name mode and extra width. Unset mode retained host fallback
after deployment-name changes; null numeric overrides adopted replacement
defaults. Both new native sessions were revoked and the shared service was
unchanged during that isolated workflow.

[Deployment](m5h-deployment-evidence.json) passed its first attempt and installed
schema 21/probe 6 at PID `3641418`, UID `995`, start ticks `26048863`. Binary
SHA-256 is `62729fa1ba6b7d5f191d598c79a14606cb139bcdec6346ff5c3f1676551afd54`.
The complete backup at `/opt/goby-test/backups/m5h-20260910` preceded service
stop. An actual isolated restore verified all 28 old tables as exact raw rows
and removed its temporary database; no live database restore occurred. The
upgrade preserved old business fields, the old settings values/revision/times,
server identity, task history, master, runtime/unit configuration and eleven
media files. New mode/width fields were backfilled without a scan.

The [main verifier](m5h-deployed-configuration.json) passed its first attempt in
**0.901 seconds**, with 23 GETs, five POSTs, one PUT, one DELETE and 25 separate
read-only verifier SQL queries. A fresh ordinary Emby administrator changed the
Partial name and named encoding width, producing two shared revision advances.
After its logout and SQL revocation barrier excluded late non-CAS writes, one
fresh native cookie restored all original overrides, deployment mode and zero
extra width with CAS. Both credentials were revoked and independently denied
with `401`; final SQL and complete-table audits confirmed restoration. Two
revoked sessions and one ordinary device remain as history, with no Touch
timestamp changes in this run. All old rows in the other 27 tables are exact;
only the managed revision/update time remain advanced. The main process and
protected inputs/media were unchanged; this workflow issued no planning,
playback, scan, task execution, key creation or service restart.

The [4K reference report](m5h-encoding-width-reference.json) adds 61 sanitized
records to the previous 2305, for **2366** total. It proves bounded software
outputs, complete decoding, baseline restoration, producer stops, credential
invalidation and fixture cleanup. Its prior startup attempt is retained
separately; successful media capture made no retries.

M5h is complete within its closed field and consumer scope. The remaining
configuration areas above, M4, M5, M6 and the full Emby/API goal remain open.
