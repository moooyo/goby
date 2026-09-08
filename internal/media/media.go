// Package media probes local media files and enumerates FFmpeg interfaces.
package media

import "time"

const TicksPerSecond int64 = 10_000_000

type Prober struct {
	FFprobePath string
	Timeout     time.Duration
}

type Info struct {
	Container     string
	DurationTicks int64
	Bitrate       int64
	Size          int64
	Streams       []Stream
	Chapters      []Chapter
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
	PixelFormat          string
	AverageFrameRate     string
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
