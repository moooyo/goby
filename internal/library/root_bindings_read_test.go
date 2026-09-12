package library

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/storagebinding"
)

func TestRootBindingReadUnitRequestIdentifiersFailBeforeDatabaseAccess(t *testing.T) {
	store := &Store{}
	actor := identity.Principal{}
	for _, value := range []string{"", " leading", "trailing ", "control\n", string([]byte{0xff}), strings.Repeat("x", 257)} {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			if _, err := store.GetRootBinding(context.Background(), actor, value, "root"); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid library identifier reached database access: %v", err)
			}
			if _, err := store.GetRootBinding(context.Background(), actor, "library", value); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid root identifier reached database access: %v", err)
			}
			if _, err := store.ListRegisteredRoots(context.Background(), actor, value); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid list identifier reached database access: %v", err)
			}
		})
	}
	if _, err := store.GetRootBinding(nil, actor, "library", "root"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil read context reached database access: %v", err)
	}
	if _, err := store.ListRegisteredRoots(nil, actor, "library"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil list context reached database access: %v", err)
	}
}

func rootBindingReadUnitIdentity(marker byte) RootStorageIdentity {
	return RootStorageIdentity{Version: RootStorageIdentityVersion, Profile: RootStorageIdentityProfile,
		FilesystemUUID: "0123456789abcdef0123456789abcdef", HandleType: int32(marker),
		Handle: []byte{0, 255, marker, 128, 0}}
}

func rootBindingReadUnitSnapshot() RootTopologySnapshot {
	return RootTopologySnapshot{Version: RootTopologyVersion,
		Mapping: RootTopologyMapping{ApprovedPath: "/approved", RegisteredPath: "/approved/library"},
		Anchor:  rootBindingReadUnitIdentity(1), RegisteredRoot: rootBindingReadUnitIdentity(2),
		Boundaries: []RootTopologyBoundary{
			{RelativePath: "nested/z", Identity: rootBindingReadUnitIdentity(3)},
			{RelativePath: "nested/a", Identity: rootBindingReadUnitIdentity(4)},
		}}
}

func rootBindingReadUnitRow() rootBindingRow {
	return rootBindingRow{root: libraryRoot{id: "root-1", libraryID: "library-1", path: "/approved/library",
		allowedPath: "/approved", relativePath: "library"}, revision: 1}
}

func rootBindingReadUnitBoundRow(t *testing.T) rootBindingRow {
	t.Helper()
	row := rootBindingReadUnitRow()
	row.stored = true
	row.document = rootBindingReadUnitEncode(t, rootBindingReadUnitSnapshot())
	boundAt := time.Date(2026, time.September, 1, 12, 34, 56, 789, time.UTC)
	boundBy := "admin:binding-test"
	row.boundAt, row.boundBy = &boundAt, &boundBy
	return row
}

func rootBindingReadUnitEncode(t *testing.T, snapshot RootTopologySnapshot) []byte {
	t.Helper()
	raw, err := storagebinding.EncodeSnapshot(snapshot)
	if err != nil {
		t.Fatalf("encode binding fixture: %v", err)
	}
	return raw
}

func TestRootBindingReadUnitMappingAcceptsCanonicalAndLegacySelfRoots(t *testing.T) {
	for _, test := range []struct {
		name, approved, registered, relative string
	}{
		{"descendant", "/approved", "/approved/library", "library"},
		{"nested descendant", "/approved", "/approved/library/with space", "library/with space"},
		{"self", "/approved", "/approved", "."},
		{"legacy self", "/approved", "/approved", ""},
		{"filesystem self", "/", "/", "."},
		{"legacy filesystem self", "/", "/", ""},
		{"filesystem descendant", "/", "/library", "library"},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := rootBindingReadUnitRow()
			row.root.allowedPath, row.root.path, row.root.relativePath = test.approved, test.registered, test.relative
			if err := row.validateMapping(); err != nil {
				t.Fatalf("valid mapping was rejected: %v", err)
			}
			if approved, err := row.validate(); err != nil || approved != nil {
				t.Fatalf("valid unbound row returned approval or error: %+v, %v", approved, err)
			}
			wantRelative := test.relative
			if wantRelative == "" {
				wantRelative = "."
			}
			if got := row.info().RelativePath; got != wantRelative {
				t.Fatalf("relative path = %q; want %q", got, wantRelative)
			}
		})
	}
}

func TestRootBindingReadUnitMappingRejectsInvalidCatalogRows(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*rootBindingRow)
	}{
		{"zero revision", func(row *rootBindingRow) { row.revision = 0 }},
		{"negative revision", func(row *rootBindingRow) { row.revision = -1 }},
		{"minimum revision", func(row *rootBindingRow) { row.revision = math.MinInt64 }},
		{"empty root ID", func(row *rootBindingRow) { row.root.id = "" }},
		{"empty library ID", func(row *rootBindingRow) { row.root.libraryID = "" }},
		{"oversized root ID", func(row *rootBindingRow) { row.root.id = strings.Repeat("x", 257) }},
		{"oversized library ID", func(row *rootBindingRow) { row.root.libraryID = strings.Repeat("x", 257) }},
		{"root ID whitespace", func(row *rootBindingRow) { row.root.id = " root-1" }},
		{"library ID control", func(row *rootBindingRow) { row.root.libraryID = "library\n1" }},
		{"root ID invalid UTF8", func(row *rootBindingRow) { row.root.id = string([]byte{0xff}) }},
		{"relative approved path", func(row *rootBindingRow) { row.root.allowedPath = "approved" }},
		{"noncanonical approved path", func(row *rootBindingRow) { row.root.allowedPath = "/approved/" }},
		{"relative registered path", func(row *rootBindingRow) { row.root.path = "approved/library" }},
		{"noncanonical registered path", func(row *rootBindingRow) { row.root.path = "/approved//library" }},
		{"registered traversal", func(row *rootBindingRow) { row.root.path = "/approved/../outside" }},
		{"shared prefix outside root", func(row *rootBindingRow) { row.root.path = "/approved-other/library" }},
		{"oversized approved path", func(row *rootBindingRow) {
			row.root.allowedPath = "/" + strings.Repeat("x", storagebinding.MaxPathBytes)
		}},
		{"registered path NUL", func(row *rootBindingRow) { row.root.path += "\x00" }},
		{"mismatched relative path", func(row *rootBindingRow) { row.root.relativePath = "other" }},
		{"noncanonical relative path", func(row *rootBindingRow) { row.root.relativePath = "./library" }},
		{"relative traversal", func(row *rootBindingRow) { row.root.relativePath = "../library" }},
		{"absolute relative path", func(row *rootBindingRow) { row.root.relativePath = "/library" }},
		{"descendant as self", func(row *rootBindingRow) { row.root.relativePath = "." }},
		{"descendant as legacy self", func(row *rootBindingRow) { row.root.relativePath = "" }},
		{"self with descendant relative path", func(row *rootBindingRow) { row.root.path = row.root.allowedPath }},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := rootBindingReadUnitRow()
			test.change(&row)
			if err := row.validateMapping(); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("invalid mapping error = %v; want unavailable", err)
			}
			if approved, err := row.validate(); !errors.Is(err, ErrUnavailable) || approved != nil {
				t.Fatalf("invalid row returned approval or wrong error: %+v, %v", approved, err)
			}
		})
	}
}

func TestRootBindingReadUnitMappingPreservesValidCatalogIdentifiers(t *testing.T) {
	row := rootBindingReadUnitRow()
	row.root.id = "root caf\u00e9"
	row.root.libraryID = strings.Repeat("\u00e9", 128)
	if err := row.validateMapping(); err != nil {
		t.Fatalf("valid catalog identifiers were rejected: %v", err)
	}
	info := row.info()
	if info.RootID != row.root.id || info.LibraryID != row.root.libraryID {
		t.Fatal("projection changed valid catalog identifiers")
	}
}

func TestRootBindingReadUnitValidationRequiresCompleteBindingMetadata(t *testing.T) {
	row := rootBindingReadUnitBoundRow(t)
	approved, err := row.validate()
	if err != nil || approved == nil {
		t.Fatalf("complete bound row was rejected: %v", err)
	}
	wantFingerprint, err := rootBindingReadUnitSnapshot().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := approved.Fingerprint(); err != nil || got != wantFingerprint {
		t.Fatalf("decoded approval lost persisted topology: %q, %v", got, err)
	}
	for _, test := range []struct {
		name   string
		bound  bool
		change func(*rootBindingRow)
	}{
		{"unbound empty document", false, func(row *rootBindingRow) { row.document = []byte{} }},
		{"unbound stored document", false, func(row *rootBindingRow) { row.document = []byte("{}") }},
		{"unbound timestamp", false, func(row *rootBindingRow) { value := time.Unix(1, 0); row.boundAt = &value }},
		{"unbound actor", false, func(row *rootBindingRow) { value := "admin"; row.boundBy = &value }},
		{"bound missing document", true, func(row *rootBindingRow) { row.document = nil }},
		{"bound empty document", true, func(row *rootBindingRow) { row.document = []byte{} }},
		{"bound missing timestamp", true, func(row *rootBindingRow) { row.boundAt = nil }},
		{"bound missing actor", true, func(row *rootBindingRow) { row.boundBy = nil }},
		{"bound missing metadata", true, func(row *rootBindingRow) { row.boundAt, row.boundBy = nil, nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := rootBindingReadUnitRow()
			if test.bound {
				row = rootBindingReadUnitBoundRow(t)
			}
			test.change(&row)
			if approved, err := row.validate(); !errors.Is(err, ErrUnavailable) || approved != nil {
				t.Fatalf("partial binding returned approval or wrong error: %+v, %v", approved, err)
			}
		})
	}
	for _, actor := range []string{"", " admin", "admin ", ".admin", "_admin", ":admin", "-admin", "admin/user", "admin\nuser", "admin\x00user", "administrátor", strings.Repeat("a", 257)} {
		t.Run("invalid actor "+fmt.Sprintf("%q", actor), func(t *testing.T) {
			row := rootBindingReadUnitBoundRow(t)
			row.boundBy = &actor
			if approved, err := row.validate(); !errors.Is(err, ErrUnavailable) || approved != nil {
				t.Fatalf("invalid approval actor was accepted: %v", err)
			}
		})
	}
	for _, actor := range []string{"a", "Admin1.name_id:part-2", strings.Repeat("a", 256)} {
		t.Run("valid actor "+fmt.Sprintf("%q", actor), func(t *testing.T) {
			row := rootBindingReadUnitBoundRow(t)
			row.boundBy = &actor
			if approved, err := row.validate(); err != nil || approved == nil {
				t.Fatalf("valid approval actor was rejected: %v", err)
			}
		})
	}
}

func TestRootBindingReadUnitValidationRejectsInvalidDocumentsPrivately(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*rootBindingRow)
	}{
		{"null", func(row *rootBindingRow) { row.document = []byte("null") }},
		{"array", func(row *rootBindingRow) { row.document = []byte("[]") }},
		{"truncated", func(row *rootBindingRow) { row.document = row.document[:len(row.document)-1] }},
		{"trailing value", func(row *rootBindingRow) { row.document = append(row.document, []byte(" {}")...) }},
		{"invalid UTF8", func(row *rootBindingRow) { row.document = append(row.document, 0xff) }},
		{"oversized document", func(row *rootBindingRow) { row.document = bytes.Repeat([]byte(" "), storagebinding.MaxDocumentBytes+1) }},
		{"unsupported version", func(row *rootBindingRow) {
			row.document = bytes.Replace(row.document, []byte(`"version":1`), []byte(`"version":2`), 1)
		}},
		{"unknown field", func(row *rootBindingRow) {
			row.document = append([]byte(`{"private-document-field":true,`), row.document[1:]...)
		}},
		{"duplicate field", func(row *rootBindingRow) { row.document = append([]byte(`{"version":1,`), row.document[1:]...) }},
		{"case variant field", func(row *rootBindingRow) {
			row.document = bytes.Replace(row.document, []byte(`"version"`), []byte(`"Version"`), 1)
		}},
		{"unknown mapping field", func(row *rootBindingRow) {
			row.document = bytes.Replace(row.document, []byte(`"mapping":{`), []byte(`"mapping":{"private-document-field":true,`), 1)
		}},
		{"unknown anchor field", func(row *rootBindingRow) {
			row.document = bytes.Replace(row.document, []byte(`"anchor":{`), []byte(`"anchor":{"private-document-field":true,`), 1)
		}},
		{"unknown registered identity field", func(row *rootBindingRow) {
			row.document = bytes.Replace(row.document, []byte(`"registered_root":{`), []byte(`"registered_root":{"private-document-field":true,`), 1)
		}},
		{"unknown boundary field", func(row *rootBindingRow) {
			row.document = bytes.Replace(row.document, []byte(`{"relative_path":`), []byte(`{"private-document-field":true,"relative_path":`), 1)
		}},
		{"unknown boundary identity field", func(row *rootBindingRow) {
			row.document = bytes.Replace(row.document, []byte(`"identity":{`), []byte(`"identity":{"private-document-field":true,`), 1)
		}},
		{"private malformed handle", func(row *rootBindingRow) {
			encoded := base64.StdEncoding.EncodeToString(rootBindingReadUnitIdentity(1).Handle)
			row.document = bytes.Replace(row.document, []byte(`"handle":"`+encoded+`"`), []byte(`"handle":"private-document-handle!"`), 1)
		}},
		{"approved mapping mismatch", func(row *rootBindingRow) {
			snapshot := rootBindingReadUnitSnapshot()
			snapshot.Mapping.ApprovedPath = "/"
			row.document = rootBindingReadUnitEncode(t, snapshot)
		}},
		{"registered mapping mismatch", func(row *rootBindingRow) {
			snapshot := rootBindingReadUnitSnapshot()
			snapshot.Mapping.RegisteredPath = "/approved/private-document-path"
			row.document = rootBindingReadUnitEncode(t, snapshot)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := rootBindingReadUnitBoundRow(t)
			test.change(&row)
			approved, err := row.validate()
			if !errors.Is(err, ErrUnavailable) || approved != nil {
				t.Fatalf("invalid document returned approval or wrong error: %+v, %v", approved, err)
			}
			for _, secret := range []string{"private-document-field", "private-document-handle", "private-document-path"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("validation error exposed %q", secret)
				}
			}
		})
	}
}

func TestRootBindingReadUnitValidationBoundsTimestampYears(t *testing.T) {
	for _, test := range []struct {
		year  int
		valid bool
	}{
		{-1, false},
		{0, true},
		{9999, true},
		{10000, false},
	} {
		t.Run(fmt.Sprintf("year %d", test.year), func(t *testing.T) {
			row := rootBindingReadUnitBoundRow(t)
			boundAt := time.Date(test.year, time.January, 1, 0, 0, 0, 0, time.UTC)
			row.boundAt = &boundAt
			approved, err := row.validate()
			if test.valid {
				if err != nil || approved == nil {
					t.Fatalf("JSON-compatible binding timestamp was rejected: %v", err)
				}
				info, err := rootBindingProjection(row, approved, rootBindingReadUnitSnapshot(), nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := json.Marshal(info); err != nil {
					t.Fatalf("accepted binding timestamp cannot be encoded: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrUnavailable) || approved != nil {
				t.Fatalf("unencodable binding timestamp returned approval or wrong error: %+v, %v", approved, err)
			}
		})
	}
}

func TestRootBindingReadUnitInfoPreservesExactRevision(t *testing.T) {
	for _, test := range []struct {
		revision int64
		want     string
	}{
		{1, "1"},
		{9007199254740993, "9007199254740993"},
		{math.MaxInt64, "9223372036854775807"},
	} {
		t.Run(test.want, func(t *testing.T) {
			row := rootBindingReadUnitRow()
			row.revision = test.revision
			info := row.info()
			want := RegisteredRootInfo{RootID: row.root.id, LibraryID: row.root.libraryID, Path: row.root.path,
				AllowedPath: row.root.allowedPath, RelativePath: row.root.relativePath, Revision: test.want}
			if info != want {
				t.Fatalf("registered root info = %+v; want %+v", info, want)
			}
			raw, err := json.Marshal(info)
			if err != nil || !bytes.Contains(raw, []byte(`"revision":"`+test.want+`"`)) {
				t.Fatalf("revision was not encoded as an exact decimal string: %s, %v", raw, err)
			}
		})
	}
}

func TestRootBindingReadUnitRowsCompareEveryRevalidatedField(t *testing.T) {
	before := rootBindingReadUnitBoundRow(t)
	same := rootBindingReadUnitBoundRow(t)
	sameInstant := same.boundAt.In(time.FixedZone("same instant", 3600))
	same.boundAt = &sameInstant
	if !before.same(same) || !same.same(before) || !rootBindingReadUnitRow().same(rootBindingReadUnitRow()) {
		t.Fatal("equivalent catalog rows did not compare equally")
	}
	for _, test := range []struct {
		name   string
		change func(*rootBindingRow)
	}{
		{"root ID", func(row *rootBindingRow) { row.root.id += "-changed" }},
		{"library ID", func(row *rootBindingRow) { row.root.libraryID += "-changed" }},
		{"registered path", func(row *rootBindingRow) { row.root.path += "-changed" }},
		{"approved path", func(row *rootBindingRow) { row.root.allowedPath += "-changed" }},
		{"relative path", func(row *rootBindingRow) { row.root.relativePath += "-changed" }},
		{"revision", func(row *rootBindingRow) { row.revision++ }},
		{"binding presence", func(row *rootBindingRow) { row.stored = false }},
		{"document bytes", func(row *rootBindingRow) { row.document = append(row.document, ' ') }},
		{"missing document", func(row *rootBindingRow) { row.document = nil }},
		{"timestamp", func(row *rootBindingRow) {
			changed := row.boundAt.Add(time.Nanosecond)
			row.boundAt = &changed
		}},
		{"missing timestamp", func(row *rootBindingRow) { row.boundAt = nil }},
		{"actor", func(row *rootBindingRow) {
			changed := *row.boundBy + "-changed"
			row.boundBy = &changed
		}},
		{"missing actor", func(row *rootBindingRow) { row.boundBy = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			after := rootBindingReadUnitBoundRow(t)
			test.change(&after)
			if before.same(after) || after.same(before) {
				t.Fatal("changed catalog row passed the post-observation comparison")
			}
		})
	}
}

func TestRootBindingReadUnitProjectionReportsObservationState(t *testing.T) {
	for _, test := range []struct {
		name        string
		bound       bool
		change      func(*RootTopologySnapshot)
		observation error
		want        RootBindingStatus
		observed    bool
	}{
		{name: "unbound observed", want: RootBindingUnbound, observed: true},
		{name: "unbound unavailable", observation: errors.New("private-filesystem-error"), want: RootBindingUnavailable},
		{name: "verified", bound: true, want: RootBindingVerified, observed: true},
		{name: "canonical boundary order", bound: true, change: func(snapshot *RootTopologySnapshot) {
			snapshot.Boundaries[0], snapshot.Boundaries[1] = snapshot.Boundaries[1], snapshot.Boundaries[0]
		}, want: RootBindingVerified, observed: true},
		{name: "identity mismatch", bound: true, change: func(snapshot *RootTopologySnapshot) {
			snapshot.RegisteredRoot.Handle[2]++
		}, want: RootBindingMismatch, observed: true},
		{name: "boundary mismatch", bound: true, change: func(snapshot *RootTopologySnapshot) {
			snapshot.Boundaries[1].Identity.HandleType++
		}, want: RootBindingMismatch, observed: true},
		{name: "lost boundary", bound: true, change: func(snapshot *RootTopologySnapshot) {
			snapshot.Boundaries = snapshot.Boundaries[:1]
		}, want: RootBindingMismatch, observed: true},
		{name: "bound unavailable", bound: true, observation: errors.New("private-filesystem-error"), want: RootBindingUnavailable},
		{name: "observation error overrides valid snapshot", bound: true, observation: fmt.Errorf("open /private/filesystem/path: %w", errors.New("private-filesystem-error")), want: RootBindingUnavailable},
		{name: "invalid observed topology", bound: true, change: func(snapshot *RootTopologySnapshot) {
			snapshot.RegisteredRoot = RootStorageIdentity{}
		}, want: RootBindingUnavailable},
		{name: "observed approved mapping mismatch", bound: true, change: func(snapshot *RootTopologySnapshot) {
			snapshot.Mapping.ApprovedPath = "/"
		}, want: RootBindingUnavailable},
		{name: "observed registered mapping mismatch", bound: true, change: func(snapshot *RootTopologySnapshot) {
			snapshot.Mapping.RegisteredPath = "/approved/other-library"
		}, want: RootBindingUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := rootBindingReadUnitRow()
			var approved *RootTopologySnapshot
			if test.bound {
				row = rootBindingReadUnitBoundRow(t)
				var err error
				approved, err = row.validate()
				if err != nil {
					t.Fatal(err)
				}
			}
			observed := rootBindingReadUnitSnapshot()
			if test.change != nil {
				test.change(&observed)
			}
			info, err := rootBindingProjection(row, approved, observed, test.observation)
			if err != nil || info.Status != test.want || info.RegisteredRootInfo != row.info() {
				t.Fatalf("projection state = %q, metadata = %+v, error = %v; want %q", info.Status, info.RegisteredRootInfo, err, test.want)
			}
			if (info.Observed != nil) != test.observed || (info.ObservedFingerprint != "") != test.observed {
				t.Fatalf("observed summary completeness does not match state: %+v", info)
			}
			if test.bound {
				wantApproved, wantFingerprint, err := rootBindingTopologyInfo(*approved)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(info.Approved, wantApproved) || info.ApprovedFingerprint != wantFingerprint ||
					info.BoundAt == nil || !info.BoundAt.Equal(*row.boundAt) || info.BoundBy != *row.boundBy {
					t.Fatalf("observation discarded or changed retained approval: %+v", info)
				}
				if info.BoundAt == row.boundAt {
					t.Fatal("projection retained a mutable catalog timestamp pointer")
				}
			} else if info.Approved != nil || info.ApprovedFingerprint != "" || info.BoundAt != nil || info.BoundBy != "" {
				t.Fatalf("unbound row acquired approval metadata: %+v", info)
			}
			if info.Status == RootBindingVerified && info.ApprovedFingerprint != info.ObservedFingerprint {
				t.Fatal("verified state has different fingerprints")
			}
			if info.Status == RootBindingMismatch && info.ApprovedFingerprint == info.ObservedFingerprint {
				t.Fatal("mismatch state has equal fingerprints")
			}
			raw, err := json.Marshal(info)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte("private-filesystem-error")) || bytes.Contains(raw, []byte("/private/filesystem/path")) {
				t.Fatal("projection exposed the original filesystem error")
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			for field, wantPresent := range map[string]bool{
				"approved": test.bound, "approved_fingerprint": test.bound,
				"bound_at": test.bound, "bound_by": test.bound,
				"observed": test.observed, "observed_fingerprint": test.observed,
			} {
				if _, present := fields[field]; present != wantPresent {
					t.Errorf("JSON field %q presence = %v; want %v", field, present, wantPresent)
				}
			}
		})
	}
}

func TestRootBindingReadUnitProjectionRejectsInvalidApproval(t *testing.T) {
	for _, observationErr := range []error{nil, errors.New("private-filesystem-error")} {
		row := rootBindingReadUnitBoundRow(t)
		approved := rootBindingReadUnitSnapshot()
		approved.Anchor = RootStorageIdentity{}
		info, err := rootBindingProjection(row, &approved, rootBindingReadUnitSnapshot(), observationErr)
		if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(info, RootBindingInfo{}) {
			t.Fatalf("invalid approval produced a partial projection: %+v, %v", info, err)
		}
	}
}

func TestRootBindingReadUnitProjectionJSONContainsOnlyIdentitySummaries(t *testing.T) {
	row := rootBindingReadUnitBoundRow(t)
	snapshot := rootBindingReadUnitSnapshot()
	info, err := rootBindingProjection(row, &snapshot, snapshot, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	wantFields := []string{"root_id", "library_id", "path", "allowed_path", "relative_path", "revision", "status",
		"approved_fingerprint", "observed_fingerprint", "approved", "observed", "bound_at", "bound_by"}
	if len(fields) != len(wantFields) {
		t.Fatalf("projection JSON has unexpected fields: %s", raw)
	}
	for _, field := range wantFields {
		if _, exists := fields[field]; !exists {
			t.Errorf("projection JSON is missing %q", field)
		}
	}
	for _, field := range []string{"handle", "handle_type", "document", "storage_binding", "namespace", "mount_id", "device", "inode", "source"} {
		if bytes.Contains(raw, []byte(`"`+field+`"`)) {
			t.Errorf("projection JSON exposed private field %q", field)
		}
	}
	identities := []RootStorageIdentity{snapshot.Anchor, snapshot.RegisteredRoot}
	for _, boundary := range snapshot.Boundaries {
		identities = append(identities, boundary.Identity)
	}
	for _, identity := range identities {
		if bytes.Contains(raw, identity.Handle) || bytes.Contains(raw, []byte(base64.StdEncoding.EncodeToString(identity.Handle))) {
			t.Fatal("projection JSON exposed opaque handle bytes")
		}
	}
	if bytes.Contains(raw, row.document) {
		t.Fatal("projection JSON exposed the storage document")
	}
	for _, name := range []string{"approved", "observed"} {
		var topology map[string]json.RawMessage
		if err := json.Unmarshal(fields[name], &topology); err != nil {
			t.Fatal(err)
		}
		if len(topology) != 3 || topology["anchor"] == nil || topology["registered_root"] == nil || topology["boundaries"] == nil {
			t.Fatalf("%s topology contains unexpected fields: %s", name, fields[name])
		}
		identityJSON := []json.RawMessage{topology["anchor"], topology["registered_root"]}
		var boundaries []map[string]json.RawMessage
		if err := json.Unmarshal(topology["boundaries"], &boundaries); err != nil {
			t.Fatal(err)
		}
		for _, boundary := range boundaries {
			if len(boundary) != 2 || boundary["relative_path"] == nil || boundary["identity"] == nil {
				t.Fatal("boundary contains fields outside its display summary")
			}
			identityJSON = append(identityJSON, boundary["identity"])
		}
		for _, value := range identityJSON {
			var summary map[string]json.RawMessage
			if err := json.Unmarshal(value, &summary); err != nil {
				t.Fatal(err)
			}
			if len(summary) != 3 || summary["profile"] == nil || summary["filesystem_uuid"] == nil || summary["digest"] == nil {
				t.Fatalf("identity contains fields outside its display summary: %s", value)
			}
		}
	}
}

func TestRootBindingReadUnitTopologySummarySortsCompleteBoundaries(t *testing.T) {
	snapshot := rootBindingReadUnitSnapshot()
	before := snapshot.Clone()
	summary, fingerprint, err := rootBindingTopologyInfo(snapshot)
	if err != nil || summary == nil {
		t.Fatalf("complete topology summary failed: %v", err)
	}
	wantFingerprint, err := snapshot.Fingerprint()
	if err != nil || fingerprint != wantFingerprint {
		t.Fatalf("summary fingerprint changed: %q, %v", fingerprint, err)
	}
	if !reflect.DeepEqual(snapshot, before) {
		t.Fatal("summary sorting mutated the observed snapshot")
	}
	wantBoundaries := []RootBindingBoundaryInfo{
		{RelativePath: "nested/a", Identity: rootBindingIdentityInfo(snapshot.Boundaries[1].Identity)},
		{RelativePath: "nested/z", Identity: rootBindingIdentityInfo(snapshot.Boundaries[0].Identity)},
	}
	if !reflect.DeepEqual(summary.Boundaries, wantBoundaries) || summary.Anchor != rootBindingIdentityInfo(snapshot.Anchor) ||
		summary.RegisteredRoot != rootBindingIdentityInfo(snapshot.RegisteredRoot) {
		t.Fatalf("topology summary lost identity or path correspondence: %+v", summary)
	}
	for _, boundaries := range [][]RootTopologyBoundary{nil, {}} {
		empty := rootBindingReadUnitSnapshot()
		empty.Boundaries = boundaries
		summary, _, err := rootBindingTopologyInfo(empty)
		if err != nil || summary == nil || summary.Boundaries == nil || len(summary.Boundaries) != 0 {
			t.Fatalf("empty boundaries did not produce a complete empty array: %+v, %v", summary, err)
		}
	}
}

func TestRootBindingReadUnitTopologySummaryEnforcesCompleteBounds(t *testing.T) {
	maximum := rootBindingReadUnitSnapshot()
	maximum.Boundaries = make([]RootTopologyBoundary, storagebinding.MaxBoundaries)
	for index := range maximum.Boundaries {
		maximum.Boundaries[index] = RootTopologyBoundary{RelativePath: fmt.Sprintf("nested/%03d", storagebinding.MaxBoundaries-index-1),
			Identity: rootBindingReadUnitIdentity(byte(index))}
	}
	summary, fingerprint, err := rootBindingTopologyInfo(maximum)
	if err != nil || summary == nil || len(summary.Boundaries) != storagebinding.MaxBoundaries || len(fingerprint) != 64 {
		t.Fatalf("maximum bounded summary was rejected or truncated: %v", err)
	}
	for index, boundary := range summary.Boundaries {
		if boundary.RelativePath != fmt.Sprintf("nested/%03d", index) {
			t.Fatal("maximum bounded summary is not sorted")
		}
	}
	for _, test := range []struct {
		name   string
		change func(*RootTopologySnapshot)
		want   error
	}{
		{"excessive boundary count", func(snapshot *RootTopologySnapshot) {
			snapshot.Boundaries = append(snapshot.Boundaries, RootTopologyBoundary{RelativePath: "one-more", Identity: rootBindingReadUnitIdentity(1)})
		}, storagebinding.ErrTopologyLimit},
		{"oversized canonical representation", func(snapshot *RootTopologySnapshot) {
			for index := range snapshot.Boundaries {
				snapshot.Boundaries[index].RelativePath = fmt.Sprintf("%03d", index) + strings.Repeat("\n", storagebinding.MaxPathBytes-3)
			}
		}, storagebinding.ErrTopologyLimit},
		{"incomplete final boundary", func(snapshot *RootTopologySnapshot) {
			snapshot.Boundaries[len(snapshot.Boundaries)-1].Identity = RootStorageIdentity{}
		}, storagebinding.ErrInvalidTopology},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := maximum.Clone()
			test.change(&snapshot)
			if summary, fingerprint, err := rootBindingTopologyInfo(snapshot); !errors.Is(err, test.want) || summary != nil || fingerprint != "" {
				t.Fatalf("invalid topology produced a partial summary: %+v, %q, %v", summary, fingerprint, err)
			}
		})
	}
}

func TestRootBindingReadUnitIdentitySummaryDigestIncludesOpaqueIdentity(t *testing.T) {
	identity := rootBindingReadUnitIdentity(1)
	before := rootBindingIdentityInfo(identity)
	if before.Profile != identity.Profile || before.FilesystemUUID != identity.FilesystemUUID || len(before.Digest) != 64 {
		t.Fatalf("identity summary is incomplete: %+v", before)
	}
	if duplicate := rootBindingIdentityInfo(identity); duplicate != before {
		t.Fatal("identical identity produced a different display digest")
	}
	for _, test := range []struct {
		name   string
		change func(*RootStorageIdentity)
	}{
		{"filesystem UUID", func(identity *RootStorageIdentity) { identity.FilesystemUUID = "1123456789abcdef0123456789abcdef" }},
		{"handle type", func(identity *RootStorageIdentity) { identity.HandleType = math.MinInt32 }},
		{"first handle byte", func(identity *RootStorageIdentity) { identity.Handle[0]++ }},
		{"last handle byte", func(identity *RootStorageIdentity) { identity.Handle[len(identity.Handle)-1]++ }},
		{"handle length", func(identity *RootStorageIdentity) { identity.Handle = append(identity.Handle, 0) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := rootBindingReadUnitIdentity(1)
			test.change(&changed)
			if err := changed.Validate(); err != nil {
				t.Fatalf("changed identity fixture is invalid: %v", err)
			}
			if got := rootBindingIdentityInfo(changed); got.Digest == before.Digest || len(got.Digest) != 64 {
				t.Fatalf("identity change did not change the summary digest: %+v", got)
			}
		})
	}
}
