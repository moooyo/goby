package identity_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestRevalidateSessionRejectsInvalidTrustedIdentifiersBeforeDatabaseAccess(t *testing.T) {
	store := identity.New(nil)
	valid := identity.Principal{SessionID: "session-id", User: identity.User{ID: "user-id"}, Kind: "emby"}
	tests := map[string]identity.Principal{
		"empty principal": {},
		"admin principal": {SessionID: valid.SessionID, User: valid.User, Kind: "admin"},
		"wrong kind case": {SessionID: valid.SessionID, User: valid.User, Kind: "Emby"},
		"missing kind":    {SessionID: valid.SessionID, User: valid.User},
	}
	for _, field := range []string{"session", "user"} {
		for name, value := range map[string]string{
			"empty": "", "whitespace": " ", "leading space": " id", "trailing space": "id ",
			"null byte": "i\x00d", "control character": "i\nd", "invalid UTF-8": "i\xffd",
			"oversized": strings.Repeat("x", 257),
		} {
			principal := valid
			if field == "session" {
				principal.SessionID = value
			} else {
				principal.User.ID = value
			}
			tests[field+" "+name] = principal
		}
	}
	for name, principal := range tests {
		t.Run(name, func(t *testing.T) {
			refreshed, err := store.RevalidateSession(context.Background(), principal)
			if !errors.Is(err, identity.ErrUnauthorized) || !reflect.DeepEqual(refreshed, identity.Principal{}) {
				t.Errorf("invalid prior authentication returned principal = %+v, error = %v", refreshed, err)
			}
		})
	}
}
