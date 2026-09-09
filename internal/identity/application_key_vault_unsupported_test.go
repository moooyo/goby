//go:build !linux

package identity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestApplicationKeyVaultUnsupportedPlatformFailsWithoutWriting(t *testing.T) {
	directory := t.TempDir()
	vault := NewApplicationKeyVault(filepath.Join(directory, "master.key"))
	if _, err := vault.Seal(context.Background(), "id", "private-token", true); !errors.Is(err, ErrApplicationKeyVaultUnsupported) {
		t.Fatalf("unsupported platform did not fail closed: %v", err)
	}
	sealed := make([]byte, len(applicationKeyHeader)+applicationKeyNonceSize+applicationKeyTagSize)
	copy(sealed, applicationKeyHeader)
	if _, err := vault.Open(context.Background(), "id", sealed); !errors.Is(err, ErrApplicationKeyVaultUnsupported) {
		t.Fatalf("unsupported platform attempted decryption: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unsupported platform wrote files: %v", err)
	}
}
