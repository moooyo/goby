# Intro quality: local correspondence and complete-opening limits

Status: the selected investigation is complete; the candidate is not adopted.
The [result manifest](intro-quality-round5-results-20260930.json) binds the
observations, retained failures and resource closure.

Baseline: `4817cb3`. The 96-by-96 experiments do not currently justify a
production recognition change. Product detector v5, extraction, schema 53,
GAFB v3, published results and the accepted Docker catalog remain unchanged.
This is known-source development research; no new independent accuracy
evaluation or recovered positive is claimed.

## Audited higher-resolution observations

Twelve retained sources were decoded on `test-env`: Beverly B1/B2/B7, NASA
N388/N389/N390, One Step Beyond O1/O2/O3 and Hubble H114/H116/H118. Each produced
1,200 full-frame 96-by-96 grayscale observations at nominal 100 ms cadence
within the first 120 seconds. Complete native source PTS, packet membership,
selected output PTS, source/tool identity, clean process exit, byte counts and
per-frame hashes were audited before a manifest became consumable.

The raw corpus is 132,710,400 bytes; compressed raster payloads total
89,656,430 bytes. Original media was reused, not downloaded again. These are
private research observations, not a new production extraction profile. The
Hubble cases specifically retain episode-title protection concerns.

## Fixed local-patch experiment

The frozen representation uses 25 canonical regions with deterministic binary
intensity comparisons, reliable-bit masks and local contrast. One geometry
remains fixed for each source across its prefix. A complete window uses one
fixed, spatially distributed patch set, including central regions; a failing
patch cannot be replaced independently at each frame.

Coverage is calculated after all clique edges agree at the same anchor sample.
Source-frame ownership is fixed before cropping. Motion uses the reliable-bit
intersection across both times and all three sources, with same-direction
changes. Pairwise evidence from different bits or different times cannot be
borrowed to manufacture a complete group. The minimum duration, coverage,
dynamic, state and finite-period rules were fixed before full-prefix execution.

The first full-prefix development attempt produced no groups. Beverly stopped
at a coarse nomination rule requiring four regions to move simultaneously.
A constructed counterexample showed that this seed is stronger than the final
rule, which permits the fixed dynamic regions to have events at different times.
One isolated development revision removed only that nomination condition;
final acceptance rules, geometry ranking and resource caps remained unchanged.

| Cohort | Furthest stage after the nomination revision | Complete groups |
| --- | --- | ---: |
| Beverly B1/B2/B7 | 44 by 33 clock combinations; none supplied the required simultaneous three-edge region coverage | 0 |
| NASA N388/N389/N390 | One target had no qualifying clock; the other had six | 0 |
| One Step Beyond O1/O2/O3 | Neither target supplied a qualifying clock | 0 |
| Hubble H114/H116/H118 | Three clocks for one target and none for the other | 0 |

The revised runs completed without resource failure. The maximum was
168,979,477 patch comparisons, 36,309,600 clock lookups and 77 retained
hypotheses; peak RSS was 112,876 KiB. These numbers describe three-source
research runs, not production capacity for 32 episodes.

Both original and revised source/binary freezes, inputs, outputs and resource
receipts remain retained. An initial launch failed before matcher execution
because `/usr/bin/time` was unavailable; the unchanged binary then ran under a
Python `os.wait4` wrapper. The nomination repeat is development evidence.

The search remains finite and heuristic: it chooses one geometry from coarse
nominations, retains maximal fixed patch sets rather than every subset, uses
a greedy state witness and checks a finite anchor-period grid. Empty output
does not prove that all possible local representations or alignments must fail.

## Point correspondences and foreground evidence

A separate fixed ORB probe used the same audited 96-by-96 frames: three
previously recorded Beverly action points and fixed 5/11/17-second NASA/OSB
points. It used mutual ratio Hamming matching and one affine estimate per pair,
with all parameters frozen before its single 27-pair execution. The API and
software identities were checked against the official
[ORB](https://docs.opencv.org/4.13.0/db/d95/classcv_1_1ORB.html),
[BFMatcher](https://docs.opencv.org/4.13.0/d3/da1/classcv_1_1BFMatcher.html) and
[affine-estimation](https://docs.opencv.org/4.13.0/d9/d0c/group__calib3d.html)
documentation.

Beverly's three action points had 3/5/1 affine-inlier keypoint cycles. The
controls also had cycles: NASA 14/2/0 and OSB 2/1/1. Such point cycles are not
interval or motion evidence. At the first two Beverly actions, both pairs
involving B1 had zero inliers in the central 50-percent region. Source-image
review of the preselected hat-down diagram places its accepted matches around
the upper/peripheral scenery and furniture; it does not establish common
foreground action beneath the different credit overlays.

This probe completed in 0.832 seconds with exit code zero. The private
dependency download timed out, after which the exact same pinned OpenCV wheel
was transported through a local download and verified remotely against its
original official SHA. No library was installed, imported or executed locally.
OpenCV 4.13.0.92 and NumPy 2.5.3 were confined to an owned remote temporary
directory, then removed after evidence archival. There is no production
dependency change. The unavailable web gateway was replaced by remote reading
of official documentation; no source frames were sent to those services.

## Shared interiors and full opening boundaries

Source-only inspection confirms head/tail editing differences independently of
the experiments. The evidence-supported three-way frontal-action envelope is
approximately B1 8-22 seconds, B2 8-21.3 seconds and B7 2-3.5 through 15-16
seconds. These are approximate action correspondences, not exact labels or a
rigorous recall ceiling. B1 has an additional street lead-in; B1 and B7 continue
with roughly eight seconds of action beyond where B2 ends.

Correctly recognizing an internal common segment could therefore still miss
the frozen complete-opening endpoint targets. The original positive labels
remain unchanged. Pairwise support has a different scope: B2 can support B7's
head and B1 can support its tail. That does not supply the existing policy's
one complete three-source witness for the entire opening, and it must not be
silently substituted for one.

## Adoption boundary

The private pipeline must first complete a known moving positive within its
original work budget and demonstrate valid boundaries. Primitive checks alone
do not establish that end-to-end capability. The initial complete synthetic
positive reached the correct clocks and joint coverage but exhausted its
200-million comparison budget during repeated overlapping-window audits;
the failure and both completed zero-output controls are retained.

An exact, bounded posterior cache repaired that resource defect without
changing the descriptor, nomination, clock search, state/period decisions,
window ordering or final thresholds. Its one-MiB table is scoped to one fixed
view/clock combination; collisions or capacity exhaustion use the original
calculation. Atomic partial masks preserve unknown evidence, and interrupted
computations do not commit partial cache entries. Twenty remote primitive and
cache-oracle tests passed, including exact cached/uncached state/period
equivalence, different support masks and cancellation. Four real-cohort
decisions and work counts remain equal to the preceding development attempt.

The unchanged synthetic moving fixture now completes in its original budget:
166,570,443 patch comparisons and 39,137,331 total lookups. The cache avoids
49,799,700 repeated patch comparisons; its 2,001,672 queries are charged to the
lookup budget. The static and different-content controls still return no group.

The strict safe-positive check nevertheless fails. The returned anchor
interval is `[18.7, 28.1]`, while the known shared interval is `[20, 28]`;
the other two sources have the same 1.3-second leading and 0.1-second trailing
overreach under their exact seven- and fourteen-second offsets. All 25 support
and dynamic patches pass, with 851-per-mille coverage. The unchanged
duration-first window selector has used aggregate tolerance to include
unmatched endpoints. Finding a common scene is therefore not sufficient to
authorize the returned skip interval. This failure was not hidden with a new
guard, selection rule or changed fixture.

| Check | Outcome |
| --- | --- |
| Audited high-resolution extraction | All 12 existing sources complete; 1,200 frames each |
| Fixed-patch real development runs | No complete groups across the four cohorts; original and revised attempts retained |
| Primitive/cache oracle checks | 20 passes; no production-package or new accuracy claim |
| Complete moving synthetic pipeline | Resource failure repaired; one known-clock group found; strict boundary containment failed |
| Static and different-content synthetic controls | Complete runs, zero groups |
| Frozen ORB point probe | 27 pairs completed; controls also contain local correspondences |

No production raster, schema or history change is appropriate on the current
recognition evidence. Raw 96-by-96 frames plus timestamps would consume
11,068,800 bytes per source, before existing features. A 32-source cohort would
carry roughly 338 MiB of that input alone. Adoption would need bounded codec
expansion, a new evidence policy, history authority and measured worker limits;
neutral patch descriptors cannot reconstruct later geometry searches.

Further work should distinguish three questions: whether a common scene can
be found, whether enough independent originals support its full extent, and
whether its boundaries authorize skipping. Increasing image resolution or
counting more local matches does not by itself answer the latter two.
Before another recognition claim, establish a boundary selector that passes
contained moving positives, then ensure the corpus has enough independent
same-variant support for the full target. Keep unsupported complete-opening
targets visible as misses rather than relabeling them as negatives.

All execution and verification in this increment ran on `test-env`. No new
database was started. Owned extraction containers and temporary OpenCV
dependencies are absent, and source media, compressed observations, original
failures and all private source snapshots remain available. One redundant
source-transport archive was reclaimed only after a remote comparison with
its local recovery copy. Shared caches were not deleted. No merge, push,
production dependency, image release or deployment is included.
