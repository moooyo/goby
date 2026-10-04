# Scan performance follow-up - October 5, 2026

Selected delivery source is full candidate
`42cb91f66b6542c2b7dc5c6d7aca1f800d765983`, with seven accepted changed paths
from `7e959c136f7ed1402513a61841153d505c9cc3b6`. The fixed A/I/F comparison
supports this limited choice: F beats image-only I in all three blocks for the
two priority episode cached_1 groups, and all three F/A cold layouts improve.
The cold paired medians are movies 0.8399026, flat episodes 0.7876278 and directory
0.8575505. Image-only I is an experiment, not the delivered source.

The result is not uniform. F/A movie incremental is slower in all three blocks:
median 1.0384958, +18.506 ms. F/I directory force_probe is also slower in all
three blocks: median 1.0423, +31.434 ms. F/A directory incremental is slower in
two blocks. All original stage-two regressions and I's adverse observations
remain below; none is dismissed as noise or treated as resolved by this choice.

## Source identities and measured scope

Stage one compares historical H `912354f2a48b5043a8824246f2fa011f8f34bbf1`,
pre-cleanup B `f5b09298b21cd47f7c6e79344f9c501898c948a8`, and cleanup current
C `7e959c136f7ed1402513a61841153d505c9cc3b6` (source `975d256e`). Its six
pilots, six independent count diagnostics and eighteen formal processes passed;
216 observations form 24 groups. Real cold C/H remains 1.3724
[1.2584,1.4615], slower in every block.

Stage two uses H, B=7e959c13 and C=full42. Six pilots, six diagnostics and eighteen
formal processes completed, with 216 observations/24 groups. The three bounded
changes and targeted correctness are listed below. Freeze SHA-256 is
`32ee00fb46e4b6cb41b13a8a86190dcbadd5a31e8f7003ff1ac8197598b99f54`;
full candidate archive SHA-256 is
`2162beef3a8b13590f1d22bd36440cc3231d6d55245194b7f0f3ebaf06b7adb0`.
The source commit is the selected delivery identity; all seven files match the
freeze. Four measurement drivers are unchanged.

The separate descriptor400 ablation uses A=7e959c13,
I=`924d2f1db719f1adf174401a90b01cb608c2a18c` (two image-only paths), and
F=full42. Its nine strict formal processes contain 189 observations/21 groups,
with no replaced samples. Root independently reconstructed every source job
value in both full and ablation batches. Timing is not pooled across stages.

## Stage one: complete paired comparison

C/B has six slower phase medians and C/H has twelve; no C/B group is slower in
all three blocks. Alongside real cold, both episode layouts' cached-after-
incremental phases are slower than H in all three blocks. Real warm C/B is
1.0305 and force is 0.9927; their C/H medians are 0.9919 and 1.0276. Every adverse
sample and wide range is retained in the complete paired table below.

| Phase | C/B three ratios | Median [min,max] | Reverse blocks | C/H three ratios | Median [min,max] | Reverse blocks |
| --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 0.4555, 2.7485, 0.9487 | 0.9487 [0.4555,2.7485] | [2] | 0.7868, 2.8380, 0.7555 | 0.7868 [0.7555,2.8380] | [2] |
| descriptor400 / directory_episodes / cached_2 | 0.3756, 0.9466, 0.9686 | 0.9466 [0.3756,0.9686] | [] | 1.0447, 0.9793, 0.8781 | 0.9793 [0.8781,1.0447] | [1] |
| descriptor400 / directory_episodes / cached_after_incremental | 0.8925, 1.0670, 0.9558 | 0.9558 [0.8925,1.0670] | [2] | 1.0798, 1.1895, 1.0675 | 1.0798 [1.0675,1.1895] | [1, 2, 3] |
| descriptor400 / directory_episodes / cold | 0.5212, 2.1344, 0.8502 | 0.8502 [0.5212,2.1344] | [2] | 1.0881, 2.2184, 0.8136 | 1.0881 [0.8136,2.2184] | [1, 2] |
| descriptor400 / directory_episodes / force_probe | 0.7632, 0.9552, 0.3167 | 0.7632 [0.3167,0.9552] | [] | 0.8568, 0.9606, 0.7109 | 0.8568 [0.7109,0.9606] | [] |
| descriptor400 / directory_episodes / incremental | 0.4671, 0.9523, 0.8675 | 0.8675 [0.4671,0.9523] | [] | 0.9232, 1.0005, 0.9497 | 0.9497 [0.9232,1.0005] | [2] |
| descriptor400 / directory_episodes / task_owned_cached | 0.3655, 1.0095, 0.7219 | 0.7219 [0.3655,1.0095] | [2] | 1.1214, 0.9923, 0.7659 | 0.9923 [0.7659,1.1214] | [1] |
| descriptor400 / flat_episodes / cached_1 | 0.3725, 2.6505, 0.9446 | 0.9446 [0.3725,2.6505] | [2] | 0.8327, 2.5337, 0.8523 | 0.8523 [0.8327,2.5337] | [2] |
| descriptor400 / flat_episodes / cached_2 | 0.3791, 1.2329, 0.7960 | 0.7960 [0.3791,1.2329] | [2] | 0.8746, 1.2796, 0.8039 | 0.8746 [0.8039,1.2796] | [2] |
| descriptor400 / flat_episodes / cached_after_incremental | 0.5603, 1.0402, 1.0302 | 1.0302 [0.5603,1.0402] | [2, 3] | 1.2682, 1.0156, 1.0372 | 1.0372 [1.0156,1.2682] | [1, 2, 3] |
| descriptor400 / flat_episodes / cold | 0.6227, 1.5209, 0.8858 | 0.8858 [0.6227,1.5209] | [2] | 1.1644, 1.6306, 0.8220 | 1.1644 [0.8220,1.6306] | [1, 2] |
| descriptor400 / flat_episodes / force_probe | 0.8736, 0.9761, 0.4269 | 0.8736 [0.4269,0.9761] | [] | 1.0807, 1.0020, 0.8982 | 1.0020 [0.8982,1.0807] | [1, 2] |
| descriptor400 / flat_episodes / incremental | 0.4341, 1.0098, 0.8492 | 0.8492 [0.4341,1.0098] | [2] | 1.0321, 0.9297, 0.9840 | 0.9840 [0.9297,1.0321] | [1] |
| descriptor400 / flat_episodes / task_owned_cached | 0.2567, 1.0211, 1.0108 | 1.0108 [0.2567,1.0211] | [2, 3] | 1.1984, 1.0557, 0.7898 | 1.0557 [0.7898,1.1984] | [1, 2] |
| descriptor400 / flat_movies / cached_1 | 0.5054, 3.0211, 0.7964 | 0.7964 [0.5054,3.0211] | [2] | 1.1632, 3.3180, 0.5469 | 1.1632 [0.5469,3.3180] | [1, 2] |
| descriptor400 / flat_movies / cached_2 | 0.3765, 3.3563, 0.7968 | 0.7968 [0.3765,3.3563] | [2] | 0.8250, 3.3558, 0.7530 | 0.8250 [0.7530,3.3558] | [2] |
| descriptor400 / flat_movies / cached_after_incremental | 0.2669, 1.0281, 0.7815 | 0.7815 [0.2669,1.0281] | [2] | 0.8999, 1.0510, 0.9291 | 0.9291 [0.8999,1.0510] | [2] |
| descriptor400 / flat_movies / cold | 0.7823, 1.0060, 1.0405 | 1.0060 [0.7823,1.0405] | [2, 3] | 1.1181, 1.0414, 0.6620 | 1.0414 [0.6620,1.1181] | [1, 2] |
| descriptor400 / flat_movies / force_probe | 0.9649, 0.9668, 0.9703 | 0.9668 [0.9649,0.9703] | [] | 1.1421, 1.0020, 0.8622 | 1.0020 [0.8622,1.1421] | [1, 2] |
| descriptor400 / flat_movies / incremental | 0.3336, 1.1145, 1.0080 | 1.0080 [0.3336,1.1145] | [2, 3] | 1.0502, 0.8561, 0.6965 | 0.8561 [0.6965,1.0502] | [1] |
| descriptor400 / flat_movies / task_owned_cached | 0.3042, 0.9299, 0.8859 | 0.8859 [0.3042,0.9299] | [] | 1.3218, 1.0054, 0.7512 | 1.0054 [0.7512,1.3218] | [1, 2] |
| real160 / mixed_real_probe / cold | 0.9040, 1.1681, 1.2227 | 1.1681 [0.9040,1.2227] | [2, 3] | 1.4615, 1.2584, 1.3724 | 1.3724 [1.2584,1.4615] | [1, 2, 3] |
| real160 / mixed_real_probe / force | 0.9668, 0.9927, 1.0465 | 0.9927 [0.9668,1.0465] | [3] | 0.9770, 1.0276, 1.0418 | 1.0276 [0.9770,1.0418] | [2, 3] |
| real160 / mixed_real_probe / warm | 0.5252, 1.0354, 1.0305 | 1.0305 [0.5252,1.0354] | [2, 3] | 1.0817, 0.9919, 0.8928 | 0.9919 [0.8928,1.0817] | [1] |
## Stage-one diagnostics and interpretation limits

C/B diagnostic raw SQL and transaction commands are identical in all 24 groups.
C/H additional SQL is compatible with sidecar writer protection (three statements
per independent writer, six per task-owned writer, plus the observed common
minus-two offset). New no-image items still performed an empty image transaction.
This count model is not statement-level timing attribution. Old AUTH labels cover
a subset and can omit current startup SQL; a recognized zero is not zero authority.
Formal SQL fields are null, not zero; TRACE1 count diagnostics have n=1 and are
separate from TRACE0 acceptance timing. Profilers and race runs are not pooled.

Only this measurement environment supplies timing samples; old milliseconds are
not pooled. Each ratio is paired within one of three interleaved blocks. At n=3,
p95 would equal max and is not stable-tail evidence. C/H allocation and malloc
medians are higher in every group, but process/observer work is included. Heap
snapshots are not peaks. Empty-pool wait excludes reserved-owner/mutex wait;
retirement is the same task leaving Store.active, separate from Store.Close and
whole-process/compilation windows. Child RUSAGE covers reaped-child CPU and block
I/O; the probe cohort peak is not native-process peak. Real160 item counters are
asserted by existing drivers but are not emitted JSON fields.

## Stage two: three bounded changes in the full candidate

- Skip a new item's empty image transaction only after its fresh private item ID
  and already-read absent catalog state are established by the committed primary
  transaction, or established stored.hasLocalImages absence. Candidates, rename,
  old-image deletion, invalid-image retention and full directory observation stay.
- Reuse the upstream file.Stat when attaching its descriptor; the later live
  checkSource still detects source changes.
- Use a bounded single-root route path where immutable borrowing is private and
  downstream governor validation/copy remains; escaping consumers still copy.

Internal startup authorization, cancellation, token/owner, actual source/mapping,
manual/concurrent data, deletion evidence and actual retirement are retained.
Two targeted race groups passed on this unchanged seven-path source:

| Group | Top-level PASS | Subtest PASS | Required passed | Skips |
| --- | --- | --- | --- | --- |
| input-routing | 44 | 40 | 13 | 0 |
| images-publication | 32 | 70 | 10 | 0 |
| Total | 76 | 110 | 23 | 0 |

Root independently accepted raw hashes and required names in
`optimization-v1/root-correctness-acceptance.json`, SHA-256
`0f6942f8b49805a29cf46e44590b8e519ea293703eeb41ff677f740d54aa1b00`.
Input-routing raw SHA-256 is
`e99cd8ec14364bcf33625280f02d6a75484eb625ef1d1e30c5bb6c5c83b245d7`;
images-publication raw SHA-256 is
`d64c14c720afc0f997a288f07970283e7916261e9318c69b3b028f41dcfb360e`.
These counts belong only to this stage; previous cleanup counts are historical.

The six pilots, six diagnostics and eighteen formal processes are complete.
Root independently reconstructed all 18 raw processes, 216 observations and
24 groups and matched each source's three job values against the summary.
This initial mixed result deferred source selection to the fixed ablation below. The
complete current-environment paired table follows; no samples are removed to
make its cache/incremental direction look uniformly beneficial. Unrelated
full-library/10k/build, mixed playback/transcoding capacity and Docker delivery
are not added to this selected campaign.

## Stage-two completed performance comparison

Here C is full candidate 42cb91f, B is 7e959c13, and H is 912354f. Directory,
flat-episode and flat-movie cold C/B medians are 0.8433, 0.8127 and 0.7629;
real cold is 0.8052. All cold C/B and C/H observations improve. Real warm C/B
is 0.9438 (three of three improve), while force is 0.9622 with one adverse block;
their C/H medians 1.0224 and 1.0177 still have two adverse blocks each.
Directory incremental is also slower than H in all three blocks. Flat-episode
and flat-movie cached-after-incremental and flat-movie force_probe are slower
than H in all three blocks. All four cold groups still have higher C/H malloc
counts in every block, so cold-time gains do not establish uniform resource
improvement. Other adverse blocks remain visible below.

| Phase | C/B three ratios | Median [min,max] | Reverse blocks | C/H three ratios | Median [min,max] | Reverse blocks |
| --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 1.2110, 1.0286, 1.0846 | 1.0846 [1.0286,1.2110] | [1, 2, 3] | 1.3324, 0.4259, 1.1435 | 1.1435 [0.4259,1.3324] | [1, 3] |
| descriptor400 / directory_episodes / cached_2 | 0.9426, 1.0331, 1.0070 | 1.0070 [0.9426,1.0331] | [2, 3] | 1.0384, 0.4367, 1.1386 | 1.0384 [0.4367,1.1386] | [1, 3] |
| descriptor400 / directory_episodes / cached_after_incremental | 0.9478, 0.9827, 0.9443 | 0.9478 [0.9443,0.9827] | [] | 1.0914, 0.9564, 0.9546 | 0.9564 [0.9546,1.0914] | [1] |
| descriptor400 / directory_episodes / cold | 0.8001, 0.8679, 0.8433 | 0.8433 [0.8001,0.8679] | [] | 0.8736, 0.4775, 0.8438 | 0.8438 [0.4775,0.8736] | [] |
| descriptor400 / directory_episodes / force_probe | 0.9748, 0.9737, 0.9966 | 0.9748 [0.9737,0.9966] | [] | 0.9896, 1.0060, 1.0766 | 1.0060 [0.9896,1.0766] | [2, 3] |
| descriptor400 / directory_episodes / incremental | 1.1527, 1.0719, 1.1020 | 1.1020 [1.0719,1.1527] | [1, 2, 3] | 1.1946, 1.1497, 1.1726 | 1.1726 [1.1497,1.1946] | [1, 2, 3] |
| descriptor400 / directory_episodes / task_owned_cached | 1.0097, 0.9681, 1.0026 | 1.0026 [0.9681,1.0097] | [1, 3] | 1.0872, 0.3898, 1.1178 | 1.0872 [0.3898,1.1178] | [1, 3] |
| descriptor400 / flat_episodes / cached_1 | 1.0448, 1.0096, 1.0076 | 1.0096 [1.0076,1.0448] | [1, 2, 3] | 1.0656, 0.4356, 1.0082 | 1.0082 [0.4356,1.0656] | [1, 3] |
| descriptor400 / flat_episodes / cached_2 | 0.9900, 0.9661, 1.0353 | 0.9900 [0.9661,1.0353] | [3] | 1.0113, 0.3419, 1.0334 | 1.0113 [0.3419,1.0334] | [1, 3] |
| descriptor400 / flat_episodes / cached_after_incremental | 1.0952, 1.0591, 0.9016 | 1.0591 [0.9016,1.0952] | [1, 2] | 1.0380, 1.1394, 1.0454 | 1.0454 [1.0380,1.1394] | [1, 2, 3] |
| descriptor400 / flat_episodes / cold | 0.8014, 0.8127, 0.8532 | 0.8127 [0.8014,0.8532] | [] | 0.8125, 0.4362, 0.8666 | 0.8125 [0.4362,0.8666] | [] |
| descriptor400 / flat_episodes / force_probe | 1.0381, 0.9867, 1.0233 | 1.0233 [0.9867,1.0381] | [1, 3] | 1.0322, 0.9777, 1.0548 | 1.0322 [0.9777,1.0548] | [1, 3] |
| descriptor400 / flat_episodes / incremental | 0.9842, 0.9912, 1.0194 | 0.9912 [0.9842,1.0194] | [3] | 1.0763, 0.9669, 1.0093 | 1.0093 [0.9669,1.0763] | [1, 3] |
| descriptor400 / flat_episodes / task_owned_cached | 1.0034, 1.0698, 0.9789 | 1.0034 [0.9789,1.0698] | [1, 2] | 0.9819, 0.3214, 1.0251 | 0.9819 [0.3214,1.0251] | [3] |
| descriptor400 / flat_movies / cached_1 | 0.9477, 0.9593, 0.9709 | 0.9593 [0.9477,0.9709] | [] | 0.9835, 0.3272, 1.0327 | 0.9835 [0.3272,1.0327] | [3] |
| descriptor400 / flat_movies / cached_2 | 1.0050, 0.9526, 0.9842 | 0.9842 [0.9526,1.0050] | [1] | 0.9512, 0.3425, 0.9537 | 0.9512 [0.3425,0.9537] | [] |
| descriptor400 / flat_movies / cached_after_incremental | 1.0324, 0.8239, 1.0480 | 1.0324 [0.8239,1.0480] | [1, 3] | 1.0734, 1.0167, 1.0331 | 1.0331 [1.0167,1.0734] | [1, 2, 3] |
| descriptor400 / flat_movies / cold | 0.7414, 0.7911, 0.7629 | 0.7629 [0.7414,0.7911] | [] | 0.7785, 0.8346, 0.8113 | 0.8113 [0.7785,0.8346] | [] |
| descriptor400 / flat_movies / force_probe | 1.0254, 0.9972, 1.0063 | 1.0063 [0.9972,1.0254] | [1, 3] | 1.0417, 1.0049, 1.0037 | 1.0049 [1.0037,1.0417] | [1, 2, 3] |
| descriptor400 / flat_movies / incremental | 0.9658, 1.0280, 0.9799 | 0.9799 [0.9658,1.0280] | [2] | 0.9985, 0.4617, 1.0698 | 0.9985 [0.4617,1.0698] | [3] |
| descriptor400 / flat_movies / task_owned_cached | 1.0304, 0.9954, 0.9310 | 0.9954 [0.9310,1.0304] | [1] | 1.0640, 0.3855, 0.9530 | 0.9530 [0.3855,1.0640] | [1] |
| real160 / mixed_real_probe / cold | 0.8052, 0.8127, 0.8043 | 0.8052 [0.8043,0.8127] | [] | 0.7952, 0.8681, 0.8891 | 0.8681 [0.7952,0.8891] | [] |
| real160 / mixed_real_probe / force | 0.9622, 0.9605, 1.0426 | 0.9622 [0.9605,1.0426] | [3] | 1.0177, 0.9794, 1.0379 | 1.0177 [0.9794,1.0379] | [1, 3] |
| real160 / mixed_real_probe / warm | 0.9371, 0.9438, 0.9921 | 0.9438 [0.9371,0.9921] | [] | 1.0276, 0.9417, 1.0224 | 1.0224 [0.9417,1.0276] | [1, 3] |

## Fixed A/I/F ablation: complete paired comparison and decision

Formal orders are A-I-F / I-F-A / F-A-I. All nine formal processes use the
post-adjustment /dev/sda1 reservation of 403,374 blocks (1 percent). Earlier H/B/C
and real160 timing came from their own batch before that change; it is not pooled
with ablation values. F's real160 cold/warm/force C/B medians 0.8052/0.9438/0.9622
refer only to its earlier full-candidate run above.

F/I directory and flat-episode cached_1 paired medians are 0.9489 and 0.9668,
all three blocks faster. I/A has adverse blocks in both priority groups and in
other phases. This favors F for this delivery, while retaining F/I directory
force_probe 3/3 slower, F/A movie incremental 3/3 slower and F/A directory
incremental 2/3 slower. It does not establish universal improvement, explain
previous adverse samples as noise, or isolate the Stat and route changes.
F/I malloc medians fall in all 21 groups by 0.230-0.841 percent; allocation
changes are mixed, so this is not a uniform resource improvement claim.

| Phase | F/I three ratios | Median [min,max] | Reverse blocks | I/A three ratios | Median [min,max] | Reverse blocks | F/A three ratios | Median [min,max] | Reverse blocks |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 0.8269, 0.9489, 0.9675 | 0.9489 [0.8269,0.9675] | [] | 1.1533, 1.0350, 0.8757 | 1.0350 [0.8757,1.1533] | [1, 2] | 0.9537, 0.9821, 0.8473 | 0.9537 [0.8473,0.9821] | [] |
| descriptor400 / directory_episodes / cached_2 | 0.9689, 1.0388, 0.9389 | 0.9689 [0.9389,1.0388] | [2] | 0.9574, 0.9927, 1.1302 | 0.9927 [0.9574,1.1302] | [3] | 0.9276, 1.0312, 1.0612 | 1.0312 [0.9276,1.0612] | [2, 3] |
| descriptor400 / directory_episodes / cached_after_incremental | 0.9537, 0.4101, 0.9586 | 0.9537 [0.4101,0.9586] | [] | 1.1344, 2.2873, 0.4619 | 1.1344 [0.4619,2.2873] | [1, 2] | 1.0819, 0.9381, 0.4428 | 0.9381 [0.4428,1.0819] | [1] |
| descriptor400 / directory_episodes / cold | 1.0452, 1.0261, 0.9577 | 1.0261 [0.9577,1.0452] | [1, 2] | 0.8494, 0.8357, 0.8454 | 0.8454 [0.8357,0.8494] | [] | 0.8878, 0.8576, 0.8096 | 0.8576 [0.8096,0.8878] | [] |
| descriptor400 / directory_episodes / force_probe | 1.0423, 1.0589, 1.0386 | 1.0423 [1.0386,1.0589] | [1, 2, 3] | 1.0244, 0.9237, 0.5204 | 0.9237 [0.5204,1.0244] | [1] | 1.0678, 0.9781, 0.5405 | 0.9781 [0.5405,1.0678] | [1] |
| descriptor400 / directory_episodes / incremental | 0.9685, 0.3927, 1.0090 | 0.9685 [0.3927,1.0090] | [3] | 1.0433, 2.7273, 0.2619 | 1.0433 [0.2619,2.7273] | [1, 2] | 1.0104, 1.0709, 0.2643 | 1.0104 [0.2643,1.0709] | [1, 2] |
| descriptor400 / directory_episodes / task_owned_cached | 0.9691, 0.3826, 1.0236 | 0.9691 [0.3826,1.0236] | [3] | 0.9810, 2.6858, 1.0056 | 1.0056 [0.9810,2.6858] | [2, 3] | 0.9507, 1.0275, 1.0294 | 1.0275 [0.9507,1.0294] | [2, 3] |
| descriptor400 / flat_episodes / cached_1 | 0.9781, 0.9457, 0.9668 | 0.9668 [0.9457,0.9781] | [] | 1.0176, 1.0544, 0.8607 | 1.0176 [0.8607,1.0544] | [1, 2] | 0.9953, 0.9972, 0.8321 | 0.9953 [0.8321,0.9972] | [] |
| descriptor400 / flat_episodes / cached_2 | 1.0064, 1.1019, 0.8871 | 1.0064 [0.8871,1.1019] | [1, 2] | 1.0233, 0.9643, 1.1006 | 1.0233 [0.9643,1.1006] | [1, 3] | 1.0298, 1.0626, 0.9764 | 1.0298 [0.9764,1.0626] | [1, 2] |
| descriptor400 / flat_episodes / cached_after_incremental | 1.0876, 0.3077, 1.0799 | 1.0799 [0.3077,1.0876] | [1, 3] | 1.0115, 3.0852, 0.2780 | 1.0115 [0.2780,3.0852] | [1, 2] | 1.1001, 0.9494, 0.3003 | 0.9494 [0.3003,1.1001] | [1] |
| descriptor400 / flat_episodes / cold | 1.0023, 1.0220, 0.6388 | 1.0023 [0.6388,1.0220] | [1, 2] | 0.7773, 0.7707, 1.3057 | 0.7773 [0.7707,1.3057] | [3] | 0.7791, 0.7876, 0.8341 | 0.7876 [0.7791,0.8341] | [] |
| descriptor400 / flat_episodes / force_probe | 0.9698, 1.0053, 0.9832 | 0.9832 [0.9698,1.0053] | [2] | 1.0502, 0.9766, 0.5555 | 0.9766 [0.5555,1.0502] | [1] | 1.0185, 0.9818, 0.5462 | 0.9818 [0.5462,1.0185] | [1] |
| descriptor400 / flat_episodes / incremental | 1.0231, 0.4165, 0.9760 | 0.9760 [0.4165,1.0231] | [1] | 0.9568, 2.4610, 0.8637 | 0.9568 [0.8637,2.4610] | [2] | 0.9789, 1.0249, 0.8429 | 0.9789 [0.8429,1.0249] | [2] |
| descriptor400 / flat_episodes / task_owned_cached | 1.0494, 0.4418, 0.8475 | 0.8475 [0.4418,1.0494] | [1] | 0.9992, 2.3459, 1.1407 | 1.1407 [0.9992,2.3459] | [2, 3] | 1.0486, 1.0365, 0.9668 | 1.0365 [0.9668,1.0486] | [1, 2] |
| descriptor400 / flat_movies / cached_1 | 1.0253, 1.0291, 0.9914 | 1.0253 [0.9914,1.0291] | [1, 2] | 0.9198, 0.9676, 0.9174 | 0.9198 [0.9174,0.9676] | [] | 0.9430, 0.9958, 0.9095 | 0.9430 [0.9095,0.9958] | [] |
| descriptor400 / flat_movies / cached_2 | 0.9661, 1.1338, 1.0013 | 1.0013 [0.9661,1.1338] | [2, 3] | 0.8644, 0.8868, 0.9367 | 0.8868 [0.8644,0.9367] | [] | 0.8352, 1.0054, 0.9379 | 0.9379 [0.8352,1.0054] | [2] |
| descriptor400 / flat_movies / cached_after_incremental | 1.1192, 0.2632, 1.0010 | 1.0010 [0.2632,1.1192] | [1, 3] | 0.8859, 3.7659, 0.1928 | 0.8859 [0.1928,3.7659] | [2] | 0.9915, 0.9912, 0.1930 | 0.9912 [0.1930,0.9915] | [] |
| descriptor400 / flat_movies / cold | 0.9426, 1.0579, 0.6123 | 0.9426 [0.6123,1.0579] | [2] | 0.7896, 0.7939, 1.3967 | 0.7939 [0.7896,1.3967] | [3] | 0.7443, 0.8399, 0.8552 | 0.8399 [0.7443,0.8552] | [] |
| descriptor400 / flat_movies / force_probe | 0.9898, 0.7403, 1.0126 | 0.9898 [0.7403,1.0126] | [3] | 1.0039, 1.3274, 0.6074 | 1.0039 [0.6074,1.3274] | [1, 2] | 0.9936, 0.9827, 0.6150 | 0.9827 [0.6150,0.9936] | [] |
| descriptor400 / flat_movies / incremental | 1.0269, 0.4018, 1.0071 | 1.0071 [0.4018,1.0269] | [1, 3] | 1.0240, 2.5844, 1.0245 | 1.0245 [1.0240,2.5844] | [1, 2, 3] | 1.0516, 1.0385, 1.0318 | 1.0385 [1.0318,1.0516] | [1, 2, 3] |
| descriptor400 / flat_movies / task_owned_cached | 0.8932, 0.9491, 0.9937 | 0.9491 [0.8932,0.9937] | [] | 1.1193, 1.0255, 1.0064 | 1.0255 [1.0064,1.1193] | [1, 2, 3] | 0.9998, 0.9733, 1.0000 | 0.9998 [0.9733,1.0000] | [] |

## Capacity recovery and diagnostic provenance

I's images-publication Go race passed 32 top-level/70 subtests and all 10 required,
then its post-race capacity wrapper exited 1. A narrow shared Go build-cache
clean restored about 1 GB; the three pilots passed qualified. Pilots are excluded
from formal timing. Diagnostic A's raw contains PASS events, but fixtureAfter
capacity checking raised an error before the old runner saved a per-run receipt.
The original raw and capacity-stop annotation remain retained, not rerun or
replaced. A is recovered counts-only with qualified=false: command, effective
environment, process identity, before/after capacity, source/protected checks,
exitCode and start/completion metadata remain null/unknown. A's timing,
allocation and pool values are excluded; its configured source is not a persisted
run identity. Only I/F diagnostics are strictly qualified, not all three.

The user approved /dev/sda1 ext4 reserved blocks 1,620,977 to 403,374
(4.02 percent to 1 percent), releasing an expected 4,987,301,888 available bytes
while retaining 1,652,219,904 reserve bytes. Applied readback was 403,374 reserved
blocks and 5,201,367,040 available bytes. No data or old environment was deleted;
the 1 percent reserve stays at closure. Adjustment receipt SHA-256 is
`1790be6ad951514002987e57f2172d7ce70df160e7b30397f345dfa911a28879`.
Following recovery, I/F diagnostics and all nine strict formal processes passed.
All 15 saved process identities exited, source/protected identities stayed
unchanged, and final execution space was about 5.19 GB. Final execution receipt
SHA-256 is `4eafde3ef034e85352ebc7cfea8943337a986abdc63ccb1ef2c0168ce767e7cd`.
Owned temporary resources are closed. The five RAM source trees, RAM archives
and task-private cache are gone; empty scratch/fixture directories were removed.
Persistent raw evidence and all five local source archives remain hash-matched.
Shared PostgreSQL, Docker and old -02 are unchanged, and the 1 percent reserve
remains 403,374 blocks. Readback was 5,173,850,112 available persistent bytes and
6,262,153,216 available RAM bytes. Closure receipt SHA-256 is
`55b0497ae1483ff2444735ffeec1b1f8bea6165a01bf892938a2c4d625dd0c6d`.

## Limits and retained evidence

Each contrast contains three same-block job_elapsed_ns ratios, defined from
persisted StartedAt/FinishedAt and excluding observer terminal polling. All
samples, ranges and adverse blocks remain. At n=3, max is not stable p95 evidence.
F/I tests the combined Stat/route changes conditional on the common image change;
it isolates neither individually. No scheduler, filesystem or CPU cause is
inferred from time, allocation, GC or pool counters. Allocation/GC/pool values
include observer/runtime work; heaps are snapshots, not peaks. Empty-pool wait
excludes reserved-owner/mutex waiting. Same-task retirement, Store.Close and
whole-process/compilation windows remain separate. Child RUSAGE/probe cohorts do
not establish source-read bytes or native-process peak. Real160 un-emitted item
counters are not invented.

Formal SQL fields are null, not zero. TRACE1 n=1 diagnostics support counts only;
A also precedes the reserve adjustment and retains its metadata gap. Old AUTH
classification is partial, and explicit BEGIN/COMMIT excludes implicit
transactions. Race, profiling, pilots and old milliseconds are not pooled with
formal evidence. The existing descriptor400/real160 version-2 contract and four
drivers remain unchanged. No unrelated full-library/10k/build, mixed playback/
transcoding capacity, Docker or WIP integration campaign is added.

Full raw and structured evidence stay outside the repository at
`D:/Code/goby/.artifacts/scan-performance-followup-20261005`. Only this report and
complete historical handoff are published; no large JSON is duplicated.

| Evidence | SHA-256 |
| --- | --- |
| Stage-one analysis/report | 25171227a837b063735ab099a4784431ae7a271bdea96b37745b1fdcffe047f2 |
| Stage-one analysis/summary | 28318826b00d77c92f70298a251eb7b742946f96de0bdd61dc225d4b801c9130 |
| Stage-one qualification | ec8e5134fdee01cd242638d0eae019f5cb052182a968cc562bd98a2db12e153b |
| Stage-one formal observations | 6b2d6855df7685eda40f1e9c02ee293692d788c7ee7ea16f9c6d035f2e29d157 |
| optimization-v1/analysis/report | 0e301cf15384cc0a26f5eb4afda2d44aa40c8a490046af8259b0c98d617f4438 |
| optimization-v1/analysis/summary | c7192d33ef8a6f3cea230c1dd06bb00eea85eae419032dc747b3fe59fbe2f934 |
| optimization-v1 root-paired-check | 5973d567d62713db42160b069dac8c479c3de3b9d6a2ea06c645eaf97e260356 |
| ablation-v1/analysis/report | 9f5722233fa5768baff2807c2663c8976614d8874452d4f5c355020f3919aecc |
| ablation-v1/analysis/summary | dc41bf6b99583cb0b6b3c171865c82beccd86774bf1fb7d30e34c383e1e6ddbb |
| ablation-v1 root-paired-check | 6fa2e32a917ea543aaee4b837bab9939f64b2c401ae0d9822c9c6953836d70de |
| Final source acceptance | 8c833eefb17c30fe1e9433e2c90c85738f75371e7217d5a35356a349f319ea3d |
| Owned temporary closure | 55b0497ae1483ff2444735ffeec1b1f8bea6165a01bf892938a2c4d625dd0c6d |

The external publication receipt records subsequent exact main/origin confirmation,
ordinary push and preservation of 199 original dirty byte states plus 56 prior
WIP identities. Source and documentation commit identities are kept separate.
