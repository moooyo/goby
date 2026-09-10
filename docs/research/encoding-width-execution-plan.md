# M5h bounded encoding-width execution study

Status: **completed for the bounded official Emby 4.9.5.0 source/profile/software
comparison, with successful owned-fixture cleanup**. The
[result report](encoding-width-reference.md) records actual 1280-by-720 and
3840-by-2160 outputs for configured widths 1280 and 0, complete decoding, timing
limits, and preserved evidence. Zero is not generalized to an unlimited setting.
The method and first-fixture failure below remain the executed study's history;
this completion does not establish Goby deployment or full client compatibility.

## Question and controlled comparison

On one new official Emby 4.9.5.0 instance, transcode the same four-second,
3840x2160, two-frame-per-second source twice. The source uses MPEG-4 Part 2
video and mono AAC audio. Both outputs request H.264/AAC progressive MP4;
different video codecs make stream copy incapable of satisfying the request.
Both copy flags and both direct-delivery flags are explicitly false.

The two cases share a complete encoding configuration clone with
`EnableHardwareEncoding: false` and `EncodingThreadCount: 1`. Only
`TranscodingMaxWidth` changes between 1280 and 0. The original complete
encoding object is retained and restored after each case and during cleanup.
`HardwareAccelerationMode` retains its original value; no numeric enum meaning
is invented. `PrivateDevices=yes` excludes GPU device access, and producer
evidence must identify a software encoder and no hardware decoder.

The DeviceProfile uses the previously successful Video/Streaming/http/MP4,
H.264/AAC shape with an empty DirectPlayProfiles array. Its streaming ceiling
is 100,000,000 bits per second, and it has no Width, Height, MaxWidth or
MaxHeight fields. MediaSourceId, UserId and stream indexes come from the new
fixture's actual item and source. StartTimeTicks is zero. Each case uses a
newly acknowledged PlaySessionId and follows the returned same-origin
TranscodingUrl without altering its media parameters. There is no fallback
to a direct/original URL or an automatically retried alternate protocol.

## Fixed owned resources

| Resource | Fixed value |
| --- | --- |
| Work parent | `/opt/goby-test/exec-work-m5h` |
| Evidence | `emby-width-fresh-m5h-20260910-02` under the work parent |
| Program data | `emby-width-fresh-data-02` under the work parent |
| Source | `source/Width Source M5h (2026)/Width Source M5h (2026).mp4` under evidence |
| Unit | `goby-emby-width-fresh-m5h-20260910-02.service` |
| Private HTTP / HTTPS configuration | `18100` / `18500` |
| Server name | `Goby Encoding Width Fresh M5h 20260910 02` |

The existing official package at
`/dev/shm/goby-emby-reference/package/opt/emby-server` is shared read-only.
No package reinstall or extraction occurs. Program data and runtime are the
only service-writable paths. Source, shared package, earlier evidence and
original fixtures are read-only in the service's actual mount namespace.
The systemd WorkingDirectory remains the new runtime directory because mount
namespacing rejects a WorkingDirectory under `/dev`. Before `exec`, the
launcher explicitly changes directory to the read-only shared APP. The actual
Emby process cwd must equal APP: its ELF interpreter is the relative path
`lib/ld-linux-x86-64.so.2`.

Normal first-run setup creates one new administrator account. Its one new
ordinary login is handed to the recorder through private attested credentials;
the recorder does not log in again. The same credential owns configuration,
library and playback requests. No application key, UI, viewer account or
playback progress/watch-state report is used. Preparation failure invalidates
an acknowledged credential when possible and stops only the owned service.

## Source and verification

The operator generates the source with the installed FFmpeg 9.0.1 software
toolchain, fixed lavfi testsrc2 and sine inputs, MPEG-4 Part 2 video, AAC audio,
two fps, four seconds and eight video frames. No existing source is read or
copied. The source is completely probed and decoded before READY. The
recorder creates at most one movie library pointing only at its marked source;
realtime monitoring, provider fetching, downloads and media-adjacent writes
are disabled. Only this library receives an explicit refresh.

For each case, a full progressive response must reach HTTP EOF within its
byte/time bound. ffprobe reads only the downloaded local file and reports
codec, dimensions and decoded frame count. A separate FFmpeg 9.0.1 `-xerror`
decode consumes the whole local output. A valid result requires H.264 output,
eight decoded video frames and successful complete decoding. It does not
require the zero-width case to have a preselected output resolution.

The owned producer's command/log is retained privately and checked for
software H.264 encoding, with no stream-copy or hardware-decoder plan. The
generated source codec and decoded output codec provide an independent
non-copy check. GET, PlaybackInfo and configuration values alone are not
output-dimension evidence.

## Bounds and stopping

- Emby and its producer: MemoryMax 1152 MiB, CPUQuota 150%, TasksMax 256;
  admission requires at least 1920 MiB MemAvailable and 2 GiB persistent free
  space. Running available-memory reserve is 512 MiB.
- Source generator and local probes run serially, with a 1 GiB address-space
  limit, explicit single-threaded media work, bounded process output, and
  process-group timeout cleanup. Source generation is limited to 90 seconds
  and 32 MiB; full source/output probe and decode operations are bounded.
- At most two playback negotiations and two complete media GETs. Media limit
  is 64 MiB per case and 128 MiB total, with 120 seconds per GET. JSON replies
  are limited to 256 KiB; main and cleanup HTTP counts have separate reserves.
- No encoder overlap: the first response, owned ActiveEncodings cleanup and
  producer-exit proof precede restoration and the second case. A failure stops
  case expansion. It never launches an unbounded retry or copies an old file
  to make the comparison pass.
- Cleanup reserves its final two HTTP requests and 512 KiB for logout and its
  independent 401 probe. If a prior stop or full restoration failed, cleanup
  may make one additional bounded attempt after checking the actual producer
  or configuration state. Media requests are never retried.

Cleanup stops each acknowledged owned PlaySessionId, proves producer exit,
restores the complete original encoding object and compares types/property
presence. The one ordinary credential is logged out and independently receives
401 from Sessions. The operator then stops its attested invocation and removes
only new DATA/source; all raw, sanitized, media and process evidence remains.
Partial evidence and incomplete cleanup are reported as such.
The root runner invokes operator cleanup even if recorder initialization
fails. A failure after an in-memory credential handoff also runs a dedicated
logout/401 path; it does not write into an incompletely initialized capture
root or claim that its HTTP evidence was durably saved.

The 1152 MiB cgroup ceiling replaces an initial unmeasured 1536 MiB proposal
after the protected environment's available-memory measurement. Admission
leaves a 768 MiB margin above that ceiling. This is a bounded execution
experiment, not an established 4K memory requirement: an owned-unit OOM is
retained as a failed observation and cannot waive complete-output acceptance.
External local probes run only after the server's producer has stopped.

The first M5h fixture failed during executable startup with exit 127, before
source generation or any login. Its launcher had omitted the APP directory
change. Root cleanup proved its service and cgroup stopped, removed only its
owned DATA/source, and preserved the old 2305-record corpus. The fixed second
fixture has independent names and roots. All 14 retained first-fixture files
and its separate frozen source bundle remain immutable evidence; first-run
credentials, source, data or server identity are never reused.

Original Emby PID 3131777 and Goby PID 3614026, UID 995, start ticks 25289276,
binary SHA-256 `e1f6b723eb963ad855f465d0798958480615b8b000455fcf26bc7b088b8d2f2f`
are protected. The 2305-record/4610-file corpus and 240 permanent media files
remain immutable. The removed M5g DATA and previously retired M5f 512-source
fixture remain explicit historical deletions; they are never reopened.
