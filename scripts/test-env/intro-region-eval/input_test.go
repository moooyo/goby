package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gzipFixture(t *testing.T, raw []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	z := gzip.NewWriter(&output)
	if _, err := z.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func writeFixtureJSON(t *testing.T, path string, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

// This tests admission of an extraction contract, not media extraction or the
// matcher. Media-container hashes remain distinct from synthetic raster hashes.
func extractionFixture(t *testing.T, dir, id string, fill byte) inputSource {
	t.Helper()
	raw := bytes.Repeat([]byte{fill}, expectedFrames*frameBytes)
	compressed := gzipFixture(t, raw)
	gzipName := id + ".gray.gz"
	if err := os.WriteFile(filepath.Join(dir, gzipName), compressed, 0600); err != nil {
		t.Fatal(err)
	}
	info := inputSource{ID: id, EpisodeKey: "series:" + strings.ToLower(id), ManifestPath: filepath.Join(dir, id+".json"), SourceSHA256: digest([]byte("container-" + id)), OrchestratorSHA256: digest([]byte("orchestrator")), DriverSHA256: digest([]byte("driver"))}
	e := extractionManifest{SchemaVersion: 1, CaseID: id, Complete: true}
	e.Raster.Width, e.Raster.Height, e.Raster.PixelFormat, e.Raster.FrameBytes, e.Raster.FullFrame, e.Raster.ScaleFlags = 96, 96, "gray", frameBytes, true, "area"
	e.Sampling.IntervalTicks, e.Sampling.EndTicks, e.Sampling.TicksPerSecond, e.Sampling.Policy = 1_000_000, 1_200_000_000, 10_000_000, extractionPolicy
	e.Source.Path, e.Source.SHA256 = "/unused-original-container/"+id, info.SourceSHA256
	e.Source.IdentityBefore, e.Source.IdentityAfter = json.RawMessage(`{"device":1,"inode":2,"size":3,"mode":33024,"mtimeNs":4,"ctimeNs":5}`), json.RawMessage(`{"device":1,"inode":2,"size":3,"mode":33024,"mtimeNs":4,"ctimeNs":5}`)
	e.Tool.Image, e.Tool.FFmpegSHA256, e.Tool.FFprobeSHA256, e.Tool.OrchestratorSHA256, e.Tool.DriverSHA256 = extractionImage, extractionFFmpeg, extractionFFprobe, info.OrchestratorSHA256, info.DriverSHA256
	e.Audit.FullRawReadbackMatched = true
	e.Raw.Frames, e.Raw.Bytes, e.Raw.SHA256, e.Raw.GzipFile, e.Raw.GzipBytes, e.Raw.GzipSHA256 = expectedFrames, len(raw), digest(raw), gzipName, len(compressed), digest(compressed)
	e.Frames = make([]extractionFrame, expectedFrames)
	for i := range e.Frames {
		e.Frames[i] = extractionFrame{Index: i, NominalTicks: int64(i) * 1_000_000, ActualTicks: int64(i) * 1_000_000, SourceOrdinal: i, RawOffset: i * frameBytes, RawBytes: frameBytes, RawSHA256: digest(raw[i*frameBytes : (i+1)*frameBytes])}
	}
	info.ManifestSHA256 = digest(writeFixtureJSON(t, info.ManifestPath, e))
	return info
}

func cohortFixture(t *testing.T) (string, []inputSource) {
	t.Helper()
	dir := t.TempDir()
	sources := []inputSource{extractionFixture(t, dir, "A", 1), extractionFixture(t, dir, "B", 2), extractionFixture(t, dir, "C", 3)}
	path := filepath.Join(dir, "input.json")
	writeFixtureJSON(t, path, inputManifest{Sources: sources})
	return path, sources
}

func TestAdmissionLoadsBoundFramesOutsideInputDirectory(t *testing.T) {
	path, sources := cohortFixture(t)
	other := t.TempDir()
	for i := range sources {
		relative, err := filepath.Rel(other, sources[i].ManifestPath)
		if err != nil {
			t.Fatal(err)
		}
		sources[i].ManifestPath = relative
	}
	data := writeFixtureJSON(t, filepath.Join(other, "relocated.json"), inputManifest{Sources: sources})
	manifest, err := parseInput(data, other)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range manifest.Sources {
		s, admitted, err := loadSource(context.Background(), info)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Raw) != expectedFrames*frameBytes || len(s.PTS) != expectedFrames || !admitted.IndividualFrameHashesVerified || admitted.ManifestSHA256 != info.ManifestSHA256 || admitted.GzipSHA256 == "" {
			t.Fatal("admitted data lost its bindings")
		}
	}
	if filepath.Dir(path) == other {
		t.Fatal("fixture did not exercise an external input directory")
	}
}

func TestAdmissionRejectsMutatedFrameAndPreservesEarlierSources(t *testing.T) {
	path, sources := cohortFixture(t)
	data, err := os.ReadFile(sources[2].ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var extraction extractionManifest
	if err := json.Unmarshal(data, &extraction); err != nil {
		t.Fatal(err)
	}
	extraction.Frames[17].RawSHA256 = digest([]byte("wrong frame"))
	sources[2].ManifestSHA256 = digest(writeFixtureJSON(t, sources[2].ManifestPath, extraction))
	writeFixtureJSON(t, path, inputManifest{Sources: sources})
	called := false
	report := evaluate(context.Background(), path, func(map[string]source, *budget) (map[string]any, error) { called = true; return nil, nil })
	if called || report.Completed || report.Status != "input-rejected" || len(report.AdmittedSources) != 2 || !strings.Contains(report.Error, "frame SHA256 mismatch") || len(report.Prefix["groups"].([]groupWitness)) != 0 {
		t.Fatalf("unexpected failed admission: %+v", report)
	}
}

func TestInputRejectsAliasedIDsContentAndJSON(t *testing.T) {
	sources := []inputSource{}
	for _, id := range []string{"A", "B", "C"} {
		sources = append(sources, inputSource{ID: id, EpisodeKey: "series:" + strings.ToLower(id), ManifestPath: id + ".json", SourceSHA256: digest([]byte(id)), ManifestSHA256: digest([]byte("manifest" + id)), OrchestratorSHA256: digest([]byte("tool")), DriverSHA256: digest([]byte("driver"))})
	}
	for _, change := range []func([]inputSource){func(s []inputSource) { s[1].ID = "a" }, func(s []inputSource) { s[1].SourceSHA256 = s[0].SourceSHA256 }, func(s []inputSource) { s[1].ManifestPath = "sub/../A.json" }, func(s []inputSource) { s[1].ID = " A" }, func(s []inputSource) { s[1].SourceSHA256 = strings.ToUpper(s[1].SourceSHA256) }} {
		copySources := append([]inputSource(nil), sources...)
		change(copySources)
		data, _ := json.Marshal(inputManifest{Sources: copySources})
		if _, err := parseInput(data, t.TempDir()); err == nil {
			t.Fatal("non-independent or noncanonical input accepted")
		}
	}
	for _, data := range [][]byte{[]byte(`{"sources":[],"Sources":[]}`), []byte(`{"sources":null}`), []byte(`{"sources":[]} {}`), []byte("{\"sources\":[],\"x\":\"\xff\"}")} {
		if _, err := parseInput(data, t.TempDir()); err == nil {
			t.Fatal("ambiguous JSON accepted")
		}
	}
}

func TestGzipRequiresOneCompleteBoundedMember(t *testing.T) {
	raw := []byte("bound frame data")
	good := gzipFixture(t, raw)
	badCRC := append([]byte(nil), good...)
	badCRC[len(badCRC)-8] ^= 1
	cases := map[string][]byte{"second-member": append(append([]byte(nil), good...), good...), "trailing-nul": append(append([]byte(nil), good...), 0), "trailing-space": append(append([]byte(nil), good...), ' '), "truncated": good[:len(good)-1], "checksum": badCRC, "expansion": gzipFixture(t, append(raw, '!'))}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := inflateSingle(context.Background(), data, len(raw)); err == nil {
				t.Fatal("invalid compressed stream accepted")
			}
		})
	}
	if decoded, err := inflateSingle(context.Background(), good, len(raw)); err != nil || !bytes.Equal(decoded, raw) {
		t.Fatalf("valid bounded stream rejected: %v", err)
	}
}

func TestEvaluationFailureCannotPublishPartialGroups(t *testing.T) {
	path, _ := cohortFixture(t)
	for _, failure := range []error{errors.New("patch comparison budget exceeded"), context.Canceled, context.DeadlineExceeded} {
		report := evaluate(context.Background(), path, func(_ map[string]source, work *budget) (map[string]any, error) {
			work.stage = "clique-closure"
			return map[string]any{"complete": true, "groups": []groupWitness{{AnchorStart: 1, AnchorEnd: 11}}, "closure": closureSummary{RejectedAmbiguousGroups: []groupWitness{{AnchorStart: 2, AnchorEnd: 12}}}}, failure
		})
		if report.Completed || report.ProductionResult || len(report.AdmittedSources) != 3 || len(report.Prefix["groups"].([]groupWitness)) != 0 || report.Prefix["complete"] != false || report.Stage != "clique-closure" || len(report.Prefix["closure"].(closureSummary).RejectedAmbiguousGroups) != 0 {
			t.Fatal("failure leaked groups or dropped trustworthy admissions")
		}
	}
}

func TestExtractionRejectsMissingFalseAndZeroAuditFields(t *testing.T) {
	info := extractionFixture(t, t.TempDir(), "A", 1)
	original, err := os.ReadFile(info.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"matcherExecuted", "detectorOutputsUsed"} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(original, &object); err != nil {
			t.Fatal(err)
		}
		delete(object, field)
		info.ManifestSHA256 = digest(writeFixtureJSON(t, info.ManifestPath, object))
		if _, _, err := loadSource(context.Background(), info); err == nil {
			t.Fatalf("missing %s accepted", field)
		}
	}
}

func TestSameEpisodeWithDifferentEncodeHashesIsNotIndependent(t *testing.T) {
	path, sources := cohortFixture(t)
	sources[1].EpisodeKey = sources[0].EpisodeKey
	if sources[1].SourceSHA256 == sources[0].SourceSHA256 {
		t.Fatal("fixture did not provide different encodes")
	}
	data := writeFixtureJSON(t, path, inputManifest{Sources: sources})
	if _, err := parseInput(data, filepath.Dir(path)); err == nil || !strings.Contains(err.Error(), "episodeKey") {
		t.Fatalf("same original episode was admitted: %v", err)
	}
	sources[1].EpisodeKey = ""
	data = writeFixtureJSON(t, path, inputManifest{Sources: sources})
	if _, err := parseInput(data, filepath.Dir(path)); err == nil {
		t.Fatal("missing original episode identity defaulted to a source ID")
	}
}

func TestExplicitAdapterBindsItsOriginalManifest(t *testing.T) {
	path, sources := cohortFixture(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	adapter := filepath.Join(filepath.Dir(path), "adapter.json")
	writeFixtureJSON(t, adapter, inputManifest{Sources: sources, DerivedFrom: &manifestOrigin{ManifestPath: path, SHA256: digest(original)}})
	runner := func(map[string]source, *budget) (map[string]any, error) {
		return map[string]any{"complete": true, "groups": []groupWitness{}}, nil
	}
	report := evaluate(context.Background(), adapter, runner)
	if !report.Completed || report.DerivedFrom == nil || report.DerivedFrom.SHA256 != digest(original) {
		t.Fatal("adapter lost its upstream byte identity")
	}
	if err := os.WriteFile(path, append(original, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	report = evaluate(context.Background(), adapter, runner)
	if report.Completed || len(report.AdmittedSources) != 0 || !strings.Contains(report.Error, "upstream manifest SHA256 mismatch") {
		t.Fatal("mutated upstream manifest was admitted")
	}
}

func TestUnicodeJSONAliasesCannotOverrideExtractionAudit(t *testing.T) {
	info := extractionFixture(t, t.TempDir(), "A", 1)
	original, err := os.ReadFile(info.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct{ from, to string }{
		{`"schemaVersion":1`, `"schemaVersion":99,"ſchemaVersion":1`},
		{`"detectorOutputsUsed":false`, `"detectorOutputsUsed":true,"detectorOutputſUsed":false`},
	} {
		data := bytes.Replace(original, []byte(mutation.from), []byte(mutation.to), 1)
		if bytes.Equal(data, original) {
			t.Fatal("fixture mutation did not apply")
		}
		if err := os.WriteFile(info.ManifestPath, data, 0600); err != nil {
			t.Fatal(err)
		}
		info.ManifestSHA256 = digest(data)
		if _, _, err := loadSource(context.Background(), info); err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
			t.Fatalf("Unicode case alias bypassed the extraction audit: %v", err)
		}
	}
}

func TestInputPathTextCannotClaimBudgetExhaustion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budget exceeded", "missing.json")
	report := evaluate(context.Background(), path, nil)
	if report.Completed || report.Status != "input-rejected" || report.Work.PatchComparisons != 0 {
		t.Fatal("input error text impersonated a computational budget failure")
	}
}

func TestExtractionRequiresACompleteRegularSourceIdentity(t *testing.T) {
	info := extractionFixture(t, t.TempDir(), "A", 1)
	original, err := os.ReadFile(info.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, identity := range []string{`{}`, `{"device":1,"inode":2,"size":3,"mode":null,"mtimeNs":4,"ctimeNs":5}`, `{"device":1,"inode":2,"size":3,"mode":4480,"mtimeNs":4,"ctimeNs":5}`, `{"device":1,"inode":2,"size":3,"mode":33024,"mtimeNs":"4","ctimeNs":5}`} {
		var e extractionManifest
		if err := json.Unmarshal(original, &e); err != nil {
			t.Fatal(err)
		}
		e.Source.IdentityBefore, e.Source.IdentityAfter = json.RawMessage(identity), json.RawMessage(identity)
		info.ManifestSHA256 = digest(writeFixtureJSON(t, info.ManifestPath, e))
		if _, _, err := loadSource(context.Background(), info); err == nil {
			t.Fatal("incomplete or non-regular source identity was admitted")
		}
	}
}
