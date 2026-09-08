// Package library manages authorized media catalogs and persistent scan jobs.
package library

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/metadata"
)

var (
	ErrNotFound     = errors.New("library resource not found")
	ErrInvalidInput = errors.New("invalid library input")
	ErrForbidden    = errors.New("library access forbidden")
	ErrBusy         = errors.New("library scan is already active or queue is full")
	ErrUnavailable  = errors.New("library service or root is unavailable")
)

type Prober interface {
	ProbeFile(context.Context, *os.File) (media.Info, error)
}

type Library struct {
	ID, Name, CollectionType string
	Paths                    []string
	CreatedAt                time.Time
	LastScanAt               *time.Time
}

type Job struct {
	ID, LibraryID, Status, Error string
	Scanned, Added, Updated      int
	CreatedAt                    time.Time
	StartedAt, FinishedAt        *time.Time
}

// Path is server data: only authorized item projections may expose it.
type Item struct {
	ID, LibraryID, ParentID, Name, SortName, Type, Path, Overview string
	IsFolder                                                      bool
	IndexNumber, ParentIndexNumber                                int
	CreatedAt                                                     time.Time
	Media                                                         *media.Info
	Metadata                                                      *metadata.Metadata
}

type Query struct {
	UserID, ParentID, SearchTerm, SortBy, SortOrder string
	Recursive                                       bool
	StartIndex, Limit                               int
	IncludeItemTypes, Ids, MediaTypes               []string
	ParentIndexNumber                               *int
}

type ItemResult struct {
	Items            []Item
	TotalRecordCount int
}

type approvedRoot struct {
	path string
	root *os.Root
}

type libraryRoot struct {
	id, libraryID, path, allowedPath, relativePath string
}

type scanTask struct {
	job    Job
	ctx    context.Context
	cancel context.CancelFunc
}

// Store runs exactly two scan workers. Administrators must be authorized by the
// caller before using management methods; item methods enforce user policies.
type Store struct {
	pool        *pgxpool.Pool
	ownership   *scanOwnership
	prober      Prober
	roots       []approvedRoot
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	closed      bool
	shutdownErr error
	active      map[string]*scanTask
	queue       chan *scanTask
	workers     sync.WaitGroup
	done        chan struct{}
}

type rowScanner interface{ Scan(...any) error }
