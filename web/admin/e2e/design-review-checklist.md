# Administrator design review loop

The current reference is the final interactive **Goby Admin Prototype** in the
November 7, 2026 `Goby Admin UI UX Design 2` HTML handoff. Its selected direction
retains the 232 px sidebar and right-side editing sheets. The gallery and older
directions boards are supporting material, not separate product requirements.

The loop combines automated frontend checks with an explicit visual review. A
passing test run alone does not count as a clear visual review. Synthetic API
fixtures never establish backend, database, or media-processing acceptance.

## Run and record a round

Build the locally authorized production frontend and serve that exact build in a
separate terminal. The loop compares the HTTP entry with `dist/index.html` and
includes the compiled assets in its fingerprint, preventing stale development
module caches from being accepted as the reviewed version.

```powershell
npm run build
npx vite preview --host 127.0.0.1 --port 18096 --strictPort
```

Then run:

```powershell
node e2e/design-review-loop.mjs run --output C:\absolute\goby-design-review --round round-01 --base-url http://127.0.0.1:18096 --reference C:\absolute\goby-admin-design2.html
```

The runner captures every canonical route in English/light at 1440/375 px and
Chinese/dark at 1440/390 px. It also checks preferences, keyboard navigation,
editing sheets, the tablet breakpoint, reduced motion, shared settings drafts,
exact revision numbers, conflicts, failed writes, and stale telemetry. Screenshots
and geometry JSON files share the same names. The report retains individual test
results and raw evidence for failed checks.

Review the reference and the captured screens. Fill `review-template.json` with
the reviewer, screenshot filenames reviewed, actionable modification suggestions,
capability differences, and relevant notes. Recommendations are never inferred
from an automated pass or omitted merely because they repeat an unresolved issue.
Capability differences are recorded separately and require product decisions.

```powershell
node e2e/design-review-loop.mjs record --output C:\absolute\goby-design-review --round round-01 --review C:\absolute\goby-design-review\round-01\review-template.json
node e2e/design-review-loop.mjs status --output C:\absolute\goby-design-review --require-complete
```

After any change, capture a new round. Two consecutive checked and reviewed
rounds must have no modification recommendations and the same source and production
asset fingerprint. The runner rejects evidence whose source or assets changed
during or after capture, or whose served entry differs from the production build. Keep
previous evidence instead of overwriting or deleting failed rounds.

For a transcoding-only follow-up, use `--scope transcoding`. This selects the
runtime settings interaction suite, retains its PNG attachments, and keeps its
clearance history distinct from full administrator rounds. Supplement the
fixture checks with separately bound actual-service captures of every changed
transcoding group at desktop and phone sizes; inspect those captures before
recording clearance. The default `administrator` scope retains the full suite.

When `--reference` is supplied, the exact design document and its SHA-256 are
also bound to the round. Recording and status checks reuse that captured path;
editing the reference invalidates a clear result just as editing source does.
Review Design 2's immediate image transactions, multi-trigger task rules,
application backup versions, registered-directory observations, and removal of
administrator playlists and playback-preference controls. Preserve ordinary
account permissions and downstream player APIs. Unsupported prototype promises
remain explicit capability differences rather than simulated success.

## Visual and interaction review

- Confirm the dark and light canvas, panels, borders, ink, and amber accent match
  the reference. Amber carries primary actions, focus, selection, and active work.
- Confirm the 232 px sidebar, 56 px header, 32 px desktop gutters, 16 px phone
  gutters, 28/24 px serif headings, compact fields, pill buttons, and dense rows.
- Confirm the final 15 sidebar destinations. Images belong to library details;
  hardware belongs to transcoding; providers belong to metadata. Legacy links
  still resolve to their existing functions.
- Check the overview columns, metric units and empty playback state; library and
  metadata tabs; media-analysis outputs; access tables and sheets; task schedules;
  log filters; notification limits; backup safety flow; and settings save bar.
- Inspect phone, tablet, landscape, long English labels, long paths, scrolling,
  loading, empty, error, disabled, stale, and unconfirmed-outcome states.
- Check focus visibility, modal focus containment, focus restoration, Escape,
  command-search shortcuts, tabs, password-manager fields, and reduced motion.
- Confirm no placeholder buttons claim successful operations and no unsupported
  telemetry or playback data is invented. Record backend capability differences.
