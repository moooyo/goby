# FFmpeg copy-timestamp progress regression

Status: source prepared; no execution result is implied by this directory.

The pinned FFmpeg 9.0.1 scheduler can report `AV_NOPTS_VALUE` after all mux input
streams finish and before the mux task finishes draining. With `-copyts`, the
unpatched `print_report()` subtracts its already-established first timestamp
from that sentinel before checking for an unknown clock. A first timestamp of
`10083333` microseconds produces the observed wrapped value
`9223372036844692475`, which Goby's bounded progress parser correctly rejects.

The patch preserves the unknown timestamp so the existing reporter emits `N/A`.
It does not change media timestamps, seeking, muxing, scheduling, Goby's parser,
or any duration or resource limit. The upstream paths are
[print_report](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/fftools/ffmpeg.c#L572)
and [progressing_dts](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/fftools/ffmpeg_sched.c#L465).

## Deterministic native gate

`native-regression.c` includes each selected tree's real `fftools/ffmpeg.c` and
calls its static `print_report()` function. It uses real progress serialization,
`ost_iter()`, AVIO file output, and final `avio_closep()`. Only CLI option values,
one output stream's counters, and the unrelated `of_filesize()` query are fixtures.
It neither copies nor substitutes the production timestamp arithmetic.

Each case runs in a fresh process to isolate the reporter's static state:

| Case | Contract |
| --- | --- |
| `unknown_before_origin` | Initial unknown clocks stay `N/A`; the first positive clock establishes the same origin. |
| `unknown_after_origin` | The observed first clock and last valid report are followed by unknown and recovered valid clocks. |
| `final_unknown` | A final unknown clock stays `N/A`, emits `progress=end`, and closes the real output. |
| `copyts_disabled` | Disabling copy timestamps preserves the existing absolute-clock behavior. |
| `zero_negative_and_recovery` | Zero, negative, and one-microsecond values do not establish an origin; later unknown and valid clocks preserve it. |

The negative control must reproduce the observed large integer in three cases;
the candidate must retain `N/A` in all three and preserve every valid value.
The harness explicitly uses `-fwrapv` because the baseline's subtraction has
signed-overflow undefined behavior in C. This makes the observed machine-level
negative control deterministic; it does not add that flag to the FFmpeg build.

The runner records source, harness, configuration, static-library, executable,
and raw progress hashes, plus bounded subprocess outcomes. It checks that bound
inputs remain unchanged and refuses an identical reporter for both controls.
The decoder queue gate remains a separate required installer step.

Run only in the designated remote verification environment, after the compatible
candidate FFmpeg build has completed:

```sh
python3 run-native-regression.py \
  --baseline "$unpatched_source" --candidate "$patched_source" \
  --build "$completed_candidate_build" \
  --binaries "$fresh_private_binaries" --evidence "$fresh_private_evidence"
```

The native and OCI installers invoke this gate before publishing their matching
FFmpeg and ffprobe pair. A changed native recipe publishes a new immutable prefix.

## Actual media and service gates

The native gate proves the production reporter's sentinel transition, not an
entire media pipeline. Bind the actual source file, recorded FFmpeg argv, old and
new executable hashes, and private input/output descriptors before executing the
media regression. The observed failure used a 120-second, 12-fps source and the
ordinary progressive H.264/AAC start path with `-copyts`; it was not a seek.

Replay the captured start argv and the separately captured 30-second seek argv
against fresh private outputs, retaining every progress block and bounded stderr.
Require normal process completion, finite-or-`N/A` progress throughout, a complete
final report, and independently decodable MP4 output with the expected duration,
stream selection, and timestamp origin. Keep the original failed binary and its
evidence unchanged. Do not manufacture a baseline media failure if it does not
recur during the selected repetitions; the native negative control owns the
deterministic overflow reproduction.

Then run the affected progressive media regressions and the original compound
service workload with the successor toolchain and unchanged parser, command,
resource bounds, fixture identities, and workload criteria. HTTP 200 alone does
not prove success: the streamed body must reach a complete EOF and the durable
job must finish successfully. Native success cannot replace these runtime gates.
