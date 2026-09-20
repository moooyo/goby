// Package notificationjournal records bounded source facts in caller-owned
// transactions. It has no identity, catalog, callback, vault, or network owner.
package notificationjournal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrJournal = errors.New("notification source journal unavailable")
var ErrCapacity = errors.New("notification source capacity reached")

type Executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}
type Reference struct {
	Kind      string `json:"Kind"`
	ID        string `json:"Id"`
	LibraryID string `json:"LibraryId,omitempty"`
	SourceID  string `json:"SourceId,omitempty"`
}

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure random unavailable")
	}
	return hex.EncodeToString(b[:])
}

func ValidID(id string) bool {
	return id != "" && len(id) <= 256 && utf8.ValidString(id) && strings.TrimSpace(id) == id && strings.IndexFunc(id, unicode.IsControl) < 0
}

func RecordCatalog(ctx context.Context, tx Executor, mutationID string, refs []Reference, resync bool) error {
	if len(refs) > 4096 {
		refs = []Reference{}
		resync = true
	}
	return record(ctx, tx, mutationID, "CatalogInvalidated", "", refs, false, resync)
}
func RecordUserData(ctx context.Context, tx Executor, userID string, ref Reference, recursive bool) error {
	return record(ctx, tx, NewID(), "UserDataInvalidated", userID, []Reference{ref}, recursive, false)
}
func RecordUserResync(ctx context.Context, tx Executor, userID string) error {
	return record(ctx, tx, NewID(), "UserDataInvalidated", userID, []Reference{}, false, true)
}
func record(ctx context.Context, tx Executor, id, kind, user string, refs []Reference, recursive, resync bool) error {
	if tx == nil || len(id) != 32 || kind == "UserDataInvalidated" && !ValidID(user) || len(refs) > 4096 {
		return ErrJournal
	}
	for _, ref := range refs {
		if !ValidID(ref.ID) || ref.Kind != "Item" && ref.Kind != "Entity" && ref.Kind != "Library" || ref.LibraryID != "" && !ValidID(ref.LibraryID) || ref.SourceID != "" && (!ValidID(ref.SourceID) || ref.LibraryID == "" || ref.Kind == "Entity") {
			return ErrJournal
		}
	}
	if refs == nil {
		refs = []Reference{}
	}
	raw, err := json.Marshal(refs)
	if err != nil {
		return ErrJournal
	}
	if len(raw) > 524288 {
		if kind != "CatalogInvalidated" {
			return ErrJournal
		}
		raw = []byte("[]")
		resync = true
	}
	_, err = tx.Exec(ctx, `SELECT goby_record_notification_source($1,$2,NULLIF($3,''),$4::jsonb,$5,$6)`, id, kind, user, raw, recursive, resync)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "P0001" && (pgErr.Message == "notification_source_capacity" || pgErr.Message == "notification_source_scope_required") {
			return ErrCapacity
		}
		return ErrJournal
	}
	return nil
}
