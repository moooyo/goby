//go:build !linux

package backupstore

import "context"

// Store and Writer are compile-only stubs on unsupported operating systems.
type Store struct{}
type Writer struct{}

func Open(Config) (*Store, error)                                   { return nil, ErrUnavailable }
func (*Store) Close() error                                         { return nil }
func (*Store) Status() Status                                       { return Status{Closed: true} }
func (*Store) Begin(context.Context, BeginOptions) (*Writer, error) { return nil, ErrUnavailable }
func (*Store) List(context.Context, int, int) (Page, error)         { return Page{}, ErrUnavailable }
func (*Store) Get(context.Context, string) (Metadata, error)        { return Metadata{}, ErrUnavailable }
func (*Store) Snapshot(context.Context, string) (*Snapshot, error)  { return nil, ErrUnavailable }
func (*Store) Scratch(context.Context, int64) (*Scratch, error)     { return nil, ErrUnavailable }
func (*Store) Protect(string) (func(), error)                       { return nil, ErrUnavailable }
func (*Store) Delete(context.Context, string, string) error         { return ErrUnavailable }
func (*Store) ReopenPrepared(context.Context, string, string) (*Writer, error) {
	return nil, ErrUnavailable
}
func (*Store) Verify(context.Context, string, string, SourceSummary) (Metadata, error) {
	return Metadata{}, ErrUnavailable
}
func (*Writer) Metadata() Metadata                        { return Metadata{} }
func (*Writer) Write([]byte) (int, error)                 { return 0, ErrUnavailable }
func (*Writer) Prepare(context.Context) (Prepared, error) { return Prepared{}, ErrUnavailable }
func (*Writer) Publish(context.Context, Prepared, *SourceSummary) (Metadata, error) {
	return Metadata{}, ErrUnavailable
}
func (*Writer) Abort(context.Context, ErrorCode) error { return ErrUnavailable }
func (*Writer) Close() error                           { return nil }
