package identity

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestDeviceProjectionNormalizesDatabaseAndJSONTimesToUTC(t *testing.T) {
	created := time.Date(2026, time.September, 10, 8, 9, 10, 123456000, time.UTC)
	seen := created.Add(37 * time.Second)
	// Binary timestamptz scanning and JSON page decoding can represent the
	// same database instant with different locations. Both public projections
	// must have one canonical representation without changing other fields.
	row := deviceRecord{ID: 2, Revision: 3, ReportedDeviceID: "projection-device", ReportedName: "Reported Device",
		AppName: "Projection Player", AppVersion: "1.0", IPAddress: "192.0.2.1", ActiveLoginCount: 4,
		CreatedAt:  created.In(time.FixedZone("Database scan location", 3*60*60)),
		LastSeenAt: seen.In(time.FixedZone("Database scan location", 3*60*60))}
	page := row
	page.CreatedAt, page.LastSeenAt = created, seen
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var decoded deviceRecord
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	direct, listed := row.device(), decoded.device()
	if !reflect.DeepEqual(direct, listed) {
		t.Fatal("direct and JSON-backed device projections differ for the same stored data")
	}
	for _, result := range []ManagedDevice{direct, listed} {
		if result.CreatedAt.Location() != time.UTC || result.LastSeenAt.Location() != time.UTC ||
			!result.CreatedAt.Equal(created) || !result.LastSeenAt.Equal(seen) {
			t.Fatal("device projection changed a timestamp instant or did not normalize its location to UTC")
		}
	}
}
