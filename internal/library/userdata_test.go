package library

import "testing"

func TestPlaybackAllowedFailsClosedForMalformedValues(t *testing.T) {
	for _, fixture := range []struct {
		policy  string
		allowed bool
	}{
		{`{}`, true},
		{`{"EnableMediaPlayback":true}`, true},
		{`{"EnableMediaPlayback":false}`, false},
		{`{"EnableMediaPlayback":null}`, false},
		{`{"EnableMediaPlayback":"true"}`, false},
		{`{"EnableMediaPlayback":1}`, false},
		{`{"EnableMediaPlayback":[]}`, false},
		{`null`, false},
		{`[]`, false},
		{`true`, false},
		{`{`, false},
	} {
		t.Run(fixture.policy, func(t *testing.T) {
			if allowed := playbackAllowed([]byte(fixture.policy)); allowed != fixture.allowed {
				t.Errorf("playbackAllowed(%s) = %v, want %v", fixture.policy, allowed, fixture.allowed)
			}
		})
	}
}
