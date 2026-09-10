package backupformat

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"filippo.io/age"
)

// Create writes a backup using DefaultLimits. The caller must publish the
// destination only after a nil return and its own successful sync/close. On
// any error, discard the destination. This streaming API cannot retract bytes
// already written and intentionally does not finish the age stream on error.
// Passphrase policy beyond nonempty valid UTF-8 and 1024 bytes belongs to the
// caller. Go and the age API retain internal copies; erasure is not guaranteed.
func Create(ctx context.Context, destination io.Writer, passphrase []byte, manifest Manifest, entries Entries) error {
	return CreateWithLimits(ctx, destination, passphrase, manifest, entries, Limits{})
}

// CreateWithLimits is Create with caller-selected storage bounds. The work
// factor remains fixed at 18. Context checks bracket the age scrypt operation;
// the library's synchronous KDF cannot be interrupted partway through it.
func CreateWithLimits(ctx context.Context, destination io.Writer, passphrase []byte, manifest Manifest, entries Entries, limits Limits) error {
	if ctx == nil || destination == nil || !validPassphrase(passphrase) || entries.Database == nil || entries.Configuration == nil {
		return ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	limits, err := normalizeLimits(limits)
	if err != nil {
		return err
	}
	manifestData, err := encodeManifest(manifest, limits)
	if err != nil {
		return err
	}
	if (len(manifest.Files) == 3) != (entries.MasterKey != nil) {
		return ErrInvalidInput
	}
	recipient, err := age.NewScryptRecipient(string(passphrase))
	if err != nil {
		return ErrInvalidInput
	}
	recipient.SetWorkFactor(ScryptWorkFactor)
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded := &budgetWriter{
		destination: contextWriter{ctx: ctx, destination: destination},
		remaining:   limits.MaxEncryptedBytes,
	}
	encrypted, err := age.Encrypt(encoded, recipient)
	if err != nil {
		return creationError(ctx, err, ErrSink)
	}
	plain := &budgetWriter{
		destination: contextWriter{ctx: ctx, destination: encrypted},
		remaining:   limits.MaxPlaintextBytes,
	}
	if err := writeMember(ctx, plain, ManifestName, int64(len(manifestData)), bytes.NewReader(manifestData), ""); err != nil {
		return err
	}
	readers := []io.Reader{entries.Database, entries.Configuration, entries.MasterKey}
	for index, file := range manifest.Files {
		if err := writeMember(ctx, plain, file.Name, file.Size, readers[index], file.SHA256); err != nil {
			return err
		}
	}
	if _, err := plain.Write(make([]byte, 2*tarBlockBytes)); err != nil {
		return creationError(ctx, err, ErrSink)
	}
	if err := encrypted.Close(); err != nil {
		return creationError(ctx, err, ErrSink)
	}
	return ctx.Err()
}

// Inspect verifies every member, each descriptor, and authenticated EOF while
// discarding member contents. It never returns an early, unverified manifest.
func Inspect(ctx context.Context, source io.Reader, passphrase []byte, limits Limits) (Manifest, error) {
	return Extract(ctx, source, passphrase, Sinks{}, limits)
}

// Extract streams the archive into fixed-purpose staging sinks. A successful
// return proves format consistency and age authentication, not source identity
// or SQL safety. Failure requires discarding every sink, even when some bytes
// were already written. Nil sinks still receive full content verification.
func Extract(ctx context.Context, source io.Reader, passphrase []byte, sinks Sinks, limits Limits) (Manifest, error) {
	if ctx == nil || source == nil || !validPassphrase(passphrase) {
		return Manifest{}, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	limits, err := normalizeLimits(limits)
	if err != nil {
		return Manifest{}, err
	}
	encoded := &budgetReader{
		source:    &contextReader{ctx: ctx, source: source},
		remaining: limits.MaxEncryptedBytes,
	}
	header, err := readAgeHeader(encoded)
	if err != nil {
		return Manifest{}, archiveReadError(ctx, err)
	}
	identity, err := age.NewScryptIdentity(string(passphrase))
	if err != nil {
		return Manifest{}, ErrInvalidInput
	}
	identity.SetMaxWorkFactor(ScryptWorkFactor)
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	decrypted, err := age.Decrypt(io.MultiReader(bytes.NewReader(header), encoded), identity)
	if err != nil {
		return Manifest{}, archiveReadError(ctx, err)
	}
	plain := &budgetReader{
		source:    &contextReader{ctx: ctx, source: decrypted},
		remaining: limits.MaxPlaintextBytes,
	}
	manifestHeader, err := readMemberHeader(plain, ManifestName, limits.MaxManifestBytes)
	if err != nil {
		return Manifest{}, archiveReadError(ctx, err)
	}
	manifestData := make([]byte, int(manifestHeader.Size))
	if _, err := io.ReadFull(plain, manifestData); err != nil {
		return Manifest{}, archiveReadError(ctx, err)
	}
	if err := readZeroes(plain, paddingSize(manifestHeader.Size)); err != nil {
		return Manifest{}, archiveReadError(ctx, err)
	}
	manifest, err := decodeManifest(manifestData, limits)
	if err != nil {
		return Manifest{}, err
	}
	writers := []io.Writer{sinks.Database, sinks.Configuration, sinks.MasterKey}
	for index, file := range manifest.Files {
		hdr, err := readMemberHeader(plain, file.Name, file.Size)
		if err != nil {
			return Manifest{}, archiveReadError(ctx, err)
		}
		if hdr.Size != file.Size {
			return Manifest{}, ErrInvalidArchive
		}
		writer := writers[index]
		if writer == nil {
			writer = io.Discard
		}
		hash := sha256.New()
		written, err := io.CopyBuffer(io.MultiWriter(contextWriter{ctx: ctx, destination: writer}, hash), io.LimitReader(plain, file.Size), make([]byte, 64<<10))
		if err != nil {
			if errors.Is(err, ErrSink) {
				return Manifest{}, creationError(ctx, err, ErrSink)
			}
			return Manifest{}, archiveReadError(ctx, err)
		}
		if written != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
			return Manifest{}, ErrInvalidArchive
		}
		if err := readZeroes(plain, paddingSize(file.Size)); err != nil {
			return Manifest{}, archiveReadError(ctx, err)
		}
	}
	if err := readZeroes(plain, 2*tarBlockBytes); err != nil {
		return Manifest{}, archiveReadError(ctx, err)
	}
	// tar.Reader normally stops at two zero blocks and can conceal appended
	// plaintext or unauthenticated ciphertext. The format requires exact EOF.
	if err := requireEOF(plain); err != nil {
		return Manifest{}, archiveReadError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func writeMember(ctx context.Context, destination io.Writer, name string, size int64, source io.Reader, digest string) error {
	header, err := canonicalHeader(name, size)
	if err != nil {
		return ErrInvalidInput
	}
	if _, err := destination.Write(header); err != nil {
		return creationError(ctx, err, ErrSink)
	}
	guarded := &contextReader{ctx: ctx, source: source}
	hash := sha256.New()
	written, err := io.CopyBuffer(io.MultiWriter(destination, hash), io.LimitReader(guarded, size), make([]byte, 64<<10))
	if err != nil {
		return creationError(ctx, err, ErrSource)
	}
	if written != size || digest != "" && hex.EncodeToString(hash.Sum(nil)) != digest {
		return creationError(ctx, ErrSourceChanged, ErrSourceChanged)
	}
	if err := requireEOF(guarded); err != nil {
		if errors.Is(err, ErrInvalidArchive) {
			return creationError(ctx, ErrSourceChanged, ErrSourceChanged)
		}
		return creationError(ctx, err, ErrSource)
	}
	if _, err := destination.Write(make([]byte, int(paddingSize(size)))); err != nil {
		return creationError(ctx, err, ErrSink)
	}
	return nil
}

func canonicalHeader(name string, size int64) ([]byte, error) {
	var block bytes.Buffer
	writer := tar.NewWriter(&block)
	err := writer.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     0600,
		Size:     size,
		ModTime:  time.Unix(0, 0).UTC(),
		Format:   tar.FormatGNU,
	})
	if err != nil || block.Len() != tarBlockBytes {
		return nil, ErrInvalidInput
	}
	return block.Bytes(), nil
}

func readMemberHeader(source io.Reader, name string, maximum int64) (*tar.Header, error) {
	var block [tarBlockBytes]byte
	if _, err := io.ReadFull(source, block[:]); err != nil {
		return nil, err
	}
	// Check the raw type before archive/tar can consume and hide PAX, GNU
	// long-name, or global extension entries from the caller of Next.
	if block[156] != tar.TypeReg {
		return nil, ErrInvalidArchive
	}
	header, err := tar.NewReader(bytes.NewReader(block[:])).Next()
	if err != nil || header.Name != name || header.Size < 1 {
		return nil, ErrInvalidArchive
	}
	if header.Size > maximum {
		return nil, ErrLimit
	}
	canonical, err := canonicalHeader(name, header.Size)
	if err != nil || !bytes.Equal(block[:], canonical) {
		return nil, ErrInvalidArchive
	}
	return header, nil
}

func readAgeHeader(source io.Reader) ([]byte, error) {
	// The official age parser remains responsible for all header semantics.
	// This framing bound rejects an oversized header before parser allocation
	// and before any attacker-selected KDF work can begin.
	header := make([]byte, 0, 256)
	lineStart := 0
	var next [1]byte
	for len(header) < maxHeaderBytes {
		if _, err := io.ReadFull(source, next[:]); err != nil {
			return nil, err
		}
		header = append(header, next[0])
		if next[0] != '\n' {
			continue
		}
		line := header[lineStart:]
		if lineStart == 0 && string(line) != "age-encryption.org/v1\n" {
			return nil, ErrInvalidArchive
		}
		if bytes.HasPrefix(line, []byte("--- ")) {
			return header, nil
		}
		lineStart = len(header)
	}
	return nil, ErrLimit
}

func readZeroes(source io.Reader, count int64) error {
	var block [2 * tarBlockBytes]byte
	if count < 0 || count > int64(len(block)) {
		return ErrInvalidArchive
	}
	if _, err := io.ReadFull(source, block[:count]); err != nil {
		return err
	}
	for _, value := range block[:count] {
		if value != 0 {
			return ErrInvalidArchive
		}
	}
	return nil
}

func requireEOF(source io.Reader) error {
	var extra [1]byte
	_, err := io.ReadFull(source, extra[:])
	if err == nil {
		return ErrInvalidArchive
	}
	if err == io.EOF {
		return nil
	}
	return err
}

func paddingSize(size int64) int64 {
	return (tarBlockBytes - size%tarBlockBytes) % tarBlockBytes
}
