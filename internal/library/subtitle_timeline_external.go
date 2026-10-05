package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"sort"

	"github.com/moooyo/goby/internal/media"
)

// Canonical facts retain every component and assigned index while excluding
// catalog IDs and directory names. A same-filesystem directory move remains
// reusable, but a different sidecar or IDX language assignment does not.
func subtitleTimelineSnapshotBitmap(snapshot indexedMediaSource) []BitmapSubtitle {
	if snapshot.mediaFile.Item.bitmapSubtitleFacts != nil {
		result := make([]BitmapSubtitle, len(snapshot.mediaFile.Item.bitmapSubtitleFacts))
		for index, track := range snapshot.mediaFile.Item.bitmapSubtitleFacts {
			result[index] = track.BitmapSubtitle
		}
		return result
	}
	return snapshot.mediaFile.Item.BitmapSubtitles
}

func validateSubtitleTimelineBitmapBinding(snapshot indexedMediaSource) error {
	for _, track := range snapshot.mediaFile.Item.bitmapSubtitleFacts {
		if err := validateBitmapSubtitleSnapshot(snapshot, track); err != nil {
			return err
		}
	}
	return nil
}

func subtitleTimelineBitmapFacts(tracks []BitmapSubtitle) ([]BitmapSubtitle, error) {
	if len(tracks) > media.MaxSubtitleTimelineTracks {
		return nil, media.ErrSubtitleTimelineUnsupported
	}
	result := slices.Clone(tracks)
	for index := range result {
		if err := ValidateBitmapSubtitle(result[index]); err != nil {
			return nil, ErrUnavailable
		}
		if result[index].Format == "vobsub" {
			components := result[index].Components
			if components[0].Identity == components[1].Identity || components[0].Size > media.MaxExternalBitmapSubtitleBytes-components[1].Size {
				return nil, ErrUnavailable
			}
		}
		result[index].Components = slices.Clone(result[index].Components)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Index < result[j].Index })
	for index := 1; index < len(result); index++ {
		if result[index-1].Index == result[index].Index {
			return nil, ErrUnavailable
		}
	}
	return result, nil
}

// The raw active catalog set is fenced under the same item lock used by
// sidecar indexing. This also covers a now-colliding or otherwise invisible
// track rather than allowing a projection filter to hide an index mutation.
func readSubtitleTimelineBitmapRevision(tx OwnedTx, itemID, rootID string) (string, error) {
	var facts string
	var count int
	if err := tx.QueryRow(`SELECT count(*),COALESCE(jsonb_agg(to_jsonb(s) ORDER BY s.stream_index),'[]'::jsonb)::text
 FROM (SELECT * FROM item_bitmap_subtitles WHERE item_id=$1 AND root_id=$2 AND active ORDER BY stream_index LIMIT $3) s`,
		itemID, rootID, maxActiveSubtitles+1).Scan(&count, &facts); err != nil {
		return "", err
	}
	if count > maxActiveSubtitles {
		return "", ErrUnavailable
	}
	if facts == "[]" {
		return "", nil
	}
	digest := sha256.Sum256([]byte(facts))
	return "subtitle-timeline-bitmap-v2-" + hex.EncodeToString(digest[:]), nil
}

func subtitleTimelineJobRevision(job SubtitleTimelineJob) string {
	if job.BitmapRevision == "" {
		return job.SourceRevision
	}
	facts, _ := json.Marshal([]string{job.SourceRevision, job.BitmapRevision})
	digest := sha256.Sum256(facts)
	return "subtitle-timeline-job-v2-" + hex.EncodeToString(digest[:])
}

type subtitleTimelineExternalFile struct {
	facts BitmapSubtitleComponent
	file  *os.File
}

type subtitleTimelineExternalFiles struct {
	read   *MediaSourceReadIO
	files  []subtitleTimelineExternalFile
	inputs []media.ExternalSubtitleTimelineInput
}

func subtitleTimelineComponentMatches(facts BitmapSubtitleComponent, info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Size() == facts.Size &&
		fileIdentity(info) == facts.Identity && info.ModTime().UnixNano() == facts.ModifiedNS &&
		media.FileChangeTime(info) == facts.ChangeTimeNS
}

// The hash is read from the held descriptor. Exact-name parent observations on
// both sides reject symlinks, companion swaps and replacement while hashing.
func recheckSubtitleTimelineComponent(ctx context.Context, parent *os.Root, source subtitleTimelineExternalFile) error {
	before, err := parent.Lstat(source.facts.Name)
	opened, statErr := source.file.Stat()
	if err != nil || statErr != nil || !subtitleTimelineComponentMatches(source.facts, before) ||
		!subtitleTimelineComponentMatches(source.facts, opened) || !sameMediaSourceFile(before, opened) {
		return ErrSourceChanged
	}
	digest := sha256.New()
	buffer := make([]byte, 64*1024)
	for offset := int64(0); offset < source.facts.Size; {
		if err := ctx.Err(); err != nil {
			return err
		}
		length := min(int64(len(buffer)), source.facts.Size-offset)
		n, err := source.file.ReadAt(buffer[:length], offset)
		if err != nil || int64(n) != length {
			return errors.Join(ErrSourceChanged, err)
		}
		_, _ = digest.Write(buffer[:n])
		offset += int64(n)
	}
	after, err := parent.Lstat(source.facts.Name)
	current, statErr := source.file.Stat()
	if err != nil || statErr != nil || !subtitleTimelineComponentMatches(source.facts, after) ||
		!subtitleTimelineComponentMatches(source.facts, current) || !sameMediaSourceFile(opened, after) ||
		!sameMediaSourceFile(opened, current) || hex.EncodeToString(digest.Sum(nil)) != source.facts.SHA256 {
		return ErrSourceChanged
	}
	return ctx.Err()
}

func (sources *subtitleTimelineExternalFiles) recheck(ctx context.Context, parent *os.Root) error {
	if len(sources.files) == 0 {
		return ctx.Err()
	}
	return sources.read.Run(ctx, func(work context.Context) error {
		for _, source := range sources.files {
			if err := recheckSubtitleTimelineComponent(work, parent, source); err != nil {
				return err
			}
		}
		return nil
	})
}

// Retirement runs before releasing the root IO capability or the enclosing
// media worker slot, including canceled and failed generations.
func (sources *subtitleTimelineExternalFiles) close() error {
	var result error
	for _, source := range sources.files {
		err := source.file.Close()
		if err != nil && !errors.Is(err, os.ErrClosed) {
			err = media.SourceReadRetirementError(err, source.file)
			if sources.read != nil {
				err = errors.Join(err, sources.read.MarkUnknown(err))
			}
			result = errors.Join(result, err)
		}
	}
	sources.files, sources.inputs = nil, nil
	if sources.read != nil {
		result = errors.Join(result, sources.read.Close())
		sources.read = nil
	}
	return result
}

func (s *Store) openSubtitleTimelineExternalFiles(ctx context.Context, parent *os.Root, snapshot indexedMediaSource) (_ *subtitleTimelineExternalFiles, resultErr error) {
	if err := validateSubtitleTimelineBitmapBinding(snapshot); err != nil {
		return nil, err
	}
	facts, err := subtitleTimelineBitmapFacts(subtitleTimelineSnapshotBitmap(snapshot))
	if err != nil {
		return nil, err
	}
	sources := &subtitleTimelineExternalFiles{}
	if len(facts) == 0 {
		return sources, nil
	}
	sources.read, err = s.PrepareMediaSourceIO(ctx, snapshot.mediaFile)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			resultErr = errors.Join(resultErr, sources.close())
		}
	}()
	err = sources.read.Run(ctx, func(work context.Context) error {
		byName := make(map[string]subtitleTimelineExternalFile)
		for _, track := range facts {
			for _, component := range track.Components {
				if held, exists := byName[component.Name]; exists {
					if held.facts != component {
						return ErrSourceChanged
					}
					continue
				}
				if err := work.Err(); err != nil {
					return err
				}
				before, err := parent.Lstat(component.Name)
				if err != nil || !subtitleTimelineComponentMatches(component, before) {
					return ErrSourceChanged
				}
				file, err := openScanFile(parent, component.Name)
				if err != nil {
					return errors.Join(ErrSourceChanged, err)
				}
				held := subtitleTimelineExternalFile{facts: component, file: file}
				sources.files = append(sources.files, held)
				byName[component.Name] = held
				if err := recheckSubtitleTimelineComponent(work, parent, held); err != nil {
					return err
				}
			}
			input := media.ExternalSubtitleTimelineInput{StreamIndex: track.Index, SourceStreamIndex: track.SourceStreamIndex,
				Codec: track.Codec, Input: byName[track.Filename].file}
			if track.Format == "vobsub" {
				input.Companion = byName[track.Components[1].Name].file
			}
			if input.Input == nil || track.Format == "vobsub" && input.Companion == nil {
				return ErrSourceChanged
			}
			sources.inputs = append(sources.inputs, input)
		}
		return work.Err()
	})
	if err != nil {
		return nil, err
	}
	ok = true
	return sources, nil
}

func (s *Store) subtitleTimelineSourceCurrent(ctx context.Context, parent *os.Root, snapshot indexedMediaSource) (bool, error) {
	current := subtitleTimelineSourceCurrent(parent, snapshot)
	sources, err := s.openSubtitleTimelineExternalFiles(ctx, parent, snapshot)
	if errors.Is(err, ErrSourceChanged) && !errors.Is(err, media.ErrProcessRetirementUnknown) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return current, sources.close()
}

func adaptSubtitleTimelineEncoder(encode SubtitleTimelineEncoder) SubtitleTimelineExternalEncoder {
	if encode == nil {
		return nil
	}
	return func(ctx context.Context, input *os.File, source MediaFile, job SubtitleTimelineJob, external []media.ExternalSubtitleTimelineInput, output io.Writer) (media.SubtitleTimelineSummary, error) {
		if len(external) != 0 {
			return media.SubtitleTimelineSummary{}, media.ErrSubtitleTimelineUnsupported
		}
		return encode(ctx, input, source, job, output)
	}
}
