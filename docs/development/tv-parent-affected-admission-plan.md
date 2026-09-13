# TV parent metadata affected admission

Status: [admission05 and independent closeout passed](tv-parent-affected-admission-closeout.json).
The selected successor
is installed and its [transition is closed](tv-parent-candidate-transition-closeout.json).
This admission covers the changed TV projections and their access path. It
reuses [admission04](audited-candidate-live-admission-closeout.json) for unchanged
backup, restore, configuration and stability contracts. It does not rerun those
business operations or relabel their original binary as the successor.

The current epoch is `76d7cc71be87851271272537795255f9ad7a5f5c3920dd6546e573f42d06bfac`,
with binding `94bd35e5523a56c60a9b712684d02785b05d6924820bb25f60c48ec8d3496c43`.
The new branch in the existing admission tool uses input/report version 3 and
`admissionKind: affected_tv_parent`. A successful result keeps explicit current
epoch, binding and source fields, fresh checks, and the reused admission04
descriptor. Existing version 1/2 contracts keep their original meaning.

The [admission tool verification](tv-parent-admission-tool-verification.json)
passed 26 legacy guards and nineteen new guards, including a saved replay of
the actual successor, binding, source/inactive state, original seed and private
credential bindings through the preflight health boundary. The [client
component verification](tv-parent-client-component-verification.json) passed
56 checks and reused the unchanged adapter/movie proofs. Neither verification
performed live HTTP, SQL, browser or service work. The [selected controller](tv-parent-client-controller-verification.json)
then passed thirty guards and three integrations against the actual saved
admission, historical hosting receipt and selected component sources.

The input is `tv-parent-admission-entry-01/input.json` beneath the retained
delivery root, SHA-256
`8d050faecc0e9b90d8affff60c0d6eb5b87798ce4c37653cc827afcb4d140b12`.
The new output is `candidate-live-admission-05`. The entry report is
`b030a673cf3cc307317c4d55fb2dacaaed5c90f2b266a1923aae64fccd8ccdaf`.
The frozen admission tool is
`791c7733a2e7fb3dc2c7272c72c372af9a2e30f4bb4d44f0424f10952ad230d3`.
Fresh preflight passed, and this input is consumed. The run took 61,601 ms and
completed all twenty normal and four cleanup requests. All seven fresh checks
passed. Independent closeout replayed every response, verified the default and
detail DTOs and denial results, reconciled exact owned rows/sequences and read
back the current process identity. The first saved-only closeout attempt had
assumed a legacy `sample.process` field; its corrected reader uses the actual
response-only samples, saved boundary process/resource identities and current
runtime pin. The original script is retained; no business request was repeated.

Exactly two sessions, two devices and four activity entries were added. All
seventeen sessions are revoked. The seven prior plays, two userdata rows,
inactive database and sequences, control files, environment, media and hosting
are exact. No resource-limit event or process restart occurred. The actual
admission report SHA-256 is
`b73a2d30926c68886bd1674a356e6330eab2072afb53fb1fa1f695c5337f8535`;
the independent closeout is
`86b224298601ff922d6fe136c026b87628282c675b925dae4831ce78068f139b`.
Client acceptance remains open.

## Fixed requests and limits

Use the existing TV browse actor as P and the existing restricted control as Q.
No account, password, policy or catalog change is permitted. The complete
budget is 180 seconds, including 60 seconds reserved for cleanup, at most
20 normal and eight cleanup requests. A successful run needs 24 requests.
The 60-second observation window samples health/readiness at 0, 30 and 60
seconds. It is a changed-runtime observation, not another ten-minute stability
claim. All verification runs through `ssh test-env`.

| Requests | Expected result |
| --- | --- |
| Six health/readiness GETs | Same candidate identity, lease and PostgreSQL process; ready/healthy at all three samples |
| Public server info | Preserved server identity and completed setup |
| P and Q login | Two owned Emby sessions and distinct recorded device identities |
| P and Q Views | Existing accessible views for P; no enabled views for restricted Q |
| P default Seasons, Episodes and recursive TV Items | Exact expected item identities and Series/Season references and names, without a Fields override |
| P Season 02 and Episode 2-1 detail | Exact authorized parent metadata derived from the retained catalog |
| Q Items by the two IDs | HTTP 200 with an empty result |
| Q own-user detail for the two IDs | HTTP 404 with no protected item metadata |
| Q token requesting P detail | HTTP 403 |
| P and Q logout, then same-token checks | Each logout succeeds and its exact token receives HTTP 401 |

No PlaybackInfo, playback, media, administrator login, backup creation, download,
restore planning, cancellation, activation, restart or reference-server request
belongs to this admission. The existing inactive recovery stage is retained.

## State and completion

Bind the new transition's exact `after.json` and its preservation proof before
any authentication. Source and inactive snapshots each contain all 35 tables
and all catalog sequences. All old source rows remain exact, including fifteen
revoked sessions, seven plays, two userdata rows and both Prepared plays.
Successful authentication adds exactly two sessions, two devices and four
login/logout activity rows, with only their proven sequence increments. The
other 32 source tables, inactive database, backup/control files, environment,
media and hosting remain unchanged. Default/detail GETs must not prepare or
expire a play, create userdata or reset history.

Record request responsibility before every operation. Always attempt bounded
closure of known owned sessions, then reconcile actual state and process
ownership. A failure retains its original evidence and cleanup responsibility;
it does not authorize another login or a renamed repeat. Publish admission
only after complete responses, exact projections/denials, the bounded health
window, same-token rejection and all preservation checks pass. Client
acceptance remains open even when this affected admission succeeds.
