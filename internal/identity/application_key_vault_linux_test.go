//go:build linux

package identity

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestApplicationKeyVaultRoundTripAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	vault := NewApplicationKeyVault(path)
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("constructor unexpectedly accessed a master file")
	}
	const id = "credential-one"
	const token = "private-application-token-sentinel"
	sealed, err := vault.Seal(context.Background(), id, token, true)
	if err != nil {
		t.Fatalf("seal initial credential: %v", err)
	}
	if bytes.Contains(sealed, []byte(token)) {
		t.Fatal("sealed credential contains plaintext")
	}
	key, err := os.ReadFile(path)
	if err != nil || len(key) != applicationKeyMasterSize {
		t.Fatalf("master key has invalid length or cannot be read: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("master key has unsafe permissions: %v", err)
	}
	for _, reader := range []*ApplicationKeyVault{vault, NewApplicationKeyVault(path)} {
		opened, err := reader.Open(context.Background(), id, sealed)
		if err != nil || opened != token {
			t.Fatalf("credential did not survive reopening: %v", err)
		}
	}
	again, err := vault.Seal(context.Background(), id, token, false)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sealed, again) {
		t.Fatal("repeated sealing reused a nonce")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(key, after) {
		t.Fatalf("existing master key was changed: %v", err)
	}
}

func TestApplicationKeyVaultRejectsTamperingAndWrongKeys(t *testing.T) {
	vault := NewApplicationKeyVault(filepath.Join(t.TempDir(), "master.key"))
	sealed, err := vault.Seal(context.Background(), "original-id", "private-token", true)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]struct {
		id     string
		sealed []byte
	}{
		"wrong credential ID": {id: "other-id", sealed: sealed},
		"empty ciphertext":    {id: "original-id", sealed: nil},
		"truncated header":    {id: "original-id", sealed: sealed[:len(applicationKeyHeader)-1]},
		"truncated tag":       {id: "original-id", sealed: sealed[:len(applicationKeyHeader)+applicationKeyNonceSize+applicationKeyTagSize-1]},
		"trailing byte":       {id: "original-id", sealed: append(bytes.Clone(sealed), 0)},
	}
	for name, offset := range map[string]int{
		"wrong magic":         0,
		"unsupported version": len(applicationKeyHeader) - 1,
		"tampered nonce":      len(applicationKeyHeader),
		"tampered ciphertext": len(applicationKeyHeader) + applicationKeyNonceSize,
		"tampered tag":        len(sealed) - 1,
	} {
		changed := bytes.Clone(sealed)
		changed[offset] ^= 0x80
		tests[name] = struct {
			id     string
			sealed []byte
		}{id: "original-id", sealed: changed}
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			opened, err := vault.Open(context.Background(), test.id, test.sealed)
			if !errors.Is(err, ErrApplicationKeyVaultCiphertext) || opened != "" {
				t.Fatalf("modified credential was not rejected: %v", err)
			}
		})
	}
	other := NewApplicationKeyVault(filepath.Join(t.TempDir(), "master.key"))
	if _, err := other.Seal(context.Background(), "other", "other-token", true); err != nil {
		t.Fatal(err)
	}
	if opened, err := other.Open(context.Background(), "original-id", sealed); !errors.Is(err, ErrApplicationKeyVaultCiphertext) || opened != "" {
		t.Fatalf("different master key authenticated ciphertext: %v", err)
	}
}

func TestApplicationKeyVaultMissingKeyNeverCreatesWithoutAuthorization(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "master.key")
	vault := NewApplicationKeyVault(path)
	if _, err := vault.Seal(context.Background(), "id", "private-token", false); !errors.Is(err, ErrApplicationKeyVaultMissing) {
		t.Fatalf("missing key should fail without creation: %v", err)
	}
	sealed := make([]byte, len(applicationKeyHeader)+applicationKeyNonceSize+applicationKeyTagSize)
	copy(sealed, applicationKeyHeader)
	if _, err := vault.Open(context.Background(), "id", sealed); !errors.Is(err, ErrApplicationKeyVaultMissing) {
		t.Fatalf("opening should never create a key: %v", err)
	}
	if _, err := vault.Open(context.Background(), "id", nil); !errors.Is(err, ErrApplicationKeyVaultCiphertext) {
		t.Fatalf("malformed input should fail: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unauthorized operation wrote files: %v", err)
	}
}

func TestApplicationKeyVaultObservedKeyCannotBeRecreatedOrReplaced(t *testing.T) {
	for _, operation := range []string{"delete", "replace", "rewrite"} {
		t.Run(operation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "master.key")
			vault := NewApplicationKeyVault(path)
			sealed, err := vault.Seal(context.Background(), "id", "private-token", true)
			if err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			wantErr := ErrApplicationKeyVaultUnsafe
			switch operation {
			case "delete":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				wantErr = ErrApplicationKeyVaultMissing
			case "replace":
				if err := os.Rename(path, path+".previous"); err != nil {
					t.Fatal(err)
				}
				// Matching bytes still must not conceal a replaced file identity.
				writeApplicationKeyFixture(t, path, original, 0600)
			case "rewrite":
				original[0] ^= 0xff
				writeApplicationKeyFixture(t, path, original, 0600)
			}
			if _, err := vault.Seal(context.Background(), "new-id", "new-token", true); !errors.Is(err, wantErr) {
				t.Fatalf("changed master key was accepted for sealing: %v", err)
			}
			if _, err := vault.Open(context.Background(), "id", sealed); !errors.Is(err, wantErr) {
				t.Fatalf("changed master key was accepted for opening: %v", err)
			}
			if operation == "delete" {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("deleted master key was recreated")
				}
			} else if after, err := os.ReadFile(path); err != nil || !bytes.Equal(original, after) {
				t.Fatalf("changed master file was rewritten: %v", err)
			}
		})
	}
}

func TestApplicationKeyVaultRejectsUnsafeMasterFilesWithoutRepair(t *testing.T) {
	tests := map[string]struct {
		contents []byte
		mode     os.FileMode
	}{
		"empty":          {contents: nil, mode: 0600},
		"short":          {contents: bytes.Repeat([]byte{1}, 31), mode: 0600},
		"long":           {contents: bytes.Repeat([]byte{1}, 33), mode: 0600},
		"world readable": {contents: bytes.Repeat([]byte{1}, 32), mode: 0644},
		"group readable": {contents: bytes.Repeat([]byte{1}, 32), mode: 0640},
		"read only":      {contents: bytes.Repeat([]byte{1}, 32), mode: 0400},
		"executable":     {contents: bytes.Repeat([]byte{1}, 32), mode: 0700},
		"setuid":         {contents: bytes.Repeat([]byte{1}, 32), mode: 0600 | os.ModeSetuid},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "master.key")
			writeApplicationKeyFixture(t, path, test.contents, test.mode)
			vault := NewApplicationKeyVault(path)
			if _, err := vault.Seal(context.Background(), "id", "private-token", true); !errors.Is(err, ErrApplicationKeyVaultUnsafe) {
				t.Fatalf("unsafe master file was accepted: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(test.contents, after) {
				t.Fatalf("unsafe master file was rewritten: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode() != test.mode {
				t.Fatalf("unsafe master file permissions were repaired: %v", err)
			}
		})
	}
}

func TestApplicationKeyVaultRejectsForeignOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing fixture ownership requires root")
	}
	path := filepath.Join(t.TempDir(), "master.key")
	writeApplicationKeyFixture(t, path, bytes.Repeat([]byte{1}, 32), 0600)
	if err := os.Chown(path, 1, -1); err != nil {
		t.Fatal(err)
	}
	vault := NewApplicationKeyVault(path)
	if _, err := vault.Seal(context.Background(), "id", "private-token", true); !errors.Is(err, ErrApplicationKeyVaultUnsafe) {
		t.Fatalf("foreign-owned master file was accepted: %v", err)
	}
}

func TestApplicationKeyVaultRejectsLinksAndSpecialFiles(t *testing.T) {
	for _, kind := range []string{"hard link", "file symlink", "parent symlink", "ancestor symlink", "directory", "fifo", "parent fifo"} {
		t.Run(kind, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "master.key")
			key := bytes.Repeat([]byte{1}, 32)
			switch kind {
			case "hard link":
				writeApplicationKeyFixture(t, path, key, 0600)
				if err := os.Link(path, filepath.Join(directory, "alias.key")); err != nil {
					t.Fatal(err)
				}
			case "file symlink":
				target := filepath.Join(directory, "target.key")
				writeApplicationKeyFixture(t, target, key, 0600)
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "parent symlink", "ancestor symlink":
				target := filepath.Join(directory, "target")
				if err := os.MkdirAll(filepath.Join(target, "nested"), 0700); err != nil {
					t.Fatal(err)
				}
				alias := filepath.Join(directory, "alias")
				if err := os.Symlink(target, alias); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(alias, "master.key")
				if kind == "ancestor symlink" {
					path = filepath.Join(alias, "nested", "master.key")
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "parent fifo":
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(path, "nested.key")
			}
			result := make(chan error, 1)
			go func() {
				_, err := NewApplicationKeyVault(path).Seal(context.Background(), "id", "private-token", true)
				result <- err
			}()
			select {
			case err := <-result:
				if !errors.Is(err, ErrApplicationKeyVaultUnsafe) {
					t.Fatalf("unsafe filesystem entry was accepted: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("opening an unsafe filesystem entry blocked")
			}
			if kind == "parent symlink" || kind == "ancestor symlink" {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("vault created a key through a symlinked ancestor")
				}
			}
		})
	}
}

func TestApplicationKeyVaultRequiresCanonicalAbsolutePath(t *testing.T) {
	for _, path := range []string{"", "master.key", "/", "/tmp/../master.key", "/tmp/./master.key", "/tmp//master.key"} {
		if _, err := NewApplicationKeyVault(path).Seal(context.Background(), "id", "private-token", true); !errors.Is(err, ErrApplicationKeyVaultUnsafe) {
			t.Fatalf("invalid master-key path was accepted: %v", err)
		}
	}
}

func TestApplicationKeyVaultConcurrentCreationUsesOneMasterKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	vaults := []*ApplicationKeyVault{NewApplicationKeyVault(path), NewApplicationKeyVault(path)}
	const count = 24
	sealed := make([][]byte, count)
	errs := make([]error, count)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := range count {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			sealed[i], errs[i] = vaults[i%len(vaults)].Seal(context.Background(), "shared-id", "private-token", true)
		}()
	}
	close(start)
	workers.Wait()
	reader := NewApplicationKeyVault(path)
	for i := range count {
		if errs[i] != nil {
			t.Fatalf("concurrent creation failed: %v", errs[i])
		}
		opened, err := reader.Open(context.Background(), "shared-id", sealed[i])
		if err != nil || opened != "private-token" {
			t.Fatalf("concurrent instances selected different keys: %v", err)
		}
	}
}

func TestApplicationKeyVaultExclusiveCreationReusesExistingKey(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "master.key")
	expected := bytes.Repeat([]byte{42}, 32)
	writeApplicationKeyFixture(t, path, expected, 0600)
	fd, err := openApplicationKeyDirectory(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	if err := lockApplicationKeyDirectory(context.Background(), fd); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	key, _, err := createApplicationKeyFile(context.Background(), fd, filepath.Base(path))
	if err != nil || !bytes.Equal(key[:], expected) {
		t.Fatalf("exclusive-create race did not reuse the existing key: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, expected) {
		t.Fatalf("exclusive-create race rewrote the existing key: %v", err)
	}
}

func TestApplicationKeyVaultCancellationDoesNotCreateFiles(t *testing.T) {
	t.Run("already cancelled", func(t *testing.T) {
		directory := t.TempDir()
		vault := NewApplicationKeyVault(filepath.Join(directory, "master.key"))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := vault.Seal(ctx, "id", "private-token", true); !errors.Is(err, context.Canceled) {
			t.Fatalf("sealing ignored cancellation: %v", err)
		}
		if _, err := vault.Open(ctx, "id", nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("opening ignored cancellation: %v", err)
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 {
			t.Fatalf("cancelled operation created files: %v", err)
		}
	})
	t.Run("waiting for directory lock", func(t *testing.T) {
		directory := t.TempDir()
		fd, err := openApplicationKeyDirectory(context.Background(), directory)
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.Close(fd)
		if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		defer syscall.Flock(fd, syscall.LOCK_UN)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		result := make(chan error, 1)
		go func() {
			_, err := NewApplicationKeyVault(filepath.Join(directory, "master.key")).Seal(ctx, "id", "private-token", true)
			result <- err
		}()
		select {
		case err := <-result:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lock wait ignored deadline: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("lock wait did not respond to cancellation")
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 {
			t.Fatalf("cancelled lock wait created files: %v", err)
		}
	})
}

func writeApplicationKeyFixture(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
