//go:build linux && !android

package library

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// The caller keeps dir open for the stage's lifetime and must exclude other
// writers. Linux has no conditional rename or unlink by inode; the final name
// checks rely on that contract. A stage must not be used concurrently.
type nfoRecoveryStage struct {
	dir       *os.Root
	directory *os.File
	pin       *os.File
	name      string
	info      os.FileInfo
	data      []byte
}

func stageNFORecovery(ctx context.Context, dir *os.Root, data []byte) (*nfoRecoveryStage, error) {
	return stageNFORecoveryWithWrite(ctx, dir, data, (*os.File).Write)
}

func stageNFORecoveryWithWrite(ctx context.Context, dir *os.Root, data []byte, write func(*os.File, []byte) (int, error)) (_ *nfoRecoveryStage, resultErr error) {
	if ctx == nil || dir == nil || len(data) == 0 || len(data) > maxLocalNFOBytes {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory, err := openScanFile(dir, ".")
	if err != nil {
		return nil, fmt.Errorf("open NFO recovery directory: %w", err)
	}
	stage := &nfoRecoveryStage{dir: dir, directory: directory, name: ".goby-nfo-recovery-" + rand.Text() + ".tmp", data: bytes.Clone(data)}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, stage.close(ctx))
		}
	}()
	file, err := openLiteralNFOLeaf(directory, stage.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return nil, fmt.Errorf("create NFO recovery stage: %w", err)
	}
	stage.pin = file
	stage.info, err = file.Stat()
	if err != nil {
		return nil, fmt.Errorf("identify NFO recovery stage: %w", err)
	}
	if !stage.info.Mode().IsRegular() {
		return nil, fmt.Errorf("NFO recovery stage is not a regular file: %w", ErrSourceChanged)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n, err := write(file, stage.data)
	if err == nil && n != len(stage.data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return nil, fmt.Errorf("write complete NFO recovery stage: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("sync NFO recovery stage: %w", err)
	}
	written, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat written NFO recovery stage: %w", err)
	}
	// Keep the created inode pinned across the write descriptor's close and
	// independent read-back so a replacement cannot reuse its identity.
	pin, err := openLiteralNFOLeaf(directory, stage.name, unix.O_PATH)
	if err != nil {
		return nil, fmt.Errorf("pin NFO recovery stage: %w", err)
	}
	pinned, err := pin.Stat()
	if err != nil || !sameLocalNFOFile(written, pinned) {
		return nil, errors.Join(fmt.Errorf("identify pinned NFO recovery stage: %w", ErrSourceChanged), err, closePrimarySidecarResource(ctx, pin))
	}
	stage.pin, stage.info = pin, written
	if err := closePrimarySidecarResource(ctx, file); err != nil {
		return nil, err
	}
	if err := stage.verifyBytes(ctx, stage.name); err != nil {
		return nil, err
	}
	return stage, nil
}

// checkReplacement does not modify either entry. The caller proves the exact
// damaged NFO and the source/candidate fence in check.
func (stage *nfoRecoveryStage) checkReplacement(ctx context.Context, leaf string, check func() error) error {
	if stage == nil || stage.directory == nil || ctx == nil || check == nil {
		return ErrInvalidInput
	}
	if leaf == stage.name || leaf == "" || leaf == "." || leaf == ".." ||
		strings.ContainsAny(leaf, "/\\\x00") || !strings.EqualFold(filepath.Ext(leaf), ".nfo") {
		return ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := stage.checkName(stage.name, stage.info); err != nil {
		return err
	}
	before, err := stage.dir.Lstat(leaf)
	if err != nil || !before.Mode().IsRegular() || os.SameFile(stage.info, before) {
		return errors.Join(fmt.Errorf("NFO recovery target is not a distinct regular file: %w", ErrSourceChanged), err)
	}
	if err := check(); err != nil {
		return err
	}
	if err := stage.checkName(leaf, before); err != nil {
		return err
	}
	if err := stage.checkName(stage.name, stage.info); err != nil {
		return err
	}
	return ctx.Err()
}

// replace requires successful preflight for the same leaf on a live stage.
// The caller must not detach the rename from the publication path.
func (stage *nfoRecoveryStage) replace(ctx context.Context, leaf string) error {
	return stage.replaceWithRename(ctx, leaf, (*os.Root).Rename)
}

func (stage *nfoRecoveryStage) replaceWithRename(ctx context.Context, leaf string, rename func(*os.Root, string, string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// A reported failure does not prove the rename was not applied. The
	// caller must re-observe the leaf rather than retry or roll back the rename.
	if err := rename(stage.dir, stage.name, leaf); err != nil {
		return errors.Join(errNFORecoveryPublicationUncertain, fmt.Errorf("replace damaged NFO: %w", err))
	}
	return nil
}

// verifyPublished syncs the directory and independently rereads the renamed
// leaf. The caller keeps the stage open through verification.
func (stage *nfoRecoveryStage) verifyPublished(ctx context.Context, leaf string) error {
	return stage.verifyPublishedWithSync(ctx, leaf, (*os.File).Sync)
}

func (stage *nfoRecoveryStage) verifyPublishedWithSync(ctx context.Context, leaf string, syncDir func(*os.File) error) error {
	if err := syncDir(stage.directory); err != nil {
		return errors.Join(errNFORecoveryPublicationUncertain, fmt.Errorf("sync NFO recovery directory: %w", err))
	}
	if err := stage.verifyBytes(ctx, leaf); err != nil {
		return errors.Join(errNFORecoveryPublicationUncertain, err)
	}
	return nil
}

func (stage *nfoRecoveryStage) checkName(name string, expected os.FileInfo) error {
	current, err := stage.dir.Lstat(name)
	if err != nil || !sameLocalNFOFile(expected, current) {
		return errors.Join(fmt.Errorf("NFO recovery entry %q changed: %w", name, ErrSourceChanged), err)
	}
	return nil
}

func (stage *nfoRecoveryStage) verifyBytes(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	before, err := stage.dir.Lstat(name)
	// Rename can change ctime. At the destination require the pinned identity
	// and use a new stamp for the coherent bounded read of that pathname.
	if err != nil || !before.Mode().IsRegular() || !os.SameFile(stage.info, before) || before.Size() != int64(len(stage.data)) ||
		name == stage.name && !sameLocalNFOFile(stage.info, before) {
		return errors.Join(fmt.Errorf("NFO recovery entry %q does not match the stage: %w", name, ErrSourceChanged), err)
	}
	file, err := openLiteralNFOLeaf(stage.directory, name, os.O_RDONLY)
	if err != nil {
		return fmt.Errorf("reopen NFO recovery entry %q: %w", name, err)
	}
	data, invalid, err := readOpenedLocalNFO(ctx, file, before, func() (os.FileInfo, error) { return stage.dir.Lstat(name) })
	if err != nil {
		return fmt.Errorf("read back NFO recovery entry %q: %w", name, err)
	}
	if invalid || !bytes.Equal(data, stage.data) {
		return fmt.Errorf("NFO recovery entry %q failed exact read-back: %w", name, ErrSourceChanged)
	}
	return ctx.Err()
}

// os.Root may resolve an in-root symlink after an O_NOFOLLOW failure. Use
// openat for a literal leaf below the already contained directory descriptor.
func openLiteralNFOLeaf(directory *os.File, name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(directory.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}

// close is idempotent and never operates on the published leaf or the caller's
// directory handle. It removes a temporary name only while its inode is pinned.
func (stage *nfoRecoveryStage) close(ctx context.Context) (resultErr error) {
	if stage == nil || stage.directory == nil {
		return nil
	}
	directory, pin := stage.directory, stage.pin
	stage.directory, stage.pin, stage.data = nil, nil, nil
	defer func() {
		if pin != nil {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, pin))
		}
		resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, directory))
	}()
	if stage.info == nil {
		return nil
	}
	current, err := stage.dir.Lstat(stage.name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(stage.info, current) {
		return errors.Join(fmt.Errorf("NFO recovery temporary name no longer identifies the owned file: %w", ErrSourceChanged), err)
	}
	if err := unix.Unlinkat(int(directory.Fd()), stage.name, 0); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove owned NFO recovery stage: %w", err)
	}
	return nil
}
