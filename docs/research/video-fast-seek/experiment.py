#!/usr/bin/env python3
"""Bounded, Linux-only FFmpeg fast-seek diagnostic; never a production worker."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time


def command(args, timeout=30):
    started = time.monotonic()
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            timeout=timeout, check=True)
    return result.stdout, result.stderr.decode(errors="replace"), time.monotonic() - started


def framehash(data):
    time_base, packets = None, []
    for line in data.decode().splitlines():
        if line.startswith("#tb 0:"):
            n, d = line.split(":", 1)[1].strip().split("/")
            time_base = (int(n), int(d))
        if not line or line.startswith("#"):
            continue
        fields = [x.strip() for x in line.split(",")]
        packets.append({"dts": int(fields[1]), "pts": int(fields[2]),
                        "duration": int(fields[3]), "size": int(fields[4]),
                        "sha256": fields[5]})
    if time_base is None:
        raise RuntimeError("missing framehash time base")
    return time_base, packets


def seconds(packet, time_base):
    return packet["pts"] * time_base[0] / time_base[1]


def input_args(ffmpeg, source, origin=0, seek=None, readrate=None):
    args = [ffmpeg, "-hide_banner", "-nostdin", "-loglevel", "warning", "-threads", "1", "-copyts"]
    if readrate is not None:
        args += ["-readrate", str(readrate)]
    if seek is not None:
        args += ["-seek_timestamp", "1", "-noaccurate_seek", "-ss", f"{seek:.9f}"]
    return args + ["-itsoffset", f"{-origin:.9f}", "-i", str(source)]


def idrs(ffmpeg, source, seek=None):
    # filter_units uses FFmpeg's H.264 bitstream parser. Only NAL type 5
    # survives; a container K flag alone never establishes an IDR entry.
    args = input_args(ffmpeg, source, seek=seek)
    args += ["-map", "0:v:0", "-c:v", "copy", "-copyinkf", "-copypriorss", "1",
             "-bsf:v", "filter_units=pass_types=5"]
    if seek is not None:
        args += ["-frames:v", "1"]
    data, stderr, elapsed = command(args + ["-f", "framehash", "-hash", "sha256", "pipe:1"], 10)
    time_base, packets = framehash(data)
    return time_base, packets, stderr, elapsed


def encode(ffmpeg, source, origin, start, target, seek=None, readrate=None, timeout=30, dual=False):
    # Actual output topology: borrowed input fd3 and append-only pipe:4 output.
    source_fd = os.open(source, os.O_RDONLY)
    output_fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_APPEND, 0o600)
    if (source_fd, output_fd) != (3, 4):
        raise RuntimeError("diagnostic expects its dedicated process descriptors 3 and 4")
    os.lseek(source_fd, 17, os.SEEK_SET)
    args = input_args(ffmpeg, "/proc/self/fd/3", origin, seek, readrate)
    if dual:
        args += ["-threads", "1", "-discard:v", "all", "-itsoffset", f"{-origin:.9f}", "-i", "/proc/self/fd/3"]
    args += ["-ss", str(start), "-t", "2", "-map", "0:v:0", "-map", ("1" if dual else "0") + ":a:0",
             "-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast", "-bf", "0",
             "-pix_fmt", "yuv420p", "-fps_mode", "passthrough", "-enc_time_base:v", "demux",
             "-b:v", "256000", "-maxrate", "256000", "-bufsize", "512000",
             "-force_key_frames", "expr:gte(t,n_forced)", "-c:a", "aac", "-threads:a", "1",
             "-profile:a", "aac_low", "-b:a", "32000", "-tag:v", "avc1", "-tag:a", "mp4a",
             "-map_metadata", "-1", "-map_chapters", "-1", "-sn", "-dn",
             "-avoid_negative_ts", "disabled", "-flush_packets", "1", "-f", "mp4",
             "-movflags", "+empty_moov+delay_moov+default_base_moof+skip_trailer",
             "-frag_duration", "1000000", "-frag_size", "1048576", "pipe:4"]
    started = time.monotonic()
    try:
        result = subprocess.run(args, pass_fds=(source_fd, output_fd), stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=timeout)
        return {"exit": result.returncode, "elapsed": time.monotonic() - started,
                "stderr": result.stderr.decode(errors="replace"), "bytes": target.stat().st_size,
                "caller_position": os.lseek(source_fd, 0, os.SEEK_CUR)}
    except subprocess.TimeoutExpired:
        return {"timeout": True, "elapsed": time.monotonic() - started, "bytes": target.stat().st_size}
    finally:
        os.close(source_fd)
        os.close(output_fd)


def video_hashes(ffmpeg, source):
    data, _, _ = command([ffmpeg, "-v", "error", "-nostdin", "-xerror", "-threads", "1",
                          "-i", str(source), "-map", "0:v:0", "-fps_mode", "passthrough",
                          "-f", "framehash", "-hash", "sha256", "pipe:1"])
    return framehash(data)


def audio(ffmpeg, source):
    data, _, _ = command([ffmpeg, "-v", "error", "-nostdin", "-xerror", "-threads", "1",
                          "-i", str(source), "-map", "0:a:0", "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1"])
    from array import array
    result = array("h")
    result.frombytes(data)
    return result


def waveform_error(reference, actual):
    # Search around zero after decoder/encoder overlap. The chirp is not
    # periodic, so an equal sample count cannot conceal a shifted source.
    start, count = 24000, 24000
    if min(len(reference), len(actual)) < start + count + 192:
        raise RuntimeError("short waveform comparison")
    power = sum(x * x for x in reference[start:start + count])
    best = None
    for lag in range(-192, 193):
        error = sum((actual[start + i + lag] - reference[start + i]) ** 2 for i in range(count)) / power
        if best is None or error < best["normalized_mse"]:
            best = {"lag_samples": lag, "normalized_mse": error}
    return best


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--ffmpeg", default="/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg")
    parser.add_argument("--ffprobe", default="/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe")
    parser.add_argument("--scratch-root", default="/opt/goby-test/exec-scratch")
    options = parser.parse_args()
    if os.name != "posix" or not Path("/proc/self/fd").is_dir():
        raise RuntimeError("Linux is required")
    root = Path(tempfile.mkdtemp(prefix="goby-fast-video-seek-long-", dir=options.scratch_root))
    (root / "OWNER.txt").write_text("goby-fast-video-seek-experiment\n")
    print("OWNED", root, flush=True)
    ff = options.ffmpeg
    source = root / "closed.mp4"
    command([ff, "-v", "error", "-nostdin", "-f", "lavfi", "-i", "testsrc2=size=96x54:rate=12:duration=180",
             "-f", "lavfi", "-i", "aevalsrc=0.3*sin(2*PI*(173*t+1.5*t*t)):s=48000:d=180",
             "-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast",
             "-crf", "35", "-g", "24", "-keyint_min", "24", "-sc_threshold", "0", "-bf", "2",
             "-c:a", "aac", "-threads:a", "1", "-b:a", "32000", str(source)], 45)
    for extension in ("mkv", "ts"):
        command([ff, "-v", "error", "-nostdin", "-i", str(source), "-map", "0:v:0", "-map", "0:a:0", "-c", "copy", str(root / ("closed." + extension))])
    report = {"sources": [], "controls": []}
    for extension in ("mp4", "mkv", "ts"):
        path = root / ("closed." + extension)
        metadata, _, _ = command([options.ffprobe, "-v", "error", "-show_entries", "format=start_time,duration", "-of", "json", str(path)])
        origin = float(json.loads(metadata)["format"]["start_time"])
        tb, entries, warning, scan_time = idrs(ff, path)
        candidate = [entry for entry in entries if seconds(entry, tb) - origin <= 120.37][-1]
        seek = seconds(candidate, tb)
        landing_tb, landing, landing_warning, preflight_time = idrs(ff, path, seek)
        if len(landing) != 1 or seconds(landing[0], landing_tb) - origin > 120.37:
            raise RuntimeError("candidate did not retain a proven IDR before the request")
        matched = [entry for entry in entries if entry["sha256"] == landing[0]["sha256"] and
                   abs(seconds(entry, tb) - seconds(landing[0], landing_tb)) < 0.000001]
        if len(matched) != 1:
            raise RuntimeError("landing packet identity is not unique in the source index")
        report["sources"].append({"container": extension, "bytes": path.stat().st_size,
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "origin": origin,
            "idr_count": len(entries), "index_seconds": scan_time, "index_warning": warning,
            "candidate_absolute_pts": seek, "landing_absolute_pts": seconds(landing[0], landing_tb),
            "preflight_seconds": preflight_time, "preflight_warning": landing_warning})
        baseline, fast, dual = root / "baseline.mp4", root / "fast.mp4", root / "dual.mp4"
        linear = encode(ff, path, origin, 120.37, baseline)
        accelerated = encode(ff, path, origin, 120.37, fast, seek)
        separate = encode(ff, path, origin, 120.37, dual, seek, dual=True)
        video_equal = video_hashes(ff, baseline) == video_hashes(ff, fast)
        reference_audio, actual_audio = audio(ff, baseline), audio(ff, fast)
        result = {"container": extension, "linear": linear, "fast": accelerated,
                  "video_framehash_equal": video_equal, "audio_sample_count_equal": len(reference_audio) == len(actual_audio),
                  "audio_comparison": waveform_error(reference_audio, actual_audio), "dual": separate,
                  "dual_video_framehash_equal": video_hashes(ff, baseline) == video_hashes(ff, dual),
                  "dual_audio_pcm_equal": reference_audio == audio(ff, dual)}
        report["controls"].append(result)
        print(json.dumps(result), flush=True)
        baseline.unlink()
        fast.unlink()
        dual.unlink()
    # An explicit slow-input control demonstrates prefix avoidance without
    # pretending this tiny fixture is a hardware decode performance benchmark.
    path = root / "closed.mp4"
    slow_linear = root / "slow-linear.mp4"
    slow_fast = root / "slow-fast.mp4"
    report["readrate_control"] = {"linear": encode(ff, path, 0, 120.37, slow_linear, readrate=2, timeout=5),
                                  "fast": encode(ff, path, 0, 120.37, slow_fast, seek=120, readrate=2, timeout=5)}
    print("readrate_control", json.dumps(report["readrate_control"]), flush=True)
    slow_linear.unlink()
    slow_fast.unlink()
    report["total_retained_bytes"] = sum(p.stat().st_size for p in root.iterdir())
    (root / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    print("REPORT", root / "report.json", flush=True)


if __name__ == "__main__":
    main()
