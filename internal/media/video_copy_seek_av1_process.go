package media

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	maxVideoCopySeekAV1ProbePackets = 8
	maxVideoCopySeekAV1ProbeBytes   = 16 * 1024 * 1024
)

// analyzeAV1CopySeekIndex treats container key flags only as candidates. A
// retained packet must pair with a decoded picture and contain its own complete
// AV1 restart state. Separate copy branches check side data on every packet and
// require an unchanged sequence header throughout the source.
func analyzeAV1CopySeekIndex(ctx context.Context, executable, probe string, file *os.File, base VideoSeekIndex, maxEntries int) (VideoSeekIndex, error) {
	if file == nil || VideoSeekCodec(base) != "av1" || base.StreamIndex < 0 ||
		maxEntries <= 0 || maxEntries > MaxVideoSeekEntries {
		return VideoSeekIndex{}, fmt.Errorf("invalid AV1 copy seek analysis configuration")
	}
	resolvedProbe, probeIdentity, err := VideoSeekToolIdentity(ctx, probe)
	if err != nil {
		return VideoSeekIndex{}, err
	}
	stream := "0:" + strconv.Itoa(base.StreamIndex)
	args := []string{"-v", "level+warning", "-nostdin", "-xerror", "-max_alloc", strconv.Itoa(MaxVideoSeekAllocationBytes),
		"-copyts", "-threads", "1", "-max_pixels", strconv.FormatInt(MaxVideoSeekPixels, 10),
		"-filter_threads", "1", "-filter_complex_threads", "1", "-skip_frame", "nokey",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-i", "/proc/self/fd/3", "-map", stream, "-map", stream, "-map", stream, "-map", stream,
		"-c:v:0", "copy", "-copyinkf:v:0", "-copypriorss:v:0", "1", "-bsf:v:0", "noise=drop=not(key)",
		"-c:v:1", "rawvideo", "-threads:v:1", "1", "-fps_mode:v:1", "passthrough", "-enc_time_base:v:1", "demux",
		"-c:v:2", "copy", "-copyinkf:v:2", "-copypriorss:v:2", "1",
		"-c:v:3", "copy", "-copyinkf:v:3", "-copypriorss:v:3", "1", "-bsf:v:3", "filter_units=pass_types=1",
		"-f", "framehash", "-hash", "sha256", "pipe:1"}
	output, err := runVideoCopySeekAV1Command(ctx, videoSeekAnalysisTimeout, maxVideoSeekScanBytes, executable, file, args...)
	if err != nil {
		return VideoSeekIndex{}, err
	}
	index, err := parseVideoCopySeekAV1FrameHash(bytes.NewReader(output), base, maxEntries)
	if err != nil {
		return VideoSeekIndex{}, err
	}
	verified := make([]VideoSeekPoint, 0, len(index.Entries))
	for _, point := range index.Entries {
		matched, err := probeVideoCopySeekAV1Point(ctx, resolvedProbe, file, index, point)
		if err != nil {
			return VideoSeekIndex{}, err
		}
		if matched {
			point.PacketSHA256 = point.CodedSHA256
			verified = append(verified, point)
		}
	}
	afterPath, afterIdentity, err := VideoSeekToolIdentity(ctx, probe)
	if err != nil || afterPath != resolvedProbe || afterIdentity != probeIdentity {
		return VideoSeekIndex{}, fmt.Errorf("AV1 copy seek probe changed during analysis")
	}
	index.Entries = verified
	index.PacketRestartChecked = true
	if err := ValidateVideoSeekIndex(index); err != nil {
		return VideoSeekIndex{}, err
	}
	return index, nil
}

// The shared limited runner does not reject diagnostics on a successful exit.
// Restart evidence also rejects errors reported by a decoder or demuxer that
// subsequently managed to finish the requested output.
func runVideoCopySeekAV1Command(ctx context.Context, timeout time.Duration, maxOutput int, executable string, file *os.File, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	processContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout := &limitedOutput{limit: maxOutput, cancel: cancel}
	stderr := &limitedOutput{limit: maxProcessStderr, cancel: cancel}
	command := exec.CommandContext(processContext, executable, args...)
	command.ExtraFiles = []*os.File{file}
	command.Env = mediaProbeEnvironment()
	command.Stdout, command.Stderr = stdout, stderr
	command.WaitDelay = time.Second
	retired, err := startMediaProcess(command)
	if err != nil {
		return nil, err
	}
	waitErr := errors.Join(<-retired, command.Wait())
	if stdout.exceeded || stderr.exceeded {
		return nil, ErrOutputLimit
	}
	if err := processContext.Err(); err != nil {
		return nil, err
	}
	if waitErr != nil {
		return nil, fmt.Errorf("execute AV1 copy seek analysis: %w", waitErr)
	}
	for _, severity := range []string{"[error]", "[fatal]", "[panic]"} {
		if strings.Contains(stderr.buffer.String(), severity) {
			return nil, fmt.Errorf("AV1 copy seek analysis reported an error")
		}
	}
	return stdout.buffer.Bytes(), nil
}

func parseVideoCopySeekAV1FrameHash(input io.Reader, base VideoSeekIndex, maxEntries int) (VideoSeekIndex, error) {
	if input == nil || VideoSeekCodec(base) != "av1" || maxEntries <= 0 || maxEntries > MaxVideoSeekEntries ||
		base.TimeBaseNumerator <= 0 || base.TimeBaseDenominator <= 0 || base.DurationTicks <= 0 {
		return VideoSeekIndex{}, fmt.Errorf("invalid AV1 copy seek parser configuration")
	}
	index := base
	index.Entries = nil
	index.ParameterSetsSHA256 = ""
	index.NALScopeChecked = false
	index.PacketSideDataChecked = false
	index.PacketRestartChecked = false
	index.DecodedFrameBytes = 0
	streams := make([]videoSeekHashStream, 4)
	for number := range streams {
		streams[number].seen = make(map[string]bool)
	}
	headers := make(map[string]bool)
	bounded := &io.LimitedReader{R: input, N: maxVideoSeekScanBytes + 1}
	reader := bufio.NewReaderSize(bounded, maxVideoSeekLine)
	started, records := false, 0
	var lastRetained *big.Rat
	interval := new(big.Rat)
	if maxEntries > 1 {
		interval.SetFrac(big.NewInt(base.DurationTicks), new(big.Int).Mul(big.NewInt(TicksPerSecond), big.NewInt(int64(maxEntries-1))))
	}
	for {
		raw, err := reader.ReadSlice('\n')
		if err == io.EOF && len(raw) == 0 {
			break
		}
		if err != nil || len(raw) > maxVideoSeekLine || bounded.N <= 0 {
			return VideoSeekIndex{}, fmt.Errorf("AV1 copy seek output is truncated or exceeds its byte budget")
		}
		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if started {
				return VideoSeekIndex{}, fmt.Errorf("AV1 copy seek headers changed after records")
			}
			if err := parseVideoSeekHashHeader(line, streams, headers); err != nil {
				return VideoSeekIndex{}, err
			}
			continue
		}
		if !started {
			if err := validateVideoSeekHashHeaders(streams, headers, base); err != nil {
				return VideoSeekIndex{}, err
			}
			started = true
		}
		records++
		if records > maxVideoSeekRecords {
			return VideoSeekIndex{}, fmt.Errorf("AV1 copy seek output exceeds its record budget")
		}
		number, record, err := parseVideoSeekHashRecord(line)
		if err != nil || number >= len(streams) {
			return VideoSeekIndex{}, fmt.Errorf("invalid AV1 copy seek record")
		}
		stream := &streams[number]
		if stream.previous != nil && (number < 2 && record.pts <= stream.previous.pts || record.dts <= stream.previous.dts) {
			return VideoSeekIndex{}, fmt.Errorf("AV1 copy seek records are not strictly ordered")
		}
		copyRecord := record
		stream.previous = &copyRecord
		if number != 1 && !videoSeekSupportedPacketSideData(record.sideData) {
			return VideoSeekIndex{}, fmt.Errorf("AV1 copy seek packet has unsupported side data")
		}
		if number == 2 {
			continue
		}
		if number == 3 {
			if index.ParameterSetsSHA256 != "" && index.ParameterSetsSHA256 != record.hash {
				return VideoSeekIndex{}, fmt.Errorf("AV1 copy seek sequence headers are not static")
			}
			index.ParameterSetsSHA256 = record.hash
			continue
		}
		if number == 1 {
			if index.DecodedFrameBytes != 0 && index.DecodedFrameBytes != record.size {
				return VideoSeekIndex{}, fmt.Errorf("AV1 copy seek decoded representation changed")
			}
			index.DecodedFrameBytes = record.size
		}
		stream.pending = append(stream.pending, record)
		for len(streams[0].pending) > 0 && len(streams[1].pending) > 0 {
			coded, decoded := streams[0].pending[0], streams[1].pending[0]
			codedTime := new(big.Rat).Mul(new(big.Rat).SetInt64(coded.pts), streams[0].timeBase)
			decodedTime := new(big.Rat).Mul(new(big.Rat).SetInt64(decoded.pts), streams[1].timeBase)
			switch codedTime.Cmp(decodedTime) {
			case -1:
				streams[0].pending = streams[0].pending[1:]
				continue
			case 1:
				streams[1].pending = streams[1].pending[1:]
				continue
			}
			streams[0].pending = streams[0].pending[1:]
			streams[1].pending = streams[1].pending[1:]
			if len(index.Entries) < maxEntries && (lastRetained == nil || new(big.Rat).Sub(codedTime, lastRetained).Cmp(interval) >= 0) {
				index.Entries = append(index.Entries, VideoSeekPoint{PTS: coded.pts, DTS: coded.dts, CodedSHA256: coded.hash, DecodedSHA256: decoded.hash})
				lastRetained = codedTime
			}
		}
		if len(streams[0].pending)+len(streams[1].pending) > maxVideoSeekPending {
			return VideoSeekIndex{}, fmt.Errorf("AV1 copy seek output exceeds its pending record budget")
		}
	}
	if !started || streams[2].previous == nil || streams[3].previous == nil || len(index.Entries) == 0 {
		return VideoSeekIndex{}, fmt.Errorf("AV1 copy seek output has incomplete packet evidence")
	}
	index.PacketSideDataChecked = true
	return index, nil
}

func probeVideoCopySeekAV1Point(ctx context.Context, executable string, file *os.File, index VideoSeekIndex, point VideoSeekPoint) (bool, error) {
	seconds := VideoSeekPointTime(index, point).FloatString(12)
	args := []string{"-v", "level+warning", "-max_alloc", strconv.Itoa(MaxVideoSeekAllocationBytes),
		"-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-select_streams", strconv.Itoa(index.StreamIndex), "-read_intervals", seconds + "%+#" + strconv.Itoa(maxVideoCopySeekAV1ProbePackets),
		"-show_packets", "-show_data", "-of", "json", "-i", "/proc/self/fd/3"}
	output, err := runVideoCopySeekAV1Command(ctx, videoSeekProofTimeout, maxVideoCopySeekAV1ProbeBytes, executable, file, args...)
	if err != nil {
		return false, err
	}
	var result struct {
		Packets []struct {
			StreamIndex *int              `json:"stream_index"`
			PTS         *int64            `json:"pts"`
			DTS         *int64            `json:"dts"`
			Size        string            `json:"size"`
			Flags       string            `json:"flags"`
			Data        string            `json:"data"`
			SideData    []json.RawMessage `json:"side_data_list"`
		} `json:"packets"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return false, fmt.Errorf("decode AV1 copy seek packet probe: %w", err)
	}
	if len(result.Packets) == 0 || len(result.Packets) > maxVideoCopySeekAV1ProbePackets {
		return false, fmt.Errorf("AV1 copy seek packet probe exceeded its packet budget or returned no packets")
	}
	matched := false
	for _, packet := range result.Packets {
		if packet.StreamIndex == nil || *packet.StreamIndex != index.StreamIndex || packet.PTS == nil || packet.DTS == nil {
			return false, fmt.Errorf("AV1 copy seek packet probe has incomplete timestamps or stream identity")
		}
		if *packet.PTS != point.PTS || *packet.DTS != point.DTS {
			continue
		}
		if matched {
			return false, fmt.Errorf("AV1 copy seek packet probe has ambiguous timestamps")
		}
		matched = true
		size, err := strconv.ParseInt(packet.Size, 10, 64)
		if err != nil || size <= 0 || size > int64(maxVideoCopySeekAV1ProbeBytes) || len(packet.SideData) != 0 || !strings.Contains(packet.Flags, "K") {
			return false, nil
		}
		data, err := videoCopySeekPacketData(packet.Data)
		if err != nil || int64(len(data)) != size || fmt.Sprintf("%x", sha256.Sum256(data)) != point.CodedSHA256 || !videoCopySeekAV1RestartPacket(data) {
			return false, nil
		}
	}
	return matched, nil
}
