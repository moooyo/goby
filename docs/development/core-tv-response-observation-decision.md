# Bounded TV response observation decision

Status: selected next diagnostic question on 2026-09-15; **no executable input
or browser run is admitted yet**. This follows the
[core acceptance resolution](core-client-acceptance-resolution.md) and retains
all original failures, the selected E11 delivery target and the main-promotion
gate.

## Question and existing evidence

Use one ordinary, unmodified-client TV browse to ask whether a native rejection
contains a Response that can be uniquely bound to an actual Goby exchange.
The immediate suspected route is `/emby/LiveTv/Programs`: earlier TV/Episode
records contain its complete 404 near a Response page error, but lack direct
rejection-to-response identity. Timing alone does not establish causality.
See [TV evidence](audited-tv-browse01-evidence-review.md) and
[Episode01 review](audited-episode-client01-review.md).

Do not repeat the old reference movie/subtitle workflow merely to reproduce
undefined errors. [Reference movie history](client-acceptance-m3e.md) already
contains four page errors, and [reference subtitles](verification-m3e-reference-av.md)
contains two undefined errors. Neither retained their native rejection identity.
The historical reference runner used browser routing plus a deny-only proxy;
the audited runner uses its physical gateway. Those are not identical rejection
boundaries. A matching error count or clean new run cannot explain an old error.

## One prospective observation

Reuse `runTVBrowseUI` and the existing, verified `attachCandidateNativeRejections`
hook. No new diagnostic layer, vendor-source/database access, client source
modification or egress change is needed. Navigate TV library, series, both
seasons and Episode 2-1 detail, return Home, then perform owned logout and the
same-token rejection check. Do not click Play, queue or administrator actions.

This observation differs from old TV/Episode attempts because those attempts
did not have the native rejection hook. The new observable is the Response's
unique request ID, URL and status, not another list of nearby timestamps.

| Result | Next action |
| --- | --- |
| A native Response uniquely identifies the actual LiveTv Programs exchange | Review that exact public response contract and support boundary; make a product change only if a concrete contract defect is established. Do not infer that full Live TV implementation is required. |
| A native Response uniquely identifies another Goby exchange | Review only that endpoint's actual request/response and relevant source contract. |
| Primitive undefined only, missing/nonunique identity, or no recurrence | Record that this branch did not obtain discriminating evidence and stop it. No automatic reference or video replay and no waiver of old errors. |
| Input, ownership, budget or cleanup failure | Preserve the first failure, close owned state and stop further business work pending resolution. |

A Response binding identifies the response carried by one native rejection. It
does not automatically identify a separate Playwright pageerror event or prove
the error harmless. Physical293 and the two newly excluded Movie05 associations
remain separate media-cancellation gaps.

## Required implementation and entry conditions

1. Add only the retained-state contract needed by the already-consumed TV actor
   to the existing controller and closer. Version 4's movie contract cannot be
   relabeled as TV. Bind the latest complete closed snapshot and the TV actor's
   independently preserved provenance. Preserve every foreign row, sequence and
   both audio references. Detail-generated PlaybackInfo may create or expire
   only explicitly authorized uncounted Prepared state; no media or Playing
   report, counted play or UserData mutation is admitted.
2. Verify that narrow contract remotely using actual retained state and relevant
   negative cases. Complete new component admission for the exact controller,
   closer and reused native hook before any browser worker. Keep old frozen
   inputs and verification receipts unchanged.
3. Bind the actual current candidate and hosting processes, source, client build,
   Chromium/Playwright, current protection/capacity and new output/worker paths.
   Historical PIDs and admission receipts are not fresh entry observations.
   Preserve the original TV limits: outer 1200 seconds with 240 reserved for
   cleanup, browser 600/120 seconds, gateway 1000 requests with 64 reserved for
   cleanup. No restart, reseed or fresh actor is a shortcut around retained state.
4. Freeze one input with the question and result alternatives above. Execute
   serially with other shared-fixture writes, close owned workers/credentials and
   state, and independently review the saved result. This document alone is not
   an execution input.

The diagnostic can use the retained audited candidate to explain its own
history, provided current admission is established. Final E11 client acceptance
still requires its separately reviewed transition and matching evidence; this
diagnostic does not transfer an old candidate's acceptance to that artifact.
