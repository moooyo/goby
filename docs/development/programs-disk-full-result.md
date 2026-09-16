# Programs disk-backed full-run result

Status: **full regression failed; owned resources closed**. This single run used
Git `3d6b79b36b6a5050e174f7a152f214b9356608c8`, its compact 924-file snapshot,
and the retained 57 administrator dist files. Exact remote paths, hashes and
review boundaries are in [the result record](programs-disk-full-result.json).

| Result | Accepted scope |
| --- | --- |
| 18 complete passing packages | 1,447 top-level passes |
| Server package failed | 567 top-level and 1,704 child pass events remain partial evidence and are excluded |
| Recovery package passed | 26 top-level passes; 260.718 seconds |
| Declared M2 mount-profile skip | Preserved once; the profile did not execute |
| Ordinary and embedded builds | Neither ran; no new build artifact or full-suite acceptance |

Server produced 9,144 complete Go JSON events. Its 10 test fail events comprise
two top-level tests and eight children, followed by one package fail. All direct
failures are ENOENT reads of nine files under
`tests/compatibility/fixtures/reference/emby-4.9.5.0`: the
`scheduled-tasks-fresh-m5f-trigger-` fixtures for clear, original,
duplicate-interval, daily, startup, unknown, mixed and system-event, plus
`scheduled-tasks-fresh-m5f-batch-a-stop-result-0.json`. These files are absent
from the frozen manifest. The archived worker source matches all 924 manifest
file lengths and hashes. The failure exposes an incomplete test-input snapshot.

The run retained `-race -p=1 -parallel=1 -count=1` and the original
1,500-second test, 1,560-second command and other business/closure budgets.
All 19 Go stdout streams parsed completely, all 19 stderr streams were empty,
and no race or Go timeout marker was found. The worker reported `test_failure`
and the unit ended with `exit-code/1`, without OOM termination. The coordinating
agent's terminal readback recorded a 2,281,836,544-byte memory peak below the
3,221,225,472-byte worker limit.

All 55 outer commands closed. The saved records bind owned PostgreSQL shutdown,
an empty worker cgroup, both ext4/loop closures, RAM unmount, deletion
of the compiler backing image, exact protected state and lock release, with no
cleanup errors. The coordinating agent verified the complete 116,502,807-byte
archive hash. Independent review verified its pinned worker/supervisor members,
the raw Go streams, referenced inputs and closure bindings; it did not repeat
the whole-archive hash or execute archived code.

Prepare a complete snapshot of the **same Git commit and unchanged 57 dist
files**, including the required compatibility fixtures. Keep assertions, the
original declared skip and all timeouts. Do not combine partial runs to claim
full-suite success. Source preparation is in progress; no replacement input pins
or new full run are recorded here.
