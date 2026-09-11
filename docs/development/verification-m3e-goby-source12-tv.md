# M3e Goby source12 ordinary TV browse checkpoint

Status: **verified for the owned ordinary TV fixture**. The
[safe summary](m3e-goby-source12-tv-summary.json) records the exact source12
candidate, browser and driver inputs, UI observations, state comparisons, and
logout proof. All execution used `ssh test-env`.

The candidate was PID 403472, start ticks 1597941, schema 24, executable SHA-256
`eb9ac18e7adc7d9738c5bb9e37f01269b121027527cbe1a082efc082cf61cf81`.
The original Emby Web 4.9.5.0 UI ran in Chromium 153.0.8010.12 with Playwright
1.63.0, using the independently receipted ordinary AV account.

`goby-av-source12-tv-01` completed actual library, Series, season 1, season 2,
episode detail, Home, and Sign Out navigation. The visible season labels were
`Season 01` and `Season 02`; their matching episode titles were exactly the
fixture's two first-season episodes and one second-season episode. The detail
heading independently identified `S2:E1 - Episode 2-1`.

The original Series view retains all three episode cards while the selected
season label changes, as observed on the reference. The report preserves that
cross-season layout and does not claim that the selector hides other seasons.
The spurious duplicate Specials entries observed on source11 were absent from
this ordinary fixture journey.

The complete browser request set had no playback report or media stream. One
PlaybackInfo request was the original client's episode-detail metadata request.
All nine observed item-detail UserData values and Configuration, Policy, and
UserSettings hashes matched before and after. UI logout returned 204, the login
view appeared, and the independent exact-token check returned 401. All five
instance checks passed through final browser shutdown.

This fixture contains no positive special-season, special-episode, or standalone
special examples. The observed `IsSpecialSeason=false`, `IsSpecialEpisode=true`,
and `IsFolder=false` requests explain this normal-data regression check.
`IsStandaloneSpecial=false` was also sent by the real client; its semantics
remain unverified and are not included in this acceptance claim. The retained
auxiliary HTTP failures and page errors likewise remain outside the passed
browse scope.

The original report is
`/opt/goby-test/exec-work-m3e/goby-av-source12-tv-01/observation.json`.
Its eight source inputs were individually hash-checked and retained in
`/opt/goby-test/exec-work-m3e/verified-source12-tv-source/`. Historical reports
and paths must not be reused for another candidate or a broader special-content
claim.
