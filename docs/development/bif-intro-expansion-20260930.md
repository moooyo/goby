# Automatic BIF delivery and expanded intro evaluation - September 30, 2026

Status: **BIF verified; expanded intro assessment completed, broader recognition NOT accepted**.

The automatic seek-preview workflow has passed its selected backend, Node,
administrator UI and actual Docker checks. The intro candidate remains
`introdetect-v3` with its existing quality and independent-support requirements.
It returned `no_result` for all 15 evaluable original episodes. All 12
source-reviewed openings were missed; the three short-ident negatives correctly
received no intro. No qualified interval was emitted. The finite assessment is
complete, but it does not establish broader recognition or improved accuracy.
The requested short-opening and variant-opening recognition remains work to
improve. Both final Docker profiles passed actual UI and retained BIF checks,
and final owned-resource closure passed. Archive/export and Git publication are
recorded separately from this verified application scope.

Final application source: `33445db2e2e64b6871116332c44605261a1bf2d4`, including
dashboard `main` snapshot `4c4ed60`. The earlier
`0014befd68420e593f0855cdd1c0991f3079abfc` application and images are retained
historical verification artifacts and have been superseded.
Database schema: **52**. Baseline: `869c665`.

See the [finite execution plan](../planning/bif-and-intro-expansion-plan-20260930.md),
[API contract](../api/media-analysis.md),
[native workflow](media-analysis-native-ui.md) and
[runtime contract](media-analysis-runtime.md),
[delivery result manifest](bif-intro-expansion-results-20260930.json) and
[intro assessment manifest](intro-accuracy-assessment-20260930.json). The
[previous automatic-intro increment](library-intro-automation-20260930.md)
retains its own source, delivery and verification boundary.

## Final application and image identities

| Artifact | Identity | Current verification state |
| --- | --- | --- |
| Application source | `33445db2e2e64b6871116332c44605261a1bf2d4` | Dashboard integration included |
| Application binary | 57,194,340 bytes; SHA-256 `8b92bc462004fe1dc98609092209d6d57882c53278411fd24468fba51a9f117d` | Final application build |
| Software image | `sha256:45dc7d9ff3eefbe79f6c8205ce2fda332777d1c1f2ecc22ef491fe7ab51bf89d` | Actual final UI and retained BIF verification passed |
| AMD image | `sha256:d25873c96b1259da69c82b38c4939f7d37a3986b4238850be7e4b8def8707ce0` | Actual final UI and retained BIF verification passed |

Final archive/export identities and completed import checks are recorded in the
delivery result manifest. Workstation copies also passed a byte-for-byte hash
comparison on `test-env`. Delivery uses
`D:/Code/goby/.artifacts/bif-intro-20260930`, with `software` and `amd` profile
directories and their matching operations toolkit. This record does not declare
Git merge or push completion.

The earlier `0014befd68420e593f0855cdd1c0991f3079abfc` application was
52,410,067 bytes, SHA-256
`6fa0bc8bd98e2b6ac0ce69f638428068e923fd2d8e7e37f61c292a3f09e5bec5`.
Its retained image/archive receipts are historical:

| Superseded profile | Image ID | Archive bytes and SHA-256 |
| --- | --- | --- |
| Software | `sha256:743cb1a66e41c2343a0e0659fe8ed68d5225c18a3c9c43f3452c0f41a8420b1d` | 492,652,032 bytes; `fe563c9d798fe8c5d4a5ff8f5e0bd5ca366355ef555e50472c47f9b181ab21eb` |
| AMD | `sha256:ba4a9bbdcfe10f693f092a9268a00494bbadc6e38b4003f8a9751dddb3dfae9e` | 1,045,122,560 bytes; `c779923d5ab44ad98cce60871a516987abab7456a1713370a8c7c4ee66e0a132` |

Both historical archives passed actual export/import. The historical AMD image
also passed startup, complete retained BIF hash/timeline checks and actual UI
login/library-switch checks with no manual-generation entry. That application
check used software axes and did not repeat GPU qualification. Its owned
containers/networks were removed, the private PostgreSQL instance on port 55997
stopped normally after zero client sessions, and HTTP port 38964 closed. Three
protected resource identities, PIDs and process start times were unchanged.
These historical receipts are retained. Final verification after dashboard
integration separately passed on both profiles, including exact retained BIF
bytes and the actual administrator UI. Final closure records zero owned
containers, zero Compose networks, zero database clients, a stopped private
PostgreSQL instance and unchanged protected services. Source material, private
database state and original evidence remain retained.

## Automatic BIF behavior

Library creation and editing provide two independent options:

| Option | Supported libraries | Effect of disabling |
| --- | --- | --- |
| `EnableIntroDetection` / Automatic intro detection | TV libraries | Withdraw detected automatic markers; retain explicit source-valid Manual/Import/chapter behavior |
| `EnablePreviewGeneration` / Automatic seek previews | Movie, TV and mixed video libraries | Prevent new automatic admission/publication; retain existing valid BIF and thumbnail references |

Both options default to false. Schema 52 preserves older library rows and their
revisions; a missing preview option means disabled. It adds the independent
`PreviewGenerationRequested` event while retaining prior migration history.

Creating an enabled library, enabling its option, or completing an independent
or task-owned scan requests work through the appropriate durable event. Scanning,
item listing, playback negotiation and preview GETs do not perform generation.
Each untouched task definition receives a 24-hour interval and its dedicated
event trigger. Existing custom, cleared and disabled task choices are preserved.
Automatic admission selects only libraries enabled for the requested kind, and
an empty eligible set cannot expand to all libraries.

A real analysis-profile change withdraws obsolete derived references and commits
new requests for enabled analysis kinds. Preview interval or quality changes thus
request a new BIF build without a manual start. No-op updates preserve references
and do not request another run. Overlapping events wait for fresh admission rather
than letting an earlier source snapshot consume newly committed scan work.

Graceful shutdown and crash recovery of unfinished automatic analysis persist a
replacement request for the corresponding kind. Old executions remain interrupted;
the new run captures current library policy and profile. Explicit stops,
maximum-runtime stops and manual runs do not create automatic replay requests.

The preview publication path rechecks the library policy before commit. Turning
off the option leaves existing previews usable, including across container
recreation. Normal source/profile invalidation, explicit clear and cache eviction
still apply. All three widths publish as one source-bound generation. Supported
widths remain 240, 320 and 400 pixels; the BIF format, timeline representation and
sealed-cache verification are unchanged.

The normal administrator page now points to library options and Tasks rather than
offering manual preview-start and Force controls. Progress, failure details,
configuration, cache controls and previous unconfirmed-request recovery remain.
Compatibility start APIs remain available with their existing explicit scope.

## Completed BIF verification

All execution and validation in this increment use `test-env`.

| Scope | Recorded result | Boundary |
| --- | --- | --- |
| Initial focused backend campaign | 168 parent tests passed | Selected automation, admission, publication, migration and related regression scope |
| Dashboard-integration backend follow-up | 7 passed: 6 new unique parent tests and 1 repeated parent | Composed total: **174 unique parent passes**; repetitions and parent/subtest events are not added together |
| Initial Node/UI campaign | 13 Node checks and 33 UI checks passed; one historical UI skip | Original changed library/analysis scope |
| Dashboard-integration Node/build campaign | 13 Node checks and administrator build passed | Repeated checks retain their own source boundary |
| Dashboard-integration browser campaign | 37 passed, 2 skipped | Historical full-Phase-3 case and a separate real dashboard-delivery fixture not configured for this campaign; neither skip is a pass |
| Actual Docker preview journey | All four phases passed | Real Goby process, library settings, automatic tasks and public preview HTTP; no manual analysis starts or Force requests |
| Final Docker software profile | Actual UI and retained BIF verification passed | Bound to final image above |
| Final Docker AMD profile | Actual UI and retained BIF verification passed | Separate actual check after dashboard integration; no new GPU qualification |
| Final owned-resource closure | Passed | Zero owned containers/Compose networks/database clients; private PostgreSQL stopped; protected services unchanged |

The [preview automation driver](../../scripts/test-env/preview-automation.py)
keeps a retained checkpoint so an affected phase can be continued without
recreating unrelated successful work. The actual Docker phases were:

| Phase | Accepted observations |
| --- | --- |
| Enable | The target library automatically produces all three BIF widths. The disabled control remains without previews. BIF indexes cover the complete expected timeline; JPEG structure/dimensions are checked throughout, with first/middle/last samples independently decoded. HTTP ThumbnailSet positions and image tags agree with the generation; byte-range delivery matches the full BIF. |
| Rescan | An ordinary scan requests automatic work. Current cache reuse retains the BIF bytes, timeline, tags and recorded generation facts. |
| Configuration | Changing the interval from 10 to 20 seconds requests rebuilding. All widths receive new bytes/timelines/tags; obsolete thumbnail tags return 404. The new generation passes the same BIF and sampled JPEG checks. |
| Disable and recreation | Disabling the option preserves valid preview bytes and thumbnail timelines. An ordinary scan creates no new automatic preview run. Container recreation preserves the disabled option and the retained generation. |

These checks establish the selected automatic BIF workflow. They do not measure
intro accuracy, claim a new GPU qualification, or substitute for final image and
archive delivery receipts.

## Expanded intro source population

The reviewed inventory contains **16 original episodes from five series in 17
media files**. The evaluable population is **15 original episodes**: 12 with
source-reviewed openings and three short-ident negatives. Twelve episodes from
four series were initially selected before detector execution; three Robin Hood
episodes were explicitly added for supplemental source review, and NASA N390
replaced the non-evaluable N386 in the NASA matching group. This is a documented
selection and replacement history, not random sampling.

| Role | Series | Original sources |
| --- | --- | --- |
| Initial calibration | The Beverly Hillbillies | B1, B2, B7 |
| Initial calibration | Hubblecast | H114, H116, H118 |
| Initial held-out series | One Step Beyond | O1, O2, O3 |
| Initial held-out series | NASA Space to Ground | Initially N386, N388, N389; evaluable group N388, N389, N390 |
| Explicit supplemental source review | Robin Hood | Three additional original episodes, separately recorded in the private source inventory |

N386's original selected medium rendition and a separately downloaded original
rendition both lack audio. The initial NASA extraction attempt terminated with a
panic; that failed attempt and both file identities are retained. These are two
media files for one non-evaluable original, not two samples and not a successful
negative. The next-week official N390 source and its source-only labels were
frozen before the first NASA matcher run. N386 remains in the reviewed inventory
and exclusion accounting. No failed source or experiment is silently removed,
and successful cases were not selected to improve the reported result.

Source URLs, upstream metadata, license statements, digests and sizes are retained
privately. Internet Archive public-domain metadata is an uploader declaration,
not a conclusion about worldwide rights. Official ESA/NASA reuse conditions and
credits remain associated with their sources. Original source media is not
redistributed in Goby images or the public evidence package.

All evaluated-source labels precede their matching runs. Review uses source-only
actual-PTS frames and explicit boundary uncertainty. Assistant visual review is
not called human annotation or
direct audio listening; any machine-ASR evidence must retain its separate status.
Unknown seasons remain unknown. Alternate encodes and repetitions do not count
as independent episodes, and old The Big Picture material supplies no support or
new-series denominator for these groups.

Hubblecast source review identified episode-specific cold opens followed by a
short common program bumper and a separate episode title card. It also identified
shared end-credit/promotion footage as protected non-opening content. The three
supplemental Robin Hood originals also have short openings; their labels are not
stretched into longer positives to satisfy a detector threshold. NASA N388,
N389 and N390 are labelled as short-ident negatives for this opening assessment.

## Completed expanded intro assessment

| Series and role | Evaluable originals | Source-review category | Production v3 outcome |
| --- | --- | --- | --- |
| The Beverly Hillbillies, initial calibration | 3 | Opening present | 3 `no_result`; 3 missed openings |
| Hubblecast, initial calibration | 3 | Short opening/title block present | 3 `no_result`; 3 missed openings |
| Robin Hood, explicit supplemental group | 3 | Short opening present | 3 `no_result`; 3 missed openings |
| One Step Beyond, held-out series | 3 | Opening present | 3 `no_result`; 3 missed openings |
| NASA Space to Ground, held-out series | 3 | Short ident; negative opening cases | 3 `no_result`; 3 correct negatives |

The result is **12 missed source-reviewed openings out of 12**, **3 correct
negatives out of 3**, **zero qualified outputs** and **zero false positives**.
Precision and boundary error are **undefined**, because there are no emitted
intervals to score. They must not be reported as zero-error or perfect-precision
metrics. No emitted interval overlaps protected content; that absence does not
compensate for the missed openings.

This source-review-based result is limited to the stated non-random population
and review method. It does not estimate population-wide accuracy or convert
assistant visual review into human ground truth. The held-out series remained
separate from calibration, and old The Big Picture results do not supply
independent support or enter the new-series denominator.

Calibration observations included short segments and insufficient common audio
evidence. B1 did not have enough shared audio with either of the other two
Beverly Hillbillies sources. One B2/B7 pair diagnostic still had weak visual
evidence and extended its tail into protected narrative. These observations did
not establish a confirmed product implementation defect or justify publishing
the diagnostic interval.

The reduced-duration calibration variant (`duration10/Auto15`) did not improve
the result and was not adopted. A proposed `MinSupport=2` variant was rejected
by validation before execution; it is not an executed experiment or additional
episode result. The evaluated production candidate
`0014befd68420e593f0855cdd1c0991f3079abfc` retains `introdetect-v3` and all
existing quality gates. The final dashboard-integrated application
`33445db2e2e64b6871116332c44605261a1bf2d4` does not change that detector or its
quality gates. Short and variant
openings remain a demonstrated limitation of this expanded sample, rather than
newly accepted recognition coverage.

## Artifact and Git handoff

Final profile runtime checks, archive import checks and owned-resource closure
have passed. The delivery manifest binds the final archives and the schema-52
recovery catalog. The final archive identities are:

| Profile | Archive bytes | Archive SHA-256 |
| --- | ---: | --- |
| Software | 499,547,136 | `6739a08edb2f233ebf3b82ed86066fc086eb22bdcbdea1f4131b3d1e665aa350` |
| AMD | 1,052,017,664 | `9a1e02339519da2ee0713dc54d741c7e0fe3d865c2017fd4af011811e8406363` |

The workstation delivery is `D:/Code/goby/.artifacts/bif-intro-20260930`;
superseded artifacts are retained in its `pre-dashboard` directory. Git
publication is recorded separately from immutable application identities.

BIF verification and the finite expanded intro assessment are complete at this
checkpoint. Broader intro recognition is not accepted, no accuracy improvement
is claimed, and short/variant-opening recognition remains to be improved.
