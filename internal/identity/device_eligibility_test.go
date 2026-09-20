package identity

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type deviceEligibilityTestRow struct {
	kind       int
	registryID int64
	record     []byte
	deviceID   string
	observedAt time.Time
	total      int64
}

type deviceEligibilityTestRows struct {
	rows  []deviceEligibilityTestRow
	index int
	err   error
}

func (rows *deviceEligibilityTestRows) Next() bool {
	if rows.index >= len(rows.rows) {
		return false
	}
	rows.index++
	return true
}

func (rows *deviceEligibilityTestRows) Scan(destinations ...any) error {
	row := rows.rows[rows.index-1]
	*destinations[0].(*int) = row.kind
	*destinations[1].(*int64) = row.registryID
	*destinations[2].(*[]byte) = row.record
	*destinations[3].(*string) = row.deviceID
	*destinations[4].(*time.Time) = row.observedAt
	*destinations[5].(*int64) = row.total
	return nil
}

func (rows *deviceEligibilityTestRows) Err() error { return rows.err }

func TestDeviceEligibilityUsesActualSessionDeviceAndObservedScheduleInstant(t *testing.T) {
	start := time.Date(2026, time.September, 21, 9, 0, 0, 0, time.Local)
	device, err := json.Marshal(deviceRecord{ID: 2, Revision: 1, ReportedDeviceID: "registry-device",
		CreatedAt: start, LastSeenAt: start})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		policy string
		at     time.Time
		want   int64
	}{
		{"actual_session_device", `{"EnableAllDevices":false,"EnabledDevices":["actual-device"]}`, start, 1},
		{"registry_device_is_not_authority", `{"EnableAllDevices":false,"EnabledDevices":["registry-device"]}`, start, 0},
		{"schedule_before_start", `{"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":9,"EndHour":10}]}`, start.Add(-time.Nanosecond), 0},
		{"schedule_inclusive_start", `{"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":9,"EndHour":10}]}`, start, 1},
		{"schedule_exclusive_end", `{"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":9,"EndHour":10}]}`, start.Add(time.Hour), 0},
		{"locked_out", `{"LockedOutDate":1}`, start, 0},
		{"cleared_lockout", `{"LockedOutDate":0}`, start, 1},
		{"malformed_lockout", `{"LockedOutDate":"later"}`, start, 0},
		{"malformed_policy", `{"EnableAllDevices":"true"}`, start, 0},
		{"oversized_policy_projection", `null`, start, 0},
		{"network_policy_is_not_historical_peer_eligibility", `{"EnableRemoteAccess":false}`, start, 1},
		{"malformed_playback_flag_does_not_deny_login", `{"EnableMediaPlayback":"true"}`, start, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Credential rows can precede device/total rows. Their observation is
			// deliberately unrelated to the test execution clock.
			rows := &deviceEligibilityTestRows{rows: []deviceEligibilityTestRow{
				{kind: 2, registryID: 2, record: []byte(test.policy), deviceID: "actual-device", observedAt: test.at.UTC()},
				{kind: 1, registryID: 2, record: device, observedAt: test.at.UTC()},
				{kind: 0, observedAt: test.at.UTC(), total: 1},
			}}
			page, err := scanDeviceEligibility(rows, ManagedDeviceFilter{Limit: 1})
			if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 || page.Items[0].ActiveLoginCount != test.want {
				t.Fatalf("credential eligibility count differs: want %d, error %v", test.want, err)
			}
		})
	}
}

func TestDeviceEligibilityBudgetsRejectExcessWithoutAdvancingAcceptedWork(t *testing.T) {
	for _, test := range []struct {
		name   string
		budget deviceEligibilityBudget
		last   []byte
		extra  []byte
	}{
		{"credential_count", deviceEligibilityBudget{credentials: maxDeviceEligibilityCredentials - 1}, []byte(`{}`), []byte(`{}`)},
		{"policy_bytes", deviceEligibilityBudget{policyBytes: maxDeviceEligibilityPolicyBytes - 2}, []byte(`{}`), []byte(`{}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.budget.consume(test.last); err != nil {
				t.Fatalf("exact budget rejected: %v", err)
			}
			before := test.budget
			if err := test.budget.consume(test.extra); !errors.Is(err, ErrDeviceEligibilityLimit) || test.budget != before {
				t.Fatalf("excess work was accepted or changed the accepted budget: %v", err)
			}
		})
	}
}

func TestDeviceEligibilityCursorFailureNeverReturnsPartialCounts(t *testing.T) {
	want := errors.New("cursor interrupted")
	rows := &deviceEligibilityTestRows{rows: []deviceEligibilityTestRow{
		{kind: 0, total: 1},
		{kind: 2, registryID: 2, record: []byte(`{}`), deviceID: "device", observedAt: time.Now()},
	}, err: want}
	page, err := scanDeviceEligibility(rows, ManagedDeviceFilter{Limit: 1})
	if !errors.Is(err, want) || page.Items != nil || page.TotalRecordCount != 0 {
		t.Fatalf("cursor failure returned a partial eligibility projection: %v", err)
	}
}
