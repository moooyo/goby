package library

import (
	"errors"
	"time"
)

const TaskAudioWaveformGenerationKey = "media.audio_waveform_generation"
const AudioWaveformItemTimeoutSeconds = 3600

var ErrAudioWaveformConflict = errors.New("audio waveform revision conflict")

type AudioWaveformDetail struct {
	ItemID            string     `json:"ItemId"`
	LibraryID         string     `json:"LibraryId"`
	Name              string     `json:"Name"`
	SourceRevision    string     `json:"SourceRevision"`
	DurationTicks     int64      `json:"DurationTicks"`
	AudioStreamCount  int        `json:"AudioStreamCount"`
	State             string     `json:"State"`
	RequestedRevision string     `json:"RequestedRevision"`
	CompletedRevision string     `json:"CompletedRevision"`
	RunID             string     `json:"RunId"`
	Reused            bool       `json:"Reused"`
	ErrorCode         string     `json:"ErrorCode"`
	RequestedAt       *time.Time `json:"RequestedAt"`
	StartedAt         *time.Time `json:"StartedAt"`
	FinishedAt        *time.Time `json:"FinishedAt"`
}

type AudioWaveformPage struct {
	Items            []AudioWaveformDetail `json:"Items"`
	TotalRecordCount int64                 `json:"TotalRecordCount"`
	StartIndex       int                   `json:"StartIndex"`
	Limit            int                   `json:"Limit"`
}

type AudioWaveformQueueResult struct {
	Queued   int64 `json:"Queued"`
	Replayed bool  `json:"Replayed"`
}

// A detached request snapshot cannot confer execution authority. Every source
// read and publication also requires the process-local task fence capability.
type AudioWaveformJob struct {
	ItemID, LibraryID, Name, RunID, ChildID string
	OperationID                             string
	ActorUserID, ActorSessionID             string
	Revision                                int64
	Force                                   bool
	SourceRevision, MediaSourceID           string
}

type AudioWaveformResult struct {
	Reused    bool
	ErrorCode string
}
