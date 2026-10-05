package library

import (
	"context"
	"encoding/json"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

// ResolveBackgroundPreviewJobInterval optionally upgrades the fallback to an
// effective automatic intro after independently checking supporting files. The
// sidecar worker calls this only after ruling out reuse of an existing artifact.
func (s *Store) ResolveBackgroundPreviewJobInterval(ctx context.Context, fence AnalysisFence, job BackgroundPreviewJob) (BackgroundPreviewJob, error) {
	source, err := s.ValidateBackgroundPreviewJob(ctx, fence, job)
	if err != nil {
		return job, err
	}
	if job.ManualStartTicks != nil || source.ItemType != "Episode" {
		return job, nil
	}
	detection, support, credits, err := s.readBackgroundDetection(ctx, source)
	if err != nil || detection.Effective == nil || detection.Effective.Provenance != "Detected" {
		if ctx.Err() != nil {
			return job, ctx.Err()
		}
		return job, nil
	}
	if len(support) == 0 || len(support) > 32 {
		return job, nil
	}
	for _, expected := range support {
		if expected.LibraryID != job.LibraryID {
			return job, nil
		}
		if _, err := s.ValidateBackgroundPreviewJob(ctx, fence, job); err != nil {
			return job, err
		}
		file, _, err := s.runPreparedMediaSourceWorker(ctx, true, func(work context.Context) (mediaSourceRootHint, error) {
			return s.readMediaSourceRootHint(work, expected.ItemID)
		}, func(work context.Context) (*os.File, MediaFile, error) {
			snapshot, err := s.readAdmittedAnalysisSource(work, job.ChildID, expected)
			if err != nil {
				return nil, MediaFile{}, err
			}
			file, err := s.openPublicMediaSource(work, snapshot)
			return file, snapshot.mediaFile, err
		})
		if err != nil {
			if ctx.Err() != nil {
				return job, ctx.Err()
			}
			return job, nil
		}
		if err := file.Close(); err != nil {
			return job, nil
		}
	}
	current, err := s.ValidateBackgroundPreviewJob(ctx, fence, job)
	if err != nil {
		return job, err
	}
	final, _, _, err := s.readBackgroundDetection(ctx, current)
	if err != nil || final.Revision != detection.Revision || final.Effective == nil || *final.Effective != *detection.Effective {
		if ctx.Err() != nil {
			return job, ctx.Err()
		}
		return job, nil
	}
	start, duration, err := selectBackgroundPreviewInterval(source.DurationTicks, nil, final.Effective, credits, int64(job.Profile.DurationSeconds)*backgroundTicksPerSecond)
	if err == nil {
		job.StartTicks, job.DurationTicks = start, duration
	}
	return job, nil
}

func (s *Store) readBackgroundDetection(ctx context.Context, source AnalysisSource) (AnalysisDetection, []AnalysisSource, *CreditsPoint, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AnalysisDetection{}, nil, nil, err
	}
	defer rollback(tx)
	access := unrestrictedLibraryAccess()
	current, err := readAnalysisSourceUsing(ctx, tx, access, source.ItemID, false)
	if err != nil {
		return AnalysisDetection{}, nil, nil, err
	}
	if !sameAnalysisSource(source, current) {
		return AnalysisDetection{}, nil, nil, ErrAnalysisSourceChanged
	}
	detection, err := readAnalysisDetection(ctx, tx, access, current)
	if err != nil {
		return detection, nil, nil, err
	}
	if detection.Effective == nil || detection.Effective.Provenance != "Detected" {
		return detection, nil, nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT `+analysisSourceColumns+` FROM items i `+analysisSourceJoins+`
  JOIN analysis_detection_sources proof ON proof.source_item_id=i.id WHERE proof.item_id=$1 AND `+analysisPhysicalSQL+` ORDER BY i.id LIMIT 33`, source.ItemID)
	if err != nil {
		return detection, nil, nil, err
	}
	support := []AnalysisSource{}
	for rows.Next() {
		entry, err := scanAnalysisSource(rows)
		if err != nil {
			rows.Close()
			return detection, nil, nil, err
		}
		support = append(support, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return detection, nil, nil, err
	}
	var rawMedia, rawCredits []byte
	if err := tx.QueryRow(ctx, `SELECT i.media,`+itemCreditsColumn+` FROM items i WHERE i.id=$1`, source.ItemID).Scan(&rawMedia, &rawCredits); err != nil {
		return detection, nil, nil, err
	}
	var info media.Info
	if err := json.Unmarshal(rawMedia, &info); err != nil {
		return detection, nil, nil, err
	}
	item := Item{ID: source.ItemID, Type: source.ItemType, Media: &info}
	if err := projectItemCredits(&item, rawCredits); err != nil {
		return detection, nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return detection, nil, nil, err
	}
	return detection, support, item.Credits, nil
}
