package library

import (
	"math"
	"time"
)

// UserDataPatch retains absent, explicit zero/false, and nullable-field removal
// as separate operations. Authority and item visibility are checked by stores.
type UserDataPatch struct {
	PlaybackPositionTicks *int64
	PlayCount             *int
	IsFavorite            *bool
	Played                *bool
	LastPlayedDate        *time.Time
	ClearLastPlayedDate   bool
	Rating                *float64
	ClearRating           bool
	Likes                 *bool
	ClearLikes            bool
	HideFromResume        *bool
}

func (patch UserDataPatch) Validate() error {
	if patch.PlaybackPositionTicks != nil && *patch.PlaybackPositionTicks < 0 ||
		patch.PlayCount != nil && (*patch.PlayCount < 0 || int64(*patch.PlayCount) > math.MaxInt32) ||
		patch.LastPlayedDate != nil && (patch.ClearLastPlayedDate || patch.LastPlayedDate.Year() < 1 || patch.LastPlayedDate.Year() > 9999) ||
		patch.Rating != nil && (patch.ClearRating || math.IsNaN(*patch.Rating) || math.IsInf(*patch.Rating, 0) || *patch.Rating < 0 || *patch.Rating > 10) ||
		patch.Likes != nil && patch.ClearLikes {
		return ErrInvalidInput
	}
	return nil
}
