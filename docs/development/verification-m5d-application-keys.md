# M5d: application keys

Verification date: 2026-09-10 (Asia/Shanghai).
Status: **focused and complete race tests, isolated browser/restart acceptance,
main deployment, and deployed workflow passed**.

This increment implements the [application-key API](../api/application-keys.md),
native administrator page, recoverable secret vault, and userless client and
playback contexts. The [operations document](application-keys.md) describes the
persistent master file and recovery requirements. This report records bounded
acceptance, not a complete compatibility or release certification.

## Source and environment

Functional, database, media, and browser verification ran through `ssh test-env`.
The focused tests and disposable browser database used PostgreSQL on port
15432. The browser application ran as UID 995 with a dedicated database and
low-privilege role. Its Node version was 20.19.2 and Playwright was 1.63.0.

The source snapshot contains 337 Go/module files, schema 16, and probe version
6. The accepted browser build contains 40 administrator assets. Artifact
identities are:

| Artifact | SHA-256 |
| --- | --- |
| Accepted Linux executable | `13857f9c33312bc13aef56d5d881c4a0fad23399ceb8a9dafa43e24c0fb2bad5` |
| Administrator asset archive | `20929adc44871b5ce74ad57b8f3a0d82283005ddea4920afbc31953301a699ca` |
| Final Go/module manifest | `e8f4a5ebfc9a4ec3564d895e01930d51b9f01ad2cc95cc0ba0d5c4ff16077ca2` |

The final manifest includes the later correction to an existing metadata
migration test. That test-only correction has its own regression result below;
it did not change the production executable or browser assets.

The [base key study](../research/api-key-reference.md) and three extensions for
[playback](../research/api-key-playback-reference.md),
[client context](../research/api-key-context-reference.md), and
[target scope](../research/api-key-scope-reference.md) brought the retained
reference corpus to 1128 records. That evidence is preserved in pushed
checkpoint `a631dd3`. Reference captures and Goby acceptance remain separate
sources of evidence.

## Focused race and integration acceptance

The [focused result](m5d-targeted-summary.json) records **75 top-level tests
passed across seven packages, zero skips**, with Go and the SSH wrapper both
exiting zero. The command was:

```text
go test -race -count=1 -v -p 1 ./internal/config ./internal/database ./internal/identity ./internal/library ./internal/transcode ./internal/events ./internal/server -run 'ApplicationKey|ApplicationClient|ApplicationRemote|ApplicationCredential'
```

The exercised contracts include:

- Independent server credentials, exact native safe metadata, native
  cookie/CSRF isolation, strict request parsing, deterministic pagination,
  compatibility full-token listing, duplicate labels, reveal, and revocation.
- AES-256-GCM recovery, credential-bound ciphertext, restart decryption,
  concurrent creation, unsafe-file refusal, and failure isolation when the
  master is missing or wrong. Hash authentication, safe metadata, and
  revocation remain available when active secrets cannot be decrypted.
- Migration 16 preservation of existing login and playback records, nullable
  user ownership, real application-client foreign keys, and
  `NULLS NOT DISTINCT` uniqueness. No synthetic user or extra login supplies
  application-key identity.
- Independent contexts under one parent key, the 256-context cap, capabilities,
  NowPlaying, playback ownership, and parent-wide revocation. Real WebSockets
  receive commands only in their target context; revoking one parent closes
  its contexts while sibling credentials remain connected.
- Explicit target-user catalog ACLs and user-state projection, alongside
  userless global access without `UserData`. Explicit favorite/played writes
  remain separate from userless playback reports.
- Profile negotiation combines global execution limits with the target's three
  audio-transcoding, video-transcoding, and remux permissions. Account
  disablement and the general media-playback flag alone do not replace those
  independent conversion permissions for an application credential.
- Revocation races and stale actors. A database barrier pauses a target update
  until its administrator actor expires; final authorization rejects the
  operation and the entire state change rolls back.

Static review identified two P2 behavior gaps that were corrected before this
acceptance: token-only Ping now restores the owned nondefault client context,
and minimal userless PlaybackInfo omits `DefaultAudioStreamIndex`. Explicit
PlaybackInfo `UserId` values are validated even without a profile. Real
PostgreSQL HTTP tests verify Ping extends only the recovered playback, keeps
sibling playback and personal user data unchanged, and checks the audio-field
distinction. This does not establish every language or audio-preference rule.

The real-media integration uses FFmpeg and ffprobe: it reads and verifies an
HLS segment, produces H.264/AAC progressive MP4 at the requested 96 by 54 video
size, probes it, and decodes it successfully. Encoding rows retain userless
parent/context ownership, another key cannot read the first key's HLS child,
and revocation denies subsequent master and segment requests. Personal playback
data remains unchanged. These are real bounded conversion tests, not GPU or
complete-client playback coverage.

## Browser acceptance, restart, and cleanup

The [accepted browser workflow](m5d-application-keys-browser.json) passed one
scenario in **6.603685 seconds**, with zero unexpected failures, skips, or flaky
tests. Setup issued 32 real keys, including three revoked history rows; the UI
created one more, for 33 issued keys in the workflow. No fabricated credential
rows were used.

The journey checks creation, explicit reveal, clipboard failure fallback,
dialog-close secret removal, 25-row pagination, literal and multibyte search,
revoked-history filtering, refresh, desktop/mobile layout, and cancellation
without revocation. Native mutations carry CSRF and exact JSON bodies. Two
distinct userless contexts authenticate with the target key. Revocation denies
both while the sibling key stays usable; repeating revocation retains its
timestamp, and revealing the revoked key returns `409`.

The checks find no raw secrets in the closed-dialog DOM, form values, URL,
browser storage, or captured private logs. Four explicit screenshots are
masked or secret-free: [created dialog](screenshots/application-keys-created-masked.png),
[desktop](screenshots/application-keys-desktop.png),
[mobile](screenshots/application-keys-mobile.png), and
[revocation dialog](screenshots/application-keys-revoke-mobile.png).

Restart preserves the key metadata and the same private 32-byte, mode-0600,
UID-995 master file in its mode-0700 directory. The original sibling token is
decrypted and still authenticates; the revoked parent remains denied for both
contexts. All ten cleanup checks pass, including application and tagged-process
shutdown, removal of the temporary database, role, runtime, and credential
files, exact HBA restoration, unchanged preexisting database catalog, and an
unchanged shared application service.

The [first browser attempt](m5d-application-keys-browser-attempt-1.json) failed
with `Target crashed` at the close action immediately after the masked creation
dialog screenshot. Its recorded scratch free space was only 69,885,952 bytes.
That observation does not prove disk pressure caused the browser crash. After
two verified duplicate old archives and two unused candidate binaries were
removed, approximately 150.5 MB was released; Go work had stopped before the
retry. The unchanged product and runner then passed. Both attempts record the
same executable, assets, verifier, shared-runner, and browser-spec hashes. The
failed attempt also passed every cleanup check.

## Full regression

The [first complete race run](m5d-full-race-attempt-1.json),
`go test -race -count=1 -v -p 2 ./...`, passed 1045 top-level tests and failed
one existing test, `TestMetadataMigrationFromThirteenPreservesEveryExistingTableAndProjection`.
It recorded zero skips and no race findings, but Go and the wrapper exited 1;
this attempt is not a passing full-suite result.

The old test still expected schema 15. Its expectation now follows schema 16;
historical-row comparisons exclude only the newly added nullable client column
in the affected tables, and both new key tables must remain empty. Existing
historical fields and projections are still compared. No production code was
changed for this correction. The [separate regression](m5d-metadata-migration-regression.json)
passed the corrected test under `-race -count=1` in 1.191 seconds.

The [final complete race run](m5d-full-race-summary.json) passes **1046 top-level
tests** across all twelve tested packages, with zero skips and no race findings.
Go and the SSH wrapper both exit zero. The server package takes 476.852 seconds;
identity takes 132.830 seconds. The corrected legacy migration test is included
in this successful complete run.

## Main deployment

The [main upgrade](m5d-deployment-evidence.json) passes with schema 16, PID
3494032 and UID 995. Its binary matches the accepted browser executable. All
337 Go/module files and sixteen migrations match the verified source, and all
40 current administrator assets match the accepted archive and actual HTTP
index/entry responses. Older immutable assets remain available.

Every preexisting business field is preserved. The two new key tables are empty
immediately after migration; existing playback, encoding and client-reference
rows receive only a null application-client column. Authentication credentials,
metadata, user state and the 41 historical scan jobs retain their prior values.

The test service receives one additional writable directory,
`/var/lib/goby-test/application-key-vault`, while retaining its prior transcode
cache scope. The operator provisions a private UID-995, mode-0600, 32-byte
master file inside that mode-0700 directory and preserves all earlier environment
assignments. A protected backup contains the preceding executable/index,
schema-15 database dump, environment and matching master file. It remains at
`/dev/shm/goby-m5d-deployment-backup`; it is not a shipped backup/restore feature.

The [first deployed workflow](m5d-deployed-application-keys-attempt-1.json) stops
at an incorrect verifier expectation: a recursive catalog query excludes the
five collection roots and returns sixteen items, while the verifier expected
all 21 stored rows. No product change is required. Both issued keys and the
new administrator login are revoked, no playback is created, and cleanup proves
that all old rows, source and vault remain unchanged. The corrected verifier
checks the sixteen recursive items and the five nonrecursive collection roots
separately, with their union covering all 21 stored items. Attempt 1 and its
exclusive marker remain unchanged; an explicit attempt number selects new
output names and never overwrites an existing attempt.

The [second deployed workflow](m5d-deployed-application-keys.json) passes in
**1.064 seconds**, with 30 GET, 22 POST, and one DELETE request and no automatic
HTTP retries. It uses the accepted executable on PostgreSQL port 5432. Its
remote report is `/opt/goby-test/m5d-deployed-application-keys-attempt-2.json`;
the adjacent `.attempt.json` marker records this explicit invocation.

The workflow creates two independent application keys and six client contexts,
including their seeded defaults. It verifies native safe listing, secret
decryption, native-cookie/CSRF isolation, userless catalog and explicit-user
projections, and the bare-array Users response. Two context-owned playbacks
negotiate direct media, each reads 32 actual Range bytes, and token-only
progress/Ping/stop requests recover the correct existing context. Ping extends
the selected playback's expiry naturally; capabilities and NowPlaying remain
isolated between clients.

Revoking one parent closes both of its real WebSockets and denies both clients,
while the sibling remains authorized. Revoked-key reveal returns 409 and repeat
revocation retains its timestamp. Logging out the sibling key returns 204 and
denies both of its contexts. Cleanup revokes all three new credentials,
including the administrator login, and stops both new playbacks. It reports no
cleanup errors.

All preexisting rows across the nineteen public tables remain byte-exact,
including the first attempt's two revoked keys and two client contexts and the
old expired Prepared playback. Only the authorized new credential/context and
playback rows are added; the final key history contains four keys and eight
contexts. The source file, master vault, service identity and executable are
unchanged. This live workflow does not restart the shared service or execute
an encoder; restart durability and bounded software conversion are established
by the separate acceptance above.

Final packaging review adds `StateDirectoryMode=0700` to the supplied Linux
unit and a persistent master path to its environment example. The maintained
test provisioning script now creates the private vault directory, preserves
existing master bytes and configured values, and grants the exact vault write
scope. Its updated source passes remote `bash -n` (SHA-256
`cd00b3d7ba521fbdca960b7638062e0c81d2a41e78904ddaafc41aa9ac11f7c3`).
The provisioning script was not rerun against the shared service: the deployed
operator already established and verified that directory and write scope.

## Remaining scope

This remains a prototype increment. Complete real-client interoperability,
GPU conversion, and full compatibility-release acceptance remain open. The
reference studies cover their recorded routes, policies, negotiation, and
bounded original reads; their conversion URLs were not executed. Product
backup/restore and turnkey master-key rotation are not implemented.
