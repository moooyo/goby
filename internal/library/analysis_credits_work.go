package library

import "context"

// WithCreditsAnalysisPublication revalidates every admitted physical source,
// then supplies the current immutable work under its task and source fences.
// The callback may validate/persist bounded result evidence and notifications;
// it must not read files, run processes, or start another store transaction.
func (s *Store) WithCreditsAnalysisPublication(ctx context.Context, childID string, fence AnalysisFence, publish func(OwnedTx, AnalysisWork) error) error {
	if publish == nil {
		return ErrInvalidInput
	}
	work, err := s.RevalidateAnalysisWork(ctx, childID, fence)
	if err != nil {
		return err
	}
	if work.TaskKey != TaskCreditsAnalysisKey {
		return ErrInvalidInput
	}
	return s.withAnalysisWork(ctx, childID, fence, func(tx OwnedTx, current AnalysisWork) error {
		if current.TaskKey != TaskCreditsAnalysisKey {
			return ErrInvalidInput
		}
		return publish(tx, current)
	})
}
