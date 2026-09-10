package identity_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

type rejectedAdministratorQueries struct {
	calls int
}

func (q *rejectedAdministratorQueries) QueryRow(context.Context, string, ...any) pgx.Row {
	q.calls++
	return rejectedAdministratorRow{}
}

type rejectedAdministratorRow struct{}

func (rejectedAdministratorRow) Scan(...any) error {
	return errors.New("unexpected authorization query")
}

func TestCheckAdministratorRejectsInvalidAudienceAndMixedIdentityBeforeQueries(t *testing.T) {
	native := identity.Principal{Kind: "admin", SessionID: "native-session", User: identity.User{ID: "administrator"}}
	emby := native
	emby.Kind = "emby"
	key := identity.Principal{Kind: identity.ApplicationKeyKind, SessionID: "key-parent", ApplicationKeyID: 9, ClientSessionID: "key-client"}
	for _, test := range []struct {
		name     string
		actor    identity.Principal
		audience identity.AdministratorAudience
	}{
		{"zero audience", native, 0},
		{"unknown audience", native, 255},
		{"empty actor", identity.Principal{}, identity.AdministratorNative},
		{"native at Emby boundary", native, identity.AdministratorEmby},
		{"Emby at native boundary", emby, identity.AdministratorNative},
		{"key at native boundary", key, identity.AdministratorNative},
		{"system actor", identity.Principal{Kind: "system", SessionID: "system"}, identity.AdministratorEmby},
		{"mixed login and key", identity.Principal{Kind: "emby", SessionID: "login", User: native.User, ApplicationKeyID: 9}, identity.AdministratorEmby},
		{"mixed login and context", identity.Principal{Kind: "admin", SessionID: "login", User: native.User, ClientSessionID: "client"}, identity.AdministratorNative},
		{"key with user", identity.Principal{Kind: identity.ApplicationKeyKind, SessionID: key.SessionID, ApplicationKeyID: key.ApplicationKeyID, ClientSessionID: key.ClientSessionID, User: native.User}, identity.AdministratorEmby},
		{"key with administrator claim", identity.Principal{Kind: identity.ApplicationKeyKind, SessionID: key.SessionID, ApplicationKeyID: key.ApplicationKeyID, ClientSessionID: key.ClientSessionID, User: identity.User{IsAdministrator: true}}, identity.AdministratorEmby},
		{"key without client", identity.Principal{Kind: identity.ApplicationKeyKind, SessionID: key.SessionID, ApplicationKeyID: key.ApplicationKeyID}, identity.AdministratorEmby},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, lock := range []bool{false, true} {
				queries := &rejectedAdministratorQueries{}
				if err := identity.CheckAdministrator(context.Background(), queries, test.actor, test.audience, lock); !errors.Is(err, identity.ErrUnauthorized) || queries.calls != 0 {
					t.Fatalf("invalid administrator input reached the database: calls=%d error=%v", queries.calls, err)
				}
			}
		})
	}
	for _, invalid := range []string{"", " parent", "parent ", "\t", "bad\x00id", "bad\xffid", strings.Repeat("x", 257)} {
		for _, field := range []string{"parent", "client", "login", "user"} {
			actor, audience := key, identity.AdministratorEmby
			switch field {
			case "parent":
				actor.SessionID = invalid
			case "client":
				actor.ClientSessionID = invalid
			case "login":
				actor, audience = native, identity.AdministratorNative
				actor.SessionID = invalid
			case "user":
				actor, audience = native, identity.AdministratorNative
				actor.User.ID = invalid
			}
			queries := &rejectedAdministratorQueries{}
			if err := identity.CheckAdministrator(context.Background(), queries, actor, audience, true); !errors.Is(err, identity.ErrUnauthorized) || queries.calls != 0 {
				t.Fatalf("invalid %s identifier reached an authorization query: %v", field, err)
			}
		}
	}
}
