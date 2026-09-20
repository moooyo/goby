package recovery

import (
	"testing"

	"github.com/moooyo/goby/internal/settings"
)

func TestHostSettingsCaptureRejectsMixedAuthorityAndNoncanonicalRevision(t *testing.T) {
	threads, port := 7, 10096
	for _, fixture := range []struct {
		name            string
		capture         *hostSettingsCapture
		operator, valid bool
	}{
		{name: "missing_online"},
		{name: "missing_offline", operator: true},
		{name: "exact_large_online", capture: &hostSettingsCapture{Revision: "9007199254740993", Settings: settings.TargetHostSettings{Threads: &threads}}, valid: true},
		{name: "offline_defaults", capture: &hostSettingsCapture{Revision: "0"}, operator: true, valid: true},
		{name: "offline_foreign_revision", capture: &hostSettingsCapture{Revision: "1"}, operator: true},
		{name: "offline_foreign_override", capture: &hostSettingsCapture{Revision: "0", Settings: settings.TargetHostSettings{Threads: &threads}}, operator: true},
		{name: "online_zero", capture: &hostSettingsCapture{Revision: "0"}},
		{name: "leading_zero", capture: &hostSettingsCapture{Revision: "01"}},
		{name: "fractional", capture: &hostSettingsCapture{Revision: "1.0"}},
		{name: "overflow", capture: &hostSettingsCapture{Revision: "9223372036854775808"}},
		{name: "invalid_hardware", capture: &hostSettingsCapture{Revision: "1", Settings: settings.TargetHostSettings{Hardware: &settings.HardwareSelection{Decode: "vaapi", Encode: "vaapi"}}}},
		{name: "bounded_target_port", capture: &hostSettingsCapture{Revision: "1", Settings: settings.TargetHostSettings{Network: &settings.NetworkOverrides{HttpPort: &port}}}, valid: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if got := fixture.capture.valid(fixture.operator); got != fixture.valid {
				t.Fatalf("host capture authority admission=%t want%t", got, fixture.valid)
			}
		})
	}
}
