package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/transcode"
)

func recoveryConfigEnvironment(t *testing.T) {
	t.Helper()
	observabilityConfigEnvironment(t)
	for _, name := range []string{
		"GOBY_RECOVERY_STATE_DIR", "GOBY_RECOVERY_OPERATIONS_DIR", "GOBY_RECOVERY_DATABASE_URL", "GOBY_BACKUP_DIR",
		"GOBY_BACKUP_MAX_OBJECT_BYTES", "GOBY_BACKUP_MAX_TOTAL_BYTES", "GOBY_BACKUP_MAX_OBJECTS",
		"GOBY_BACKUP_MIN_FREE_BYTES", "GOBY_BACKUP_TIMEOUT", "GOBY_PG_DUMP", "GOBY_PG_RESTORE",
	} {
		t.Setenv(name, "")
	}
}

func TestRecoveryEnvironmentDefaultsAndOverrides(t *testing.T) {
	recoveryConfigEnvironment(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := RecoveryConfig{
		Directory: "/var/lib/goby/recovery", OperationsDirectory: "/var/lib/goby/recovery-operations", Backups: backupstore.Config{}.WithDefaults(),
		PGDumpPath: "pg_dump", PGRestorePath: "pg_restore", OperationTimeout: 30 * time.Minute,
	}
	if c.Recovery != want || (RecoveryConfig{}).WithDefaults() != want {
		t.Fatal("production recovery policy did not retain explicit defaults")
	}
	for name, value := range map[string]string{
		"GOBY_RECOVERY_STATE_DIR": "/srv/goby/state", "GOBY_BACKUP_DIR": "/srv/goby/backups",
		"GOBY_RECOVERY_DATABASE_URL":   "postgres://recovery_user:target-secret@secondary.example/recovery_db?sslmode=require",
		"GOBY_BACKUP_MAX_OBJECT_BYTES": "1048576", "GOBY_BACKUP_MAX_TOTAL_BYTES": "4194304",
		"GOBY_BACKUP_MAX_OBJECTS": "4", "GOBY_BACKUP_MIN_FREE_BYTES": "8192",
		"GOBY_BACKUP_TIMEOUT": "12m30s", "GOBY_PG_DUMP": "/opt/postgresql/bin/pg_dump", "GOBY_PG_RESTORE": "pg_restore-17",
	} {
		t.Setenv(name, value)
	}
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Recovery.Directory != "/srv/goby/state" || c.Recovery.OperationsDirectory != "/srv/goby/state-operations" || c.Recovery.Backups.Directory != "/srv/goby/backups" ||
		c.Recovery.Backups.MaxObjectBytes != 1048576 || c.Recovery.Backups.MaxTotalBytes != 4194304 ||
		c.Recovery.Backups.MaxObjects != 4 || c.Recovery.Backups.MinFreeBytes != 8192 ||
		c.Recovery.OperationTimeout != 12*time.Minute+30*time.Second || c.Recovery.PGDumpPath != "/opt/postgresql/bin/pg_dump" ||
		c.Recovery.PGRestorePath != "pg_restore-17" || c.Recovery.DatabaseURL == "" {
		t.Fatal("explicit recovery policy was lost or replaced")
	}
}

func TestRecoveryOperationsDirectoryDefaultsFollowFinalStateDirectory(t *testing.T) {
	recoveryConfigEnvironment(t)
	for _, test := range []struct{ name, state, operations, want string }{
		{"default", "", "", "/var/lib/goby/recovery-operations"},
		{"custom_state", "/srv/goby/state", "", "/srv/goby/state-operations"},
		{"explicit_operations", "", "/srv/private/operation-log", "/srv/private/operation-log"},
		{"both_explicit", "/srv/goby/state", "/srv/private/operation-log", "/srv/private/operation-log"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GOBY_RECOVERY_STATE_DIR", test.state)
			t.Setenv("GOBY_RECOVERY_OPERATIONS_DIR", test.operations)
			c, err := Load()
			if err != nil || c.Recovery.OperationsDirectory != test.want {
				t.Fatalf("operation directory default or explicit override changed: %v", err)
			}
			direct := RecoveryConfig{Directory: test.state, OperationsDirectory: test.operations}.WithDefaults()
			if direct.OperationsDirectory != test.want {
				t.Fatal("direct defaults derive a different operation directory from environment loading")
			}
		})
	}
}

func TestRecoveryZeroValuePreservesDirectConfigFixtures(t *testing.T) {
	c := Config{
		DatabaseURL: "postgres://localhost", PublicURL: "http://localhost:8096",
		ServerName: "Goby", StartupTimeout: time.Minute,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("zero recovery policy changed legacy primary URL validation: %v", err)
	}
	if c.Recovery != (RecoveryConfig{}) {
		t.Fatal("validation silently opted a direct configuration into recovery")
	}
	c.Recovery = (RecoveryConfig{}).WithDefaults()
	if err := c.Validate(); err != nil {
		t.Fatalf("a missing optional recovery URL tightened legacy primary URL validation: %v", err)
	}
	c.Recovery.DatabaseURL = "postgres://recovery_user@localhost/recovery_db?sslmode=disable"
	if err := c.Validate(); err == nil {
		t.Fatal("recovery accepted a primary database identity that cannot be compared")
	}
}

func TestRecoveryEnvironmentNumericBoundsAndExplicitInvalidValues(t *testing.T) {
	recoveryConfigEnvironment(t)
	for _, setting := range []struct {
		name    string
		maximum int64
	}{
		{"GOBY_BACKUP_MAX_OBJECT_BYTES", 1 << 40},
		{"GOBY_BACKUP_MAX_TOTAL_BYTES", 1 << 44},
		{"GOBY_BACKUP_MAX_OBJECTS", 4096},
		{"GOBY_BACKUP_MIN_FREE_BYTES", 1 << 40},
	} {
		for _, value := range []int64{1, setting.maximum} {
			t.Run(setting.name+"/valid/"+strconv.FormatInt(value, 10), func(t *testing.T) {
				t.Setenv("GOBY_BACKUP_MAX_OBJECT_BYTES", "1")
				t.Setenv("GOBY_BACKUP_MAX_TOTAL_BYTES", strconv.FormatInt(1<<44, 10))
				t.Setenv(setting.name, strconv.FormatInt(value, 10))
				if _, err := Load(); err != nil {
					t.Fatalf("inclusive backup policy boundary was rejected: %v", err)
				}
			})
		}
		for index, value := range []string{
			"0", "-1", strconv.FormatInt(setting.maximum+1, 10), "1.5", " 1", "1 ",
			"9223372036854775808", "private-value?password=never-echo",
		} {
			t.Run(fmt.Sprintf("%s/invalid/%d", setting.name, index), func(t *testing.T) {
				t.Setenv(setting.name, value)
				c, err := Load()
				if err == nil || !strings.Contains(err.Error(), setting.name) || strings.Contains(err.Error(), "never-echo") {
					t.Fatalf("invalid backup policy did not return a fixed setting error: %v", err)
				}
				if c.Recovery != (RecoveryConfig{}) {
					t.Fatal("invalid environment returned a partial recovery policy")
				}
			})
		}
	}
	t.Setenv("GOBY_BACKUP_MAX_OBJECT_BYTES", "2")
	t.Setenv("GOBY_BACKUP_MAX_TOTAL_BYTES", "1")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOBY_BACKUP_MAX_TOTAL_BYTES") {
		t.Fatal("a backup object limit larger than the total quota was accepted")
	}
}

func TestRecoveryOperationTimeoutBounds(t *testing.T) {
	recoveryConfigEnvironment(t)
	for _, value := range []string{"1m", "1m0.000000001s", "10m30s", "30m"} {
		t.Run("valid/"+value, func(t *testing.T) {
			t.Setenv("GOBY_BACKUP_TIMEOUT", value)
			if _, err := Load(); err != nil {
				t.Fatalf("bounded operation timeout was rejected: %v", err)
			}
		})
	}
	for _, value := range []string{"0", "-1m", "59.999999999s", "30m0.000000001s", "24h", " 1m", "private-timeout"} {
		t.Run("invalid/"+value, func(t *testing.T) {
			t.Setenv("GOBY_BACKUP_TIMEOUT", value)
			if _, err := Load(); err == nil || err.Error() != "GOBY_BACKUP_TIMEOUT must be a Go duration between 1m and 30m" {
				t.Fatalf("invalid operation timeout was accepted or exposed: %v", err)
			}
		})
	}
}

func TestRecoveryLinuxDirectoriesAndExecutables(t *testing.T) {
	recoveryConfigEnvironment(t)
	for _, setting := range []string{"GOBY_RECOVERY_STATE_DIR", "GOBY_BACKUP_DIR", "GOBY_RECOVERY_OPERATIONS_DIR"} {
		for index, value := range []string{
			" ", ".", "relative/path", "/", "/owned/../other", "/owned/", "//owned", `C:\owned`,
			`/owned\child`, "/owned\nchild", "/owned/" + string([]byte{0xff}), strings.Repeat("/a", 2049),
		} {
			t.Run(fmt.Sprintf("%s/invalid/%d", setting, index), func(t *testing.T) {
				t.Setenv(setting, value)
				if _, err := Load(); err == nil || !strings.Contains(err.Error(), setting) {
					t.Fatalf("invalid Linux data directory was accepted: %v", err)
				}
			})
		}
		for _, value := range []string{"/srv/owned/path", "/srv/owned directory/path", "/var/lib/goby/backups-old"} {
			t.Run(setting+"/valid/"+value, func(t *testing.T) {
				t.Setenv(setting, value)
				if _, err := Load(); err != nil {
					t.Fatalf("canonical Linux directory was rejected: %v", err)
				}
			})
		}
	}
	for _, setting := range []string{"GOBY_PG_DUMP", "GOBY_PG_RESTORE"} {
		for index, value := range []string{" ", ".", "..", "/", "./pg_dump", "bin/pg_dump", "/bin/../pg_dump", "/bin/pg_dump/", `C:\pg_dump`, "pg\ndump", string([]byte{0xff}), strings.Repeat("p", 4097)} {
			t.Run(fmt.Sprintf("%s/invalid/%d", setting, index), func(t *testing.T) {
				t.Setenv(setting, value)
				if _, err := Load(); err == nil || !strings.Contains(err.Error(), setting) {
					t.Fatalf("invalid executable path was accepted: %v", err)
				}
			})
		}
		for _, value := range []string{"pg_dump", "pg_restore-17", "/opt/PostgreSQL 17/bin/pg_dump"} {
			t.Run(setting+"/valid/"+value, func(t *testing.T) {
				t.Setenv(setting, value)
				if _, err := Load(); err != nil {
					t.Fatalf("literal executable name or path was rejected: %v", err)
				}
			})
		}
	}
}

func TestRecoveryDirectPathPolicyRejectsNUL(t *testing.T) {
	// Environment variables cannot contain NUL, but direct configurations can.
	for _, mutate := range []func(*RecoveryConfig){
		func(c *RecoveryConfig) { c.Directory = "/owned\x00state" },
		func(c *RecoveryConfig) { c.OperationsDirectory = "/owned\x00operations" },
		func(c *RecoveryConfig) { c.Backups.Directory = "/owned\x00backups" },
		func(c *RecoveryConfig) { c.PGDumpPath = "pg\x00dump" },
		func(c *RecoveryConfig) { c.PGRestorePath = "pg\x00restore" },
	} {
		c := (RecoveryConfig{}).WithDefaults()
		mutate(&c)
		if err := c.Validate(""); err == nil {
			t.Fatal("a direct path containing NUL was accepted")
		}
	}
}

func TestRecoveryOperationsDirectoryRemainsDisjoint(t *testing.T) {
	recoveryConfigEnvironment(t)
	t.Setenv("GOBY_RECOVERY_STATE_DIR", "/owned/state")
	t.Setenv("GOBY_BACKUP_DIR", "/owned/backups")
	t.Setenv("GOBY_MEDIA_ROOTS", "/media/library")
	for _, directory := range []string{
		"/owned/state", "/owned/state/operations", "/owned/backups", "/owned/backups/operations", "/owned",
		"/var/cache/goby/transcodes", "/var/cache/goby/transcodes/operations", "/var/cache/goby",
		"/var/log/goby", "/var/log/goby/operations", "/var/log",
		"/media/library", "/media/library/operations", "/media",
	} {
		t.Run(directory, func(t *testing.T) {
			t.Setenv("GOBY_RECOVERY_OPERATIONS_DIR", directory)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOBY_RECOVERY_OPERATIONS_DIR") {
				t.Fatalf("overlapping operation directory was accepted: %v", err)
			}
		})
	}
	for _, directory := range []string{"/owned/state-operations", "/owned/backups-operations", "/media/library-operations"} {
		t.Run("disjoint/"+directory, func(t *testing.T) {
			t.Setenv("GOBY_RECOVERY_OPERATIONS_DIR", directory)
			if _, err := Load(); err != nil {
				t.Fatalf("a sibling operation directory was mistaken for nested storage: %v", err)
			}
		})
	}
	t.Setenv("GOBY_BACKUP_DIR", "/owned/state-operations")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOBY_RECOVERY_OPERATIONS_DIR") {
		t.Fatal("derived operation storage was allowed to overlap explicit backup storage")
	}
}

func TestRecoveryDirectoriesRemainDisjointFromManagedAndMediaData(t *testing.T) {
	recoveryConfigEnvironment(t)
	for _, test := range []struct{ state, backups string }{
		{"/owned", "/owned"}, {"/owned", "/owned/backups"}, {"/owned/state", "/owned"},
		{"/var/cache/goby/transcodes/state", "/owned/backups"}, {"/owned/state", "/var/log/goby/backups"},
		{"/owned", "/media/library/backups"}, {"/media", "/owned/backups"},
	} {
		t.Run(test.state+"/"+test.backups, func(t *testing.T) {
			t.Setenv("GOBY_RECOVERY_STATE_DIR", test.state)
			t.Setenv("GOBY_BACKUP_DIR", test.backups)
			t.Setenv("GOBY_MEDIA_ROOTS", "/media/library")
			if _, err := Load(); err == nil {
				t.Fatal("overlapping persistent storage was accepted")
			}
		})
	}
	t.Setenv("GOBY_RECOVERY_STATE_DIR", "/owned/state")
	t.Setenv("GOBY_BACKUP_DIR", "/owned/state-backups")
	if _, err := Load(); err != nil {
		t.Fatalf("a shared name prefix was mistaken for nested directories: %v", err)
	}
}

func TestRecoveryDatabaseRequiresExplicitIndependentIdentity(t *testing.T) {
	recoveryConfigEnvironment(t)
	base := "postgres://recovery_user:never-echo@secondary.example/recovery_db?sslmode=disable"
	for name, value := range map[string]string{
		"missing_host":     "postgres://recovery_user@/recovery_db?sslmode=disable",
		"missing_user":     "postgres://secondary.example/recovery_db?sslmode=disable",
		"missing_database": "postgres://recovery_user@secondary.example/?sslmode=disable",
		"missing_sslmode":  "postgres://recovery_user@secondary.example/recovery_db",
		"same_user":        "postgres://goby@secondary.example/recovery_db?sslmode=disable",
		"same_database":    "postgres://recovery_user@secondary.example/goby?sslmode=disable",
		"encoded_user":     "postgres://%67oby@secondary.example/recovery_db?sslmode=disable",
		"encoded_database": "postgres://recovery_user@secondary.example/%67oby?sslmode=disable",
		"keyword_dsn":      "host=secondary.example user=recovery_user dbname=recovery_db",
		"user_override":    base + "&user=other", "encoded_user_override": base + "&%75ser=other",
		"database_override": base + "&dbname=other", "host_override": base + "&host=other",
		"service": base + "&service=other", "options": base + "&options=-c%20role=goby", "unknown": base + "&unexpected=true",
		"duplicate_sslmode": base + "&sslmode=require", "encoded_duplicate": base + "&ssl%6dode=require",
		"malformed_query": base + "&sslrootcert=%GG", "empty_query_field": base + "&", "fragment": base + "#private", "empty_fragment": base + "#",
		"bad_port":             "postgres://recovery_user@secondary.example:65536/recovery_db?sslmode=disable",
		"noncanonical_port":    "postgres://recovery_user@secondary.example:05432/recovery_db?sslmode=disable",
		"multihost":            "postgres://recovery_user@secondary.example,other.example/recovery_db?sslmode=disable",
		"invalid_ipv6":         "postgres://recovery_user@[:::]/recovery_db?sslmode=disable",
		"nul_password":         "postgres://recovery_user:%00@secondary.example/recovery_db?sslmode=disable",
		"implicit_trust":       "postgres://recovery_user@secondary.example/recovery_db?sslmode=verify-full",
		"ambiguous_query_plus": base + "&sslrootcert=/owned/root+bundle.pem",
		"raw_space":            "postgres://recovery_user@secondary.example/ recovery_db?sslmode=disable",
		"uppercase_scheme":     "Postgres://recovery_user@secondary.example/recovery_db?sslmode=disable",
		"ambiguous_at":         "postgres://recovery_user:pass@word@secondary.example/recovery_db?sslmode=disable",
		"truncated_role":       "postgres://" + strings.Repeat("r", 64) + "@secondary.example/recovery_db?sslmode=disable",
		"truncated_database":   "postgres://recovery_user@secondary.example/" + strings.Repeat("d", 64) + "?sslmode=disable",
		"multibyte_name_limit": "postgres://recovery_user@secondary.example/" + strings.Repeat("%C3%A9", 32) + "?sslmode=disable",
		"oversized":            base + strings.Repeat("x", 8193),
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GOBY_RECOVERY_DATABASE_URL", value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOBY_RECOVERY_DATABASE_URL") || strings.Contains(err.Error(), "never-echo") {
				t.Fatalf("invalid recovery URL was accepted or exposed: %v", err)
			}
		})
	}
	for name, value := range map[string]string{
		"explicit":       base,
		"encoded_spaces": "postgres://%20recovery_user%20@secondary.example/%20recovery_db%20?sslmode=disable",
		"encoded_at":     "postgres://recovery_user:pass%40word@secondary.example/recovery_db?sslmode=disable",
		"maximum_name":   "postgres://recovery_user@secondary.example/" + strings.Repeat("d", 63) + "?sslmode=disable",
		"ipv6":           "postgresql://recovery_user:never-echo@[::1]:5433/recovery_db?sslmode=require",
		"system_trust":   "postgres://recovery_user@secondary.example/recovery_db?sslmode=verify-full&sslrootcert=system",
		"explicit_trust": "postgres://recovery_user@secondary.example/recovery_db?sslmode=verify-ca&sslrootcert=%2Fowned%2Froot%2Bbundle.pem&connect_timeout=30",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GOBY_RECOVERY_DATABASE_URL", value)
			if _, err := Load(); err != nil {
				t.Fatalf("explicit independent recovery URL was rejected: %v", err)
			}
		})
	}
}

func backupDefaultsConfigFixture() Config {
	return Config{
		ListenAddress: "127.0.0.1:18096", DatabaseURL: "postgres://target_user:target-secret@primary/target_db?sslmode=disable",
		PublicURL: "https://target.example", ServerName: "Target Server", SetupToken: "target-setup-secret", CookieSecure: true,
		WebDirectory: "/target/web", FFmpegPath: "/target/bin/ffmpeg", FFprobePath: "/target/bin/ffprobe",
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, MediaRoots: []string{"/target/approved/media"},
		StartupTimeout: 2 * time.Minute, APIKeyMasterKeyFile: "/target/private/master.key", ActivityRetentionDays: 60,
		Diagnostics: diagnostics.Config{Directory: "/target/logs", MaxFiles: 7, MaxFileBytes: 32768, RetentionDays: 3, MinFreeBytes: 8192},
		Recovery:    RecoveryConfig{Directory: "/target/state", OperationsDirectory: "/target/private-operation-log", DatabaseURL: "postgres://inactive:inactive-secret@secondary/inactive_db?sslmode=disable", Backups: backupstore.Config{Directory: "/target/backups"}, PGDumpPath: "/target/pg_dump", PGRestorePath: "/target/pg_restore", OperationTimeout: time.Minute},
		Transcoding: TranscodingConfig{
			Enabled: false, CacheDirectory: "/target/cache", Threads: 3, MaxJobs: 3, MaxUserJobs: 2, MaxSessionJobs: 1,
			MaxQueueJobs: 5, MaxRetainedJobs: 9, MaxCacheBytes: 1000000, MaxJobBytes: 500000, MinFreeBytes: 1000,
			MaxBitrate: 3000000, MaxWidth: 1280, MaxHeight: 720, MaxAudioChannels: 2,
			Hardware: transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD128"},
		},
	}
}

func TestBackupDefaultsEncodingUsesOnlyLogicalAllowlist(t *testing.T) {
	source := backupDefaultsConfigFixture()
	data, err := EncodeBackupDefaults(source)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "serverName", "maxBitrate", "maxWidth", "maxHeight", "maxAudioChannels"} {
		if _, present := fields[key]; !present {
			t.Fatalf("logical backup field %s is absent", key)
		}
	}
	if len(fields) != 6 || bytes.Contains(data, []byte("target-secret")) || bytes.Contains(data, []byte("/target/")) || bytes.Contains(data, []byte("vaapi")) {
		t.Fatal("backup defaults contain a field outside the logical allowlist")
	}
	got, err := DecodeBackupDefaults(data)
	want, sourceErr := BackupDefaultsFromConfig(source)
	if err != nil || sourceErr != nil || got != want {
		t.Fatalf("logical deployment defaults changed during round trip: %v", err)
	}
}

func TestBackupDefaultsApplyPreservesEveryTargetOperationalSetting(t *testing.T) {
	target := backupDefaultsConfigFixture()
	d := BackupDefaults{Version: 1, ServerName: "Restored Server", MaxBitrate: 9000000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 6}
	want := target
	want.ServerName = d.ServerName
	want.Transcoding.MaxBitrate, want.Transcoding.MaxWidth = d.MaxBitrate, d.MaxWidth
	want.Transcoding.MaxHeight, want.Transcoding.MaxAudioChannels = d.MaxHeight, d.MaxAudioChannels
	got, err := d.Apply(target)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("restoring logical defaults changed target secrets, approved roots, or operating policy")
	}
	if !reflect.DeepEqual(target, backupDefaultsConfigFixture()) {
		t.Fatal("applying defaults mutated the caller's original configuration")
	}
	d.MaxBitrate = 0
	got, err = d.Apply(target)
	if !errors.Is(err, ErrInvalidBackupDefaults) || !reflect.DeepEqual(got, target) {
		t.Fatal("invalid defaults partially changed the target configuration")
	}
}

func TestBackupDefaultsRejectsInvalidArtifacts(t *testing.T) {
	valid := `{"version":1,"serverName":"Archive Server","maxBitrate":20000000,"maxWidth":1920,"maxHeight":1080,"maxAudioChannels":8}`
	invalid := map[string][]byte{
		"empty": nil, "array": []byte(`[]`), "null": []byte(`null`), "string": []byte(`"defaults"`),
		"unknown":           []byte(strings.Replace(valid, "{", `{"databaseURL":"never-restore",`, 1)),
		"case_variant":      []byte(strings.Replace(valid, "serverName", "ServerName", 1)),
		"duplicate":         []byte(strings.Replace(valid, "{", `{"version":1,`, 1)),
		"escaped_duplicate": []byte(strings.Replace(valid, "{", `{"vers\u0069on":1,`, 1)),
		"nested":            []byte(strings.Replace(valid, `"Archive Server"`, `{"name":"Archive Server"}`, 1)),
		"trailing_object":   []byte(valid + `{}`), "trailing_scalar": []byte(valid + ` true`), "trailing_garbage": []byte(valid + `x`),
		"bom": append([]byte{0xef, 0xbb, 0xbf}, []byte(valid)...), "oversized": []byte(valid + strings.Repeat(" ", MaxBackupDefaultsBytes)),
		"raw_invalid_utf8":         bytes.Replace([]byte(valid), []byte("Archive Server"), []byte{0xff}, 1),
		"lone_high_surrogate":      []byte(strings.Replace(valid, "Archive Server", `\ud800`, 1)),
		"lone_low_surrogate":       []byte(strings.Replace(valid, "Archive Server", `\udc00`, 1)),
		"reversed_surrogates":      []byte(strings.Replace(valid, "Archive Server", `\udc00\ud800`, 1)),
		"high_then_regular_escape": []byte(strings.Replace(valid, "Archive Server", `\ud800\u0061`, 1)),
		"high_then_literal":        []byte(strings.Replace(valid, "Archive Server", `\ud800x`, 1)),
	}
	for field, badValues := range map[string][]string{
		"version":    {`0`, `2`, `"1"`, `1.0`, `null`},
		"serverName": {`""`, `" "`, `"\u0000"`, `null`, `9`, strconv.Quote(strings.Repeat("x", 129))},
		"maxBitrate": {`0`, `-1`, `1000000001`, `1.5`, `"20000000"`, `null`, `9223372036854775808`},
		"maxWidth":   {`0`, `8193`, `null`}, "maxHeight": {`0`, `8193`, `null`}, "maxAudioChannels": {`0`, `9`, `null`},
	} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(valid), &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, field)
		missing, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		invalid["missing_"+field] = missing
		for index, raw := range badValues {
			fields[field] = json.RawMessage(raw)
			data, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			invalid[fmt.Sprintf("%s/%d", field, index)] = data
		}
	}
	for name, data := range invalid {
		t.Run(name, func(t *testing.T) {
			got, err := DecodeBackupDefaults(data)
			if !errors.Is(err, ErrInvalidBackupDefaults) || got != (BackupDefaults{}) {
				t.Fatalf("malformed artifact was accepted or returned partial defaults: %v", err)
			}
		})
	}
}

func TestBackupDefaultsAcceptsLosslessUnicodeWhitespaceAndFieldOrder(t *testing.T) {
	for _, name := range []string{`\ud83d\udc1f Server`, `\u0047oby`, `Goby \\ud800`, "Goby \ufffd"} {
		data := []byte(" \n\t" + `{"maxAudioChannels":1,"maxHeight":1,"maxWidth":1,"maxBitrate":1,"serverName":"` + name + `","version":1}` + "\r\n")
		if _, err := DecodeBackupDefaults(data); err != nil {
			t.Fatalf("lossless JSON representation was rejected: %v", err)
		}
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.ServerName = "" },
		func(c *Config) { c.ServerName = string([]byte{0xff}) },
		func(c *Config) { c.Transcoding.MaxBitrate = 0 },
		func(c *Config) { c.Transcoding.MaxWidth = 8193 },
		func(c *Config) { c.Transcoding.MaxHeight = 8193 },
		func(c *Config) { c.Transcoding.MaxAudioChannels = 9 },
	} {
		c := backupDefaultsConfigFixture()
		mutate(&c)
		if data, err := EncodeBackupDefaults(c); !errors.Is(err, ErrInvalidBackupDefaults) || data != nil {
			t.Fatal("invalid source defaults were encoded into an archive")
		}
	}
}
