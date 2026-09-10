package backuppg

import (
	"context"
	"io"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
)

// RestoreOffline restores into a distinct, empty target without contacting the
// original database. Options.SourceURL MUST come from the operator's trusted
// deployment configuration, including its environment, unit, or configuration
// file. It must never come from an archive or an HTTP request payload.
//
// The original database and role names are parsed exactly and compared with
// the target's actual identity, even across different hosts. No source DNS,
// connection, TLS-file access, or SQL is performed. This proves separation from
// the configured source identity; it cannot prove that deployment configuration
// itself has not been replaced. The coordinator must enforce that trust boundary
// and exclusively reserve the target before invoking either restore entry point.
// Every target privilege, emptiness, catalog, data, and transaction check is the
// same as Restore. The existing Restore contract still requires a live source.
func RestoreOffline(ctx context.Context, target *pgxpool.Pool, archive io.Reader, facts backupformat.SourceFacts, options Options) (RestoreResult, error) {
	return restoreOffline(ctx, target, archive, facts, options, nil)
}

// RestoreOfflineFinalized atomically includes trusted application validation,
// normalization, and ownership stamping before an offline target can commit.
// SourceURL retains the same protected deployment-configuration boundary as
// RestoreOffline. A nil finalizer is rejected rather than weakening this API.
func RestoreOfflineFinalized(ctx context.Context, target *pgxpool.Pool, archive io.Reader, facts backupformat.SourceFacts, options Options, finalizer Finalizer) (RestoreResult, error) {
	if finalizer == nil {
		return RestoreResult{}, ErrConfiguration
	}
	return restoreOffline(ctx, target, archive, facts, options, finalizer)
}

func restoreOffline(ctx context.Context, target *pgxpool.Pool, archive io.Reader, facts backupformat.SourceFacts, options Options, finalizer Finalizer) (RestoreResult, error) {
	if target == nil || archive == nil {
		return RestoreResult{}, ErrConfiguration
	}
	if err := ctx.Err(); err != nil {
		return RestoreResult{}, err
	}
	source, err := offlineSourceIdentity(options)
	if err != nil {
		return RestoreResult{}, err
	}
	return restoreTarget(ctx, target, archive, facts, options, finalizer, func(ctx context.Context, _ string) (databaseIdentity, error) {
		if err := ctx.Err(); err != nil {
			return databaseIdentity{}, err
		}
		return source, nil
	})
}

func offlineSourceIdentity(options Options) (databaseIdentity, error) {
	// URI parsers can trim raw ASCII spaces at component boundaries. Logical
	// spaces must be explicitly percent encoded to preserve exact name identity.
	if strings.Contains(options.SourceURL, " ") {
		return databaseIdentity{}, ErrConfiguration
	}
	if !strings.HasPrefix(options.SourceURL, "postgresql://") && !strings.HasPrefix(options.SourceURL, "postgres://") {
		return databaseIdentity{}, ErrConfiguration
	}
	values, err := sourceCommandConfig(options)
	if err != nil {
		return databaseIdentity{}, err
	}
	parsed, err := url.Parse(options.SourceURL)
	if err != nil || strings.Contains(parsed.RawQuery, "+") {
		return databaseIdentity{}, ErrConfiguration
	}
	// pgx splits userinfo at the first raw @ whereas net/url uses the last.
	// Literal @ bytes inside credentials therefore require percent encoding.
	authorityStart := strings.Index(options.SourceURL, "://")
	if authorityStart < 0 {
		return databaseIdentity{}, ErrConfiguration
	}
	authority := options.SourceURL[authorityStart+3:]
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	if strings.Count(authority, "@") != 1 {
		return databaseIdentity{}, ErrConfiguration
	}
	for _, name := range []string{values["PGDATABASE"], values["PGUSER"]} {
		// PostgreSQL name values have at most 63 UTF-8 bytes. Do not silently
		// truncate, trim, fold case, normalize Unicode, or decode a second time.
		if len(name) == 0 || len(name) > 63 || !utf8.ValidString(name) || strings.TrimSpace(name) == "" || strings.ContainsAny(name, "/\\") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return databaseIdentity{}, ErrConfiguration
		}
	}
	return databaseIdentity{Database: values["PGDATABASE"], User: values["PGUSER"]}, nil
}
