//go:build linux

package lifecycle

import "context"

// ReadCurrent verifies the complete retained history once and captures the
// current state, generation files, and selected master path under the same
// cancellable gate. The path has the same loader requirements as MasterKeyPath.
func (s *Store) ReadCurrent(ctx context.Context) (Snapshot, error) {
	if err := s.enter(ctx); err != nil {
		return Snapshot{}, err
	}
	defer s.leave()
	if err := s.verify(ctx); err != nil {
		return Snapshot{}, err
	}
	result := Snapshot{State: s.stateOf(s.active)}
	if result.State.GenerationID == "" {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		return result, nil
	}
	files, err := s.readGenerationLocked(ctx, result.State.GenerationID)
	if err != nil {
		return Snapshot{}, err
	}
	result.Files = files
	returned := false
	defer func() {
		if !returned {
			clear(result.Files.Config)
			clear(result.Files.Master)
		}
	}()
	if result.State.Master == MasterGeneration {
		result.MasterKeyPath, err = s.masterKeyPathLocked(result.State.GenerationID)
		if err != nil {
			return Snapshot{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	returned = true
	return result, nil
}
