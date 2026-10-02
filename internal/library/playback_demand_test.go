package library

import "testing"

func TestPlaybackReportPauseHintsPreserveCompatibility(t *testing.T) {
	for _, test := range []struct {
		event, hint  string
		paused, want bool
	}{
		{"Progress", "Pause", false, true},
		{"Progress", "Unpause", true, false},
		{"Progress", "TimeUpdate", true, true},
		{"Progress", "Seek", true, true},
		{"Progress", "", false, false},
		{"Started", "Pause", false, false},
		{"Ping", "Unpause", true, true},
	} {
		if got := playbackReportPaused(test.event, test.hint, test.paused); got != test.want {
			t.Fatalf("%s/%s pause = %v, want %v", test.event, test.hint, got, test.want)
		}
	}
}
