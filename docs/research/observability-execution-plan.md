# M5i bounded observability execution study

Status: completed on 2026-09-10. The first controlled capture passed all eight
checks and independent operator teardown completed. The extension adds 96
sanitized records: 15 setup observations, 76 complete capture HTTP exchanges,
one audit and four operator-cleanup HTTP exchanges, taking the corpus from
2366 to 2462. Setup includes one retained connection-refused readiness attempt
on the same fixture; it is not a complete HTTP exchange.

See the [observed contracts and unresolved semantics](observability-reference.md)
and [execution summary](../development/m5i-observability-reference.json).
The sections below retain the executed scope and safeguards rather than
authorizing another run. Goby product acceptance is separate.

## Questions and evidence boundaries

Observe the four documented administrative routes on a completely new Emby
4.9.5.0 instance. Authentication status, DTO property presence, types, paging,
filtering and diagnostic redaction must come from captured responses rather
than from the generated documentation alone.

| Route | Documented input | Required observation |
| --- | --- | --- |
| `GET /System/Logs/Query` | `StartIndex`, `Limit` | Actual `QueryResult_LogFile` shape, ordering, limits and empty pages. |
| `GET /System/Logs/{Name}` | One returned name; optional `Sanitize` | Complete bounded download, content type, omitted/false/true behavior, and local secret removal. |
| `GET /System/Logs/{Name}/Lines` | One returned name | Actual `QueryResult_String` shape and relation to the downloaded file. Candidate paging parameters are separate, unverified observations. |
| `GET /System/ActivityLog/Entries` | `StartIndex`, `Limit`, `MinDate` | Real account-creation activity, identity/date fields, ordering and date-filter behavior. |

The route and parameter declarations are retained in the local
[SystemService inventory](../api/services/SystemService.md) and
[ActivityLogService inventory](../api/services/ActivityLogService.md), derived
from the pinned official schema. The schema describes all four routes as
requiring administrator authentication. Observations must still distinguish
anonymous, invalid-token, ordinary viewer, ordinary administrator and owned
application-key requests. A declared role requirement does not predict the
actual error status or response body.

The deprecated `/System/Logs` and `/System/Logs/Log` spellings are outside this
study. No original-instance diagnostics, filesystem traversal, guessed
absolute file names, external URLs, native cookies or browser UI are used.

## Fixed isolated fixture

| Resource | Fixed value |
| --- | --- |
| Work parent | `/opt/goby-test/exec-work-m5i` |
| Evidence root | `emby-observability-fresh-m5i-20260910-01` under the work parent |
| Program data | `emby-observability-fresh-data-01` under the work parent |
| Unit | `goby-emby-observability-fresh-m5i-20260910-01.service` |
| Private HTTP / configured HTTPS ports | `18101` / `18501` |
| Shared APP | `/dev/shm/goby-emby-reference/package/opt/emby-server` |

The installed package is shared read-only, with the same explicit read-only
bind required by `PrivateDevices=yes`. The service has a private network and
temporary directory, uses loopback only, and disables remote access, automatic
updates, browser launch and automatic restart during setup. Only new DATA and
runtime are service-writable. This fixture has no SOURCE directory, media
generation, library creation, GPU work or external metadata providers.

The systemd `WorkingDirectory` is the new runtime directory. The launcher must
execute `cd "$APP_DIR"` before starting Emby, and process attestation requires
the actual cwd to equal APP. This preserves the already observed requirement
of the relative ELF interpreter `lib/ld-linux-x86-64.so.2`, while avoiding the
known systemd rejection of a `WorkingDirectory` under `/dev` with mount
namespacing.

MemoryMax is 768 MiB, CPUQuota is 150%, and TasksMax is 256. Preparation requires
at least 1280 MiB MemAvailable and 2 GiB free persistent space. Running checks
retain a 384 MiB available-memory floor. Both DATA and evidence growth have
independent 256 MiB ceilings. An exceeded limit records a partial result and
stops only this owned invocation; it does not authorize altering another
process, cache, mount or service.

## Two ordinary credentials and explicit handoff

Normal first-run setup creates the new administrator with the observed
Startup/User `Name` and `Password` form. That administrator creates one new
viewer through the already observed `POST /Users/New` body containing only
`Name`. The viewer must be non-administrator and must establish the previously
observed empty-`Pw` ordinary login. A different result is retained as a failed
setup; no alternative password contract is invented.

Exactly two ordinary logins are allowed across preparation and capture. The
operator hands both live tokens to the recorder instead of logging in again.
Each acknowledgment is retained in memory before later DTO checks or evidence
writes can fail. The acknowledged tokens must be distinct and must resolve to
the two separately reserved users and reported device IDs.

The READY manifest carries `credentialState: "LIVE_HANDOFF"`,
`ordinaryLoginCount: 2`, and `credentials.admin` / `credentials.viewer` entries
with user ID, username, reported device ID, client name, token SHA-256 and the
paths and SHA-256 values of the private credential and login-response files.
Each credential file has exactly these keys:

```text
REFERENCE_USERNAME
REFERENCE_PASSWORD
REFERENCE_TOKEN
REFERENCE_USER_ID
REFERENCE_DEVICE_ID
```

The viewer password may be empty only after its successful login has been
observed. Full bootstrap Users and Devices DTOs, an empty library result, safe
startup configuration and protected-session access for both tokens are retained
as fresh ownership evidence. The manifest also includes the server ID, process
PID/start ticks, invocation ID, cwd, network namespace, exact sandbox properties,
actual mount/device proofs and the immutable historical baseline. The new
server ID must differ from the original reference server ID.

Neither credential file nor a historical token is a source of authorization
for another instance. The recorder uses only these two attested live tokens
and an application key that it later proves was created by this study.

## Bounded capture sequence

The separate recorder first verifies the prepared identity, immutable evidence,
two account/device owners and protected access. It then captures baseline
activity and log listings using the new administrator.

One additional reserved user may be created after the baseline through
`POST /Users/New` solely to provide an attributable, real activity event. That
user is never logged in. Final membership is therefore three owned accounts
and two ordinary credentials. The creation acknowledgment, fresh Users listing
and bounded subsequent Activity observations establish the causal evidence;
the study does not invent an expected activity Type, message or timestamp.

The recorder may create exactly one application key after proving the fresh
key list and reserved application name. It may use that uniquely acknowledged
key only for the four scoped read routes and its independent invalidity probe.
The operator never creates a key. The key is revoked before the two ordinary
tokens, and its subsequent protected request must independently return 401.

Log names come only from the fresh Query response and are restricted to safe
single path segments. One fixed nonexistent literal name is permitted as an
error observation. No slash, backslash, traversal segment, foreign root or
arbitrary path is permitted. The same owned name is used for omitted, false
and true `Sanitize` values, plus one fixed malformed value as an error
observation. Two HEAD observations, one administrator and one
anonymous, are allowed for that same name; redirects are recorded without
following them.

Log-query pagination and Activity pagination use finite offset/limit cases.
Activity `MinDate` candidates are derived from actually observed Date values.
Undeclared Lines `StartIndex`, `Limit`, `StartPosition` and `SearchTerm`
candidates remain explicitly separate experiments: they must not be presented
as documented or supported before the responses demonstrate their behavior.
All cases retain empty results,
unexpected statuses, omitted/null values and complete wire bodies honestly.

## Privacy and finite evidence

Original HTTP bodies, diagnostic files, passwords, tokens and raw command
failure output remain in root-private evidence with exclusive creation. The
operator does not export complete service logs or download diagnostic text.
Its bootstrap JSON exports use the already reviewed deep sanitizer, with both
account passwords and tokens collected before export. No credential appears in
stdout, process arguments or a user-visible URL.

The recorder must treat a server-side `Sanitize=true` response as evidence,
not as a privacy guarantee. Complete downloaded bytes remain private. Public
diagnostic text or summaries require the recorder's local redaction and audit
of known credentials, sensitive structured fields, URL userinfo/query values,
authorization headers and credential echoes. If export cannot be established
as safe, retain the private response and report the failed export rather than
publishing raw text. A log without a secret-bearing example cannot prove the
server's redaction algorithm effective.

The combined HTTP attempt ceiling is 150: at most 24 setup requests, 118
capture requests including a reserved 12 for capture cleanup, and 8 independent
operator-cleanup requests. Setup readiness polling consumes its setup budget;
it does not create additional logins or a second fixture. Responses have
explicit finite byte and time limits. A body that exceeds a limit, fails
Content-Length/EOF completion, or cannot be safely represented is recorded as
partial evidence and cannot satisfy a successful observation.

Bootstrap has a 180-second main deadline and a separate 90-second failure
cleanup budget. Its parent allows 300 seconds to cover both phases and a
30-second supervision margin, so the parent does not kill a child midway
through credential cleanup. Capture has a 240-second main deadline and a
separate 90-second cleanup budget. Operator credential cleanup also has a
90-second total budget, with an independent 40-second slice reserved for each
ordinary account. One account cannot consume the other account's time slice.

Within recorder cleanup, the application key, viewer and administrator each
receive at most 30 seconds, in that order. The evidence phase also has at most
30 seconds and shares the same overall 90-second deadline. The outer alarm
covers authority checks, HTTP and persistence; HTTP restores that alarm, and
expired phases cannot begin another authority check or evidence write.
Recorder initialization failure reserves separate 40-second ordinary-account
slices within its 90-second cleanup deadline.

Cleanup keeps separate request and byte reserves for the application-key and
ordinary-token invalidity probes. Diagnostic or export storage failure must
not skip those owned invalidation requests. Stopping the independently attested
unit remains possible even if API cleanup, evidence preservation or a deadline
check fails.

## Teardown and protected history

The recorder revokes its one application key, then logs out the viewer and
administrator separately and proves each ordinary token invalid with its own
Sessions 401 response. No user deletion, password change, library, media,
configuration or scheduled-task write is part of capture cleanup. Owned user,
device and activity history remains in the disposable DATA until teardown.

The operator independently handles both ordinary credentials, stops its exact
unit invocation and process/cgroup, and removes only its marked new DATA after
the required ownership and preservation checks. There is no SOURCE removal
path in M5i. All private, exported and failed evidence is retained. The root
runner must invoke cleanup even after preparation or recorder initialization
fails; partial invalidity or preservation proof is reported as incomplete.

The protected original Emby PID is 3131777, with server ID
`ec69ef1cf84140e88489c30326529308`. Goby remains PID 3641418, UID 995, start ticks
26048863, binary SHA-256
`62729fa1ba6b7d5f191d598c79a14606cb139bcdec6346ff5c3f1676551afd54`.
No study call targets either service's API or copies its accounts or data.

The pre-study protected corpus contained 2366 records / 4732 raw-export files and 240
permanent media sources. Its union comprises the earlier 2305 records, 12 M5h
setup records, 47 M5h capture records and 2 M5h operator-cleanup records. The
duplicate setup paths already present in the latest baseline are counted once.
All M5h private/raw/media evidence, the 14 failed first-fixture files and their
25-file source bundle, and the successful 21-file second source bundle remain
immutable. M5h DATA02/source02 and the previously retired sources stay explicit
historical deletions; their retained metadata is verified without reopening
removed paths.

The completed study preserves that baseline and adds 96 audited safe records,
bringing the repository corpus to 2462. The two ordinary tokens and the one
application key have independent 401 proofs. The owned process/cgroup is gone
and only the new DATA was removed; private and exported evidence remains.

Existing cache, node_modules and official-package archive symlinks to
persistent storage are outside the mutation scope. The shared extracted APP
and its ownership/provenance markers retain their existing identity.
