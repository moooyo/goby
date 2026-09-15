# Live TV Programs query with no EPG source

Status: **source implementation checkpoint; focused verification independently
accepted; final regression incomplete and no successor artifact built**.
The [focused result](live-tv-programs-focused-verification.json) records 14
top-level passes and 118 subtest passes on the isolated remote PostgreSQL
profile, with independent result/resource review.
The [ordinary regression interruption](live-tv-programs-full-interruption.json)
was requested by the user for plan review and safely closed: 10 of 25 packages
completed with 341 passes, zero failures and zero skips. A further 109 raw
identity-package passes do not complete that package and are excluded from the
completed-package total. No build ran, full verification is false, and no
product assertion failure was observed. This is a user-interrupted partial run,
not a product failure or a passing full suite.

The current target is the Programs successor; its binary and package identities
are not assigned. E11 remains the earlier build/G2 baseline. Recovery and all
three final journey entry contracts must be ready before A is replaced; their
pure-tool preparation may proceed in parallel with final verification of frozen
Go source. Tool-only Python/JavaScript changes do not require another Go full run.
The planned single worker will verify all 25 ordinary packages, build the
ordinary binary, then build one embedded amd64 systemd package. This has not run;
no second build launcher or completed artifact is claimed.
The product decision follows the independently reviewed
[TV browse02 diagnostic](audited-tv-browse02-diagnostic.json) and the
[reference response observation](reference-programs-verification.json).

## Product meaning

Goby currently has no EPG source, broadcast-program ingestion or tuner/recording
implementation. Its recorded Series and Episodes are library items, not a
broadcast guide. The authenticated `GET /emby/LiveTv/Programs` query therefore
has an empty program set after its input and requested catalog subject have
been authorized. This is a defined query for the current zero-source profile;
it is not an empty-success fallback for unsupported operations or failed reads.

The response has exactly `Items` as an empty JSON array and `TotalRecordCount`
as integer zero, with `application/json; charset=utf-8`. It has no `StartIndex`,
invented channel, broadcast schedule, Episode projection, or playable source.
Normal JSON serialization whitespace need not match the reference bytes.
Any future EPG source must replace this zero-source behavior with a real query
before that source can be advertised as supported.

Only the canonical GET operation is selected in this increment. POST Programs,
channels, tuners, DVR and Live TV playback remain deferred. Namespace aliases
are unchanged. This query does not establish full Live TV or client acceptance.

## Input and permission boundary

Authentication uses the existing `requireEmby` middleware. The existing
`embyBusinessQuery` helper separates declared authentication/client carriers
and retains their header/query conflict rules. The Programs handler separately
accepts the observed `X-Emby-Language` hint without changing that shared helper
or other endpoints' allowlists.

Each business parameter occurs at most once. Unknown names, alternate business
name casing and repeated values are rejected. The original request query is
preserved. Framed or otherwise present GET bodies are rejected; the existing
bounded body owner performs the unread-body closure.

| Parameter | Current Goby boundary |
| --- | --- |
| `UserId`, `LibrarySeriesId` | Opaque UTF-8 identifiers of at most 256 bytes, without control characters or a nonempty whitespace-only value. Empty values retain the existing default/no-filter meaning. |
| `HasAired`, `EnableUserData` | Literal `true` or `false` when supplied. |
| `Limit`, `ImageTypeLimit` | Canonical decimal integer from 0 through 2,147,483,647. Omission and zero are valid. These values do not bypass authorization. |
| `SortBy` | `StartDate` when supplied. |
| `Fields` | A nonempty CSV subset of `PrimaryImageAspectRatio` and `ChannelInfo`; order and surrounding spaces may vary, but duplicate or unknown entries are rejected. |
| `EnableImageTypes` | The corresponding CSV subset of `Primary`, `Thumb` and `Backdrop`. |
| `X-Emby-Language` | Endpoint-local transport metadata: name matching is case-insensitive, one value only, at most 256 UTF-8 bytes, no controls or nonempty whitespace-only value. It is not user authority. |

These are explicit implementation limits, not claims about every input the
reference server might accept. In particular, the four-request reference read
did not calibrate malformed parameters, user roles or Series filtering.

`itemUser` selects the subject using the authenticated principal. Ordinary
users cannot use a different `UserId`, an administrator cookie, or a forged
header to gain access. Administrator-selected users use the target's current
library policy. Application credentials retain their independent authority;
they do not silently inherit their creator's user identity. Their explicit
target policies follow the existing application-key catalog contract.

A supplied Series identifier goes through `GetItemFor` under that subject and
must resolve to a visible `Series`. Missing and inaccessible items retain the
existing 404 behavior; a visible item of another type returns 400. Without a
Series filter, `ListUserLibrariesFor` still performs current subject and storage
authorization. `Limit=0` and `EnableUserData=false` never skip this step.
Authentication, authorization, invalid input and unavailable storage keep their
existing errors; they are not converted to successful empty results.

## Evidence and remaining gate

The owned Emby 4.9.5.0 host returned the 33-byte empty query result for the
declared request. That observation used one new login, one Programs GET,
logout and same-token rejection, all independently reviewed. It proves that
response shape for those filters. It does not prove the current entire guide
configuration or a corresponding reference Series: the empty-library host has
no matching Series, so that filter was omitted.

The independently reviewed focused tests cover the recorded query including its locale hint, strict
parsing, real ordinary/administrator/application-key subjects, current library
visibility and credential changes, unavailable storage, and preservation of
nonzero UserData, terminal playback, references and encoding history. Preserve
their exact source scope; do not repeat the reference read, diagnostic or focused
work unless an actual change invalidates its evidence. Full regression and the
successor build remain open after the user-requested interruption.
New original-client acceptance requires a reviewed successor artifact and
current retained state. The old TV, episode, movie and subtitle failures remain
on their original artifacts and are not retroactively waived.

The [successor transition decision](e11-candidate-transition-decision.md) records
the remaining recovery decision and incomplete episode/subtitle entry contracts.
After one A transition, run the three final client journeys and close the audio
reuse bridge before G3. Other M2-M6 profiles remain independent; M7 is deferred.
