# Source18 original-client checkpoint

This is a partial M3e increment, not completion of M3 or full Emby compatibility.
All verification ran through `ssh test-env`; no local tests or builds ran.
The product source is frozen at
`/opt/goby-test/exec-work-m3e/source-attempt-18`, manifest SHA-256
`fa46e1547416afcb65c15f97aa5a1b7d3c344579386a424478ec310f0e01660b`.
Its executable SHA-256 is
`665df2d3851dc1b4a251012805678559e17e274c08f2548aead560e593a08d2b`.

## Implemented and verified scope

The increment addresses original Emby Web bootstrap, form/query authentication,
configuration and persisted preferences, observed catalog filters and local
endpoint projection, subtitle format negotiation, partial TV resume selection,
embedded music metadata and artist relationships, Universal audio capability
parsing, and owned correlated audio playback reports. The unchanged original
client assets come from the pinned Emby 4.9.5.0 installation. Reference and Goby
use separate state and services.

[Full source18 regression](m3e-source18-full.json) passed 1,741 top-level race
tests across 24 packages with zero failures or skips. The final binary matched
the isolated candidate. Disposable test pairs were removed while the existing
PostgreSQL catalog and HBA were preserved. The [working product comparison](m3e-source18-working-product-match.json)
matched all 3,180 product, test, module, deployment and web source files exactly
to the frozen source. Documentation and independently verified operator/browser
tools have separate provenance.

[Original-client audio evidence](verification-m3e-source18-audio.md) establishes
real MP3 and FLAC media decoding, advancement, pause, forward/backward seeks,
resume, stop, eight successful playback reports per format, persisted play count
and position, and UI logout followed by exact-token rejection. FLAC completed
Home. MP3 retains its original Home harness uniqueness failure; its saved Home
DOM is narrower evidence and the original run was not relabeled or replayed.
The source15-to16-to18 upgrade chain preserves the existing controlled Music
scan and state. Source17 was never installed.

Earlier [movie and preference](client-acceptance-m3e.md) and
[subtitle/TV](verification-m3e-source15-subtitle-tv.md) evidence remains scoped
to its recorded source and process. These historical browser runs were not
silently relabeled as source18 runs.

## Protected main-service preparation

The independent revision03 deployment operator passed 47 remote guards and its
helper build. The [fresh preparation](m3e-schema25-preparation-source18.json)
copied 444 private material files and made a 284,599-byte schema23 dump using the
same exported snapshot as the ordinary-role state inspection. A new, owned
rehearsal database restored that dump, migrated to schema25, preserved every old
business column and sequence, and was removed after identity and quiescence
checks. The original main state matched exactly after rehearsal.

The retained run is
`/opt/goby-test/backups/client-schema25-v1/run-20260911T062405Z-975ef4c2e0c2b3078d517dc5`.
Its prepared receipt SHA-256 is
`b1d686b1c8aed83aa545dd02618c22793cc8b371f6659a0adc1f40710ddfd0ce`.
The [two earlier failed preparations](m3e-schema25-preparation-failed-01-02.json)
remain intact. They stopped before helper execution, dump, rehearsal or main
migration and are not successful preparation evidence.

[Main deployment](m3e-source18-main-deployment.json) passed. A separate cache
creator passed seven memory guards and a real read-only inspection, then
created only the missing empty `/dev/shm/goby-transcodes-test` directory with
UID 995, GID 986 and mode 0700. Its intent and receipt remain outside the cache
and prepared-run inventory; existing cache paths cannot be adopted or repaired.

The main database migrated in one transaction from schema23 to25, preserving
all old business columns, identities and sequences across the 30-table result.
The installed source18 binary became ready as PID 539535/start ticks 3115871.
Health, readiness, the native admin index, login and seven authenticated reads
passed, including the existing backup list and status. Logout returned 204 and
the exact cookie was rejected with 401. Historical archives, paired secrets and
the M5j operational backup remained intact. No main restore or old-binary
rollback ran. The active isolated candidate remains a separate process,
PID 506532/start ticks 2681316.

## Remaining limits

Similar and ThemeMedia requests still return 404 in the audio flows; both runs
retain page errors. Broader playlists, collections, music and client variants,
event compatibility, additional media/track profiles, and actual GPU execution
remain outside this checkpoint. The [delivery plan](../planning/delivery-and-verification.md)
and [auxiliary-read plan](client-auxiliary-reads-plan.md) retain the next gates.
