//go:build linux

package backuppg

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

func TestPostgreSQLManagedTranscodingSchema77ArchivePreservesCompleteRuntimeOverrides(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 77)
	const revision int64 = 9007199254740995
	want := managedTranscodingArchiveRuntimeOverrides()
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal("encode the historical managed runtime values", err)
	}
	// Freeze the source's eleven-group contract even when the current Go model
	// adds more fields. Only explicitly named historical groups enter this row.
	var currentGroups map[string]json.RawMessage
	if json.Unmarshal(raw, &currentGroups) != nil {
		t.Fatal("decode the managed runtime witness")
	}
	historicalGroups := make(map[string]json.RawMessage, 11)
	for _, group := range []string{"Network", "Hardware", "Threads", "H264", "HEVC", "SoftwareToneMapping", "VulkanToneMapping", "Transcoding", "TranscodingTempPath", "SoftwareToneMapOptions", "CodecConfigurations"} {
		value, exists := currentGroups[group]
		if !exists {
			t.Fatalf("the historical runtime witness omitted %s", group)
		}
		historicalGroups[group] = value
	}
	raw, err = json.Marshal(historicalGroups)
	if err != nil {
		t.Fatal("encode all eleven historical managed runtime groups", err)
	}
	if err := settings.ValidateStoredRuntimeOverrides(raw); err != nil {
		t.Fatal("the explicit source runtime witness must satisfy the stored contract", err)
	}
	seeded, err := source.Exec(ctx, `UPDATE managed_settings SET runtime_overrides=$1,revision=$2 WHERE id=1`, raw, revision)
	if err != nil || seeded.RowsAffected() != 1 {
		t.Fatal("seed the complete historical runtime payload and exact revision", err)
	}
	var originalStored []byte
	if err := source.QueryRow(ctx, `SELECT runtime_overrides FROM managed_settings WHERE id=1`).Scan(&originalStored); err != nil {
		t.Fatal("capture the canonical historical runtime row", err)
	}
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	archive, facts := sourceArchive(t, ctx, source, options)
	if facts.SchemaVersion != 77 || len(facts.MigrationChecksums) != 77 || !equalJSON(facts, before) {
		t.Fatal("the actual pg_dump archive lost its schema77 source witness")
	}
	result, err := Restore(ctx, source, target, archive, facts, options)
	if err != nil || result.SourceVersion != 77 || result.CurrentVersion != currentRecoveryVersion(t) ||
		!equalJSON(result.Tables, facts.Tables) {
		t.Fatal("restore the actual schema77 managed runtime archive", err)
	}
	for _, database := range []struct {
		name string
		pool *pgxpool.Pool
	}{{"source", source}, {"target", target}} {
		var savedRevision int64
		var saved []byte
		if err := database.pool.QueryRow(ctx, `SELECT revision,runtime_overrides FROM managed_settings WHERE id=1`).Scan(&savedRevision, &saved); err != nil {
			t.Fatalf("read %s managed runtime state: %v", database.name, err)
		}
		if err := settings.ValidateStoredRuntimeOverrides(saved); err != nil {
			t.Fatalf("%s runtime state failed stored-contract validation: %v", database.name, err)
		}
		var actual settings.RuntimeOverrides
		var groups map[string]json.RawMessage
		if json.Unmarshal(saved, &actual) != nil || json.Unmarshal(saved, &groups) != nil || len(groups) != 11 ||
			savedRevision != revision || !bytes.Equal(saved, originalStored) || !reflect.DeepEqual(actual, want) {
			t.Fatalf("%s changed a runtime group, Auto/manual thread choice, source path, or exact revision", database.name)
		}
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func managedTranscodingArchiveRuntimeOverrides() settings.RuntimeOverrides {
	bindHost, port, threads := "127.0.0.1", 10096, 3
	softwareToneMapping, vulkanToneMapping := true, false
	tempPath := "/srv/source-managed-transcodes"
	codecs := transcode.CodecConfigurations{
		{CodecId: transcode.H264SoftwareCodecID, IsEnabled: true, Priority: transcode.MinCodecPriority},
		{CodecId: transcode.HEVCSoftwareCodecID, IsEnabled: false, Priority: transcode.MaxCodecPriority},
	}
	return settings.RuntimeOverrides{
		Network:  &settings.NetworkOverrides{BindHost: &bindHost, HttpPort: &port},
		Hardware: &settings.HardwareSelection{Decode: "software", Encode: "software"},
		Threads:  &threads,
		H264: &transcode.CPUQuality{Preset: "slower", RateControl: "capped_crf", CRF: 1,
			ThreadCountSet: true, X264Options: "subme=0:me_range=4:rc_lookahead=10:partitions=none"},
		HEVC: &transcode.CPUQuality{Preset: "placebo", RateControl: "bitrate", CRF: 51,
			ThreadCount: 16, ThreadCountSet: true},
		SoftwareToneMapping: &softwareToneMapping,
		VulkanToneMapping:   &vulkanToneMapping,
		Transcoding: &settings.TranscodingPolicy{EnableHevcEncoding: false, EnableHardwareEncoding: false,
			DownMixAudioBoost: 2, EnableSubtitleExtraction: false, EnableOnTheFlyAttachmentExtraction: true},
		TranscodingTempPath:    &tempPath,
		SoftwareToneMapOptions: &transcode.SoftwareToneMapOptions{ToneMapSizeLimit: "Limit720", SoftwareToneMapAlgorithm: "Reinhard"},
		CodecConfigurations:    &codecs,
	}
}
