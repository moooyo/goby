package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

const maxAdditionalParts = 16

var additionalPartName = regexp.MustCompile(`(?i)^(.+?)[ ._-]+(part|cd)[ ._-]*([1-9][0-9]?)$`)

type additionalPartIdentity struct {
	directory string
	stem      string
	kind      string
	index     int
}

func parseAdditionalPart(relative string) (additionalPartIdentity, bool) {
	if !validMediaSourceRelativePath(relative) || path.Clean(relative) != relative {
		return additionalPartIdentity{}, false
	}
	name := path.Base(relative)
	name = strings.TrimSuffix(name, path.Ext(name))
	match := additionalPartName.FindStringSubmatch(name)
	if len(match) != 4 {
		return additionalPartIdentity{}, false
	}
	index, err := strconv.Atoi(match[3])
	stem := strings.TrimRight(match[1], " ._-")
	if err != nil || index < 1 || index > maxAdditionalParts || strings.TrimSpace(stem) == "" {
		return additionalPartIdentity{}, false
	}
	return additionalPartIdentity{directory: path.Dir(relative), stem: stem, kind: strings.ToLower(match[2]), index: index}, true
}

// AdditionalParts recognizes only explicit contiguous part/cd names in one
// indexed directory/root. It returns later parts in numeric order; mixed naming,
// duplicate ordinals or conflicting provider identities never create a stack.
// Each source is safely reopened without reading its media body before return.
func (s *Store) AdditionalParts(ctx context.Context, subject Subject, itemID string) (ItemResult, error) {
	result := ItemResult{Items: make([]Item, 0)}
	if !metadataIdentifier(itemID) {
		return result, ErrInvalidInput
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return result, err
	}
	defer rollback(tx)
	var rootID, relative string
	seed, err := scanItem(tx.QueryRow(ctx, "SELECT "+access.itemColumnsSQL()+`,i.root_id,i.relative_path FROM items i
		WHERE i.id=$1 AND NOT i.is_folder AND i.type IN ('Movie','Episode','Video') AND i.root_id IS NOT NULL
		AND `+access.ordinarySQL("i")+` AND NOT EXISTS (SELECT 1 FROM media_deletion_operations operation
		WHERE operation.item_id=i.id AND operation.kind='media')`, itemID), &rootID, &relative)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	identity, recognized := parseAdditionalPart(relative)
	if !recognized {
		if err := tx.Commit(ctx); err != nil {
			return result, err
		}
		return result, nil
	}
	// The literal prefix only narrows a bounded candidate read. Exact directory,
	// case-sensitive stem and marker-kind equality are checked below.
	// This Go raw SQL string needs two backslashes in E'\\' so PostgreSQL
	// receives one escape character, independent of standard_conforming_strings.
	prefix := identity.stem
	if identity.directory != "." {
		prefix = identity.directory + "/" + prefix
	}
	rows, err := tx.Query(ctx, "SELECT "+access.itemColumnsSQL()+`,i.relative_path FROM items i
		WHERE i.root_id=$1 AND i.library_id=$2 AND i.parent_id IS NOT DISTINCT FROM NULLIF($3::text,'')
		AND i.type=$4 AND NOT i.is_folder AND i.relative_path LIKE $5 ESCAPE E'\\'
		AND `+access.ordinarySQL("i")+` AND NOT EXISTS (SELECT 1 FROM media_deletion_operations operation
		WHERE operation.item_id=i.id AND operation.kind='media') ORDER BY i.relative_path,i.id LIMIT 65`,
		rootID, seed.LibraryID, seed.ParentID, seed.Type, escapeLikeLiteral(prefix)+"%")
	if err != nil {
		return result, err
	}
	defer rows.Close()
	parts := make(map[int]Item)
	seen := 0
	for rows.Next() {
		var candidatePath string
		item, err := scanItem(rows, &candidatePath)
		if err != nil {
			return result, err
		}
		seen++
		if seen > 64 {
			return result, ErrInvalidInput
		}
		candidate, ok := parseAdditionalPart(candidatePath)
		if !ok || candidate.directory != identity.directory || candidate.stem != identity.stem || candidate.kind != identity.kind {
			continue
		}
		if _, duplicate := parts[candidate.index]; duplicate {
			return result, fmt.Errorf("%w: ambiguous additional-part ordinal", ErrInvalidInput)
		}
		if !compatiblePartProviders(seed, item) {
			return result, fmt.Errorf("%w: conflicting additional-part provider identity", ErrInvalidInput)
		}
		for _, previous := range parts {
			if !compatiblePartProviders(previous, item) {
				return result, fmt.Errorf("%w: conflicting additional-part provider identity", ErrInvalidInput)
			}
		}
		item.CanPlay = access.canPlay
		parts[candidate.index] = item
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	rows.Close()
	// A missing/hidden middle part is not an invitation to invent an incomplete
	// continuous program. Missing and unauthorized candidates have one result.
	for index := 1; index <= len(parts); index++ {
		if _, exists := parts[index]; !exists {
			return result, tx.Commit(ctx)
		}
	}
	if len(parts) < 2 || parts[identity.index].ID != seed.ID {
		return result, tx.Commit(ctx)
	}
	indices := make([]int, 0, len(parts))
	for index := range parts {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	sources := make([]indexedMediaSource, 0, len(parts))
	for _, index := range indices {
		item := parts[index]
		snapshot, err := readIndexedMediaSource(ctx, tx, access, item.ID, "")
		if err != nil {
			return result, err
		}
		if snapshot.root.id != rootID || path.Dir(snapshot.relativePath) != identity.directory {
			return result, ErrUnavailable
		}
		if err := captureMediaPublicationRead(ctx, tx, &snapshot); err != nil {
			return result, err
		}
		sources = append(sources, snapshot)
		if index > identity.index {
			result.Items = append(result.Items, item)
		}
	}
	if err := attachUserData(ctx, tx, subject.UserID, result.Items, access); err != nil {
		return result, err
	}
	if err := attachSubtitles(ctx, tx, result.Items); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	for _, source := range sources {
		file, _, err := runMediaSourceWorker(ctx, mediaSourceWorkers, func() (*os.File, MediaFile, error) {
			opened, err := s.openPublicMediaSource(ctx, source)
			return opened, source.mediaFile, err
		})
		if err != nil {
			return ItemResult{}, err
		}
		if err := file.Close(); err != nil {
			return ItemResult{}, err
		}
	}
	result.TotalRecordCount = len(result.Items)
	return result, nil
}

func compatiblePartProviders(left, right Item) bool {
	if left.Metadata == nil || right.Metadata == nil {
		return true
	}
	for key, value := range left.Metadata.ProviderIDs {
		for otherKey, other := range right.Metadata.ProviderIDs {
			if strings.EqualFold(key, otherKey) && value != "" && other != "" && other != value {
				return false
			}
		}
	}
	return true
}
