package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// Eligibility is an exact credential count, never an online-presence estimate.
// These response-wide budgets bound retained devices and streamed policy work;
// exceeding either budget fails the entire projection instead of truncating it.
const (
	maxDeviceEligibilityDevices     = 10_000
	maxDeviceEligibilityCredentials = 100_000
	maxDeviceEligibilityPolicyBytes = 64 << 20
)

// ErrDeviceEligibilityLimit rejects an exact projection that cannot fit its
// processing budgets. No partial list or truncated count accompanies the error.
var ErrDeviceEligibilityLimit = errors.New("device eligibility projection exceeds its processing limit")

type deviceEligibilityBudget struct {
	credentials int
	policyBytes int
}

func (budget *deviceEligibilityBudget) consume(policy []byte) error {
	if budget.credentials >= maxDeviceEligibilityCredentials || len(policy) > maxDeviceEligibilityPolicyBytes-budget.policyBytes {
		return ErrDeviceEligibilityLimit
	}
	budget.credentials++
	budget.policyBytes += len(policy)
	return nil
}

// readDeviceEligibility keeps the page, total, current account policies and one
// observed instant in a single statement snapshot. It deliberately does not
// change the transaction isolation used for current administrator checks.
// Credential rows never contain token hashes or historical peer addresses.
func readDeviceEligibility(ctx context.Context, tx pgx.Tx, filter ManagedDeviceFilter, id int64) (ManagedDevicesPage, error) {
	rows, err := tx.Query(ctx, `WITH observation AS MATERIALIZED (SELECT clock_timestamp() AS observed_at),
		filtered AS MATERIALIZED (
			SELECT d.id, d.last_seen_at FROM devices d LEFT JOIN users last_user ON last_user.id = d.last_user_id
			WHERE d.id > 1 AND d.deleted_at IS NULL AND ($2::bigint = 0 OR d.id = $2)
			AND ($1::text = '' OR strpos(lower(COALESCE(d.custom_name, d.reported_name)), lower($1)) > 0
			OR strpos(lower(d.reported_name), lower($1)) > 0 OR strpos(lower(d.reported_device_id), lower($1)) > 0
			OR strpos(lower(d.app_name), lower($1)) > 0 OR strpos(lower(COALESCE(last_user.name, '')), lower($1)) > 0)
		), page AS MATERIALIZED (
			SELECT * FROM filtered ORDER BY last_seen_at DESC, id DESC LIMIT $3 OFFSET $4
		), projected AS (
			SELECT `+deviceColumns+` FROM page JOIN devices d ON d.id = page.id
			LEFT JOIN users last_user ON last_user.id = d.last_user_id
		), credentials AS (
			SELECT authentication.device_registry_id, authentication.device_id,
				CASE WHEN octet_length(account.policy::text) <= $5 THEN account.policy ELSE 'null'::jsonb END AS policy
			FROM page JOIN sessions authentication ON authentication.device_registry_id = page.id
			JOIN users account ON account.id = authentication.user_id CROSS JOIN observation
			WHERE authentication.kind = 'emby' AND authentication.revoked_at IS NULL
			AND authentication.expires_at > observation.observed_at AND NOT account.is_disabled
			LIMIT $6
		)
		SELECT 0 AS row_kind, 0::bigint AS registry_id, NULL::jsonb AS record, ''::text AS device_id,
			observation.observed_at, (SELECT count(*) FROM filtered) AS total FROM observation
		UNION ALL
		SELECT 1, projected.id, to_jsonb(projected), '', observation.observed_at, 0::bigint
			FROM projected CROSS JOIN observation
		UNION ALL
		SELECT 2, credentials.device_registry_id, credentials.policy, credentials.device_id,
			observation.observed_at, 0::bigint FROM credentials CROSS JOIN observation`,
		filter.SearchTerm, id, filter.Limit, filter.StartIndex, MaxManagedPolicyBytes, maxDeviceEligibilityCredentials+1)
	if err != nil {
		return ManagedDevicesPage{}, fmt.Errorf("query device eligibility: %w", err)
	}
	defer rows.Close()
	return scanDeviceEligibility(rows, filter)
}

type deviceEligibilityRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanDeviceEligibility(rows deviceEligibilityRows, filter ManagedDeviceFilter) (ManagedDevicesPage, error) {
	result := ManagedDevicesPage{Items: make([]ManagedDevice, 0), StartIndex: filter.StartIndex, Limit: filter.Limit}
	counts := make(map[int64]int64)
	var budget deviceEligibilityBudget
	for rows.Next() {
		var kind int
		var registryID, total int64
		var record []byte
		var deviceID string
		var observedAt time.Time
		if err := rows.Scan(&kind, &registryID, &record, &deviceID, &observedAt, &total); err != nil {
			return ManagedDevicesPage{}, fmt.Errorf("read device eligibility: %w", err)
		}
		switch kind {
		case 0:
			result.TotalRecordCount = total
		case 1:
			if len(result.Items) >= maxDeviceEligibilityDevices {
				return ManagedDevicesPage{}, ErrDeviceEligibilityLimit
			}
			var device deviceRecord
			if err := json.Unmarshal(record, &device); err != nil {
				return ManagedDevicesPage{}, fmt.Errorf("decode ordinary device: %w", err)
			}
			result.Items = append(result.Items, device.device())
		case 2:
			if err := budget.consume(record); err != nil {
				return ManagedDevicesPage{}, err
			}
			// Oversized stored policies become JSON null in SQL, preserving the
			// shared predicate's fail-closed result without an unbounded row.
			if loginPolicyAllows(record, deviceID, observedAt) {
				counts[registryID]++
			}
		default:
			return ManagedDevicesPage{}, errors.New("invalid device eligibility row")
		}
	}
	if err := rows.Err(); err != nil {
		return ManagedDevicesPage{}, fmt.Errorf("read device eligibility rows: %w", err)
	}
	for index := range result.Items {
		result.Items[index].ActiveLoginCount = counts[result.Items[index].ID]
	}
	// UNION ALL and cursor transport do not establish result order. Sorting the
	// bounded device projection retains the existing activity/identity ordering.
	sort.Slice(result.Items, func(i, j int) bool {
		if !result.Items[i].LastSeenAt.Equal(result.Items[j].LastSeenAt) {
			return result.Items[i].LastSeenAt.After(result.Items[j].LastSeenAt)
		}
		return result.Items[i].ID > result.Items[j].ID
	})
	return result, nil
}
