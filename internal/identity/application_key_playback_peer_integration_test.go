package identity_test

import (
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestStoreApplicationKeyPlaybackContextPreservesAuthenticatedPeer(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Playback peer context")
	client := bindApplicationClient(t, ctx, store, key, identity.Client{
		Name: "Playback client", DeviceID: "playback-peer-device", Device: "Playback device", Version: "1",
	})
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id, name, collection_type)
		VALUES ('peer-library', 'Peer Library', 'movies');
		INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
		VALUES ('peer-root', 'peer-library', '/synthetic/peer', '/synthetic', 'peer');
		INSERT INTO items (id, library_id, root_id, name, sort_name, type, path, relative_path)
		VALUES ('peer-item', 'peer-library', 'peer-root', 'Peer Movie', 'peer movie',
		'Movie', '/synthetic/peer/movie.mp4', 'movie.mp4')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO play_sessions
		(id, user_id, auth_session_id, application_client_id, device_id, item_id,
		media_source_id, state, duration_ticks, expires_at, client_correlated)
		VALUES ('play_peer_context', NULL, $1, $2, $3, 'peer-item', 'source_peer-item',
		'Prepared', 90000000, clock_timestamp() + interval '1 hour', true)`,
		key.CredentialID, client.ClientSessionID, client.Client.DeviceID); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name string
		peer string
	}{
		{"loopback-ipv4", "127.0.0.1"},
		{"loopback-ipv6", "::1"},
		{"remote-ipv4", "192.0.2.31"},
		{"remote-ipv6", "2001:db8::31"},
		{"unknown", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// The peer comes from this request's authenticated transport, while
			// the metadata-free media URL recovers a persisted client context.
			previous, err := store.ResolveEmbyForClientWithPeer(ctx, key.Token, identity.Client{}, testCase.peer)
			if err != nil || previous.PeerIP != testCase.peer {
				t.Fatalf("authenticate transport context: %v", err)
			}
			recovered, err := store.ResolveApplicationKeyPlaybackContext(ctx, previous, "play_peer_context")
			if err != nil {
				t.Fatalf("recover authenticated playback context: %v", err)
			}
			if recovered.PeerIP != previous.PeerIP || identity.IsLocalPeer(recovered.PeerIP) != identity.IsLocalPeer(previous.PeerIP) {
				t.Fatal("playback context recovery lost the current authenticated transport peer")
			}
			if !recovered.IsApplicationKey() || recovered.ApplicationKeyID != key.ID ||
				recovered.SessionID != key.CredentialID || recovered.ClientSessionID != client.ClientSessionID ||
				recovered.Client != client.Client || recovered.User.ID != "" {
				t.Fatal("peer preservation changed playback credential or client ownership")
			}
			revalidated, err := store.RevalidateSession(ctx, recovered)
			if err != nil {
				t.Fatalf("revalidate authenticated playback context: %v", err)
			}
			if revalidated.PeerIP != recovered.PeerIP || revalidated.SessionID != recovered.SessionID ||
				revalidated.ApplicationKeyID != recovered.ApplicationKeyID || revalidated.ClientSessionID != recovered.ClientSessionID {
				t.Fatal("session revalidation changed authenticated peer or playback ownership")
			}
		})
	}
}
