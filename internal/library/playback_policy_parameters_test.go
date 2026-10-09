//go:build linux

package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

// Retain the old literal query only as a test oracle and historical cache
// calibration. Production source reads use indexedPlaybackMediaQuery.
func indexedPlaybackMediaSQL(access libraryAccess) string {
	return indexedPlaybackMediaSQLWithPolicy(access.directSQL("i"))
}

func playbackPolicyParameterAccess(t *testing.T, libraryID string, index int) libraryAccess {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"EnableAllFolders": false, "EnabledFolders": []string{libraryID, fmt.Sprintf("unused-library-%d", index)},
		"ExcludedSubFolders": []string{fmt.Sprintf("unused-folder-%d", index)},
		"BlockedTags":        []string{fmt.Sprintf("unused-block-%d", index)},
		"IncludeTags":        []string{"Family", fmt.Sprintf("unused-allow-%d", index)},
		"MaxParentalRating":  7 + index%2, "BlockUnratedItems": []string{"Movie"},
	})
	if err != nil {
		t.Fatal(err)
	}
	access, err := parseLibraryPolicy(encoded)
	if err != nil {
		t.Fatal(err)
	}
	access.userID = fmt.Sprintf("source-policy-user-%d", index)
	return access
}

func TestPlaybackPolicyParametersKeepValuesOutsideStatement(t *testing.T) {
	first := playbackPolicyParameterAccess(t, "library-one", 1)
	second := playbackPolicyParameterAccess(t, "library-two", 2)
	second.userID = "quoted-'user\\name"
	second.folders = append(second.folders, "another-'folder\\name")
	second.policy.IncludeTags = append(second.policy.IncludeTags, "a'quoted\\tag")
	before, beforeArgs := indexedPlaybackMediaQuery(first, "source-one")
	after, afterArgs := indexedPlaybackMediaQuery(second, "source-two")
	if before != after || reflect.DeepEqual(beforeArgs, afterArgs) {
		t.Fatal("equal policy structure must reuse SQL with independent values")
	}
	for _, value := range []string{second.userID, second.folders[0], second.policy.IncludeTags[2], "source-two"} {
		if strings.Contains(after, value) {
			t.Fatal("a caller's policy value entered executable SQL")
		}
	}
	if afterArgs[0] != "source-two" || afterArgs[3] != second.userID || !reflect.DeepEqual(afterArgs[2], second.folders) {
		t.Fatal("source or subject parameters changed identity")
	}
	// The primary item and both owner predicates share the same policy slots.
	if len(afterArgs) != 9 || strings.Count(after, "$4::text") != 6 {
		t.Fatal("nested owner checks duplicated or omitted policy parameters")
	}
}

func TestPlaybackPolicyParameterizedSourcePlansReuseAcrossScopes(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	metadata := `{"OfficialRating":"PG","Tags":["Family"]}`
	for _, statement := range []string{
		"UPDATE items SET local_metadata=$2::jsonb WHERE id=$1",
		"UPDATE item_metadata_state SET effective=$2::jsonb WHERE item_id=$1",
		"SELECT sync_catalog_item_entities($1,$2::jsonb)",
	} {
		if _, err := fixture.pool.Exec(fixture.ctx, statement, fixture.item.ID, metadata); err != nil {
			t.Fatal(err)
		}
	}
	const scopes = 8
	for _, planMode := range []string{"force_custom_plan", "force_generic_plan"} {
		t.Run(planMode, func(t *testing.T) {
			configuration := fixture.pool.Config().ConnConfig.Copy()
			configuration.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
			configuration.StatementCacheCapacity = scopes
			configuration.RuntimeParams["plan_cache_mode"] = planMode
			legacy, err := pgx.ConnectConfig(fixture.ctx, configuration.Copy())
			if err != nil {
				t.Fatal(err)
			}
			defer legacy.Close(fixture.ctx)
			current, err := pgx.ConnectConfig(fixture.ctx, configuration.Copy())
			if err != nil {
				t.Fatal(err)
			}
			defer current.Close(fixture.ctx)
			var statement, preparedName string
			for repeat := range 2 {
				for index := range scopes {
					access := playbackPolicyParameterAccess(t, fixture.library.ID, index)
					query, _ := indexedPlaybackMediaQuery(access, fixture.item.ID)
					if statement != "" && query != statement {
						t.Fatal("rotating policy values fragmented the source SQL")
					}
					statement = query
					previous, modified, err := scanIndexedPlaybackMedia(legacy.QueryRow(fixture.ctx, indexedPlaybackMediaSQL(access),
						pgx.QueryExecModeCacheStatement, fixture.item.ID, access.all, access.folders))
					if err != nil || modified == nil {
						t.Fatalf("read literal source oracle: %v", err)
					}
					previous, err = prepareIndexedMediaSourceSnapshot(previous, media.SourceID(fixture.item.ID), modified, access.canPlay)
					if err == nil {
						previous, err = finishIndexedMediaSourceSnapshot(previous)
					}
					if err != nil {
						t.Fatalf("complete literal source oracle: %v", err)
					}
					actual, err := playbackSourcePlanRead(fixture.ctx, current, access, fixture.item.ID, media.SourceID(fixture.item.ID))
					if err != nil || !reflect.DeepEqual(actual, previous) {
						t.Fatalf("bound policy changed the authorized source: %v", err)
					}
					plan := playbackSourcePlanObserve(t, fixture.ctx, current, statement)
					if plan.count != 1 || plan.total != 1 || plan.genericPlans+plan.customPlans != int64(repeat*scopes+index+1) ||
						preparedName != "" && plan.name != preparedName {
						t.Fatalf("the same physical source statement was not reused: %+v", plan)
					}
					preparedName = plan.name
				}
			}
			var legacyCount int
			if err := legacy.QueryRow(fixture.ctx, "SELECT count(*) FROM pg_prepared_statements", pgx.QueryExecModeExec).Scan(&legacyCount); err != nil || legacyCount != scopes {
				t.Fatalf("literal calibration retained %d source statements, want %d: %v", legacyCount, scopes, err)
			}
			t.Logf("source_policy_cache plan_mode=%s scopes=%d reads_per_arm=%d literal_statements=%d bound_statements=1", planMode, scopes, 2*scopes, legacyCount)
			access := playbackPolicyParameterAccess(t, fixture.library.ID, 0)
			query, arguments := indexedPlaybackMediaQuery(access, fixture.item.ID)
			for _, sample := range []struct {
				name string
				sql  string
				args []any
			}{
				{"literal", indexedPlaybackMediaSQL(access), []any{fixture.item.ID, access.all, access.folders}},
				{"bound", query, arguments},
			} {
				var encoded []byte
				args := append([]any{pgx.QueryExecModeExec}, sample.args...)
				if err := current.QueryRow(fixture.ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sample.sql, args...).Scan(&encoded); err != nil {
					t.Fatal(err)
				}
				if len(encoded) > 1<<20 {
					t.Fatal("source plan evidence exceeds its bounded fixture budget")
				}
				t.Logf("source_policy_plan observation=one_shot_explain configured_plan_mode=%s arm=%s sql_bytes=%d plan=%s", planMode, sample.name, len(sample.sql), encoded)
			}
			// Reusing the same named plan must observe a new deny value, rather
			// than reusing the previously authorized row or generic-plan constants.
			access.policy.ExcludedSubFolders = []string{fixture.item.ID}
			result, err := playbackSourcePlanRead(fixture.ctx, current, access, fixture.item.ID, media.SourceID(fixture.item.ID))
			if !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(result, indexedMediaSource{}) {
				t.Fatalf("cached source statement ignored changed exclusion parameters: %v", err)
			}
		})
	}
}
