package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	scanEvidenceMarker        = "goby-scan-evidence-owner-v1"
	scanEvidenceManifest      = ".goby-scan-evidence.json"
	scanEvidencePending       = ".goby-scan-evidence.next"
	scanEvidenceLockName      = ".goby-scan-evidence.lock"
	scanEvidenceManifestLimit = 16 << 10
)

var scanEvidenceChildName = regexp.MustCompile(`^scan-evidence-[0-9a-f]{32}$`)

type scanEvidenceDiskLease struct {
	Name       string `json:"name"`
	Bytes      int64  `json:"bytes"`
	Device     uint64 `json:"device"`
	Inode      uint64 `json:"inode"`
	Created    bool   `json:"created"`
	HandleType int32  `json:"handleType"`
	Handle     []byte `json:"handle"`
}

type scanEvidenceFilesystem struct {
	Type int64    `json:"type"`
	ID   [2]int32 `json:"id"`
}

type scanEvidenceDocument struct {
	Marker     string                  `json:"marker"`
	Scope      string                  `json:"scope"`
	Owner      string                  `json:"owner"`
	Filesystem scanEvidenceFilesystem  `json:"filesystem"`
	Leases     []scanEvidenceDiskLease `json:"leases"`
}

type scanEvidenceDisk struct {
	root     *os.Root
	lock     *os.File
	path     string
	info     os.FileInfo
	excluded []string
	document scanEvidenceDocument
}

func openScanEvidenceDisk(path, scope string, excluded []string) (_ *scanEvidenceDisk, result error) {
	if !scanEvidencePlatformSupported() {
		return nil, fmt.Errorf("%w: scan evidence ownership requires Linux", ErrUnavailable)
	}
	if len(scope) != 64 || strings.Trim(scope, "0123456789abcdef") != "" {
		return nil, ErrInvalidInput
	}
	if err := checkScanEvidencePlacement(path, excluded); err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("open configured scan evidence directory: %w", err)
	}
	if err = scanEvidencePrivateInfo(before, true); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	disk := &scanEvidenceDisk{root: root, path: path, info: before, excluded: append([]string(nil), excluded...)}
	defer func() {
		if result != nil {
			result = errors.Join(result, disk.close())
		}
	}()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		return nil, errors.Join(ErrUnavailable, err)
	}
	_, markerErr := root.Lstat(scanEvidenceManifest)
	fresh := errors.Is(markerErr, os.ErrNotExist)
	if markerErr != nil && !fresh {
		return nil, markerErr
	}
	if fresh {
		names, err := scanEvidenceNames(root, 1)
		if err != nil || len(names) != 0 {
			return nil, errors.Join(fmt.Errorf("%w: unowned scan evidence directory is not empty", ErrUnavailable), err)
		}
	}
	disk.lock, err = scanEvidenceOpenFile(root, scanEvidenceLockName, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err = scanEvidenceLockFile(disk.lock); err != nil {
		return nil, err
	}
	if err = disk.checkAnchor(); err != nil {
		return nil, err
	}
	if fresh {
		names, err := scanEvidenceNames(root, 2)
		if err != nil || len(names) != 1 || names[0] != scanEvidenceLockName {
			return nil, errors.Join(ErrUnavailable, err)
		}
		owner, err := randomID()
		if err != nil {
			return nil, err
		}
		filesystem, err := scanEvidenceFilesystemIdentity(root)
		if err != nil {
			return nil, err
		}
		disk.document = scanEvidenceDocument{Marker: scanEvidenceMarker, Scope: scope, Owner: owner, Filesystem: filesystem, Leases: []scanEvidenceDiskLease{}}
		if err = disk.writeDocument(disk.document); err != nil {
			return nil, err
		}
	} else {
		document, err := disk.readDocument()
		if err != nil {
			return nil, err
		}
		if document.Scope != scope {
			return nil, fmt.Errorf("%w: scan evidence belongs to another catalog or server", ErrUnavailable)
		}
		disk.document = document
	}
	filesystem, err := scanEvidenceFilesystemIdentity(root)
	if err != nil || filesystem != disk.document.Filesystem {
		return nil, errors.Join(fmt.Errorf("%w: scan evidence filesystem identity changed", ErrUnavailable), err)
	}
	// Only the fixed interrupted-manifest slot belongs to this protocol. Other
	// names are never inferred from a prefix or adopted as old scan evidence.
	if _, err = root.Lstat(scanEvidencePending); err == nil {
		file, openErr := scanEvidenceOpenFile(root, scanEvidencePending, os.O_RDONLY, 0)
		if openErr != nil {
			return nil, openErr
		}
		if err = file.Close(); err != nil {
			return nil, err
		}
		if err = root.Remove(scanEvidencePending); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err = disk.checkInventory(); err != nil {
		return nil, err
	}
	for len(disk.document.Leases) != 0 {
		lease := disk.document.Leases[0]
		if err = disk.reclaim(lease); err != nil {
			return nil, fmt.Errorf("recover owned scan evidence: %w", err)
		}
		if err = disk.release(lease.Name); err != nil {
			return nil, err
		}
	}
	return disk, nil
}

func scanEvidenceCanonical(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(absolute)
		if err == nil {
			for index := len(tail) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, tail[index])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(absolute) == absolute {
			return "", err
		}
		tail = append(tail, filepath.Base(absolute))
		absolute = filepath.Dir(absolute)
	}
}

func checkScanEvidencePlacement(path string, excluded []string) error {
	actual, err := filepath.EvalSymlinks(path)
	if err != nil || actual != path {
		return fmt.Errorf("%w: scan evidence directory must already exist at its canonical path", ErrInvalidInput)
	}
	for _, other := range excluded {
		if other == "" {
			continue
		}
		resolved, err := scanEvidenceCanonical(other)
		if err != nil {
			return fmt.Errorf("%w: cannot resolve an excluded scan evidence root", ErrInvalidInput)
		}
		if pathWithin(path, resolved) || pathWithin(resolved, path) {
			return fmt.Errorf("%w: scan evidence must not overlap media or another work root", ErrInvalidInput)
		}
	}
	return nil
}

func scanEvidenceNames(root *os.Root, maximum int) ([]string, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, readErr := directory.ReadDir(maximum + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, errors.Join(readErr, closeErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(entries) > maximum {
		return nil, fmt.Errorf("%w: scan evidence directory has unexpected contents", ErrUnavailable)
	}
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name()
	}
	sort.Strings(names)
	return names, nil
}

func (disk *scanEvidenceDisk) checkAnchor() error {
	if disk == nil || disk.root == nil || disk.lock == nil {
		return ErrUnavailable
	}
	if err := checkScanEvidencePlacement(disk.path, disk.excluded); err != nil {
		return err
	}
	for _, infoSource := range []func() (os.FileInfo, error){func() (os.FileInfo, error) { return os.Lstat(disk.path) }, func() (os.FileInfo, error) { return disk.root.Stat(".") }} {
		info, err := infoSource()
		if err != nil || !os.SameFile(info, disk.info) {
			return errors.Join(ErrUnavailable, err)
		}
		if err = scanEvidencePrivateInfo(info, true); err != nil {
			return err
		}
	}
	held, err := disk.lock.Stat()
	if err != nil {
		return err
	}
	named, err := disk.root.Lstat(scanEvidenceLockName)
	if err != nil || !os.SameFile(held, named) {
		return errors.Join(ErrUnavailable, err)
	}
	return scanEvidencePrivateInfo(named, false)
}

func (disk *scanEvidenceDisk) check() error {
	if err := disk.checkAnchor(); err != nil {
		return err
	}
	filesystem, err := scanEvidenceFilesystemIdentity(disk.root)
	if err != nil || filesystem != disk.document.Filesystem {
		return errors.Join(ErrUnavailable, err)
	}
	document, err := disk.readDocument()
	if err != nil {
		return err
	}
	expected, _ := json.Marshal(disk.document)
	actual, _ := json.Marshal(document)
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("%w: scan evidence ownership changed", ErrUnavailable)
	}
	return disk.checkInventory()
}

func (disk *scanEvidenceDisk) checkInventory() error {
	names, err := scanEvidenceNames(disk.root, scanEvidenceMaxPasses+2)
	if err != nil {
		return err
	}
	allowed := map[string]bool{scanEvidenceManifest: true, scanEvidenceLockName: true}
	for _, lease := range disk.document.Leases {
		allowed[lease.Name] = true
	}
	for _, name := range names {
		if !allowed[name] {
			return fmt.Errorf("%w: unknown file in scan evidence owner directory", ErrUnavailable)
		}
	}
	return nil
}

func (disk *scanEvidenceDisk) readDocument() (scanEvidenceDocument, error) {
	var document scanEvidenceDocument
	file, err := scanEvidenceOpenFile(disk.root, scanEvidenceManifest, os.O_RDONLY, 0)
	if err != nil {
		return document, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, scanEvidenceManifestLimit+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(raw) > scanEvidenceManifestLimit {
		return document, errors.Join(ErrUnavailable, readErr, closeErr)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&document); err != nil {
		return document, err
	}
	canonical, _ := json.Marshal(document)
	if !bytes.Equal(raw, append(canonical, '\n')) || document.Marker != scanEvidenceMarker || len(document.Scope) != 64 ||
		len(document.Owner) != 32 || strings.Trim(document.Owner, "0123456789abcdef") != "" || document.Leases == nil || len(document.Leases) > scanEvidenceMaxPasses {
		return document, fmt.Errorf("%w: invalid scan evidence owner manifest", ErrUnavailable)
	}
	seen, total := make(map[string]bool), int64(0)
	for _, lease := range document.Leases {
		if !scanEvidenceChildName.MatchString(lease.Name) || seen[lease.Name] || lease.Bytes < 64<<10 || lease.Bytes > 1<<30 ||
			lease.Created && lease.Inode == 0 || !lease.Created && (lease.Device != 0 || lease.Inode != 0 || lease.HandleType != 0 || len(lease.Handle) != 0) ||
			lease.HandleType < 0 || lease.HandleType == 0 && len(lease.Handle) != 0 || lease.HandleType > 0 && (len(lease.Handle) < 1 || len(lease.Handle) > 128) {
			return document, ErrUnavailable
		}
		seen[lease.Name] = true
		total += lease.Bytes
	}
	if total > scanEvidenceMaxReservation {
		return document, ErrUnavailable
	}
	return document, nil
}

func (disk *scanEvidenceDisk) writeDocument(document scanEvidenceDocument) error {
	data, err := json.Marshal(document)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > scanEvidenceManifestLimit {
		return ErrInvalidInput
	}
	file, err := scanEvidenceOpenFile(disk.root, scanEvidencePending, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err = disk.root.Rename(scanEvidencePending, scanEvidenceManifest); err != nil {
		return err
	}
	directory, err := disk.root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func (disk *scanEvidenceDisk) reserve(name string, bytes int64) error {
	if err := disk.check(); err != nil {
		return err
	}
	if !scanEvidenceChildName.MatchString(name) || bytes < 64<<10 || bytes > 1<<30 || len(disk.document.Leases) >= scanEvidenceMaxPasses {
		return ErrBusy
	}
	total := bytes
	for _, lease := range disk.document.Leases {
		if lease.Name == name {
			return ErrUnavailable
		}
		total += lease.Bytes
	}
	if total > scanEvidenceMaxReservation {
		return ErrBusy
	}
	next := disk.document
	next.Leases = append(append([]scanEvidenceDiskLease{}, disk.document.Leases...), scanEvidenceDiskLease{Name: name, Bytes: bytes})
	if err := disk.writeDocument(next); err != nil {
		return err
	}
	disk.document = next
	return nil
}

func (disk *scanEvidenceDisk) created(name string, info os.FileInfo) error {
	if err := disk.check(); err != nil {
		return err
	}
	if err := scanEvidencePrivateInfo(info, true); err != nil {
		return err
	}
	device, inode, err := scanEvidenceFileIdentity(info)
	if err != nil {
		return err
	}
	child, err := disk.root.OpenRoot(name)
	if err != nil {
		return err
	}
	held, err := child.Stat(".")
	if err != nil || !os.SameFile(held, info) {
		return errors.Join(ErrUnavailable, err, child.Close())
	}
	handleType, handle, handleErr := scanEvidencePersistentHandle(child)
	if err = errors.Join(handleErr, child.Close()); err != nil {
		return err
	}
	next := disk.document
	next.Leases = append([]scanEvidenceDiskLease{}, disk.document.Leases...)
	found := false
	for index := range next.Leases {
		if next.Leases[index].Name == name {
			next.Leases[index].Device, next.Leases[index].Inode, next.Leases[index].Created = device, inode, true
			next.Leases[index].HandleType, next.Leases[index].Handle = handleType, handle
			found = true
		}
	}
	if !found {
		return ErrUnavailable
	}
	if err := disk.writeDocument(next); err != nil {
		return err
	}
	disk.document = next
	return nil
}

func (disk *scanEvidenceDisk) release(name string) error {
	if err := disk.check(); err != nil {
		return err
	}
	if _, err := disk.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(fmt.Errorf("%w: scan evidence directory still exists at retirement", ErrUnavailable), err)
	}
	next := disk.document
	next.Leases = make([]scanEvidenceDiskLease, 0, len(disk.document.Leases))
	for _, lease := range disk.document.Leases {
		if lease.Name != name {
			next.Leases = append(next.Leases, lease)
		}
	}
	if len(next.Leases) == len(disk.document.Leases) {
		return ErrUnavailable
	}
	if err := disk.writeDocument(next); err != nil {
		return err
	}
	disk.document = next
	return nil
}

func (disk *scanEvidenceDisk) reclaim(lease scanEvidenceDiskLease) (result error) {
	info, err := disk.root.Lstat(lease.Name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = scanEvidencePrivateInfo(info, true); err != nil {
		return err
	}
	device, inode, err := scanEvidenceFileIdentity(info)
	if err != nil || lease.Created && (device != lease.Device || inode != lease.Inode) {
		return errors.Join(ErrUnavailable, err)
	}
	child, err := disk.root.OpenRoot(lease.Name)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, child.Close()) }()
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return errors.Join(ErrUnavailable, err)
	}
	limit := 0
	if lease.Created {
		limit = 131072
	}
	names, err := scanEvidenceNames(child, limit)
	if err != nil {
		return err
	}
	if len(names) != 0 {
		// Inode numbers can be reused after a crash. Only an exportable handle
		// binds a nonempty historical child across process lifetimes. Mount IDs
		// are intentionally excluded; the parent filesystem identity is pinned.
		handleType, handle, err := scanEvidencePersistentHandle(child)
		if err != nil || lease.HandleType == 0 || handleType != lease.HandleType || !bytes.Equal(handle, lease.Handle) {
			return errors.Join(fmt.Errorf("%w: nonempty scan evidence has no stable restart identity", ErrUnavailable), err)
		}
	}
	for _, name := range names {
		if len(name) != 68 || !strings.HasSuffix(name, ".dir") || strings.Trim(strings.TrimSuffix(name, ".dir"), "0123456789abcdef") != "" {
			return ErrUnavailable
		}
		entry, err := child.Lstat(name)
		if err != nil {
			return err
		}
		if err = scanEvidencePrivateInfo(entry, false); err != nil {
			return err
		}
	}
	// Recovery only removes regular records named by this exact persisted pass.
	// No recursive prefix sweep can claim another owner or an unknown child.
	for _, name := range names {
		if err = child.Remove(name); err != nil {
			return err
		}
	}
	current, err := disk.root.Lstat(lease.Name)
	if err != nil || !os.SameFile(current, info) {
		return errors.Join(ErrUnavailable, err)
	}
	return disk.root.Remove(lease.Name)
}

func (disk *scanEvidenceDisk) close() error {
	if disk == nil {
		return nil
	}
	var result error
	if disk.lock != nil {
		result = errors.Join(result, disk.lock.Close())
		disk.lock = nil
	}
	if disk.root != nil {
		result = errors.Join(result, disk.root.Close())
		disk.root = nil
	}
	return result
}
