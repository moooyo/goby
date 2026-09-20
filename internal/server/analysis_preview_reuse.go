package server

import (
	"context"
	"errors"
	"strconv"

	"github.com/moooyo/goby/internal/analysiscache"
	"github.com/moooyo/goby/internal/bif"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

// Reuse requires current fenced database references and every actual immutable
// width variant. A database row or cheap inventory presence alone is not enough.
func (r *mediaAnalysisRuntime) reusePreview(ctx context.Context, task tasks.Work, work library.AnalysisWork) (reused bool, resultErr error) {
	if work.Force {
		return false, nil
	}
	references, err := r.server.library.GetAnalysisWorkPreviews(ctx, task.ChildID, task.Fence)
	if err != nil {
		return false, err
	}
	if len(references) != len(work.Execution.PreviewWidths) {
		return false, nil
	}
	leases := make([]*analysiscache.Lease, 0, len(references))
	defer func() {
		for _, lease := range leases {
			resultErr = errors.Join(resultErr, lease.Close())
		}
		if resultErr != nil {
			reused = false
		}
	}()
	manifest, err := r.acquirePreviewManifest(ctx, references, &work)
	if errors.Is(err, analysiscache.ErrNotFound) || errors.Is(err, analysiscache.ErrBusy) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	leases = append(leases, manifest)
	seen := make(map[int]bool, len(references))
	for _, reference := range references {
		allowed := false
		for _, width := range work.Execution.PreviewWidths {
			allowed = allowed || reference.Width == width
		}
		if !allowed || seen[reference.Width] || reference.ProfileFingerprint != work.ConfigurationFingerprint ||
			reference.ProfileRevision != work.ConfigurationRevision || reference.PublicationEpoch != work.PublicationEpoch {
			return false, library.ErrAnalysisSourceChanged
		}
		seen[reference.Width] = true
		lease, err := r.cache.Acquire(ctx, reference.CacheKey, reference.Seal, strconv.Itoa(reference.Width)+".bif")
		if errors.Is(err, analysiscache.ErrNotFound) || errors.Is(err, analysiscache.ErrBusy) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		leases = append(leases, lease)
		if lease.Artifact.SHA256 != reference.SHA256 || lease.Artifact.Size != reference.Bytes || lease.Seal != reference.Seal {
			return false, analysiscache.ErrUnsafe
		}
		index, err := bif.Open(lease.File, reference.Bytes, bif.DefaultLimits())
		if err != nil || index.Len() != reference.FrameCount || len(reference.NominalTicks) != index.Len() {
			return false, errors.Join(analysiscache.ErrUnsafe, err)
		}
		for position := 0; position < index.Len(); position++ {
			entry, err := index.Entry(position)
			if err != nil || entry.TimestampMillis != uint64(reference.NominalTicks[position]/(media.TicksPerSecond/1000)) {
				return false, errors.Join(analysiscache.ErrUnsafe, err)
			}
		}
	}
	if _, err := r.server.library.RevalidateAnalysisWork(ctx, task.ChildID, task.Fence); err != nil {
		return false, err
	}
	return true, ctx.Err()
}
