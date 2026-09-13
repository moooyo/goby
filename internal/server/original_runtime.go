package server

import (
	"context"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
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
	fresh, err := s.identity.RevalidateSession(ctx, principal)
	if err != nil {
		return err
	}
	if fresh.IsApplicationKey() != principal.IsApplicationKey() || fresh.User.ID != principal.User.ID ||
		fresh.ClientSessionID != principal.ClientSessionID ||
		fresh.SessionID != principal.SessionID || fresh.Client.DeviceID != principal.Client.DeviceID {
		return library.ErrNotFound
	}
	verified, current, err := s.library.OpenMediaFor(ctx, librarySubject(fresh, fresh.User.ID), source.Item.ID, source.SourceID)
	if err != nil {
		return err
	}
	_ = verified.Close()
	if current.ETag != source.ETag {
		return library.ErrSourceChanged
	}
	return nil
}

// guardOriginalMedia rechecks access before headers and during long responses.
// Permanent revocation closes the source and interrupts a blocked HTTP write;
// transient database or storage observations do not erase an existing grant.
// Cleanup joins the watcher before the connection can serve another request.
func (s *Server) guardOriginalMedia(w http.ResponseWriter, r *http.Request, file *os.File, source library.MediaFile) (context.Context, func(), error) {
	principal := r.Context().Value(principalKey).(identity.Principal)
	lifetime, leave, err := s.originals.enter(principal)
	if err != nil {
		return nil, nil, err
	}
	work, cancel := context.WithCancel(r.Context())
	stopLifetime := context.AfterFunc(lifetime, cancel)
	check, stopCheck := context.WithTimeout(work, 10*time.Second)
	err = s.authorizeOriginal(check, principal, source)
	stopCheck()
	if err != nil {
		stopLifetime()
		cancel()
		leave()
		return nil, nil, err
	}
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
				err := s.authorizeOriginal(check, principal, source)
				stopCheck()
				if permanentHLSError(err) {
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
		leave()
	}
	return work, finish, nil
}
