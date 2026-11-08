package identity_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestValidateUserActivityIDsBoundsAndExactIdentities(t *testing.T) {
	maximum := make([]string, identity.MaxUserActivityIDs)
	for index := range maximum {
		maximum[index] = fmt.Sprintf("activity-user-%d", index)
	}
	for _, ids := range [][]string{{"user"}, {"user", "another-user"}, maximum} {
		if err := identity.ValidateUserActivityIDs(ids); err != nil {
			t.Fatalf("valid activity batch was rejected: %v", err)
		}
	}
	for _, ids := range [][]string{
		nil, {}, {""}, {" user"}, {"user "}, {"user\n"}, {"user\x00"}, {"user\xff"},
		{"user,user"}, {"user", "user"}, {strings.Repeat("a", 257)}, append(maximum, "too-many"),
	} {
		if err := identity.ValidateUserActivityIDs(ids); !errors.Is(err, identity.ErrInvalidInput) {
			t.Fatal("an invalid activity batch was accepted")
		}
	}
}

func TestUserActivityRejectsNonNativePrincipalsBeforeDatabaseAdmission(t *testing.T) {
	store := identity.New(nil)
	for _, actor := range []identity.Principal{
		{},
		{Kind: "emby", SessionID: "credential", User: identity.User{ID: "administrator", IsAdministrator: true}},
		{Kind: "admin", SessionID: "credential", User: identity.User{ID: "administrator"}, ClientSessionID: "application-client"},
		{Kind: identity.ApplicationKeyKind, SessionID: "credential", ClientSessionID: "application-client", ApplicationKeyID: 1},
	} {
		if _, err := store.ListUserActivity(context.Background(), actor, []string{"user"}); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatal("non-native principal was admitted to an activity read")
		}
		if err := store.TouchAdministratorActivity(context.Background(), actor); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatal("non-native principal was admitted to native activity sampling")
		}
	}
}
