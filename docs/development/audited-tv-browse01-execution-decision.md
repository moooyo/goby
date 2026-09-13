# One bounded TV browse execution

**Consumed.** TV browse01 completed the visible cross-season navigation, but
the browser failed a detail-field assertion and recorded one page error.
Its [owned state is closed](audited-core-tv-browse01-owned-state-closeout.json).
Follow the [evidence review](audited-tv-browse01-evidence-review.md); this frozen
decision cannot be reused for another run.

Execute one `tv-browse-01` scenario under the [revised plan](../planning/current-execution-plan.md).
The selected path uses `runTVBrowseUI`, its existing ordinary actor and the
version-3 null-baseline contract. It does not invoke the movie workflow.
Movie acceptance and its retained-baseline adjustment remain open.

## Scope and expected result

Use the unmodified hosted client to navigate from Home into M3e Client Television,
open M3e Client Series, select seasons 1 and 2, inspect the owned episode titles,
open Episode 2-1 with its visible season/episode identity, and return Home. Then
perform owned UI logout and prove rejection of the same token. Do not click a
playback, queue, mutation or administrative action.

The accepted visible layouts include a selected-season list or the already
observed cross-season series list. A cross-season result must still show the
requested season label and the exact complete owned episode set with matching
season/episode prefixes and card navigation. It does not prove strict season
filtering. The episode detail must be a unique visible non-card heading in its
detail context, not a card title left behind by an asynchronous transition.
The adapter and physical closeout also bind returned DTO identities to the seed.

PlaybackInfo preparation caused by normal detail navigation is allowed only
with complete request/response and exact owned state evidence. No media request,
Playing report or counted play is expected. Any new unstarted preparation must
be associated with the new revoked session; it cannot change movie06's foreign
history, user data or other source rows.

## Frozen input and reused verification

All relative remote paths below are under
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/`.

| Artifact | SHA-256 |
| --- | --- |
| `candidate-tv-browse01-entry-review-01/input.json` | `160b30e5968dd61eaeaca0fda6dee0b6b8b08f84df0f7f9cffd44c11dd044902` |
| `candidate-tv-browse01-entry-review-01/review.json` | `fa6a9fea2cb556faaf2486a1534dfb17b99d90fe6e74c12ada686532f36f31e7` |
| `client-v3-controller-verification-01/run-audited-candidate-client.py` | `8e32fe7b73457059c978b33eff91b5b790a6682061b7780ab6863a4977d24545` |
| `client-v3-controller-verification-01/verification.json` | `8f5bcbc15b04be86943b0722c34a029ed748d75909e61689e39329c46d2627b5` |
| `client-v3-component-verification-01/verification.json` | `994c013949005f3de6171588f1bf234ac13c4026f0cafdd3bf562871a34a0366` |
| `client-v3-component-verification-01/client-browser-tv-flow.mjs` | `5acb53c7fd5852f501e6590d09974913e888b12bd64ac8aef233953dc7cdfb65` |
| `candidate-core-movie06-owned-state-closeout.json` | `2634f867924b4d28078669ba87069265560b1e8b0b0ba8c67ac5514a3739139f` |

The complete source set remains the verified v3 bundle: adapter `3ecbffb8...`,
closer `dedb3b54...`, gateway `b343f522...` and all input-pinned dependencies.
Reuse its 71 component/controller checks, 48 unchanged adapter checks and the
original TV helper evidence. The selected movie helper remains the frozen
`a3ad0b64...` file and is not called by TV browse; do not substitute the later
movie-only readiness change into this input. No product, dependency or tool
change is needed for this scenario, so no full-suite rerun is required.

The same TV helper previously completed its visible navigation in
`/opt/goby-test/exec-work-m3e/goby-av-source15-tv-01/observation.json`, SHA-256
`8b36fb7cb494e7a0c07a2512e41dc0f8a93262735a34083ca09c51929b937338`.
That observation supports reuse of the card, lazy season selector, cross-season
list, detail and Home controls. It also contains five auxiliary HTTP failures
and five page errors, so it is not passing evidence for the current strict
candidate gate. There is no dedicated pure test of `runTVBrowseUI`; the 48
adapter checks cover its scope/identity/cleanup guards, not its full DOM journey.
The new run's bounded redacted error collector can retain actionable details
if those events recur. Unknown page errors still fail acceptance, and timing
near an external request is not sufficient attribution. Do not present movie's
19 readiness tests as TV coverage.

The fresh entry capture at `2026-09-13T13:49:39.493111Z` matched all 35 tables
and sequences to the closed movie06 state. It confirmed the TV actor has no
play, reference or userdata history, all fourteen sessions are revoked, all
six foreign play rows and movie userdata are exact, and there are no references
or encoding jobs. Candidate PID423828, PostgreSQL PID363520, deployment lease
and original-client hosting were continuous. The new output and both new worker
units were absent. The controller repeats its live checks before execution.

## Limits, cleanup and stop conditions

Keep the existing 1,200-second outer budget with 240 seconds reserved for
cleanup; browser 600/120 seconds; gateway 1,000 requests with 64 reserved for
cleanup. Only the new `goby-core-client-tv-browse-01-{gateway,browser}.service`
units and `candidate-core-client-tv-browse-01` output belong to this run.
Capture candidate stdout and complete source state before and after, preserve
physical evidence, close both workers and run the existing offline closeout.
Do not reset actors, rescan, restart candidate/hosting/main, widen the external
network boundary or reuse a consumed output.

A pass closes TV browse only and must identify its observed list mode. Playback,
movie errors, other core scenarios and main promotion remain separate gates.
An actual failure retains its original outcome and requires owned-state closure
before further state-changing work. If shared authentication, observation or
cleanup tooling blocks the run, pause affected paths and review saved evidence;
do not switch scenarios as an automatic retry. This decision permits one run,
with no automatic business retry or repeated helper revision loop.
