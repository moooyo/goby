# Programs saved product reader: captured check passed, product gate failed

The first saved-file consumer ran once on the real 23-field transition input
and the independently reviewed capture02 startup summary. `--check-captured`
passed with child exit 0. The following `runtime.load_programs_product` call
failed with child exit 2 and `ContractError: programs_artifact_independent_review`.
The canonical immutable review has status `passed`; the Python and JavaScript
complete-profile consumers incorrectly require `verified`. At this failed-attempt
checkpoint, the correction was prepared but not tested or integrated.
The canonical review and the original failure evidence remain unchanged.

The [checkpoint](programs-saved-product-first-attempt-20260917.json) pins the
original caller result, closure readback, stage outputs and real input. Original
SSH exit 2 was returned synchronously. Four recorded PIDs, three groups and the
owned unit cgroup are gone; source and input pins stayed exact. Peak memory was
47,345,664 bytes against the unchanged 1 GiB limit. No automatic retry ran.

This is a saved-file product-gate failure. Large-archive acceptance was not
reached, and the existing application/PostgreSQL runtime was not re-observed.
Capture02's accepted state and closure, capture01's failure and the separate
138/142/151 component scopes retain their original meanings. Fresh entry,
product-gate acceptance and recovery admission remain necessary. The new direct
transition output is uncreated, no transition has run, and the hold remains;
there is no new v4 epoch or client acceptance from this attempt. The subsequent
[152-check correction](programs-review-status-verification-20260917.md) and
[second actual product acceptance](programs-saved-product-second-attempt-20260917.md)
retain separate scopes and do not relabel this failure.
