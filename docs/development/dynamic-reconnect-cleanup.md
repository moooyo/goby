# Dynamic reconnect scratch ownership

Status: the focused remote HTTP regression passed in
`phase2-reconnect-cleanup01` on `test-env`, against phase 2 `source17`
(`5aead247d22261a73658e56d3363d7fce0ff723f`). Its five test pass events include
the new cleanup regression and the affected dynamic playback/subtitle paths,
with no failures or skips. The verification unit exited with `MainPID=0`.
The separate `phase2-browser03` scope now passed on the same `source17`: overall
`Accepted=true`, browser and fixture exits zero, and Go one pass event with zero
failures or skips. The later v3 `phase2-browser05` scope also passed, and
[phase 2](amd-media-phase2-20260919.md) now has complete owned-resource/documentation closeout.

A dynamic presentation keeps the same authenticated timeshift window across
upstream reconnections, while each connection generation owns a separate
transcode job. Completed media is copied into the timeshift Store before the
publication callback returns. Old encoder scratch is therefore independent of
the retained playback window and its advertised-resource grace.

Previously, the reconnect loop replaced `session.jobID` with the new job without
invalidating the completed job's cache. A later stop could cancel only the
current job, leaving old scratch until the engine's ordinary idle retention.
The loop now cancels the terminal job before closing its input or opening the
next generation. An already missing job is treated as reclaimed. Other
cancellation errors leave its identity intact and postpone reconnection.

`CancelJob` preserves completed durable history and wakes the engine's existing
cache reclaimer. Directory deletion remains asynchronous and waits for any
already-open readers, but no longer waits for the idle TTL. Retained Store media
remains readable under its original scope and grace. Explicit playback stop
revokes the window and cancels the current producer as before.

The focused real-HTTP regression uses the existing isolated dynamic-source and
FFmpeg fixture. It advertises and reads an old artifact, ends the first upstream
response, waits for actual next-generation publication, and requires old scratch
to disappear within five seconds while the previously advertised bytes still
read identically. It then stops playback and requires both job directories and
all Store windows, bytes, readers and publication reservations to be gone. The
fixture's ordinary cache idle TTL cannot satisfy that short cleanup bound.

The browser resource-cleanup predicate was unchanged. Before global close,
`phase2-browser03` observed all three cache directories absent, finite/dynamic
sessions and active jobs zero, every Store usage counter zero, stream slots and
active upstreams zero, and the manager ready. Thus ordinary runtime cleanup,
rather than global shutdown, satisfied the gate.

The browser scope reported 51.781 seconds and `minfree=675704832`. Source remained
unchanged, the private context was removed, process groups closed, `MainPID=0`,
and the cgroup PID list was empty. Evidence is retained under
`/opt/goby-amd-media-20260919-50f45177f297/evidence/phase2-browser03/`.
The client was a native-video and HLS.js 1.6.0 beta 2 harness; this result does not
claim complete original Emby Web compatibility. The earlier `phase2-browser02`
client pass with failed fixture cleanup remains a failed aggregate receipt.
