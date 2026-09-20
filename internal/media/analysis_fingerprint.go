package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const analysisChromaprintRevision = "aed8eba2202dd9d7b3b0a56c77904cc805490d72"

// Integer timing values are obtained from the pinned Chromaprint API, never
// inferred from a version's rounded millisecond display or an assumed FPS.
type AudioFingerprintMetadata struct {
	ProtocolVersion     int      `json:"protocol_version"`
	Mode                string   `json:"mode"`
	ChromaprintVersion  string   `json:"chromaprint_version"`
	ChromaprintRevision string   `json:"chromaprint_revision"`
	Algorithm           int      `json:"algorithm"`
	SampleRate          int      `json:"sample_rate"`
	Channels            int      `json:"channels"`
	ItemDurationSamples int64    `json:"item_duration_samples"`
	DelaySamples        int64    `json:"delay_samples"`
	FirstItemEndSample  int64    `json:"first_item_end_sample"`
	MaxInputSamples     int64    `json:"max_input_samples"`
	MaxInputBytes       int64    `json:"max_input_bytes"`
	MaxOutputBytes      int      `json:"max_output_bytes"`
	InputSamples        int64    `json:"input_samples,omitempty"`
	InputBytes          int64    `json:"input_bytes,omitempty"`
	RawCount            int      `json:"raw_count,omitempty"`
	Raw                 []uint32 `json:"raw,omitempty"`
}

type AnalysisAvailability struct {
	AudioAvailable    bool
	VisualAvailable   bool
	PreviewAvailable  bool
	FFmpegPath        string
	FFmpegSHA256      string
	FFprobePath       string
	FFprobeSHA256     string
	FingerprintPath   string
	FingerprintSHA256 string
	Fingerprint       AudioFingerprintMetadata
	AudioReason       string
	VideoReason       string
}

func parseAnalysisFingerprint(data []byte, mode string) (AudioFingerprintMetadata, error) {
	var value AudioFingerprintMetadata
	if len(data) > 65_536 {
		return value, ErrAnalysisBudget
	}
	if _, err := mediaEditDecodeJSON(data); err != nil {
		return value, fmt.Errorf("%w: malformed fingerprint metadata", ErrAnalysisUnproven)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.ProtocolVersion != 1 || value.Mode != mode ||
		value.ChromaprintVersion != "1.6.1" || value.ChromaprintRevision != analysisChromaprintRevision || value.Algorithm != 1 ||
		value.SampleRate != 11025 || value.Channels != 1 || value.ItemDurationSamples < 1 || value.ItemDurationSamples > int64(value.SampleRate)*2 ||
		value.DelaySamples <= 0 || value.DelaySamples > int64(value.SampleRate)*10 || value.FirstItemEndSample != value.DelaySamples+value.ItemDurationSamples ||
		value.MaxInputSamples != 600*int64(value.SampleRate) || value.MaxInputBytes != maxAnalysisPCMBytes || value.MaxOutputBytes != 65_536 {
		return AudioFingerprintMetadata{}, fmt.Errorf("%w: unsupported fingerprint helper protocol", ErrAnalysisUnavailable)
	}
	if mode == "describe" {
		if value.InputSamples != 0 || value.InputBytes != 0 || value.RawCount != 0 || len(value.Raw) != 0 {
			return AudioFingerprintMetadata{}, ErrAnalysisUnproven
		}
	} else {
		if mode != "fingerprint" || value.InputSamples < value.FirstItemEndSample || value.InputSamples > value.MaxInputSamples || value.InputBytes != value.InputSamples*2 || value.RawCount != len(value.Raw) ||
			int64(value.RawCount) != (value.InputSamples-value.FirstItemEndSample)/value.ItemDurationSamples+1 {
			return AudioFingerprintMetadata{}, fmt.Errorf("%w: incomplete fingerprint support inventory", ErrAnalysisUnproven)
		}
	}
	return value, nil
}

type analysisTool struct {
	path   string
	file   *os.File
	before os.FileInfo
	sha    string
}

func analysisOpenTool(ctx context.Context, path string) (*analysisTool, error) {
	if strings.TrimSpace(path) == "" || strings.ContainsAny(path, "\x00\r\n") {
		return nil, ErrAnalysisUnavailable
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return nil, ErrAnalysisUnavailable
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return nil, ErrAnalysisUnavailable
	}
	file, err := openLocalMedia(resolved)
	if err != nil {
		return nil, ErrAnalysisUnavailable
	}
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > 256<<20 || before.Mode().Perm()&0111 == 0 {
		file.Close()
		return nil, ErrAnalysisUnavailable
	}
	digest, err := mediaEditFileDigest(ctx, file, before.Size())
	if err != nil {
		file.Close()
		return nil, err
	}
	tool := &analysisTool{path: resolved, file: file, before: before, sha: digest}
	if err := tool.check(); err != nil {
		file.Close()
		return nil, err
	}
	return tool, nil
}

func analysisValidSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func analysisOpenToolExpected(ctx context.Context, path, expected string) (*analysisTool, error) {
	if expected != "" && !analysisValidSHA256(expected) {
		return nil, fmt.Errorf("%w: malformed admitted executable digest", ErrAnalysisUnavailable)
	}
	tool, err := analysisOpenTool(ctx, path)
	if err != nil {
		return nil, err
	}
	if expected != "" && tool.sha != expected {
		tool.file.Close()
		return nil, fmt.Errorf("%w: executable differs from admitted bytes", ErrAnalysisUnavailable)
	}
	return tool, nil
}

func (tool *analysisTool) check() error {
	pathInfo, err := os.Stat(tool.path)
	if err != nil || !os.SameFile(tool.before, pathInfo) || mediaEditCheckUnchanged(tool.file, tool.before) != nil {
		return fmt.Errorf("%w: analysis executable changed", ErrAnalysisUnavailable)
	}
	return nil
}

func analysisDescribeFingerprint(ctx context.Context, tool *analysisTool) (AudioFingerprintMetadata, error) {
	var data []byte
	sink := &analysisDiscardStderr{}
	err := runAnalysisProcess(ctx, "/proc/self/fd/3", nil, nil, []string{"--describe"}, 5*time.Second, 4096, sink,
		func(reader io.Reader) error { var err error; data, err = io.ReadAll(reader); return err }, tool.file)
	if err != nil {
		return AudioFingerprintMetadata{}, err
	}
	if err := sink.failure(); err != nil {
		return AudioFingerprintMetadata{}, err
	}
	if err := tool.check(); err != nil {
		return AudioFingerprintMetadata{}, err
	}
	return parseAnalysisFingerprint(data, "describe")
}

func (e AnalysisExtractor) Availability(ctx context.Context) (AnalysisAvailability, error) {
	var result AnalysisAvailability
	if ctx == nil {
		return result, ErrAnalysisUnavailable
	}
	bounded, release, err := analysisAcquire(ctx, 20*time.Second)
	if err != nil {
		return result, err
	}
	defer release()
	ffmpeg, err := analysisOpenToolExpected(bounded, e.FFmpegPath, e.ExpectedFFmpegSHA256)
	if err != nil {
		result.AudioReason, result.VideoReason = "ffmpeg_unavailable", "ffmpeg_unavailable"
		return result, nil
	}
	defer ffmpeg.file.Close()
	result.FFmpegPath, result.FFmpegSHA256 = ffmpeg.path, ffmpeg.sha
	query := func(args ...string) (string, error) {
		var data []byte
		sink := &analysisDiscardStderr{}
		err := runAnalysisProcess(bounded, "/proc/self/fd/3", nil, nil, args, 5*time.Second, 128<<10, sink,
			func(reader io.Reader) error { var err error; data, err = io.ReadAll(reader); return err }, ffmpeg.file)
		if err == nil {
			err = sink.failure()
		}
		return string(data), err
	}
	version, versionErr := query("-version")
	filters, filterErr := query("-hide_banner", "-filters")
	encoders, encoderErr := query("-hide_banner", "-encoders")
	if versionErr != nil || filterErr != nil || encoderErr != nil || !analysisFFmpegVersion(version) || ffmpeg.check() != nil {
		if err := bounded.Err(); err != nil {
			return result, err
		}
		result.AudioReason, result.VideoReason = "ffmpeg_profile_unavailable", "ffmpeg_profile_unavailable"
		return result, nil
	}
	has := func(document, name string) bool {
		for _, line := range strings.Split(document, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == name {
				return true
			}
		}
		return false
	}
	videoReady := true
	for _, name := range []string{"select", "showinfo", "scale", "format", "trim", "sidedata", "transpose", "hflip", "vflip", "setsar"} {
		videoReady = videoReady && has(filters, name)
	}
	result.VisualAvailable = videoReady && has(encoders, "rawvideo")
	result.PreviewAvailable = result.VisualAvailable && has(encoders, "wrapped_avframe")
	if !result.VisualAvailable {
		result.VideoReason = "video_filters_unavailable"
	} else if !result.PreviewAvailable {
		result.VideoReason = "preview_encoder_unavailable"
	}
	probe, probeErr := analysisOpenToolExpected(bounded, e.FFprobePath, e.ExpectedFFprobeSHA256)
	if probeErr == nil {
		defer probe.file.Close()
		probeErr = analysisValidateFFprobe(bounded, probe)
		if probeErr == nil {
			result.FFprobePath, result.FFprobeSHA256 = probe.path, probe.sha
		}
	}
	if probeErr != nil {
		result.VisualAvailable, result.PreviewAvailable = false, false
		result.VideoReason = "geometry_probe_unavailable"
	}
	audioReady := true
	for _, name := range []string{"atrim", "aresample", "aformat", "asettb", "ashowinfo"} {
		audioReady = audioReady && has(filters, name)
	}
	if !audioReady || !has(encoders, "pcm_s16le") {
		result.AudioReason = "audio_filters_unavailable"
		return result, nil
	}
	fingerprint, err := analysisOpenToolExpected(bounded, e.FingerprintPath, e.ExpectedFingerprintSHA256)
	if err != nil {
		result.AudioReason = "fingerprint_unavailable"
		return result, nil
	}
	defer fingerprint.file.Close()
	result.FingerprintPath, result.FingerprintSHA256 = fingerprint.path, fingerprint.sha
	result.Fingerprint, err = analysisDescribeFingerprint(bounded, fingerprint)
	if err != nil {
		if bounded.Err() != nil {
			return result, bounded.Err()
		}
		result.AudioReason = "fingerprint_profile_unavailable"
		return result, nil
	}
	result.AudioAvailable = true
	return result, nil
}

func analysisFFmpegVersion(output string) bool {
	fields := strings.Fields(output)
	return len(fields) >= 3 && fields[0] == "ffmpeg" && fields[1] == "version" && (fields[2] == "9.0.1" || strings.HasPrefix(fields[2], "9.0.1-"))
}

func analysisValidateFFmpeg(ctx context.Context, tool *analysisTool) error {
	var data []byte
	sink := &analysisDiscardStderr{}
	err := runAnalysisProcess(ctx, "/proc/self/fd/3", nil, nil, []string{"-version"}, 5*time.Second, 128<<10, sink,
		func(reader io.Reader) error { var err error; data, err = io.ReadAll(reader); return err }, tool.file)
	if err != nil {
		return err
	}
	if err := sink.failure(); err != nil {
		return err
	}
	if !analysisFFmpegVersion(string(data)) {
		return fmt.Errorf("%w: FFmpeg 9.0.1 is required by the timestamp evidence parser", ErrAnalysisUnavailable)
	}
	return tool.check()
}

func analysisValidateFFprobe(ctx context.Context, tool *analysisTool) error {
	var data []byte
	sink := &analysisDiscardStderr{}
	err := runAnalysisProcess(ctx, "/proc/self/fd/3", nil, nil, []string{"-version"}, 5*time.Second, 128<<10, sink,
		func(reader io.Reader) error { var err error; data, err = io.ReadAll(reader); return err }, tool.file)
	if err != nil {
		return err
	}
	if err := sink.failure(); err != nil {
		return err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 || fields[0] != "ffprobe" || fields[1] != "version" || fields[2] != "9.0.1" && !strings.HasPrefix(fields[2], "9.0.1-") {
		return fmt.Errorf("%w: FFprobe 9.0.1 is required by the geometry evidence parser", ErrAnalysisUnavailable)
	}
	return tool.check()
}

// IntroAlgorithmProfile derives the cache/matcher profile from admission facts
// without launching a tool or reading a source. It intentionally excludes file
// names, stream indexes, source timestamps and other per-episode identity.
func IntroAlgorithmProfile(available AnalysisAvailability, visualIntervalTicks int64) (string, error) {
	if !available.AudioAvailable || !available.VisualAvailable || !analysisValidSHA256(available.FFmpegSHA256) ||
		!analysisValidSHA256(available.FFprobeSHA256) || !analysisValidSHA256(available.FingerprintSHA256) {
		return "", ErrAnalysisUnavailable
	}
	data, err := json.Marshal(available.Fingerprint)
	if err != nil {
		return "", err
	}
	metadata, err := parseAnalysisFingerprint(data, "describe")
	if err != nil {
		return "", err
	}
	if visualIntervalTicks == 0 {
		visualIntervalTicks = TicksPerSecond / 2
	}
	if visualIntervalTicks < TicksPerSecond/10 || visualIntervalTicks > 10*TicksPerSecond {
		return "", ErrAnalysisUnproven
	}
	return fmt.Sprintf("%s:ffmpeg=%s:helper=%s;visual=%s;geometry=%s;ffprobe=%s;visual_interval_ticks=%d",
		analysisAudioProfile(metadata), available.FFmpegSHA256, available.FingerprintSHA256, VisualHashProfile, AnalysisGeometryProfile,
		available.FFprobeSHA256, visualIntervalTicks), nil
}

func analysisAudioProfile(metadata AudioFingerprintMetadata) string {
	descriptor := fmt.Sprintf("chromaprint:%s:%s:test2:pcm-s16le:mono:rate=%d:step=%d:delay=%d:left-support-anchor:v1",
		metadata.ChromaprintVersion, metadata.ChromaprintRevision, metadata.SampleRate, metadata.ItemDurationSamples, metadata.DelaySamples)
	digest := sha256.Sum256([]byte(descriptor))
	return "intro-audio-v1-" + hex.EncodeToString(digest[:])
}
