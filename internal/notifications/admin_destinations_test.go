package notifications

import (
	"strings"
	"testing"
)

func validAdminWebhookFixture() AdminDestinationInput {
	return AdminDestinationInput{Revision: "0", Name: "Administrator receiver", Transport: "webhook", Enabled: true,
		EventIds: []string{"TaskFinished", "BackupFinished"}, AllowedNetworks: []string{}, Endpoint: "https://receiver.example/events", SMTP: defaultAdminSMTP()}
}

func TestAdminDestinationValidationPreservesExactRevisionsAndBoundedSecrets(t *testing.T) {
	input := validAdminWebhookFixture()
	secret := "administrator-test-secret"
	input.Secret = &secret
	if _, err := validateAdminDestination(input, true); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"00", "01", "1.0", "1e0", "-1", "9223372036854775807"} {
		input.Revision = value
		if _, err := validateAdminDestination(input, true); err == nil {
			t.Fatalf("creation accepted revision %q", value)
		}
	}
	input.Revision = "9007199254740993"
	if _, err := validateAdminDestination(input, false); err != nil {
		t.Fatal("an exact revision above JavaScript's safe integer range was rejected")
	}
	input.Revision = "1"
	for _, value := range []string{"short", strings.Repeat("x", 1025), "secret-with\na-newline"} {
		input.Secret = &value
		if _, err := validateAdminDestination(input, false); err == nil {
			t.Fatal("unsafe webhook secret was accepted")
		}
	}
}

func TestAdminDestinationRejectsUnsafeNetworkAndEmailConfiguration(t *testing.T) {
	for _, endpoint := range []string{"http://receiver.example", "https://user:password@receiver.example", "https://receiver.example/events?token=secret", "https://receiver.example/#fragment"} {
		input := validAdminWebhookFixture()
		input.Endpoint = endpoint
		if _, err := validateAdminDestination(input, true); err == nil {
			t.Fatalf("unsafe webhook endpoint was accepted: %s", endpoint)
		}
	}
	input := validAdminWebhookFixture()
	input.AllowedNetworks = []string{"10.0.0.1/24"}
	if _, err := validateAdminDestination(input, true); err == nil {
		t.Fatal("noncanonical network permission was accepted")
	}
	input = validAdminWebhookFixture()
	input.Transport = "smtp"
	input.SMTP = AdminSMTPConfig{Host: "smtp.example", Port: 587, Security: "starttls", Username: "administrator", From: "goby@example.com", To: []string{"operator@example.com"}}
	password := "short"
	input.Secret = &password
	if _, err := validateAdminDestination(input, true); err != nil {
		t.Fatal("valid encrypted SMTP password was rejected")
	}
	for _, recipient := range []string{"Operator <operator@example.com>", "operator@example.com\r\nBcc: other@example.com", "运维@example.com"} {
		input.SMTP.To = []string{recipient}
		if _, err := validateAdminDestination(input, true); err == nil {
			t.Fatal("unsupported or injected SMTP envelope was accepted")
		}
	}
	input.SMTP.To = []string{"operator@example.com", "Operator@example.com"}
	if _, err := validateAdminDestination(input, true); err == nil {
		t.Fatal("case-duplicated recipient was accepted")
	}
	input.SMTP.To = []string{"operator@example.com"}
	input.SMTP.Security = "none"
	if _, err := validateAdminDestination(input, true); err == nil {
		t.Fatal("unencrypted SMTP was accepted")
	}
}

func TestAdminNotificationEventsDoNotDuplicateBackupLifecycleActions(t *testing.T) {
	if adminEventKind("backup.published") != "BackupFinished" {
		t.Fatal("committed backup publication was not subscribed")
	}
	for _, action := range []string{"backup.finished", "backup.requested", "backup.imported", "backup.downloaded", "restore.applied", "task.admitted"} {
		if adminEventKind(action) != "" {
			t.Fatalf("nonterminal action produced an unintended notification: %s", action)
		}
	}
	input := validAdminWebhookFixture()
	input.EventIds = []string{"BackupFinished", "BackupFinished"}
	if _, err := validateAdminDestination(input, true); err == nil {
		t.Fatal("duplicate subscription was accepted")
	}
}

func TestAdminNotificationSecretEnvelopePreservesShortPasswordsAndRejectsOtherPurposes(t *testing.T) {
	for _, password := range []string{"x", "pass word", "longer-smtp-password"} {
		envelope := encodeAdminSecret(password)
		if got, err := decodeAdminSecret(envelope); err != nil || got != password {
			t.Fatal("SMTP secret did not survive its vault envelope")
		}
	}
	for _, malformed := range []string{"personal-client-token", adminDestinationSecretPrefix, adminDestinationSecretPrefix + "%%%%"} {
		if _, err := decodeAdminSecret(malformed); err == nil {
			t.Fatal("another purpose or malformed envelope was accepted")
		}
	}
}
