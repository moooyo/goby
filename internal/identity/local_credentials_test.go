package identity

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestProfilePinEnvelopeRejectsTamperingCrossAccountAndApplicationDomain(t *testing.T) {
	gcm, err := applicationKeyGCM(bytes.Repeat([]byte{0x5a}, applicationKeyMasterSize))
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{0x3c}, applicationKeyNonceSize)
	seal := func(header, purpose, id, pin string) []byte {
		envelope := append([]byte(header), nonce...)
		return gcm.Seal(envelope, nonce, []byte(pin), []byte(purpose+id))
	}
	valid := seal(profilePinHeader, profilePinPurpose, "account-one", "2468")
	if got, err := openProfilePin(gcm, "account-one", valid); err != nil || got != "2468" {
		t.Fatal("valid envelope failed")
	}
	if _, err := openProfilePin(gcm, "account-two", valid); err == nil {
		t.Fatal("account binding was omitted")
	}
	for index := range valid {
		changed := append([]byte(nil), valid...)
		changed[index] ^= 1
		if _, err := openProfilePin(gcm, "account-one", changed); err == nil {
			t.Fatal("modified envelope authenticated")
		}
	}
	for _, invalid := range [][]byte{nil, valid[:len(valid)-1], seal(applicationKeyHeader, applicationKeyPurpose, "account-one", "2468"), seal(profilePinHeader, profilePinPurpose, "account-one", "abcd")} {
		if _, err := openProfilePin(gcm, "account-one", invalid); err == nil {
			t.Fatal("invalid envelope was accepted")
		}
	}
}

func TestProfilePinPatchSeparatesNullClearFromAbsentAndRejectsAliases(t *testing.T) {
	for _, raw := range []string{`null`, `""`, `"1357"`} {
		patch, pin, err := splitProfilePinPatch(UserConfigurationPatch{"ProfilePin": json.RawMessage(raw), "SubtitleMode": json.RawMessage(`"Always"`)})
		if err != nil || pin == nil || len(patch) != 1 || patch["ProfilePin"] != nil {
			t.Fatal("profile PIN was not extracted from the generic configuration")
		}
	}
	if _, pin, err := splitProfilePinPatch(UserConfigurationPatch{}); err != nil || pin != nil {
		t.Fatal("absent profile PIN became a clear operation")
	}
	if _, _, err := splitProfilePinPatch(UserConfigurationPatch{"ProfilePin": json.RawMessage(`null`), "profilepin": json.RawMessage(`"1357"`)}); err == nil {
		t.Fatal("duplicate profile PIN aliases were accepted")
	}
}
