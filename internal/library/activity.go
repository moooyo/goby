package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
)

type catalogAdministrator struct {
	actor    identity.Principal
	audience identity.AdministratorAudience
}

func (administrator *catalogAdministrator) check(ctx context.Context, tx identity.AuthorizationTx, lock bool) error {
	if administrator == nil {
		return nil
	}
	if err := identity.CheckAdministrator(ctx, tx, administrator.actor, administrator.audience, lock); err != nil {
		if errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, identity.ErrClientSessionForbidden) {
			return errors.Join(ErrForbidden, err)
		}
		return err
	}
	return nil
}

func (administrator *catalogAdministrator) event(action activity.Action, resource activity.Resource) activity.Event {
	event := catalogSystemEvent(action, resource)
	if administrator == nil {
		return event
	}
	event.Source = activity.SourceNative
	if administrator.audience == identity.AdministratorEmby {
		event.Source = activity.SourceEmby
	}
	event.Actor = activity.Actor{Kind: activity.ActorUser, ID: administrator.actor.User.ID,
		CredentialID: administrator.actor.SessionID}
	if administrator.actor.IsApplicationKey() {
		event.Actor.Kind = activity.ActorApplicationKey
		event.Actor.ID = strconv.FormatInt(administrator.actor.ApplicationKeyID, 10)
	}
	return event
}

func catalogSystemEvent(action activity.Action, resource activity.Resource) activity.Event {
	return activity.Event{Action: action, Resource: resource,
		Source: activity.SourceSystem, Actor: activity.Actor{Kind: activity.ActorSystem}}
}

// The wrapped transaction already owns the catalog and supplies its protected
// write context. Activity never opens another connection or transaction.
type catalogActivityTx struct{ tx pgx.Tx }

func (tx catalogActivityTx) Exec(statement string, args ...any) (pgconn.CommandTag, error) {
	return tx.tx.Exec(context.Background(), statement, args...)
}

type catalogAuthorizationTx struct{ tx OwnedTx }

func (tx catalogAuthorizationTx) QueryRow(_ context.Context, statement string, args ...any) pgx.Row {
	return tx.tx.QueryRow(statement, args...)
}

func recordScanFinished(tx activity.OwnedExecutor, job Job) error {
	event := catalogSystemEvent(activity.ActionScanFinished,
		activity.Resource{Kind: activity.ResourceScan, ID: job.ID})
	switch job.Status {
	case "Completed":
		event.State = activity.StateCompleted
	case "Failed":
		event.State, event.Severity = activity.StateFailed, activity.SeverityError
	case "Cancelled":
		event.State = activity.StateCancelled
	case "Interrupted":
		event.State, event.Severity = activity.StateInterrupted, activity.SeverityWarning
	default:
		return activity.ErrInvalidInput
	}
	return activity.RecordOwned(tx, event)
}

// Names come only from the finite edit schema. Compare both sparse layers so
// resetting an override and adding or removing a lock retain the affected field
// even when the effective value does not change.
func metadataActivityFields(previousOverrides, overrides, previousLocks, locks map[string]json.RawMessage) ([]activity.Field, error) {
	fields := make([]activity.Field, 0, len(metadataValueFieldNames)+2)
	overridesChanged, locksChanged := false, false
	for _, name := range metadataValueFieldNames {
		overrideChanged, err := metadataActivityValueChanged(name, previousOverrides, overrides)
		if err != nil {
			return nil, err
		}
		lockChanged, err := metadataActivityValueChanged(name, previousLocks, locks)
		if err != nil {
			return nil, err
		}
		if !overrideChanged && !lockChanged {
			continue
		}
		overridesChanged = overridesChanged || overrideChanged
		locksChanged = locksChanged || lockChanged
		field, ok := metadataActivityField(name)
		if !ok {
			return nil, activity.ErrInvalidInput
		}
		fields = append(fields, field)
	}
	if overridesChanged {
		fields = append(fields, activity.FieldOverrides)
	}
	if locksChanged {
		fields = append(fields, activity.FieldLockedFields)
	}
	return fields, nil
}

func metadataActivityValueChanged(name string, previous, current map[string]json.RawMessage) (bool, error) {
	oldValue, hadOld := previous[name]
	newValue, hasNew := current[name]
	if hadOld != hasNew {
		return true, nil
	}
	if !hadOld {
		return false, nil
	}
	var oldDecoded, newDecoded any
	if err := json.Unmarshal(oldValue, &oldDecoded); err != nil {
		return false, err
	}
	if err := json.Unmarshal(newValue, &newDecoded); err != nil {
		return false, err
	}
	oldValue, err := json.Marshal(oldDecoded)
	if err != nil {
		return false, err
	}
	newValue, err = json.Marshal(newDecoded)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(oldValue, newValue), nil
}

func metadataActivityField(name string) (activity.Field, bool) {
	switch name {
	case "Name":
		return activity.FieldName, true
	case "SortName":
		return activity.FieldSortName, true
	case "Overview":
		return activity.FieldOverview, true
	case "OriginalTitle":
		return activity.FieldOriginalTitle, true
	case "OfficialRating":
		return activity.FieldOfficialRating, true
	case "ProductionYear":
		return activity.FieldProductionYear, true
	case "IndexNumber":
		return activity.FieldIndexNumber, true
	case "ParentIndexNumber":
		return activity.FieldParentIndexNumber, true
	case "PremiereDate":
		return activity.FieldPremiereDate, true
	case "CommunityRating":
		return activity.FieldCommunityRating, true
	case "ProviderIds":
		return activity.FieldProviderIDs, true
	case "Genres":
		return activity.FieldGenres, true
	case "Tags":
		return activity.FieldTags, true
	case "Studios":
		return activity.FieldStudios, true
	case "People":
		return activity.FieldPeople, true
	default:
		return "", false
	}
}
