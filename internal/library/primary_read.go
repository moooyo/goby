package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/moooyo/goby/internal/primaryio"
)

const originalMediaReadChunk = 32 * 1024

// These process-wide budgets survive Store replacement. A retained consumer
// remains registered while its descriptor is idle between bounded read phases.
var originalMediaReadGovernor, originalMediaReadOwners = newOriginalMediaReadRuntime()

var originalMediaReadDomains = originalMediaReadDomainRegistry{claims: make(map[*originalMediaReadDomainClaim]struct{})}

type originalMediaReadDomainRegistry struct {
	mu     sync.Mutex
	claims map[*originalMediaReadDomainClaim]struct{}
}

type originalMediaReadDomainClaim struct {
	registry *originalMediaReadDomainRegistry
	domain   string
	once     sync.Once
	backing  *originalMediaReadDomainClaim
	refs     int // Only a registry entry owns refs; registry.mu protects it.
}

// acquire rejects inconsistent live configurations rather than splitting one
// overlapping storage domain across exact governor tokens. The registry only
// observes trusted canonical mappings and never probes the filesystem.
func (r *originalMediaReadDomainRegistry) acquire(domain string) (*originalMediaReadDomainClaim, error) {
	if r == nil || domain == "" || !filepath.IsAbs(domain) || filepath.Clean(domain) != domain {
		return nil, ErrUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.claims) >= 64 {
		return nil, ErrBusy
	}
	for claim := range r.claims {
		if claim.domain != domain && (pathWithin(claim.domain, domain) || pathWithin(domain, claim.domain)) {
			return nil, fmt.Errorf("%w: configured original media read domains overlap", ErrUnavailable)
		}
	}
	claim := &originalMediaReadDomainClaim{registry: r, domain: domain, refs: 1}
	if r.claims == nil {
		r.claims = make(map[*originalMediaReadDomainClaim]struct{})
	}
	r.claims[claim] = struct{}{}
	return claim, nil
}

// acquireShared borrows only an exact configured domain. It grants no catalog
// or source authority and checks every conflicting configuration before using
// an existing entry. An alias does not consume another retained-body entry.
func (r *originalMediaReadDomainRegistry) acquireShared(domain string) (*originalMediaReadDomainClaim, error) {
	if r == nil || domain == "" || !filepath.IsAbs(domain) || filepath.Clean(domain) != domain {
		return nil, ErrUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var backing *originalMediaReadDomainClaim
	for claim := range r.claims {
		if claim.domain != domain && (pathWithin(claim.domain, domain) || pathWithin(domain, claim.domain)) {
			return nil, fmt.Errorf("%w: configured original media read domains overlap", ErrUnavailable)
		}
		if claim.domain == domain {
			backing = claim
		}
	}
	if backing != nil {
		backing.refs++
		return &originalMediaReadDomainClaim{registry: r, domain: domain, backing: backing}, nil
	}
	if len(r.claims) >= 64 {
		return nil, ErrBusy
	}
	claim := &originalMediaReadDomainClaim{registry: r, domain: domain, refs: 1}
	if r.claims == nil {
		r.claims = make(map[*originalMediaReadDomainClaim]struct{})
	}
	r.claims[claim] = struct{}{}
	return claim, nil
}

func (c *originalMediaReadDomainClaim) release() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		c.registry.mu.Lock()
		backing := c
		if c.backing != nil {
			backing = c.backing
		}
		backing.refs--
		if backing.refs == 0 {
			delete(c.registry.claims, backing)
		}
		c.registry.mu.Unlock()
	})
}

// The SectionReader owns the logical position and verified indexed length.
// Read is the only source operation that accesses the underlying file body;
// Seek performs no root I/O, and no raw-file methods are promoted.
type originalMediaReadSource struct {
	reader *io.SectionReader
	file   *os.File
}

func (s *originalMediaReadSource) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (s *originalMediaReadSource) Seek(offset int64, whence int) (int64, error) {
	return s.reader.Seek(offset, whence)
}
func (s *originalMediaReadSource) Close() error {
	err := s.file.Close()
	// The existing authorization guard may already have closed its borrowed
	// descriptor to interrupt a syscall. Its joined close is valid retirement.
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

func newOriginalMediaReadRuntime() (*primaryio.Governor, *primaryio.OwnerRuntime) {
	governor, err := primaryio.NewGovernor(primaryio.Limits{
		Owners: 4, BackgroundOwners: 2,
		RootOwners: 3, RootBackgroundOwners: 2,
		DomainOwners: 3, DomainBackgroundOwners: 2,
		Queued: 128, RootQueued: 32, DomainQueued: 32,
	})
	if err != nil {
		panic("invalid original media read governor defaults")
	}
	owners, err := primaryio.NewOwnerRuntime(governor, 64)
	if err != nil {
		panic("invalid original media retained owner defaults")
	}
	return governor, owners
}

// primaryReadRoute uses only configured Store anchors and catalog routing data.
// An exact-token governor needs every configured governing ancestor, unlike the
// source-open governor's dynamic path-overlap accounting. No filesystem probe or
// client-supplied pathname classifies the operation.
func (s *Store) primaryReadRoute(hint mediaSourceRootHint) (primaryio.Route, error) {
	root, domain, err := s.mediaSourceRootLane(hint)
	if err != nil {
		return primaryio.Route{}, err
	}
	route := primaryio.Route{Roots: []primaryio.RootKey{{Catalog: root.catalog, RootID: root.id}}}
	seen := make(map[string]bool)
	addDomain := func(path string) {
		path = filepath.Clean(path)
		if !seen[path] {
			seen[path] = true
			route.Domains = append(route.Domains, path)
		}
	}
	addDomain(domain)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.closing.Load() {
		return primaryio.Route{}, ErrUnavailable
	}
	for _, approved := range s.roots {
		if pathWithin(approved.path, hint.root.allowedPath) {
			addDomain(approved.path)
		}
	}
	// Route dimensions are bounded by the governor. Reject an unsupported
	// configuration rather than silently omitting a governing ancestor.
	if len(route.Domains) > 16 {
		return primaryio.Route{}, ErrUnavailable
	}
	return route, nil
}

// OpenOriginalMediaFor reopens the planning snapshot under fresh playback
// authority and hands its complete descriptor lifetime to a bounded reader.
// ctx is the actual delivery lifetime, not the planning timeout. The returned
// file is borrowed only for cancellation by the existing HTTP guard; callers
// must read through content and finish descriptor ownership with content.Close.
func (s *Store) OpenOriginalMediaFor(ctx context.Context, subject Subject, itemID, sourceID, expectedETag string) (*os.File, MediaFile, *primaryio.ReadSeeker, error) {
	return s.openOriginalMediaFor(ctx, subject, itemID, sourceID, expectedETag, s.openPublicMediaSource)
}

// The private opener seam lets integration fixtures retain a real syscall owner
// past caller cancellation. Production always uses the public-source opener.
func (s *Store) openOriginalMediaFor(ctx context.Context, subject Subject, itemID, sourceID, expectedETag string, opener func(context.Context, indexedMediaSource) (*os.File, error)) (file *os.File, source MediaFile, content *primaryio.ReadSeeker, resultErr error) {
	// Planning retains its full public projection. Fresh delivery needs only
	// complete primary media facts and the same authority/identity/publication
	// proof; avoid unrelated catalog, parent, intro and subtitle projections.
	readSnapshot := func(ctx context.Context, subject Subject, itemID, sourceID string) (indexedMediaSource, error) {
		return s.readMediaRevalidationFor(ctx, subject, itemID, sourceID, false)
	}
	return s.openOriginalReadFor(ctx, subject, itemID, sourceID, expectedETag, readSnapshot, opener)
}

// openOriginalReadFor keeps delivery ownership independent from the authority
// purpose. Every snapshot and cancellation recheck uses the same purpose reader;
// sharing actual-read budgets must never grant playback or download authority.
func (s *Store) openOriginalReadFor(ctx context.Context, subject Subject, itemID, sourceID, expectedETag string, readSnapshot func(context.Context, Subject, string, string) (indexedMediaSource, error), opener func(context.Context, indexedMediaSource) (*os.File, error)) (file *os.File, source MediaFile, content *primaryio.ReadSeeker, resultErr error) {
	if ctx == nil || strings.TrimSpace(itemID) == "" || strings.ContainsRune(itemID, '\x00') ||
		strings.ContainsRune(sourceID, '\x00') || expectedETag == "" || strings.ContainsRune(expectedETag, '\x00') || readSnapshot == nil || opener == nil {
		return nil, MediaFile{}, nil, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return nil, MediaFile{}, nil, ErrUnavailable
	}
	work, finish, err := s.beginMediaSourceLifetime(ctx)
	if err != nil {
		return nil, MediaFile{}, nil, err
	}
	owner, err := originalMediaReadOwners.Register(work)
	if err != nil {
		finish()
		if errors.Is(err, primaryio.ErrBusy) {
			return nil, MediaFile{}, nil, fmt.Errorf("%w: retain original media reader: %w", ErrBusy, err)
		}
		if errors.Is(err, primaryio.ErrClosed) {
			return nil, MediaFile{}, nil, fmt.Errorf("%w: retain original media reader: %w", ErrUnavailable, err)
		}
		return nil, MediaFile{}, nil, err
	}
	adopted := false
	var ownedFile *os.File
	var domainClaim *originalMediaReadDomainClaim
	defer func() {
		if adopted {
			return
		}
		// This is only the idle consumer registration. The source-open worker
		// retains its separate 4/3 admission and Store lifetime until its syscall
		// and any undelivered descriptor cleanup finish, even after this return.
		if ownedFile != nil {
			if closeErr := ownedFile.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
				// Unknown descriptor retirement must retain both external pins and
				// the consumer record instead of advertising successful cleanup.
				resultErr = errors.Join(resultErr, fmt.Errorf("%w: close original media construction input: %w", ErrUnavailable, closeErr))
				return
			}
		}
		if err := owner.Complete(); err != nil {
			// An unretired owner must keep its Store registration. Successful
			// constructor transfer is the only path allowed to change this owner.
			resultErr = errors.Join(resultErr, fmt.Errorf("%w: retire original media reader: %w", ErrUnavailable, err))
			return
		}
		domainClaim.release()
		finish()
	}()
	var route primaryio.Route
	file, source, err = s.runPreparedMediaSourceWorker(work, false, func(ctx context.Context) (mediaSourceRootHint, error) {
		hint, err := s.readMediaSourceRootHint(ctx, itemID)
		if err != nil {
			return mediaSourceRootHint{}, err
		}
		route, err = s.primaryReadRoute(hint)
		if err != nil {
			return mediaSourceRootHint{}, err
		}
		domainClaim, err = originalMediaReadDomains.acquire(route.Domains[0])
		if err != nil {
			return mediaSourceRootHint{}, err
		}
		return hint, nil
	}, func(ctx context.Context) (*os.File, MediaFile, error) {
		snapshot, err := readSnapshot(ctx, subject, itemID, sourceID)
		if err != nil {
			return nil, MediaFile{}, err
		}
		if snapshot.mediaFile.ETag != expectedETag {
			return nil, MediaFile{}, fmt.Errorf("%w: %w: original media snapshot changed before delivery", ErrUnavailable, ErrSourceChanged)
		}
		opened, err := opener(ctx, snapshot)
		if err != nil {
			// The actual source worker closes a nonnil error result while still
			// holding source-open I/O admission and Store lifetime.
			return opened, MediaFile{}, err
		}
		return opened, snapshot.mediaFile, nil
	}, func(ctx context.Context) error {
		_, err := readSnapshot(ctx, subject, itemID, sourceID)
		return err
	})
	ownedFile = file
	if err != nil {
		return nil, MediaFile{}, nil, err
	}
	logicalSource := &originalMediaReadSource{reader: io.NewSectionReader(file, 0, source.Size), file: file}
	retired := func() {
		domainClaim.release()
		finish()
	}
	content, err = primaryio.NewReadSeeker(owner, route, primaryio.Foreground, logicalSource, originalMediaReadChunk, retired)
	if err != nil {
		return nil, MediaFile{}, nil, err
	}
	adopted = true
	return file, source, content, nil
}
