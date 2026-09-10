package backuppg

import (
	"crypto/tls"
	"net/url"
	"reflect"
	"strings"

	"github.com/jackc/pgx/v5"
)

// validateSourceTLS rejects connection policies changed after URI parsing.
// libpq and pgx decode query '+' differently, so callers must percent-encode
// that byte in URI query values. The password/user components are compared
// separately after the parsers have decoded them.
func validateSourceTLS(raw string, actual *pgx.ConnConfig) error {
	uri, err := url.Parse(raw)
	if err != nil || strings.Contains(uri.RawQuery, "+") {
		return ErrConfiguration
	}
	expected, err := pgx.ParseConfig(raw)
	if err != nil {
		return ErrConfiguration
	}
	// A libpq TLS/authentication parameter that pgx instead forwards as a
	// server GUC has not been implemented with equivalent transport semantics.
	// Also reject ambient PGOPTIONS-derived runtime configuration.
	if len(expected.RuntimeParams) != 0 {
		return ErrConfiguration
	}
	if actual.TLSConfig != nil {
		query := uri.Query()
		if len(actual.TLSConfig.Certificates) > 0 && (query.Get("sslcert") == "" || query.Get("sslkey") == "") {
			return ErrConfiguration
		}
		if actual.TLSConfig.RootCAs != nil && query.Get("sslrootcert") == "" {
			return ErrConfiguration
		}
	}
	if !equivalentTLS(expected.TLSConfig, actual.TLSConfig) || len(expected.Fallbacks) != len(actual.Fallbacks) {
		return ErrConfiguration
	}
	for i, want := range expected.Fallbacks {
		got := actual.Fallbacks[i]
		if got == nil || want == nil || got.Host != want.Host || got.Port != want.Port || !equivalentTLS(want.TLSConfig, got.TLSConfig) {
			return ErrConfiguration
		}
	}
	return nil
}

func equivalentTLS(want, got *tls.Config) bool {
	if want == nil || got == nil {
		return want == got
	}
	// pgx's VerifyPeerCertificate closure is used for verify-ca and is not
	// introspectable. Reject it rather than treating equal code addresses as
	// proof that captured roots or hostname policy are equal. verify-full uses
	// Go's standard verification with explicit root and hostname fields.
	if want.VerifyPeerCertificate != nil || got.VerifyPeerCertificate != nil || want.VerifyConnection != nil || got.VerifyConnection != nil {
		return false
	}
	if want.GetCertificate != nil || got.GetCertificate != nil || want.GetClientCertificate != nil || got.GetClientCertificate != nil || want.GetConfigForClient != nil || got.GetConfigForClient != nil {
		return false
	}
	if want.RootCAs == nil || got.RootCAs == nil {
		if want.RootCAs != got.RootCAs {
			return false
		}
	} else if !want.RootCAs.Equal(got.RootCAs) {
		return false
	}
	if want.ClientCAs == nil || got.ClientCAs == nil {
		if want.ClientCAs != got.ClientCAs {
			return false
		}
	} else if !want.ClientCAs.Equal(got.ClientCAs) {
		return false
	}
	// Root pools were compared semantically above. Clone before masking them;
	// no live pool configuration is mutated by this comparison.
	a, b := want.Clone(), got.Clone()
	a.RootCAs = nil
	b.RootCAs = nil
	a.ClientCAs = nil
	b.ClientCAs = nil
	return reflect.DeepEqual(a, b)
}
