# Third-party source and license notices

The files under `vendor/chromaprint/` are an unmodified subset of Chromaprint
1.6.1, commit `aed8eba2202dd9d7b3b0a56c77904cc805490d72`. Every included upstream
file and its SHA-256 are listed in `vendor/SHA256SUMS`: 59 MIT source/header
files, six BSD-3-Clause source/header files, and three original license files.
Paths in this notice are
relative to that upstream root; no absent component is required at runtime.

## Chromaprint's own source files

The retained `src/*.h`, `src/*.cpp`, `src/audio/audio_slicer.h`, and
`src/utils/*.h` / `src/utils/base64.cpp` each carry an explicit MIT license
header naming Lukas Lalinsky. This includes the `audio_processor` implementation
and the `fft_lib_kissfft` adapter; the former's optional FFmpeg resampler calls
are excluded by `USE_INTERNAL_AVRESAMPLE=0`. Their bytes and individual headers
are preserved. The full MIT permission, conditions, disclaimer, and copyright
are retained in [the original upstream license file](vendor/chromaprint/LICENSE.md).

This is a statement about the actual selected compilation inputs, not a
relicensing of the complete Chromaprint distribution. Upstream `LICENSE.md`
explicitly describes that distribution as LGPL 2.1 because it also includes
FFmpeg-derived code. This source subset neither copies nor compiles
`src/avresample/`, including its LGPL `resample2.c` and `avcodec.h`. It also
excludes the external FFmpeg audio-conversion and FFT backends. No LGPL/GPL
library is selected or linked by the dedicated CMake target. The unchanged
upstream combined-license explanation remains present for accurate provenance.

## Bundled KissFFT subset

These six source/header files are retained from Chromaprint's bundled KissFFT:

- `src/3rdparty/kissfft/kiss_fft.c`
- `src/3rdparty/kissfft/kiss_fftr.c`
- `src/3rdparty/kissfft/kiss_fft.h`
- `src/3rdparty/kissfft/kiss_fftr.h`
- `src/3rdparty/kissfft/_kiss_fft_guts.h`
- `src/3rdparty/kissfft/kiss_fft_log.h`

Each names Mark Borgerding, retains its original copyright notice, and carries
`SPDX-License-Identifier: BSD-3-Clause`. The original
[COPYING](vendor/chromaprint/src/3rdparty/kissfft/COPYING) and complete
[BSD-3-Clause text](vendor/chromaprint/src/3rdparty/kissfft/LICENSES/BSD-3-Clause)
are included. Only `kiss_fft.c` and `kiss_fftr.c` are compiled. Their default
floating-point scalar implementation is selected; fixed-point, SIMD, tools,
tests, and optional external backends are not enabled.

## Distribution

Retain the MIT and BSD copyright, permission/conditions, and disclaimer texts
with source distributions and the installed notices accompanying binaries.
The helper's install target copies these original license documents. Neither
Chromaprint, KissFFT, nor their authors endorse Goby. Platform toolchain/runtime
licenses remain the responsibility of the distributor of that platform build.
