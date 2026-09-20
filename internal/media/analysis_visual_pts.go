package media

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

const maxAnalysisPendingPTS = 4096

var analysisPacketLine = regexp.MustCompile(`^demuxer -> ist_index:([0-9]+):([0-9]+) type:([a-z]+) pkt_pts:(\S+) pkt_pts_time:\S+ pkt_dts:\S+ pkt_dts_time:\S+ duration:\S+ duration_time:\S+(?:\s|$)`)

// analysisPacketPTS binds decoded frames to timestamps that existed before
// ffmpeg's timestamp repair. showinfo alone is insufficient: ffmpeg can invent
// a frame PTS from its previous duration even when -copyts is enabled.
// The grammar is pinned to FFmpeg n9.0.1 fftools/ffmpeg_demux.c SHOW_TS_DEBUG.
// Callers must use -fflags +nofillin-genpts and -debug_ts on the held descriptor.
// Membership proves an original timestamp candidate, not a packet-position
// identity for the decoded frame. It does not rule out every decoder repair.
type analysisPacketPTS struct {
	streamIndex int
	mediaType   string
	timeBase    *big.Rat
	maxPackets  int
	packets     int
	observeOnly bool
	pending     map[string]struct{}
}

// newAnalysisPacketPTSAudit validates original packet timestamps without
// retaining a packet-to-frame join. Resampled audio uses a separate continuity
// proof because resampling does not preserve one packet per output frame.
func newAnalysisPacketPTSAudit(stream Stream, maxPackets int) (*analysisPacketPTS, error) {
	audit, err := newAnalysisPacketPTS(stream, maxPackets)
	if err == nil {
		audit.observeOnly = true
	}
	return audit, err
}

func newAnalysisPacketPTS(stream Stream, maxPackets int) (*analysisPacketPTS, error) {
	base, err := analysisTimeBase(stream.TimeBase)
	if err != nil || stream.Index < 0 || maxPackets <= 0 ||
		(stream.CodecType != "video" && stream.CodecType != "audio") {
		return nil, fmt.Errorf("analysis requires a known source stream time base")
	}
	return &analysisPacketPTS{streamIndex: stream.Index, mediaType: stream.CodecType,
		timeBase: base, maxPackets: maxPackets, pending: make(map[string]struct{})}, nil
}

func (audit *analysisPacketPTS) line(line string) (bool, error) {
	position := strings.Index(line, "demuxer -> ")
	if position < 0 {
		return false, nil
	}
	fields := analysisPacketLine.FindStringSubmatch(strings.TrimSpace(line[position:]))
	if fields == nil {
		return true, fmt.Errorf("invalid analysis source packet timestamp record")
	}
	inputIndex, inputErr := strconv.Atoi(fields[1])
	streamIndex, streamErr := strconv.Atoi(fields[2])
	if inputErr != nil || streamErr != nil || inputIndex != 0 {
		return true, fmt.Errorf("unexpected analysis source input")
	}
	if streamIndex != audit.streamIndex {
		return true, nil
	}
	if fields[3] != audit.mediaType {
		return true, fmt.Errorf("analysis source packet media type changed")
	}
	pts, err := analysisPTSInteger(fields[4])
	if err != nil {
		return true, err
	}
	audit.packets++
	if audit.packets > audit.maxPackets || len(audit.pending) >= maxAnalysisPendingPTS {
		return true, fmt.Errorf("%w: source packet timestamps", ErrAnalysisBudget)
	}
	if audit.observeOnly {
		return true, nil
	}
	key := analysisPTSKey(pts, audit.timeBase)
	if _, exists := audit.pending[key]; exists {
		return true, fmt.Errorf("duplicate analysis source packet timestamp")
	}
	audit.pending[key] = struct{}{}
	return true, nil
}

func (audit *analysisPacketPTS) match(pts int64, base *big.Rat) error {
	key := analysisPTSKey(pts, base)
	if _, exists := audit.pending[key]; !exists {
		return fmt.Errorf("decoded analysis frame has no original packet PTS")
	}
	delete(audit.pending, key)
	return nil
}

func analysisTimeBase(value string) (*big.Rat, error) {
	if len(value) > 64 {
		return nil, fmt.Errorf("invalid analysis time base")
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid analysis time base")
	}
	numerator, err1 := strconv.ParseInt(parts[0], 10, 32)
	denominator, err2 := strconv.ParseInt(parts[1], 10, 32)
	if err1 != nil || err2 != nil || numerator <= 0 || denominator <= 0 {
		return nil, fmt.Errorf("invalid analysis time base")
	}
	return new(big.Rat).SetFrac64(numerator, denominator), nil
}

func analysisPTSInteger(value string) (int64, error) {
	pts, err := strconv.ParseInt(value, 10, 64)
	if err != nil || pts == math.MinInt64 {
		return 0, fmt.Errorf("unknown or invalid analysis source PTS")
	}
	return pts, nil
}

func analysisPTSKey(pts int64, base *big.Rat) string {
	return new(big.Rat).Mul(new(big.Rat).SetInt64(pts), base).RatString()
}

// analysisVisualTicks uses exact rational arithmetic before flooring to the
// source-relative 100 ns grid. FormatStartTicks is never replaced by a frame.
func analysisVisualTicks(pts int64, base *big.Rat, origin int64) (int64, error) {
	value := new(big.Rat).Mul(new(big.Rat).SetInt64(pts), base)
	value.Mul(value, new(big.Rat).SetInt64(TicksPerSecond))
	value.Sub(value, new(big.Rat).SetInt64(origin))
	if value.Sign() < 0 {
		return 0, fmt.Errorf("analysis frame precedes the source timeline")
	}
	ticks := new(big.Int).Quo(value.Num(), value.Denom())
	if !ticks.IsInt64() {
		return 0, fmt.Errorf("analysis source PTS overflows ticks")
	}
	return ticks.Int64(), nil
}
