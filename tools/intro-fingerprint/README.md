# Goby intro fingerprint helper

This isolated helper consumes bounded native-rate PCM on stdin and returns
Chromaprint raw 32-bit fingerprints. It is independent of FFmpeg's Chromaprint
integration and of `fpcalc`. It does not decode media. See [PROTOCOL.md](PROTOCOL.md)
for the exact request, response, time coordinates, and limitations.
The descriptor-based Go audio, visual and preview extraction contract is in
[EXTRACTION.md](EXTRACTION.md).
The independently observed rotation/SAR display profile is in
[GEOMETRY.md](GEOMETRY.md).

## Pinned source and build recipe

The vendored, unmodified source subset comes from the official Chromaprint
`v1.6.1` annotated tag (`e88b7414e7d32dc9466b7022ea675786a8a4bc15`), resolving to
commit `aed8eba2202dd9d7b3b0a56c77904cc805490d72`. The tag is not signed. The
retrieved source archive is:

```text
https://codeload.github.com/acoustid/chromaprint/tar.gz/aed8eba2202dd9d7b3b0a56c77904cc805490d72
SHA-256: eba1536d49daa17ae3c56904ea004342c42135dfdaabd7e9c5decbdb473d95ca
```

[vendor/SHA256SUMS](vendor/SHA256SUMS) records each retained upstream file's
exact bytes. CMake checks these entries before compiling. The subset retains
the original relative paths and licensing notices; it excludes upstream
`avresample`, command-line tools, audio decoding backends, optional FFT backends,
upstream tests, and GoogleTest. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)
for the file-level licensing basis, including the distinction from the complete
upstream distribution's LGPL notice.

This dedicated CMake project uses an explicit static source list with
`USE_KISSFFT=1`, `USE_INTERNAL_AVRESAMPLE=0`, and no optional backend defines.
It does not execute upstream CMake discovery. Upstream's corresponding choices
are `BUILD_SHARED_LIBS=OFF`, `BUILD_TOOLS=OFF`, `BUILD_TESTS=OFF`,
`USE_INTERNAL_AVRESAMPLE=OFF`, and `FFT_LIB=kissfft`; simply passing those choices
to upstream is insufficient to prevent its unconditional FFmpeg discovery.
The dedicated recipe avoids that discovery entirely. The actual C++
`AudioProcessor::Reset` implementation rejects a non-native rate with internal
resampling disabled, and the helper independently requires 11,025 Hz mono.

Required tools are a C99/C++14 compiler and CMake 3.16 or newer. There is no
configure-time network access or package download. On the designated Linux
verification/build host, from the repository root:

```sh
cmake -S tools/intro-fingerprint -B /tmp/goby-intro-fingerprint-build -DCMAKE_BUILD_TYPE=Release
cmake --build /tmp/goby-intro-fingerprint-build --target goby-intro-fingerprint --parallel 2
python3 tools/intro-fingerprint/tests/test_protocol.py --helper /tmp/goby-intro-fingerprint-build/goby-intro-fingerprint
cmake --install /tmp/goby-intro-fingerprint-build --prefix /opt/goby-intro-fingerprint
```

These are instructions for a separately authorized verification phase, not a
claim that they have been run. The initial source delivery has not been built
or executed locally or remotely. Record compiler, CMake, target architecture,
build flags, executable SHA-256, and test output for a released build. The source
pin establishes reproducible inputs; it does not promise bit-identical binaries
across different toolchains. Keep the installed license directory and notices
with a distributed binary.

The target links the selected Chromaprint and KissFFT code statically. Its
remaining platform runtime dependencies are the C/C++ runtimes and, on Unix,
the math library. It does not require `libav*`, `libswresample`, FFTW, a system
Chromaprint shared library, or an FFmpeg executable. The server should invoke
the installed absolute path with fixed arguments, isolated environment,
bounded stdout/stderr, and its process-group timeout/resource controls.

## Source refresh procedure

A version change is a protocol/profile review, not an automatic dependency
upgrade. Download the official archive to a private staging directory, verify
its pinned digest, and inspect the tag-to-commit resolution. Copy only the
manifest-listed paths from that revision, preserving their bytes. Review each
file's license notice and re-create the per-file SHA-256 manifest. Update the
explicit CMake sources, compile-time version checks, protocol revision, API
metadata expectations, timing derivation, and tests together. Do not enable an
audio converter to make an incompatible input rate silently work.

## Coverage awaiting execution

The Python standard-library contract tests drive only this helper. They cover
description without EOF, exact metadata, argument rejection, empty/unaligned
input, first-window sample precision, lack of silence trimming, equivalent
chunking, raw unsigned ranges, the 600-second boundary and its sentinel, and
whole-stride PCM offsets mapped to corresponding raw indexes. They create PCM
in memory and require no media decoder, audio fixtures, network, or extra Python
packages. A passing result must be collected on the designated verification
host before the helper is marked ready.
