package library

import "context"

// ValidatedPlaybackStop contains immutable cancellation identity only. It is
// minted by the complete current report validation stage, never by a request
// decoder, and is not a reusable AUTH, position, revision or media grant.
type ValidatedPlaybackStop struct {
	owner                    PlaybackOwner
	playID, itemID, sourceID string
	dynamic                  bool
}

func (stop ValidatedPlaybackStop) Owner() PlaybackOwner  { return stop.owner }
func (stop ValidatedPlaybackStop) PlaySessionID() string { return stop.playID }
func (stop ValidatedPlaybackStop) ItemID() string        { return stop.itemID }
func (stop ValidatedPlaybackStop) MediaSourceID() string { return stop.sourceID }
func (stop ValidatedPlaybackStop) IsDynamic() bool       { return stop.dynamic }

// PlaybackValidatedStopAction owns a restrictive stop operation. The factory
// installs only its memory fence; Cancel begins existing resource retirement
// before user data can wait. Finish receives true only for the actual authorized
// committed terminal result, never for an early validation snapshot.
type PlaybackValidatedStopAction struct {
	Cancel func(context.Context) error
	Finish func(terminalCommitted bool)
}

type PlaybackValidatedStopFactory func(ValidatedPlaybackStop) (PlaybackValidatedStopAction, error)

// PlaybackSessionAdmission runs with current authority and canonical SHARE
// locks held. It is memory-only, never enqueues and never performs IO or SQL.
// Its cleanup is called if the final check/commit fails. After success the
// caller retains the actual minted owner until metadata is consumed/rejected.
type PlaybackSessionAdmission func(PlaySession) (cleanup func(), err error)

func validatedPlaybackStopIdentity(owner PlaybackOwner, play PlaySession) (ValidatedPlaybackStop, error) {
	if !validPlaybackOwner(owner) || !validClientPlaybackReference(play.ID) ||
		play.UserID != owner.UserID || play.AuthSessionID != owner.SessionID || play.DeviceID != owner.DeviceID ||
		play.ApplicationKey != owner.ApplicationKey || play.ApplicationClientID != owner.ApplicationClientID ||
		play.ItemID == "" || play.MediaSourceID == "" {
		return ValidatedPlaybackStop{}, ErrNotFound
	}
	if _, err := sourceForItem(play.ItemID, play.MediaSourceID); err != nil {
		return ValidatedPlaybackStop{}, err
	}
	return ValidatedPlaybackStop{owner: owner, playID: play.ID, itemID: play.ItemID, sourceID: play.MediaSourceID, dynamic: play.IsDynamic}, nil
}
