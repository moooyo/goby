package settings

import (
	"fmt"
	"slices"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

func hasConfigurationPolicy(mutation ConfigurationMutation) bool {
	return mutation.EnableRemoteAccess != nil || mutation.LocalNetworkSubnets != nil || mutation.UICulture != nil
}

func applyConfigurationPolicy(previous ServerPolicyValues, mutation ConfigurationMutation) ServerPolicyValues {
	previous.LocalNetworkSubnets = slices.Clone(previous.LocalNetworkSubnets)
	if mutation.EnableRemoteAccess != nil {
		previous.EnableRemoteAccess = *mutation.EnableRemoteAccess
	}
	if mutation.LocalNetworkSubnets != nil {
		previous.LocalNetworkSubnets = slices.Clone(*mutation.LocalNetworkSubnets)
	}
	if mutation.UICulture != nil {
		previous.UICulture = *mutation.UICulture
	}
	return previous
}

func validateConfigurationPolicy(mutation ConfigurationMutation) error {
	if !hasConfigurationPolicy(mutation) {
		return nil
	}
	if mutation.Section != ConfigurationFull && mutation.Section != ConfigurationPartial {
		return &ValidationError{Fields: map[string]string{"Configuration": "access policy belongs to server configuration"}}
	}
	return ValidateServerPolicy(applyConfigurationPolicy(DefaultServerPolicy(), mutation))
}

// configurationPolicyChange is internal to the combined publisher. Its store
// must belong to the same catalog owner as managed settings. Both writer locks
// remain held through the single transaction and post-commit publication.
type configurationPolicyChange struct {
	store         *ServerPolicyStore
	clientAddress string
	mutation      ConfigurationMutation
	previous      ServerPolicySnapshot
	committed     ServerPolicySnapshot
}

func (change *configurationPolicyChange) lockCurrent(tx library.OwnedTx, actor Actor) error {
	previous, err := readServerPolicy(tx.QueryRow("SELECT " + serverPolicyColumns + " FROM server_policy_settings WHERE id=1 FOR UPDATE"))
	if err != nil {
		return err
	}
	if err := checkActor(tx, actor, false); err != nil {
		return err
	}
	// An explicitly authorized compatibility write may close its own access.
	// A request denied by the latest locked policy may never reopen it, even
	// when its original HTTP admission happened before that policy changed.
	if !previous.Allows(change.clientAddress) {
		return ErrServerPolicyDenied
	}
	published := change.store.current.Load()
	if published == nil || published.Revision != previous.Revision || !equalServerPolicy(published.Values, previous.Values) {
		return fmt.Errorf("%w: persisted policy differs from the published application state", ErrStoredSettings)
	}
	change.previous = previous
	return nil
}

func (change *configurationPolicyChange) apply(tx library.OwnedTx, actor Actor) error {
	// Missing policy fields preserve their latest values for both full and
	// partial objects, including full objects produced by older clients.
	values := applyConfigurationPolicy(change.previous.Values, change.mutation)
	if err := ValidateServerPolicy(values); err != nil {
		return err
	}
	if equalServerPolicy(change.previous.Values, values) {
		change.committed = change.previous
		return nil
	}
	if err := validateRevision(change.previous.Revision); err != nil {
		return err
	}
	committed, err := readServerPolicy(tx.QueryRow(`UPDATE server_policy_settings SET revision=revision+1,
		enable_remote_access=$2, local_network_subnets=$3, ui_culture=$4, updated_at=clock_timestamp()
		WHERE id=1 AND revision=$1 RETURNING `+serverPolicyColumns,
		change.previous.Revision, values.EnableRemoteAccess, values.LocalNetworkSubnets, values.UICulture))
	if err != nil {
		return err
	}
	if err := recordServerPolicyActivity(tx, actor, committed.Revision); err != nil {
		return err
	}
	if err := systemevents.Record(tx.Exec, systemevents.ConfigurationChanged); err != nil {
		return err
	}
	change.committed = committed
	return nil
}
