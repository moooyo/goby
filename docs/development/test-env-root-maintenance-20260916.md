# Selected cleanup and additional root expansion

The user selected the KISS directories directly under `/root/work` and four
specific BiliComics temporary directories for deletion, then added 25 GiB to the
virtual disk and requested that execution resume after maintenance.

All 134 selected KISS directories and four BiliComics directories were removed.
The 319 unselected top-level entries retained their identities, including the
Android NDK and 51 non-KISS project directories. Loose KISS files directly under
`/root/work` were outside the directory selection. The observed available-space
increase was 17949364224 bytes, approximately 16.72 GiB.

The virtual disk grew from 97 to 122 GiB. `growpart /dev/sda 1` and
`resize2fs /dev/sda1` brought the new capacity into the mounted root filesystem
online. The root partition start/UUID, GPT identity, boot partitions, filesystem
UUID, root directory identity, boot and six protected process identities were
preserved. No service action, SQL, HTTP or product test was performed.

| Observation | Bytes |
| --- | ---: |
| Root available after selected cleanup | 21820792832 |
| Expanded virtual disk | 130996502528 |
| Expanded root partition | 130862267904 |
| Expanded filesystem capacity | 128719773696 |
| Root available after growth | 47169531904 |
| Root available at independent review | 47169355776 |

The independent review confirmed all 138 target paths absent, retained entries
unchanged, the saved command results consistent and current partition/filesystem
geometry correct. `df` reported approximately 120G total, 72G used and 44G
available, at 62% usage. The [checkpoint](test-env-root-maintenance-20260916.json)
pins the private selection, cleanup, growth and independent-review records.

The earlier [capacity review and cache cleanup](test-env-root-cleanup-20260916.md)
remain historical. Directory totals with hard links are not additive release
guarantees, and current free space is not a reservation. Resume the existing
serialized verification queue with its normal runtime and capacity admission.

The later [four-directory temporary cleanup](test-env-tmp-memory-cleanup-20260916.md)
has also completed under a separate user selection. It concerns PowerToys agentic
review-related paths on `/tmp`, with no Goby ownership claim. The observed tmpfs
reduction was 1,436,913,664 bytes (about 1.34 GiB); the after-sample reported
5,829,570,560 available memory bytes. Concurrent workload effects prevent exact
attribution of the memory-availability increase. The original self-`O_PATH`
false-positive abort and all unselected directories remain preserved.
