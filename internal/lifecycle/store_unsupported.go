//go:build !linux

package lifecycle

import "context"

// Store requires Linux descriptor-relative filesystem operations and flock.
type Store struct{}

func Open(context.Context, string) (*Store, error) { return nil, ErrUnavailable }
func (*Store) Current() (State, error)             { return State{}, ErrUnavailable }
func (*Store) StageGeneration(context.Context, string, []byte, []byte) (Generation, error) {
	return Generation{}, ErrUnavailable
}
func (*Store) ReadGeneration(context.Context, string) (GenerationFiles, error) {
	return GenerationFiles{}, ErrUnavailable
}
func (*Store) MasterKeyPath(context.Context, string) (string, error) { return "", ErrUnavailable }
func (*Store) Generations(context.Context) ([]Generation, error)     { return nil, ErrUnavailable }
func (*Store) Plan(context.Context, State, Candidate) (Plan, error)  { return Plan{}, ErrUnavailable }
func (*Store) Pending(context.Context) (*Plan, error)                { return nil, ErrUnavailable }
func (*Store) Activate(context.Context, string) (State, error)       { return State{}, ErrUnavailable }
func (*Store) Finish(context.Context, string) error                  { return ErrUnavailable }
func (*Store) Abort(context.Context, string) error                   { return ErrUnavailable }
func (*Store) Close() error                                          { return nil }
