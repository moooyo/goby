//go:build linux

package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/transcode"
)

// This covers the disabled correlated-owner route with a real catalog-issued
// capability and native descriptor. An invalid segment uses a preloaded timeline
// so the first consuming Close runs without a probe, encoder or fake Start.
func TestHLSPrimarySourceCloseConsumedUnownedInputRetiresRealStore(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			fixture := newStreamHTTPFixture(t)
			f := fixture.f
			f.app.correlatedHLSOwnershipEnabled = false
			f.app.correlatedHLSEarlyStopEnabled = false
			principal, err := f.users.ResolveWithPeer(f.ctx, fixture.token, "emby", "127.0.0.1")
			if err != nil {
				t.Fatal("resolve the actual authorized stream-fixture principal")
			}
			file, source, err := f.app.library.OpenMediaFor(f.ctx, librarySubject(principal, principal.User.ID), fixture.video.id, "")
			if err != nil || file == nil {
				t.Fatal("open a real descriptor from the committed authorized catalog snapshot")
			}
			read, err := f.app.library.PrepareMediaSourceIO(f.ctx, source)
			if err != nil || read == nil {
				_ = file.Close()
				t.Fatal("retain the real Store lifetime from its private committed source proof")
			}
			h, jobs := hlsRuntimeTestFixture(t)
			h.server = f.app
			session := hlsRuntimeTestSession(t, h, "unowned-consumed-"+method, false)
			ctx := read.Context(f.ctx)
			loan := &hlsPlaybackSource{runtime: h, file: file, read: read, work: ctx}
			t.Cleanup(func() {
				if err := loan.close(); err != nil {
					t.Error("close the real unowned source loan during cleanup")
				}
				cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := h.Close(cleanup); err != nil {
					t.Error("join the isolated preloaded-timeline runtime during cleanup")
				}
			})
			opened, openErr := file.Stat()
			indexed, indexedErr := os.Stat(fixture.video.path)
			if openErr != nil || indexedErr != nil || !os.SameFile(opened, indexed) || opened.Size() != source.Size {
				t.Fatal("the authorized loan did not hold the exact indexed native source descriptor")
			}
			var handle *transcode.ReadHandle
			if method == http.MethodHead {
				handle, err = h.segmentHeader(ctx, session, file, -1)
			} else {
				handle, err = h.segment(ctx, session, file, -1)
			}
			if handle != nil || !errors.Is(err, transcode.ErrJobNotFound) {
				t.Fatal("the invalid preloaded segment did not return its consuming early-exit result")
			}
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatal("the GET or HEAD consumer did not actually close the exact native source descriptor")
			}
			if err := loan.close(); err != nil {
				t.Fatal("the first loan cleanup treated the consumer's completed descriptor Close as unknown retirement")
			}
			if err := loan.close(); err != nil {
				t.Fatal("repeated loan cleanup did not preserve the known descriptor retirement")
			}
			jobs.mu.Lock()
			ensured := len(jobs.ensured)
			jobs.mu.Unlock()
			if ensured != 0 {
				t.Fatal("the consuming early-exit source-close regression unexpectedly admitted a producer")
			}
			// One deadline covers every actual join. Extending it or replacing the
			// Store join with scalar idle observations would hide the leaked owner.
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := h.Close(cleanup); err != nil {
				t.Fatal("join the isolated consumed-input runtime")
			}
			if err := f.app.hls.Close(cleanup); err != nil {
				t.Fatal("join the fixture's production conversion runtime before Store cleanup")
			}
			if err := f.app.mediaOperations.Close(cleanup); err != nil {
				t.Fatal("join media operations before closing their real catalog")
			}
			if err := f.app.taskManager.Close(cleanup); err != nil {
				t.Fatal("join task ownership before closing the real catalog")
			}
			if err := f.app.mediaAnalysis.Close(cleanup); err != nil {
				t.Fatal("join media analysis before closing the real catalog")
			}
			if err := f.app.library.Close(cleanup); err != nil {
				t.Fatal("the real Store retained its source lifetime after successful consumer and repeated loan Close")
			}
			t.Logf("hls_primary_source_close method=%s exact_native_fd_consumed=true repeated_loan_close_known=true producer_admissions=0 real_store_joined=true", method)
		})
	}
}
