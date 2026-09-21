package library

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/moooyo/goby/internal/media"
)

const (
	scanSpoolEntrySize           = 384
	scanSpoolVersionSize         = 80
	scanSpoolIdentitySize        = 144
	scanSpoolHeaderFixed         = 8 + 1 + 4 + 2 + 2 + 2*scanSpoolVersionSize + scanSpoolIdentitySize + 4
	scanSpoolMaxHeader           = scanSpoolHeaderFixed + 2*scanReconciliationMaxPathBytes + sha256.Size
	scanSpoolMaxDirectoryEntries = 262144
)

// Directory is an independently owned writable location outside media roots.
// Each pass uses one private child and at most MaxDirectories regular files.
// Limits account for the complete encoded files, including indexes/checksums.
// Production supplies Parent after validating placement against every media
// and work root. Parent is borrowed only while an independent clone is made;
// creation, reads, and cleanup then remain relative to that retained anchor.
type scanReconciliationSpoolOptions struct {
	Directory          string
	Parent             *os.Root
	MaxDirectories     int
	MaxEntries         int
	MaxBytes           int64
	MaxRoots           int
	MaxFallbackHandles int
	BeforeCreate       func(string) error
	// Created runs after the child is held and identified, before any records
	// exist. The owner can persist a generation witness for restart cleanup.
	Created           func(string, os.FileInfo) error
	OnCleanup         func(scanReconciliationSpoolCleanup)
	directoryIdentity scanSpoolIdentityReader
}

type scanSpoolIdentityReader func(*os.File) (scanSpoolIdentity, bool, error)

// Exported Linux file handles include the inode generation, unlike a stat
// tuple. Unsupported filesystems retain the original descriptor instead.
type scanSpoolIdentity struct {
	kind       byte
	mountID    int32
	handleType int32
	bytes      string
	fallback   uint32
}

type scanReconciliationSpoolStats struct {
	Roots              int
	Directories        int
	Entries            int
	Bytes              int64
	FallbackHandles    int
	FallbackHandlePeak int
	CleanupFinished    bool
	CleanupErr         error
}

type scanReconciliationSpoolCleanup struct {
	Path  string
	Bytes int64
	Files int
	Err   error
}

// The receipt remains reachable after retirement. Failed cleanup retains its
// path and charges, so the owning store can report and reclaim the remainder.
type scanReconciliationSpool struct {
	ctx             context.Context
	options         scanReconciliationSpoolOptions
	parent          *os.Root
	owned           *os.Root
	info            os.FileInfo
	name            string
	path            string
	created         bool
	bytes           int64
	directories     int
	entries         int
	fallbacks       []*os.Root
	mu              sync.Mutex
	done            chan struct{}
	cleanup         scanReconciliationSpoolCleanup
	cleanupFinished bool
	stats           scanReconciliationSpoolStats
}

type scanSpoolVersion struct {
	identity     string
	mode         os.FileMode
	size         int64
	mtimeSeconds int64
	mtimeNanos   uint32
	ctime        int64
}

type scanSpoolDirectory struct {
	rootID   string
	relative string
	root     scanSpoolVersion
	version  scanSpoolVersion
	identity scanSpoolIdentity
	complete bool
	count    int
	ordinal  int
	offset   int64
	file     *os.File
}

type scanSpoolEntry struct {
	name    string
	mode    os.FileMode
	version scanSpoolVersion
}

func newScanReconciliationSpoolEvidence(ctx context.Context, options scanReconciliationSpoolOptions) *scanReconciliationEvidence {
	evidence := &scanReconciliationEvidence{roots: make(map[string]*scanReconciliationRootEvidence)}
	if ctx == nil || options.Directory == "" && options.Parent == nil {
		evidence.err = errScanReconciliationEvidenceUnavailable
		evidence.closed = true
		return evidence
	}
	if options.MaxDirectories == 0 {
		options.MaxDirectories = 131072
	}
	if options.MaxEntries == 0 {
		options.MaxEntries = 1048576
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = 1 << 30
	}
	if options.MaxRoots == 0 {
		options.MaxRoots = scanReconciliationMaxRoots
	}
	if options.MaxFallbackHandles == 0 {
		options.MaxFallbackHandles = 4096
	}
	if options.directoryIdentity == nil {
		options.directoryIdentity = scanSpoolDirectoryIdentity
	}
	if options.MaxDirectories < 1 || options.MaxEntries < 1 || options.MaxBytes < scanSpoolMaxHeader || options.MaxRoots < 1 || options.MaxRoots > scanReconciliationMaxRoots || options.MaxFallbackHandles < 1 || options.MaxFallbackHandles > 4096 {
		evidence.err = errScanReconciliationEvidenceBudget
		evidence.closed = true
		return evidence
	}
	spool := &scanReconciliationSpool{ctx: ctx, options: options, done: make(chan struct{})}
	evidence.spool = spool
	if err := ctx.Err(); err != nil {
		evidence.Disable(err)
		return evidence
	}
	var parent *os.Root
	var err error
	if options.Parent != nil {
		before, statErr := options.Parent.Stat(".")
		if statErr != nil {
			evidence.Disable(statErr)
			return evidence
		}
		parent, err = options.Parent.OpenRoot(".")
		if err == nil {
			var after os.FileInfo
			after, err = parent.Stat(".")
			if err == nil && !os.SameFile(before, after) {
				err = errScanReconciliationEvidenceUnavailable
			}
		}
	} else {
		var parentPath string
		parentPath, err = filepath.Abs(options.Directory)
		if err == nil {
			parent, err = os.OpenRoot(parentPath)
		}
	}
	spool.parent = parent
	if err != nil {
		evidence.Disable(err)
		return evidence
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		evidence.Disable(err)
		return evidence
	}
	spool.name = "scan-evidence-" + hex.EncodeToString(nonce[:])
	spool.path = filepath.Join(parent.Name(), spool.name)
	if options.BeforeCreate != nil {
		if err := options.BeforeCreate(spool.name); err != nil {
			evidence.Disable(err)
			return evidence
		}
	}
	if err := ctx.Err(); err != nil {
		evidence.Disable(err)
		return evidence
	}
	if err = parent.Mkdir(spool.name, 0o700); err != nil {
		evidence.Disable(err)
		return evidence
	}
	spool.created = true
	spool.info, err = parent.Lstat(spool.name)
	if err == nil {
		spool.owned, err = parent.OpenRoot(spool.name)
	}
	if err == nil {
		var opened os.FileInfo
		opened, err = spool.owned.Stat(".")
		if err == nil && !os.SameFile(spool.info, opened) {
			err = errScanReconciliationEvidenceUnavailable
		}
	}
	if err != nil {
		evidence.Disable(err)
		return evidence
	}
	if options.Created != nil {
		if err := options.Created(spool.name, spool.info); err != nil {
			evidence.Disable(err)
			return evidence
		}
	}
	if err := ctx.Err(); err != nil {
		evidence.Disable(err)
		return evidence
	}
	return evidence
}

func (evidence *scanReconciliationEvidence) CleanupDone() <-chan struct{} {
	if evidence != nil && evidence.spool != nil {
		return evidence.spool.done
	}
	done := make(chan struct{})
	close(done)
	return done
}

func (evidence *scanReconciliationEvidence) CleanupStatus() (bool, scanReconciliationSpoolCleanup) {
	if evidence == nil || evidence.spool == nil {
		return true, scanReconciliationSpoolCleanup{}
	}
	spool := evidence.spool
	spool.mu.Lock()
	defer spool.mu.Unlock()
	return spool.cleanupFinished, spool.cleanup
}

func (evidence *scanReconciliationEvidence) SpoolStats() scanReconciliationSpoolStats {
	if evidence == nil || evidence.spool == nil {
		return scanReconciliationSpoolStats{}
	}
	spool := evidence.spool
	spool.mu.Lock()
	defer spool.mu.Unlock()
	return spool.stats
}

func (spool *scanReconciliationSpool) close(result error) error {
	for _, held := range spool.fallbacks {
		result = errors.Join(result, held.Close())
	}
	if spool.parent != nil {
		if spool.created {
			current, err := spool.parent.Lstat(spool.name)
			if err != nil {
				result = errors.Join(result, err)
			} else {
				if spool.info == nil || !os.SameFile(spool.info, current) || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 {
					result = errors.Join(result, errors.New("scan evidence cleanup path changed identity"))
				} else {
					cleanupErr := spool.removeFiles()
					if cleanupErr == nil {
						// Remove only an empty directory. A replacement after the
						// identity check must never acquire recursive authority.
						current, cleanupErr = spool.parent.Lstat(spool.name)
						if cleanupErr == nil && !os.SameFile(spool.info, current) {
							cleanupErr = errScanReconciliationEvidenceUnavailable
						}
						if cleanupErr == nil {
							cleanupErr = removeScanSpoolDirectory(spool.parent, spool.name)
						}
					}
					result = errors.Join(result, cleanupErr)
				}
			}
		}
		if spool.owned != nil {
			result = errors.Join(result, spool.owned.Close())
			spool.owned = nil
		}
		result = errors.Join(result, spool.parent.Close())
		spool.parent = nil
	}
	receipt := scanReconciliationSpoolCleanup{Path: spool.path, Bytes: spool.bytes, Files: spool.directories, Err: result}
	spool.mu.Lock()
	spool.cleanup, spool.cleanupFinished = receipt, true
	spool.stats.CleanupFinished, spool.stats.CleanupErr = true, result
	if result == nil {
		spool.stats.Roots, spool.stats.FallbackHandles = 0, 0
		spool.stats.Directories, spool.stats.Entries, spool.stats.Bytes = 0, 0, 0
	}
	close(spool.done)
	spool.mu.Unlock()
	if spool.options.OnCleanup != nil {
		spool.options.OnCleanup(receipt)
	}
	return result
}

// Cleanup never descends into a directory found at a record name. The owned
// child can contain only leaf record files, and enumeration has the same finite
// file bound as construction even if its contents have become corrupt.
func (spool *scanReconciliationSpool) removeFiles() (result error) {
	if spool.owned == nil {
		return errScanReconciliationEvidenceUnavailable
	}
	directory, err := openScanFile(spool.owned, ".")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, directory.Close()) }()
	removed := 0
	for {
		files, readErr := directory.ReadDir(scanReconciliationReadBatch)
		for _, file := range files {
			if removed >= spool.options.MaxDirectories || file.IsDir() {
				return errScanReconciliationEvidenceUnavailable
			}
			if err := removeScanSpoolLeaf(spool.owned, file.Name()); err != nil {
				return err
			}
			removed++
		}
		if readErr != nil {
			if readErr != io.EOF {
				return readErr
			}
			return nil
		}
		if len(files) == 0 {
			return errScanReconciliationEvidenceUnavailable
		}
	}
}

func (evidence *scanReconciliationEvidence) attachSpoolRoot(rootID string, borrowed *os.Root) error {
	spool := evidence.spool
	if err := spool.ctx.Err(); err != nil {
		return evidence.Disable(err)
	}
	if len(rootID) > scanReconciliationMaxPathBytes || len(evidence.roots) >= spool.options.MaxRoots {
		return evidence.Disable(errScanReconciliationEvidenceBudget)
	}
	// Resolve symlink spelling before checking placement; all subsequent file
	// access uses the independently retained roots instead of absolute paths.
	mediaAbsolute, err := filepath.Abs(borrowed.Name())
	if err != nil {
		return evidence.Disable(err)
	}
	mediaPath, err := filepath.EvalSymlinks(mediaAbsolute)
	spoolPath, spoolErr := filepath.EvalSymlinks(spool.path)
	if err != nil || spoolErr != nil || scanSpoolPathsOverlap(mediaPath, spoolPath) {
		return evidence.unavailable("scan evidence spool overlaps a media root or has unavailable placement")
	}
	before, err := borrowed.Stat(".")
	if err != nil || !scanReconciliationDirectoryInfo(before) {
		return evidence.unavailable("root information is unavailable")
	}
	anchor, err := borrowed.OpenRoot(".")
	if err != nil {
		return evidence.Disable(err)
	}
	after, err := anchor.Stat(".")
	if err != nil || !scanReconciliationSameInfo(before, after) {
		_ = anchor.Close()
		return evidence.unavailable("root changed while being retained")
	}
	evidence.roots[strings.Clone(rootID)] = &scanReconciliationRootEvidence{anchor: anchor, info: before}
	spool.mu.Lock()
	spool.stats.Roots++
	spool.mu.Unlock()
	return nil
}

func scanSpoolPathsOverlap(a, b string) bool {
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return true
		}
	}
	return false
}

func scanSpoolFileName(rootID, relative string) string {
	hash := sha256.New()
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(rootID)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write([]byte(rootID))
	_, _ = hash.Write([]byte(relative))
	return hex.EncodeToString(hash.Sum(nil)) + ".dir"
}

func scanSpoolRawName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 && !strings.ContainsAny(name, "/\x00")
}

func scanSpoolInfo(info os.FileInfo) (scanSpoolVersion, bool) {
	if !scanReconciliationInfo(info) {
		return scanSpoolVersion{}, false
	}
	identity := fileIdentity(info)
	if len(identity) > 41 {
		return scanSpoolVersion{}, false
	}
	return scanSpoolVersion{identity: identity, mode: info.Mode(), size: info.Size(), mtimeSeconds: info.ModTime().Unix(), mtimeNanos: uint32(info.ModTime().Nanosecond()), ctime: media.FileChangeTime(info)}, true
}

func (version scanSpoolVersion) matches(info os.FileInfo) bool {
	other, valid := scanSpoolInfo(info)
	return valid && version == other
}

func (version scanSpoolVersion) directory() bool {
	return version.mode.IsDir() && version.mode&os.ModeSymlink == 0
}

func scanSpoolEncodeVersion(data []byte, version scanSpoolVersion) {
	data[0] = byte(len(version.identity))
	copy(data[1:42], version.identity)
	binary.LittleEndian.PutUint32(data[42:46], uint32(version.mode))
	binary.LittleEndian.PutUint64(data[46:54], uint64(version.size))
	binary.LittleEndian.PutUint64(data[54:62], uint64(version.mtimeSeconds))
	binary.LittleEndian.PutUint32(data[62:66], version.mtimeNanos)
	binary.LittleEndian.PutUint64(data[66:74], uint64(version.ctime))
}

func scanSpoolDecodeVersion(data []byte) (scanSpoolVersion, error) {
	length := int(data[0])
	if length < 1 || length > 41 {
		return scanSpoolVersion{}, errScanReconciliationEvidenceUnavailable
	}
	version := scanSpoolVersion{identity: string(data[1 : 1+length]), mode: os.FileMode(binary.LittleEndian.Uint32(data[42:46])), size: int64(binary.LittleEndian.Uint64(data[46:54])), mtimeSeconds: int64(binary.LittleEndian.Uint64(data[54:62])), mtimeNanos: binary.LittleEndian.Uint32(data[62:66]), ctime: int64(binary.LittleEndian.Uint64(data[66:74]))}
	if version.ctime <= 0 || version.mtimeNanos >= 1000000000 {
		return scanSpoolVersion{}, errScanReconciliationEvidenceUnavailable
	}
	return version, nil
}

func scanSpoolEncodeHeader(record *scanSpoolDirectory) []byte {
	data := make([]byte, scanSpoolHeaderFixed+len(record.rootID)+len(record.relative)+sha256.Size)
	copy(data, "GOBYSP01")
	if record.complete {
		data[8] = 1
	}
	binary.LittleEndian.PutUint32(data[9:13], uint32(record.count))
	binary.LittleEndian.PutUint16(data[13:15], uint16(len(record.rootID)))
	binary.LittleEndian.PutUint16(data[15:17], uint16(len(record.relative)))
	scanSpoolEncodeVersion(data[17:17+scanSpoolVersionSize], record.root)
	scanSpoolEncodeVersion(data[17+scanSpoolVersionSize:17+2*scanSpoolVersionSize], record.version)
	identity := data[17+2*scanSpoolVersionSize : 17+2*scanSpoolVersionSize+scanSpoolIdentitySize]
	identity[0], identity[1] = record.identity.kind, byte(len(record.identity.bytes))
	binary.LittleEndian.PutUint32(identity[2:6], uint32(record.identity.mountID))
	binary.LittleEndian.PutUint32(identity[6:10], uint32(record.identity.handleType))
	binary.LittleEndian.PutUint32(identity[10:14], record.identity.fallback)
	copy(identity[14:142], record.identity.bytes)
	binary.LittleEndian.PutUint32(data[scanSpoolHeaderFixed-4:scanSpoolHeaderFixed], uint32(record.ordinal))
	copy(data[scanSpoolHeaderFixed:], record.rootID)
	copy(data[scanSpoolHeaderFixed+len(record.rootID):], record.relative)
	digest := sha256.Sum256(data[:len(data)-sha256.Size])
	copy(data[len(data)-sha256.Size:], digest[:])
	return data
}

func scanSpoolWriteHeader(record *scanSpoolDirectory) error {
	data := scanSpoolEncodeHeader(record)
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(data)))
	if _, err := record.file.WriteAt(length[:], 0); err != nil {
		return err
	}
	_, err := record.file.WriteAt(data, 4)
	return err
}

func (spool *scanReconciliationSpool) open(rootID, relative string, flags int) (*scanSpoolDirectory, error) {
	file, err := openScanSpoolFile(spool.owned, scanSpoolFileName(rootID, relative), flags)
	if err != nil {
		return nil, err
	}
	record, err := scanSpoolReadHeader(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if record.rootID != rootID || record.relative != relative {
		_ = file.Close()
		return nil, errScanReconciliationEvidenceUnavailable
	}
	return record, nil
}

func scanSpoolReadHeader(file *os.File) (*scanSpoolDirectory, error) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	var length [4]byte
	if _, err := file.ReadAt(length[:], 0); err != nil {
		return nil, err
	}
	size := int(binary.LittleEndian.Uint32(length[:]))
	if size < scanSpoolHeaderFixed+sha256.Size || size > scanSpoolMaxHeader {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	data := make([]byte, size)
	if _, err := file.ReadAt(data, 4); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data[:size-sha256.Size])
	if string(data[:8]) != "GOBYSP01" || data[8] > 1 || string(digest[:]) != string(data[size-sha256.Size:]) {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	rootLength, pathLength := int(binary.LittleEndian.Uint16(data[13:15])), int(binary.LittleEndian.Uint16(data[15:17]))
	count := int(binary.LittleEndian.Uint32(data[9:13]))
	if rootLength < 1 || rootLength > scanReconciliationMaxPathBytes || pathLength < 1 || pathLength > scanReconciliationMaxPathBytes || count > scanSpoolMaxDirectoryEntries || size != scanSpoolHeaderFixed+rootLength+pathLength+sha256.Size {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	root, err := scanSpoolDecodeVersion(data[17 : 17+scanSpoolVersionSize])
	if err != nil {
		return nil, err
	}
	version, err := scanSpoolDecodeVersion(data[17+scanSpoolVersionSize : 17+2*scanSpoolVersionSize])
	if err != nil || !version.directory() || !root.directory() {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	identityData := data[17+2*scanSpoolVersionSize : 17+2*scanSpoolVersionSize+scanSpoolIdentitySize]
	identity := scanSpoolIdentity{kind: identityData[0], mountID: int32(binary.LittleEndian.Uint32(identityData[2:6])), handleType: int32(binary.LittleEndian.Uint32(identityData[6:10])), fallback: binary.LittleEndian.Uint32(identityData[10:14])}
	if identityData[1] > 128 {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	identity.bytes = string(identityData[14 : 14+int(identityData[1])])
	if identity.kind != 1 && identity.kind != 2 && identity.kind != 3 || identity.kind == 1 && (identity.mountID <= 0 || identity.handleType <= 0 || identity.bytes == "") || identity.kind == 2 && identity.fallback == 0 {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	if info.Size() != int64(4+size)+int64(count)*scanSpoolEntrySize {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	record := &scanSpoolDirectory{rootID: string(data[scanSpoolHeaderFixed : scanSpoolHeaderFixed+rootLength]), relative: string(data[scanSpoolHeaderFixed+rootLength : scanSpoolHeaderFixed+rootLength+pathLength]), root: root, version: version, identity: identity, complete: data[8] == 1, count: count, ordinal: int(binary.LittleEndian.Uint32(data[scanSpoolHeaderFixed-4 : scanSpoolHeaderFixed])), offset: int64(4 + size), file: file}
	if identity.kind == 3 && record.relative != "." {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	if _, valid := scanReconciliationRelative(record.relative, true); !valid {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	return record, nil
}

func scanSpoolEncodeEntry(entry scanSpoolEntry) [scanSpoolEntrySize]byte {
	var data [scanSpoolEntrySize]byte
	binary.LittleEndian.PutUint16(data[:2], uint16(len(entry.name)))
	copy(data[2:257], entry.name)
	binary.LittleEndian.PutUint32(data[257:261], uint32(entry.mode))
	scanSpoolEncodeVersion(data[261:341], entry.version)
	digest := sha256.Sum256(data[:scanSpoolEntrySize-sha256.Size])
	copy(data[scanSpoolEntrySize-sha256.Size:], digest[:])
	return data
}

func (record *scanSpoolDirectory) entry(index int) (scanSpoolEntry, error) {
	if index < 0 || index >= record.count {
		return scanSpoolEntry{}, errScanReconciliationEvidenceUnavailable
	}
	var data [scanSpoolEntrySize]byte
	if _, err := record.file.ReadAt(data[:], record.offset+int64(index)*scanSpoolEntrySize); err != nil {
		return scanSpoolEntry{}, err
	}
	digest := sha256.Sum256(data[:scanSpoolEntrySize-sha256.Size])
	length := int(binary.LittleEndian.Uint16(data[:2]))
	if length < 1 || length > 255 || string(digest[:]) != string(data[scanSpoolEntrySize-sha256.Size:]) {
		return scanSpoolEntry{}, errScanReconciliationEvidenceUnavailable
	}
	version, err := scanSpoolDecodeVersion(data[261:341])
	if err != nil {
		return scanSpoolEntry{}, err
	}
	entry := scanSpoolEntry{name: string(data[2 : 2+length]), mode: os.FileMode(binary.LittleEndian.Uint32(data[257:261])), version: version}
	if !scanSpoolRawName(entry.name) || entry.mode != version.mode.Type() {
		return scanSpoolEntry{}, errScanReconciliationEvidenceUnavailable
	}
	return entry, nil
}

func (record *scanSpoolDirectory) lookup(name string) (scanSpoolEntry, int, bool, error) {
	low, high := 0, record.count
	for low < high {
		middle := low + (high-low)/2
		entry, err := record.entry(middle)
		if err != nil {
			return scanSpoolEntry{}, 0, false, err
		}
		if entry.name < name {
			low = middle + 1
		} else {
			high = middle
		}
	}
	if low == record.count {
		return scanSpoolEntry{}, low, false, nil
	}
	entry, err := record.entry(low)
	return entry, low, err == nil && entry.name == name, err
}

func (evidence *scanReconciliationEvidence) recordSpoolDirectory(rootID, relative string, before os.FileInfo, raw []os.DirEntry) (result error) {
	if err := evidence.Err(); err != nil {
		return err
	}
	if !evidence.observation.retain() {
		return errScanReconciliationEvidenceUnavailable
	}
	defer evidence.observation.release()
	spool := evidence.spool
	if err := spool.ctx.Err(); err != nil {
		return evidence.Disable(err)
	}
	relative, valid := scanReconciliationRelative(relative, true)
	root := evidence.roots[rootID]
	version, versionValid := scanSpoolInfo(before)
	if !valid || root == nil || !versionValid || !version.directory() {
		return evidence.unavailable("directory observation is invalid")
	}
	rootVersion, _ := scanSpoolInfo(root.info)
	record := &scanSpoolDirectory{rootID: rootID, relative: relative, root: rootVersion, version: version, count: len(raw), ordinal: spool.directories + 1}
	header := scanSpoolEncodeHeader(record)
	charge := int64(4+len(header)) + int64(len(raw))*scanSpoolEntrySize
	if len(raw) > scanSpoolMaxDirectoryEntries || spool.directories >= spool.options.MaxDirectories || len(raw) > spool.options.MaxEntries-spool.entries || charge > spool.options.MaxBytes-spool.bytes {
		return evidence.Disable(errScanReconciliationEvidenceBudget)
	}
	for _, entry := range raw {
		if entry == nil || !scanSpoolRawName(entry.Name()) {
			return evidence.unavailable("directory membership contains an invalid raw name")
		}
	}
	ordered := append([]os.DirEntry(nil), raw...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name() < ordered[j].Name() })
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1].Name() == ordered[index].Name() {
			return evidence.unavailable("directory membership contains a duplicate raw name")
		}
	}
	held, err := evidence.openSpoolDirectory(spool.ctx, rootID, relative, false)
	if err != nil {
		return evidence.Disable(err)
	}
	defer held.Close()
	observed, err := held.Stat(".")
	if err != nil || !version.matches(observed) {
		return evidence.unavailable("walked directory changed before membership capture")
	}
	record.identity, err = evidence.captureSpoolIdentity(record, held)
	if err != nil {
		return evidence.Disable(err)
	}
	file, err := openScanSpoolFile(spool.owned, scanSpoolFileName(rootID, relative), os.O_CREATE|os.O_EXCL|os.O_RDWR)
	if err != nil {
		return evidence.Disable(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			result = errors.Join(result, evidence.Disable(err))
		}
	}()
	record.file, record.offset = file, int64(4+len(header))
	// Charge ownership immediately: a short write still owns this file and all
	// of its reserved space until cleanup reports the actual disposition.
	spool.bytes += charge
	spool.directories++
	spool.entries += len(raw)
	spool.mu.Lock()
	spool.stats.Bytes, spool.stats.Directories, spool.stats.Entries = spool.bytes, spool.directories, spool.entries
	spool.mu.Unlock()
	evidence.incompleteDirectories++
	if err := scanSpoolWriteHeader(record); err != nil {
		return evidence.Disable(err)
	}
	for index, entry := range ordered {
		if err := spool.ctx.Err(); err != nil {
			return evidence.Disable(err)
		}
		info, err := held.Lstat(entry.Name())
		version, valid := scanSpoolInfo(info)
		if err != nil || !valid || version.mode.Type() != entry.Type().Type() {
			return evidence.unavailable("directory entry changed during membership capture")
		}
		data := scanSpoolEncodeEntry(scanSpoolEntry{name: entry.Name(), mode: entry.Type().Type(), version: version})
		if _, err := file.WriteAt(data[:], record.offset+int64(index)*scanSpoolEntrySize); err != nil {
			return evidence.Disable(err)
		}
	}
	if err := evidence.verifySpoolDirectory(spool.ctx, record, held); err != nil {
		return evidence.Disable(err)
	}
	return nil
}

func (evidence *scanReconciliationEvidence) completeSpoolDirectory(rootID, relative string) (result error) {
	if err := evidence.Err(); err != nil {
		return err
	}
	if !evidence.observation.retain() {
		return errScanReconciliationEvidenceUnavailable
	}
	defer evidence.observation.release()
	spool := evidence.spool
	if err := spool.ctx.Err(); err != nil {
		return evidence.Disable(err)
	}
	relative, valid := scanReconciliationRelative(relative, true)
	if !valid || evidence.roots[rootID] == nil {
		return evidence.unavailable("completed directory was not observed")
	}
	record, err := spool.open(rootID, relative, os.O_RDWR)
	if err != nil {
		return evidence.Disable(err)
	}
	defer func() {
		if err := record.file.Close(); err != nil {
			result = errors.Join(result, evidence.Disable(err))
		}
	}()
	if err := evidence.verifySpoolDirectory(spool.ctx, record, nil); err != nil {
		return evidence.Disable(err)
	}
	if !record.complete {
		record.complete = true
		if err := scanSpoolWriteHeader(record); err != nil {
			return evidence.Disable(err)
		}
		evidence.incompleteDirectories--
		if relative == "." {
			evidence.completeRoots++
		}
	}
	return nil
}

// Each hop is rooted in the retained registered root, checked against both
// immutable parent membership and its recorded directory version. Only current
// and next are open at once. The returned final handle owns the observation.
func (evidence *scanReconciliationEvidence) openSpoolDirectory(ctx context.Context, rootID, relative string, requireFinal bool) (_ *os.Root, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := evidence.roots[rootID]
	if root == nil {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	current, err := root.anchor.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			_ = current.Close()
		}
	}()
	info, err := current.Stat(".")
	if err != nil || !scanReconciliationSameInfo(root.info, info) {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	parent, remaining := ".", relative
	if relative == "." {
		remaining = ""
	}
	for remaining != "" {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		component, rest, _ := strings.Cut(remaining, "/")
		record, err := evidence.spool.open(rootID, parent, os.O_RDONLY)
		if err != nil {
			return nil, err
		}
		previous, _, exists, lookupErr := record.lookup(component)
		_ = record.file.Close()
		parentBefore, parentErr := current.Stat(".")
		if lookupErr != nil || !exists || !previous.version.directory() || parentErr != nil || !record.root.matches(root.info) || !record.version.matches(info) || !record.version.matches(parentBefore) {
			return nil, errScanReconciliationEvidenceUnavailable
		}
		if err := evidence.checkSpoolIdentity(record, current); err != nil {
			return nil, err
		}
		before, err := current.Lstat(component)
		if err != nil || !previous.version.matches(before) {
			return nil, errScanReconciliationEvidenceUnavailable
		}
		next, err := current.OpenRoot(component)
		if err != nil {
			return nil, err
		}
		nextInfo, nextErr := next.Stat(".")
		namedAfter, namedErr := current.Lstat(component)
		parentAfter, parentErr := current.Stat(".")
		if nextErr != nil || namedErr != nil || parentErr != nil || !previous.version.matches(nextInfo) || !previous.version.matches(namedAfter) || !record.version.matches(parentAfter) {
			_ = next.Close()
			return nil, errScanReconciliationEvidenceUnavailable
		}
		_ = current.Close()
		current, info = next, nextInfo
		parent = relative[:len(relative)-len(rest)]
		if rest != "" {
			parent = parent[:len(parent)-1]
		}
		remaining = rest
	}
	record, err := evidence.spool.open(rootID, relative, os.O_RDONLY)
	if err == nil {
		_ = record.file.Close()
		if !record.root.matches(root.info) || !record.version.matches(info) {
			return nil, errScanReconciliationEvidenceUnavailable
		}
		if err := evidence.checkSpoolIdentity(record, current); err != nil {
			return nil, err
		}
	} else if requireFinal || !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return current, nil
}

func (evidence *scanReconciliationEvidence) verifySpoolDirectory(ctx context.Context, record *scanSpoolDirectory, held *os.Root) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root := evidence.roots[record.rootID]
	if root == nil || !record.root.matches(root.info) {
		return errScanReconciliationEvidenceUnavailable
	}
	if held == nil {
		var err error
		held, err = evidence.openSpoolDirectory(ctx, record.rootID, record.relative, true)
		if err != nil {
			return err
		}
		defer held.Close()
	}
	if err := evidence.checkSpoolIdentity(record, held); err != nil {
		return err
	}
	directory, err := openScanFile(held, ".")
	if err != nil {
		return err
	}
	defer directory.Close()
	before, err := directory.Stat()
	if err != nil || !record.version.matches(before) {
		return errScanReconciliationEvidenceUnavailable
	}
	// One bit per bounded directory member detects duplicate enumeration without
	// mutating immutable records or retaining a second whole-pass name map.
	seen := make([]byte, (record.count+7)/8)
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, readErr := directory.ReadDir(min(scanReconciliationReadBatch, record.count-count+1))
		for _, entry := range entries {
			previous, index, exists, err := record.lookup(entry.Name())
			if err != nil || !exists || seen[index/8]&(1<<uint(index%8)) != 0 || entry.Type().Type() != previous.mode {
				return errScanReconciliationEvidenceUnavailable
			}
			info, err := held.Lstat(entry.Name())
			if err != nil || !previous.version.matches(info) {
				return errScanReconciliationEvidenceUnavailable
			}
			seen[index/8] |= 1 << uint(index%8)
			count++
		}
		if readErr != nil {
			if readErr != io.EOF {
				return readErr
			}
			break
		}
		if len(entries) == 0 {
			return errScanReconciliationEvidenceUnavailable
		}
	}
	last := ""
	for index := 0; index < record.count; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		previous, err := record.entry(index)
		if err != nil || index > 0 && previous.name <= last {
			return errScanReconciliationEvidenceUnavailable
		}
		last = previous.name
		info, err := held.Lstat(previous.name)
		if err != nil || !previous.version.matches(info) {
			return errScanReconciliationEvidenceUnavailable
		}
	}
	after, err := directory.Stat()
	heldAfter, heldErr := held.Stat(".")
	if count != record.count || err != nil || heldErr != nil || !record.version.matches(after) || !record.version.matches(heldAfter) {
		return errScanReconciliationEvidenceUnavailable
	}
	named, err := evidence.openSpoolDirectory(ctx, record.rootID, record.relative, true)
	if err != nil {
		return err
	}
	_ = named.Close()
	return ctx.Err()
}

func (evidence *scanReconciliationEvidence) revalidateSpool(ctx context.Context) error {
	if !evidence.observation.retain() {
		return errScanReconciliationEvidenceUnavailable
	}
	defer evidence.observation.release()
	spool := evidence.spool
	directory, err := openScanFile(spool.owned, ".")
	if err != nil {
		return evidence.Disable(err)
	}
	defer directory.Close()
	count, entries, bytes := 0, 0, int64(0)
	seen := make([]byte, (spool.directories+7)/8)
	for {
		if err := ctx.Err(); err != nil {
			return evidence.Disable(err)
		}
		files, readErr := directory.ReadDir(scanReconciliationReadBatch)
		for _, file := range files {
			if !file.Type().IsRegular() || len(file.Name()) != 68 {
				return evidence.unavailable("scan spool contains an unexpected file")
			}
			recordFile, err := openScanSpoolFile(spool.owned, file.Name(), os.O_RDONLY)
			if err != nil {
				return evidence.Disable(err)
			}
			record, err := scanSpoolReadHeader(recordFile)
			if err == nil && (!record.complete || scanSpoolFileName(record.rootID, record.relative) != file.Name()) {
				err = errScanReconciliationEvidenceUnavailable
			}
			if err == nil {
				ordinal := record.ordinal - 1
				if ordinal < 0 || ordinal >= spool.directories || seen[ordinal/8]&(1<<uint(ordinal%8)) != 0 {
					err = errScanReconciliationEvidenceUnavailable
				} else {
					seen[ordinal/8] |= 1 << uint(ordinal%8)
				}
			}
			if err == nil {
				err = evidence.verifySpoolDirectory(ctx, record, nil)
			}
			_ = recordFile.Close()
			if err != nil {
				return evidence.Disable(err)
			}
			count++
			entries += record.count
			bytes += record.offset + int64(record.count)*scanSpoolEntrySize
			if count > spool.directories || entries > spool.entries || bytes > spool.bytes {
				return evidence.unavailable("scan spool exceeds its recorded ownership")
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return evidence.Disable(readErr)
			}
			break
		}
		if len(files) == 0 {
			return evidence.unavailable("scan spool enumeration made no progress")
		}
	}
	if count != spool.directories || entries != spool.entries || bytes != spool.bytes {
		return evidence.unavailable("scan spool membership is incomplete")
	}
	return nil
}

func (evidence *scanReconciliationEvidence) spoolPathAbsent(ctx context.Context, rootID, relative string) (bool, error) {
	if !evidence.observation.retain() {
		return false, errScanReconciliationEvidenceUnavailable
	}
	defer evidence.observation.release()
	relative, valid := scanReconciliationRelative(relative, false)
	if !valid || evidence.roots[rootID] == nil {
		return false, evidence.unavailable("absence candidate is not a physical root-relative path")
	}
	parent, remaining := ".", relative
	for remaining != "" {
		if err := ctx.Err(); err != nil {
			return false, evidence.Disable(err)
		}
		component, rest, _ := strings.Cut(remaining, "/")
		record, err := evidence.spool.open(rootID, parent, os.O_RDONLY)
		if err != nil {
			return false, evidence.Disable(err)
		}
		previous, _, exists, lookupErr := record.lookup(component)
		_ = record.file.Close()
		if lookupErr != nil || !record.complete {
			return false, evidence.unavailable("absence parent was not completely observed")
		}
		held, err := evidence.openSpoolDirectory(ctx, rootID, parent, true)
		if err != nil {
			return false, evidence.Disable(err)
		}
		before, beforeErr := held.Stat(".")
		entry, entryErr := held.Lstat(component)
		after, afterErr := held.Stat(".")
		// Keep the candidate's actual parent open through the second named-chain
		// validation, instead of observing an unrelated cached descriptor.
		named, namedErr := evidence.openSpoolDirectory(ctx, rootID, parent, true)
		if named != nil {
			_ = named.Close()
		}
		_ = held.Close()
		if beforeErr != nil || afterErr != nil || namedErr != nil || !record.version.matches(before) || !record.version.matches(after) {
			return false, evidence.unavailable("absence parent changed during lookup")
		}
		if err := ctx.Err(); err != nil {
			return false, evidence.Disable(err)
		}
		if !exists {
			if errors.Is(entryErr, os.ErrNotExist) {
				return true, nil
			}
			return false, evidence.unavailable("unlisted component does not have fresh absence evidence")
		}
		if entryErr != nil || !previous.version.matches(entry) {
			return false, evidence.unavailable("previously listed component changed or disappeared")
		}
		if rest == "" {
			return false, nil
		}
		if !previous.version.directory() {
			return false, evidence.unavailable("absence path contains a symlink or non-directory")
		}
		parent = relative[:len(relative)-len(rest)-1]
		remaining = rest
	}
	return false, evidence.Disable(fmt.Errorf("%w: absence candidate has no component", errScanReconciliationEvidenceUnavailable))
}

func (evidence *scanReconciliationEvidence) captureSpoolIdentity(record *scanSpoolDirectory, held *os.Root) (scanSpoolIdentity, error) {
	if record.relative == "." {
		return scanSpoolIdentity{kind: 3}, nil
	}
	file, err := openScanFile(held, ".")
	if err != nil {
		return scanSpoolIdentity{}, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !record.version.matches(before) {
		return scanSpoolIdentity{}, errScanReconciliationEvidenceUnavailable
	}
	identity, supported, err := evidence.spool.options.directoryIdentity(file)
	if err != nil {
		return scanSpoolIdentity{}, err
	}
	after, err := file.Stat()
	if err != nil || !record.version.matches(after) {
		return scanSpoolIdentity{}, errScanReconciliationEvidenceUnavailable
	}
	if supported {
		if identity.kind != 1 || identity.mountID <= 0 || identity.handleType <= 0 || len(identity.bytes) == 0 || len(identity.bytes) > 128 {
			return scanSpoolIdentity{}, errScanReconciliationEvidenceUnavailable
		}
		return identity, nil
	}
	spool := evidence.spool
	if len(spool.fallbacks) >= spool.options.MaxFallbackHandles {
		return scanSpoolIdentity{}, errScanReconciliationEvidenceBudget
	}
	retained, err := held.OpenRoot(".")
	if err != nil {
		return scanSpoolIdentity{}, err
	}
	info, err := retained.Stat(".")
	if err != nil || !record.version.matches(info) {
		_ = retained.Close()
		return scanSpoolIdentity{}, errScanReconciliationEvidenceUnavailable
	}
	spool.fallbacks = append(spool.fallbacks, retained)
	spool.mu.Lock()
	spool.stats.FallbackHandles = len(spool.fallbacks)
	spool.stats.FallbackHandlePeak = len(spool.fallbacks)
	spool.mu.Unlock()
	return scanSpoolIdentity{kind: 2, fallback: uint32(len(spool.fallbacks))}, nil
}

func (evidence *scanReconciliationEvidence) checkSpoolIdentity(record *scanSpoolDirectory, held *os.Root) error {
	switch record.identity.kind {
	case 3:
		if record.relative != "." {
			return errScanReconciliationEvidenceUnavailable
		}
		return nil
	case 2:
		slot := int(record.identity.fallback) - 1
		if slot < 0 || slot >= len(evidence.spool.fallbacks) {
			return errScanReconciliationEvidenceUnavailable
		}
		original, err := evidence.spool.fallbacks[slot].Stat(".")
		current, currentErr := held.Stat(".")
		if err != nil || currentErr != nil || !record.version.matches(original) || !record.version.matches(current) || !os.SameFile(original, current) {
			return errScanReconciliationEvidenceUnavailable
		}
		return nil
	case 1:
		file, err := openScanFile(held, ".")
		if err != nil {
			return err
		}
		defer file.Close()
		before, err := file.Stat()
		if err != nil || !record.version.matches(before) {
			return errScanReconciliationEvidenceUnavailable
		}
		current, supported, err := evidence.spool.options.directoryIdentity(file)
		after, afterErr := file.Stat()
		if err != nil || !supported || current != record.identity || afterErr != nil || !record.version.matches(after) {
			return errScanReconciliationEvidenceUnavailable
		}
		return nil
	default:
		return errScanReconciliationEvidenceUnavailable
	}
}
