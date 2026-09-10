//go:build !linux

package diagnostics

import "context"

// Store requires Linux directory descriptors and process locks.
type Store struct{}

func Open(Config) (*Store, error) { return nil, ErrUnavailable }
func (*Store) Close() error       { return nil }
func (*Store) Status() Status     { return Status{Degraded: true, Format: "jsonl"} }
func (*Store) List(context.Context, ListOptions) (Page, error) {
	return Page{}, ErrUnavailable
}
func (*Store) Snapshot(context.Context, string) (*Snapshot, error) {
	return nil, ErrUnavailable
}
func (*Store) appendRecord(context.Context, []byte) error { return ErrUnavailable }
func (*Store) markDegraded()                              {}
