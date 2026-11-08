package identity

import (
	"bytes"
	"crypto/cipher"
	"errors"
	"testing"
)

func sealBackupPolicyFixture(t *testing.T, gcm cipher.AEAD, generation int64, plain []byte) []byte {
	t.Helper()
	aad, err := backupPolicySecretAAD(generation)
	if err != nil {
		t.Fatal(err)
	}
	header := append([]byte(backupPolicySecretHeader), bytes.Repeat([]byte{9}, applicationKeyNonceSize)...)
	return gcm.Seal(header, header[4:], plain, aad)
}

func TestBackupPolicyPassphraseUsesExactBytesAndGenerationBinding(t *testing.T) {
	gcm, err := applicationKeyGCM(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("  exact UTF-8 passphrase \xe5\xaf\x86\xe7\xa0\x81  ")
	before := bytes.Clone(plain)
	sealed := sealBackupPolicyFixture(t, gcm, 9, plain)
	opened, err := openBackupPolicyPassphrase(gcm, 9, sealed)
	defer clear(opened)
	if err != nil || !bytes.Equal(opened, plain) || !bytes.Equal(plain, before) {
		t.Fatal("passphrase bytes were changed")
	}
	if _, err := openBackupPolicyPassphrase(gcm, 10, sealed); !errors.Is(err, ErrApplicationKeyVaultCiphertext) {
		t.Fatalf("wrong-generation ciphertext accepted: %v", err)
	}
	for _, invalid := range [][]byte{nil, []byte("GBP\x01"), bytes.Repeat([]byte{'x'}, 1057), append([]byte("GPC\x01"), sealed[4:]...)} {
		if _, err := openBackupPolicyPassphrase(gcm, 9, invalid); !errors.Is(err, ErrApplicationKeyVaultCiphertext) {
			t.Fatalf("invalid secret accepted: %v", err)
		}
	}
}

func TestBackupPolicyPassphraseRejectsInvalidPlaintextBeforePersistence(t *testing.T) {
	for _, value := range [][]byte{nil, []byte("short"), bytes.Repeat([]byte{'a'}, 1025), append(bytes.Repeat([]byte{'a'}, 12), 0xff)} {
		if validBackupPolicyPassphrase(value) {
			t.Fatal("accepted invalid passphrase")
		}
	}
	for _, value := range [][]byte{bytes.Repeat([]byte{'a'}, 12), bytes.Repeat([]byte{'a'}, 1024), []byte("\xe5\xaf\x86\xe7\xa0\x81\xe5\xaf\x86\xe7\xa0\x81")} {
		if !validBackupPolicyPassphrase(value) {
			t.Fatal("rejected bounded exact UTF-8 passphrase")
		}
	}
}
