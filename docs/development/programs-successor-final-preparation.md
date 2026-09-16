# Programs successor final verification preparation

A fresh full verification was started on September 16 after the
[original theme](library-theme-diagnostic-result.md) and
[complete Library package](library-package-diagnostic-result.md) each passed
once with resource closure. It has since [stopped at the worker memory limit](programs-successor-memory-limit-result.md),
with 16 complete passing packages, neither build executed and all owned resources
closed. The preparation and active-state observations below are historical.
Do not replay its input.

The new scope is `/opt/goby-test/livetv-programs-successor-final-20260916`.
At 05:14:42 UTC, its unit
`goby-livetv-programs-successor-final-20260916-e3fa16b52963.service` was active
with MainPID 1338290, actual MemoryMax 2147483648 and MemorySwapMax 0. Its worker
scope is `ram/livetv-programs-successor-final-20260916-20260916T051331Z-e3fa16b52963`
beneath that root. Unit names and PIDs are observations, not reusable authority.

The existing final adapter retains one complete 25-package race-enabled run,
the ordinary Go build, one embedded amd64 systemd build, result readback,
archiving, resource closure and artifact materialization. It does not run npm
or rebuild the unchanged frontend. The worker hard limit is tightened to 2 GiB,
with zero swap; tmpfs remains 3 GiB and both admission stages require 5 GiB
available memory. Original test, command, runtime, retention, root-reserve and
package-build budgets are retained.

The compact source has 924 files and 13254936 uncompressed bytes. Its 867 tracked
inputs match commit `3d6b79b36b6a5050e174f7a152f214b9356608c8`; compared with the
previous frozen compact source, only the two verified diagnostic test files
changed. Production and packaging code did not change. The 57 prebuilt frontend
assets retain all names, sizes and hashes from the verified E11 manifest
`851cf3b02372fb99b42f75f4548556ca3eb693ddf3290055e2363c16fb91f43a`.

All following paths are beneath the new scope:

| Input | Bytes | SHA-256 |
| --- | ---: | --- |
| `private/source.tar.gz` | 2716437 | `3232a968559fb237f706175b7a652fb13478f8190a520fbaaa287048e254886a` |
| `private/source-manifest.json` | 158512 | `967cf0a98a571bd6effe31419e9459bcf0d130566da7225f1bf434725f0079bd` |
| `private/source-checkpoint.json` | 170007 | `473f90a046d23b17e33cf2fd7a07d4818ea133f61a126fa4e697c7404aa7c1d8` |
| `private/source-preparation.json` | 995 | `9d4ecc5c7924feea045bf0387b89a1e495eb5523f68b4ea11a288a4e7b569e71` |
| `private/verify-livetv-programs-final.py` | 40476 | `e2d7b843f849c120cec6899d1a8423aa59a096c6f54ff593a6f35d5e1d215c0a` |
| `private/input.json` | 4992 | `9e0ab4966401d49b9f6ad7ecb922fb8ed9d928a5c07e2dde82e727b0eb3c291f` |
| `preflight.json` | 14966 | `1de3b970d8f9a4bcb98d142cfa8252cd5730b86621880fe9038d6ef1ba47e870` |

Independent static/byte review confirmed the source comparison, frontend reuse,
unchanged full/build/closure sequence and new resource bounds. Preflight passed
with 5838282752 bytes of available memory and 5398122496 available root bytes.
The guard explicitly retains the two failed candidate invocations and all
existing PG/file/configuration/reference protection. Its scope includes final
regression and building only; it grants no existing-service restart, application
database access, ready-runtime or client-transition authority.

Accept a final result only after the actual 25-package result, both builds,
artifact identities and complete resource closure have been reviewed. A passing
Library package or active worker alone does not satisfy this gate. The previous
failed full execution and both historical recovery scopes remain unchanged.
