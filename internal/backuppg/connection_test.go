package backuppg

import (
	"crypto/tls"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestSourceTLSRejectsChangedPoliciesAndAmbiguousQuery(t *testing.T) {
	const raw = "postgresql://backup:private-password@127.0.0.1:5432/backup?sslmode=require"
	config, err := pgx.ParseConfig(raw)
	if err != nil {
		t.Fatal("parse fixture connection policy")
	}
	if err := validateSourceTLS(raw, config); err != nil {
		t.Fatalf("unchanged parsed TLS policy rejected: %v", err)
	}
	changed := config.Copy()
	changed.TLSConfig = changed.TLSConfig.Clone()
	changed.TLSConfig.InsecureSkipVerify = !changed.TLSConfig.InsecureSkipVerify
	if validateSourceTLS(raw, changed) == nil {
		t.Fatal("changed TLS verification policy accepted")
	}
	changed = config.Copy()
	changed.TLSConfig = changed.TLSConfig.Clone()
	changed.TLSConfig.MinVersion = tls.VersionTLS13
	if validateSourceTLS(raw, changed) == nil {
		t.Fatal("changed TLS minimum accepted")
	}
	if validateSourceTLS(raw+"&sslrootcert=/tmp/root+certificate.pem", config) == nil {
		t.Fatal("ambiguous libpq/pgx query decoding accepted")
	}
}

func TestSourceTLSRejectsOpaqueCallbacks(t *testing.T) {
	baseline := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "backup.example"}
	changed := baseline.Clone()
	changed.VerifyConnection = func(tls.ConnectionState) error { return nil }
	if equivalentTLS(baseline, changed) || equivalentTLS(changed, changed) {
		t.Fatal("opaque verification callback accepted as equivalent")
	}
	changed = baseline.Clone()
	changed.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return &tls.Certificate{}, nil }
	if equivalentTLS(baseline, changed) {
		t.Fatal("opaque client certificate callback accepted")
	}
	if !equivalentTLS(nil, nil) || equivalentTLS(nil, baseline) || !equivalentTLS(baseline, baseline.Clone()) {
		t.Fatal("static TLS policy equivalence is incorrect")
	}
}
