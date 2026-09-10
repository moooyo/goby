package settings

import (
	"context"
	"errors"
	"fmt"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// GetConfiguration distinguishes an authenticated viewer from an invalid
// credential. Viewers receive no settings or initialization information; the
// HTTP adapter may project the observed empty total-configuration object.
func (s *Store) GetConfiguration(ctx context.Context, actor Actor) (Configuration, error) {
	if actor.Audience != identity.AdministratorEmby {
		return Configuration{}, identity.ErrUnauthorized
	}
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	var result Configuration
	err := s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		err := checkActor(tx, actor, true)
		if errors.Is(err, identity.ErrClientSessionForbidden) {
			// The first query established a valid viewer. Recheck its lifetime
			// before returning an empty view; never turn Unauthorized into it.
			final := checkActor(tx, actor, false)
			if final != nil && !errors.Is(final, identity.ErrClientSessionForbidden) {
				return final
			}
			return nil
		}
		if err != nil {
			return err
		}
		initialized, err := readInitialized(tx)
		if err != nil {
			return err
		}
		result = Configuration{Snapshot: s.Snapshot(), StartupWizardCompleted: initialized, CanManage: true}
		return checkActor(tx, actor, false)
	})
	if err != nil {
		return Configuration{}, err
	}
	return result, nil
}

// ApplyConfiguration has no client revision. It applies only the selected
// section to the latest locked record, preserving unrelated native fields.
// Native Update retains its separate revision-CAS and replacement contract.
func (s *Store) ApplyConfiguration(ctx context.Context, actor Actor, mutation ConfigurationMutation) (Snapshot, error) {
	if actor.Audience != identity.AdministratorEmby {
		return Snapshot{}, identity.ErrUnauthorized
	}
	mutation.ServerName = clonePointer(mutation.ServerName)
	mutation.StartupWizardCompleted = clonePointer(mutation.StartupWizardCompleted)
	if err := validateConfigurationMutation(mutation); err != nil {
		return Snapshot{}, err
	}
	return s.change(ctx, actor, nil, func(tx library.OwnedTx, previous settingsRecord) (settingsRecord, error) {
		if mutation.StartupWizardCompleted != nil {
			actual, err := readInitialized(tx)
			if err != nil {
				return settingsRecord{}, err
			}
			if actual != *mutation.StartupWizardCompleted {
				return settingsRecord{}, &ValidationError{Fields: map[string]string{
					"IsStartupWizardCompleted": "initialization state is read-only and must match the server",
				}}
			}
		}
		switch mutation.Section {
		case ConfigurationFull:
			previous.Overrides.ServerName = clonePointer(mutation.ServerName)
			previous.ServerNameMode = configurationNameMode(mutation.ServerName)
		case ConfigurationPartial:
			if mutation.ServerNamePresent {
				previous.Overrides.ServerName = clonePointer(mutation.ServerName)
				previous.ServerNameMode = configurationNameMode(mutation.ServerName)
			}
		case ConfigurationEncoding:
			// Absence in a complete named encoding object resets only this
			// compatibility ceiling. Native MaxWidth is never changed here.
			previous.Encoding.TranscodingMaxWidth = mutation.TranscodingMaxWidth
		}
		return previous, nil
	})
}

func configurationNameMode(value *string) ServerNameMode {
	if value == nil {
		return ServerNameUnset
	}
	if *value == "" {
		return ServerNameEmpty
	}
	return ServerNameCustom
}

func validateConfigurationMutation(value ConfigurationMutation) error {
	invalid := func(message string) error {
		return &ValidationError{Fields: map[string]string{"Configuration": message}}
	}
	if !value.ServerNamePresent && value.ServerName != nil {
		return invalid("a name value requires its explicit presence marker")
	}
	if !value.TranscodingMaxWidthPresent && value.TranscodingMaxWidth != 0 {
		return invalid("an encoding width value requires its explicit presence marker")
	}
	switch value.Section {
	case ConfigurationFull, ConfigurationPartial:
		if value.TranscodingMaxWidthPresent || value.TranscodingMaxWidth != 0 {
			return invalid("server configuration cannot include named encoding settings")
		}
		if value.ServerNamePresent {
			return validateName(configurationNameMode(value.ServerName), value.ServerName)
		}
	case ConfigurationEncoding:
		if value.ServerNamePresent || value.ServerName != nil || value.StartupWizardCompleted != nil {
			return invalid("named encoding configuration cannot include server settings")
		}
		return validateEncoding(Encoding{TranscodingMaxWidth: value.TranscodingMaxWidth})
	default:
		return invalid("use a supported configuration section")
	}
	return nil
}

// Keep the same persisted predicate as identity.Store.Initialized: a surviving
// account also closes setup even if no setup_completed setting exists.
func readInitialized(tx library.OwnedTx) (bool, error) {
	var initialized bool
	err := tx.QueryRow(`SELECT
		EXISTS (SELECT 1 FROM server_settings WHERE key = 'setup_completed' AND value = 'true')
		OR EXISTS (SELECT 1 FROM users)`).Scan(&initialized)
	if err != nil {
		return false, fmt.Errorf("read configuration initialization state: %w", err)
	}
	return initialized, nil
}
