package library

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

const (
	maxActiveSubtitles     = 32
	maxSubtitleIdentities  = 4096
	maxSubtitleStreamIndex = 1<<31 - 1
)

// Subtitle is an indexed, validated external text track. Its index shares the
// media stream namespace, while the source file snapshot is stored separately.
type Subtitle struct {
	Index                                           int
	Codec, Language, Title, MIMEType, Tag, Filename string
	IsDefault, IsForced, IsHearingImpaired          bool
	Size                                            int64
	ModifiedAt                                      time.Time
}

// SubtitleContent contains a validated source, before output conversion.
type SubtitleContent struct {
	Data []byte
	Info Subtitle
	// ModifiedAt includes filesystem change time for conditional HTTP responses.
	ModifiedAt time.Time
}

type storedSubtitle struct {
	Subtitle
	relativePath, identity, rootID string
	changeTimeNs                   int64
}

const subtitleColumns = `s.stream_index, s.codec, s.language, s.title, s.mime_type,
	s.source_hash, s.is_default, s.is_forced, s.is_hearing_impaired,
	s.file_size, s.modified_at, s.relative_path, s.file_identity, s.change_time_ns, s.root_id`

func scanStoredSubtitle(row rowScanner, additional ...any) (storedSubtitle, error) {
	var source storedSubtitle
	destinations := []any{&source.Index, &source.Codec, &source.Language, &source.Title,
		&source.MIMEType, &source.Tag, &source.IsDefault, &source.IsForced, &source.IsHearingImpaired,
		&source.Size, &source.ModifiedAt, &source.relativePath, &source.identity, &source.changeTimeNs, &source.rootID}
	if err := row.Scan(append(destinations, additional...)...); err != nil {
		return storedSubtitle{}, err
	}
	source.ModifiedAt = source.ModifiedAt.UTC()
	source.Filename = filepath.Base(filepath.FromSlash(source.relativePath))
	return source, nil
}

// attachSubtitles uses the caller's authorized transaction and the same item
// snapshot as the surrounding projection. No filesystem work is done here.
func attachSubtitles(ctx context.Context, tx pgx.Tx, items []Item) error {
	ids := make([]string, 0, len(items))
	positions := make(map[string][]int, len(items))
	for index := range items {
		items[index].Subtitles = []Subtitle{}
		if items[index].IsFolder || items[index].Media == nil {
			continue
		}
		if _, exists := positions[items[index].ID]; !exists {
			ids = append(ids, items[index].ID)
		}
		positions[items[index].ID] = append(positions[items[index].ID], index)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, "SELECT "+subtitleColumns+`, s.item_id FROM item_subtitles s
		JOIN items i ON i.id = s.item_id AND i.root_id = s.root_id
		JOIN library_roots r ON r.id = i.root_id AND r.library_id = i.library_id
		WHERE s.item_id = ANY($1::text[]) AND s.active
		ORDER BY s.item_id, s.stream_index`, ids)
	if err != nil {
		return fmt.Errorf("read item subtitles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var itemID string
		source, err := scanStoredSubtitle(rows, &itemID)
		if err != nil {
			return fmt.Errorf("decode item subtitle: %w", err)
		}
		for _, index := range positions[itemID] {
			// Reprobed embedded streams invalidate colliding external indexes even
			// if a following sidecar scan failed before it could reallocate them.
			if source.Index > highestEmbeddedStreamIndex(items[index].Media) && len(items[index].Subtitles) < maxActiveSubtitles {
				items[index].Subtitles = append(items[index].Subtitles, source.Subtitle)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read item subtitle rows: %w", err)
	}
	return nil
}

func highestEmbeddedStreamIndex(info *media.Info) int {
	highest := -1
	if info != nil {
		for _, stream := range info.Streams {
			if stream.Index > highest {
				highest = stream.Index
			}
		}
	}
	return highest
}

func subtitleMIME(codec string) string {
	switch strings.ToLower(codec) {
	case "srt":
		return "application/x-subrip"
	case "vtt":
		return "text/vtt"
	default:
		return ""
	}
}
