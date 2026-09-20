//go:build linux

package backuppg

import (
	"errors"
	"testing"
)

func TestPostgreSQLCompatibilityLongTailRejectsInvalidMetadataInEveryRetainedLayer(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	if _, err := source.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('long-tail-library','Retained TV','tvshows');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder,local_metadata)
		VALUES('long-tail-series','long-tail-library','Retained series','retained series','Series',true,
		'{"Status":"Ended","EndDate":"2025-01-02T00:00:00Z"}')`); err != nil {
		t.Fatalf("seed retained TV metadata: %v", err)
	}
	for _, fixture := range []struct {
		name, mutation string
		valid          bool
	}{
		{"known_state", `UPDATE item_metadata_state SET overrides='{"Status":"Continuing"}' WHERE item_id='long-tail-series'`, true},
		{"explicit_unknown", `UPDATE item_metadata_state SET locked_values='{"Status":null,"AirsBeforeSeasonNumber":null}' WHERE item_id='long-tail-series'`, true},
		{"bad_source", `UPDATE items SET local_metadata='{"Status":"secret-invalid-state"}' WHERE id='long-tail-series'`, false},
		{"bad_automatic", `UPDATE item_metadata_state SET automatic=jsonb_set(automatic,'{EndDate}','"2025-02-30T00:00:00Z"') WHERE item_id='long-tail-series'`, false},
		{"bad_override", `UPDATE item_metadata_state SET overrides='{"AirsBeforeSeasonNumber":2147483648}' WHERE item_id='long-tail-series'`, false},
		{"bad_dormant_lock", `UPDATE item_metadata_state SET locked_values='{"AirsBeforeEpisodeNumber":-1}' WHERE item_id='long-tail-series'`, false},
		{"bad_effective", `UPDATE item_metadata_state SET effective='{"AirsAfterSeasonNumber":"2"}' WHERE item_id='long-tail-series'`, false},
		{"dormant_online", `UPDATE item_metadata_state SET online_source='{"Status":"Unknown"}' WHERE item_id='long-tail-series'`, false},
		{"competing_case_alias", `UPDATE item_metadata_state SET overrides='{"Status":"Ended","status":"Unknown"}' WHERE item_id='long-tail-series'`, false},
		{"oversized_fact", `UPDATE item_metadata_state SET overrides=jsonb_build_object('Status',repeat('x',9000)) WHERE item_id='long-tail-series'`, false},
		{"lost_explicit_sort", `UPDATE items SET local_metadata=local_metadata||'{"SortName":"preserved sort"}' WHERE id='long-tail-series';
			UPDATE item_metadata_state SET automatic_sort_name_explicit=false WHERE item_id='long-tail-series'`, false},
		{"historical_empty_online_sort", `UPDATE items SET local_metadata=local_metadata||'{"SortName":"shadowed sort"}' WHERE id='long-tail-series';
			UPDATE item_metadata_state SET online_type='Series',online_source='{"SortName":null}',automatic_sort_name_explicit=false WHERE item_id='long-tail-series'`, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal("begin retained metadata mutation")
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal(err)
			}
			if tag, err := tx.Exec(ctx, fixture.mutation); err != nil || tag.RowsAffected() != 1 {
				t.Fatalf("mutate one retained metadata layer: %v", err)
			}
			err = validateCompatibilityLongTailState(ctx, tx, 49)
			if fixture.valid && err != nil || !fixture.valid && !errors.Is(err, ErrSchema) {
				t.Fatalf("retained metadata validity=%v, got %v", fixture.valid, err)
			}
		})
	}
}
