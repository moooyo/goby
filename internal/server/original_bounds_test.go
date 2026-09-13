package server

import (
	"context"
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func TestOriginalStreamQuotaSharesAccountsAndSeparatesApplicationCredentials(t *testing.T) {
	for _, application := range []bool{false, true} {
		name := "account"
		if application {
			name = "application credential"
		}
		t.Run(name, func(t *testing.T) {
			runtime := newOriginalStreamRuntime()
			defer runtime.stop()
			principal := identity.Principal{User: identity.User{ID: "owner-a"}, SessionID: "authentication-a", Kind: "emby"}
			if application {
				principal = identity.Principal{Kind: identity.ApplicationKeyKind, ApplicationKeyID: 1,
					SessionID: "credential-a", ClientSessionID: "client-a"}
			}
			var releases []func()
			defer func() {
				for _, release := range releases {
					release()
				}
				runtime.wait()
			}()
			for index := 0; index < maxOriginalOwnerStreams; index++ {
				_, release, err := runtime.enter(principal)
				if err != nil {
					t.Fatal(err)
				}
				releases = append(releases, release)
			}
			sibling := principal
			sibling.Client.DeviceID = "other-device"
			if application {
				sibling.ClientSessionID = "another-application-client"
			} else {
				sibling.SessionID = "another-login"
			}
			if _, release, err := runtime.enter(sibling); !errors.Is(err, library.ErrBusy) || release != nil {
				t.Fatal("another login or client context bypassed its owner's original-stream quota")
			}
			peer := sibling
			if application {
				peer.SessionID, peer.ApplicationKeyID = "credential-b", 2
			} else {
				peer.User.ID = "owner-b"
			}
			_, release, err := runtime.enter(peer)
			if err != nil {
				t.Fatal("one owner consumed another owner's original-stream allowance")
			}
			releases = append(releases, release)
			releases[0]()
			releases[0]()
			_, release, err = runtime.enter(sibling)
			if err != nil {
				t.Fatal("a released original stream did not restore exactly one slot")
			}
			releases = append(releases, release)
			runtime.stop()
			if _, _, err := runtime.enter(peer); !errors.Is(err, context.Canceled) {
				t.Fatal("shutdown admitted a new original stream")
			}
		})
	}
}

func TestImageTransferBudgetBoundsCountAndBackingCapacity(t *testing.T) {
	cache := newImageCache()
	release, ok := cache.beginTransfer(imageTransferBytes)
	if !ok {
		t.Fatal("the exact transfer-byte budget should be available")
	}
	if _, ok := cache.beginTransfer(1); ok {
		t.Fatal("the image transfer-byte budget was exceeded")
	}
	release()
	release()
	var releases []func()
	for index := 0; index < imageTransferCount; index++ {
		release, ok := cache.beginTransfer(0)
		if !ok {
			t.Fatal("a header-only transfer lost its bounded slot")
		}
		releases = append(releases, release)
	}
	if _, ok := cache.beginTransfer(0); ok {
		t.Fatal("header-only transfers bypassed the transfer-count limit")
	}
	for _, release := range releases {
		release()
	}
	if cache.activeTransfers != 0 || cache.activeBytes != 0 {
		t.Fatal("image transfer accounting did not return to zero")
	}
}
