//go:build !linux

package recoverycontrol

import "context"

// Store is only supported on Linux.
type Store struct{}

func Open(context.Context, string, string) (*Store, error) { return nil, ErrUnavailable }
func (*Store) Read(context.Context) (Snapshot, error)      { return Snapshot{}, ErrUnavailable }
func (*Store) CompareAndSwap(context.Context, string, []byte) (Snapshot, error) {
	return Snapshot{}, ErrUnavailable
}
func (*Store) Close() error { return nil }
