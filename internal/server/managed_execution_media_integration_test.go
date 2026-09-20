//go:build linux

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/transcode"
)

func TestHTTPManagedAMDSelectionExecutesRealMediaAndRetainsAdmittedPlan(t *testing.T) {
	device := os.Getenv("GOBY_TEST_VAAPI_DEVICE")
	if device == "" {
		t.Skip("GOBY_TEST_VAAPI_DEVICE selects the explicitly admitted AMD worker")
	}
	if os.Getenv("GOBY_FFMPEG") == "" || os.Getenv("GOBY_FFPROBE") == "" || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("the selected AMD worker requires explicit media tools and an isolated database")
	}
	fixture := newHLSHTTPFixture(t, 3*time.Minute)
	item := hardwareEncodingHTTPSource(t, fixture)
	// Start a real generation with CPU defaults and one authorized AMD node.
	// No injected inventory, encoder result or runner establishes availability.
	fixture.server.Close()
	if err := fixture.f.app.Close(fixture.f.ctx); err != nil {
		t.Fatal("close the initial media fixture application")
	}
	cfg := fixture.f.cfg
	cfg.Transcoding.Hardware = transcode.Hardware{Decode: "software", Encode: "software"}
	cfg.Transcoding.AllowedAMDDevices, cfg.Transcoding.AllowedAMDDeviceCount = [8]string{device}, 1
	if err := cfg.Transcoding.Validate(); err != nil {
		t.Fatal("the selected AMD deployment configuration is invalid")
	}
	app, err := New(fixture.f.ctx, cfg, fixture.f.pool, fixture.f.users, fixture.f.log, "managed-execution-media")
	if err != nil {
		t.Fatal("start the CPU-default application with authorized AMD hardware")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Error("close the managed AMD application")
		}
	})
	fixture.f.app, fixture.f.cfg, fixture.f.handler = app, cfg, app.Handler()
	server := httptest.NewServer(fixture.f.handler)
	fixture.server = server
	t.Cleanup(server.Close)
	cookie, csrf := fixture.f.adminLogin(t)
	headers := http.Header{"Cookie": {cookie.String()}, "X-CSRF-Token": {csrf}, "Origin": {cfg.PublicURL}}
	readSettings := func() map[string]any {
		t.Helper()
		response := fixture.request(t, http.MethodGet, "/admin/v1/settings", nil, headers)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		var value map[string]any
		if json.Unmarshal(response.body, &value) != nil {
			t.Fatal("read the managed AMD settings response")
		}
		return value
	}
	initial := readSettings()
	runtime := objectValue(t, initial, "Runtime")
	defaults := objectValue(t, objectValue(t, runtime, "Defaults"), "Hardware")
	devices, ok := objectValue(t, runtime, "Hardware")["Devices"].([]any)
	if !ok || len(devices) != 1 || defaults["Decode"] != "software" || defaults["Encode"] != "software" || defaults["DeviceId"] != "" {
		t.Fatal("the actual startup did not preserve CPU defaults and its single authorized node")
	}
	selected, ok := devices[0].(map[string]any)
	if !ok || selected["Available"] != true || selected["DeviceId"] != managedHardwareDeviceID(device) {
		t.Fatal("the explicitly selected AMD node lacks actual startup identity admission")
	}
	deviceID := selected["DeviceId"]
	update := func(current map[string]any, encode string, threads int) map[string]any {
		return map[string]any{"Revision": current["Revision"], "Overrides": current["Overrides"],
			"Runtime": map[string]any{"Hardware": map[string]any{"Decode": "software", "Encode": encode, "DeviceId": deviceID}, "Threads": threads}}
	}
	body := update(initial, "vaapi", 3)
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodPut, "/admin/v1/settings", body, fixture.accounts.viewer.headers), http.StatusUnauthorized)
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodPut, "/admin/v1/settings", body, headers), http.StatusOK)
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodPut, "/admin/v1/settings", body, headers), http.StatusConflict)
	saved := readSettings()
	effective := objectValue(t, objectValue(t, saved, "Runtime"), "Effective")
	if objectValue(t, effective, "Hardware")["Encode"] != "vaapi" || effective["Threads"] != float64(3) || saved["Revision"] == initial["Revision"] {
		t.Fatal("the native CAS edit did not publish its selected AMD execution profile")
	}
	profile := videoHTTPProfile("http", false, false)
	profile["MaxWidth"], profile["MaxHeight"] = 320, 180
	request := videoHTTPBody(false, profile)
	prepared := videoHTTPPrepare(t, fixture, item, request, "http")
	if videoHTTPJobCount(t, fixture, prepared.playID, false) != 0 {
		t.Fatal("PlaybackInfo unexpectedly launched the media producer")
	}
	response := fixture.request(t, http.MethodGet, prepared.uri.String(), nil, nil)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	want := videoHTTPOutput{codec: "h264", pixelFormat: "yuv420p", width: 320, height: 180, frames: 48, seconds: 2, color: [3]int{255, 0, 0}}
	videoHTTPVerifyMP4(t, fixture, response.body, want)
	session, record := videoHTTPRecord(t, fixture, prepared.playID, "h264", 0)
	admitted := record.Spec.Plan
	t.Logf("stage=managed_gpu state=%s decode=%s encode=%s device_present=%t execution_version=%d threads=%d",
		record.State, admitted.Hardware.Decode, admitted.Hardware.Encode, admitted.Hardware.Device != "", admitted.ExecutionVersion, admitted.Execution.Threads)
	if record.State != "completed" || admitted.Hardware != (transcode.Hardware{Decode: "software", Encode: "vaapi", Device: device}) ||
		admitted.ExecutionVersion != transcode.ExecutionVersion || admitted.Execution.Threads != 3 {
		t.Fatal("real media execution did not consume the admitted AMD device and thread capture")
	}
	// Preserve the chosen DeviceId while switching the new-admission CPU profile.
	// The completed hardware producer must not acquire these later choices.
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodPut, "/admin/v1/settings", update(saved, "software", 5), headers), http.StatusOK)
	var persisted []byte
	var historical transcode.Plan
	if fixture.f.pool.QueryRow(fixture.f.ctx, "SELECT plan FROM encoding_jobs WHERE id=$1", record.ID).Scan(&persisted) != nil ||
		json.Unmarshal(persisted, &historical) != nil || historical != admitted {
		t.Fatal("a settings edit rewrote the persisted admitted hardware plan")
	}
	retained, err := app.hls.manager.Snapshot(session.key.scope, record.ID)
	if err != nil || retained.Spec.Plan != admitted {
		t.Fatal("a settings edit changed the live admitted hardware record")
	}
	cpuSettings := readSettings()
	if objectValue(t, objectValue(t, objectValue(t, cpuSettings, "Runtime"), "Effective"), "Hardware")["DeviceId"] != deviceID {
		t.Fatal("a CPU selection discarded the administrator's retained device choice")
	}
	cpu := videoHTTPPrepare(t, fixture, item, request, "http")
	response = fixture.request(t, http.MethodGet, cpu.uri.String(), nil, nil)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	videoHTTPVerifyMP4(t, fixture, response.body, want)
	// Negotiating the same source can legitimately reuse its Prepared play ID.
	// Both immutable encoding revisions then match the generic codec/start
	// helper; identify the new producer independently of its expected settings.
	cpuSession, cpuRecord := managedExecutionNewHTTPRecord(t, fixture, cpu.playID, record.ID)
	t.Logf("stage=managed_cpu state=%s decode=%s encode=%s device_present=%t execution_version=%d threads=%d job_reused=%t play_reused=%t",
		cpuRecord.State, cpuRecord.Spec.Plan.Hardware.Decode, cpuRecord.Spec.Plan.Hardware.Encode,
		cpuRecord.Spec.Plan.Hardware.Device != "", cpuRecord.Spec.Plan.ExecutionVersion, cpuRecord.Spec.Plan.Execution.Threads,
		cpuRecord.ID == record.ID, cpu.playID == prepared.playID)
	if cpuRecord.State != "completed" || cpuRecord.Spec.Plan.Hardware != (transcode.Hardware{Decode: "software", Encode: "software"}) ||
		cpuRecord.Spec.Plan.ExecutionVersion != transcode.ExecutionVersion || cpuRecord.Spec.Plan.Execution.Threads != 5 || cpuRecord.ID == record.ID {
		t.Fatal("new CPU admission retained an inactive device or reused old execution settings")
	}
	var cpuPersisted transcode.Plan
	if fixture.f.pool.QueryRow(fixture.f.ctx, "SELECT plan FROM encoding_jobs WHERE id=$1", cpuRecord.ID).Scan(&persisted) != nil ||
		json.Unmarshal(persisted, &cpuPersisted) != nil || cpuPersisted != cpuRecord.Spec.Plan {
		t.Fatal("the new CPU producer did not persist its actual admitted plan")
	}
	if app.cfg.Transcoding.Hardware != cfg.Transcoding.Hardware {
		t.Fatal("managed execution mutated the CPU deployment defaults")
	}
	for _, played := range []struct {
		id      string
		session *hlsSession
	}{{prepared.playID, session}, {cpu.playID, cpuSession}} {
		expectHLSHTTPStatus(t, fixture.request(t, http.MethodDelete, videoHTTPStopPath(fixture.accounts.viewer, played.id), nil, fixture.accounts.viewer.headers), http.StatusNoContent)
		videoHTTPWaitRetired(t, fixture, played.id, []*hlsSession{played.session})
	}
	expectHLSHTTPStatus(t, fixture.request(t, http.MethodDelete, "/admin/v1/session", nil, headers), http.StatusNoContent)
}

func managedExecutionNewHTTPRecord(t *testing.T, fixture *hlsHTTPFixture, playID, previousJobID string) (*hlsSession, transcode.Record) {
	t.Helper()
	var selectedSession *hlsSession
	var selectedRecord transcode.Record
	newProducers := 0
	for _, session := range videoHTTPSessions(fixture, playID) {
		session.mu.Lock()
		producers := append([]hlsProducer(nil), session.producers...)
		session.mu.Unlock()
		for _, producer := range producers {
			if producer.id == previousJobID {
				continue
			}
			newProducers++
			record, err := fixture.f.app.hls.manager.Snapshot(session.key.scope, producer.id)
			if err != nil {
				t.Fatalf("inspect the new scoped managed producer (%T)", err)
			}
			selectedSession, selectedRecord = session, record
		}
	}
	if newProducers != 1 {
		t.Fatalf("new managed HTTP admission must own exactly one distinct producer: count=%d", newProducers)
	}
	return selectedSession, selectedRecord
}
