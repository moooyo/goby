package identity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"sync"
)

const (
	applicationKeyMasterSize = 32
	applicationKeyNonceSize  = 12
	applicationKeyTagSize    = 16
	applicationKeyHeader     = "GAK\x01"
	applicationKeyPurpose    = "goby/application-key/v1\x00"
)

var (
	ErrApplicationKeyVaultUnavailable = errors.New("application key vault unavailable")
	ErrApplicationKeyVaultUnsafe      = errors.New("application key vault master key is unsafe")
	ErrApplicationKeyVaultMissing     = errors.New("application key vault master key is missing")
	ErrApplicationKeyVaultCiphertext  = errors.New("invalid application key ciphertext")
	ErrApplicationKeyVaultUnsupported = errors.New("application key vault is unsupported on this platform")
)

// ApplicationKeyVault seals recoverable application credentials with a Linux
// service-owned master key. It is safe for concurrent use. Every operation
// checks the master file; a missing or changed key is never silently replaced.
type ApplicationKeyVault struct {
	path string

	mu          sync.Mutex
	keySeen     bool
	keyIdentity applicationKeyFileIdentity
	keyDigest   [sha256.Size]byte
}

type applicationKeyFileIdentity struct {
	device uint64
	inode  uint64
}

// NewApplicationKeyVault configures an absolute, clean master-key path without
// reading or creating files. Unsupported platforms fail only when it is used.
func NewApplicationKeyVault(path string) *ApplicationKeyVault {
	return &ApplicationKeyVault{path: path}
}

// Seal encrypts a token and binds it to its credential ID. allowCreate must be
// true only while the caller holds its database lock and has established that
// no previously sealed application credentials exist. It does not authorize
// replacing an existing, malformed, or previously observed master key.
// When ciphertext already exists, the caller should authenticate an existing
// record first to detect a different but otherwise valid key after a restart.
func (v *ApplicationKeyVault) Seal(ctx context.Context, credentialID string, token string, allowCreate bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, err := v.loadMasterKey(ctx, allowCreate)
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gcm, err := applicationKeyGCM(key[:])
	if err != nil {
		return nil, err
	}
	sealed := make([]byte, len(applicationKeyHeader)+applicationKeyNonceSize)
	copy(sealed, applicationKeyHeader)
	nonce := sealed[len(applicationKeyHeader):]
	if _, err := rand.Read(nonce); err != nil {
		return nil, ErrApplicationKeyVaultUnavailable
	}
	plaintext := []byte(token)
	defer clear(plaintext)
	sealed = gcm.Seal(sealed, nonce, plaintext, []byte(applicationKeyPurpose+credentialID))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return sealed, nil
}

// Open authenticates and decrypts a token for its original credential ID. It
// never creates a master key, including when the sealed input is invalid.
func (v *ApplicationKeyVault) Open(ctx context.Context, credentialID string, sealed []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(sealed) < len(applicationKeyHeader)+applicationKeyNonceSize+applicationKeyTagSize ||
		string(sealed[:len(applicationKeyHeader)]) != applicationKeyHeader {
		return "", ErrApplicationKeyVaultCiphertext
	}
	key, err := v.loadMasterKey(ctx, false)
	if err != nil {
		return "", err
	}
	defer clear(key[:])
	if err := ctx.Err(); err != nil {
		return "", err
	}
	gcm, err := applicationKeyGCM(key[:])
	if err != nil {
		return "", err
	}
	nonceEnd := len(applicationKeyHeader) + applicationKeyNonceSize
	plaintext, err := gcm.Open(nil, sealed[len(applicationKeyHeader):nonceEnd], sealed[nonceEnd:], []byte(applicationKeyPurpose+credentialID))
	if err != nil {
		return "", ErrApplicationKeyVaultCiphertext
	}
	defer clear(plaintext)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func applicationKeyGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrApplicationKeyVaultUnavailable
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrApplicationKeyVaultUnavailable
	}
	return gcm, nil
}

func (v *ApplicationKeyVault) hasObservedKey() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.keySeen
}

func (v *ApplicationKeyVault) observeKey(key []byte, identity applicationKeyFileIdentity) error {
	digest := sha256.Sum256(key)
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.keySeen && (v.keyIdentity != identity || v.keyDigest != digest) {
		return ErrApplicationKeyVaultUnsafe
	}
	v.keySeen = true
	v.keyIdentity = identity
	v.keyDigest = digest
	return nil
}
