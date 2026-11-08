# Administrator capability implementation

Date: November 6, 2026.

The subsequent [core API follow-up](admin-core-api-20261106.md) supersedes the
first-run, update-check, configuration-policy, and recent-playback classifications
below. Its verification belongs to the newer source state; the five-round
results in this document remain historical evidence for the earlier increment.

## Scope and upstream reference

This increment completes the administrator capabilities identified after the
[November design rewrite](admin-design-rewrite-20261104.md). It distinguishes
Goby's native administrator contracts from its Emby-compatible client contracts.
An absent administrator projection is an implementation task, rather than
evidence that Emby lacks the underlying capability.

The upstream baseline is the official
[Emby SDK 4.10.2.0](https://github.com/MediaBrowser/Emby.SDK/blob/45e73012eddf985dcfe878ca031b500085e16496/SampleCode/RestApi/Version.txt),
at commit 45e73012eddf985dcfe878ca031b500085e16496. The initial notification and
backup implementations also used documented plugin product behavior as
reference. The follow-up scope excludes plugin-only parity work.

## Implemented behavior

| Area | Result |
| --- | --- |
| Playback management | The overview and Sessions page show authenticated current playback, actual progress, media specifications, and client connectivity. Native cookie/CSRF commands support pause, resume, stop, seek, messages, and remote play. Every command receipt distinguishes acceptance from observed execution. Native PlayNow selects one item; explicitly supported queue operations can select multiple items. |
| Goby player control | The standalone player registers its actual capabilities, receives authenticated WebSocket commands, and changes the real video element. Native media events produce playback reports. Reconnection, duplicate message IDs, logout, unavailable media sources, and browser playback restrictions retain explicit handling. |
| Media information | Library item lists batch-load technical, artwork, and analysis summaries. File and chapter views expose indexed streams, external subtitles, complete bounded chapter lists, size, bitrate, and duration. Unknown facts remain unknown. |
| User summaries | Login and sampled activity dates persist independently of login retention. User lists and profiles show actual activity and completed-item counts. These counts do not represent total viewing time or per-play history. |
| Provider configuration | TMDB, MusicBrainz, and OpenSubtitles support encrypted saved overrides, replacement, clearing, and deployment defaults. New requests and tasks use the saved snapshot. Bounded connection tests use fixed official endpoints and return redacted outcomes. |
| Notifications | Administrators can manage up to sixteen HTTPS webhook or TLS SMTP destinations, subscribe to actual committed event groups, and request test delivery. Credentials are write-only. Configuration changes fence old queued work; unconfirmed deliveries are not blindly repeated. |
| Backup policy | Daily or weekly automatic encrypted backups use the existing task scheduler. Policies control retention and library-image inclusion. Retention considers only successfully published archives owned by that policy and deployment. Manual backup creation retains its established defaults. |
| Backup image selection | Excluding library images and avatars removes artwork state, manual image payloads, online-provider payloads, and embedded covers from a consistent snapshot. Metadata and source-path references remain. OCR review records retain their text and associated review evidence. |
| Backup completion | A separately classified publication fact follows actual artifact publication. A durable receipt prevents duplicate completion notifications after retries, reconciliation, or activity-log pruning. Recovery validates the sequence consumer retained by that receipt. |
| Global server policy | Global remote-access rules and LAN ranges apply to existing and new HTTP, streaming, and WebSocket requests. Proxy interpretation uses configured trusted proxies. Server interface language initializes a new browser without overwriting an explicit personal choice. |
| Restart | The administrator requests a real, graceful application-generation rebuild inside the existing process. Reopening its listener and retained identities establishes readiness; HTTP acceptance alone does not. This works independently of a container restart policy. |
| Hardware information | The matrix distinguishes compiled codec interfaces, configured support, available devices, and actual qualified encoding samples. Explicit refresh uses the existing bounded probe path; ordinary reads do not start a probe. |
| Directory access | Roots report readable, writable, or unknown observations using held descriptors and filesystem access checks. The observation creates no media files. Existing unique-volume totals remain separate from directory quotas. |
| Episode ordering | Explicit TMDB group selection enables DVD or absolute display/play ordering with its own CAS revision and preview digest. Lists, NextUp, and queues consume the mapping. Canonical item IDs, source numbering, paths, and viewing state remain stable. |
| Online images | Online Logo candidates support preview, download, and application. A separately enabled library policy fills missing images from known TMDB identities after scans and daily checks, preserving existing images, manual overrides, and manual deletion masks. |

The interface retains the supplied sidebar, header, typography, themes, compact
tables, and right-hand editing sheets. Independent credential, access-policy,
and series-order operations retain their drafts and uncertain-write gates
alongside the parent editor. Requests are bounded and cancelled on disposal.

Implementation contracts:

- [Playback and native administration](../api/admin-playback.md).
- [Provider management](../api/provider-configuration.md).
- [User activity summaries](../api/user-activity.md).
- [Administrator notification destinations](../api/admin-notification-destinations.md).
- [Automatic backup policy](../api/backup-policy.md).
- [Global server policy](../api/server-policy.md).
- [Codec capabilities](../api/codec-capabilities.md).
- [Series display order](../api/series-display-order.md).
- [Automatic images](../api/automatic-images.md).

## Decisions without a confirmed standard upstream contract

The following items remain explicit product decisions. Missing public
documentation does not establish that every Emby installation or plugin lacks
related behavior.

| Item | Upstream boundary | Decision |
| --- | --- | --- |
| Complete long-term playback statistics | Core activity provides bounded playback start/stop events, implemented in the follow-up. Complete historical reporting was not established as a core contract. | Reporting-plugin parity is excluded from this task. |
| Independent task enable switch | The public task contract exposes trigger edits and run/stop operations, without an independently writable Enabled field. | Define whether disabling preserves triggers and permits manual execution. |
| Per-image lock and atomic multi-image drafts | General metadata locks and individual image writes do not establish these exact semantics. | Define lock ownership, reset behavior, and transaction boundaries. |
| Backup content diff and guaranteed rollback window | Restore planning and rollback exist in Goby; neither supplies a standard per-item content-diff or fixed-time rollback promise. | Select diff granularity and storage/retention guarantees. |
| Root-to-volume grouping and directory quotas | Path validation does not define a stable root-to-volume identity or directory-quota contract. | Select platform identity, mount-change, and quota semantics. |
| Internal database/schema details | Public system version information does not establish a portable database-schema administration contract. | Expose a Goby-specific diagnostic view if required. |

## Deployment decisions

The follow-up implements real system-update observations from a deployment
selected Goby release source. Container replacement still belongs to the
deployment owner. See [server update observations](../api/server-updates.md).

A writable base-path setting also needs an explicit proxy and routing contract.
The inspected SDK did not establish a uniform writable base-path API. The
current server public URL remains an origin.

Additional hardware backends require actual engine/device qualification.
The current matrix reports the implemented software and VA-API scope.
Episode display ordering uses known canonical identities and an explicit
provider map; DVD filename identity conversion is a separate matching policy.

## Verification

Local verification is authorized by the original task. Windows runs the
production frontend builds and browser checks. Local WSL Debian supplies
Go 1.27.1, an isolated PostgreSQL 17.11 cluster, and native ext4 fixtures.
The designated remote host had insufficient persistent capacity, so no new
remote verification environment was admitted.

PostgreSQL and FFmpeg 9.0.1 tools were extracted into task-owned directories.
Fixed local launchers locate the extracted PostgreSQL shared libraries while
Goby continues to clear inherited loader and credential environment variables.
The application smoke uses a new unprivileged database role, new accounts,
and an existing synthetic WebM fixture. It sends no production notification
or request to a third-party provider.

The real three-application smoke passed twelve checks and seven control
commands, including an actual restart and successful control after reconnect.
Video state and authenticated server reports independently confirm execution.
Four screenshots and redacted receipts are retained. Earlier failed evidence
also remains available.

The formal browser loop binds the administrator and player sources, backend
sources, test specifications, and production assets to one fingerprint. Each
round verifies the served production entry, records its automated checks,
captures screenshots, and requires a separate actual visual review. Two
consecutive clear rounds for unchanged inputs are required.

## Recorded outcome

The loop completed after five retained rounds. Rounds 4 and 5 each passed all
195 browser cases, with zero skipped, unexpected, flaky, or retried cases.
Each round actually reviewed all 202 newly captured screenshots through two
independent partitions. Both rounds recorded zero new modification
recommendations. The recorded status is consecutiveClearRounds=2 and
exitConditionMet=true.

Round 1 retained its seven automated failures and visual findings. Round 2
passed, while round 3 found a previously missed word break in the episode-order
table header. That finding reset the clear-round count. The first-column
layout was corrected and its text geometry gained a regression assertion
before the final two rounds.

The final immutable inputs are:

- Loop source and production fingerprint:
  b38dd950c97ef8367851d4dd9c0a98671cbe6854f9527259c5c28a8d8ba9f88f.
- Administrator production entry SHA256:
  b0e849ae468a70c18559377cca0cd652f0d43c6c56d2b628e4ab8cb151d11f23.
- Embedded Linux amd64 binary SHA256:
  22d1cbdb8df0274eee1e99ad5d5fdc91121c73cdc13176c4f97c8463174cc149.
- Production asset inventory: 276 files, with individual retained SHA256 facts.

Windows TypeScript/Vite production builds passed for both applications. The
Linux amd64 embedded cross-build uses Go 1.27.1, CGO disabled, and trimpath.
Local WSL resource checks passed five parent tests and nineteen subtests
against the latest embedded bundle.

The combined backend suite passed 124 parent tests and 227 subtests with no
failures or skips. A subsequent complete provider suite passed 35 parent tests
and 57 subtests, including a bounded round-robin image selection regression:
large poster/backdrop collections must not starve Logo candidates. Sixteen
frontend option/schedule tests and twelve player remote-control browser cases
also passed.

The current-production real backend/player/administrator smoke passed twelve
checks and seven actual commands, with four screenshots and no browser errors.
It verified media state, authenticated reports, graceful restart, retained
identities, reconnection, and subsequent play/stop. The served administrator
entry matched the production bytes before and after the real restart.

PostgreSQL 17 exports produced trusted recovery catalogs for schemas 69 through
75 from fresh databases. Image omission, encrypted configuration witnesses,
automatic schedule/retention, publication idempotency, and sequence collision
rejection after log pruning received real database coverage. SMTP certificate
rejection uses a real loopback TCP fixture and verifies that untrusted TLS
sends no authentication or mail.

Both npm dependency audits report zero findings after the compatible Vite and
source-map-js fixes. Git diff whitespace checking passed, with line-ending
normalization notices retained in its log. Vite's non-blocking chunk advisory
remains visible.

The application, proxy, preview, and task PostgreSQL service are stopped.
Shared Go caches remain available. Inactive native compiler output and duplicate
remote tool scratch were reclaimed; source, data, tools, binaries, and raw
evidence remain retained. An empty Windows compiler directory was preserved
after automatic command review blocked its removal; it contains no scratch
files.

Raw evidence is retained outside the repository under
C:\Users\moooyo\.codex\tmp\goby-admin-capabilities-20261106. The final closeout,
loop journal, per-round reports, both visual-review partitions, artifact
inventory, real-stack receipts, and original failed attempts are kept there.
