package server

import (
	"context"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

// Original responses have a separate lifetime from conversion jobs. The shared
// HTTP stream slots bound their count; this runtime fences shutdown and waits
// until their authorization watchers have stopped using the catalog.
type originalStreamRuntime struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	closing  bool
	requests sync.WaitGroup
	owners   map[originalStreamOwner]int
}

const maxOriginalOwnerStreams = 8

type originalStreamOwner struct {
	application bool
	id          string
}

func newOriginalStreamRuntime() *originalStreamRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	return &originalStreamRuntime{ctx: ctx, cancel: cancel, owners: make(map[originalStreamOwner]int)}
}

func (runtime *originalStreamRuntime) enter(principal identity.Principal) (context.Context, func(), error) {
	if runtime == nil {
		return nil, nil, context.Canceled
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.closing {
		return nil, nil, context.Canceled
	}
	owner := originalStreamOwner{id: principal.User.ID}
	if principal.IsApplicationKey() {
		owner.application, owner.id = true, principal.SessionID
	}
	if owner.id == "" {
		return nil, nil, identity.ErrUnauthorized
	}
	if runtime.owners[owner] >= maxOriginalOwnerStreams {
		return nil, nil, library.ErrBusy
	}
	runtime.owners[owner]++
	runtime.requests.Add(1)
	var once sync.Once
	leave := func() {
		once.Do(func() {
			runtime.mu.Lock()
			if runtime.owners[owner] <= 1 {
				delete(runtime.owners, owner)
			} else {
				runtime.owners[owner]--
			}
			runtime.mu.Unlock()
			runtime.requests.Done()
		})
	}
	return runtime.ctx, leave, nil
}

func (runtime *originalStreamRuntime) stop() {
	if runtime == nil {
		return
	}
	runtime.mu.Lock()
	runtime.closing = true
	runtime.cancel()
	runtime.mu.Unlock()
}

func (runtime *originalStreamRuntime) wait() {
	if runtime != nil {
		runtime.requests.Wait()
	}
}

func (s *Server) authorizeOriginal(ctx context.Context, principal identity.Principal, source library.MediaFile) error {
	_, err := s.authorizeOriginalPolicy(ctx, principal, source)
	return err
}

func (s *Server) authorizeOriginalPolicy(ctx context.Context, principal identity.Principal, source library.MediaFile) (identity.Principal, error) {
	fresh, err := s.identity.RevalidateSession(ctx, principal)
	if err != nil {
		return fresh, err
	}
	if fresh.IsApplicationKey() != principal.IsApplicationKey() || fresh.User.ID != principal.User.ID ||
		fresh.ClientSessionID != principal.ClientSessionID ||
		fresh.SessionID != principal.SessionID || fresh.Client.DeviceID != principal.Client.DeviceID {
		return fresh, library.ErrNotFound
	}
	verified, current, err := s.library.OpenMediaFor(ctx, librarySubject(fresh, fresh.User.ID), source.Item.ID, source.SourceID)
	if err != nil {
		return fresh, err
	}
	_ = verified.Close()
	if current.ETag != source.ETag {
		return fresh, library.ErrSourceChanged
	}
	if !principalOriginalBitrateAllowed(fresh, current) {
		return fresh, library.ErrForbidden
	}
	return fresh, nil
}

// guardOriginalMedia rechecks access before headers and during long responses.
// Permanent revocation closes the source and interrupts a blocked HTTP write;
// transient database or storage observations do not erase an existing grant.
// Its known credential expiry remains a hard bound during those observations.
// Cleanup joins the watcher before the connection can serve another request.
func (s *Server) guardOriginalMedia(w http.ResponseWriter, r *http.Request, file *os.File, source library.MediaFile) (context.Context, func(), error) {
	principal := r.Context().Value(principalKey).(identity.Principal)
	lifetime, leave, err := s.originals.enter(principal)
	if err != nil {
		return nil, nil, err
	}
	work, cancel := context.WithCancel(r.Context())
	stopLifetime := context.AfterFunc(lifetime, cancel)
	var expiresAt time.Time
	var expiryTimer *time.Timer
	// Setup and then the watcher own this state. Cleanup joins the watcher
	// before stopping the last timer or reading the effective deadline.
	tightenExpiry := func(current identity.Principal) {
		if current.IsApplicationKey() || current.ExpiresAt.IsZero() ||
			!expiresAt.IsZero() && !current.ExpiresAt.Before(expiresAt) {
			return
		}
		expiresAt = current.ExpiresAt
		if expiryTimer != nil {
			expiryTimer.Stop()
		}
		expiryTimer = time.AfterFunc(time.Until(expiresAt), cancel)
	}
	stopExpiry := func() {
		if expiryTimer != nil {
			expiryTimer.Stop()
		}
	}
	tightenExpiry(principal)
	check, stopCheck := context.WithTimeout(work, 10*time.Second)
	fresh, err := s.authorizeOriginalPolicy(check, principal, source)
	tightenExpiry(fresh)
	var scope transcode.Scope
	if err == nil {
		scope, err = s.originalMediaPolicyScope(check, r, fresh, source)
	}
	stopCheck()
	if err != nil {
		stopExpiry()
		stopLifetime()
		cancel()
		leave()
		return nil, nil, err
	}
	policyDone := func() {}
	if r.Method != http.MethodHead {
		policyWork, done, err := s.acquireMediaPolicy(work, fresh, scope)
		if err != nil {
			stopExpiry()
			stopLifetime()
			cancel()
			leave()
			return nil, nil, err
		}
		work, policyDone = policyWork, done
	}
	work = context.WithValue(work, mediaPolicyScopeContextKey{}, scope)
	finished, watched := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watched)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		abort := func() {
			cancel()
			_ = file.Close()
		}
		for {
			select {
			case <-finished:
				return
			case <-work.Done():
				abort()
				return
			case <-ticker.C:
				check, stopCheck := context.WithTimeout(work, 750*time.Millisecond)
				fresh, err := s.authorizeOriginalPolicy(check, principal, source)
				tightenExpiry(fresh)
				if err == nil {
					err = s.checkMediaPolicy(fresh, scope)
				}
				stopCheck()
				if permanentHLSError(err) {
					s.releaseMediaPolicy(scope)
					abort()
					return
				}
			}
		}
	}()
	finish := func() {
		close(finished)
		stopLifetime()
		cancel()
		<-watched
		stopExpiry()
		if !expiresAt.IsZero() && !time.Now().Before(expiresAt) {
			// Expired credentials cannot resume an idle lease with another range.
			s.releaseMediaPolicy(scope)
		}
		policyDone()
		leave()
	}
	return work, finish, nil
}
