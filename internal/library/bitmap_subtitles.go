package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

// BitmapSubtitleComponent binds one regular source-side file. Names are local
// basenames only; API projections must never expose private filesystem facts.
type BitmapSubtitleComponent struct {
	Name, Identity, SHA256         string
	Size, ModifiedNS, ChangeTimeNS int64
}

// BitmapSubtitle belongs to the shared public stream namespace, but remains
// separate from text subtitle delivery and single-file management operations.
type BitmapSubtitle struct {
	Index, SourceStreamIndex                      int
	Codec, Format, Language, Title, Tag, Filename string
	IsDefault, IsForced, IsHearingImpaired        bool
	Components                                    []BitmapSubtitleComponent
}

type storedBitmapSubtitle struct {
	BitmapSubtitle
	relativePath, rootID string
}

const bitmapSubtitleColumns = `s.stream_index,s.source_stream_index,s.codec,s.format,s.language,s.title,
	s.source_hash,s.is_default,s.is_forced,s.is_hearing_impaired,s.components,s.relative_path,s.root_id`

func scanStoredBitmapSubtitle(row rowScanner, additional ...any) (storedBitmapSubtitle, error) {
	var track storedBitmapSubtitle
	var components []byte
	values := []any{&track.Index, &track.SourceStreamIndex, &track.Codec, &track.Format, &track.Language,
		&track.Title, &track.Tag, &track.IsDefault, &track.IsForced, &track.IsHearingImpaired,
		&components, &track.relativePath, &track.rootID}
	if err := row.Scan(append(values, additional...)...); err != nil {
		return storedBitmapSubtitle{}, err
	}
	if err := json.Unmarshal(components, &track.Components); err != nil {
		return storedBitmapSubtitle{}, fmt.Errorf("%w: invalid bitmap subtitle components", ErrUnavailable)
	}
	track.Filename = filepath.Base(filepath.FromSlash(track.relativePath))
	if err := ValidateBitmapSubtitle(track.BitmapSubtitle); err != nil {
		return storedBitmapSubtitle{}, err
	}
	return track, nil
}

// BitmapSubtitleSourceHash binds ordered component content and its selected
// language track. Filesystem identity and timestamps belong to the source stamp,
// so a metadata-only refresh does not emit a false catalog content notification.
func BitmapSubtitleSourceHash(format string, sourceStreamIndex int, components []BitmapSubtitleComponent) string {
	value := struct {
		Format            string
		SourceStreamIndex int
		Hashes            []string
	}{Format: format, SourceStreamIndex: sourceStreamIndex, Hashes: make([]string, len(components))}
	for index, component := range components {
		value.Hashes[index] = component.SHA256
	}
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// ValidateBitmapSubtitle validates the portable indexed shape without touching
// storage. Callers still must authorize and recheck every component descriptor.
func ValidateBitmapSubtitle(track BitmapSubtitle) error {
	bad := func() error { return fmt.Errorf("%w: invalid bitmap subtitle snapshot", ErrUnavailable) }
	if track.Index < 0 || track.Index > maxSubtitleStreamIndex || track.SourceStreamIndex < 0 || track.SourceStreamIndex > 31 ||
		!validOwnedSubtitleMetadata(track.Language, track.Title) || !safeSubtitleFilename(track.Filename) {
		return bad()
	}
	switch track.Format {
	case "sup":
		if track.Codec != "hdmv_pgs_subtitle" || track.SourceStreamIndex != 0 || len(track.Components) != 1 || !strings.EqualFold(filepath.Ext(track.Filename), ".sup") {
			return bad()
		}
	case "vobsub":
		if track.Codec != "dvd_subtitle" || len(track.Components) != 2 || !strings.EqualFold(filepath.Ext(track.Filename), ".idx") {
			return bad()
		}
		companion := track.Components[1].Name
		if !strings.EqualFold(filepath.Ext(companion), ".sub") ||
			!strings.EqualFold(strings.TrimSuffix(track.Filename, filepath.Ext(track.Filename)), strings.TrimSuffix(companion, filepath.Ext(companion))) {
			return bad()
		}
	default:
		return bad()
	}
	if track.Components[0].Name != track.Filename {
		return bad()
	}
	var bytes int64
	identities := make(map[string]bool, len(track.Components))
	for _, component := range track.Components {
		if !safeSubtitleFilename(component.Name) || component.Name == "." || component.Name == ".." ||
			component.Identity == "" || len(component.Identity) > 512 || strings.ContainsRune(component.Identity, 0) ||
			component.Size <= 0 || component.Size > media.MaxExternalBitmapSubtitleBytes-bytes || component.ModifiedNS <= 0 || component.ChangeTimeNS <= 0 ||
			len(component.SHA256) != 64 || strings.ToLower(component.SHA256) != component.SHA256 {
			return bad()
		}
		if identities[component.Identity] {
			return bad()
		}
		identities[component.Identity] = true
		bytes += component.Size
		if digest, err := hex.DecodeString(component.SHA256); err != nil || len(digest) != sha256.Size {
			return bad()
		}
	}
	if track.Tag != BitmapSubtitleSourceHash(track.Format, track.SourceStreamIndex, track.Components) {
		return bad()
	}
	return nil
}

func validateBitmapSubtitleSnapshot(primary indexedMediaSource, source storedBitmapSubtitle) error {
	if err := ValidateBitmapSubtitle(source.BitmapSubtitle); err != nil {
		return err
	}
	path := filepath.FromSlash(source.relativePath)
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(primary.relativePath), filepath.Ext(primary.relativePath)))
	stem := strings.ToLower(strings.TrimSuffix(source.Filename, filepath.Ext(source.Filename)))
	if !validMediaSourceRelativePath(path) || filepath.Clean(path) != path || filepath.Base(path) != source.Filename ||
		filepath.Dir(path) != filepath.Dir(filepath.FromSlash(primary.relativePath)) || source.rootID != primary.root.id ||
		(stem != base && !strings.HasPrefix(stem, base+".")) {
		return fmt.Errorf("%w: invalid bitmap subtitle source binding", ErrUnavailable)
	}
	if _, ok := subtitleNameMetadata(source.Filename, stem[len(base):], source.Format); !ok {
		return fmt.Errorf("%w: invalid bitmap subtitle source name", ErrUnavailable)
	}
	return nil
}

// readBitmapSubtitleRows uses its caller's authorized transaction. Empty rootID
// includes prior roots for the scanner's locked retirement pass only.
func readBitmapSubtitleRows(ctx context.Context, tx pgx.Tx, itemID, rootID string) ([]storedBitmapSubtitle, error) {
	rows, err := tx.Query(ctx, `SELECT `+bitmapSubtitleColumns+` FROM item_bitmap_subtitles s
		WHERE s.item_id=$1 AND ($2='' OR s.root_id=$2) AND s.active ORDER BY s.stream_index`, itemID, rootID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]storedBitmapSubtitle, 0)
	for rows.Next() {
		track, err := scanStoredBitmapSubtitle(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, track)
		if len(result) > maxActiveSubtitles {
			return nil, fmt.Errorf("%w: excessive bitmap subtitle inventory", ErrUnavailable)
		}
	}
	return result, rows.Err()
}

func attachBitmapSubtitles(ctx context.Context, tx pgx.Tx, items []Item, ids []string, positions map[string][]int) error {
	rows, err := tx.Query(ctx, `SELECT `+bitmapSubtitleColumns+`,s.item_id FROM item_bitmap_subtitles s
		JOIN items i ON i.id=s.item_id AND i.root_id=s.root_id
		JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE s.item_id=ANY($1::text[]) AND s.active ORDER BY s.item_id,s.stream_index`, ids)
	if err != nil {
		return fmt.Errorf("read bitmap item subtitles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var itemID string
		track, err := scanStoredBitmapSubtitle(rows, &itemID)
		if err != nil {
			return err
		}
		for _, position := range positions[itemID] {
			facts := track
			facts.Components = append([]BitmapSubtitleComponent(nil), track.Components...)
			items[position].bitmapSubtitleFacts = append(items[position].bitmapSubtitleFacts, facts)
			if track.Index > highestEmbeddedStreamIndex(items[position].Media) {
				copy := track.BitmapSubtitle
				copy.Components = append([]BitmapSubtitleComponent(nil), track.Components...)
				items[position].BitmapSubtitles = append(items[position].BitmapSubtitles, copy)
			}
		}
	}
	return rows.Err()
}

// trimSubtitleProjection keeps a single bounded public namespace even when old
// catalog state predates a tighter limit. Allocation itself enforces the limit.
func trimSubtitleProjection(item *Item) {
	indexes := make([]int, 0, len(item.Subtitles)+len(item.BitmapSubtitles))
	for _, track := range item.Subtitles {
		indexes = append(indexes, track.Index)
	}
	for _, track := range item.BitmapSubtitles {
		indexes = append(indexes, track.Index)
	}
	if len(indexes) <= maxActiveSubtitles {
		return
	}
	sort.Ints(indexes)
	last := indexes[maxActiveSubtitles-1]
	item.Subtitles = item.Subtitles[:sort.Search(len(item.Subtitles), func(index int) bool { return item.Subtitles[index].Index > last })]
	item.BitmapSubtitles = item.BitmapSubtitles[:sort.Search(len(item.BitmapSubtitles), func(index int) bool { return item.BitmapSubtitles[index].Index > last })]
}
