# Automatic episode intro workflow

Status: implementation in progress. Baseline: `40e02bb`.

The user selected one normal workflow: enable intro detection in a TV library,
analyze eligible episodes in the background, and expose qualified results to
playback. Progress and failures remain visible. Human candidate approval,
rejection and manual correction are not part of the product workflow. An
uncertain or absent match produces no automatic intro marker.

## Finite scope

- Add the TV-library `EnableIntroDetection` option, defaulting to false. Existing
  library rows retain their settings. Expose the option in library creation and
  editing, with a supported Emby library-option mapping.
- Enabling detection requests background work. Completed independent and
  task-owned scans request another pass through a dedicated durable event.
  Install a default daily task and event trigger without overwriting existing
  administrator schedule choices. Automatic runs select only enabled TV libraries.
- Publish qualified results automatically. Disabling detection withdraws the
  library's effective detected markers; existing explicit chapters and historical
  manual/imported markers remain compatible. Keep old API and audit data, but
  remove human correction controls from the administration workflow.
- Retain the existing matcher, thresholds and first-600-second extraction policy.
  This increment does not change the three-independent-episode support threshold
  or claim broader accuracy across new series.
- Retain background progress, stop and failure reporting. Keep preview generation
  available without coupling it to the TV intro option.

## Verification and delivery

Run focused migrations, library-policy, scan-completion, scheduling, publication,
backup/recovery and HTTP tests on `test-env`. Verify that disabled libraries and
empty enabled selections are not analyzed, overlapping scan events are not lost,
and restart does not consume an unprocessed request.

Build and check the changed administrator UI remotely. Verify the library switch,
automatic status and removal of human-decision controls. Use three previously
accepted positive episodes and two insufficient-evidence sources for the actual
Docker workflow. Do not issue an analysis-start or candidate-decision request to
make the automatic journey pass. Reuse prior accuracy evidence without calling
these sources a new holdout population.

Deliver updated software/AMD Docker application layers and the matching current
release catalog after the selected checks pass. Preserve original failed attempts,
close only owned resources, retain private state/evidence, and keep Git publication
separate from application-image and runtime receipts. Do not resume online scraping
or a historical capacity/fault campaign.
