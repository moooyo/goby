package activity

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// PlaybackFingerprint identifies one observed transition of a canonical play.
// Callers must supply the stored play id, never a client correlation reference.
// The digest permits bounded retention and duplicate detection without exposing
// a playback, authentication, or device identifier to activity readers.
func PlaybackFingerprint(action Action, playSessionID string) (string, error) {
	if action != ActionPlaybackStarted && action != ActionPlaybackStopped ||
		!validIdentifier(playSessionID, false) || !strings.HasPrefix(playSessionID, "play_") {
		return "", ErrInvalidInput
	}
	value := sha256.Sum256([]byte("goby/playback-observation/v1\x00" + string(action) + "\x00" + playSessionID))
	return hex.EncodeToString(value[:]), nil
}

// ValidPlaybackObservation checks the fixed playback fact shape. It provides
// no actor authorization and cannot prove that a player performed a command.
// The playback owner records it only after persisting the observed transition
// in the same transaction, before that transaction's final authorization check.
func ValidPlaybackObservation(event Event) bool {
	if event.Action != ActionPlaybackStarted && event.Action != ActionPlaybackStopped ||
		event.Severity != "" && event.Severity != SeverityInfo ||
		event.Source != SourceNative && event.Source != SourceEmby ||
		!validActor(event.Actor) || event.Actor.Kind == ActorSystem ||
		!validIdentifier(event.Actor.CredentialID, false) ||
		event.Source == SourceNative && event.Actor.Kind != ActorUser ||
		event.Resource.Kind != ResourceItem || !validIdentifier(event.Resource.ID, false) ||
		!validIdentifier(event.RequestID, true) ||
		event.Revision != 0 || event.PreviousRevision != 0 || event.Count != 0 ||
		event.State != "" || len(event.ChangedFields) != 0 ||
		!validObservationFingerprint(event.ObservationFingerprint) {
		return false
	}
	return true
}
