# Test-environment capacity governance - October 5, 2026

Capacity maintenance is complete: five inactive private Go build caches reclaimed
9,201,422,336 bytes. Limited further classification found no eligible additional
Go cache/scratch targets. Final maintenance-receipt /dev/sda1 availability is 14,030,917,632
bytes (about 14.03 GB), with the 1 percent reserve unchanged. Source/data/evidence
and shared services/caches are retained.

This documentation-only delivery starts from main
`5483acc924d5a615bd41dee1bbe7ade3dc76b781`. The persistent
[cache and capacity policy](test-env-capacity-policy.md) makes the lifecycle
reusable, and [repository agent instructions](../../AGENTS.md) direct future
agents to read it before new environments. Historical consumed runners and user
testing rules are unchanged. No product code, product tests/builds, performance
matrix or Docker publication is part of this maintenance.

## Initial occupancy and attribution

The read-only inventory observed /dev/sda1 ext4 availability of 4,859,138,048
bytes (4.859 GB), with the existing 403,374 reserved blocks (1 percent) retained.
Shared build cache `/root/.cache/go-build` occupied 563,388,416 allocated bytes;
shared module cache `/root/go/pkg/mod` occupied 920,547,328 allocated bytes.
Both remain retained.

Recent inspected standard-cache objects contain Goby compilation artifacts.
Their content and timestamps establish artifact provenance, not the actual
compiler or launcher identity. No such writer was captured in the inventory
snapshots. The only observed Go-related process was the shared Goby service
PID 426773/start 72128478, `/usr/local/bin/goby`, with no configured cache
variables. Its existence is not evidence that it wrote the cache. Writer identity
and any cause of later growth remain unknown rather than inferred from timing.

Five private directories have the standard Go cache top layout: 256 hexadecimal
subdirectories plus README/trim metadata. Their initial allocated sum is
9,201,483,776 bytes (9.201 GB) before this maintenance. Local
runner/closure provenance binds their selected history; historical closure alone
is not a fresh inactivity proof. Root released exact `go clean -cache` only after
current canonical-path, owner/liveness and layout confirmation. Whole old roots
are not cleanup targets.

| Selected private build cache | Before allocated bytes | After allocated bytes | Actual outcome |
| --- | ---: | ---: | --- |
| `/opt/goby-performance-remeasure-20261003-01/cache` | 570527744 | 12288 | Cleaned; siblings/services unchanged |
| `/opt/goby-scan-authority-optimize-20261003-01/cache` | 1188614144 | 12288 | Cleaned; siblings/services unchanged |
| `/opt/goby-scan-operation-authorization-20261004-02/cache` | 1421598720 | 12288 | Cleaned; siblings/services unchanged |
| `/opt/goby-performance-roadmap-20261002-01/cache` | 3054223360 | 12288 | Cleaned; siblings/services unchanged |
| `/opt/goby-stop-read-isolation-20261003-01/cache` | 2966519808 | 12288 | Cleaned; siblings/services unchanged |

All five cache records are cleaned: allocated bytes fell from 9,201,483,776 to
61,440, an exact reduction of 9,201,422,336 bytes. Small cache roots/metadata remain.
The first cleanup window's available space rose from 4,829,835,264 to
14,031,257,600 bytes; its observed delta matches that cache reduction. The earlier
4,859,138,048-byte inventory sample is a separate point, not the cleanup baseline.
Every target recorded empty active references and unchanged retained siblings and
service identities. Shared root build/module caches were not cleaned. The receipt
is `first-batch-cleanup-receipt.json`, SHA-256
`8be51827b5f9561a3904c6c0b0da1af818d396495a320148596340c0bcdb84da`.
These are actual first-batch results, not a prediction of another batch or a
reservation for future work; filesystem availability can change concurrently.

## Retention and recurrence prevention

Shared build/module caches, source snapshots, raw/failed evidence, receipts,
independent binaries, database/media data, artifacts and services are retained.
No whole historical root, shared Docker object, other project or module cache is
pruned. The 1 percent reserve remains unchanged.

The limited /opt/goby-test scan and inclusive-depth correction found no build-work
directories; its 35 cache-named directories did not have standard Go cache layout.
Eligible additional Go caches were zero. All 123 direct /var/tmp entries lacked
the standard go-build-plus-digits scratch form; eligible scratch was zero. The
largest entry was the 2,741,915,648-byte Android NDK bundle. PowerToys/agentic-review
source, evidence and toolchains remain retained. A cache-like name or large size
does not make an old caller's source/toolchain directory disposable. This bounded
classification does not assert that every cache on the host was inventoried.

The earlier metadata readback reports /dev/sda1 available bytes 14,031,228,928 and
reserved blocks 403,374. It is separate from the first cleanup's 14,031,257,600-byte
sample. No second cleanup batch was selected. Earlier metadata SHA-256 is
`6aed5c6b9e97a79120b9b801c3eac97d72f1125eaba9fb1301de217db36d8aa9`.

Ordinary runs reuse the current shared build/module defaults. Respect explicitly
selected isolated environments. When isolation is required, create an empty,
budgeted private GOCACHE rather than copying a large build/module cache per task.
Keep media-fixture TMPDIR physical-filesystem requirements separate from compiler
GOTMPDIR scratch. Reclaim confirmed private compiler output and scratch after
actual worker exit; retaining an empty cache root is acceptable. Preserve source,
data and evidence under their own retention contract. Routine authorized closeout
does not create another permission step. Size headroom for the selected workload;
no new global quota, automatic cleanup schedule, daemon or large receipt framework
is introduced.


The final maintenance receipt reports 14,030,917,632 available bytes. All five
caches read back at 12,288 allocated bytes each; shared build/module caches are
includedInCleanup=false, retained siblings and service identities are unchanged,
and the reserve remains 403,374 blocks. Its SHA-256 is
`6679ececdd43f0662327842257f4e60578ab7ded255dd53f0aa5b2d55429d336`.
The temporary-retirement receipt verifies all 12 exported members and removal of
the owned temporary copies, SHA-256
`58abc5081cad66e316b935f35f7da3c1a4e707acb287be082c2e81fe1bd754fa`.
Persistent evidence and retained source/data/services remain available.

## Evidence and publication scope

Local evidence is retained at
`D:/Code/goby/.artifacts/test-env-capacity-governance-20261005`:
`inventory.json`, `local-cache-provenance.json` and
`supplemental-local-cache-provenance.json`. Provenance is bounded static evidence;
it neither proves exclusive writers nor inspects current ownership itself.
Completed limited classification is recorded in `nested-cache-classification.json`
and `build-work-boundary-classification.json`; final availability is in
`final-capacity-metadata.txt`. Delivery is exactly four documents: root AGENTS.md,
policy, this report and the full historical handoff. No product
verification follows from this documentation-only change. Subsequent publication
receipt records exact main/origin readback and preservation of the original
199 dirty byte states plus 56 prior distinct WIP hashes.
