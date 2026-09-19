package identity

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// ProfilePin never enters the generic JSON configuration document. Its current
// client contract accepts four digits and null (or empty string) to clear.
func splitProfilePinPatch(input UserConfigurationPatch) (UserConfigurationPatch, *string, error) {
	result := make(UserConfigurationPatch, len(input))
	var pin *string
	for name, raw := range input {
		if configurationField(name) != "ProfilePin" {
			result[name] = raw
			continue
		}
		if pin != nil {
			return nil, nil, managedUserFieldError("Configuration.ProfilePin", "Supply the profile PIN field exactly once.")
		}
		value := ""
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			if json.Unmarshal(raw, &value) != nil {
				return nil, nil, managedUserFieldError("Configuration.ProfilePin", "Use exactly 4 ASCII digits, or null to clear the PIN.")
			}
		}
		if err := validateLocalSecret(value, true); err != nil {
			return nil, nil, err
		}
		pin = &value
	}
	return result, pin, nil
}

// The caller retains the management lock and current account/credential locks,
// validates the rest of the patch first, and updates configuration_revision once
// for the entire patch. Clearing is possible even if the old master is missing.
func (s *Store) updateProfilePinPreference(ctx context.Context, tx pgx.Tx, userID, pin string) (bool, error) {
	var existing []byte
	if err := tx.QueryRow(ctx, "SELECT profile_pin_ciphertext FROM users WHERE id=$1", userID).Scan(&existing); err != nil {
		return false, err
	}
	if existing == nil && pin == "" {
		return false, nil
	}
	if existing != nil && pin != "" {
		current, err := s.applicationKeyVault.openProfilePin(ctx, userID, existing)
		if err != nil {
			return false, err
		}
		if current == pin {
			return false, nil
		}
	}
	sealed, err := s.prepareProfilePin(ctx, tx, userID, pin)
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE users SET profile_pin_ciphertext=$2,
		local_credentials_revision=local_credentials_revision+1,configuration=configuration-'ProfilePin',
		updated_at=clock_timestamp() WHERE id=$1`, userID, sealed)
	return err == nil, err
}
