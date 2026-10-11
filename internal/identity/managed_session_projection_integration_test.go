package identity_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

func TestManagedSessionProjectionPreservesHistoryAndPageSemantics(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	const (
		viewerID  = "projection-viewer"
		blockedID = "projection-blocked"
		managerID = "projection-manager"
		history   = 240
	)
	if _, err := pool.Exec(ctx, `INSERT INTO users
		(id, name, normalized_name, password_hash, is_administrator, is_disabled) VALUES
		('projection-viewer', 'Projection Viewer %_Name', 'projection viewer %_name', 'fixture', false, false),
		('projection-blocked', 'Projection Blocked', 'projection blocked', 'fixture', true, true),
		('projection-manager', 'Projection Manager', 'projection manager', 'fixture', true, false)`); err != nil {
		t.Fatal(err)
	}
	var observedAt time.Time
	if err := pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&observedAt); err != nil {
		t.Fatal(err)
	}
	observedAt = observedAt.UTC()
	profiles := []identity.ManagedSession{
		{UserID: viewerID, UserName: "Projection Viewer %_Name", Kind: "emby", Status: identity.ManagedSessionActive},
		{UserID: blockedID, UserName: "Projection Blocked", UserIsAdministrator: true, UserIsDisabled: true, Kind: "admin", Status: identity.ManagedSessionRevoked},
		{UserID: blockedID, UserName: "Projection Blocked", UserIsAdministrator: true, UserIsDisabled: true, Kind: "emby", Status: identity.ManagedSessionDisabled},
		{UserID: managerID, UserName: "Projection Manager", UserIsAdministrator: true, Kind: "admin", Status: identity.ManagedSessionActive},
		{UserID: viewerID, UserName: "Projection Viewer %_Name", Kind: "emby", Status: identity.ManagedSessionExpired},
		{UserID: managerID, UserName: "Projection Manager", UserIsAdministrator: true, Kind: "emby", Status: identity.ManagedSessionActive},
	}
	fixtures := make([]identity.ManagedSession, history)
	rows := make([][]any, 0, history+1)
	for index := range fixtures {
		session := profiles[index%len(profiles)]
		session.SessionID = fmt.Sprintf("projection-session-%03d", index)
		session.Client = identity.Client{
			Name: fmt.Sprintf("Projection Client %03d", index), DeviceID: fmt.Sprintf("projection-device-%d", index%3),
			Device: fmt.Sprintf("Projection Device %03d", index), Version: fmt.Sprintf("1.%03d", index),
		}
		switch index {
		case 17:
			session.Client.Name = "Projection %_Client"
		case 18:
			session.Client.DeviceID = "projection-device-%_ID"
		case 19:
			session.Client.Device = "Projection %_Device"
		case 20:
			session.Client.Version = "v%_Version"
		}
		// Ties exercise the session-ID ordering, while activity runs in the
		// opposite order and must never determine a record's page position.
		session.CreatedAt = observedAt.Add(-48*time.Hour + time.Duration(index/2)*time.Second)
		session.LastSeenAt = observedAt.Add(-time.Hour - time.Duration(index)*time.Second)
		session.ExpiresAt = observedAt.Add(24 * time.Hour)
		if session.Status != identity.ManagedSessionActive {
			session.ExpiresAt = observedAt.Add(-24 * time.Hour)
		}
		if session.Status == identity.ManagedSessionRevoked {
			revokedAt := observedAt.Add(-6 * time.Hour)
			session.RevokedAt = &revokedAt
		}
		fixtures[index] = session
		hash := sha256.Sum256([]byte(session.SessionID))
		rows = append(rows, []any{
			session.SessionID, session.UserID, hash[:], session.Kind, session.Client.Name, session.Client.DeviceID,
			session.Client.Device, session.Client.Version, session.CreatedAt, session.LastSeenAt, session.ExpiresAt, session.RevokedAt,
		})
	}
	applicationHash := sha256.Sum256([]byte("projection-application"))
	rows = append(rows, []any{
		"projection-application", nil, applicationHash[:], "application_key", "Projection Application", "projection-device-1",
		"Projection Application Device", "1.000", observedAt, observedAt, nil, nil,
	})
	if _, err := pool.CopyFrom(ctx, pgx.Identifier{"sessions"}, []string{
		"id", "user_id", "token_hash", "kind", "client_name", "device_id", "device_name", "client_version",
		"created_at", "last_seen_at", "expires_at", "revoked_at",
	}, pgx.CopyFromRows(rows)); err != nil {
		t.Fatal(err)
	}
	selectFixtures := func(keep func(identity.ManagedSession) bool) []identity.ManagedSession {
		selected := make([]identity.ManagedSession, 0)
		for index := len(fixtures) - 1; index >= 0; index-- {
			if keep == nil || keep(fixtures[index]) {
				selected = append(selected, fixtures[index])
			}
		}
		return selected
	}
	assertPage := func(t *testing.T, filter identity.ManagedSessionFilter, want []identity.ManagedSession) {
		t.Helper()
		page, err := store.ListManagedSessions(ctx, actor, filter)
		if err != nil {
			t.Fatal(err)
		}
		limit := filter.Limit
		if limit == 0 {
			limit = identity.ManagedSessionDefaultLimit
		}
		start := min(filter.StartIndex, len(want))
		end := min(start+limit, len(want))
		if page.TotalRecordCount != int64(len(want)) || page.StartIndex != filter.StartIndex || page.Limit != limit || page.Items == nil || len(page.Items) != end-start {
			t.Fatalf("page metadata or empty-page total changed: %#v, want total %d, start %d, limit %d", page, len(want), filter.StartIndex, limit)
		}
		for index, actual := range page.Items {
			actual.CreatedAt = actual.CreatedAt.UTC()
			actual.LastSeenAt = actual.LastSeenAt.UTC()
			actual.ExpiresAt = actual.ExpiresAt.UTC()
			if actual.RevokedAt != nil {
				revokedAt := actual.RevokedAt.UTC()
				actual.RevokedAt = &revokedAt
			}
			if !reflect.DeepEqual(actual, want[start+index]) {
				t.Fatalf("projected item %d = %#v, want %#v", index, actual, want[start+index])
			}
		}
	}
	for _, status := range []string{"", identity.ManagedSessionActive, identity.ManagedSessionRevoked, identity.ManagedSessionDisabled, identity.ManagedSessionExpired, identity.ManagedSessionAll} {
		t.Run("status/"+status, func(t *testing.T) {
			wantStatus := status
			if wantStatus == "" {
				wantStatus = identity.ManagedSessionActive
			}
			want := selectFixtures(func(session identity.ManagedSession) bool {
				return wantStatus == identity.ManagedSessionAll || session.Status == wantStatus
			})
			assertPage(t, identity.ManagedSessionFilter{Status: status, SearchTerm: "projection", Limit: identity.MaxManagedSessions}, want)
		})
	}
	for _, test := range []struct {
		name   string
		filter identity.ManagedSessionFilter
		keep   func(identity.ManagedSession) bool
	}{
		{name: "default limit", filter: identity.ManagedSessionFilter{}},
		{name: "last page", filter: identity.ManagedSessionFilter{StartIndex: history - 1, Limit: 2}},
		{name: "empty page", filter: identity.ManagedSessionFilter{StartIndex: history, Limit: 2}},
		{name: "maximum offset", filter: identity.ManagedSessionFilter{StartIndex: identity.MaxManagedSessionStartIndex, Limit: 2}},
		{name: "no match", filter: identity.ManagedSessionFilter{SearchTerm: "missing-projection"}, keep: func(identity.ManagedSession) bool { return false }},
		{name: "user", filter: identity.ManagedSessionFilter{UserID: viewerID}, keep: func(session identity.ManagedSession) bool { return session.UserID == viewerID }},
		{name: "kind", filter: identity.ManagedSessionFilter{Kind: "admin"}, keep: func(session identity.ManagedSession) bool { return session.Kind == "admin" }},
		{name: "device", filter: identity.ManagedSessionFilter{DeviceID: "projection-device-1"}, keep: func(session identity.ManagedSession) bool { return session.Client.DeviceID == "projection-device-1" }},
		{
			name: "combined filters", filter: identity.ManagedSessionFilter{UserID: viewerID, Kind: "emby", DeviceID: "projection-device-1", Status: identity.ManagedSessionExpired},
			keep: func(session identity.ManagedSession) bool {
				return session.UserID == viewerID && session.Status == identity.ManagedSessionExpired
			},
		},
		{name: "literal user search", filter: identity.ManagedSessionFilter{SearchTerm: "%_nAmE"}, keep: func(session identity.ManagedSession) bool { return session.UserID == viewerID }},
		{name: "literal client search", filter: identity.ManagedSessionFilter{SearchTerm: "%_cLiEnT"}, keep: func(session identity.ManagedSession) bool { return session.SessionID == fixtures[17].SessionID }},
		{name: "literal device ID search", filter: identity.ManagedSessionFilter{SearchTerm: "%_iD"}, keep: func(session identity.ManagedSession) bool { return session.SessionID == fixtures[18].SessionID }},
		{name: "literal device name search", filter: identity.ManagedSessionFilter{SearchTerm: "%_dEvIcE"}, keep: func(session identity.ManagedSession) bool { return session.SessionID == fixtures[19].SessionID }},
		{name: "literal version search", filter: identity.ManagedSessionFilter{SearchTerm: "%_vErSiOn"}, keep: func(session identity.ManagedSession) bool { return session.SessionID == fixtures[20].SessionID }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.filter.Status == "" {
				test.filter.Status = identity.ManagedSessionAll
			}
			if test.filter.SearchTerm == "" {
				test.filter.SearchTerm = "projection"
			}
			assertPage(t, test.filter, selectFixtures(test.keep))
		})
	}
	current, err := store.ListManagedSessions(ctx, actor, identity.ManagedSessionFilter{UserID: admin.ID, Status: identity.ManagedSessionAll})
	if err != nil || current.TotalRecordCount != 1 || len(current.Items) != 1 || !current.Items[0].IsCurrent || current.Items[0].SessionID != actor.SessionID {
		t.Fatalf("actor's current login lost its projection: %#v %v", current, err)
	}
	all, err := store.ListManagedSessions(ctx, actor, identity.ManagedSessionFilter{Status: identity.ManagedSessionAll, Limit: 1})
	if err != nil || all.TotalRecordCount != history+1 || len(all.Items) != 1 || all.Items[0].SessionID != actor.SessionID {
		t.Fatalf("userless application credential entered the count or page: %#v %v", all, err)
	}

	// An uncommitted activity writer must not block the display snapshot or move
	// either end of the history; committed activity changes only LastSeenAt.
	activityWriter, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer activityWriter.Rollback(ctx)
	activityAt := observedAt.Add(time.Hour)
	if _, err := activityWriter.Exec(ctx, "UPDATE sessions SET last_seen_at=$1 WHERE id=ANY($2::text[])", activityAt, []string{fixtures[0].SessionID, fixtures[history-1].SessionID}); err != nil {
		t.Fatal(err)
	}
	firstPage := identity.ManagedSessionFilter{Status: identity.ManagedSessionAll, SearchTerm: "projection", Limit: 2}
	lastPage := identity.ManagedSessionFilter{Status: identity.ManagedSessionAll, SearchTerm: "projection", StartIndex: history - 2, Limit: 2}
	assertPage(t, firstPage, selectFixtures(nil))
	assertPage(t, lastPage, selectFixtures(nil))
	if err := activityWriter.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	fixtures[0].LastSeenAt, fixtures[history-1].LastSeenAt = activityAt, activityAt
	assertPage(t, firstPage, selectFixtures(nil))
	assertPage(t, lastPage, selectFixtures(nil))

	if _, err := pool.Exec(ctx, `UPDATE users SET is_disabled=true WHERE id=$1`, viewerID); err != nil {
		t.Fatal(err)
	}
	for index := range fixtures {
		if fixtures[index].UserID == viewerID {
			fixtures[index].UserIsDisabled = true
			fixtures[index].Status = identity.ManagedSessionDisabled
		}
	}
	assertPage(t, identity.ManagedSessionFilter{UserID: viewerID, Status: identity.ManagedSessionDisabled, Limit: identity.MaxManagedSessions}, selectFixtures(func(session identity.ManagedSession) bool { return session.UserID == viewerID }))
	if _, err := pool.Exec(ctx, `UPDATE users SET is_administrator=false WHERE id=$1`, managerID); err != nil {
		t.Fatal(err)
	}
	for index := range fixtures {
		if fixtures[index].UserID == managerID {
			fixtures[index].UserIsAdministrator = false
			if fixtures[index].Kind == "admin" {
				fixtures[index].Status = identity.ManagedSessionDisabled
			}
		}
	}
	for _, status := range []string{identity.ManagedSessionActive, identity.ManagedSessionDisabled} {
		assertPage(t, identity.ManagedSessionFilter{UserID: managerID, Status: status, Limit: identity.MaxManagedSessions}, selectFixtures(func(session identity.ManagedSession) bool {
			return session.UserID == managerID && session.Status == status
		}))
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET is_administrator=false WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if page, err := store.ListManagedSessions(ctx, actor, identity.ManagedSessionFilter{Status: identity.ManagedSessionAll}); !errors.Is(err, identity.ErrUnauthorized) || page.Items != nil || page.TotalRecordCount != 0 {
		t.Fatalf("a stale administrator received a display projection: %#v %v", page, err)
	}
}
