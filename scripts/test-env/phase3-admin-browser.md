# Phase 3 administrator browser acceptance

Status: source added; coordinated remote verification is pending. This guide
does not claim that the journey has passed.

The private `TestPhase3AdminBrowserIntegration` integration fixture and
`web/admin/e2e/phase3-admin-workflows.spec.ts` exercise the embedded administrator
application against real native HTTP handlers and a disposable PostgreSQL
database. The browser does not mock application responses. The fixture owns the
media files, users, independent concurrent administrator, restart, and cleanup.
Run this only on the designated `test-env` environment under the root task's
exclusive verification slot. No dependency additions are required.

`web/admin/e2e/phase3-permissions.spec.ts` is a separate, explicitly synthetic UI
regression suite. It mocks every native endpoint and proves that directory
`access_denied` preserves both the session and an unsaved library draft, while
`administrator_required` and `csrf_invalid` end the session without refreshing
or replaying the rejected mutation. It supplements the real revocation journey;
it does not claim backend authorization coverage.

Before editing, the permission suite requires an exact accessible dialog name
and a unique, correctly referenced visible title. It attaches
`library-dialog-aria` JSON with the dialog's label attributes, actual heading
IDs and text, and referenced-ID match counts. Anonymous dialog selection is used
only to collect these diagnostics; workflow actions retain exact named-dialog
locators. MUI's automatic `DialogTitle` ID must not duplicate a separately
labelled child span in the artwork and preference dialogs.

## Inputs and artifact boundary

The browser reads `GOBY_PHASE3_BROWSER_CONTEXT` and checks that the context is a
bounded `0600` regular file under an owned `0700` directory on Linux. Its `RunId`
must equal `GOBY_PHASE3_BROWSER_RUN_ID`; its `Marker` is
`goby-phase3-browser-fixture-v1`. Only this private context carries the disposable
administrator password. The origin must be an isolated `127.0.0.1` HTTP port,
excluding database and retained service ports.

The context contains `BaseURL`, `AdminId`, `AdminName`, `AdminPassword`, `UserId`,
`UserName`, `LibraryId`, `LibraryName`, `LibraryPath`, `WritableDirectory`,
`AudioItemId`, `AudioItemName`, `ArtworkEntityId`, `ArtworkEntityName`,
`CacheTaskId`, `CacheTaskName`, `ImageUploadPath`, `ImageUploadPathSecond`,
`ArtifactsDir`, and `ResultPath`. The two PNG files have different pixels. Names
and identifiers come from the fixture's actual database records.

The spec disables trace, video, automatic failure screenshots, and service
workers. It blocks foreign network origins and records only credential-free
checks and stage names. Explicit desktop and mobile screenshots show saved
metadata and artist navigation. Input credentials are never copied to results.

## Stage contract

For each stage, the browser atomically writes the owned `0600` file
`stage-<phase>-request.json` with `{RunId, Phase}`. The Go observer independently
checks the database or performs the fixed stage action, then writes
`stage-<phase>-database.json`. Each acknowledgement must contain
`Marker: goby-phase3-stage-database-v1`, matching `RunId` and `Phase`,
`Complete: true`, and `SourcesUnchanged: true`. The browser waits for each
acknowledgement before continuing.

1. `authentication`: sign in through the actual administrator form.
2. `library-conflict`: retain a dirty library draft while a separate actor
   renames the saved library. A stale save must fail with `409`; reload must show
   the concurrent name. Canceling the discard prompt keeps the original draft.
3. `library`: reject a relative path in the form, browse and validate an approved
   directory, add it, rename the library, and disable both future local import
   options. The original path remains present and no scan is implicitly started.
4. `preferences`: reject a resume rewind of `301`, then save audio `eng`, subtitle
   `zho`, subtitle mode `Always`, and a rewind of `10` seconds. Unimplemented
   client preference fields are not offered as writable controls.
5. `artwork`: reject a text file in the picker; upload, delete, and restore a
   genre's Primary image; add two different Backdrops, reorder their real tags,
   and reset them to the automatic empty set. Upload a user avatar. Saved images
   must actually load in the browser.
6. `music`: reject an artist name exceeding 1 KiB of UTF-8, then save an album
   tag, two separate artists, an album artist, a composer, track `7`, and disc `2`
   through metadata overrides; keep the seeded genre. The album tag does not
   change physical album identity or directory membership.
   Upload the audio item's Primary artwork.
7. `schedule`: replace the cache task's schedule with a `ServerStarted` system
   event, preview an event with no calendar occurrences, and save it.
8. `restart`: close the application and restart it on the same private origin.
   The observer requires one completed startup event run and returns its real
   `StartupRunId`.
9. `persistence`: refetch saved library, preference, music, and decoded image
   content through the restarted application. Browse real music artists at a
   mobile viewport; the observer compares the persisted state.
10. `permission-revoked`: remove administrator access while another library
    draft is open, using the real managed-user mutation and session revocation.
11. `permission-denied`: the draft must not save, and the browser returns to
    sign-in. The observer proves the library did not change and restores the
    account role without restoring the revoked session.
12. `cleanup`: sign in again, sign out through the UI, and require the observer
    to close its own session and prove no fixture sessions remain.

The browser result marker is `goby-phase3-browser-result-v1`. It includes all
twelve ordered stages, the observed `StartupRunId`, and nine checks:
`LibrarySaved`, `LibraryConflict`, `PreferencesSaved`, `ArtworkSaved`,
`MusicSaved`, `TriggersSaved`, `RestartPersisted`, `PermissionRevocation`, and
`Completed`. Every check must be true, with no browser runtime errors or foreign
requests. `Complete` becomes true only after the cleanup acknowledgement. A
context file, successful HTTP response, or screenshot alone is not acceptance.

The browser's internal deadline is 420 seconds, within the fixture's bounded
driver lifetime. The selected remote runner must retain both browser and Go
results and prove owned process, session, application, and database cleanup.
Failures write a partial result with `Complete: false`. The root task records the
selected source, runtime and dependency hashes, actual results, limitations, and
closeout evidence after coordinated verification.
