package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

const maxServerDirectoryEntries = 10000

var ErrDirectoryLimit = errors.New("server directory contains too many entries to browse")
var serverDirectoryWorkers = make(chan struct{}, 4)

type ServerDirectory struct {
	Name string
	Path string
}

type ServerDirectoryPage struct {
	Path             string
	ParentPath       string `json:"ParentPath,omitempty"`
	Items            []ServerDirectory
	TotalRecordCount int
	StartIndex       int
	Limit            int
}

type serverDirectoryResult struct {
	page ServerDirectoryPage
	err  error
}

// The private reader seam keeps authorization, admission and resource ownership
// in one path while tests gate the completion of real directory reads.
type serverDirectoryReader func(context.Context, []string, string, int, int, bool) (ServerDirectoryPage, error)

// BrowseServerDirectories never exposes drives, files, network shares, or a
// parent outside the configured roots. An empty path lists approved roots only.
func (s *Store) BrowseServerDirectories(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, path string, start, limit int) (ServerDirectoryPage, error) {
	return s.serverDirectories(ctx, &catalogAdministrator{actor: actor, audience: audience}, path, start, limit, false)
}

// ValidateServerDirectory is a read-only directory check, not a write probe.
func (s *Store) ValidateServerDirectory(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, path string) (string, error) {
	if path == "" {
		return "", ErrInvalidInput
	}
	result, err := s.serverDirectories(ctx, &catalogAdministrator{actor: actor, audience: audience}, path, 0, 1, true)
	return result.Path, err
}

func (s *Store) ServerDirectoryParent(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, path string) (string, error) {
	if path == "" {
		return "", ErrInvalidInput
	}
	result, err := s.serverDirectories(ctx, &catalogAdministrator{actor: actor, audience: audience}, path, 0, 1, true)
	return result.ParentPath, err
}

func (s *Store) serverDirectories(ctx context.Context, administrator *catalogAdministrator, path string, start, limit int, validateOnly bool) (ServerDirectoryPage, error) {
	return s.serverDirectoriesWithReader(ctx, administrator, path, start, limit, validateOnly, s.readServerDirectories)
}

func (s *Store) serverDirectoriesWithReader(ctx context.Context, administrator *catalogAdministrator, path string, start, limit int, validateOnly bool, read serverDirectoryReader) (ServerDirectoryPage, error) {
	if s == nil || s.pool == nil {
		return ServerDirectoryPage{}, ErrUnavailable
	}
	if ctx == nil || read == nil || start < 0 || start > maxServerDirectoryEntries || limit < 0 || limit > 200 {
		return ServerDirectoryPage{}, ErrInvalidInput
	}
	if limit == 0 {
		limit = 100
	}
	if path != "" {
		var err error
		path, err = libraryEditPath(path)
		if err != nil {
			return ServerDirectoryPage{}, err
		}
	}
	if err := s.checkLibraryRegistrationAdministrator(ctx, administrator); err != nil {
		return ServerDirectoryPage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case serverDirectoryWorkers <- struct{}{}:
	case <-ctx.Done():
		return ServerDirectoryPage{}, ctx.Err()
	}
	// Keep shutdown aware of admitted filesystem calls. A cancelled HTTP request
	// does not free the worker slot while a storage syscall is still blocked.
	s.mu.Lock()
	if s.closed || s.closing.Load() {
		s.mu.Unlock()
		<-serverDirectoryWorkers
		return ServerDirectoryPage{}, ErrUnavailable
	}
	s.rootOpens.Add(1)
	approvedPaths := make([]string, 0, len(s.roots))
	for _, root := range s.roots {
		approvedPaths = append(approvedPaths, strings.Clone(root.path))
	}
	s.mu.Unlock()
	completed := make(chan serverDirectoryResult)
	go func() {
		defer s.rootOpens.Done()
		defer func() { <-serverDirectoryWorkers }()
		page, err := read(ctx, approvedPaths, path, start, limit, validateOnly)
		select {
		case completed <- serverDirectoryResult{page: page, err: err}:
		case <-ctx.Done():
		}
	}()
	select {
	case result := <-completed:
		if result.err != nil {
			return ServerDirectoryPage{}, result.err
		}
		if err := s.checkLibraryRegistrationAdministrator(ctx, administrator); err != nil {
			return ServerDirectoryPage{}, err
		}
		return result.page, nil
	case <-ctx.Done():
		return ServerDirectoryPage{}, ctx.Err()
	}
}

func (s *Store) readServerDirectories(ctx context.Context, approvedPaths []string, path string, start, limit int, validateOnly bool) (ServerDirectoryPage, error) {
	page := ServerDirectoryPage{Path: path, StartIndex: start, Limit: limit, Items: make([]ServerDirectory, 0)}
	if err := ctx.Err(); err != nil {
		return ServerDirectoryPage{}, err
	}
	if path == "" {
		for _, root := range approvedPaths {
			page.Items = append(page.Items, ServerDirectory{Name: filepath.Base(root), Path: root})
		}
	} else {
		registration, err := s.authorizePath(path)
		if err != nil {
			return ServerDirectoryPage{}, err
		}
		defer registration.Close()
		page.Path = registration.root.path
		if page.Path != registration.root.allowedPath {
			page.ParentPath = filepath.Dir(page.Path)
		}
		if !validateOnly {
			directory, err := registration.registered.Open(".")
			if err != nil {
				return ServerDirectoryPage{}, ErrUnavailable
			}
			defer directory.Close()
			seen := 0
			for {
				if err := ctx.Err(); err != nil {
					return ServerDirectoryPage{}, err
				}
				entries, err := directory.ReadDir(256)
				seen += len(entries)
				if seen > maxServerDirectoryEntries {
					return ServerDirectoryPage{}, ErrDirectoryLimit
				}
				for _, entry := range entries {
					if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
						continue
					}
					// The entry can change after ReadDir. Re-check through the held
					// root and never advertise a symlink as an authorized directory.
					info, statErr := registration.registered.Lstat(entry.Name())
					if statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
						page.Items = append(page.Items, ServerDirectory{Name: entry.Name(), Path: filepath.Join(page.Path, entry.Name())})
					}
				}
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					return ServerDirectoryPage{}, ErrUnavailable
				}
			}
		}
		if err := registration.Revalidate(ctx); err != nil {
			return ServerDirectoryPage{}, err
		}
	}
	sort.Slice(page.Items, func(i, j int) bool { return page.Items[i].Path < page.Items[j].Path })
	page.TotalRecordCount = len(page.Items)
	if start > len(page.Items) {
		start = len(page.Items)
	}
	end := start + limit
	if end > len(page.Items) {
		end = len(page.Items)
	}
	page.Items = page.Items[start:end]
	return page, ctx.Err()
}
