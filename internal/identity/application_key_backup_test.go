package identity

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAuthenticateApplicationKeyBackupRowCanonicalTokens(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  []byte
	}{
		{name: "zero bytes", raw: make([]byte, 32)},
		{name: "URL alphabet", raw: bytes.Repeat([]byte{0xfb}, 32)},
		{name: "maximum bytes", raw: bytes.Repeat([]byte{0xff}, 32)},
	} {
		t.Run(test.name, func(t *testing.T) {
			gcm := applicationKeyBackupTestGCM(t, 0x51)
			plaintext := []byte(base64.RawURLEncoding.EncodeToString(test.raw))
			sealed := sealApplicationKeyBackupTestToken(gcm, "backup-credential", plaintext)
			digest := sha256.Sum256(plaintext)
			sealedBefore, hashBefore := bytes.Clone(sealed), bytes.Clone(digest[:])
			if err := authenticateApplicationKeyBackupRow(gcm, "backup-credential", sealed, digest[:]); err != nil {
				t.Fatal("canonical credential failed authentication")
			}
			if !bytes.Equal(sealed, sealedBefore) || !bytes.Equal(digest[:], hashBefore) {
				t.Fatal("authentication modified caller-owned inputs")
			}
		})
	}
}

func TestAuthenticateApplicationKeyBackupRowRejectsTamperingWithoutMutation(t *testing.T) {
	gcm := applicationKeyBackupTestGCM(t, 0x52)
	wrongGCM := applicationKeyBackupTestGCM(t, 0x53)
	const credentialID = "backup-credential"
	plaintext := []byte(strings.Repeat("A", 43))
	sealed := sealApplicationKeyBackupTestToken(gcm, credentialID, plaintext)
	digest := sha256.Sum256(plaintext)
	changed := func(offset int) []byte {
		result := bytes.Clone(sealed)
		result[offset] ^= 0x80
		return result
	}
	wrongHash := bytes.Clone(digest[:])
	wrongHash[0] ^= 1
	for _, test := range []struct {
		name         string
		gcm          cipher.AEAD
		credentialID string
		sealed       []byte
		tokenHash    []byte
		want         error
	}{
		{name: "nil AEAD", credentialID: credentialID, sealed: sealed, tokenHash: digest[:], want: ErrApplicationKeyBackupInvalid},
		{name: "empty credential ID", gcm: gcm, sealed: sealed, tokenHash: digest[:], want: ErrApplicationKeyBackupInvalid},
		{name: "padded credential ID", gcm: gcm, credentialID: " " + credentialID, sealed: sealed, tokenHash: digest[:], want: ErrApplicationKeyBackupInvalid},
		{name: "control in credential ID", gcm: gcm, credentialID: credentialID + "\n", sealed: sealed, tokenHash: digest[:], want: ErrApplicationKeyBackupInvalid},
		{name: "oversized credential ID", gcm: gcm, credentialID: strings.Repeat("x", 257), sealed: sealed, tokenHash: digest[:], want: ErrApplicationKeyBackupInvalid},
		{name: "invalid UTF-8 credential ID", gcm: gcm, credentialID: string([]byte{0xff}), sealed: sealed, tokenHash: digest[:], want: ErrApplicationKeyBackupInvalid},
		{name: "wrong credential AAD", gcm: gcm, credentialID: "different-credential", sealed: sealed, tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "case-sensitive credential AAD", gcm: gcm, credentialID: "Backup-credential", sealed: sealed, tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "wrong master", gcm: wrongGCM, credentialID: credentialID, sealed: sealed, tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "nil hash", gcm: gcm, credentialID: credentialID, sealed: sealed, want: ErrApplicationKeyBackupInvalid},
		{name: "short hash", gcm: gcm, credentialID: credentialID, sealed: sealed, tokenHash: digest[:31], want: ErrApplicationKeyBackupInvalid},
		{name: "long hash", gcm: gcm, credentialID: credentialID, sealed: sealed, tokenHash: append(bytes.Clone(digest[:]), 0), want: ErrApplicationKeyBackupInvalid},
		{name: "wrong hash", gcm: gcm, credentialID: credentialID, sealed: sealed, tokenHash: wrongHash, want: ErrApplicationKeyVaultCiphertext},
		{name: "nil ciphertext", gcm: gcm, credentialID: credentialID, tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "empty ciphertext", gcm: gcm, credentialID: credentialID, sealed: []byte{}, tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "short header", gcm: gcm, credentialID: credentialID, sealed: sealed[:len(applicationKeyHeader)-1], tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "short nonce", gcm: gcm, credentialID: credentialID, sealed: sealed[:len(applicationKeyHeader)+applicationKeyNonceSize-1], tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "short tag", gcm: gcm, credentialID: credentialID, sealed: sealed[:len(sealed)-1], tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "trailing byte", gcm: gcm, credentialID: credentialID, sealed: append(bytes.Clone(sealed), 0), tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "wrong magic", gcm: gcm, credentialID: credentialID, sealed: changed(0), tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "unsupported version", gcm: gcm, credentialID: credentialID, sealed: changed(len(applicationKeyHeader) - 1), tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "tampered nonce", gcm: gcm, credentialID: credentialID, sealed: changed(len(applicationKeyHeader)), tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "tampered ciphertext", gcm: gcm, credentialID: credentialID, sealed: changed(len(applicationKeyHeader) + applicationKeyNonceSize), tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
		{name: "tampered tag", gcm: gcm, credentialID: credentialID, sealed: changed(len(sealed) - 1), tokenHash: digest[:], want: ErrApplicationKeyVaultCiphertext},
	} {
		t.Run(test.name, func(t *testing.T) {
			sealedBefore, hashBefore := bytes.Clone(test.sealed), bytes.Clone(test.tokenHash)
			if err := authenticateApplicationKeyBackupRow(test.gcm, test.credentialID, test.sealed, test.tokenHash); err != test.want {
				t.Fatal("authentication did not return the expected fixed error")
			}
			if !bytes.Equal(test.sealed, sealedBefore) || !bytes.Equal(test.tokenHash, hashBefore) {
				t.Fatal("failed authentication modified caller-owned inputs")
			}
		})
	}
}

func TestAuthenticateApplicationKeyBackupRowRejectsEncryptedMalformedTokens(t *testing.T) {
	gcm := applicationKeyBackupTestGCM(t, 0x54)
	for _, test := range []struct {
		name  string
		token string
	}{
		{name: "empty", token: ""},
		{name: "short", token: strings.Repeat("A", 42)},
		{name: "long", token: strings.Repeat("A", 44)},
		{name: "nonalphabet", token: "#" + strings.Repeat("A", 42)},
		{name: "standard Base64 plus", token: "+" + strings.Repeat("A", 42)},
		{name: "standard Base64 slash", token: "/" + strings.Repeat("A", 42)},
		{name: "padding", token: strings.Repeat("A", 42) + "="},
		{name: "carriage return", token: "\r" + strings.Repeat("A", 42)},
		{name: "line feed", token: "\n" + strings.Repeat("A", 42)},
		{name: "NUL", token: strings.Repeat("A", 42) + "\x00"},
		{name: "noncanonical trailing bit one", token: strings.Repeat("A", 42) + "B"},
		{name: "noncanonical trailing bit two", token: strings.Repeat("A", 42) + "C"},
		{name: "noncanonical trailing bits", token: strings.Repeat("A", 42) + "D"},
	} {
		t.Run(test.name, func(t *testing.T) {
			plaintext := []byte(test.token)
			sealed := sealApplicationKeyBackupTestToken(gcm, "backup-credential", plaintext)
			// The hash matches the malformed plaintext so only its format fails.
			digest := sha256.Sum256(plaintext)
			sealedBefore, hashBefore := bytes.Clone(sealed), bytes.Clone(digest[:])
			if err := authenticateApplicationKeyBackupRow(gcm, "backup-credential", sealed, digest[:]); err != ErrApplicationKeyVaultCiphertext {
				t.Fatal("malformed encrypted token did not return the fixed ciphertext error")
			}
			if !bytes.Equal(sealed, sealedBefore) || !bytes.Equal(digest[:], hashBefore) {
				t.Fatal("format rejection modified caller-owned inputs")
			}
		})
	}
}

func TestApplicationKeyBackupExportedHelpersRejectInvalidArguments(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	unusedTx := applicationKeyBackupUnusedTestTx{}
	vault := &ApplicationKeyVault{}
	t.Run("witness clears output", func(t *testing.T) {
		for _, test := range []struct {
			name  string
			ctx   context.Context
			tx    pgx.Tx
			vault *ApplicationKeyVault
			size  int
			want  error
		}{
			{name: "nil output", ctx: context.Background(), tx: unusedTx, vault: vault, want: ErrInvalidInput},
			{name: "short output", ctx: context.Background(), tx: unusedTx, vault: vault, size: 31, want: ErrInvalidInput},
			{name: "long output", ctx: context.Background(), tx: unusedTx, vault: vault, size: 33, want: ErrInvalidInput},
			{name: "nil transaction", ctx: context.Background(), vault: vault, size: 32, want: ErrInvalidInput},
			{name: "nil vault", ctx: context.Background(), tx: unusedTx, size: 32, want: ErrApplicationKeyVaultUnavailable},
			{name: "cancelled", ctx: cancelled, tx: unusedTx, vault: vault, size: 32, want: context.Canceled},
		} {
			t.Run(test.name, func(t *testing.T) {
				var master []byte
				if test.size != 0 {
					master = bytes.Repeat([]byte{0x61}, test.size)
				}
				witness, err := test.vault.WitnessBackup(test.ctx, test.tx, master)
				if err != test.want || witness != (ApplicationKeyBackupWitness{}) {
					t.Fatal("invalid witness arguments did not return a zero witness and fixed error")
				}
				if !bytes.Equal(master, make([]byte, len(master))) {
					t.Fatal("failed witness left caller-owned output uncleared")
				}
			})
		}
	})
	t.Run("recovery preserves supplied key", func(t *testing.T) {
		for _, test := range []struct {
			name string
			ctx  context.Context
			tx   pgx.Tx
			size int
			want error
		}{
			{name: "nil transaction and absent key", ctx: context.Background(), want: ErrInvalidInput},
			{name: "nil transaction and exact key", ctx: context.Background(), size: 32, want: ErrInvalidInput},
			{name: "short key", ctx: context.Background(), tx: unusedTx, size: 31, want: ErrInvalidInput},
			{name: "long key", ctx: context.Background(), tx: unusedTx, size: 33, want: ErrInvalidInput},
			{name: "cancelled and absent key", ctx: cancelled, tx: unusedTx, want: context.Canceled},
			{name: "cancelled and exact key", ctx: cancelled, tx: unusedTx, size: 32, want: context.Canceled},
		} {
			t.Run(test.name, func(t *testing.T) {
				master := bytes.Repeat([]byte{0x62}, test.size)
				before := bytes.Clone(master)
				witness, err := ValidateApplicationKeyRecovery(test.ctx, test.tx, master)
				if err != test.want || witness != (ApplicationKeyBackupWitness{}) {
					t.Fatal("invalid recovery arguments did not return a zero witness and fixed error")
				}
				if !bytes.Equal(master, before) {
					t.Fatal("recovery validation modified the supplied master key")
				}
			})
		}
	})
	t.Run("revocation returns zero count", func(t *testing.T) {
		if count, err := RevokeRecoveredCredentials(context.Background(), nil); err != ErrInvalidInput || count != 0 {
			t.Fatal("nil recovery transaction did not return zero and the fixed input error")
		}
		if count, err := RevokeRecoveredCredentials(cancelled, unusedTx); err != context.Canceled || count != 0 {
			t.Fatal("cancelled recovery revocation did not return zero and cancellation")
		}
	})
}

func TestApplicationKeyBackupDatabaseFailuresReturnFixedErrors(t *testing.T) {
	ctx := context.Background()
	tx := applicationKeyBackupErrorTestTx{}
	master := bytes.Repeat([]byte{0x63}, 32)
	before := bytes.Clone(master)
	witness, err := ValidateApplicationKeyRecovery(ctx, tx, master)
	if err != ErrApplicationKeyBackupUnavailable || witness != (ApplicationKeyBackupWitness{}) {
		t.Fatal("database detail escaped the fixed recovery validation error")
	}
	if !bytes.Equal(master, before) {
		t.Fatal("database failure modified the supplied master key")
	}
	if count, err := RevokeRecoveredCredentials(ctx, tx); err != ErrApplicationKeyBackupUnavailable || count != 0 {
		t.Fatal("database detail escaped the fixed recovery revocation error")
	}
}

func applicationKeyBackupTestGCM(t *testing.T, fill byte) cipher.AEAD {
	t.Helper()
	master := bytes.Repeat([]byte{fill}, applicationKeyMasterSize)
	defer clear(master)
	gcm, err := applicationKeyGCM(master)
	if err != nil {
		t.Fatal("create in-memory application key fixture")
	}
	return gcm
}

func sealApplicationKeyBackupTestToken(gcm cipher.AEAD, credentialID string, plaintext []byte) []byte {
	sealed := make([]byte, len(applicationKeyHeader)+applicationKeyNonceSize)
	copy(sealed, applicationKeyHeader)
	nonce := sealed[len(applicationKeyHeader):]
	// Deterministic nonces are confined to these in-memory test fixtures.
	for index := range nonce {
		nonce[index] = byte(index + 1)
	}
	return gcm.Seal(sealed, nonce, plaintext, []byte(applicationKeyPurpose+credentialID))
}

// Any database method on this transaction panics, proving early rejection.
type applicationKeyBackupUnusedTestTx struct {
	pgx.Tx
}

type applicationKeyBackupErrorTestTx struct {
	pgx.Tx
}

func (applicationKeyBackupErrorTestTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return applicationKeyBackupErrorTestRow{}
}

func (applicationKeyBackupErrorTestTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("private database detail sentinel")
}

type applicationKeyBackupErrorTestRow struct{}

func (applicationKeyBackupErrorTestRow) Scan(...any) error {
	return errors.New("private database detail sentinel")
}
