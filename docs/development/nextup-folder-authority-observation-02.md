# Original folder authority observation02

Status: **six requests independently attested and the exact administrator
session closed. No policy was written; grant semantics remain unproven.**

Preparation03 echoed numeric EnabledFolders values 101 and 103 but returned
empty own-token Views. This separate observation records selectable identifiers
without changing that policy or replaying a consumed preparation.

## Public contract and observed mapping

The official [SelectableMediaFolders endpoint](https://dev.emby.media/reference/RestAPI/LibraryService/getLibrarySelectablemediafolders.html)
is an authenticated GET without declared query parameters, returning a bare
array with Name, Id, Guid, SubFolders and IsUserAccessConfigurable. The
[VirtualFolders query](https://dev.emby.media/reference/RestAPI/LibraryStructureService/getLibraryVirtualfoldersQuery.html)
provides independent management identifiers and paths. The
[UserPolicy schema](https://dev.emby.media/reference/RestAPI/UserService/postUsersByIdPolicy.html)
declares EnabledFolders strings without specifying which identifier to use.

The actual selectable response has ten rows. The independent audit uniquely
relates the owned rows to unchanged management DTOs and established root paths:

| Symbol | Management/view/selectable Id | Selectable Guid | Native SubFolder.Id |
| --- | --- | --- | --- |
| LA | 101 | b953914417ce42b5a5e07a3e8f6fd386 | 102 |
| LB | 103 | 38fbd09cb8af4731993dd77e1750457f | 104 |

Both owned top-level rows and their single subfolders report
IsUserAccessConfigurable true. Their exact paths end in
`reference-nextup-global-preparation-03/media/LA` and `media/LB` beneath
`/opt/goby-test/exec-work-m3e`. This establishes identity relations, not which
identifier produces restricted access.

## Actual execution and attestation

- Run: `nextup-folder-authority-observation-20260913-02`.
- Output: `/opt/goby-test/exec-work-m3e/reference-nextup-folder-authority-observation-02`.
- Execution: `/opt/goby-test/exec-work-m3e/reference-nextup-folder-authority-execution-02`.
- Input SHA-256: `88284a97ac12a9198479a4a3c90d186f1f140344cb14905cbcf5776bceebae37`.
- Worker SHA-256: `c667f0094d33699c50fbb12127796b24868db137b0337e5b908810dca73d89a2`.
- Unit: `goby-nextup-folder-authority-observation-02.service`.
- Invocation: `ac1ab87be20946a19976c77c1ef382e0`; former PID `1509103`.

The exact sequence was administrator login200, SelectableMediaFolders200,
VirtualFolders/Query200, Devices200, logout204 and same-token Sessions401.
The [producer terminal](nextup-folder-authority-observation-02-terminal.json)
retains awaiting_independent_attestation, six requests, completed/closed true,
no uncertainty or pending request, and grantSemanticsProven false.

The [independent terminal](nextup-folder-authority-observation-02-independent-terminal.json),
SHA-256 `5fa01e1b01e4ea1d8fa69edc528151243531c451825a434ed3c7c298f93da7a5`,
checks all six intent/response pairs, payloads, token contexts, raw lengths,
timestamp order and exact logout/rejection. It verifies exit0/MainPID0,
absent original PID and empty cgroup. All ten library DTOs and all ninety
predecessor device DTOs remain identical. The only new device matches the login
user, client metadata and InternalDeviceId; the registry now has 91 devices.

The [before](nextup-folder-authority-observation-02-preservation-before.json)
and [after](nextup-folder-authority-observation-02-preservation-after.json)
receipts confirm all 138 historical roots, main files and service states remain
identical. The full Goby v7 state matches except capture time, at 83 sessions,
70 devices and 185 activity entries. The consumed scope inventory SHA-256 is
`a923843d7b0b26cead3d063b1c1b78bc90604ace13d261e5475c7e77f1f36fdc`.

This is not a new complete public snapshot of every user, configuration,
preference and detail route. Claims are limited to the recorded endpoints and
preservation checks. All execution used remote isolated Python. The observation
and audit read no original implementation or reference database bytes. The
existing proxy was reused unchanged; its internal executable reads are not
claimed to be zero.

Input/execution01 was never dispatched. Static review caught an unsupported
private keyword passed to the frozen protected helper. Its original files and
pre-execution-review.json remain preserved with zero HTTP and no unit/output.
Revision02 uses the supported uid argument and an explicit owner-only mode
check in fresh input/execution/output roots.

## Subsequent bounded verification

The [109-request Guid verification](nextup-folder-grant-verification-01.md)
subsequently confirmed the Guid pair through P's own Views, catalogs and six
episode details, restored the complete original policy and closed both tokens.
That separate experiment supplies grant evidence; this earlier read-only
observation and its original false grantSemanticsProven flag remain unchanged.

The experiment treated the observed Guid pair as a candidate and froze the
complete original policy, exact proposed grants,
raw identifier authority, before/after public preservation and cleanup limits.
Require own-token profile, Views and complete catalog evidence. Restore the
original full policy and close every new token after success or known failure;
unknown outcomes require separate recovery with no automatic replay.

Do not substitute EnableAllFolders true for restricted access. Fresh matrix
preparation still requires current population/budget authority, four playback
calibrations and independent fixture attestation. Main upgrade and positive
client acceptance remain open.
