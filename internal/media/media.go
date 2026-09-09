// Package media probes local media files and enumerates FFmpeg interfaces.
package media

import "time"

const TicksPerSecond int64 = 10_000_000

// CurrentProbeVersion identifies the media facts stored by this prober.
const CurrentProbeVersion = 2

type Prober struct {
	FFprobePath string
	Timeout     time.Duration
}

type Info struct {
	ProbeVersion     int
	FileChangeTimeNs int64
	Container        string
	DurationTicks    int64
	Bitrate          int64
	Size             int64
	Streams          []Stream
	Chapters         []Chapter
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
}

type Chapter struct {
	StartTicks int64
	EndTicks   int64
	Title      string
}
