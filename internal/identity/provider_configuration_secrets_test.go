package identity

import (
	"bytes"
	"errors"
	"testing"
)

func TestProviderCiphertextBindsItsRevisionAndSecretDomain(t *testing.T) {
	gcm, err := applicationKeyGCM(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	document := []byte(`{"tmdb.Token":"private-test-credential"}`)
	aad, err := providerConfigurationAAD("9007199254740993")
	if err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 16)
	copy(header, providerConfigurationHeader)
	sealed := gcm.Seal(header, header[4:], document, aad)
	plain, err := openProviderConfiguration(gcm, "9007199254740993", sealed)
	if err != nil || !bytes.Equal(plain, document) {
		t.Fatal("the revision-bound provider document could not be recovered")
	}
	clear(plain)
	for _, revision := range []string{"9007199254740992", "1", "01", ""} {
		plain, err := openProviderConfiguration(gcm, revision, sealed)
		if !errors.Is(err, ErrApplicationKeyVaultCiphertext) || plain != nil {
			t.Fatal("provider ciphertext accepted another revision")
		}
	}
	modified := append([]byte(nil), sealed...)
	modified[len(modified)-1] ^= 1
	for _, invalid := range [][]byte{modified, sealed[:16], append([]byte("GNT\x01"), sealed[4:]...)} {
		if plain, err := openProviderConfiguration(gcm, "9007199254740993", invalid); !errors.Is(err, ErrApplicationKeyVaultCiphertext) || plain != nil {
			t.Fatal("provider ciphertext accepted tampering or another secret domain")
		}
	}
}
