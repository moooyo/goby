package identity

import (
	"errors"
	"strings"
	"testing"
)

func TestApplicationKeyPrincipalRequiresExplicitIdentity(t *testing.T) {
	valid := Principal{Kind: ApplicationKeyKind, ApplicationKeyID: 12, SessionID: "credential", ClientSessionID: "client"}
	if !valid.IsApplicationKey() || !valid.CanManageServer() {
		t.Fatal("explicit application credential lacks server authority")
	}
	for _, principal := range []Principal{
		{}, {Kind: ApplicationKeyKind, SessionID: "credential"},
		{Kind: ApplicationKeyKind, ApplicationKeyID: 12},
		{Kind: ApplicationKeyKind, ApplicationKeyID: 12, SessionID: "credential"},
		{Kind: "emby", ApplicationKeyID: 12, SessionID: "credential"},
		{Kind: ApplicationKeyKind, ApplicationKeyID: 12, SessionID: "credential", ClientSessionID: "client", User: User{ID: "target"}},
		{Kind: ApplicationKeyKind, ApplicationKeyID: 12, SessionID: "credential", ClientSessionID: "client", User: User{IsAdministrator: true}},
	} {
		if principal.IsApplicationKey() || principal.CanManageServer() {
			t.Fatal("incomplete or user-bound principal gained application key authority")
		}
	}
	admin := Principal{Kind: "emby", SessionID: "login", User: User{ID: "admin", IsAdministrator: true}}
	if !admin.CanManageServer() || admin.IsApplicationKey() {
		t.Fatal("administrator login authority became an application key")
	}
	admin.User.IsDisabled = true
	if admin.CanManageServer() {
		t.Fatal("disabled administrator snapshot retained management authority")
	}
}

func TestApplicationKeyFilterBoundsAndLiteralSearch(t *testing.T) {
	valid, err := validateApplicationKeyFilter(ApplicationKeyFilter{SearchTerm: "_100%"})
	if err != nil || valid.Limit != 50 || valid.SearchTerm != "_100%" {
		t.Fatalf("valid application key filter changed: %v", err)
	}
	for _, filter := range []ApplicationKeyFilter{
		{Limit: -1}, {Limit: 201}, {StartIndex: -1}, {SearchTerm: "invalid\ntext"},
		{SearchTerm: strings.Repeat("x", 257)}, {SearchTerm: string([]byte{0xff})},
	} {
		if _, err := validateApplicationKeyFilter(filter); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid application key filter accepted: %v", err)
		}
	}
}
