package settings

import (
	"strconv"

	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// Record only persisted choices and their field names. Neither configured names
// nor deployment values enter the activity payload. The owner's final fresh
// authorization check follows this insert before the common COMMIT.
func recordSettingsActivity(tx library.OwnedTx, actor Actor, previous, changed settingsRecord) error {
	source := activity.SourceNative
	if actor.Audience == identity.AdministratorEmby {
		source = activity.SourceEmby
	}
	identityActor := activity.Actor{Kind: activity.ActorUser, ID: actor.Principal.User.ID, CredentialID: actor.Principal.SessionID}
	if actor.Principal.IsApplicationKey() {
		identityActor.Kind = activity.ActorApplicationKey
		identityActor.ID = strconv.FormatInt(actor.Principal.ApplicationKeyID, 10)
	}
	fields := make([]activity.Field, 0, 7)
	if !sameActivityValue(previous.Overrides.ServerName, changed.Overrides.ServerName) {
		fields = append(fields, activity.FieldServerName)
	}
	if previous.ServerNameMode != changed.ServerNameMode {
		fields = append(fields, activity.FieldServerNameMode)
	}
	if !sameActivityValue(previous.Overrides.MaxBitrate, changed.Overrides.MaxBitrate) {
		fields = append(fields, activity.FieldMaxBitrate)
	}
	if !sameActivityValue(previous.Overrides.MaxWidth, changed.Overrides.MaxWidth) {
		fields = append(fields, activity.FieldMaxWidth)
	}
	if !sameActivityValue(previous.Overrides.MaxHeight, changed.Overrides.MaxHeight) {
		fields = append(fields, activity.FieldMaxHeight)
	}
	if !sameActivityValue(previous.Overrides.MaxAudioChannels, changed.Overrides.MaxAudioChannels) {
		fields = append(fields, activity.FieldMaxAudioChannels)
	}
	if previous.Encoding != changed.Encoding {
		fields = append(fields, activity.FieldTranscodingMaxWidth)
	}
	return activity.RecordOwned(tx, activity.Event{
		Action: activity.ActionSettingsUpdated, Severity: activity.SeverityInfo,
		Source: source, Actor: identityActor,
		Resource: activity.Resource{Kind: activity.ResourceSettings, ID: "1"},
		Revision: changed.Revision, Count: 1, ChangedFields: fields,
	})
}

func sameActivityValue[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
