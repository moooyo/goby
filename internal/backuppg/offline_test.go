//go:build linux

package backuppg

import (
	"net/url"
	"strings"
	"testing"
)

// These tests exercise only deployment-URI parsing. They never connect to a
// database, execute a PostgreSQL program, or open certificate files. Archive
// provenance and the RestoreOffline call boundary require separate coverage.
func TestOfflineSourceIdentityPreservesExplicitDecodedNames(t *testing.T) {
	for name, fixture := range map[string]struct {
		uri      string
		database string
		user     string
	}{
		"explicit_tcp":                  {"postgresql://Source_User:private-password@db.example.invalid:5433/Source_DB?sslmode=require", "Source_DB", "Source_User"},
		"scheme_alias_and_default_port": {"postgres://Source_User@db.example.invalid/Source_DB?sslmode=disable", "Source_DB", "Source_User"},
		"ipv6":                          {"postgresql://Source_User:private-password@[2001:db8::17]:5432/Source_DB?sslmode=require", "Source_DB", "Source_User"},
		"escaped_names":                 {"postgresql://Source%20Operator:private-password@db.example.invalid/Media%20Database?sslmode=require", "Media Database", "Source Operator"},
		"literal_plus":                  {"postgresql://Source+Operator:private+password@db.example.invalid/Media+Database?sslmode=require", "Media+Database", "Source+Operator"},
		"quoted_names":                  {"postgresql://%22Source%20Operator%22:private-password@db.example.invalid/%22Media%20Database%22?sslmode=require", `"Media Database"`, `"Source Operator"`},
		"leading_and_trailing_space":    {"postgresql://%20Source%20Operator%20:private-password@db.example.invalid/%20Media%20Database%20?sslmode=require", " Media Database ", " Source Operator "},
		"single_percent_decode":         {"postgresql://User%252FName:private-password@db.example.invalid/DB%255CName?sslmode=require", "DB%5CName", "User%2FName"},
		"encoded_uri_delimiters":        {"postgresql://User%40Name:private-password@db.example.invalid/DB%3FName%23Part?sslmode=require", "DB?Name#Part", "User@Name"},
		"userinfo_delimiters":           {"postgresql://User%3AOps%40Node:p%40ss%3Aword@db.example.invalid/Media?sslmode=require", "Media", "User:Ops@Node"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := offlineSourceIdentity(Options{SourceURL: fixture.uri})
			if err != nil {
				t.Fatalf("explicit source identity rejected: %v", err)
			}
			want := databaseIdentity{Database: fixture.database, User: fixture.user}
			if got != want {
				t.Fatal("decoded identity changed or unobserved server-version facts were invented")
			}
		})
	}
}

func TestOfflineSourceIdentityCollapsesEquivalentURIsOnly(t *testing.T) {
	for name, pair := range map[string][2]string{
		"scheme_and_port": {
			"postgresql://Source:one-password@db.example.invalid/Media?sslmode=require",
			"postgres://Source:another-password@db.example.invalid:5432/Media?sslmode=disable",
		},
		"encoded_ascii": {
			"postgresql://Source@db.example.invalid/Media?sslmode=require",
			"postgresql://%53ource@db.example.invalid/%4dedia?sslmode=require",
		},
		"encoded_plus": {
			"postgresql://Source+Ops@db.example.invalid/Media+DB?sslmode=require",
			"postgresql://Source%2bOps@db.example.invalid/Media%2BDB?sslmode=require",
		},
		"host_alias_cannot_hide_equal_names": {
			"postgresql://Source@source-one.invalid:5432/Media?sslmode=require",
			"postgresql://Source@source-two.invalid:6432/Media?sslmode=require",
		},
	} {
		t.Run(name, func(t *testing.T) {
			first, err := offlineSourceIdentity(Options{SourceURL: pair[0]})
			if err != nil {
				t.Fatalf("first explicit URI rejected: %v", err)
			}
			second, err := offlineSourceIdentity(Options{SourceURL: pair[1]})
			if err != nil {
				t.Fatalf("equivalent explicit URI rejected: %v", err)
			}
			if first != second {
				t.Fatal("URI spelling or endpoint aliases changed the database/role comparison identity")
			}
		})
	}
}

func TestOfflineSourceIdentityDoesNotNormalizeDistinctPostgreSQLNames(t *testing.T) {
	for name, names := range map[string][2]string{
		"case":                         {"Source", "source"},
		"whitespace":                   {"Source", " Source "},
		"quotes_are_name_bytes":        {"Source", `"Source"`},
		"plus_is_not_space":            {"Source+Ops", "Source Ops"},
		"percent_is_not_decoded_twice": {"Source%41", "SourceA"},
		"unicode_normalization":        {"Caf\u00e9", "Cafe\u0301"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, field := range []string{"database", "user"} {
				firstURI := offlineIdentityFixtureURI("StableSource", "StableDatabase")
				secondURI := firstURI
				if field == "database" {
					firstURI = offlineIdentityFixtureURI("StableSource", names[0])
					secondURI = offlineIdentityFixtureURI("StableSource", names[1])
				} else {
					firstURI = offlineIdentityFixtureURI(names[0], "StableDatabase")
					secondURI = offlineIdentityFixtureURI(names[1], "StableDatabase")
				}
				first, firstErr := offlineSourceIdentity(Options{SourceURL: firstURI})
				second, secondErr := offlineSourceIdentity(Options{SourceURL: secondURI})
				if firstErr != nil || secondErr != nil {
					t.Fatalf("valid distinct %s names rejected", field)
				}
				if first == second {
					t.Fatalf("distinct %s names collapsed before target comparison", field)
				}
			}
		})
	}
}

func TestOfflineSourceIdentityRejectsIndirectConnectionOverrides(t *testing.T) {
	const base = "postgresql://Source:private-password@db.example.invalid/Media?sslmode=require"
	for name, suffix := range map[string]string{
		"service":                      "&service=alternate",
		"servicefile":                  "&servicefile=/ignored/service.conf",
		"user":                         "&user=Different",
		"same_user_still_override":     "&user=Source",
		"encoded_user":                 "&%75ser=Different",
		"database":                     "&dbname=Different",
		"same_database_still_override": "&dbname=Media",
		"encoded_database":             "&db%6eame=Different",
		"host":                         "&host=other.invalid",
		"encoded_host":                 "&%68ost=other.invalid",
		"hostaddr":                     "&hostaddr=127.0.0.1",
		"port":                         "&port=6432",
		"password":                     "&password=another-password",
		"runtime_options":              "&options=-c%20role=Different",
		"application_name":             "&application_name=other",
		"unknown_database_alias":       "&database=Different",
		"double_encoded_parameter":     "&%2575ser=Different",
		"duplicate_sslmode":            "&sslmode=disable",
		"equal_duplicate_sslmode":      "&sslmode=require",
		"encoded_duplicate_sslmode":    "&ssl%6Dode=require",
		"uppercase_parameter":          "&SSLMODE=require",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := offlineSourceIdentity(Options{SourceURL: base + suffix})
			if err != ErrConfiguration || got != (databaseIdentity{}) {
				t.Fatal("indirect identity or connection override returned a usable identity")
			}
		})
	}
}

func TestOfflineSourceIdentityRejectsImplicitOrAmbiguousURIs(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":               "",
		"keyword_form":        "host=db.example.invalid user=Source dbname=Media sslmode=require",
		"service_form":        "service=source",
		"wrong_scheme":        "https://Source@db.example.invalid/Media?sslmode=require",
		"uppercase_scheme":    "POSTGRESQL://Source@db.example.invalid/Media?sslmode=require",
		"mixed_case_scheme":   "Postgres://Source@db.example.invalid/Media?sslmode=require",
		"implicit_user":       "postgresql://db.example.invalid/Media?sslmode=require",
		"empty_user":          "postgresql://:private-password@db.example.invalid/Media?sslmode=require",
		"implicit_database":   "postgresql://Source@db.example.invalid?sslmode=require",
		"empty_database":      "postgresql://Source@db.example.invalid/?sslmode=require",
		"implicit_host":       "postgresql://Source@/Media?sslmode=require",
		"implicit_sslmode":    "postgresql://Source@db.example.invalid/Media",
		"empty_sslmode":       "postgresql://Source@db.example.invalid/Media?sslmode=",
		"invalid_sslmode":     "postgresql://Source@db.example.invalid/Media?sslmode=unknown",
		"multi_host":          "postgresql://Source@first.invalid,second.invalid/Media?sslmode=require",
		"multi_host_ports":    "postgresql://Source@first.invalid:5432,second.invalid:5433/Media?sslmode=require",
		"socket_host":         "postgresql://Source@%2Fvar%2Frun%2Fpostgresql/Media?sslmode=require",
		"fragment":            "postgresql://Source@db.example.invalid/Media?sslmode=require#alternate",
		"empty_fragment":      "postgresql://Source@db.example.invalid/Media?sslmode=require#",
		"raw_nul":             "postgresql://Source@db.example.invalid/Media\x00?sslmode=require",
		"password_nul":        "postgresql://Source:private%00password@db.example.invalid/Media?sslmode=require",
		"bad_user_escape":     "postgresql://Source%GG@db.example.invalid/Media?sslmode=require",
		"bad_database_escape": "postgresql://Source@db.example.invalid/Media%GG?sslmode=require",
		"query_semicolon":     "postgresql://Source@db.example.invalid/Media?sslmode=require;user=Other",
		"empty_query_field":   "postgresql://Source@db.example.invalid/Media?sslmode=require&",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := offlineSourceIdentity(Options{SourceURL: raw})
			if err != ErrConfiguration || got != (databaseIdentity{}) {
				t.Fatal("implicit or ambiguous source URI returned a usable identity")
			}
		})
	}
}

func TestOfflineSourceIdentityRejectsRawSpaceAndAuthorityDelimiterDivergence(t *testing.T) {
	// pgx follows libpq's component rules: raw leading/trailing spaces are
	// trimmed and the first raw @ ends userinfo. net/url preserves raw path
	// spaces and uses the last @. Offline comparison must reject those URIs.
	for name, raw := range map[string]string{
		"database_leading_space":  "postgresql://Source@db.example.invalid/ Media?sslmode=require",
		"database_trailing_space": "postgresql://Source@db.example.invalid/Media ?sslmode=require",
		"database_both_spaces":    "postgresql://Source@db.example.invalid/ Media ?sslmode=require",
		"database_interior_space": "postgresql://Source@db.example.invalid/Media DB?sslmode=require",
		"user_space":              "postgresql:// Source :private-password@db.example.invalid/Media?sslmode=require",
		"password_space":          "postgresql://Source: private-password @db.example.invalid/Media?sslmode=require",
		"host_space":              "postgresql://Source@ db.example.invalid /Media?sslmode=require",
		"query_space":             "postgresql://Source@db.example.invalid/Media?sslmode=verify-full&sslrootcert=/does-not-exist/root bundle.pem",
		"second_userinfo_at":      "postgresql://Source@Other@db.example.invalid/Media?sslmode=require",
		"password_at":             "postgresql://Source:private@password@db.example.invalid/Media?sslmode=require",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := offlineSourceIdentity(Options{SourceURL: raw})
			if err != ErrConfiguration || got != (databaseIdentity{}) {
				t.Fatal("URI parser divergence returned a usable source identity")
			}
		})
	}
	got, err := offlineSourceIdentity(Options{SourceURL: "postgresql://%20Source%40Ops%20:%20private%40password%20@db.example.invalid/%20Media%20DB%20?sslmode=verify-full&sslrootcert=/does-not-exist/root%20bundle.pem"})
	if err != nil || got != (databaseIdentity{Database: " Media DB ", User: " Source@Ops "}) {
		t.Fatal("explicitly escaped name delimiters or spaces were lost")
	}
}

func TestOfflineSourceIdentityRequiresUnambiguousQueryPlusEncoding(t *testing.T) {
	const base = "postgresql://Source+Ops:private+password@db.example.invalid/Media+DB?sslmode=verify-full"
	for name, suffix := range map[string]string{
		"raw_plus_value": "&sslrootcert=/does-not-exist/root+bundle.pem",
		"raw_plus_name":  "&sslroot+cert=/does-not-exist/roots.pem",
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := offlineSourceIdentity(Options{SourceURL: base + suffix}); err != ErrConfiguration || got != (databaseIdentity{}) {
				t.Fatal("ambiguous raw query plus returned a usable identity")
			}
		})
	}
	for _, encodedPlus := range []string{"%2B", "%2b"} {
		got, err := offlineSourceIdentity(Options{SourceURL: base + "&sslrootcert=/does-not-exist/root" + encodedPlus + "bundle.pem"})
		if err != nil || got != (databaseIdentity{Database: "Media+DB", User: "Source+Ops"}) {
			t.Fatal("explicit encoded query plus or literal identity plus changed")
		}
	}
}

func TestOfflineSourceIdentityRejectsTruncatedAndUnsafeNames(t *testing.T) {
	for name, value := range map[string]string{
		"empty":                    "",
		"ascii_space":              "   ",
		"unicode_space":            "\u00a0\u2002\u3000",
		"ascii_64_bytes":           strings.Repeat("a", 64),
		"utf8_64_bytes":            strings.Repeat("\u00e9", 32),
		"utf8_64_bytes_three_byte": strings.Repeat("\u754c", 21) + "a",
		"nul":                      "Before\x00After",
		"tab":                      "Before\tAfter",
		"newline":                  "Before\nAfter",
		"carriage_return":          "Before\rAfter",
		"ascii_control":            "Before\x01After",
		"delete":                   "Before\x7fAfter",
		"unicode_control":          "Before\u0085After",
		"slash":                    "Before/After",
		"backslash":                "Before\\After",
		"invalid_utf8":             "Before\xffAfter",
		"overlong_utf8_slash":      "Before\xc0\xafAfter",
		"utf8_surrogate":           "Before\xed\xa0\x80After",
		"truncated_utf8":           "Before\xe4\xb8",
	} {
		t.Run(name, func(t *testing.T) {
			for _, field := range []string{"database", "user"} {
				raw := offlineIdentityFixtureURI("StableSource", value)
				if field == "user" {
					raw = offlineIdentityFixtureURI(value, "StableDatabase")
				}
				got, err := offlineSourceIdentity(Options{SourceURL: raw})
				if err != ErrConfiguration || got != (databaseIdentity{}) {
					t.Fatalf("unsafe %s produced a comparison identity", field)
				}
			}
		})
	}
}

func TestOfflineSourceIdentityAcceptsNamesAtDecodedByteBoundary(t *testing.T) {
	for name, value := range map[string]string{
		"ascii_63_bytes":              strings.Repeat("a", 63),
		"utf8_63_bytes":               strings.Repeat("\u00e9", 31) + "a",
		"utf8_three_byte_63":          strings.Repeat("\u754c", 21),
		"mixed_quoted_name":           `Media "Operations" - East`,
		"sql_shaped_name":             `Media; SELECT 1;--`,
		"surrounding_spaces":          " Media Operations ",
		"valid_replacement_character": "Name\ufffd",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := offlineSourceIdentity(Options{SourceURL: offlineIdentityFixtureURI(value, value)})
			if err != nil || got != (databaseIdentity{Database: value, User: value}) {
				t.Fatal("safe decoded name was rejected, truncated, or normalized")
			}
		})
	}
}

func TestOfflineSourceIdentityDoesNotInspectSourceOrRuntimeDependencies(t *testing.T) {
	// An identity-only parser must not discover these files, start these
	// programs, resolve this endpoint, or consult ambient connection identity.
	// The deliberately absent certificate paths also reject a hidden call to
	// pgx.ParseConfig, which would load those files even without connecting.
	for name, value := range map[string]string{
		"PGDATABASE": "AmbientDatabase", "PGUSER": "AmbientUser", "PGSERVICE": "AmbientService",
		"PGSERVICEFILE": "/does-not-exist/ambient-service.conf", "PGHOST": "ambient.invalid",
		"PGPASSFILE": "/does-not-exist/ambient-password-file",
		"PGPASSWORD": "ambient-private-password", "PGOPTIONS": "-c role=AmbientRole",
	} {
		t.Setenv(name, value)
	}
	options := Options{
		SourceURL: "postgresql://Source:configured-private-password@never-resolve-source.invalid:5433/Media?sslmode=verify-full&sslrootcert=/does-not-exist/offline-roots.pem&sslcert=/does-not-exist/offline-client.pem&sslkey=/does-not-exist/offline-client.key",
		PGDump:    "/does-not-exist/pg_dump",
		PGRestore: "/does-not-exist/pg_restore",
	}
	got, err := offlineSourceIdentity(options)
	if err != nil || got != (databaseIdentity{Database: "Media", User: "Source"}) {
		t.Fatal("identity parsing depends on source availability, ambient identity, programs, or certificate files")
	}
	if got, err := offlineSourceIdentity(Options{}); err != ErrConfiguration || got != (databaseIdentity{}) {
		t.Fatal("ambient connection identity replaced missing explicit deployment configuration")
	}
}

func offlineIdentityFixtureURI(user, database string) string {
	return "postgresql://" + url.UserPassword(user, "fixture-private-password").String() + "@db.example.invalid/" + url.PathEscape(database) + "?sslmode=require"
}
