package library

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
)

// Playback observations are part of the existing report transaction. They
// describe a persisted player observation, not an accepted remote command.
// The locked canonical play prevents retries and client aliases from creating
// another fact; a partial unique index also rejects duplicate observations.
func recordPlaybackObservations(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, previous, current PlaySession) error {
	var action activity.Action
	if previous.StartedAt == nil && current.StartedAt != nil &&
		(current.State == "Playing" || current.State == "Paused") {
		action = activity.ActionPlaybackStarted
	} else if (previous.State == "Prepared" || previous.State == "Playing" || previous.State == "Paused") &&
		current.State == "Stopped" && current.StoppedAt != nil {
		action = activity.ActionPlaybackStopped
	} else {
		return nil
	}
	if previous.ID != current.ID || previous.ItemID != current.ItemID ||
		current.UserID != owner.UserID || current.AuthSessionID != owner.SessionID ||
		current.DeviceID != owner.DeviceID || current.ApplicationKey != owner.ApplicationKey ||
		current.ApplicationClientID != owner.ApplicationClientID {
		return ErrForbidden
	}
	fingerprint, err := activity.PlaybackFingerprint(action, current.ID)
	if err != nil {
		return fmt.Errorf("identify playback observation: %w", err)
	}
	// Authorization already owns the credential and account locks. Resolve
	// their persisted kind and public sidecar identity without accepting any
	// client-reported identity or display value. Final authorization still
	// follows the activity insert, so expiry or revocation rolls it back.
	var kind, userID, applicationID string
	if err := tx.QueryRow(ctx, "SELECT authentication.kind,COALESCE(authentication.user_id,''),"+
		"COALESCE(application.id::text,'') FROM sessions authentication "+
		"LEFT JOIN application_keys application ON application.credential_id=authentication.id "+
		"WHERE authentication.id=$1", owner.SessionID).Scan(&kind, &userID, &applicationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrForbidden
		}
		return fmt.Errorf("resolve playback activity actor: %w", err)
	}
	event := activity.Event{Action: action, Source: activity.SourceEmby,
		Actor:                  activity.Actor{Kind: activity.ActorUser, ID: userID, CredentialID: owner.SessionID},
		Resource:               activity.Resource{Kind: activity.ResourceItem, ID: current.ItemID},
		ObservationFingerprint: fingerprint}
	switch kind {
	case "admin":
		if owner.ApplicationKey || userID != owner.UserID {
			return ErrForbidden
		}
		event.Source = activity.SourceNative
	case "emby":
		if owner.ApplicationKey || userID != owner.UserID {
			return ErrForbidden
		}
	case "application_key":
		if !owner.ApplicationKey || userID != "" || applicationID == "" {
			return ErrForbidden
		}
		event.Actor.Kind, event.Actor.ID = activity.ActorApplicationKey, applicationID
	default:
		return ErrForbidden
	}
	if err := activity.Record(ctx, tx, event); err != nil {
		return fmt.Errorf("persist playback observation: %w", err)
	}
	return nil
}
