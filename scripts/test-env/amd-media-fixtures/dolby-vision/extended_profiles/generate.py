#!/usr/bin/env python3
"""Author compact original DV charts and independent scalar PQ color references."""

from __future__ import annotations

import argparse
import array
import hashlib
import importlib.util
import json
import math
from pathlib import Path
import shutil
import subprocess
import sys

WIDTH, HEIGHT, FPS = 320, 180, 24
REVISION = "614c816b6446dcd1dbaf433403d499a6026fbb5a"
LMS_TO_BT2020 = ((3.06441879, -2.16597676, 0.10155818),
                (-0.65612108, 1.78554118, -0.12943749),
                (0.01736321, -0.04725154, 1.03004253))
SWATCHES = ((0, 0, 0), (0.1, 0.1, 0.1), (10, 10, 10), (100, 100, 100),
            (90, 15, 15), (15, 90, 15), (15, 15, 90), (80, 80, 15),
            (15, 80, 80), (80, 15, 80), (65, 35, 20), (20, 40, 70),
            (250, 250, 250), (500, 500, 500), (35, 70, 45), (70, 45, 20))


def matrix(a, b):
    return [[sum(x * b[k][j] for k, x in enumerate(row)) for j in range(3)] for row in a]


def multiply(a, v):
    return [sum(x * y for x, y in zip(row, v, strict=True)) for row in a]


def inverse(m):
    (a, b, c), (d, e, f), (g, h, i) = m
    det = a * (e * i - f * h) - b * (d * i - f * g) + c * (d * h - e * g)
    if abs(det) < 1e-12:
        raise ValueError("Singular color matrix")
    return [[v / det for v in row] for row in ((e*i-f*h, c*h-b*i, b*f-c*e),
                                              (f*g-d*i, a*i-c*g, c*d-a*f),
                                              (d*h-e*g, b*g-a*h, a*e-b*d))]


def pq(value):
    value = max(0.0, value) ** (2610 / 16384)
    return ((3424 / 4096 + 2413 / 128 * value) / (1 + 2392 / 128 * value)) ** (2523 / 32)


def unpq(value):
    value = max(0.0, value) ** (32 / 2523)
    return (max(value - 3424 / 4096, 0) / max(2413 / 128 - 2392 / 128 * value, 1e-15)) ** (16384 / 2610)


def hlg(value):
    a, b, c = 0.17883277, 0.28466892, 0.55991073
    return math.sqrt(3 * value) if value <= 1 / 12 else a * math.log(12 * value - b) + c


def bt709(value):
    return 4.5 * value if value < 0.018 else 1.099 * value ** 0.45 - 0.099


def matrices(rpu):
    dm = rpu["vdr_dm_data"]
    nonlinear = [[dm[f"ycc_to_rgb_coef{i*3+j}"] / 8192 for j in range(3)] for i in range(3)]
    linear = [[dm[f"rgb_to_lms_coef{i*3+j}"] / 16384 for j in range(3)] for i in range(3)]
    offsets = [dm[f"ycc_to_rgb_offset{i}"] / (1 << 28) * 1024 / 1023 for i in range(3)]
    return nonlinear, matrix(LMS_TO_BT2020, linear), offsets


def source_codes(profile, rgb, rpu):
    if profile == "5":
        # Invert the real P5 IPTPQc2 matrices before encoding. Ordinary YCbCr
        # pixels with an edited profile tag would not recover these RGB values.
        nonlinear, linear, offsets = matrices(rpu)
        lms = multiply(inverse(linear), [value / 10000 for value in rgb])
        codes = [value + offset for value, offset in zip(
            multiply(inverse(nonlinear), [pq(value) for value in lms]), offsets, strict=True)]
        if any(value < 0 or value > 1 for value in codes):
            raise ValueError("Authored P5 color is outside the encoded signal range")
        return [round(value * 1023) for value in codes]
    if profile == "8.4":
        # Scene-linear HLG values use a 1000 nit nominal scale. This is actual
        # HLG OETF authoring, not a PQ or SDR chart with HLG VUI labels.
        signal = [hlg(value / 1000) for value in rgb]
        kr, kb = 0.2627, 0.0593
    else:
        # Original SDR display content is authored in BT.709. The deliberate
        # RPU grade is defined separately and evaluated independently below.
        signal = [bt709(min(value, 100) / 100) for value in rgb]
        kr, kb = 0.2126, 0.0722
    y = kr * signal[0] + (1 - kr - kb) * signal[1] + kb * signal[2]
    return [round(64 + 876 * y), round(512 + 896 * (signal[2] - y) / (2 * (1-kb))),
            round(512 + 896 * (signal[0] - y) / (2 * (1-kr)))]


def reshape(sig, rpu):
    result = []
    denominator = 1 << rpu["header"]["coefficient_log2_denom"]
    for component, curve in enumerate(rpu["rpu_data_mapping"]["curves"]):
        pivots, total = [], 0
        for delta in curve["pivots"]:
            total += delta
            pivots.append(total / 1023)
        segment = min(sum(sig[component] >= value for value in pivots[1:-1]), len(pivots)-2)
        if curve["mapping_idc"] == "Polynomial":
            p = curve
            coefficients = [a + b / denominator for a, b in zip(
                p["poly_coef_int"][segment], p["poly_coef"][segment], strict=True)]
            value = sum(coefficient * sig[component] ** exponent for exponent, coefficient in enumerate(coefficients))
        else:
            m = curve
            value = m["mmr_constant_int"][segment] + m["mmr_constant"][segment] / denominator
            x, y, z = sig
            terms = (x, y, z, x*y, x*z, y*z, x*y*z)
            for order in range(m["mmr_order_minus1"][segment] + 1):
                coefficients = [a + b / denominator for a, b in zip(
                    m["mmr_coef_int"][segment][order], m["mmr_coef"][segment][order], strict=True)]
                value += sum(coefficient * term ** (order+1) for coefficient, term in zip(coefficients, terms, strict=True))
        result.append(min(max(value, pivots[0]), pivots[-1]))
    return result


def oracle(codes, rpu):
    nonlinear, linear, offsets = matrices(rpu)
    signal = reshape([code / 1023 for code in codes], rpu)
    lms_pq = multiply(nonlinear, [value - offset for value, offset in zip(signal, offsets, strict=True)])
    rgb = multiply(linear, [unpq(value) for value in lms_pq])
    return [pq(value) for value in rgb]


def write_chart(path, swatches, floating=False):
    # Two one-second states keep the product's motion gate meaningful without
    # allocating 28 seconds of raw frames. The bottom 12 rows alternate between
    # dark and bright; all sixteen center ROIs remain untouched.
    with path.open("wb") as stream:
        for phase in range(2):
            planes = [[], [], []]
            for y in range(HEIGHT):
                for x in range(WIDTH):
                    index = min(3, y * 4 // HEIGHT) * 4 + min(3, x * 4 // WIDTH)
                    value = swatches[(0 if phase == 0 else 13) if y >= HEIGHT-12 else index]
                    for component in range(3):
                        planes[component].append(value[component])
            if floating:
                values = planes[1] + planes[2] + planes[0]
                data = array.array("f", values)
            else:
                values = planes[0]
                for component in (1, 2):
                    values += [planes[component][y*WIDTH+x] for y in range(0, HEIGHT, 2) for x in range(0, WIDTH, 2)]
                data = array.array("H", values)
            if sys.byteorder != "little":
                data.byteswap()
            stream.write(data.tobytes())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--ffmpeg", required=True)
    parser.add_argument("--ffprobe", required=True)
    parser.add_argument("--cargo", default="cargo")
    parser.add_argument("--rpu-generator", type=Path, help="Use an already built pinned generator")
    parser.add_argument("--seconds", type=int, default=28, choices=range(2, 61))
    parser.add_argument("--profiles", nargs="+", choices=("5", "8.4", "8.2"), default=["5", "8.4", "8.2"])
    args = parser.parse_args()
    if sys.platform != "linux":
        parser.error("Fixture generation is restricted to an authorized Linux worker")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    source = Path(__file__).resolve().parent
    spec = importlib.util.spec_from_file_location("baseline", source.parent / "generate.py")
    baseline = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(baseline)
    commands = []

    def run(argv, filename):
        result = subprocess.run([str(arg) for arg in argv], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        (output / filename).write_bytes(result.stdout + result.stderr)
        commands.append({"argv": [str(arg) for arg in argv], "exit_code": result.returncode, "log": filename})
        (output / "commands.json").write_text(json.dumps(commands, indent=2) + "\n")
        result.check_returncode()
        return result.stdout

    for tool, name in ((args.ffmpeg, "ffmpeg"), (args.ffprobe, "ffprobe")):
        run([tool, "-version"], f"{name}-version.txt")
    generator = args.rpu_generator
    if generator is None:
        build = output / "rpu-generator"
        build.mkdir()
        shutil.copy2(source / "Cargo.toml", build / "Cargo.toml")
        shutil.copytree(source / "src", build / "src")
        run([args.cargo, "build", "--release", "--manifest-path", build / "Cargo.toml"], "cargo-build.txt")
        generator = build / "target/release/goby-extended-dovi-rpu"
    manifest = {"origin": "Original procedural color charts; no audiovisual downloads", "rpu_source_revision": REVISION,
                "seconds": args.seconds, "width": WIDTH, "height": HEIGHT, "fps": FPS,
                "oracle": "Independent scalar RPU evaluation into BT.2020 PQ; no FFmpeg/libplacebo decoding used by the oracle",
                "status": "Fixture generation only; product rendering acceptance is a separate gate", "profiles": {}}
    frames = FPS * args.seconds
    for profile in args.profiles:
        label = "profile" + profile.replace(".", "")
        directory = output / label
        directory.mkdir()
        config = json.loads((source.parent / "profile81.json").read_text())
        config.update(profile=profile if profile != "8.2" else "8.1", length=frames)
        config["shots"] = [{"start": 0, "duration": frames, "metadata_blocks": []}]
        config_path = directory / "config.json"
        config_path.write_text(json.dumps(config, indent=2) + "\n")
        run([generator, profile, config_path, directory], f"{label}-rpu.txt")
        rpu = json.loads((directory / "first-rpu.json").read_text())
        codes = [source_codes(profile, rgb, rpu) for rgb in SWATCHES]
        expected = [oracle(code, rpu) for code in codes]
        raw = directory / "source-frame.yuv"
        reference = directory / "oracle-frame.gbrpf32le"
        write_chart(raw, codes)
        write_chart(reference, expected, floating=True)
        swatches = [{"index": index, "source_rgb_nits": rgb, "source_codes": code, "expected_bt2020_pq_rgb": expect,
                     "center_x": (index % 4 + .5) * WIDTH / 4, "center_y": (index // 4 + .5) * HEIGHT / 4}
                    for index, (rgb, code, expect) in enumerate(zip(SWATCHES, codes, expected, strict=True))]
        (directory / "oracle-swatches.json").write_text(json.dumps(swatches, indent=2) + "\n")
        if profile == "5":
            color_range, primaries, transfer, colorspace = "pc", "unknown", "unknown", "unknown"
        elif profile == "8.4":
            color_range, primaries, transfer, colorspace = "tv", "bt2020", "arib-std-b67", "bt2020nc"
        else:
            color_range, primaries, transfer, colorspace = "tv", "bt709", "bt709", "bt709"
        colors = ["-color_range", color_range, "-color_primaries", primaries, "-color_trc", transfer, "-colorspace", colorspace]
        signal = f"setparams=range={'full' if color_range == 'pc' else 'limited'}:color_primaries={primaries}:color_trc={transfer}:colorspace={colorspace}"
        base = directory / "base.hevc"
        run([args.ffmpeg, "-hide_banner", "-nostdin", "-v", "info", "-stream_loop", "-1", "-f", "rawvideo",
             "-pixel_format", "yuv420p10le", "-video_size", f"{WIDTH}x{HEIGHT}", "-framerate", "1",
             *colors, "-i", raw, "-frames:v", str(frames), "-vf", "fps=24," + signal, "-an", "-c:v", "libx265",
             "-preset", "ultrafast", "-crf", "6", "-profile:v", "main10", "-pix_fmt", "yuv420p10le", *colors,
             "-x265-params", "repeat-headers=1:aud=1:bframes=0:keyint=24:min-keyint=24:scenecut=0:open-gop=0:pools=1:frame-threads=1",
             "-f", "hevc", base], f"{label}-encode.txt")
        elementary = directory / f"{label}.hevc"
        baseline.inject_rpus(base, directory / "rpu.bin", elementary, frames)
        movie = directory / f"{label}.mp4"
        run([args.ffmpeg, "-hide_banner", "-nostdin", "-v", "info", "-fflags", "+genpts", "-r", str(FPS),
             "-f", "hevc", "-i", elementary, "-map", "0:v:0", "-c:v", "copy", "-bsf:v", "dovi_rpu=compression=none",
             "-tag:v", "hvc1", "-strict", "unofficial", "-movflags", "+write_colr", movie], f"{label}-mux.txt")
        probe = json.loads(run([args.ffprobe, "-v", "error", "-show_streams", "-show_format", "-of", "json", movie], f"{label}-probe.json"))
        stream = probe["streams"][0]
        config_out = next((data for data in stream.get("side_data_list", []) if data.get("side_data_type") == "DOVI configuration record"), {})
        expected_config = {"dv_profile": 5 if profile == "5" else 8, "dv_bl_signal_compatibility_id": {"5": 0, "8.4": 4, "8.2": 2}[profile],
                           "rpu_present_flag": 1, "el_present_flag": 0, "bl_present_flag": 1}
        if any(config_out.get(key) != value for key, value in expected_config.items()):
            raise ValueError(f"Incorrect {profile} configuration: {config_out}")
        if profile != "5" and (stream.get("color_transfer"), stream.get("color_primaries"), stream.get("color_space")) != (transfer, primaries, colorspace):
            raise ValueError("Base-layer signal was not preserved")
        if len(baseline.mp4_rpu_offsets(movie.read_bytes())) != frames:
            raise ValueError("Missing per-frame RPU payloads")
        decoded_path = directory / "decoded-base-frame.yuv"
        run([args.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", movie, "-frames:v", "1", "-an",
             "-pix_fmt", "yuv420p10le", "-f", "rawvideo", decoded_path], f"{label}-decode.txt")
        samples = array.array("H")
        samples.frombytes(decoded_path.read_bytes())
        if sys.byteorder != "little":
            samples.byteswap()
        decoded_codes = []
        for swatch in swatches:
            x, y = int(swatch["center_x"]), int(swatch["center_y"])
            decoded = [samples[y*WIDTH+x], samples[WIDTH*HEIGHT+(y//2)*(WIDTH//2)+x//2],
                       samples[WIDTH*HEIGHT*5//4+(y//2)*(WIDTH//2)+x//2]]
            if max(abs(a-b) for a,b in zip(decoded, swatch["source_codes"], strict=True)) > 5:
                raise ValueError("Encoded source does not match authored chart")
            decoded_codes.append(decoded)
            swatch["decoded_source_codes"] = decoded
            swatch["decoded_expected_bt2020_pq_rgb"] = oracle(decoded, rpu)
        write_chart(reference, [oracle(code, rpu) for code in decoded_codes], floating=True)
        (directory / "oracle-swatches.json").write_text(json.dumps(swatches, indent=2) + "\n")
        reference_movie = directory / "oracle-pq.mp4"
        pq_colors = ["-color_range", "tv", "-color_primaries", "bt2020", "-color_trc", "smpte2084", "-colorspace", "bt2020nc"]
        run([args.ffmpeg, "-hide_banner", "-nostdin", "-v", "info", "-stream_loop", "-1", "-f", "rawvideo",
             "-pixel_format", "gbrpf32le", "-video_size", f"{WIDTH}x{HEIGHT}", "-framerate", "1",
             "-color_range", "pc", "-color_primaries", "bt2020", "-color_trc", "smpte2084", "-colorspace", "rgb",
             "-i", reference, "-frames:v", str(frames), "-vf",
             "fps=24,zscale=matrixin=gbr:transferin=smpte2084:primariesin=bt2020:rangein=full:matrix=bt2020nc:transfer=smpte2084:primaries=bt2020:range=limited,format=yuv420p10le,setparams=range=limited:color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc",
             "-an", "-c:v", "libx265", "-preset", "ultrafast", "-crf", "6", "-profile:v", "main10", *pq_colors,
             "-x265-params", "bframes=0:keyint=24:min-keyint=24:scenecut=0:open-gop=0:pools=1:frame-threads=1:master-display=G(8500,39850)B(6550,2300)R(35400,14600)WP(15635,16450)L(10000000,0):max-cll=1000,400",
             "-tag:v", "hvc1", reference_movie], f"{label}-reference-encode.txt")
        original = movie.read_bytes()
        offsets = baseline.mp4_rpu_offsets(original)
        corruptions = []
        for kind in ("missing-rpu", "bad-crc"):
            damaged = bytearray(original)
            location = offsets[36]
            if kind == "missing-rpu":
                damaged[location[0]] = 61 << 1
            else:
                baseline.corrupt_crc(damaged, *location)
            bad_path = directory / f"{kind}.mp4"
            bad_path.write_bytes(damaged)
            corruptions.append({"kind": kind, "file": str(bad_path), "frame": 36,
                                "sha256": hashlib.sha256(damaged).hexdigest()})
        manifest["profiles"][profile] = {"movie": str(movie), "reference": str(reference), "reference_movie": str(reference_movie),
                                        "reference_format": "Two 320x180 gbrpf32le BT.2020 PQ frames at 1 fps, looped; plain PQ MP4 at 24 fps",
                                        "reference_luminance": {"mastering_min_nits": 0, "mastering_max_nits": 1000, "max_cll": 1000, "max_fall": 400},
                                        "swatches": str(directory / "oracle-swatches.json"), "corruptions": corruptions,
                                        "configuration": config_out, "sha256": {path.name: hashlib.sha256(path.read_bytes()).hexdigest()
                                        for path in directory.iterdir() if path.is_file()}}
        (output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(output / "manifest.json")


if __name__ == "__main__":
    main()
