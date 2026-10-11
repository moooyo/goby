//go:build linux

package lifecycle

import "context"

// ReadGenerationSnapshot verifies the complete retained history before reading
// the requested image and deriving its master path within the same store gate.
// Callers own the sensitive bytes and must retain MasterKeyPath's loader checks.
func (s *Store) ReadGenerationSnapshot(ctx context.Context, id string) (GenerationSnapshot, error) {
	if !validHex(id, 32) {
		return GenerationSnapshot{}, ErrInvalid
	}
	if err := s.enter(ctx); err != nil {
		return GenerationSnapshot{}, err
	}
	defer s.leave()
	if err := s.verify(ctx); err != nil {
		return GenerationSnapshot{}, err
	}
	files, err := s.readGenerationLocked(ctx, id)
	if err != nil {
		return GenerationSnapshot{}, err
	}
	result := GenerationSnapshot{Files: files}
	returned := false
	defer func() {
		if !returned {
			clear(result.Files.Config)
			clear(result.Files.Master)
		}
	}()
	if len(files.Master) != 0 {
		result.MasterKeyPath, err = s.masterKeyPathLocked(id)
		if err != nil {
			return GenerationSnapshot{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return GenerationSnapshot{}, err
	}
	returned = true
	return result, nil
}

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
