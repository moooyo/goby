// Package media probes local media files and enumerates FFmpeg interfaces.
package media

import "time"

const TicksPerSecond int64 = 10_000_000

// CurrentProbeVersion identifies the media facts stored by this prober.
const CurrentProbeVersion = 8

type Prober struct {
	FFprobePath string
	FFmpegPath  string
	// AnalyzeVideoSeek enables optional, bounded seek indexing during scanning.
	AnalyzeVideoSeek bool
	Timeout          time.Duration
}

type Info struct {
	ProbeVersion     int
	FileChangeTimeNs int64
	Container        string
	DurationTicks    int64
	// Format start is the demuxer's explicitly reported clock origin. It is
	// independent of the first audible sample and remains unknown when absent.
	FormatStartTicks int64
	FormatStartKnown bool
	// Audio-only sources require an exact sample/packet scan before conversion.
	// Unproven sources retain their original metadata and byte-stream access.
	AudioDurationExact      bool
	AudioDurationReason     string
	PresentationOriginTicks int64
	Bitrate                 int64
	Size                    int64
	Streams                 []Stream
	Chapters                []Chapter
	// EmbeddedMusic is nil for legacy snapshots or non-music media. An empty
	// versioned value records that supported format tags were actually inspected.
	EmbeddedMusic *MusicMetadata `json:",omitempty"`
	// VideoSeekIndexes are private decoder restart evidence, never public DTOs.
	VideoSeekIndexes []VideoSeekIndex
}

type Stream struct {
	Index     int
	Codec     string
	CodecType string
	Language  string
	Title     string
	Filename  string
	MIMEType  string
	// SubtitleTag binds an authorized finite subtitle to its indexed bytes.
	// Dynamic sources bind an operator declaration and lease instead; their
	// current generation and authority are checked before reading each source.
	// Primary probing leaves it empty; the library projection supplies it.
	SubtitleTag          string
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
	DolbyVision          *DolbyVisionMetadata
	IsDefault            bool
	IsForced             bool
	IsHearingImpaired    bool
	IsExternal           bool
	IsTextSubtitleStream bool
	AudioTiming          *AudioTiming
}

// DolbyVisionMetadata preserves the probed Dolby Vision profile and layer flags.
// It is present only when all six configuration fields are known and consistent.
// RPU evidence is accepted only after a complete, bounded access-unit scan.
type DolbyVisionMetadata struct {
	Profile             int
	Level               int
	RPUPresent          bool
	ELPresent           bool
	BLPresent           bool
	CompatibilityID     int
	MetadataCompression string
	RPUVerified         bool
	// RPUProfile is the common profile inferred from RPU headers, or zero.
	RPUProfile       int
	RPUResidualMixed bool
	ResidualDisabled bool
	RPUFrameCount    int64
	// Verified RPUs can still report residual or profile inconsistency here.
	RPUValidationReason string
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
