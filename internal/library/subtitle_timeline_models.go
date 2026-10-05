package library

import (
	"errors"
	"time"
)

const TaskSubtitleTimelineGenerationKey = "media.subtitle_timeline_generation"
const SubtitleTimelineItemTimeoutSeconds = 3600

var ErrSubtitleTimelineConflict = errors.New("subtitle timeline revision conflict")

type SubtitleTimelineDetail struct {
	ItemID              string     `json:"ItemId"`
	LibraryID           string     `json:"LibraryId"`
	Name                string     `json:"Name"`
	SourceRevision      string     `json:"SourceRevision"`
	DurationTicks       int64      `json:"DurationTicks"`
	SubtitleStreamCount int        `json:"SubtitleStreamCount"`
	State               string     `json:"State"`
	RequestedRevision   string     `json:"RequestedRevision"`
	CompletedRevision   string     `json:"CompletedRevision"`
	RunID               string     `json:"RunId"`
	Reused              bool       `json:"Reused"`
	ErrorCode           string     `json:"ErrorCode"`
	RequestedAt         *time.Time `json:"RequestedAt"`
	StartedAt           *time.Time `json:"StartedAt"`
	FinishedAt          *time.Time `json:"FinishedAt"`
}

type SubtitleTimelinePage struct {
	Items            []SubtitleTimelineDetail `json:"Items"`
	TotalRecordCount int64                    `json:"TotalRecordCount"`
	StartIndex       int                      `json:"StartIndex"`
	Limit            int                      `json:"Limit"`
}

type SubtitleTimelineQueueResult struct {
	Queued   int64 `json:"Queued"`
	Replayed bool  `json:"Replayed"`
}

// A detached request snapshot cannot confer execution authority. Every source
// read and publication also requires the process-local task fence capability.
type SubtitleTimelineJob struct {
	ItemID, LibraryID, Name, RunID, ChildID string
	OperationID                             string
	ActorUserID, ActorSessionID             string
	Revision                                int64
	Force                                   bool
	SourceRevision, MediaSourceID           string
	BitmapRevision                          string
}

type SubtitleTimelineResult struct {
	Reused    bool
	ErrorCode string
}
