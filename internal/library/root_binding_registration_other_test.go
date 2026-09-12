//go:build !linux

package library

import (
	"context"
	"testing"
)

func TestRootBindingRegistrationUnsupportedPlatformKeepsNativePathAuthorization(t *testing.T) {
	store, _, path := rootBindingRegistrationDirectory(t)
	registration, err := store.authorizePath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close()
	if err := registration.prepare(context.Background(), captureRootBindingRegistrationTopology); err != nil {
		t.Fatalf("a valid native directory failed unsupported-platform registration: %v", err)
	}
	if registration.document != nil || registration.anchor == nil {
		t.Fatal("unsupported platform did not retain an unbound authorized anchor")
	}
	if err := registration.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
}
