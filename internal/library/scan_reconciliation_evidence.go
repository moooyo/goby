package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/moooyo/goby/internal/media"
)

var (
	errScanReconciliationEvidenceUnavailable = errors.New("scan reconciliation evidence changed or is unavailable")
	errScanReconciliationEvidenceBudget      = errors.New("scan reconciliation evidence exceeds its budget")
)

const (
	scanReconciliationDirectoryBytes = int64(512)
	scanReconciliationEntryBytes     = int64(512)
	scanReconciliationSeenIDBytes    = int64(96)
	scanReconciliationScratchBytes   = int64(128 << 10)
	scanReconciliationReadBatch      = 64
	scanReconciliationMaxPathBytes   = 4096
)

type scanReconciliationEvidenceLimits struct {
	directories int
	entries     int
	seenIDs     int
	bytes       int64
}

type scanReconciliationEntryEvidence struct {
	info     os.FileInfo
	mode     os.FileMode
	lastRead uint64
}

type scanReconciliationDirectoryEvidence struct {
	relative string
	held     *os.Root
	info     os.FileInfo
	entries  map[string]scanReconciliationEntryEvidence
	complete bool
	readPass uint64
}

type scanReconciliationRootEvidence struct {
	anchor      *os.Root
	info        os.FileInfo
	directories map[string]*scanReconciliationDirectoryEvidence
}

// scanReconciliationEvidence belongs to one sequential library scan. Its
// directory versions and membership are evidence, never storage approval or
// deletion authority. Repeated filesystem observations are not an atomic lock
// shared with the catalog transaction.
type scanReconciliationEvidence struct {
	limits                scanReconciliationEvidenceLimits
	directories           int
	entries               int
	seenIDs               int
	bytes                 int64
	incompleteDirectories int
	completeRoots         int
	roots                 map[string]*scanReconciliationRootEvidence
	seen                  map[string]struct{}
	err                   error
	closed                bool
}

func newScanReconciliationEvidence() *scanReconciliationEvidence {
	return newScanReconciliationEvidenceWithLimits(scanReconciliationEvidenceLimits{
		directories: 4096,
		entries:     262144,
		seenIDs:     131072,
		bytes:       64 << 20,
	})
}

// The byte charges conservatively cover retained maps, file information and
// strings, plus fixed bounded directory-read and component-traversal scratch.
// Every retained root anchor also consumes the shared directory-handle budget.
func newScanReconciliationEvidenceWithLimits(limits scanReconciliationEvidenceLimits) *scanReconciliationEvidence {
	evidence := &scanReconciliationEvidence{limits: limits}
	if limits.directories < 0 || limits.entries < 0 || limits.seenIDs < 0 || limits.bytes < scanReconciliationScratchBytes {
		evidence.err = errScanReconciliationEvidenceBudget
		evidence.closed = true
		return evidence
	}
	evidence.bytes = scanReconciliationScratchBytes
	evidence.roots = make(map[string]*scanReconciliationRootEvidence)
	evidence.seen = make(map[string]struct{})
	return evidence
}

// AttachRoot borrows the argument only while making an independent clone. The
// caller continues to own the argument and can close it after its walk.
func (evidence *scanReconciliationEvidence) AttachRoot(rootID string, borrowed *os.Root) error {
	if err := evidence.Err(); err != nil {
		return err
	}
	if rootID == "" || strings.ContainsRune(rootID, '\x00') || borrowed == nil || evidence.roots[rootID] != nil {
		return evidence.unavailable("root attachment is invalid or repeated")
	}
	if err := evidence.reserve(1, 0, 0, scanReconciliationDirectoryBytes+int64(len(rootID))); err != nil {
		return err
	}
	before, err := borrowed.Stat(".")
	if err != nil || !scanReconciliationDirectoryInfo(before) {
		return evidence.unavailable("root information is unavailable")
	}
	anchor, err := borrowed.OpenRoot(".")
	if err != nil {
		return evidence.unavailable("root cannot be retained")
	}
	after, err := anchor.Stat(".")
	if err != nil || !scanReconciliationSameInfo(before, after) {
		_ = anchor.Close()
		return evidence.unavailable("root changed while being retained")
	}
	evidence.roots[strings.Clone(rootID)] = &scanReconciliationRootEvidence{
		anchor: anchor, info: before, directories: make(map[string]*scanReconciliationDirectoryEvidence),
	}
	return nil
}

// RecordDirectory receives the entire pre-classification ReadDir result and
// the walk's held directory information from before that read. Raw membership
// includes ignored names, auxiliary resources, symlinks and unsupported files.
// Ancestors must already be recorded; their walks need not yet be complete.
func (evidence *scanReconciliationEvidence) RecordDirectory(rootID, relative string, before os.FileInfo, raw []os.DirEntry) error {
	if err := evidence.Err(); err != nil {
		return err
	}
	relative, valid := scanReconciliationRelative(relative, true)
	root := evidence.roots[rootID]
	if !valid || root == nil || root.directories[relative] != nil || !scanReconciliationDirectoryInfo(before) {
		return evidence.unavailable("directory observation is invalid or repeated")
	}
	if len(raw) > evidence.limits.entries-evidence.entries {
		return evidence.Disable(errScanReconciliationEvidenceBudget)
	}
	charge := scanReconciliationDirectoryBytes + int64(len(relative))
	if charge > evidence.limits.bytes-evidence.bytes {
		return evidence.Disable(errScanReconciliationEvidenceBudget)
	}
	for _, entry := range raw {
		if entry == nil || !scanReconciliationEntryName(entry.Name()) {
			return evidence.unavailable("directory membership contains an invalid name")
		}
		entryCharge := scanReconciliationEntryBytes + 2*int64(len(entry.Name()))
		if entryCharge > evidence.limits.bytes-evidence.bytes-charge {
			return evidence.Disable(errScanReconciliationEvidenceBudget)
		}
		charge += entryCharge
	}
	if err := evidence.reserve(1, len(raw), 0, charge); err != nil {
		return err
	}
	held, err := evidence.openDirectory(context.Background(), root, relative)
	if err != nil {
		return evidence.Disable(err)
	}
	observed, err := held.Stat(".")
	if err != nil || !scanReconciliationSameInfo(before, observed) {
		_ = held.Close()
		return evidence.unavailable("walked directory changed before membership capture")
	}
	witness := &scanReconciliationDirectoryEvidence{
		relative: strings.Clone(relative), held: held, info: before,
		entries: make(map[string]scanReconciliationEntryEvidence, len(raw)),
	}
	// Register ownership before any later failure can release the whole pass.
	root.directories[witness.relative] = witness
	evidence.incompleteDirectories++
	for _, entry := range raw {
		name := entry.Name()
		if _, duplicate := witness.entries[name]; duplicate {
			return evidence.unavailable("directory membership contains a duplicate name")
		}
		info, err := held.Lstat(name)
		if err != nil || !scanReconciliationInfo(info) || info.Mode().Type() != entry.Type().Type() {
			return evidence.unavailable("directory entry changed during membership capture")
		}
		witness.entries[strings.Clone(name)] = scanReconciliationEntryEvidence{info: info, mode: entry.Type().Type()}
	}
	if err := evidence.verifyDirectory(context.Background(), root, witness); err != nil {
		return evidence.Disable(err)
	}
	return nil
}

// CompleteDirectory is called only after the directory's entire walk succeeds.
// A repeated completion is harmless; it cannot replace the original snapshot.
func (evidence *scanReconciliationEvidence) CompleteDirectory(rootID, relative string) error {
	if err := evidence.Err(); err != nil {
		return err
	}
	relative, valid := scanReconciliationRelative(relative, true)
	root := evidence.roots[rootID]
	if !valid || root == nil || root.directories[relative] == nil {
		return evidence.unavailable("completed directory was not observed")
	}
	witness := root.directories[relative]
	if err := evidence.verifyDirectory(context.Background(), root, witness); err != nil {
		return evidence.Disable(err)
	}
	if !witness.complete {
		witness.complete = true
		evidence.incompleteDirectories--
		if relative == "." {
			evidence.completeRoots++
		}
	}
	return nil
}

// MarkSeen records only a successfully accepted ordinary item, including a
// cache hit. IDs share one bound across all roots and repeated IDs cost nothing.
func (evidence *scanReconciliationEvidence) MarkSeen(itemID string) error {
	if err := evidence.Err(); err != nil {
		return err
	}
	if itemID == "" || strings.ContainsRune(itemID, '\x00') {
		return evidence.unavailable("accepted item ID is invalid")
	}
	if _, exists := evidence.seen[itemID]; exists {
		return nil
	}
	if err := evidence.reserve(0, 0, 1, scanReconciliationSeenIDBytes+int64(len(itemID))); err != nil {
		return err
	}
	evidence.seen[strings.Clone(itemID)] = struct{}{}
	return nil
}

func (evidence *scanReconciliationEvidence) Seen(itemID string) bool {
	if evidence == nil {
		return false
	}
	_, exists := evidence.seen[itemID]
	return exists
}

// Revalidate checks every complete listing and its currently named chain.
// It uses only owned handles and the supplied original scan context, and never
// takes Store.mu. Storage topology and approved revisions require separate checks.
func (evidence *scanReconciliationEvidence) Revalidate(ctx context.Context) error {
	if err := evidence.requireComplete(ctx); err != nil {
		return err
	}
	for _, root := range evidence.roots {
		for _, witness := range root.directories {
			if err := evidence.verifyDirectory(ctx, root, witness); err != nil {
				return evidence.Disable(err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return evidence.Disable(err)
	}
	return nil
}

// PathAbsent accepts only physical clean relative paths. A missing component
// must be absent both from the original complete parent listing and a fresh
// anchored Lstat returning actual ENOENT. A previously present entry that later
// disappears is changed evidence. Existing ignored names remain present.
func (evidence *scanReconciliationEvidence) PathAbsent(ctx context.Context, rootID, relative string) (bool, error) {
	if err := evidence.requireComplete(ctx); err != nil {
		return false, err
	}
	relative, valid := scanReconciliationRelative(relative, false)
	root := evidence.roots[rootID]
	if !valid || root == nil {
		return false, evidence.unavailable("absence candidate is not a physical root-relative path")
	}
	parent, remaining := ".", relative
	for remaining != "" {
		if err := ctx.Err(); err != nil {
			return false, evidence.Disable(err)
		}
		component, rest, _ := strings.Cut(remaining, "/")
		witness := root.directories[parent]
		if witness == nil || !witness.complete {
			return false, evidence.unavailable("absence parent was not completely observed")
		}
		named, err := evidence.openDirectory(ctx, root, parent)
		if err != nil {
			return false, evidence.Disable(err)
		}
		_ = named.Close()
		before, err := witness.held.Stat(".")
		if err != nil || !scanReconciliationSameInfo(witness.info, before) {
			return false, evidence.unavailable("absence parent changed before lookup")
		}
		entry, entryErr := witness.held.Lstat(component)
		after, afterErr := witness.held.Stat(".")
		if afterErr != nil || !scanReconciliationSameInfo(witness.info, after) {
			return false, evidence.unavailable("absence parent changed during lookup")
		}
		named, err = evidence.openDirectory(ctx, root, parent)
		if err != nil {
			return false, evidence.Disable(err)
		}
		_ = named.Close()
		if err := ctx.Err(); err != nil {
			return false, evidence.Disable(err)
		}
		previous, present := witness.entries[component]
		if !present {
			if errors.Is(entryErr, os.ErrNotExist) {
				return true, nil
			}
			return false, evidence.unavailable("unlisted component does not have fresh absence evidence")
		}
		if entryErr != nil || !scanReconciliationSameInfo(previous.info, entry) {
			return false, evidence.unavailable("previously listed component changed or disappeared")
		}
		if rest == "" {
			return false, nil
		}
		if !scanReconciliationDirectoryInfo(entry) {
			return false, evidence.unavailable("absence path contains a symlink or non-directory")
		}
		parent = relative[:len(relative)-len(rest)-1]
		remaining = rest
	}
	return false, evidence.unavailable("absence candidate has no component")
}

func (evidence *scanReconciliationEvidence) Err() error {
	if evidence == nil {
		return errScanReconciliationEvidenceUnavailable
	}
	return evidence.err
}

// Disable is irreversible. It releases every owned witness without closing
// any borrowed scan root, and preserves the first reason for diagnostics.
func (evidence *scanReconciliationEvidence) Disable(cause error) error {
	if evidence == nil {
		return errScanReconciliationEvidenceUnavailable
	}
	if evidence.err == nil {
		if errors.Is(cause, errScanReconciliationEvidenceBudget) {
			evidence.err = errScanReconciliationEvidenceBudget
		} else {
			evidence.err = errors.Join(errScanReconciliationEvidenceUnavailable, cause)
		}
	}
	_ = evidence.release()
	return evidence.err
}

func (evidence *scanReconciliationEvidence) Close() error {
	if evidence == nil || evidence.closed {
		return nil
	}
	if evidence.err == nil {
		evidence.err = errScanReconciliationEvidenceUnavailable
	}
	return evidence.release()
}

func (evidence *scanReconciliationEvidence) release() error {
	if evidence.closed {
		return nil
	}
	evidence.closed = true
	var result error
	for _, root := range evidence.roots {
		for _, witness := range root.directories {
			result = errors.Join(result, witness.held.Close())
		}
		result = errors.Join(result, root.anchor.Close())
	}
	evidence.roots = nil
	evidence.seen = nil
	return result
}

func (evidence *scanReconciliationEvidence) unavailable(reason string) error {
	return evidence.Disable(fmt.Errorf("%w: %s", errScanReconciliationEvidenceUnavailable, reason))
}

func (evidence *scanReconciliationEvidence) reserve(directories, entries, seenIDs int, bytes int64) error {
	if directories < 0 || entries < 0 || seenIDs < 0 || bytes < 0 ||
		directories > evidence.limits.directories-evidence.directories ||
		entries > evidence.limits.entries-evidence.entries ||
		seenIDs > evidence.limits.seenIDs-evidence.seenIDs || bytes > evidence.limits.bytes-evidence.bytes {
		return evidence.Disable(errScanReconciliationEvidenceBudget)
	}
	evidence.directories += directories
	evidence.entries += entries
	evidence.seenIDs += seenIDs
	evidence.bytes += bytes
	return nil
}

func (evidence *scanReconciliationEvidence) requireComplete(ctx context.Context) error {
	if err := evidence.Err(); err != nil {
		return err
	}
	if ctx == nil {
		return evidence.unavailable("scan context is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return evidence.Disable(err)
	}
	if len(evidence.roots) == 0 || evidence.completeRoots != len(evidence.roots) || evidence.incompleteDirectories != 0 {
		return evidence.unavailable("not every root and observed directory completed its walk")
	}
	return nil
}

// openDirectory checks each name from the retained registered root. An
// existing parent listing must identify every traversed child. It returns an
// independent descriptor, and never cleans, follows or invents components.
func (evidence *scanReconciliationEvidence) openDirectory(ctx context.Context, root *scanReconciliationRootEvidence, relative string) (_ *os.Root, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := root.anchor.OpenRoot(".")
	if err != nil {
		return nil, errScanReconciliationEvidenceUnavailable
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
		witness := root.directories[parent]
		if witness == nil || !scanReconciliationSameInfo(witness.info, info) {
			return nil, errScanReconciliationEvidenceUnavailable
		}
		previous, present := witness.entries[component]
		if !present || !scanReconciliationDirectoryInfo(previous.info) {
			return nil, errScanReconciliationEvidenceUnavailable
		}
		before, err := current.Lstat(component)
		if err != nil || !scanReconciliationSameInfo(previous.info, before) {
			return nil, errScanReconciliationEvidenceUnavailable
		}
		next, err := current.OpenRoot(component)
		if err != nil {
			return nil, errScanReconciliationEvidenceUnavailable
		}
		nextInfo, nextErr := next.Stat(".")
		namedAfter, namedErr := current.Lstat(component)
		parentAfter, parentErr := current.Stat(".")
		if nextErr != nil || namedErr != nil || parentErr != nil ||
			!scanReconciliationSameInfo(previous.info, nextInfo) ||
			!scanReconciliationSameInfo(previous.info, namedAfter) ||
			!scanReconciliationSameInfo(witness.info, parentAfter) {
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
	if witness := root.directories[relative]; witness != nil && !scanReconciliationSameInfo(witness.info, info) {
		return nil, errScanReconciliationEvidenceUnavailable
	}
	return current, nil
}

func (evidence *scanReconciliationEvidence) verifyDirectory(ctx context.Context, root *scanReconciliationRootEvidence, witness *scanReconciliationDirectoryEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	named, err := evidence.openDirectory(ctx, root, witness.relative)
	if err != nil {
		return err
	}
	_ = named.Close()
	directory, err := openScanFile(witness.held, ".")
	if err != nil {
		return errScanReconciliationEvidenceUnavailable
	}
	defer directory.Close()
	before, err := directory.Stat()
	if err != nil || !scanReconciliationSameInfo(witness.info, before) {
		return errScanReconciliationEvidenceUnavailable
	}
	witness.readPass++
	if witness.readPass == 0 {
		return errScanReconciliationEvidenceUnavailable
	}
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Read only a bounded batch, including one possible unexpected member.
		// Never allocate an unbounded fresh listing or another membership map.
		batch := min(scanReconciliationReadBatch, len(witness.entries)-count+1)
		entries, readErr := directory.ReadDir(batch)
		for _, entry := range entries {
			previous, exists := witness.entries[entry.Name()]
			if !exists || previous.lastRead == witness.readPass || entry.Type().Type() != previous.mode {
				return errScanReconciliationEvidenceUnavailable
			}
			info, err := witness.held.Lstat(entry.Name())
			if err != nil || !scanReconciliationSameInfo(previous.info, info) {
				return errScanReconciliationEvidenceUnavailable
			}
			previous.lastRead = witness.readPass
			witness.entries[entry.Name()] = previous
			count++
		}
		if readErr != nil {
			if readErr != io.EOF {
				return errScanReconciliationEvidenceUnavailable
			}
			break
		}
		if len(entries) == 0 {
			return errScanReconciliationEvidenceUnavailable
		}
	}
	// Entry writes do not necessarily change their parent's timestamps. Repeat
	// member versions after the complete read so a rewrite after its earlier
	// Lstat cannot hide behind an unchanged directory version at this boundary.
	for name, previous := range witness.entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := witness.held.Lstat(name)
		if err != nil || !scanReconciliationSameInfo(previous.info, info) {
			return errScanReconciliationEvidenceUnavailable
		}
	}
	after, err := directory.Stat()
	heldAfter, heldErr := witness.held.Stat(".")
	if count != len(witness.entries) || err != nil || heldErr != nil ||
		!scanReconciliationSameInfo(witness.info, after) || !scanReconciliationSameInfo(witness.info, heldAfter) {
		return errScanReconciliationEvidenceUnavailable
	}
	named, err = evidence.openDirectory(ctx, root, witness.relative)
	if err != nil {
		return err
	}
	_ = named.Close()
	return ctx.Err()
}

func scanReconciliationRelative(relative string, allowRoot bool) (string, bool) {
	if relative == "" || len(relative) > scanReconciliationMaxPathBytes || strings.ContainsRune(relative, '\x00') ||
		filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" || len(relative) >= 2 && relative[1] == ':' {
		return "", false
	}
	relative = filepath.ToSlash(relative)
	if relative == "." {
		return relative, allowRoot
	}
	if strings.Contains(relative, "\\") || strings.HasPrefix(relative, "/") || path.Clean(relative) != relative {
		return "", false
	}
	remaining, components := relative, 0
	for remaining != "" {
		components++
		component, rest, _ := strings.Cut(remaining, "/")
		if components > MaxThemeAncestorDepth+1 || !scanReconciliationEntryName(component) {
			return "", false
		}
		remaining = rest
	}
	return relative, true
}

func scanReconciliationEntryName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 &&
		!strings.ContainsAny(name, "/\\\x00")
}

func scanReconciliationInfo(info os.FileInfo) bool {
	return info != nil && media.FileChangeTime(info) > 0 && fileIdentity(info) != ""
}

func scanReconciliationDirectoryInfo(info os.FileInfo) bool {
	return scanReconciliationInfo(info) && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func scanReconciliationSameInfo(before, after os.FileInfo) bool {
	return scanReconciliationInfo(before) && scanReconciliationInfo(after) && os.SameFile(before, after) &&
		before.Mode() == after.Mode() && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) &&
		media.FileChangeTime(before) == media.FileChangeTime(after)
}
