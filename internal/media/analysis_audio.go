package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/big"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/moooyo/goby/internal/introdetect"
)

type AudioAnalysis struct {
	Samples                  []introdetect.AudioSample
	Metadata                 AudioFingerprintMetadata
	BoundaryUncertaintyTicks int64
	AlgorithmProfile         string
}

type analysisAudioFrame struct {
	pts      int64
	samples  int64
	checksum uint32
}

var analysisAudioLine = regexp.MustCompile(`\bn:([0-9]+) pts:([^ ]+) pts_time:[^ ]+ fmt:s16 channels:1 chlayout:mono rate:11025 nb_samples:([0-9]+) checksum:([0-9A-Fa-f]{8})(?: |$)`)

type analysisAudioLog struct {
	mu         sync.Mutex
	pending    []byte
	bytes      int64
	limit      int64
	frameLimit int
	frames     []analysisAudioFrame
	audit      *analysisPacketPTS
	closed     bool
	err        error
}

func (log *analysisAudioLog) Write(data []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.err != nil {
		return 0, log.err
	}
	if log.closed {
		return 0, io.ErrClosedPipe
	}
	log.bytes += int64(len(data))
	if log.bytes > log.limit {
		log.err = ErrAnalysisBudget
		return 0, log.err
	}
	for _, b := range data {
		if b != '\n' {
			if len(log.pending) >= 16<<10 {
				log.err = ErrAnalysisBudget
				return 0, log.err
			}
			log.pending = append(log.pending, b)
			continue
		}
		if err := log.line(string(log.pending)); err != nil {
			log.err = err
			return 0, err
		}
		log.pending = log.pending[:0]
	}
	return len(data), nil
}

func (log *analysisAudioLog) line(line string) error {
	if strings.Contains(line, "[error]") || strings.Contains(line, "[fatal]") || strings.Contains(line, "[panic]") {
		return fmt.Errorf("%w: audio decoder reported an error", ErrAnalysisUnproven)
	}
	if _, err := log.audit.line(line); err != nil {
		return fmt.Errorf("%w: audio original packet timestamps: %w", ErrAnalysisUnproven, err)
	}
	// The complete timing/checksum body is one av_log call, but plane checksums
	// and the newline are separate calls. Other pipeline threads can insert a
	// demuxer record in that same physical line, so process both records and do
	// not require a prefix on every continuation. There is one ashowinfo filter.
	matches := analysisAudioLine.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		if strings.Contains(line, "ashowinfo@analysis_audio") && strings.Contains(line, "n:") {
			return fmt.Errorf("%w: incomplete PCM frame timing", ErrAnalysisUnproven)
		}
		return nil
	}
	if len(matches) != 1 {
		return fmt.Errorf("%w: ambiguous PCM frame timing", ErrAnalysisUnproven)
	}
	fields := matches[0]
	number, nerr := strconv.Atoi(fields[1])
	pts, perr := analysisPTSInteger(fields[2])
	count, cerr := strconv.ParseInt(fields[3], 10, 64)
	checksum, herr := strconv.ParseUint(fields[4], 16, 32)
	if nerr != nil || perr != nil || cerr != nil || herr != nil || number != len(log.frames) || count <= 0 || count > 1<<20 ||
		pts < -12*60*60*11025 || pts > 24*60*60*11025 {
		return fmt.Errorf("%w: invalid PCM frame inventory", ErrAnalysisUnproven)
	}
	if len(log.frames) >= log.frameLimit {
		return ErrAnalysisBudget
	}
	if len(log.frames) != 0 {
		previous := log.frames[len(log.frames)-1]
		if pts != previous.pts+previous.samples {
			return fmt.Errorf("%w: PCM frame %d is discontinuous (%d != %d)", ErrAnalysisUnproven, number, pts, previous.pts+previous.samples)
		}
	}
	log.frames = append(log.frames, analysisAudioFrame{pts: pts, samples: count, checksum: uint32(checksum)})
	return nil
}

func (log *analysisAudioLog) Close(err error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.closed {
		return
	}
	log.closed = true
	if log.err == nil && err != nil {
		log.err = err
	}
	if log.err == nil && len(log.pending) != 0 {
		log.err = fmt.Errorf("%w: truncated audio diagnostics", ErrAnalysisUnproven)
	}
}

// ashowinfo uses AVAdler's zero seed, unlike hash/adler32's normal seed one.
// The checksum binds each exact raw PCM extent to the frame carrying its PTS.
func analysisPCMChecksum(data []byte) uint32 {
	var first, second uint64
	for len(data) != 0 {
		count := min(len(data), 5552)
		for _, value := range data[:count] {
			first += uint64(value)
			second += first
		}
		first %= 65521
		second %= 65521
		data = data[count:]
	}
	return uint32(second<<16 | first)
}

func (log *analysisAudioLog) provePCM(data []byte) (int64, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.err != nil {
		return 0, log.err
	}
	if !log.closed || len(log.frames) == 0 || log.audit.packets == 0 || len(data)%2 != 0 {
		return 0, fmt.Errorf("%w: empty or unfinished PCM proof", ErrAnalysisUnproven)
	}
	position := int64(0)
	for index, frame := range log.frames {
		end := position + frame.samples*2
		if end > int64(len(data)) || analysisPCMChecksum(data[position:end]) != frame.checksum {
			return 0, fmt.Errorf("%w: PCM frame %d bytes do not match its timing record", ErrAnalysisUnproven, index)
		}
		position = end
	}
	if position != int64(len(data)) {
		return 0, fmt.Errorf("%w: PCM output has unbound samples", ErrAnalysisUnproven)
	}
	return log.frames[0].pts, nil
}

func analysisAudioArgs(info Info, stream Stream, window int64, sampleRate int) []string {
	absolute := func(ticks int64) string {
		return new(big.Rat).SetFrac64(info.FormatStartTicks+ticks, TicksPerSecond).FloatString(7)
	}
	rate := strconv.Itoa(sampleRate)
	filter := "atrim=start=" + absolute(0) + ":end=" + absolute(window) + ",aresample=" + rate + ":async=0,aformat=sample_fmts=s16:channel_layouts=mono," +
		"asettb=expr=1/" + rate + ",atrim=end_sample=" + strconv.FormatInt(600*int64(sampleRate), 10) + ",ashowinfo@analysis_audio"
	return []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "repeat+level+info", "-debug_ts", "-xerror", "-max_alloc", "268435456",
		"-copyts", "-fflags", "+nofillin-genpts", "-threads", "1", "-filter_threads", "1", "-filter_complex_threads", "1", "-reinit_filter", "0",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats, "-i", "/proc/self/fd/3", "-map", "0:" + strconv.Itoa(stream.Index),
		"-vn", "-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1", "-af", filter, "-c:a", "pcm_s16le", "-threads:a", "1",
		"-ar", rate, "-ac", "1", "-f", "s16le", "-flush_packets", "1", "pipe:1"}
}

// ExtractAudio decodes the first bounded source window once. Raw PCM and its
// per-frame checksum/timing inventory are bounded separately. No pathname is
// sent to a decoder, and the independent helper accepts only PCM on stdin.
func (e AnalysisExtractor) ExtractAudio(ctx context.Context, input *os.File, info Info, streamIndex int) (result AudioAnalysis, resultErr error) {
	if ctx == nil {
		return result, ErrAnalysisUnavailable
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	limits, err := e.analysisLimits()
	if err != nil {
		return result, err
	}
	before, err := analysisCheckSource(input, info)
	if err != nil {
		return result, err
	}
	defer func() {
		if err := videoSeekCheckSource(input, before); err != nil {
			result, resultErr = AudioAnalysis{}, err
		}
		if err := ctx.Err(); err != nil {
			result, resultErr = AudioAnalysis{}, err
		}
	}()
	stream, err := analysisSelectedStream(info, streamIndex, "audio")
	if err != nil {
		return result, err
	}
	if stream.SampleRate < 1 || stream.SampleRate > 384000 || stream.Channels < 1 || stream.Channels > 64 {
		return result, ErrAnalysisUnproven
	}
	bounded, release, err := analysisAcquire(ctx, limits.Timeout)
	if err != nil {
		return result, err
	}
	defer release()
	ffmpeg, err := analysisOpenToolExpected(bounded, e.FFmpegPath, e.ExpectedFFmpegSHA256)
	if err != nil {
		return result, err
	}
	defer ffmpeg.file.Close()
	if err := analysisValidateFFmpeg(bounded, ffmpeg); err != nil {
		return result, err
	}
	helper, err := analysisOpenToolExpected(bounded, e.FingerprintPath, e.ExpectedFingerprintSHA256)
	if err != nil {
		return result, err
	}
	defer helper.file.Close()
	metadata, err := analysisDescribeFingerprint(bounded, helper)
	if err != nil {
		return result, err
	}
	audit, err := newAnalysisPacketPTSAudit(stream, limits.MaxAudioFrames*4)
	if err != nil {
		return result, err
	}
	log := &analysisAudioLog{limit: limits.MaxStderrBytes, frameLimit: limits.MaxAudioFrames, audit: audit}
	window := min(info.DurationTicks, MaxIntroAnalysisTicks)
	var pcm []byte
	err = runAnalysisStream(bounded, "/proc/self/fd/4", input, analysisAudioArgs(info, stream, window, metadata.SampleRate), limits.Timeout, limits.MaxPCMBytes, log,
		func(reader io.Reader) error { var err error; pcm, err = io.ReadAll(reader); return err }, ffmpeg.file)
	if err != nil {
		return result, err
	}
	firstPTS, err := log.provePCM(pcm)
	if err != nil {
		return result, err
	}
	if int64(len(pcm)) < metadata.FirstItemEndSample*2 || int64(len(pcm)) > metadata.MaxInputBytes {
		return result, fmt.Errorf("%w: insufficient bounded PCM support", ErrAnalysisUnproven)
	}
	var output []byte
	sink := &analysisDiscardStderr{}
	err = runAnalysisProcess(bounded, "/proc/self/fd/3", nil, bytes.NewReader(pcm), []string{"--sample-rate", strconv.Itoa(metadata.SampleRate), "--channels", "1"},
		limits.Timeout, int64(metadata.MaxOutputBytes), sink, func(reader io.Reader) error { var err error; output, err = io.ReadAll(reader); return err }, helper.file)
	if err != nil {
		return result, err
	}
	if err := sink.failure(); err != nil {
		return result, err
	}
	fingerprint, err := parseAnalysisFingerprint(output, "fingerprint")
	if err != nil {
		return result, err
	}
	if fingerprint.InputBytes != int64(len(pcm)) || fingerprint.SampleRate != metadata.SampleRate || fingerprint.ItemDurationSamples != metadata.ItemDurationSamples ||
		fingerprint.DelaySamples != metadata.DelaySamples || fingerprint.FirstItemEndSample != metadata.FirstItemEndSample {
		return result, fmt.Errorf("%w: fingerprint timing profile changed", ErrAnalysisUnproven)
	}
	if err := ffmpeg.check(); err != nil {
		return result, err
	}
	if err := helper.check(); err != nil {
		return result, err
	}
	samples, guard, err := mapAnalysisAudioSamples(firstPTS, info.FormatStartTicks, window, fingerprint)
	if err != nil {
		return result, err
	}
	// The tool byte identities also prevent feature-cache reuse across changed
	// decoders or builds, even when their human-readable versions are equal.
	profile := analysisAudioProfile(metadata) + ":ffmpeg=" + ffmpeg.sha + ":helper=" + helper.sha
	fingerprint.Raw = nil
	if err := bounded.Err(); err != nil {
		return AudioAnalysis{}, err
	}
	return AudioAnalysis{Samples: samples, Metadata: fingerprint, BoundaryUncertaintyTicks: guard, AlgorithmProfile: profile}, nil
}

func mapAnalysisAudioSamples(firstPTS, origin, window int64, metadata AudioFingerprintMetadata) ([]introdetect.AudioSample, int64, error) {
	if metadata.SampleRate != 11025 || metadata.ItemDurationSamples <= 0 || metadata.ItemDurationSamples > 2*int64(metadata.SampleRate) ||
		metadata.DelaySamples <= 0 || metadata.DelaySamples > 10*int64(metadata.SampleRate) || metadata.FirstItemEndSample != metadata.ItemDurationSamples+metadata.DelaySamples ||
		metadata.RawCount != len(metadata.Raw) || len(metadata.Raw) > 65_536 || firstPTS < -12*60*60*11025 || firstPTS > 24*60*60*11025 ||
		origin < -MaxAnalysisDurationTicks || origin > MaxAnalysisDurationTicks || window <= 0 || window > MaxIntroAnalysisTicks {
		return nil, 0, ErrAnalysisUnproven
	}
	rate := big.NewInt(int64(metadata.SampleRate))
	toTicks := func(sample int64) (int64, error) {
		value := new(big.Int).Mul(big.NewInt(sample), big.NewInt(TicksPerSecond))
		value.Div(value, rate) // Floor also preserves a negative absolute origin.
		value.Sub(value, big.NewInt(origin))
		if !value.IsInt64() {
			return 0, ErrAnalysisUnproven
		}
		return value.Int64(), nil
	}
	guard := (metadata.FirstItemEndSample*TicksPerSecond + int64(metadata.SampleRate) - 1) / int64(metadata.SampleRate)
	result := make([]introdetect.AudioSample, 0, len(metadata.Raw))
	for index, fingerprint := range metadata.Raw {
		startSample := firstPTS + int64(index)*metadata.ItemDurationSamples
		start, err := toTicks(startSample)
		if err != nil {
			return nil, 0, err
		}
		end, err := toTicks(startSample + metadata.ItemDurationSamples)
		if err != nil {
			return nil, 0, err
		}
		if start < 0 || end > window {
			continue
		}
		if end <= start || len(result) != 0 && start < result[len(result)-1].EndTicks {
			return nil, 0, ErrAnalysisUnproven
		}
		result = append(result, introdetect.AudioSample{StartTicks: start, EndTicks: end, Fingerprint: fingerprint})
	}
	if len(result) == 0 {
		return nil, 0, fmt.Errorf("%w: no fully source-bound fingerprint anchors", ErrAnalysisUnproven)
	}
	return result, guard, nil
}
