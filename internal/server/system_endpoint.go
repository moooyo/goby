package server

import (
	"net"
	"net/http"
	"net/netip"
)

type systemEndpointInfo struct {
	IsLocal     bool
	IsInNetwork bool
}

// Endpoint classification describes the resolved client connection. It does
// not grant permissions or override playback policy. The observed reference
// loopback response is {"IsLocal":true,"IsInNetwork":true}; other address
// classes below are Goby's explicit policy, not a complete reference matrix.
func (s *Server) endpointInfo(r *http.Request) systemEndpointInfo {
	peer, err := netip.ParseAddr(s.policyClientAddress(r))
	if err != nil || peer.IsUnspecified() || peer.IsMulticast() {
		return systemEndpointInfo{}
	}
	peer = peer.Unmap()
	local := peer.IsLoopback()
	if address, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if host, _, err := net.SplitHostPort(address.String()); err == nil {
			if server, err := netip.ParseAddr(host); err == nil && !server.IsUnspecified() {
				local = local || peer == server.Unmap()
			}
		}
	}
	inNetwork := local || peer.IsPrivate() || peer.IsLinkLocalUnicast()
	if s.serverPolicy != nil {
		inNetwork = local || s.serverPolicy.Snapshot().IsLocal(peer.String())
	}
	return systemEndpointInfo{
		IsLocal:     local,
		IsInNetwork: inNetwork,
	}
}

func (s *Server) systemEndpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, s.endpointInfo(r))
}
