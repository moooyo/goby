package identity

import (
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestNormalizeNameCaseFolding(t *testing.T) {
	tests := []struct {
		first  string
		second string
	}{
		{first: "Admin", second: "  ADMIN  "},
		{first: "Media-User", second: "media-user"},
		{first: "Σίγμα", second: "ςίγμα"},
		{first: "Kelvin", second: "Kelvin"},
	}
	for _, item := range tests {
		t.Run(item.first, func(t *testing.T) {
			_, first, err := normalizeName(item.first)
			if err != nil {
				t.Fatal(err)
			}
			_, second, err := normalizeName(item.second)
			if err != nil {
				t.Fatal(err)
			}
			if first != second {
				t.Fatalf("case-equivalent names produced different keys: %q and %q", first, second)
			}
		})
	}
	name, _, err := normalizeName("  MiXeD Case  ")
	if err != nil || name != "MiXeD Case" {
		t.Fatalf("display name was not preserved after trimming: %q, %v", name, err)
	}
}

func TestNormalizeNameRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"", " \t ", "line\nbreak", "nul\x00byte", string([]byte{0xff}), strings.Repeat("x", 129)} {
		if _, _, err := normalizeName(name); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("normalizeName(%q) error = %v, want ErrInvalidInput", name, err)
		}
	}
	if _, _, err := normalizeName(strings.Repeat("α", 128)); err != nil {
		t.Errorf("128 Unicode characters must be accepted: %v", err)
	}
}

func TestPasswordLimitCountsBytes(t *testing.T) {
	for _, password := range []string{strings.Repeat("x", 73), strings.Repeat("α", 37)} {
		if err := validatePassword(password, false); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("password with %d bytes was accepted: %v", len(password), err)
		}
	}
	for _, password := range []string{strings.Repeat("x", 72), strings.Repeat("α", 36), ""} {
		if err := validatePassword(password, false); err != nil {
			t.Errorf("password with %d bytes was rejected: %v", len(password), err)
		}
	}
	if err := validatePassword("", true); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty administrator password error = %v, want ErrInvalidInput", err)
	}
}

func TestFakePasswordHashUsesStoredHashCost(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(fakePasswordHash))
	if err != nil {
		t.Fatal(err)
	}
	if cost != passwordCost {
		t.Fatalf("fake hash cost = %d, stored hash cost = %d", cost, passwordCost)
	}
	err = bcrypt.CompareHashAndPassword([]byte(fakePasswordHash), []byte("a deliberately incorrect password"))
	if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Fatalf("fake password check error = %v, want bcrypt mismatch", err)
	}
}

func TestTokenDigestRejectsMalformedOrNoncanonicalTokens(t *testing.T) {
	token, digest, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := tokenDigest(token); !ok || got != digest || got != sha256.Sum256([]byte(token)) {
		t.Fatal("issued token could not be resolved to its stored digest")
	}
	for _, value := range []string{"", token + "=", token[:len(token)-1], "!" + token[1:], strings.Repeat("A", 42) + "B"} {
		if _, ok := tokenDigest(value); ok {
			t.Errorf("malformed token %q was accepted", value)
		}
	}
}

func TestClientMetadataRejectsOversizedOrNullValues(t *testing.T) {
	for _, client := range []Client{
		{Name: strings.Repeat("x", 257)},
		{DeviceID: "device\x00id"},
		{Device: strings.Repeat("α", 129)},
		{Version: string([]byte{0xff})},
	} {
		if err := validateClient(client); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("client error = %v, want ErrInvalidInput", err)
		}
	}
}
