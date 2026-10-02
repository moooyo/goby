package identity

import (
	"context"
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

// ValidateRevalidatedSessionAt refreshes only clock-dependent policy decisions
// for a principal whose database authority has already been checked. It does not
// authenticate an identifier or check revocation. Transaction callers must keep
// its account and credential facts protected by row locks until this check.
func ValidateRevalidatedSessionAt(principal Principal, observedAt time.Time) error {
	if principal.IsApplicationKey() {
		return nil
	}
	policy, err := ParseRuntimePolicy(principal.User.Policy)
	if err != nil || !observedAt.Before(principal.ExpiresAt) ||
		!loginPolicyAllows(principal.User.Policy, principal.Client.DeviceID, observedAt) ||
		(!policy.EnableRemoteAccess && !IsLocalPeer(principal.PeerIP)) {
		return ErrUnauthorized
	}
	return nil
}
