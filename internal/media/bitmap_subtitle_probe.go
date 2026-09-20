package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"math/big"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var bitmapSubtitleSlots = make(chan struct{}, 2)

// DecodeBitmapSubtitles obtains actual demuxed packets from an authorized held
// descriptor. Display times are decoded from PGS compositions or DVD commands,
// never from sampled video frames. It does not mutate the source or publish text.
func DecodeBitmapSubtitles(ctx context.Context, config BitmapSubtitleConfig, input *os.File, stream Stream, source Info) ([]BitmapSubtitleCue, []string, error) {
	var result []BitmapSubtitleCue
	pixels := 0
	warnings, err := walkBitmapSubtitles(ctx, config, input, stream, source, func(cue BitmapSubtitleCue) error {
		count := cue.Image.Bounds().Dx() * cue.Image.Bounds().Dy()
		if count > maxBitmapSubtitleTotalPixels-pixels {
			return fmt.Errorf("%w: batch bitmap pixel limit", ErrBitmapSubtitle)
		}
		pixels += count
		var original bytes.Buffer
		if err := png.Encode(&original, cue.Image); err != nil {
			return err
		}
		digest := sha256.Sum256(original.Bytes())
		cue.ImageSHA256 = hex.EncodeToString(digest[:])
		result = append(result, cue)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return result, warnings, nil
}

// walkBitmapSubtitles retains only the bounded demuxed packet inventory and
// decoder working set. The synchronous consumer must release the raster before
// returning; production OCR keeps its bounded compressed PNG evidence instead.
func walkBitmapSubtitles(ctx context.Context, config BitmapSubtitleConfig, input *os.File, stream Stream, source Info, emit func(BitmapSubtitleCue) error) ([]string, error) {
	if runtime.GOOS != "linux" || input == nil || stream.Index < 0 || stream.Index > 4095 ||
		stream.CodecType != "subtitle" || stream.IsExternal ||
		(stream.Codec != "hdmv_pgs_subtitle" && stream.Codec != "dvd_subtitle") ||
		source.DurationTicks <= 0 || source.DurationTicks > 7*24*60*60*TicksPerSecond || emit == nil {
		return nil, ErrBitmapSubtitle
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	select {
	case bitmapSubtitleSlots <- struct{}{}:
		defer func() { <-bitmapSubtitleSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	before, err := input.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 {
		return nil, ErrBitmapSubtitle
	}
	if source.Size > 0 && source.Size != before.Size() || source.FileChangeTimeNs != 0 && source.FileChangeTimeNs != FileChangeTime(before) {
		return nil, fmt.Errorf("%w: stale source identity", ErrBitmapSubtitle)
	}
	executable, executableInfo, err := openPinnedSubtitleFile(ctx, config.FFprobePath, config.FFprobeSHA256, 128<<20)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBitmapSubtitle, err)
	}
	defer executable.Close()
	output, err := runLimitedFilesOutput(ctx, 2*time.Minute, maxBitmapSubtitleProbeBytes, "/proc/self/fd/4", []*os.File{input, executable},
		"-v", "error", "-threads", "1", "-max_alloc", "67108864", "-probesize", "16777216", "-analyzeduration", "30000000", "-select_streams", strconv.Itoa(stream.Index),
		"-show_packets", "-show_streams", "-show_format", "-show_data",
		"-show_entries", "packet=stream_index,pts,duration,size,data:stream=index,codec_name,codec_type,time_base,extradata:format=start_time,duration",
		"-of", "json", "-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats, "-i", "/proc/self/fd/3")
	if err != nil {
		return nil, fmt.Errorf("%w: packet scan: %w", ErrBitmapSubtitle, err)
	}
	if !subtitleFileUnchanged(input, before) || !subtitleFileUnchanged(executable, executableInfo) {
		return nil, fmt.Errorf("%w: source or tool changed", ErrBitmapSubtitle)
	}
	if len(bytes.TrimSpace(output.stderr)) != 0 {
		return nil, fmt.Errorf("%w: demuxer reported corrupt input", ErrBitmapSubtitle)
	}
	packets, extra, err := parseBitmapSubtitleProbe(output.stdout, stream, source)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBitmapSubtitle, err)
	}
	clipped := false
	count := 0
	consume := func(cue BitmapSubtitleCue) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if cue.Image == nil || cue.Image.Bounds().Empty() || cue.StartTicks >= cue.EndTicks {
			return fmt.Errorf("%w: invalid decoded cue", ErrBitmapSubtitle)
		}
		if cue.EndTicks <= 0 || cue.StartTicks >= source.DurationTicks {
			clipped = true
			return nil
		}
		if cue.StartTicks < 0 {
			cue.StartTicks = 0
			clipped = true
		}
		if cue.EndTicks > source.DurationTicks {
			cue.EndTicks = source.DurationTicks
			clipped = true
		}
		cue.Forced = cue.Forced || stream.IsForced
		cue.HearingImpaired = stream.IsHearingImpaired
		count++
		return emit(cue)
	}
	var warnings []string
	limits := bitmapSubtitleLimits(source.DurationTicks)
	limits.MaxWorkPixels = 1_000_000_000
	if stream.Codec == "hdmv_pgs_subtitle" {
		warnings, err = walkPGSSubtitles(ctx, packets, limits, consume)
	} else {
		warnings, err = walkDVDSubtitles(ctx, packets, extra, limits, consume)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBitmapSubtitle, err)
	}
	if count == 0 || !subtitleFileUnchanged(input, before) || !subtitleFileUnchanged(executable, executableInfo) {
		return nil, fmt.Errorf("%w: empty or stale decoded track", ErrBitmapSubtitle)
	}
	if clipped {
		warnings = append(warnings, "cue_intervals_clipped_to_source_presentation")
	}
	return warnings, nil
}

type bitmapProbePacket struct {
	StreamIndex int    `json:"stream_index"`
	PTS         scalar `json:"pts"`
	Duration    scalar `json:"duration"`
	Size        scalar `json:"size"`
	Data        string `json:"data"`
}

type bitmapProbePackets []bitmapProbePacket

// Enforce the count and declared byte budgets while decoding each element;
// unmarshalling a complete unbounded packet array before checking it would
// amplify a small-packet input far beyond the bounded process output.
func (packets *bitmapProbePackets) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return fmt.Errorf("invalid packet list")
	}
	var parsed bitmapProbePackets
	total := int64(0)
	for decoder.More() {
		if len(parsed) >= maxBitmapSubtitlePackets {
			return fmt.Errorf("subtitle packet count limit")
		}
		var packet bitmapProbePacket
		if err := decoder.Decode(&packet); err != nil {
			return fmt.Errorf("invalid subtitle packet")
		}
		size, err := packet.Size.integer()
		if err != nil || size <= 0 || size > int64(maxBitmapSubtitlePacketBytes)-total || int64(len(packet.Data)) > size*5+4096 {
			return fmt.Errorf("subtitle packet byte limit")
		}
		total += size
		parsed = append(parsed, packet)
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') {
		return fmt.Errorf("invalid packet list end")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return fmt.Errorf("trailing packet list data")
	}
	*packets = parsed
	return nil
}

type bitmapSubtitleProbe struct {
	Packets bitmapProbePackets `json:"packets"`
	Streams []struct {
		Index     int    `json:"index"`
		Codec     string `json:"codec_name"`
		CodecType string `json:"codec_type"`
		TimeBase  scalar `json:"time_base"`
		ExtraData string `json:"extradata"`
	} `json:"streams"`
	Format struct {
		StartTime scalar `json:"start_time"`
	} `json:"format"`
}

func parseBitmapSubtitleProbe(data []byte, stream Stream, source Info) ([]bitmapSubtitlePacket, []byte, error) {
	var document bitmapSubtitleProbe
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return nil, nil, fmt.Errorf("invalid packet document")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || len(document.Streams) != 1 || len(document.Packets) == 0 || len(document.Packets) > maxBitmapSubtitlePackets {
		return nil, nil, fmt.Errorf("invalid packet inventory")
	}
	facts := document.Streams[0]
	if facts.Index != stream.Index || facts.Codec != stream.Codec || facts.CodecType != "subtitle" || facts.TimeBase.missing() {
		return nil, nil, fmt.Errorf("subtitle stream identity mismatch")
	}
	base, err := facts.TimeBase.rational()
	if err != nil || base.Sign() <= 0 {
		return nil, nil, fmt.Errorf("invalid subtitle time base")
	}
	origin := int64(0)
	if !document.Format.StartTime.missing() {
		reported, err := bitmapScalarTicks(document.Format.StartTime, big.NewRat(1, 1))
		if err != nil {
			return nil, nil, err
		}
		origin = reported
	}
	if source.FormatStartKnown {
		if document.Format.StartTime.missing() || origin < source.FormatStartTicks-1 || origin > source.FormatStartTicks+1 {
			return nil, nil, fmt.Errorf("subtitle presentation origin changed")
		}
		origin = source.FormatStartTicks
	}
	if origin < -7*24*60*60*TicksPerSecond || origin > 7*24*60*60*TicksPerSecond {
		return nil, nil, fmt.Errorf("subtitle origin out of range")
	}
	extra, err := decodeSubtitleHexDump(facts.ExtraData, -1, 64<<10)
	if err != nil {
		return nil, nil, err
	}
	packets := make([]bitmapSubtitlePacket, 0, len(document.Packets))
	total := 0
	for _, packet := range document.Packets {
		if packet.StreamIndex != stream.Index || packet.PTS.missing() {
			return nil, nil, fmt.Errorf("missing subtitle packet timestamp")
		}
		pts, err := bitmapScalarTicks(packet.PTS, base)
		if err != nil || pts < -7*24*60*60*TicksPerSecond || pts > 7*24*60*60*TicksPerSecond {
			return nil, nil, fmt.Errorf("invalid subtitle packet timestamp")
		}
		duration := int64(0)
		if !packet.Duration.missing() {
			duration, err = bitmapScalarTicks(packet.Duration, base)
			if err != nil || duration < 0 || duration > 7*24*60*60*TicksPerSecond {
				return nil, nil, fmt.Errorf("invalid subtitle packet duration")
			}
		}
		size, err := packet.Size.integer()
		if err != nil || size <= 0 || size > int64(maxBitmapSubtitlePacketBytes-total) {
			return nil, nil, fmt.Errorf("subtitle packet byte limit")
		}
		payload, err := decodeSubtitleHexDump(packet.Data, int(size), int(size))
		if err != nil {
			return nil, nil, err
		}
		total += len(payload)
		packets = append(packets, bitmapSubtitlePacket{PTS: pts - origin, Duration: duration, Data: payload})
	}
	return packets, extra, nil
}

func bitmapScalarTicks(value scalar, base *big.Rat) (int64, error) {
	rational, err := value.rational()
	if err != nil {
		return 0, fmt.Errorf("invalid subtitle timestamp")
	}
	rational.Mul(rational, base).Mul(rational, big.NewRat(TicksPerSecond, 1))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(rational.Num(), rational.Denom(), remainder)
	if rational.Sign() < 0 && remainder.Sign() != 0 {
		quotient.Sub(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, fmt.Errorf("subtitle timestamp overflow")
	}
	return quotient.Int64(), nil
}

// ffprobe's show_data dump has a hexadecimal offset, at most sixteen bytes,
// then a double-space delimiter and an untrusted ASCII rendering. Parse only
// the byte column; hex-looking subtitle text is never treated as packet data.
func decodeSubtitleHexDump(text string, expected, limit int) ([]byte, error) {
	if limit <= 0 || expected > limit {
		return nil, fmt.Errorf("invalid subtitle dump limit")
	}
	if strings.TrimSpace(text) == "" {
		if expected <= 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("empty subtitle packet data")
	}
	result := make([]byte, 0, max(0, expected))
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		left, right, found := strings.Cut(line, ":")
		offset, err := strconv.ParseUint(left, 16, 64)
		if !found || err != nil || offset != uint64(len(result)) {
			return nil, fmt.Errorf("invalid subtitle hex offset")
		}
		right = strings.TrimLeft(right, " ")
		column, _, found := strings.Cut(right, "  ")
		if !found {
			return nil, fmt.Errorf("missing subtitle hex delimiter")
		}
		hexText := strings.ReplaceAll(column, " ", "")
		if len(hexText) == 0 || len(hexText) > 32 || len(hexText)%2 != 0 {
			return nil, fmt.Errorf("invalid subtitle hex row")
		}
		decoded, err := hex.DecodeString(hexText)
		if err != nil || len(result)+len(decoded) > limit {
			return nil, fmt.Errorf("subtitle packet byte limit")
		}
		result = append(result, decoded...)
	}
	if expected >= 0 && len(result) != expected {
		return nil, fmt.Errorf("subtitle packet size mismatch")
	}
	return result, nil
}
