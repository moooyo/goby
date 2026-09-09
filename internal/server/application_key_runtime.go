package server

import (
	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
)

// clientSessionID is the public client context, independent of the credential
// that authenticates it. Ordinary logins have a single authentication context.
func clientSessionID(principal identity.Principal) string {
	if principal.IsApplicationKey() {
		return principal.ClientSessionID
	}
	return principal.SessionID
}

func clientEventScope(principal identity.Principal) events.Scope {
	scope := events.Scope{UserID: principal.User.ID, SessionID: clientSessionID(principal), DeviceID: principal.Client.DeviceID}
	if principal.IsApplicationKey() {
		scope.ApplicationKey, scope.CredentialID = true, principal.SessionID
	}
	return scope
}

func targetEventScope(session identity.ClientSession) events.Scope {
	scope := events.Scope{UserID: session.UserID, SessionID: session.SessionID, DeviceID: session.Client.DeviceID}
	if session.Kind == identity.ApplicationKeyKind {
		scope.ApplicationKey, scope.CredentialID = true, session.CredentialID
	}
	return scope
}
