package identity

import (
	"slices"
	"strconv"

	"github.com/moooyo/goby/internal/activity"
)

// identityActivityActor only converts an already authenticated and transactionally
// authorized principal. It never supplies authority or invents a system fallback.
func identityActivityActor(principal Principal) (activity.Actor, error) {
	if principal.IsApplicationKey() {
		return activity.Actor{Kind: activity.ActorApplicationKey,
			ID: strconv.FormatInt(principal.ApplicationKeyID, 10), CredentialID: principal.SessionID}, nil
	}
	if (principal.Kind != "admin" && principal.Kind != "emby") ||
		principal.ApplicationKeyID != 0 || principal.ClientSessionID != "" ||
		!validRevalidationID(principal.User.ID) || !validRevalidationID(principal.SessionID) {
		return activity.Actor{}, ErrUnauthorized
	}
	return activity.Actor{Kind: activity.ActorUser, ID: principal.User.ID, CredentialID: principal.SessionID}, nil
}

func identityActivitySource(kind string) activity.Source {
	switch kind {
	case "admin":
		return activity.SourceNative
	case "emby", ApplicationKeyKind:
		return activity.SourceEmby
	default:
		return ""
	}
}

func managedUserActivityFields(current, updated ManagedUser) []activity.Field {
	fields := make([]activity.Field, 0, 9)
	if current.User.Name != updated.User.Name {
		fields = append(fields, activity.FieldName)
	}
	if current.User.IsAdministrator != updated.User.IsAdministrator {
		fields = append(fields, activity.FieldIsAdministrator)
	}
	if current.User.IsDisabled != updated.User.IsDisabled {
		fields = append(fields, activity.FieldIsDisabled)
	}
	if current.Policy.EnableAllFolders != updated.Policy.EnableAllFolders {
		fields = append(fields, activity.FieldEnableAllFolders)
	}
	if !slices.Equal(current.Policy.EnabledFolders, updated.Policy.EnabledFolders) {
		fields = append(fields, activity.FieldEnabledFolders)
	}
	if current.Policy.EnableMediaPlayback != updated.Policy.EnableMediaPlayback {
		fields = append(fields, activity.FieldEnableMediaPlayback)
	}
	if current.Policy.EnablePlaybackRemuxing != updated.Policy.EnablePlaybackRemuxing {
		fields = append(fields, activity.FieldEnablePlaybackRemuxing)
	}
	if current.Policy.EnableAudioPlaybackTranscoding != updated.Policy.EnableAudioPlaybackTranscoding {
		fields = append(fields, activity.FieldEnableAudioPlaybackTranscoding)
	}
	if current.Policy.EnableVideoPlaybackTranscoding != updated.Policy.EnableVideoPlaybackTranscoding {
		fields = append(fields, activity.FieldEnableVideoPlaybackTranscoding)
	}
	return fields
}
