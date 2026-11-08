package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestManagedPlaybackControllerRejectsOtherCredentialKindsBeforeDatabaseAccess(t *testing.T) {
	store := identity.New(nil)
	for _, principal := range []identity.Principal{
		{},
		{Kind: "emby", SessionID: "emby-login", User: identity.User{ID: "user", IsAdministrator: true}},
		{Kind: "admin", SessionID: "admin-login", User: identity.User{ID: "user"}, ApplicationKeyID: 1},
		{Kind: "admin", SessionID: "admin-login", User: identity.User{ID: "user"}, ClientSessionID: "client"},
		{Kind: "admin", SessionID: "", User: identity.User{ID: "user"}},
		{Kind: "admin", SessionID: "admin-login", User: identity.User{ID: " user"}},
		{Kind: identity.ApplicationKeyKind, SessionID: "key", ClientSessionID: "client", ApplicationKeyID: 1},
	} {
		if _, err := store.RevalidateManagedPlaybackController(context.Background(), principal); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatal("native playback controller accepted a mixed or non-native credential")
		}
	}
}
