package library

import (
	"errors"
	"fmt"
	"time"
)

const TaskBackgroundPreviewGenerationKey = "media.background_preview_generation"
const BackgroundPreviewRuleVersion = 1
const backgroundTicksPerSecond int64 = 10000000

var ErrBackgroundPreviewConflict = errors.New("background preview revision conflict")

type BackgroundPreviewProfile struct {
	DurationSeconds       int `json:"DurationSeconds"`
	MaxWidth              int `json:"MaxWidth"`
	VideoBitrate          int `json:"VideoBitrate"`
	MaxItemRuntimeSeconds int `json:"MaxItemRuntimeSeconds"`
}

type BackgroundPreviewConfiguration struct {
	Revision  string                   `json:"Revision"`
	Profile   BackgroundPreviewProfile `json:"Profile"`
	Defaults  BackgroundPreviewProfile `json:"Defaults"`
	UpdatedAt time.Time                `json:"UpdatedAt"`
}

type BackgroundPreviewConfigurationUpdate struct {
	Revision string                   `json:"Revision"`
	Profile  BackgroundPreviewProfile `json:"Profile"`
}

type BackgroundPreviewEdit struct {
	Revision       string `json:"Revision"`
	SourceRevision string `json:"SourceRevision"`
	StartTicks     *int64 `json:"StartTicks"`
}

type BackgroundPreviewDetail struct {
	ItemID            string     `json:"ItemId"`
	LibraryID         string     `json:"LibraryId"`
	Name              string     `json:"Name"`
	Revision          string     `json:"Revision"`
	SourceRevision    string     `json:"SourceRevision"`
	DurationTicks     int64      `json:"DurationTicks"`
	StartTicks        *int64     `json:"StartTicks"`
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

type BackgroundPreviewPage struct {
	Items            []BackgroundPreviewDetail `json:"Items"`
	TotalRecordCount int64                     `json:"TotalRecordCount"`
	StartIndex       int                       `json:"StartIndex"`
	Limit            int                       `json:"Limit"`
}

type BackgroundPreviewQueueResult struct {
	Queued   int64 `json:"Queued"`
	Replayed bool  `json:"Replayed"`
}

// A job is a detached claim snapshot. Only the task's process-local fence can
// authorize source access and publication; this value alone grants no authority.
type BackgroundPreviewJob struct {
	ItemID, LibraryID, Name, RunID, ChildID string
	OperationID                             string
	ActorUserID, ActorSessionID             string
	Revision                                int64
	Force                                   bool
	SourceRevision, MediaSourceID           string
	ManualStartTicks                        *int64
	StartTicks, DurationTicks               int64
	Profile                                 BackgroundPreviewProfile
}

type BackgroundPreviewResult struct {
	Reused    bool
	ErrorCode string
}

func DefaultBackgroundPreviewProfile() BackgroundPreviewProfile {
	return BackgroundPreviewProfile{DurationSeconds: 25, MaxWidth: 1280, VideoBitrate: 1500000, MaxItemRuntimeSeconds: 1200}
}

func ValidateBackgroundPreviewProfile(profile BackgroundPreviewProfile) error {
	if profile.DurationSeconds < 5 || profile.DurationSeconds > 60 ||
		profile.MaxWidth != 640 && profile.MaxWidth != 960 && profile.MaxWidth != 1280 && profile.MaxWidth != 1920 ||
		profile.VideoBitrate < 250000 || profile.VideoBitrate > 8000000 ||
		profile.MaxItemRuntimeSeconds < 30 || profile.MaxItemRuntimeSeconds > 7200 {
		return fmt.Errorf("%w: invalid background preview profile", ErrInvalidInput)
	}
	return nil
}

// SelectBackgroundPreviewInterval uses explicit editorial input before marker
// evidence and a deterministic early-film fallback. No shot quality is inferred.
func SelectBackgroundPreviewInterval(durationTicks int64, manual *int64, intro *IntroInterval, credits *CreditsPoint) (int64, int64, error) {
	return selectBackgroundPreviewInterval(durationTicks, manual, intro, credits, 25*backgroundTicksPerSecond)
}

func selectBackgroundPreviewInterval(durationTicks int64, manual *int64, intro *IntroInterval, credits *CreditsPoint, desired int64) (int64, int64, error) {
	if durationTicks <= 0 || desired <= 0 {
		return 0, 0, ErrInvalidInput
	}
	end := durationTicks
	if validCreditsPoint(credits, durationTicks) {
		end = credits.StartTicks
	}
	if end <= 0 {
		return 0, 0, ErrInvalidInput
	}
	start := durationTicks / 20
	if start < 30*backgroundTicksPerSecond {
		start = 30 * backgroundTicksPerSecond
	}
	if start > 300*backgroundTicksPerSecond {
		start = 300 * backgroundTicksPerSecond
	}
	hasIntro := validIntroInterval(intro, durationTicks)
	if hasIntro {
		start = intro.EndTicks + 3*backgroundTicksPerSecond
	}
	if manual != nil {
		if *manual < 0 || *manual >= end {
			return 0, 0, fmt.Errorf("%w: manual start must precede credits and media end", ErrInvalidInput)
		}
		start = *manual
	}
	if start >= end {
		if hasIntro {
			return 0, 0, fmt.Errorf("%w: no content remains after the intro", ErrInvalidInput)
		}
		start = 0
	}
	// Keep a valid manual/intro start, shortening the clip at the end. The
	// fallback moves earlier only for a source shorter than its default start.
	length := desired
	if end-start < length {
		length = end - start
	}
	return start, length, nil
}
