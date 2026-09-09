# Multithreaded seek oracle

The MP4/AAC threads=2 failure came from comparing independently produced lossy H.264 outputs byte-for-byte after decoding. Two repetitions of the same linear command produced 45 frames with identical timestamps, durations, and sizes, but different full-color hashes for 42 frames. Repeating the fast command also changed 41 frame hashes. This establishes that the observed output-hash difference cannot diagnose a seek regression.

An independent replay retained the input clocks, seek window, selected stream, filters, and two decoder threads, then replaced encoding and muxing with raw YUV420 framehash output. All 45 complete linear and fast records, including the header, time base, timestamps, sizes, and SHA-256 hashes, were byte-identical. Both files have SHA-256 5fa5cbfb3c2a6427c9b96dbdca11e1dce35961065bb669417269761a211bce90.

Every frame of all four encoded outputs was also compared with the independently decoded source, including the preceding and following wrong frames. The largest normalized MSE was 0.000006080994489298988, and the smallest neighboring-frame/correct-frame MSE ratio was 556.4410490574874. The existing 0.001 and 2 bounds are unchanged.

The regression keeps real threads=2 production and runtime seek proof. It replays the recorded producer arguments for strict complete pre-encode equality and checks every final full-color frame against the source. Single-thread output SHA equality, complete final timelines, strict decoding, and complete audio PCM equality remain required.

The accompanying JSON records the independent experiment, commands, hashes, and preserved evidence manifest. The raw recipes, media, stdout, stderr, and metrics remain in the owned m4f-threads-diagnostic-20260910 remote scratch directory. This diagnostic does not claim a test pass for the updated source; the final targeted run must use the updated M4f snapshot.