//go:build linux

package transcode

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type cacheFileFact struct {
	name           string
	device         uint64
	inode          uint64
	bytes          int64
	mode           uint32
	links          uint64
	modified       syscall.Timespec
	changed        syscall.Timespec
	previous       *cacheFileFact
	next           *cacheFileFact
	chargePrevious *cacheFileFact
	chargeNext     *cacheFileFact
}

type cacheInode struct {
	device uint64
	number uint64
}

type cacheInodeCharge struct {
	first *cacheFileFact
	refs  int
	bytes int64
}

type cacheRenditionCount struct {
	playlists int
	init      int
	segments  [4]int
}

// One nonblocking notification queue belongs to one opened job directory.
// Events identify candidates for descriptor checks; they never supply file
// identity, sizes, deletion authority or proof of unchanged file contents.
type cacheInventory struct {
	owner             *cacheRoot
	directory         *os.File
	device            uint64
	inode             uint64
	watch             int
	watchID           int
	fallback          bool
	failure           error
	plan              Plan
	bound             bool
	seeded            bool
	frozen            bool
	files             map[string]*cacheFileFact
	inodes            map[cacheInode]*cacheInodeCharge
	first             *cacheFileFact
	audit             *cacheFileFact
	auditAt           time.Time
	membershipAt      time.Time
	membershipActive  bool
	membershipOffset  int64
	membershipData    [4096]byte
	membershipStart   int
	membershipEnd     int
	directoryModified syscall.Timespec
	directoryChanged  syscall.Timespec
	bytes             int64
	counts            [5]cacheRenditionCount
	dirty             map[string]struct{}
	events            [4096]byte
	eventStart        int
	eventEnd          int
}

func cacheNotificationFilesystem(kind int64) bool {
	// Directory notifications do not cover remote/network or arbitrary FUSE
	// writers. Restrict incremental accounting to proven local filesystems.
	switch kind {
	case unix.EXT4_SUPER_MAGIC, unix.XFS_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC, unix.TMPFS_MAGIC:
		return true
	default:
		return false
	}
}

// inventory requires the root read lock and the keyed job lock. The registry
// owns long-lived state independently of the ref-counted operation locks.
func (c *cacheRoot) inventory(id string) (*cacheInventory, error) {
	c.jobsMu.Lock()
	existing := c.maintenance[id]
	c.jobsMu.Unlock()
	if existing != nil {
		return existing, existing.failure
	}
	directory, err := cacheOpenDirectoryAt(c.dir, id)
	if err != nil {
		return nil, err
	}
	state := &cacheInventory{owner: c, directory: directory, watch: -1, files: make(map[string]*cacheFileFact), inodes: make(map[cacheInode]*cacheInodeCharge),
		dirty: make(map[string]struct{}), auditAt: time.Now(), membershipAt: time.Now()}
	defer func() {
		if err != nil {
			_ = state.close()
		}
	}()
	info, err := directory.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		err = ErrCacheUnsafe
		return nil, err
	}
	state.device, state.inode = uint64(stat.Dev), stat.Ino
	if watchErr := state.startWatcher(); watchErr != nil {
		// Notification capacity is optional. A running job without a reliable
		// watcher keeps the original complete-inspection accounting path.
		if state.watch >= 0 {
			_ = unix.Close(state.watch)
			state.watch = -1
		}
		state.fallback = true
	}
	// This first membership pass is mandatory even for an empty newly created
	// directory. It also closes the boundary for externally seeded fixtures.
	state.membershipActive = true
	c.jobsMu.Lock()
	if c.maintenance == nil {
		c.maintenance = make(map[string]*cacheInventory)
	}
	c.maintenance[id] = state
	c.jobsMu.Unlock()
	return state, nil
}

func (s *cacheInventory) startWatcher() error {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(int(s.directory.Fd()), &filesystem); err != nil {
		return err
	}
	if !cacheNotificationFilesystem(int64(filesystem.Type)) {
		return fmt.Errorf("%w: cache filesystem cannot witness local changes", ErrCacheUnsafe)
	}
	var err error
	s.watch, err = unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return err
	}
	const mask = unix.IN_ONLYDIR | unix.IN_ATTRIB | unix.IN_MODIFY | unix.IN_CLOSE_WRITE |
		unix.IN_CREATE | unix.IN_DELETE | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF |
		unix.IN_MOVED_FROM | unix.IN_MOVED_TO | unix.IN_UNMOUNT
	s.watchID, err = unix.InotifyAddWatch(s.watch, "/proc/self/fd/"+strconv.Itoa(int(s.directory.Fd())), mask)
	return err
}

func (s *cacheInventory) close() error {
	var err error
	if s.watch >= 0 {
		err = unix.Close(s.watch)
		s.watch = -1
	}
	s.membershipActive = false
	if s.directory != nil {
		err = errors.Join(err, s.directory.Close())
		s.directory = nil
	}
	return err
}

func cacheFact(info os.FileInfo, name string) (cacheFileFact, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink > 1 || info.Size() < 0 {
		return cacheFileFact{}, ErrCacheUnsafe
	}
	return cacheFileFact{name: name, device: uint64(stat.Dev), inode: stat.Ino,
		bytes: info.Size(), mode: stat.Mode, links: uint64(stat.Nlink), modified: stat.Mtim, changed: stat.Ctim}, nil
}

func (f *cacheFileFact) same(other *cacheFileFact) bool {
	return f != nil && other != nil && f.device == other.device && f.inode == other.inode &&
		f.bytes == other.bytes && f.mode == other.mode && f.links == other.links &&
		f.modified == other.modified && f.changed == other.changed
}

func (s *cacheInventory) validName(name string) bool {
	return validCacheFileName(name) && (s.plan.OutputMode != "progressive" && name != "stream.bin" ||
		s.plan.OutputMode == "progressive" && (name == "stream.bin" || privateCacheAssetName(name)))
}

func (s *cacheInventory) count(f *cacheFileFact, delta int) {
	if f == nil || f.bytes == 0 {
		return
	}
	kind, ok := HLSArtifact(f.name)
	if !ok {
		return
	}
	index := 0
	if len(f.name) > 2 && f.name[0] == 'v' && f.name[1] >= '0' && f.name[1] <= '3' {
		index = int(f.name[1]-'0') + 1
	}
	r := &s.counts[index]
	switch kind {
	case "playlist":
		r.playlists += delta
	case "init":
		r.init += delta
	case "segment":
		for index, extension := range [4]string{".ts", ".m4s", ".aac", ".mp3"} {
			if strings.HasSuffix(f.name, extension) {
				r.segments[index] += delta
				break
			}
		}
	}
}

func (s *cacheInventory) ready() bool {
	if s.plan.OutputMode == "progressive" {
		return s.files["stream.bin"] != nil && s.files["stream.bin"].bytes > 0
	}
	ready := func(r cacheRenditionCount) bool {
		if r.playlists == 0 {
			return false
		}
		switch s.plan.HLS.SegmentType {
		case "mpegts":
			return r.segments[0] > 0
		case "fmp4":
			return r.init > 0 && r.segments[1] > 0
		case "packed":
			return r.segments[2] > 0 || r.segments[3] > 0
		case "":
			return r.segments[0] > 0 || r.init > 0 && r.segments[1] > 0 || r.segments[2] > 0 || r.segments[3] > 0
		default:
			return false
		}
	}
	if s.plan.HLS.RenditionCount == 0 {
		return ready(s.counts[0])
	}
	if s.plan.HLS.RenditionCount < 2 || s.plan.HLS.RenditionCount > len(s.plan.HLS.Renditions) {
		return false
	}
	for index := range s.plan.HLS.RenditionCount {
		if !ready(s.counts[index+1]) {
			return false
		}
	}
	return true
}

func (c *cacheRoot) replaceFact(s *cacheInventory, name string, replacement *cacheFileFact) error {
	old := s.files[name]
	if s.frozen && !old.same(replacement) {
		return fmt.Errorf("%w: completed output changed", ErrCacheUnsafe)
	}
	if old.same(replacement) {
		return nil
	}
	if replacement != nil && old == nil {
		c.jobsMu.Lock()
		if c.maintenanceFacts >= cacheMaintenanceMaxFacts || len(s.files) >= maxJobFiles {
			c.jobsMu.Unlock()
			return fmt.Errorf("%w: cache fact budget exhausted", ErrCacheUnsafe)
		}
		c.maintenanceFacts++
		c.jobsMu.Unlock()
	}
	s.count(old, -1)
	s.count(replacement, 1)
	if old != nil {
		s.removeCharge(old)
	}
	if old != nil && replacement != nil {
		old.device, old.inode, old.bytes, old.mode, old.links = replacement.device, replacement.inode, replacement.bytes, replacement.mode, replacement.links
		old.modified, old.changed = replacement.modified, replacement.changed
		return s.addCharge(old)
	}
	if old != nil {
		if old.previous != nil {
			old.previous.next = old.next
		} else {
			s.first = old.next
		}
		if old.next != nil {
			old.next.previous = old.previous
		}
		if s.audit == old {
			s.audit = old.next
			if s.audit == nil {
				s.auditAt = time.Now()
			}
		}
		delete(s.files, name)
		c.jobsMu.Lock()
		c.maintenanceFacts--
		c.jobsMu.Unlock()
	}
	if replacement != nil {
		replacement.next = s.first
		if s.first != nil {
			s.first.previous = replacement
		}
		s.first = replacement
		s.files[name] = replacement
		return s.addCharge(replacement)
	}
	return nil
}

// Charges are keyed by inode independently of cached names. An atomic rename
// can leave both old and new name facts across bounded batches; publishing the
// same inode must not manufacture extra bytes or a false quota violation.
func (s *cacheInventory) addCharge(fact *cacheFileFact) error {
	key := cacheInode{fact.device, fact.inode}
	charge := s.inodes[key]
	if charge == nil {
		charge = &cacheInodeCharge{}
		s.inodes[key] = charge
	}
	maximum := max(charge.bytes, fact.bytes)
	if maximum-charge.bytes > math.MaxInt64-s.bytes {
		return fmt.Errorf("%w: aggregate job size overflow", ErrCacheUnsafe)
	}
	s.bytes += maximum - charge.bytes
	charge.bytes, charge.refs = maximum, charge.refs+1
	fact.chargeNext = charge.first
	if charge.first != nil {
		charge.first.chargePrevious = fact
	}
	charge.first = fact
	return nil
}

func (s *cacheInventory) removeCharge(fact *cacheFileFact) {
	key := cacheInode{fact.device, fact.inode}
	charge := s.inodes[key]
	if fact.chargePrevious != nil {
		fact.chargePrevious.chargeNext = fact.chargeNext
	} else {
		charge.first = fact.chargeNext
	}
	if fact.chargeNext != nil {
		fact.chargeNext.chargePrevious = fact.chargePrevious
	}
	fact.chargePrevious, fact.chargeNext = nil, nil
	charge.refs--
	previous := charge.bytes
	if charge.refs == 0 {
		charge.bytes = 0
		delete(s.inodes, key)
	} else if charge.refs == 1 {
		charge.bytes = charge.first.bytes
	}
	// Multiple cached aliases retain a conservative maximum until stale names
	// are resolved. Ordinary files and a closed rename batch have one name.
	s.bytes += charge.bytes - previous
}

func (c *cacheRoot) inspectInventoryName(s *cacheInventory, name string) (bool, error) {
	if !s.validName(name) {
		return false, fmt.Errorf("%w: unrecognized or mixed output name", ErrCacheUnsafe)
	}
	file, err := cacheOpenRegular(s.directory, name, syscall.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) && !s.frozen && s.plan.OutputMode == "" {
		return true, c.replaceFact(s, name, nil)
	}
	if err != nil {
		return false, err
	}
	info, err := file.Stat()
	_ = file.Close()
	if err != nil {
		return false, err
	}
	fact, err := cacheFact(info, name)
	if err != nil {
		return false, err
	}
	if fact.links == 0 && !s.frozen {
		// Atomic publication can unlink the opened temporary inode. Its name
		// remains pending until a fresh descriptor resolves the rename.
		return false, nil
	}
	if s.files[name].same(&fact) {
		return true, nil
	}
	// Stable audits compare stack facts. Allocate retained facts only after
	// detecting a change, avoiding map/charge churn for unchanged segments.
	updated := fact
	return true, c.replaceFact(s, name, &updated)
}

// readEvent consumes at most one record, retaining unparsed bytes across
// batches. A failed observation generation never becomes usable after drain.
func (s *cacheInventory) readEvent() (bool, error) {
	if s.eventStart == s.eventEnd {
		s.eventStart, s.eventEnd = 0, 0
		n, err := unix.Read(s.watch, s.events[:])
		if errors.Is(err, unix.EAGAIN) {
			return false, nil
		}
		if errors.Is(err, unix.EINTR) {
			return true, nil
		}
		if err != nil || n <= 0 {
			return false, errors.Join(ErrCacheUnsafe, err)
		}
		s.eventEnd = n
	}
	if s.eventEnd-s.eventStart < unix.SizeofInotifyEvent {
		return false, fmt.Errorf("%w: truncated cache change record", ErrCacheUnsafe)
	}
	data := s.events[s.eventStart:s.eventEnd]
	watchID := int(int32(binary.NativeEndian.Uint32(data[:4])))
	mask := binary.NativeEndian.Uint32(data[4:8])
	length := int(binary.NativeEndian.Uint32(data[12:16]))
	if length < 0 || length > unix.NAME_MAX+1 || unix.SizeofInotifyEvent+length > len(data) {
		return false, fmt.Errorf("%w: invalid cache change record", ErrCacheUnsafe)
	}
	s.eventStart += unix.SizeofInotifyEvent + length
	if watchID != s.watchID || mask&(unix.IN_Q_OVERFLOW|unix.IN_IGNORED|unix.IN_UNMOUNT|unix.IN_DELETE_SELF|unix.IN_MOVE_SELF|unix.IN_ISDIR) != 0 || length == 0 {
		return false, fmt.Errorf("%w: cache change history is unavailable", ErrCacheUnsafe)
	}
	name := strings.TrimRight(string(data[unix.SizeofInotifyEvent:unix.SizeofInotifyEvent+length]), "\x00")
	if !s.validName(name) {
		return false, fmt.Errorf("%w: invalid or excessive cache changes", ErrCacheUnsafe)
	}
	if s.frozen && mask&(unix.IN_MODIFY|unix.IN_CLOSE_WRITE|unix.IN_CREATE|unix.IN_DELETE|unix.IN_MOVED_FROM|unix.IN_MOVED_TO) != 0 {
		// Completed output has no remaining producer. A write/name-change
		// witness revokes reuse even if coarse timestamps happen to coincide;
		// the eventual final/cleanup inspection still supplies deletion facts.
		return false, fmt.Errorf("%w: completed output was mutated", ErrCacheUnsafe)
	}
	return true, s.markDirty(name)
}

func (s *cacheInventory) markDirty(name string) error {
	if _, exists := s.dirty[name]; exists {
		return nil
	}
	s.owner.jobsMu.Lock()
	defer s.owner.jobsMu.Unlock()
	if len(s.dirty) >= maxJobFiles || s.owner.maintenanceDirty >= cacheMaintenanceMaxFacts {
		return fmt.Errorf("%w: cache change budget exhausted", ErrCacheUnsafe)
	}
	s.dirty[name] = struct{}{}
	s.owner.maintenanceDirty++
	return nil
}

func (s *cacheInventory) forgetDirty(name string) {
	if _, exists := s.dirty[name]; exists {
		delete(s.dirty, name)
		s.owner.jobsMu.Lock()
		s.owner.maintenanceDirty--
		s.owner.jobsMu.Unlock()
	}
}

func (s *cacheInventory) queued() (bool, error) {
	if s.eventStart != s.eventEnd || len(s.dirty) != 0 {
		return true, nil
	}
	if s.watch < 0 && s.frozen {
		return false, nil
	}
	bytes, err := unix.IoctlGetInt(s.watch, unix.TIOCINQ)
	if err != nil {
		return false, errors.Join(ErrCacheUnsafe, err)
	}
	return bytes != 0, nil
}

// nextMember retains a Linux directory cookie and unparsed fixed-size bytes,
// rather than a descriptor per retained cache. Each refill opens and verifies
// the same directory through its already-authorized descriptor.
func (s *cacheInventory) nextMember() (string, bool, error) {
	if s.membershipStart == s.membershipEnd {
		s.membershipStart, s.membershipEnd = 0, 0
		reader, err := cacheOpenDirectoryAt(s.directory, ".")
		if err != nil {
			return "", false, err
		}
		if _, err := reader.Seek(s.membershipOffset, io.SeekStart); err != nil {
			_ = reader.Close()
			return "", false, err
		}
		n, err := unix.ReadDirent(int(reader.Fd()), s.membershipData[:])
		closeErr := reader.Close()
		if err != nil || closeErr != nil {
			return "", false, errors.Join(err, closeErr)
		}
		if n == 0 {
			return "", false, nil
		}
		s.membershipEnd = n
	}
	data := s.membershipData[s.membershipStart:s.membershipEnd]
	// getdents64 uses the same fixed header on every Linux architecture.
	if len(data) < 20 {
		return "", false, fmt.Errorf("%w: invalid directory record", ErrCacheUnsafe)
	}
	length := int(binary.NativeEndian.Uint16(data[16:18]))
	if length < 20 || length > len(data) {
		return "", false, fmt.Errorf("%w: invalid directory record length", ErrCacheUnsafe)
	}
	s.membershipOffset = int64(binary.NativeEndian.Uint64(data[8:16]))
	s.membershipStart += length
	nameBytes := data[19:length]
	terminator := -1
	for index, value := range nameBytes {
		if value == 0 {
			terminator = index
			break
		}
	}
	if terminator < 0 {
		return "", false, fmt.Errorf("%w: unterminated directory name", ErrCacheUnsafe)
	}
	name := string(nameBytes[:terminator])
	if name == "." || name == ".." || binary.NativeEndian.Uint64(data[:8]) == 0 {
		return "", true, nil
	}
	return name, true, nil
}

// MaintainPlanJob bounds both changed-name checks and blind-spot audits. A
// directory timestamp is never consulted as proof of unchanged file content.
// The caller serializes the resulting accounting update with this operation.
func (c *cacheRoot) MaintainPlanJob(id string, plan Plan, budget int) (result cacheMaintenanceResult, err error) {
	if !validJobID(id) || budget < 6 || plan.OutputMode != "" && plan.OutputMode != "progressive" {
		return result, ErrCacheInvalid
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return result, os.ErrClosed
	}
	job := c.lockJob(id)
	defer c.unlockJob(id, job)
	s, err := c.inventory(id)
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			s.failure = err
		}
	}()
	if s.bound && s.plan != plan {
		return result, fmt.Errorf("%w: cache plan changed", ErrCacheUnsafe)
	}
	s.bound, s.plan = true, plan
	result.bytes, result.ready = s.bytes, s.ready()
	if (!s.fallback || s.frozen) && (time.Since(s.auditAt) > cacheMaintenanceAuditLimit || time.Since(s.membershipAt) > cacheMaintenanceAuditLimit) {
		return result, fmt.Errorf("%w: cache integrity audit exceeded its deadline", ErrCacheUnsafe)
	}
	// Reopen the root entry so renaming the held job descriptor cannot hide a
	// same-ID replacement. The retained directory remains the I/O authority.
	current, err := cacheOpenDirectoryAt(c.dir, id)
	if err != nil {
		return result, err
	}
	if err = s.checkDirectory(current); err != nil {
		_ = current.Close()
		return result, err
	}
	if s.directory == nil {
		s.directory = current
		defer func() { _ = current.Close(); s.directory = nil }()
	} else {
		_ = current.Close()
	}
	if s.fallback && !s.frozen {
		// This is intentionally the legacy complete scan, rather than an
		// unobserved incremental census. Resource-limited systems keep their
		// previous capacity and safety at the previous maintenance cost.
		files, bytes, ready, scanErr := cacheInspectJobMode(s.directory, s.plan.OutputMode == "", func() cacheInspectionMode {
			if s.plan.OutputMode == "progressive" {
				return cacheInspectProgressive
			}
			return cacheInspectHLS
		}())
		if scanErr == nil && s.plan.OutputMode == "" {
			ready = cacheHLSReady(files, plan)
		}
		if scanErr == nil {
			s.auditAt, s.membershipAt, s.seeded = time.Now(), time.Now(), true
		}
		return cacheMaintenanceResult{bytes: bytes, ready: ready, work: budget}, scanErr
	}
	result.work = 1
	// Split each allowance so a continuous active writer cannot starve either
	// authoritative membership checks or stat checks of previously known files.
	eventBudget := min(budget/3, cacheMaintenanceJobBudget/3)
	for !s.frozen && result.work < eventBudget {
		observed, readErr := s.readEvent()
		result.work++
		if readErr != nil {
			return result, readErr
		}
		if !observed {
			break
		}
	}
	for name := range s.dirty {
		if result.work+1 >= budget/2 {
			break
		}
		resolved, inspectErr := c.inspectInventoryName(s, name)
		result.work++
		if inspectErr != nil {
			return result, inspectErr
		}
		if resolved {
			s.forgetDirty(name)
		}
	}
	if !s.membershipActive {
		s.membershipActive, s.membershipOffset, s.membershipStart, s.membershipEnd = true, 0, 0, 0
	}
	for result.work+2 < budget*3/4 {
		name, present, readErr := s.nextMember()
		result.work++
		if name != "" {
			if !s.validName(name) {
				return result, fmt.Errorf("%w: unrecognized cache membership", ErrCacheUnsafe)
			}
			if s.files[name] == nil {
				resolved, inspectErr := c.inspectInventoryName(s, name)
				result.work++
				if inspectErr != nil {
					return result, inspectErr
				}
				if !resolved {
					if err = s.markDirty(name); err != nil {
						return result, err
					}
				}
			}
		}
		if !present && readErr == nil {
			s.membershipActive = false
			s.seeded, s.membershipAt = true, time.Now()
			break
		}
		if readErr != nil {
			return result, readErr
		}
	}
	if s.audit == nil {
		s.audit = s.first
	}
	for s.audit != nil && result.work < budget {
		fact := s.audit
		s.audit = fact.next
		resolved, inspectErr := c.inspectInventoryName(s, fact.name)
		result.work++
		if inspectErr != nil {
			return result, inspectErr
		}
		if !resolved {
			if err = s.markDirty(fact.name); err != nil {
				return result, err
			}
		}
	}
	if s.audit == nil {
		s.auditAt = time.Now()
	}
	result.bytes, result.ready = s.bytes, s.ready()
	result.pending, err = s.queued()
	// The first membership census must finish before an empty/new baseline can
	// claim complete accounting. Subsequent audits retain verified old facts.
	if !s.seeded {
		result.pending = true
	}
	return result, err
}

func (c *cacheRoot) checkFrozenFile(id, name string, info os.FileInfo, directory *os.File) error {
	c.jobsMu.Lock()
	s := c.maintenance[id]
	c.jobsMu.Unlock()
	if s == nil || !s.frozen {
		return nil
	}
	if s.failure != nil {
		return s.failure
	}
	err := s.checkDirectory(directory)
	if err != nil {
		s.failure = err
		return err
	}
	fact, err := cacheFact(info, name)
	if err == nil && !s.files[name].same(&fact) {
		err = fmt.Errorf("%w: completed output changed", ErrCacheUnsafe)
	}
	if err != nil {
		s.failure = err
	}
	return err
}

// FinalizePlanJob is deliberately a complete inspection after the producer
// has exited. Bounded maintenance never replaces this completion boundary or
// the separate guarded deletion inspection.
func (c *cacheRoot) FinalizePlanJob(id string, plan Plan) (bytes int64, ready bool, err error) {
	if !validJobID(id) || plan.OutputMode != "" && plan.OutputMode != "progressive" {
		return 0, false, ErrCacheInvalid
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return 0, false, os.ErrClosed
	}
	job := c.lockJob(id)
	defer c.unlockJob(id, job)
	s, err := c.inventory(id)
	if err != nil {
		// Observation loss does not bypass the authoritative final inspection.
		// Its manager fence stays sticky even if this scan can recover a charge.
		bytes, ready, scanErr := c.scanFinalJob(id, plan)
		return bytes, ready, errors.Join(err, scanErr)
	}
	defer func() {
		if err != nil {
			s.failure = err
		}
	}()
	if s.bound && s.plan != plan {
		return 0, false, fmt.Errorf("%w: cache plan changed", ErrCacheUnsafe)
	}
	current, err := cacheOpenDirectoryAt(c.dir, id)
	if err != nil {
		return 0, false, err
	}
	err = s.checkDirectory(current)
	if err != nil {
		_ = current.Close()
		return 0, false, err
	}
	if s.directory == nil {
		s.directory = current
		defer func() { _ = current.Close(); s.directory = nil }()
	} else {
		_ = current.Close()
	}
	s.bound, s.plan = true, plan
	// Ignore ordinary pre-final dirty hints only after a complete inspection;
	// malformed/lost history remains a failure of this observer generation.
	for attempt := 0; !s.fallback && !s.frozen && attempt < maxJobFiles; attempt++ {
		observed, readErr := s.readEvent()
		if readErr != nil {
			s.failure = readErr
			_, _, scanErr := c.scanFinalJob(id, plan)
			return 0, false, errors.Join(readErr, scanErr)
		}
		if !observed {
			break
		}
		if attempt == maxJobFiles-1 {
			return 0, false, fmt.Errorf("%w: final cache history did not quiesce", ErrCacheUnsafe)
		}
	}
	beforeInfo, err := s.directory.Stat()
	if err != nil {
		return 0, false, err
	}
	before, ok := beforeInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false, ErrCacheUnsafe
	}
	mode := cacheInspectHLS
	if plan.OutputMode == "progressive" {
		mode = cacheInspectProgressive
	}
	files, bytes, ready, err := cacheInspectJobMode(s.directory, false, mode)
	if err != nil {
		return bytes, ready, err
	}
	if mode == cacheInspectHLS {
		ready = cacheHLSReady(files, plan)
	}
	if s.frozen {
		if len(files) != len(s.files) {
			return bytes, ready, fmt.Errorf("%w: completed membership changed", ErrCacheUnsafe)
		}
		for _, snapshot := range files {
			fact, factErr := cacheFact(snapshot.info, snapshot.name)
			if factErr != nil || !s.files[snapshot.name].same(&fact) {
				return bytes, ready, errors.Join(ErrCacheUnsafe, factErr)
			}
		}
		s.auditAt, s.membershipAt = time.Now(), time.Now()
		s.audit = nil
		s.membershipActive, s.membershipOffset, s.membershipStart, s.membershipEnd = false, 0, 0, 0
		return bytes, ready, nil
	}
	// Replace facts while preserving the root-wide allocation bound.
	c.jobsMu.Lock()
	c.maintenanceFacts -= len(s.files)
	c.jobsMu.Unlock()
	s.frozen = false
	s.files, s.inodes, s.first, s.audit = make(map[string]*cacheFileFact, len(files)), make(map[cacheInode]*cacheInodeCharge, len(files)), nil, nil
	s.counts, s.bytes = [5]cacheRenditionCount{}, 0
	for _, snapshot := range files {
		fact, factErr := cacheFact(snapshot.info, snapshot.name)
		if factErr != nil || fact.links == 0 {
			return bytes, ready, errors.Join(ErrCacheUnsafe, factErr)
		}
		if err = c.replaceFact(s, snapshot.name, &fact); err != nil {
			return bytes, ready, err
		}
	}
	c.jobsMu.Lock()
	c.maintenanceDirty -= len(s.dirty)
	c.jobsMu.Unlock()
	s.dirty = make(map[string]struct{})
	s.membershipActive, s.membershipOffset, s.membershipStart, s.membershipEnd = false, 0, 0, 0
	afterDirectory, err := cacheOpenDirectoryAt(c.dir, id)
	if err != nil {
		return bytes, ready, err
	}
	afterInfo, err := afterDirectory.Stat()
	_ = afterDirectory.Close()
	if err != nil {
		return bytes, ready, err
	}
	after, ok := afterInfo.Sys().(*syscall.Stat_t)
	if !ok || uint64(after.Dev) != s.device || after.Ino != s.inode || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return bytes, ready, fmt.Errorf("%w: final cache namespace changed during inspection", ErrCacheUnsafe)
	}
	// Retain the pre-scan namespace boundary, rather than adopting timestamps
	// belonging to a change that happened after the membership scan.
	s.directoryModified, s.directoryChanged = before.Mtim, before.Ctim
	s.frozen, s.seeded, s.auditAt, s.membershipAt = true, true, time.Now(), time.Now()
	queued, err := s.queued()
	if err != nil || queued {
		return bytes, ready, errors.Join(ErrCacheUnsafe, err)
	}
	// Only active/finalizing producers retain watcher/directory descriptors.
	// Frozen caches retain facts and resumable cookies, not kernel instances.
	if err = s.close(); err != nil {
		return bytes, ready, err
	}
	return bytes, ready, nil
}

func (s *cacheInventory) checkDirectory(directory *os.File) error {
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(stat.Dev) != s.device || stat.Ino != s.inode {
		return fmt.Errorf("%w: job directory identity changed", ErrCacheUnsafe)
	}
	if s.frozen && (stat.Mtim != s.directoryModified || stat.Ctim != s.directoryChanged) {
		return fmt.Errorf("%w: completed cache namespace changed", ErrCacheUnsafe)
	}
	return nil
}

func (c *cacheRoot) scanFinalJob(id string, plan Plan) (int64, bool, error) {
	directory, err := cacheOpenDirectoryAt(c.dir, id)
	if err != nil {
		return 0, false, err
	}
	defer directory.Close()
	mode := cacheInspectHLS
	if plan.OutputMode == "progressive" {
		mode = cacheInspectProgressive
	}
	files, bytes, ready, err := cacheInspectJobMode(directory, false, mode)
	if err == nil && mode == cacheInspectHLS {
		ready = cacheHLSReady(files, plan)
	}
	return bytes, ready, err
}
