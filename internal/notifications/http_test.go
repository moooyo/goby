package notifications

import (
	"errors"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"net/netip"
	"testing"
	"time"
)

func TestNotificationTemporaryDatabaseFailureRetriesWithoutInventingRevocation(t *testing.T) {
	for _, err := range []error{ErrUnavailable, errors.New("temporary database read"), errSourceVisibilityChanged} {
		state, code, delay := failureDisposition(1, err)
		if state != "pending" || code != "retry" || delay <= 0 {
			t.Fatal("temporary uncertainty permanently lost a durable event")
		}
	}
	if state, code, _ := failureDisposition(5, ErrUnavailable); state != "failed" || code != "retry_exhausted" {
		t.Fatal("database retry escaped its attempt bound")
	}
	for _, err := range []error{identity.ErrUnauthorized, library.ErrForbidden, library.ErrNotFound} {
		state, code, _ := failureDisposition(1, err)
		if state != "suppressed" || code == "retry" {
			t.Fatal("revoked or hidden source was scheduled as an authorized retry")
		}
	}
}

func TestNotificationEgressRequiresExplicitPrivateAddressAdmission(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1", "fe80::1", "100.64.0.1", "192.0.2.1", "0.0.0.0", "224.0.0.1", "::ffff:127.0.0.1"} {
		if allowedAddress(netip.MustParseAddr(raw), nil) {
			t.Fatalf("unapproved egress admitted: %s", raw)
		}
	}
	if !allowedAddress(netip.MustParseAddr("127.0.0.1"), []string{"127.0.0.1/32"}) || allowedAddress(netip.MustParseAddr("127.0.0.2"), []string{"127.0.0.1/32"}) || allowedAddress(netip.MustParseAddr("169.254.169.254"), []string{"0.0.0.0/0"}) {
		t.Fatal("explicit destination policy escaped its exact admitted address")
	}
}
func TestNotificationRetryAfterHasNoOverflowOrUnboundedDelay(t *testing.T) {
	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	for _, raw := range []string{"-1", "3601", "9223372036854775807", "garbage"} {
		if retryAfter(raw, now) != 0 {
			t.Fatal("unbounded retry delay accepted")
		}
	}
	if retryAfter("30", now) != 30*time.Second || retryAfter(now.Add(time.Minute).Format("Mon, 02 Jan 2006 15:04:05 GMT"), now) != time.Minute {
		t.Fatal("bounded Retry-After was not retained")
	}
}
func TestNotificationConfigRejectsLossyRevisionAndSecretFields(t *testing.T) {
	for _, raw := range []string{"", "01", "1.0", "9223372036854775807", "-1"} {
		if _, err := revision(raw, false); err == nil {
			t.Fatal("invalid revision admitted")
		}
	}
	if value, err := revision("9007199254740993", false); err != nil || value != 9007199254740993 {
		t.Fatal("revision lost integer precision")
	}
	secret := "secret with spaces"
	if validateConfig(ConfigUpdate{Revision: "1", AllowedNetworks: []string{}, ReceiverCredential: &secret}) == nil {
		t.Fatal("unsafe header secret accepted")
	}
}
