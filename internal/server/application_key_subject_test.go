package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func TestLibrarySubjectPreservesASeparateUserActor(t *testing.T) {
	for _, test := range []struct {
		name        string
		makeSubject func(identity.Principal, string) library.Subject
	}{
		{"direct", librarySubject},
		{"request", func(actor identity.Principal, target string) library.Subject {
			request := httptest.NewRequest(http.MethodPost, "/emby/Users/target/FavoriteItems/item", nil)
			request = request.WithContext(context.WithValue(request.Context(), principalKey, actor))
			return requestLibrarySubject(request, target)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			actor := identity.Principal{
				Kind: "emby", SessionID: "actor-session", PeerIP: "192.168.1.20",
				User:   identity.User{ID: "actor", IsAdministrator: true},
				Client: identity.Client{Name: "State client", DeviceID: "actor-device"},
			}
			expectedActor := actor
			subject := test.makeSubject(actor, "target")
			if subject.UserID != "target" || subject.ApplicationCredentialID != "" || subject.Actor == nil {
				t.Fatalf("user subject lost its target or authenticated actor: %+v", subject)
			}
			if subject.Actor == &actor || !reflect.DeepEqual(*subject.Actor, expectedActor) {
				t.Fatal("user subject did not retain a separate copy of the authenticated principal")
			}
			actor.User.ID, actor.SessionID, actor.Kind, actor.PeerIP = "changed-user", "changed-session", "admin", "8.8.8.8"
			actor.Client.DeviceID = "changed-device"
			if !reflect.DeepEqual(*subject.Actor, expectedActor) || subject.UserID != "target" {
				t.Fatal("changing the caller's principal changed the stored actor or target")
			}
		})
	}
}

func TestLibrarySubjectKeepsApplicationAuthorityIndependent(t *testing.T) {
	actor := identity.Principal{
		Kind: identity.ApplicationKeyKind, SessionID: "application-credential",
		ApplicationKeyID: 1, ClientSessionID: "application-client", PeerIP: "192.168.1.20",
	}
	for _, target := range []string{"", "target"} {
		subject := librarySubject(actor, target)
		if subject.UserID != target || subject.ApplicationCredentialID != actor.SessionID || subject.Actor != nil {
			t.Fatalf("application subject borrowed user authority: %+v", subject)
		}
	}
}

func TestRequestLibrarySubjectWithoutPrincipalDoesNotBecomeTrusted(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/emby/Users/target/FavoriteItems/item", nil)
	subject := requestLibrarySubject(request, "target")
	if subject.UserID != "target" || subject.ApplicationCredentialID != "" || subject.Actor == nil ||
		!reflect.DeepEqual(*subject.Actor, identity.Principal{}) {
		t.Fatal("an unauthenticated request became a trusted internal subject")
	}
}

func TestPlaybackOwnerPreservesTrustedPeerAcrossSourceBoundaries(t *testing.T) {
	for _, principal := range []identity.Principal{
		{Kind: "emby", SessionID: "viewer-session", User: identity.User{ID: "viewer"},
			PeerIP: "192.168.1.20", Client: identity.Client{DeviceID: "viewer-device"}},
		{Kind: identity.ApplicationKeyKind, SessionID: "application-credential", ApplicationKeyID: 1,
			ClientSessionID: "application-client", PeerIP: "8.8.8.8", Client: identity.Client{DeviceID: "application-device"}},
	} {
		t.Run(principal.Kind, func(t *testing.T) {
			owner := playbackOwner(principal)
			dynamicOwner := dynamicSourceOwner(principal)
			if owner.PeerIP != principal.PeerIP || dynamicOwner.PeerIP != principal.PeerIP {
				t.Fatal("playback or dynamic-source ownership lost the authenticated transport peer")
			}
		})
	}
}
