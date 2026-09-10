# Encoding-width execution reference

Research date: 2026-09-10 (Asia/Shanghai).
Status: **bounded official-reference execution study and owned-fixture teardown complete**.

On official Emby Server **4.9.5.0**, the same generated 3840 by 2160 source
produced **1280 by 720** H.264/AAC MP4 with `TranscodingMaxWidth: 1280`, and
**3840 by 2160** H.264/AAC MP4 with `TranscodingMaxWidth: 0`. Both responses
completed, contained eight decoded video frames, and passed strict full decoding.
This supports the inference that zero imposed no additional width cap for this
source, fixed profile, and software path. It does not establish an unlimited
setting for all sources, clients, encoders, or other server policies.

The [compact result](../development/m5h-encoding-width-reference.json) and
[capture audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/encoding-width-fresh-m5h-2-audit.json)
record the completed results and cleanup. The
[execution plan](encoding-width-execution-plan.md) retains the resource,
ownership, and stopping constraints used for the comparison. This extends the
earlier [configuration mutation study](configuration-mutation-reference.md),
which observed stored width values without executing their media URLs.

## Instance, source, and toolchain

The successful second fixture used server ID
`61dea10d3f974dc784be57226a8daf62`, PID `3640734`, and unit
`goby-emby-width-fresh-m5h-20260910-02.service`. Its private network namespace
exposed loopback HTTP port `18100`. Program data and the generated source were
owned by this invocation. The instance has since been stopped and its DATA/source
removed; retained evidence is independent of those deleted runtime paths.

The [preparation operator](../../scripts/test-env/prepare-encoding-width-fresh.py)
generated a four-second, two-frame-per-second, eight-frame MP4 from fixed
`testsrc2` and sine inputs: 3840 by 2160 MPEG-4 Part 2 video plus mono AAC.
It probed and completely decoded the source before handing off the credential.
Source SHA-256:
`65cf7a11f33c785bcf885bec5d115d076e06967ab55c9aebea349d6a5f218953`.
That identity is bound into both output observations and the audit. The source
was newly generated, not copied from the 240 permanent media fixtures.

The operator generated the source and independently checked retained outputs
with the installed **FFmpeg/ffprobe 9.0.1** toolchain. Emby's playback producer
used FFmpeg from its shared official package. These are separate executables;
the oracle's 9.0.1 version must not be attributed to Emby's producer. Full
generation, tool, and producer command evidence remains private; public records
retain bounded results and hashes rather than credential-bearing command text.

One new ordinary administrator credential was created during setup and handed
to the recorder privately. The recorder issued **zero login requests and zero
application-key requests**. It created one owned movie library and refreshed
only that library to obtain the real item/source/stream identities. There was
no viewer account, browser, progress/watch-state report, or external media URL.

## Controlled requests

Both cases posted a complete encoding configuration clone to
`POST /emby/System/Configuration/encoding`. Relative to the original baseline,
both fixed `EnableHardwareEncoding` to false and `EncodingThreadCount` to 1.
**Only `TranscodingMaxWidth` differed between the two experimental bodies.**
`HardwareAccelerationMode` and every other unselected encoding value were
retained. The deprecated thread-count property is a fixed input here, not proof
of the producer's actual thread count. Submitted configuration bodies:
[1280](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/encoding-width-fresh-m5h-2-width-1280-settings.json),
[zero](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/encoding-width-fresh-m5h-2-width-0-settings.json).

The two `POST /emby/Items/6/PlaybackInfo` bodies were identical. They used the
actual `UserId` and `MediaSourceId`, video/audio indexes `0`/`1`, subtitle index
`-1`, `StartTimeTicks: 0`, and `IsPlayback: true`. DirectPlay, DirectStream,
video copy, and audio copy were explicitly disabled; transcoding was enabled.
Both the top-level and profile streaming ceilings were 100,000,000 bits/second.
The profile had no width/height selectors and used this conversion entry:

```json
{
  "Type": "Video",
  "Container": "mp4",
  "VideoCodec": "h264",
  "AudioCodec": "aac",
  "MaxAudioChannels": "2",
  "Protocol": "http",
  "Context": "Streaming"
}
```

`DirectPlayProfiles` was `[]`. The string `"2"` is a channel ceiling, not a
request to manufacture stereo: the actual outputs remained mono. See the
[1280 PlaybackInfo](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/encoding-width-fresh-m5h-2-width-1280-playback.json)
and [zero PlaybackInfo](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/encoding-width-fresh-m5h-2-width-0-playback.json).

Each case used its own acknowledged `PlaySessionId` and followed the returned
same-origin progressive `TranscodingUrl` without changing media parameters.
The server returned `/videos/6/stream.mp4`; the recorder used its `/emby` alias.
The URLs contained no width/height query parameter. Public exports preserve
query names while redacting query values, so they are not replayable URLs.
PlaybackInfo's 3840 by 2160 MPEG-4 metadata describes the source, not the
converted output. Only the downloaded media supplies the output dimensions.

## Complete outputs and producer evidence

| Global width | Actual video | Actual audio | Video frames | Retained body bytes | Evidence |
| --- | --- | --- | ---: | ---: | --- |
| `1280` | H.264 Main, 1280 by 720 | AAC LC, 48 kHz, mono | 8 | 194,979 | [HTTP](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/encoding-width-fresh-m5h-2-width-1280-media.json), [probe/decode](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/encoding-width-fresh-m5h-2-width-1280-media-probe.json) |
| `0` | H.264 Main, 3840 by 2160 | AAC LC, 48 kHz, mono | 8 | 1,177,454 | [HTTP](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/encoding-width-fresh-m5h-2-width-0-media.json), [probe/decode](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/encoding-width-fresh-m5h-2-width-0-media-probe.json) |

Output SHA-256 values are:

- Width 1280: `d0028180f2783ac5c4aa29bd640b77da9f9b4d682e9d897b9f811cf53b8ef726`.
- Width 0: `a75589914a0755c07e323dbd690987d0dce111d8669ba71181d85a17d09766dc`.

Both GETs returned complete `200 video/mp4` responses with chunked transfer
and `Accept-Ranges: none`. The full bodies reached EOF and remained in private
evidence. Independent ffprobe counted eight video frames in each file; a
separate FFmpeg `-xerror` decode consumed both video and audio through
`progress=end`, exited zero, and reported no duplicate or dropped video frames.

Each output also has a live process observation in the owned cgroup and two
retained transcode logs confirming the same command hash. The recorder's
`software_command` check requires the executable to belong to the official
package, the input to be the sole owned source, exactly `libx264` video encoding,
and an owned MP4 destination. It rejects stream-copy and hardware-acceleration
arguments. The service's private device namespace excludes GPU device paths.
This establishes actual software conversion rather than relying on a profile
label; it supplies no positive GPU evidence. The different source/output video
codecs independently rule out copied video.

The output probes also retain a timing limitation: both containers report
**5.000000 seconds**, video starts at **1.000000** with a four-second duration,
and audio starts at zero with a **4.032000-second** duration. They are not simply
four-second output containers. Complete decoding and eight frames establish
the width comparison, not timestamp equivalence or correct A/V synchronization.

## Accounting, restoration, and teardown

The capture completed in **7.182 seconds**; that is workflow elapsed time, not
an individual encoder latency or a 4K performance benchmark. Its additions are:

| Record group | Count | Meaning |
| --- | ---: | --- |
| `encoding-width-fresh-m5h-2-` HTTP | 44 | Complete exchanges, including both media GETs |
| Same prefix, media probe observations | 2 | Independent checks tied to retained output hashes |
| Same prefix, audit | 1 | Eight successful cleanup/preservation checks |
| `encoding-width-fresh-setup-m5h-2-` | 12 | Complete setup HTTP exchanges |
| `encoding-width-fresh-operator-cleanup-m5h-2-1-` | 2 | Independent operator credential-invalidity reads |
| Total added records | **61** | Corpus grows from 2305 to **2366** |

The initial second-fixture readiness response was a complete
[HTTP 503](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/encoding-width-fresh-setup-m5h-2-001-readiness-public.json),
followed by `200`. It is not a missing/non-HTTP observation. Derived summaries
and guard reports are outside the corpus count.

The complete original encoding object was restored after each case and verified
again during final cleanup: all three restoration proofs preserve values,
types, and field presence. Both acknowledged playback scopes received owned
ActiveEncodings DELETE cleanup returning `204`, with producer-exit proof before
the next case. There were no media retries, cleanup-stop retries, or
cleanup-restoration retries. No alternate protocol or original-file fallback
was used to obtain a successful result.

The ordinary credential's
[logout](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/encoding-width-fresh-m5h-2-cleanup-logout-control.json)
returned `204`; its independent
[protected read](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/encoding-width-fresh-m5h-2-cleanup-invalid-control.json)
returned `401`. Operator cleanup then independently observed `401` twice,
stopped the attested service, removed only DATA02/SOURCE02, and retained evidence.
The [result summary](../development/m5h-encoding-width-reference.json) records
successful operator cleanup and both removals. The capture audit's
`freshProcessUnchanged` describes the preceding capture phase, before teardown.

All preceding **2305 records**, their **4610 raw/export files**, and **240
permanent source files** remain preserved. The capture additionally checked
the 24 setup raw/export files, giving its 4634-file preservation count. The
original Emby PID 3131777 and M5g Goby PID 3614026 were protected and unchanged
by this research. Previously removed disposable fixtures were not recreated.

## Retained first failure and source pins

Fixture 01 failed during executable startup with exit 127 because the official
binary uses the relative ELF interpreter `lib/ld-linux-x86-64.so.2`. The launcher
had not changed to APP before execution. This occurred **before source generation
or any login**, added no HTTP records, and created no credential. Its
[failure/cleanup report](../development/m5h-encoding-width-startup-attempt-1.json)
proves the owned service/cgroup stopped and owned DATA/source removed.

Fixture 02 retained systemd's runtime WorkingDirectory but changed the launcher
to APP immediately before `exec`. It used independent roots and identities.
All 14 retained first-fixture files and 25 frozen bundle files, **39 files in
total**, remain immutable evidence. They do not add reference-corpus records.
The [plan](encoding-width-execution-plan.md) records the detailed boundary.

The [final 25 guard tests](../development/m5h-encoding-width-guards-attempt-2.json)
passed with synthetic memory-only fixtures and zero HTTP, media processes, or
capture writes. The earlier 22-test report remains its own pre-correction
checkpoint. These guard results are separate from the two actual media outputs.

| Final source pin | SHA-256 |
| --- | --- |
| [Recorder](../../scripts/test-env/reference-encoding-width-fresh.py) | `39e88efeab63ea3ce14aee8460e59c0dfffd878b5da702820bd64ac258b1804e` |
| Preparation/cleanup operator | `54ca55914049561752cb25e7825ae14a15aaa3d25b7275b9f9e8eba86c6e6c06` |
| [Guard tests](../../scripts/test-env/test-encoding-width-reference.py) | `0f963b3e634370f7af51dbd436a1c3ca68ff10fec69f0e0730aa978cc45663c9` |

## Interpretation boundary

Under this fixed source/profile/software configuration, 1280 limits the actual
output to 1280 pixels, while zero permits the source's full 3840-pixel width.
Other codec profiles, client limits, bitrates, aspect ratios, inputs, hardware
paths, HLS, seeks, simultaneous sessions, and durations were not compared.
The result does not establish missing-field reset behavior, universal unlimited
width, actual encoder thread semantics, GPU compatibility, or A/V timing parity.

Goby separately combines a positive compatibility ceiling with its native
effective `MaxWidth`; zero leaves the native ceiling in force. That remains
an explicit [Goby implementation policy](../development/configuration-compatibility.md),
not an Emby native-setting equivalence inferred from this experiment. This
official-reference result does not constitute a Goby deployment or complete
M5h/client acceptance. At this research checkpoint, the protected live Goby
service was still the accepted M5g/schema-20 instance.
