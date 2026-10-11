package identity

import (
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// RevalidateSessionInTransaction preserves RevalidateSession's trusted-principal
// contract while reading through a caller-owned transaction. It does not acquire
// row locks or commit the transaction. Callers that retain these facts through
// subsequent waits must lock the account, credential and client before reading.
func RevalidateSessionInTransaction(ctx context.Context, tx pgx.Tx, previous Principal) (Principal, error) {
	return revalidateSession(ctx, tx, previous)
}

// SessionObservation retains parsed policy facts from one successful session
// revalidation. It is valid only while the caller keeps the observed account,
// credential and client facts fixed under row locks in that same transaction.
// It must never be retained across transactions or authorization stages.
type SessionObservation struct {
	policy         ManagedPolicy
	expiresAt      time.Time
	valid          bool
	applicationKey bool
}

// RevalidateSessionObservationInTransaction returns the same full principal as
// RevalidateSessionInTransaction together with its parsed policy observation.
// The principal owns its JSON snapshots; the observation retains no aliases to
// them. This function neither acquires row locks nor completes the transaction.
func RevalidateSessionObservationInTransaction(ctx context.Context, tx pgx.Tx, previous Principal) (Principal, SessionObservation, error) {
	return revalidateSessionObservation(ctx, tx, previous, true)
}

// Policy returns an independent copy of the parsed ordinary-login policy.
// Application-key observations have no user policy; their existing authority
// boundary must be handled separately by the caller.
func (observation SessionObservation) Policy() ManagedPolicy {
	policy := observation.policy
	policy.BlockedTags = slices.Clone(policy.BlockedTags)
	policy.IncludeTags = slices.Clone(policy.IncludeTags)
	policy.AccessSchedules = slices.Clone(policy.AccessSchedules)
	policy.BlockUnratedItems = slices.Clone(policy.BlockUnratedItems)
	policy.RestrictedFeatures = slices.Clone(policy.RestrictedFeatures)
	policy.EnableContentDeletionFromFolders = slices.Clone(policy.EnableContentDeletionFromFolders)
	policy.EnabledFolders = slices.Clone(policy.EnabledFolders)
	policy.ExcludedSubFolders = slices.Clone(policy.ExcludedSubFolders)
	policy.EnabledDevices = slices.Clone(policy.EnabledDevices)
	if policy.MaxParentalRating != nil {
		rating := *policy.MaxParentalRating
		policy.MaxParentalRating = &rating
	}
	return policy
}

// ValidateAt repeats the time-dependent decisions after a later row wait.
// Fixed device, peer and login-lockout facts were checked when this observation
// was created and remain protected by its caller's account and credential locks.
func (observation SessionObservation) ValidateAt(observedAt time.Time) error {
	if !observation.valid {
		return ErrUnauthorized
	}
	if observation.applicationKey {
		return nil
	}
	if !observedAt.Before(observation.expiresAt) || !observation.policy.AllowsAccessAt(observedAt) {
		return ErrUnauthorized
	}
	return nil
}

// ValidateRevalidatedSessionAt refreshes only clock-dependent policy decisions
// for a principal whose database authority has already been checked. It does not
// authenticate an identifier or check revocation. Transaction callers must keep
// its account and credential facts protected by row locks until this check.
func ValidateRevalidatedSessionAt(principal Principal, observedAt time.Time) error {
	_, err := observeRevalidatedSessionAt(principal, observedAt)
	return err
}

func observeRevalidatedSessionAt(principal Principal, observedAt time.Time) (SessionObservation, error) {
	if principal.IsApplicationKey() {
		return SessionObservation{valid: true, applicationKey: true}, nil
	}
	policy, err := ParseRuntimePolicy(principal.User.Policy)
	if err != nil || !observedAt.Before(principal.ExpiresAt) ||
		!parsedLoginPolicyAllows(policy, principal.User.Policy, principal.Client.DeviceID, observedAt) ||
		(!policy.EnableRemoteAccess && !IsLocalPeer(principal.PeerIP)) {
		return SessionObservation{}, ErrUnauthorized
	}
	return SessionObservation{policy: policy, expiresAt: principal.ExpiresAt, valid: true}, nil
}
