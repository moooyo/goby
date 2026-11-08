package library

import (
	"context"
	"errors"

	"github.com/moooyo/goby/internal/identity"
)

var ErrDirectoryNotWritable = errors.New("server directory is not writable by the service")

// ValidateWritableServerDirectory observes effective access on an authorized,
// held directory. It does not create, truncate, rename, or remove a probe file.
// A successful observation is not a reservation against a later mount change.
func (s *Store) ValidateWritableServerDirectory(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, path string) (string, error) {
	if path == "" {
		return "", ErrInvalidInput
	}
	read := func(work context.Context, approved []string, path string, start, limit int, validateOnly bool) (page ServerDirectoryPage, resultErr error) {
		page, err := s.readServerDirectories(work, approved, path, start, limit, true)
		if err != nil {
			return ServerDirectoryPage{}, err
		}
		registration, err := s.authorizePathObserved(work, page.Path)
		if err != nil {
			return ServerDirectoryPage{}, err
		}
		defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(work, registration.Close)) }()
		directory, err := registration.registered.Open(".")
		if err != nil {
			return ServerDirectoryPage{}, errors.Join(ErrUnavailable, err)
		}
		defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(work, directory.Close)) }()
		writable, err := observeDirectoryWriteAccess(work, directory)
		if err != nil {
			return ServerDirectoryPage{}, err
		}
		if err := registration.Revalidate(work); err != nil {
			return ServerDirectoryPage{}, err
		}
		if writable == nil {
			return ServerDirectoryPage{}, ErrUnavailable
		}
		if !*writable {
			return ServerDirectoryPage{}, ErrDirectoryNotWritable
		}
		return page, work.Err()
	}
	page, err := s.serverDirectoriesWithReader(ctx, &catalogAdministrator{actor: actor, audience: audience}, path, 0, 1, true, read)
	return page.Path, err
}
