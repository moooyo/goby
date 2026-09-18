package server

// cancelPlaybackResources retires every delivery mode for the same verified
// playback owner. Direct files and dynamic sources remain cancellable when the
// ordinary conversion runtime is disabled or no HLS registration exists.
func (s *Server) cancelPlaybackResources(authID, playID string) {
	if authID == "" {
		return
	}
	s.cancelMediaPolicy(authID, playID)
	s.cancelDynamicStreams(authID, playID)
	s.hls.cancelMatching(authID, playID)
}

func (s *Server) cancelPlaybackCredential(authID string) {
	if authID == "" {
		return
	}
	s.cancelMediaPolicy(authID, "")
	s.cancelDynamicStreams(authID, "")
	s.hls.cancelCredential(authID)
}
