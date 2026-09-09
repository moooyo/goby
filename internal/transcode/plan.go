// Package transcode runs bounded Linux media conversions for authorized local
// sources. Authentication and source validation belong to the API/catalog layer.
package transcode

import "time"

// Hardware selects decoding and encoding independently. Empty selections use
// software. Supported combinations and device identifiers are validated before
// executing FFmpeg; hardware availability is never inferred from this setting.
type Hardware struct {
	Decode string `json:"Decode,omitempty"`
	Encode string `json:"Encode,omitempty"`
	Device string `json:"Device,omitempty"`
}

// Plan is immutable for a job. Stream indexes refer to the opened original file;
// -1 disables a stream. DurationTicks is the full source duration, and StartTicks
// selects the source-time origin of this output. No token, input path, arbitrary
// FFmpeg argument, or client-provided filter expression belongs in a plan.
type Plan struct {
	// Empty mode preserves HLS output. Progressive mode produces one append-only
	// audio stream or fragmented MP4 video, subject to the closed runner matrix.
	OutputMode       string `json:"OutputMode,omitempty"`
	Container        string `json:"Container"`
	VideoCodec       string `json:"VideoCodec,omitempty"`
	AudioCodec       string `json:"AudioCodec,omitempty"`
	VideoStreamIndex int    `json:"VideoStreamIndex"`
	AudioStreamIndex int    `json:"AudioStreamIndex"`
	StartTicks       int64  `json:"StartTicks"`
	DurationTicks    int64  `json:"DurationTicks"`
	// Progressive video uses the probed container clock even when a track is
	// disabled. The explicit known bit distinguishes an actual zero origin from
	// a missing timestamp; neither field is populated from client arguments.
	SourceFormatStartKnown bool    `json:"SourceFormatStartKnown,omitempty"`
	SourceFormatStartTicks int64   `json:"SourceFormatStartTicks,omitempty"`
	Width                  int     `json:"Width,omitempty"`
	Height                 int     `json:"Height,omitempty"`
	FrameRate              float64 `json:"FrameRate,omitempty"`
	VideoBitrate           int64   `json:"VideoBitrate,omitempty"`
	AudioBitrate           int64   `json:"AudioBitrate,omitempty"`
	AudioChannels          int     `json:"AudioChannels,omitempty"`
	AudioSampleRate        int     `json:"AudioSampleRate,omitempty"`
	AudioBitDepth          int     `json:"AudioBitDepth,omitempty"`
	AudioSourceSampleRate  int     `json:"AudioSourceSampleRate,omitempty"`
	AudioSourceSampleCount int64   `json:"AudioSourceSampleCount,omitempty"`
	// Sample seeking decodes from the beginning and trims in the input sample
	// domain. It is used when the source demuxer cannot establish a precise seek.
	AudioSampleSeek bool `json:"AudioSampleSeek,omitempty"`
	SegmentSeconds  int  `json:"SegmentSeconds"`
	// VOD mode uses explicit source-time cut points and globally stable output
	// numbers. Empty mode retains the measured EVENT output used by the engine.
	SegmentMode         string   `json:"SegmentMode,omitempty"`
	SegmentStartNumber  int      `json:"SegmentStartNumber,omitempty"`
	EndTicks            int64    `json:"EndTicks,omitempty"`
	SegmentTimes        string   `json:"SegmentTimes,omitempty"`
	ReferenceStartTicks int64    `json:"ReferenceStartTicks,omitempty"`
	Hardware            Hardware `json:"Hardware"`
}

// Scope fixes every ownership dimension for a conversion. Identifiers are
// correlation metadata supplied only after the caller has been authenticated.
type Scope struct {
	UserID        string `json:"UserId"`
	AuthSessionID string `json:"AuthSessionId"`
	DeviceID      string `json:"DeviceId"`
	PlaySessionID string `json:"PlaySessionId"`
	ItemID        string `json:"ItemId"`
	SourceID      string `json:"SourceId"`
}

type Spec struct {
	Scope       Scope  `json:"Scope"`
	SourceStamp string `json:"SourceStamp"`
	Plan        Plan   `json:"Plan"`
}

// Record is the durable, non-secret status projection. ErrorCode is a bounded
// server-owned classification; raw FFmpeg stderr is not a client-facing error.
type Record struct {
	ID           string
	Spec         Spec
	State        string
	OutputBytes  int64
	ErrorCode    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastAccessAt time.Time
}

// Progress carries FFmpeg output timing only. It cannot change authoritative
// playback position, watched state, or the requesting user's permissions.
type Progress struct {
	OutputTicks int64
	Bytes       int64
	Ended       bool
	// Ready signals that the progressive writer has verified initial media
	// payload beyond its container headers. It is not a playback-state report.
	Ready bool
}

type RunResult struct {
	ExitCode   int
	StderrTail string
}
