// Package media probes local media files and enumerates FFmpeg interfaces.
package media

import "time"

const TicksPerSecond int64 = 10_000_000

// CurrentProbeVersion identifies the media facts stored by this prober.
const CurrentProbeVersion = 4

type Prober struct {
	FFprobePath string
	Timeout     time.Duration
}

type Info struct {
	ProbeVersion     int
	FileChangeTimeNs int64
	Container        string
	DurationTicks    int64
	// Audio-only sources require an exact sample/packet scan before conversion.
	// Unproven sources retain their original metadata and byte-stream access.
	AudioDurationExact      bool
	AudioDurationReason     string
	PresentationOriginTicks int64
	Bitrate                 int64
	Size                    int64
	Streams                 []Stream
	Chapters                []Chapter
}

type Stream struct {
	Index                int
	Codec                string
	CodecType            string
	Language             string
	Title                string
	Width                int
	Height               int
	Channels             int
	SampleRate           int
	Bitrate              int64
	Profile              string
	Level                int
	BitDepth             int
	CodecTag             string
	CodecTagString       string
	PixelFormat          string
	TimeBase             string
	AverageFrameRate     string
	RealFrameRate        string
	ChannelLayout        string
	RefFrames            int
	FieldOrder           string
	IsInterlaced         bool
	InterlaceKnown       bool
	IsAVC                bool
	IsAVCKnown           bool
	IsAttachedPicture    bool
	ColorRange           string
	ColorSpace           string
	ColorTransfer        string
	ColorPrimaries       string
	VideoRange           string
	VideoRangeKnown      bool
	IsDefault            bool
	IsForced             bool
	IsExternal           bool
	IsTextSubtitleStream bool
	AudioTiming          *AudioTiming
}

// AudioTiming describes a fully scanned, continuous audio presentation. Sample
// counts come from decoded frames after the decoder applies priming and padding.
// PacketCount includes discarded packets; first/last positions refer only to
// packets that actually yielded presentation samples. Positions are relative to
// Info.PresentationOriginTicks, and packet starts may be negative after priming.
// Starts round down and ends/maximum durations round up to 100 ns boundaries.
type AudioTiming struct {
	Exact                  bool
	SampleCount            int64
	PacketCount            int64
	StartTicks             int64
	EndTicks               int64
	FirstPacketStartTicks  int64
	LastPacketStartTicks   int64
	MaxPacketDurationTicks int64
}

type Chapter struct {
	StartTicks int64
	EndTicks   int64
	Title      string
}
