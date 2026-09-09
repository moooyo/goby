package library

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

const maxPlayerStateBytes = 2048

// PlayerStateUpdate contains optional display hints reported by a player.
// Missing fields retain their previous values; explicit false, zero, and -1
// values remain distinguishable. These hints never authorize media operations
// or replace server-owned position, pause state, item, source, or identity.
type PlayerStateUpdate struct {
	CanSeek             *bool    `json:"CanSeek,omitempty"`
	IsMuted             *bool    `json:"IsMuted,omitempty"`
	VolumeLevel         *int     `json:"VolumeLevel,omitempty"`
	AudioStreamIndex    *int     `json:"AudioStreamIndex,omitempty"`
	SubtitleStreamIndex *int     `json:"SubtitleStreamIndex,omitempty"`
	PlayMethod          *string  `json:"PlayMethod,omitempty"`
	RepeatMode          *string  `json:"RepeatMode,omitempty"`
	PlaybackRate        *float64 `json:"PlaybackRate,omitempty"`
	Shuffle             *bool    `json:"Shuffle,omitempty"`
	SubtitleOffset      *int     `json:"SubtitleOffset,omitempty"`
}

// PlayerState is the validated, cumulative subset of client hints stored for a
// playback session. A nil field is still unknown, not an invented zero value.
type PlayerState = PlayerStateUpdate

func validatePlayerState(state PlayerState) error {
	if state.VolumeLevel != nil && (*state.VolumeLevel < 0 || *state.VolumeLevel > 100) {
		return fmt.Errorf("%w: player volume must be between 0 and 100", ErrInvalidInput)
	}
	for _, value := range []*int{state.AudioStreamIndex, state.SubtitleStreamIndex} {
		if value != nil && (*value < -1 || int64(*value) > math.MaxInt32) {
			return fmt.Errorf("%w: player stream indices must be between -1 and int32 maximum", ErrInvalidInput)
		}
	}
	if state.SubtitleOffset != nil && (int64(*state.SubtitleOffset) < math.MinInt32 || int64(*state.SubtitleOffset) > math.MaxInt32) {
		return fmt.Errorf("%w: player subtitle offset must fit int32", ErrInvalidInput)
	}
	if state.PlayMethod != nil {
		switch *state.PlayMethod {
		case "DirectPlay", "DirectStream", "Transcode":
		default:
			return fmt.Errorf("%w: unsupported player play method", ErrInvalidInput)
		}
	}
	if state.RepeatMode != nil {
		switch *state.RepeatMode {
		case "RepeatNone", "RepeatAll", "RepeatOne":
		default:
			return fmt.Errorf("%w: unsupported player repeat mode", ErrInvalidInput)
		}
	}
	if state.PlaybackRate != nil && (math.IsNaN(*state.PlaybackRate) || math.IsInf(*state.PlaybackRate, 0) || *state.PlaybackRate <= 0 || *state.PlaybackRate > 10) {
		return fmt.Errorf("%w: player playback rate must be finite and greater than 0 through 10", ErrInvalidInput)
	}
	return nil
}

func encodePlayerStateUpdate(update *PlayerStateUpdate) ([]byte, error) {
	if update == nil {
		return []byte("{}"), nil
	}
	if err := validatePlayerState(*update); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(update)
	if err != nil || len(encoded) > maxPlayerStateBytes {
		return nil, fmt.Errorf("%w: player state cannot be encoded within its limit", ErrInvalidInput)
	}
	return encoded, nil
}

func decodePlayerState(encoded []byte) (PlayerState, error) {
	invalid := func() (PlayerState, error) {
		return PlayerState{}, fmt.Errorf("%w: stored player state is invalid", ErrUnavailable)
	}
	encoded = bytes.TrimSpace(encoded)
	if len(encoded) == 0 || len(encoded) > maxPlayerStateBytes || encoded[0] != '{' {
		return invalid()
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &values); err != nil {
		return invalid()
	}
	for key, value := range values {
		switch key {
		case "CanSeek", "IsMuted", "VolumeLevel", "AudioStreamIndex", "SubtitleStreamIndex", "PlayMethod", "RepeatMode", "PlaybackRate", "Shuffle", "SubtitleOffset":
		default:
			return invalid()
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return invalid()
		}
	}
	var state PlayerState
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return invalid()
	}
	if _, err := decoder.Token(); err != io.EOF {
		return invalid()
	}
	if err := validatePlayerState(state); err != nil {
		return invalid()
	}
	return state, nil
}
