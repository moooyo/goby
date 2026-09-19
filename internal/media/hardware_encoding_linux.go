package media

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// HardwareEncodingIdentity binds cached evidence to executable filesystem
// identities, the DRM device/driver, loader environment and VAAPI libraries.
// It does not execute a tool or open the GPU. Missing identity fails closed.
func HardwareEncodingIdentity(ctx context.Context, ffmpeg, ffprobe, device string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var parts []string
	for _, executable := range []string{ffmpeg, ffprobe} {
		stamp, err := EncodingToolIdentity(ctx, executable)
		if err != nil {
			return "", err
		}
		parts = append(parts, stamp)
	}
	info, err := os.Stat(device)
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return "", fmt.Errorf("hardware encoding device identity is unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("hardware encoding device identity is unavailable")
	}
	parts = append(parts, fmt.Sprintf("%s:%d:%d:%d:%d:%d", device, stat.Dev, stat.Ino, stat.Rdev, stat.Ctim.Sec, stat.Ctim.Nsec))
	base := filepath.Join("/sys/class/drm", filepath.Base(device), "device")
	driver, err := filepath.EvalSymlinks(filepath.Join(base, "driver"))
	if err != nil {
		return "", fmt.Errorf("hardware encoding driver identity is unavailable")
	}
	parts = append(parts, driver)
	for _, name := range []string{"vendor", "device", "revision", "subsystem_vendor", "subsystem_device", "driver/module/version", "driver/module/srcversion"} {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if len(data) > 4096 {
			return "", fmt.Errorf("hardware encoding driver identity exceeds its limit")
		}
		parts = append(parts, name, string(data))
	}
	patterns := []string{"/usr/lib/*/dri/*_drv_video.so", "/usr/lib/dri/*_drv_video.so", "/usr/lib64/dri/*_drv_video.so", "/usr/local/lib/dri/*_drv_video.so",
		"/usr/lib/*/libva.so*", "/usr/lib/*/libdrm*.so*", "/usr/lib64/libva.so*", "/usr/lib64/libdrm*.so*"}
	for _, key := range []string{"LD_LIBRARY_PATH", "LIBVA_DRIVER_NAME", "LIBVA_DRIVERS_PATH"} {
		value := os.Getenv(key)
		if len(value) > 4096 {
			return "", fmt.Errorf("hardware encoding environment exceeds its limit")
		}
		parts = append(parts, key, value)
		if key == "LIBVA_DRIVERS_PATH" {
			for _, directory := range filepath.SplitList(value) {
				patterns = append(patterns, filepath.Join(directory, "*_drv_video.so"))
			}
		} else if key == "LD_LIBRARY_PATH" {
			for _, directory := range filepath.SplitList(value) {
				patterns = append(patterns, filepath.Join(directory, "*.so*"))
			}
		}
	}
	var libraries []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil || len(libraries)+len(matches) > 512 {
			return "", fmt.Errorf("hardware encoding library identity exceeds its limit")
		}
		libraries = append(libraries, matches...)
	}
	sort.Strings(libraries)
	if len(libraries) == 0 {
		return "", fmt.Errorf("hardware encoding VAAPI library identity is unavailable")
	}
	for _, path := range libraries {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		stamp, err := VideoSeekSourceIdentity(info)
		if err != nil {
			return "", err
		}
		parts = append(parts, path, stamp)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\x00")))), nil
}

// EncodingToolIdentity is a cheap executable and loader-library fingerprint.
// The inode/change-time identity prevents reuse after replacement or mutation.
func EncodingToolIdentity(ctx context.Context, executable string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", err
	}
	paths := []string{resolved}
	loader := os.Getenv("LD_LIBRARY_PATH")
	if len(loader) > 4096 {
		return "", fmt.Errorf("hardware encoding loader identity exceeds its limit")
	}
	patterns := []string{"/usr/lib/*/libav*.so*", "/usr/lib/*/libsw*.so*", "/usr/lib64/libav*.so*", "/usr/lib64/libsw*.so*",
		filepath.Join(filepath.Dir(resolved), "..", "lib", "*.so*")}
	for _, directory := range filepath.SplitList(loader) {
		patterns = append(patterns, filepath.Join(directory, "*.so*"))
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil || len(paths)+len(matches) > 512 {
			return "", fmt.Errorf("hardware encoding loader identity exceeds its limit")
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)
	parts := []string{resolved, loader}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		stamp, err := VideoSeekSourceIdentity(info)
		if err != nil {
			return "", err
		}
		parts = append(parts, path, stamp)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\x00")))), nil
}

// ProbeHardwareEncoding runs three bounded synthetic frames, verifies exact
// output metadata, then strictly decodes the same private bytes and checks the
// color pattern. It neither reads user media nor changes a conversion plan.
func ProbeHardwareEncoding(ctx context.Context, ffmpeg, ffprobe string, request HardwareEncodingRequest) (HardwareEncodingResult, error) {
	if !request.valid() {
		return hardwareEncodingRejected(ctx, "hardware_encoding_tuple_invalid")
	}
	work, cancel := context.WithTimeout(ctx, hardwareEncodingTimeout)
	defer cancel()
	reject := func(code string) (HardwareEncodingResult, error) {
		if err := work.Err(); err != nil {
			return HardwareEncodingResult{}, err
		}
		return hardwareEncodingRejected(ctx, code)
	}
	identity, err := HardwareEncodingIdentity(work, ffmpeg, ffprobe, request.Device)
	if err != nil {
		return reject("hardware_encoding_identity_unavailable")
	}
	encoded, err := runHardwareEncodingProcess(work, ffmpeg, nil, hardwareEncodingBytes, hardwareEncodingArgs(request)...)
	if err != nil || len(encoded.stderr) != 0 || len(encoded.stdout) == 0 {
		return reject("hardware_encoding_probe_failed")
	}
	file, err := os.CreateTemp("", "goby-hardware-encoding-*.mkv")
	if err != nil {
		return reject("hardware_encoding_scratch_unavailable")
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(encoded.stdout); err != nil {
		return reject("hardware_encoding_scratch_unavailable")
	}
	files := []*os.File{file}
	probe, err := runHardwareEncodingProcess(work, ffprobe, files, 16*1024,
		"-v", "error", "-max_alloc", "268435456", "-threads", "1", "-protocol_whitelist", "file,pipe", "-f", "matroska", "-i", "/proc/self/fd/3",
		"-select_streams", "v:0", "-count_frames", "-show_entries", "stream=codec_name,profile,width,height,pix_fmt,nb_read_frames", "-of", "json")
	if err != nil || len(probe.stderr) != 0 || !hardwareEncodingMetadataMatches(probe.stdout, request) {
		return reject("hardware_encoding_output_mismatch")
	}
	decoded, err := runHardwareEncodingProcess(work, ffmpeg, files, 3*8*8*3,
		"-hide_banner", "-v", "error", "-nostdin", "-xerror", "-err_detect", "explode", "-max_alloc", "268435456", "-timelimit", "3",
		"-threads", "1", "-filter_threads", "1", "-protocol_whitelist", "file,pipe", "-f", "matroska", "-i", "/proc/self/fd/3",
		"-map", "0:v:0", "-an", "-sn", "-dn", "-vf", "scale=8:8:flags=area,format=rgb24", "-frames:v", "3", "-threads", "1", "-f", "rawvideo", "pipe:1")
	if err != nil || len(decoded.stderr) != 0 || !hardwareEncodingPixelsMatch(decoded.stdout) {
		return reject("hardware_encoding_decode_failed")
	}
	after, err := HardwareEncodingIdentity(work, ffmpeg, ffprobe, request.Device)
	if err != nil || after != identity {
		return reject("hardware_encoding_identity_changed")
	}
	return HardwareEncodingResult{Usable: true, Code: "hardware_encoding_usable"}, nil
}

func runHardwareEncodingProcess(ctx context.Context, executable string, files []*os.File, limit int, args ...string) (mediaProcessOutput, error) {
	// The descriptor selects the ordinary sanitized process environment. VAAPI
	// loader settings are copied explicitly because production uses them too.
	if err := ctx.Err(); err != nil {
		return mediaProcessOutput{}, err
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	stdout := &limitedOutput{limit: limit, cancel: cancel}
	stderr := &limitedOutput{limit: maxProcessStderr, cancel: cancel}
	command := exec.CommandContext(work, executable, args...)
	command.ExtraFiles = files
	command.Env = mediaProbeEnvironment()
	for _, key := range []string{"LIBVA_DRIVER_NAME", "LIBVA_DRIVERS_PATH", "LIBVA_MESSAGING_LEVEL", "XDG_RUNTIME_DIR", "XDG_CACHE_HOME"} {
		if value, exists := os.LookupEnv(key); exists {
			command.Env = append(command.Env, key+"="+value)
		}
	}
	command.Stdout, command.Stderr = stdout, stderr
	command.WaitDelay = hardwareEncodingTimeout
	retired, err := startMediaProcess(command)
	if err == nil {
		retireErr := <-retired
		err = command.Wait()
		if retireErr != nil {
			err = retireErr
		}
	}
	if stdout.exceeded || stderr.exceeded {
		return mediaProcessOutput{}, ErrOutputLimit
	}
	if work.Err() != nil {
		return mediaProcessOutput{}, work.Err()
	}
	if err != nil {
		return mediaProcessOutput{}, fmt.Errorf("hardware encoding process failed")
	}
	return mediaProcessOutput{stdout: stdout.buffer.Bytes(), stderr: stderr.buffer.Bytes()}, nil
}
