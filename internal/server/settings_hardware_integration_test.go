//go:build linux

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

func TestHTTPSettingsHardwareInspectionIsConsumerScoped(t *testing.T) {
	f, cookie, _, _ := adminSettingsHTTPFixture(t)
	const firstDevice, secondDevice = "/dev/dri/renderD128", "/dev/dri/renderD129"
	var inspections atomic.Int32
	var missing, replaced atomic.Bool
	inventory := newManagedHardwareInventoryWithInspector(config.TranscodingConfig{
		Hardware:          transcode.Hardware{Decode: "software", Encode: "software", Device: firstDevice},
		AllowedAMDDevices: [8]string{secondDevice}, AllowedAMDDeviceCount: 1},
		func(device string) (managedHardwareIdentity, string) {
			inspections.Add(1)
			if missing.Load() {
				return managedHardwareIdentity{}, managedHardwareMissing
			}
			inode := uint64(20)
			if device == secondDevice {
				inode = 30
			}
			if replaced.Load() {
				inode++
			}
			return managedHardwareTestIdentity(inode), ""
		})
	before := f.app.settings.Snapshot()
	store, err := settings.New(f.ctx, f.pool, f.app.library, before.Defaults, before.HostName, settings.RuntimeOptions{
		Network: before.Runtime.DesiredNetwork, Hardware: inventory.defaultSelection(), Execution: before.Runtime.Execution,
		AuthorizedDeviceIDs: inventory.authorizedDeviceIDs(), AvailableDeviceIDs: inventory.authorizedDeviceIDs()})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: f.app.cfg, db: f.pool, identity: f.app.identity, library: f.app.library, log: f.app.log,
		serverID: f.app.serverID, version: f.app.version, dashboardFiles: f.app.dashboardFiles, settings: store, managedHardware: inventory}
	diagnostics, err := newMediaDiagnosticRuntime(s)
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostics.cancel()
	s.mediaDiagnostics = diagnostics
	mux := http.NewServeMux()
	mux.HandleFunc("GET /emby/System/Info/Public", s.publicSystemInfo)
	mux.HandleFunc("GET /admin/v1/overview", s.requireAdmin(s.overview))
	mux.HandleFunc("GET /admin/v1/capabilities", s.requireAdmin(s.capabilities))
	mux.HandleFunc("GET /admin/", s.dashboard)
	s.registerAdminSettingsRoutes(mux)
	handler := s.withSettingsSnapshot(mux)
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil).WithContext(f.ctx)
		r.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}
	inspections.Store(0)
	for _, path := range []string{"/emby/System/Info/Public", "/admin/v1/overview", "/admin/"} {
		if response := request(path); response.Code != http.StatusOK {
			t.Fatalf("scalar-only route %s returned %d", path, response.Code)
		}
	}
	var captured *http.Request
	s.withSettingsSnapshot(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { captured = r })).ServeHTTP(
		httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/planning-observer", nil))
	if inspections.Load() != 0 || captured == nil || s.requestSettings(captured).HardwareSelection != inventory.defaultSelection() {
		t.Fatal("request capture or scalar-only routes inspected selected hardware")
	}
	planning := s.requestPlanningConfig(captured)
	if planning.Hardware.Device != firstDevice || planning.HardwareUnavailable || inspections.Swap(0) != 1 {
		t.Fatal("planning did not inspect and preserve the software codec selection's Vulkan device")
	}
	if response := request("/admin/v1/capabilities"); response.Code != http.StatusOK || inspections.Swap(0) != 1 {
		t.Fatal("capabilities did not inspect current selected hardware exactly once")
	}
	if response := request("/admin/v1/settings"); response.Code != http.StatusOK || inspections.Swap(0) != 2 {
		t.Fatal("settings management did not reuse its device-list observations for the selected device")
	}
	profile, revision, available := s.mediaDiagnostics.captureProfile()
	if !available || profile.Device != firstDevice || revision != before.Revision || inspections.Swap(0) != 1 {
		t.Fatal("diagnostics did not capture and resolve one settings revision")
	}
	options := acceptedBackgroundDolbyVisionOptions(firstDevice)
	s.backgroundPreviews = &backgroundPreviewRuntime{available: true, dolbyVision: backgroundPreviewDolbyVisionState{options: &options}}
	if actual, err := s.backgroundDolbyVisionOptions(f.ctx); err != nil || actual.Device != firstDevice || inspections.Swap(0) != 1 {
		t.Fatal("background preview did not recheck its cached software/Vulkan device")
	}
	replaced.Store(true)
	if actual, err := s.backgroundDolbyVisionOptions(f.ctx); actual != nil || !errors.Is(err, media.ErrBackgroundClipDolbyVisionUnavailable) || inspections.Swap(0) != 1 {
		t.Fatal("background preview reused cached acceptance after device replacement")
	}
	replaced.Store(false)
	actor, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	selection := settings.HardwareSelection{Decode: "vaapi", Encode: "vaapi", DeviceID: managedHardwareDeviceID(secondDevice)}
	threads, toneMapping := 5, false
	updated, err := store.Update(f.ctx, settings.Actor{Principal: actor, Audience: identity.AdministratorNative}, settings.UpdateRequest{
		Revision: before.Revision, Overrides: before.Overrides, Runtime: &settings.RuntimeUpdate{
			Hardware: settings.Change[settings.HardwareSelection]{Present: true, Value: &selection},
			Threads:  settings.Change[int]{Present: true, Value: &threads}, VulkanToneMapping: settings.Change[bool]{Present: true, Value: &toneMapping}}})
	if err != nil {
		t.Fatal(err)
	}
	retained := s.requestPlanningConfig(captured)
	fresh := s.requestPlanningConfig(httptest.NewRequest(http.MethodGet, "/planning-observer", nil))
	if retained.Hardware != planning.Hardware || retained.Execution != planning.Execution ||
		fresh.Hardware.Device != secondDevice || fresh.HardwareUnavailable || fresh.Threads != threads || inspections.Swap(0) != 2 {
		t.Fatal("planning mixed hardware or execution choices from different settings revisions")
	}
	if actual, err := s.backgroundDolbyVisionOptions(f.ctx); actual != nil || !errors.Is(err, media.ErrBackgroundClipDolbyVisionUnavailable) || inspections.Load() != 0 {
		t.Fatal("disabled Vulkan preview performed hardware work or used its cached admission")
	}
	profile, revision, available = s.mediaDiagnostics.captureProfile()
	if !available || profile.Device != secondDevice || revision != updated.Revision || inspections.Swap(0) != 1 {
		t.Fatal("new diagnostic capture did not resolve the updated revision")
	}
	missing.Store(true)
	rejected := s.requestPlanningConfig(captured)
	if !rejected.HardwareUnavailable || rejected.Hardware.Device != "" || inspections.Swap(0) != 1 {
		t.Fatal("a device removed after request capture remained available to planning")
	}
	response := request("/admin/v1/capabilities")
	var capabilities map[string]any
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &capabilities) != nil || objectValue(t, capabilities, "Hardware")["Available"] != false || inspections.Swap(0) != 1 {
		t.Fatal("hardware capabilities concealed current device removal")
	}
	profile, revision, available = s.mediaDiagnostics.captureProfile()
	if available || profile.Device != "" || revision != updated.Revision || inspections.Swap(0) != 1 {
		t.Fatal("diagnostics concealed current hardware removal")
	}
	missing.Store(false)
	replaced.Store(true)
	if available, code := inventory.checkHardware(planning.Hardware); available || code != managedHardwareIdentityChanged {
		t.Fatal("execution identity check accepted a replaced device after planning")
	}
	if available := s.mediaDiagnostics.validateProfile(media.DiagnosticProfile{Decode: "vaapi", Encode: "vaapi", Device: secondDevice}); available {
		t.Fatal("diagnostic execution accepted a replaced captured device")
	}
}
