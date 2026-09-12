package library

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type rootMountInfo struct {
	ID, ParentID            int
	Major, Minor            uint32
	Root, MountPoint        string
	FilesystemType, Source  string
	Options, OptionalFields []string
	SuperOptions            []string
}

type rootMountPlan struct {
	Anchor, Root rootMountInfo
	Nested       []rootMountInfo
	Records      []rootMountInfo
}

// The kernel emits complete LF-terminated records with single ASCII spaces
// between fields. Reject truncation or noncanonical framing instead of turning
// a partial namespace observation into an apparently complete mount plan.
func parseRootMountInfo(raw []byte) ([]rootMountInfo, error) {
	if len(raw) > MaxRootMountInfoBytes {
		return nil, ErrRootTopologyLimit
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return nil, ErrInvalidRootTopology
	}
	entries := make([]rootMountInfo, 0, 64)
	seen := make(map[int]bool)
	for len(raw) != 0 {
		end := bytes.IndexByte(raw, '\n')
		if end < 0 || end == 0 {
			return nil, ErrInvalidRootTopology
		}
		if end > MaxRootMountInfoLineBytes || len(entries) >= MaxRootMountInfoEntries {
			return nil, ErrRootTopologyLimit
		}
		entry, err := parseRootMountInfoLine(raw[:end])
		if err != nil {
			return nil, fmt.Errorf("mountinfo record %d: %w", len(entries)+1, err)
		}
		if seen[entry.ID] {
			return nil, ErrInvalidRootTopology
		}
		seen[entry.ID] = true
		entries = append(entries, entry)
		raw = raw[end+1:]
	}
	return entries, nil
}

func parseRootMountInfoLine(line []byte) (rootMountInfo, error) {
	var entry rootMountInfo
	if bytes.IndexByte(line, '\t') >= 0 || bytes.IndexByte(line, 0) >= 0 {
		return entry, ErrInvalidRootTopology
	}
	fields := strings.Split(string(line), " ")
	if len(fields) < 10 {
		return entry, ErrInvalidRootTopology
	}
	for _, field := range fields {
		if field == "" {
			return entry, ErrInvalidRootTopology
		}
	}
	separator := -1
	for index := 6; index < len(fields); index++ {
		if fields[index] == "-" {
			separator = index
			break
		}
	}
	if separator < 6 || len(fields) != separator+4 {
		return entry, ErrInvalidRootTopology
	}
	id, valid := rootMountNumber(fields[0], 31, true)
	parent, parentValid := rootMountNumber(fields[1], 31, true)
	majorText, minorText, deviceValid := strings.Cut(fields[2], ":")
	major, majorValid := rootMountNumber(majorText, 32, false)
	minor, minorValid := rootMountNumber(minorText, 32, false)
	if !valid || !parentValid || !deviceValid || !majorValid || !minorValid {
		return entry, ErrInvalidRootTopology
	}
	entry.ID, entry.ParentID, entry.Major, entry.Minor = int(id), int(parent), uint32(major), uint32(minor)
	var err error
	if entry.Root, err = decodeRootMountField(fields[3]); err != nil {
		return rootMountInfo{}, err
	}
	if entry.MountPoint, err = decodeRootMountField(fields[4]); err != nil {
		return rootMountInfo{}, err
	}
	if entry.Source, err = decodeRootMountField(fields[separator+2]); err != nil {
		return rootMountInfo{}, err
	}
	entry.FilesystemType = fields[separator+1]
	entry.Options = strings.Split(fields[5], ",")
	entry.OptionalFields = append([]string{}, fields[6:separator]...)
	entry.SuperOptions = strings.Split(fields[separator+3], ",")
	if _, err := validateRootMountInfo(entry); err != nil {
		return rootMountInfo{}, err
	}
	return entry, nil
}

func rootMountNumber(value string, bits int, positive bool) (uint64, bool) {
	if value == "" {
		return 0, false
	}
	number, err := strconv.ParseUint(value, 10, bits)
	return number, err == nil && (!positive || number != 0) && strconv.FormatUint(number, 10) == value
}

// Decode only the four octal escapes defined for mountinfo path/source fields.
// Decoding is deliberately single-pass: an escaped backslash followed by 040
// represents the literal suffix, not another encoded space.
func decodeRootMountField(value string) (string, error) {
	if len(value) > MaxRootMountInfoLineBytes {
		return "", ErrRootTopologyLimit
	}
	decoded := make([]byte, 0, min(len(value), maxRootTopologyPathBytes))
	for index := 0; index < len(value); index++ {
		current := value[index]
		if current == '\\' {
			if index+3 >= len(value) {
				return "", ErrInvalidRootTopology
			}
			switch value[index+1 : index+4] {
			case "040":
				current = ' '
			case "011":
				current = '\t'
			case "012":
				current = '\n'
			case "134":
				current = '\\'
			default:
				return "", ErrInvalidRootTopology
			}
			index += 3
		}
		if len(decoded) == maxRootTopologyPathBytes {
			return "", ErrRootTopologyLimit
		}
		decoded = append(decoded, current)
	}
	if len(decoded) == 0 || bytes.IndexByte(decoded, 0) >= 0 || !utf8.Valid(decoded) {
		return "", ErrInvalidRootTopology
	}
	return string(decoded), nil
}

// Known optional fields have their documented numeric/bare syntax. Future
// fields are retained verbatim and cannot silently lose meaning. A scoped plan
// rejects unknown fields only when their record is relevant to that plan.
func validateRootMountOptional(fields []string) (bool, error) {
	known := true
	seen := make(map[string]bool)
	for _, field := range fields {
		if !validRootMountToken(field) {
			return false, ErrInvalidRootTopology
		}
		name, value, argument := strings.Cut(field, ":")
		if name == "" || argument && value == "" {
			return false, ErrInvalidRootTopology
		}
		switch name {
		case "shared", "master", "propagate_from":
			if _, valid := rootMountNumber(value, 31, true); !argument || !valid || seen[name] {
				return false, ErrInvalidRootTopology
			}
			seen[name] = true
		case "unbindable":
			if argument || seen[name] {
				return false, ErrInvalidRootTopology
			}
			seen[name] = true
		default:
			known = false
		}
	}
	return known, nil
}

func validRootMountToken(value string) bool {
	return value != "" && utf8.ValidString(value) && !strings.ContainsRune(value, ' ') &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

// A Linux nsfs bind mount exposes its namespace dentry as e.g. net:[4026532415]
// in mountinfo's root field. This is valid metadata, not a directory pathname.
// Recognize only the known namespace names and a canonical nonzero inode; an
// unrelated filesystem must still supply a canonical absolute root path.
func validRootMountFilesystemRoot(root, filesystem string) bool {
	if validRootTopologyAbsolute(root) {
		return true
	}
	if filesystem != "nsfs" || !strings.HasSuffix(root, "]") {
		return false
	}
	name, inode, found := strings.Cut(strings.TrimSuffix(root, "]"), ":[")
	if !found {
		return false
	}
	switch name {
	case "mnt", "net", "uts", "ipc", "pid", "user", "cgroup", "time":
		_, valid := rootMountNumber(inode, 64, true)
		return valid
	default:
		return false
	}
}

// Plan also accepts parsed records directly. Bound their decoded content and
// reject impossible hand-built records before following any parent links.
func validateRootMountInfo(entry rootMountInfo) (int, error) {
	if entry.ID <= 0 || int64(entry.ID) > 2147483647 || entry.ParentID <= 0 || int64(entry.ParentID) > 2147483647 {
		return 0, ErrInvalidRootTopology
	}
	if len(entry.Root) > maxRootTopologyPathBytes || len(entry.MountPoint) > maxRootTopologyPathBytes || len(entry.Source) > maxRootTopologyPathBytes {
		return 0, ErrRootTopologyLimit
	}
	if !validRootMountFilesystemRoot(entry.Root, entry.FilesystemType) || !validRootTopologyAbsolute(entry.MountPoint) ||
		entry.Source == "" || !utf8.ValidString(entry.Source) || strings.ContainsRune(entry.Source, 0) || !validRootMountToken(entry.FilesystemType) ||
		len(entry.Options) == 0 || len(entry.SuperOptions) == 0 {
		return 0, ErrInvalidRootTopology
	}
	size := len(entry.Root) + len(entry.MountPoint) + len(entry.Source) + len(entry.FilesystemType)
	if size > MaxRootMountInfoLineBytes {
		return 0, ErrRootTopologyLimit
	}
	for index, fields := range [][]string{entry.Options, entry.OptionalFields, entry.SuperOptions} {
		if len(fields) > MaxRootMountInfoLineBytes {
			return 0, ErrRootTopologyLimit
		}
		for _, field := range fields {
			if len(field) > MaxRootMountInfoLineBytes-size {
				return 0, ErrRootTopologyLimit
			}
			size += len(field)
			if !validRootMountToken(field) || index != 1 && strings.ContainsRune(field, ',') {
				return 0, ErrInvalidRootTopology
			}
		}
	}
	if _, err := validateRootMountOptional(entry.OptionalFields); err != nil {
		return 0, err
	}
	return size, nil
}

func planRootMounts(entries []rootMountInfo, mapping RootTopologyMapping, anchorID, rootID int) (rootMountPlan, error) {
	var result rootMountPlan
	if _, err := rootTopologyRelative(mapping); err != nil || anchorID <= 0 || rootID <= 0 || len(entries) == 0 {
		return result, ErrInvalidRootTopology
	}
	if len(entries) > MaxRootMountInfoEntries {
		return result, ErrRootTopologyLimit
	}
	byID := make(map[int]rootMountInfo, len(entries))
	remaining := MaxRootMountInfoBytes
	for _, entry := range entries {
		size, err := validateRootMountInfo(entry)
		if err != nil {
			return result, err
		}
		if size > remaining {
			return result, ErrRootTopologyLimit
		}
		remaining -= size
		if _, duplicate := byID[entry.ID]; duplicate {
			return result, ErrInvalidRootTopology
		}
		byID[entry.ID] = entry
	}
	anchor, anchorPresent := byID[anchorID]
	root, rootPresent := byID[rootID]
	if !anchorPresent || !rootPresent || !rootTopologyWithin(anchor.MountPoint, mapping.ApprovedPath) ||
		!rootTopologyWithin(root.MountPoint, mapping.RegisteredPath) {
		return result, ErrRootTopologyChanged
	}
	chain, err := rootMountChain(byID, rootID, anchorID)
	if err != nil {
		return result, err
	}
	relevant := make(map[int]rootMountInfo, len(chain))
	rootAncestors := make(map[int]bool, len(chain))
	for _, entry := range chain {
		if entry.ID != anchorID && (!rootTopologyWithin(mapping.ApprovedPath, entry.MountPoint) || !rootTopologyWithin(entry.MountPoint, mapping.RegisteredPath)) {
			return result, ErrRootTopologyChanged
		}
		relevant[entry.ID] = entry
		rootAncestors[entry.ID] = true
	}
	nested := make([]rootMountInfo, 0)
	for _, entry := range entries {
		if entry.ID == anchorID || entry.ID == rootID {
			continue
		}
		corridor := rootTopologyWithin(mapping.ApprovedPath, entry.MountPoint) && rootTopologyWithin(entry.MountPoint, mapping.RegisteredPath)
		inside := rootTopologyWithin(mapping.RegisteredPath, entry.MountPoint)
		if !corridor && !inside {
			continue
		}
		if corridor {
			if !rootAncestors[entry.ID] {
				return result, ErrRootTopologyAmbiguous
			}
		}
		if !inside {
			continue
		}
		if len(nested) >= MaxRootTopologyBoundaries {
			return result, ErrRootTopologyLimit
		}
		parents, err := rootMountChain(byID, entry.ID, rootID)
		if err != nil {
			return result, err
		}
		for _, parent := range parents {
			relevant[parent.ID] = parent
		}
		nested = append(nested, entry)
	}
	ordered := make([]rootMountInfo, 0, len(relevant))
	for _, entry := range relevant {
		// An external namespace bind mount may be excluded, but a namespace
		// dentry cannot stand in for any directory that this capture must hold.
		if !validRootTopologyAbsolute(entry.Root) {
			return result, ErrRootTopologyAmbiguous
		}
		known, err := validateRootMountOptional(entry.OptionalFields)
		if err != nil {
			return result, err
		}
		if !known {
			return result, ErrRootTopologyAmbiguous
		}
		ordered = append(ordered, entry)
	}
	// A separator-terminated key keeps each path's descendants contiguous.
	// Plain lexical order would place /a-other between /a and /a/child and
	// could discard the covering /a record before checking its hidden child.
	sort.Slice(ordered, func(first, second int) bool {
		left := strings.TrimSuffix(ordered[first].MountPoint, "/") + "/"
		right := strings.TrimSuffix(ordered[second].MountPoint, "/") + "/"
		if left == right {
			return ordered[first].ID < ordered[second].ID
		}
		return left < right
	})
	covering := make([]rootMountInfo, 0, len(ordered))
	for _, entry := range ordered {
		for len(covering) != 0 && !rootTopologyWithin(covering[len(covering)-1].MountPoint, entry.MountPoint) {
			covering = covering[:len(covering)-1]
		}
		if len(covering) != 0 {
			parent := covering[len(covering)-1]
			if parent.MountPoint == entry.MountPoint || !rootMountHasAncestor(byID, entry.ID, parent.ID) {
				return result, ErrRootTopologyAmbiguous
			}
		}
		covering = append(covering, entry)
	}
	sortRootMountPoints(nested)
	sort.Slice(ordered, func(first, second int) bool { return ordered[first].ID < ordered[second].ID })
	result.Anchor, result.Root = cloneRootMountInfo(anchor), cloneRootMountInfo(root)
	result.Nested = make([]rootMountInfo, len(nested))
	result.Records = make([]rootMountInfo, len(ordered))
	for index, entry := range nested {
		result.Nested[index] = cloneRootMountInfo(entry)
	}
	for index, entry := range ordered {
		result.Records[index] = cloneRootMountInfo(entry)
	}
	return result, nil
}

func rootMountChain(entries map[int]rootMountInfo, start, ancestor int) ([]rootMountInfo, error) {
	chain := make([]rootMountInfo, 0, 8)
	seen := make(map[int]bool)
	for current := start; ; {
		if seen[current] {
			return nil, ErrRootTopologyAmbiguous
		}
		entry, present := entries[current]
		if !present {
			return nil, ErrRootTopologyChanged
		}
		seen[current] = true
		chain = append(chain, entry)
		if current == ancestor {
			return chain, nil
		}
		parent, present := entries[entry.ParentID]
		if !present || !rootTopologyWithin(parent.MountPoint, entry.MountPoint) {
			return nil, ErrRootTopologyChanged
		}
		current = parent.ID
	}
}

func rootMountHasAncestor(entries map[int]rootMountInfo, start, ancestor int) bool {
	for count, current := 0, start; count < len(entries); count++ {
		if current == ancestor {
			return true
		}
		entry, present := entries[current]
		if !present || entry.ParentID == current {
			return false
		}
		current = entry.ParentID
	}
	return false
}

func sortRootMountPoints(entries []rootMountInfo) {
	sort.Slice(entries, func(first, second int) bool {
		if entries[first].MountPoint == entries[second].MountPoint {
			return entries[first].ID < entries[second].ID
		}
		return entries[first].MountPoint < entries[second].MountPoint
	})
}

func cloneRootMountInfo(entry rootMountInfo) rootMountInfo {
	entry.Root = strings.Clone(entry.Root)
	entry.MountPoint = strings.Clone(entry.MountPoint)
	entry.FilesystemType = strings.Clone(entry.FilesystemType)
	entry.Source = strings.Clone(entry.Source)
	entry.Options = append([]string{}, entry.Options...)
	entry.OptionalFields = append([]string{}, entry.OptionalFields...)
	entry.SuperOptions = append([]string{}, entry.SuperOptions...)
	return entry
}
