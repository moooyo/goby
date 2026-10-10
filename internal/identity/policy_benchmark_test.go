package identity

import (
	"encoding/json"
	"testing"
)

var benchmarkRuntimePolicy ManagedPolicy

func BenchmarkParseRuntimePolicy(b *testing.B) {
	populated := DefaultManagedPolicy()
	populated.EnabledFolders = []string{"movies", "series", "music"}
	populated.EnabledDevices = []string{"living-room", "bedroom"}
	populated.BlockedTags = []string{"restricted", "archived"}
	populated.RestrictedFeatures = []string{FeatureDownloads}
	populated.AccessSchedules = []AccessSchedule{{DayOfWeek: "Weekday", StartHour: 8, EndHour: 22}, {DayOfWeek: "Weekend", StartHour: 9, EndHour: 23}}
	encoded, err := json.Marshal(populated)
	if err != nil {
		b.Fatal(err)
	}
	for _, test := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"legacy defaults", json.RawMessage(`{}`)},
		{"populated", encoded},
		{"playback type fallback", json.RawMessage(`{"EnableMediaPlayback":"false","EnableAllFolders":false,"EnabledFolders":["movies"],"EnableRemoteAccess":false}`)},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for index := 0; index < b.N; index++ {
				policy, err := ParseRuntimePolicy(test.raw)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkRuntimePolicy = policy
			}
		})
	}
}
