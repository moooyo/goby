package media

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math/big"
	"os"
	"strconv"
	"strings"
)

// VideoCopySeekAudio binds an independently selected audio packet to a video
// restart. Its clock is the source clock, not a rounded playback timestamp.
type VideoCopySeekAudio struct {
	StreamIndex         int    `json:"stream_index"`
	Codec               string `json:"codec"`
	TimeBaseNumerator   int64  `json:"time_base_numerator"`
	TimeBaseDenominator int64  `json:"time_base_denominator"`
	SampleRate          int    `json:"sample_rate"`
	Channels            int    `json:"channels"`
	PTS                 int64  `json:"pts"`
	Duration            int64  `json:"duration"`
	PacketSHA256        string `json:"packet_sha256"`
}

func validateVideoCopySeekAudio(audio VideoCopySeekAudio) error {
	if audio.StreamIndex < 0 || audio.Codec != "aac" || audio.TimeBaseNumerator <= 0 || audio.TimeBaseDenominator <= 0 ||
		audio.SampleRate < 8000 || audio.SampleRate > 192000 || audio.Channels < 1 || audio.Channels > 8 ||
		audio.PTS == -1<<63 || audio.Duration <= 0 || !videoSeekSHA256(audio.PacketSHA256) {
		return fmt.Errorf("invalid video copy seek audio packet")
	}
	duration := new(big.Rat).Mul(new(big.Rat).SetInt64(audio.Duration), videoSeekTimeBase(audio.TimeBaseNumerator, audio.TimeBaseDenominator))
	if duration.Cmp(big.NewRat(1, 2)) > 0 {
		return fmt.Errorf("video copy seek audio packet duration is unsupported")
	}
	return nil
}

func analyzeVideoCopySeekAudio(ctx context.Context, executable string, file *os.File, info Info, index VideoSeekIndex) VideoSeekIndex {
	count := 0
	for _, stream := range info.Streams {
		if stream.CodecType != "audio" || stream.Codec != "aac" || stream.IsExternal ||
			!strings.EqualFold(stream.Profile, "LC") || stream.SampleRate < 8000 || stream.SampleRate > 192000 || stream.Channels < 1 || stream.Channels > 8 {
			continue
		}
		count++
		if count > 32 || ctx.Err() != nil {
			break
		}
		timeBase, err := parseVideoSeekTimeBase(stream.TimeBase)
		if err != nil {
			continue
		}
		args := []string{"-v", "level+warning", "-nostdin", "-copyts", "-threads", "1", "-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
			"-i", "/proc/self/fd/3", "-map", "0:" + strconv.Itoa(stream.Index), "-map_metadata", "-1", "-map_chapters", "-1", "-vn", "-sn", "-dn", "-c:a", "copy",
			"-avoid_negative_ts", "disabled", "-f", "framehash", "-hash", "sha256", "pipe:1"}
		output, err := runLimitedFiles(ctx, videoSeekAnalysisTimeout, maxVideoSeekScanBytes, executable, []*os.File{file}, args...)
		if err != nil {
			continue
		}
		proofs, err := parseVideoCopySeekAudio(bytes.NewReader(output), stream, timeBase, index)
		if err != nil {
			continue
		}
		for position, proof := range proofs {
			if proof != nil {
				index.Entries[position].Audio = append(index.Entries[position].Audio, *proof)
			}
		}
	}
	return index
}

func parseVideoCopySeekAudio(input io.Reader, source Stream, timeBase *big.Rat, index VideoSeekIndex) ([]*VideoCopySeekAudio, error) {
	proofs := make([]*VideoCopySeekAudio, len(index.Entries))
	positions := make(map[string]int, len(index.Entries))
	for position, point := range index.Entries {
		if point.PTS == point.DTS {
			positions[VideoSeekPointTime(index, point).RatString()] = position
		}
	}
	streams := []videoSeekHashStream{{seen: make(map[string]bool)}}
	headers := make(map[string]bool)
	bounded := &io.LimitedReader{R: input, N: maxVideoSeekScanBytes + 1}
	reader := bufio.NewReaderSize(bounded, maxVideoSeekLine)
	started, records := false, 0
	var previous *videoSeekHashRecord
	for {
		raw, err := reader.ReadSlice('\n')
		if err == io.EOF && len(raw) == 0 {
			break
		}
		if err != nil || len(raw) > maxVideoSeekLine || bounded.N <= 0 {
			return nil, fmt.Errorf("audio copy seek scan exceeds its line budget")
		}
		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if started {
				return nil, fmt.Errorf("audio copy seek scan changed headers")
			}
			if err := parseVideoSeekHashHeader(line, streams, headers); err != nil {
				return nil, err
			}
			continue
		}
		if !started {
			if !headers["format"] || !headers["version"] || !headers["hash"] || streams[0].media != "audio" || streams[0].codec != source.Codec ||
				streams[0].timeBase == nil || streams[0].timeBase.Cmp(timeBase) != 0 {
				return nil, fmt.Errorf("audio copy seek metadata differs from source")
			}
			started = true
		}
		records++
		if records > maxVideoSeekRecords {
			return nil, fmt.Errorf("audio copy seek scan exceeds its record budget")
		}
		number, record, err := parseVideoSeekHashRecord(line)
		if err != nil || number != 0 || record.pts != record.dts || previous != nil && record.pts <= previous.pts {
			return nil, fmt.Errorf("audio copy seek packet timeline is not ordered")
		}
		previous = &record
		duration, err := strconv.ParseInt(strings.TrimSpace(strings.Split(line, ",")[3]), 10, 64)
		if err != nil || duration <= 0 {
			return nil, fmt.Errorf("audio copy seek packet duration is invalid")
		}
		// Priming, skip-sample, padding, configuration changes, and unknown
		// side data cannot authorize a copied restart at this packet.
		if len(record.sideData) != 0 {
			continue
		}
		packetTime := new(big.Rat).Mul(new(big.Rat).SetInt64(record.pts), timeBase)
		if position, found := positions[packetTime.RatString()]; found {
			proof := VideoCopySeekAudio{StreamIndex: source.Index, Codec: source.Codec, TimeBaseNumerator: timeBase.Num().Int64(), TimeBaseDenominator: timeBase.Denom().Int64(),
				SampleRate: source.SampleRate, Channels: source.Channels, PTS: record.pts, Duration: duration, PacketSHA256: record.hash}
			if validateVideoCopySeekAudio(proof) == nil {
				proofs[position] = &proof
			}
		}
	}
	if !started {
		return nil, fmt.Errorf("audio copy seek scan is empty")
	}
	return proofs, nil
}
