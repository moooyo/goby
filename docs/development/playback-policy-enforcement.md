# Playback policy enforcement

`SimultaneousStreamLimit` is enforced at media delivery admission. Preparing a
playback session or negotiating `PlaybackInfo` does not consume this allowance.
Original files, progressive responses, HLS output, and dynamic-source output use
the same gate in a server process. Existing original-response and conversion
manager resource limits remain independent and may reject a request earlier.

A canonical play session, credential, device, item, and source identify one
delivery lease. Multiple byte ranges, HLS segments, output revisions, and
renditions for that identity share the lease. Another login for the same user
shares the user's limit. Userless application credentials retain their separate
server-owned authority and do not borrow a request user's policy.

Legacy original-file requests without a play session identifier are grouped by
credential, device, item, and source. This contract cannot distinguish two
players opening the same source on the same authenticated device. A supplied
client play reference is resolved to its owned canonical session before it can
deduplicate original delivery with a conversion.

Active response references keep a lease alive. When the last segment or range
response ends, a 90-second idle interval allows the next request to reuse that
lease. Full original responses and completed progressive responses release it
after their last active reader. Playback stop, credential revocation, policy
denial, and server shutdown cancel the associated delivery contexts and
producers. Idle expiration also cancels producers; a client must renegotiate an
expired output revision before resuming it. If a limit is lowered below
the existing count, the oldest admitted plays are retained and newer excess
plays are cancelled during current-policy revalidation.

HEAD requests do not acquire a playback lease or start an encoder. An uncached
HLS HEAD may expose its content type without inventing a length or validator.
This accounting is process-local; it is not a distributed quota between
independent Goby server instances.

`RemoteClientBitrateLimit` is a media planning and admission ceiling in bits per
second. The trusted authenticated transport peer determines whether it applies;
an untrusted forwarding header cannot select a local-network exemption. Original
delivery uses the largest available source-total, stream-total, or physical
size/duration average. Unknown or excessive source rates are rejected when a
remote ceiling applies. Copied tracks conservatively reserve the source rate.

Negotiation may retry a rejected copy candidate with authorized encoding. An
explicit codec-copy requirement is preserved and can make that request
unsupported. Executable encoder targets, lossless payload bounds, and HLS
transport reservations are checked again at registration and fresh source
authorization; projected output metadata alone cannot authorize a response.
Existing long-response and producer guards recheck current policy. HLS registry
identities distinguish local and remote authorization contexts so a reused
session cannot retain the wrong network policy snapshot.

The bitrate policy is not packet pacing and does not guarantee an instantaneous
wire-rate ceiling for HTTP bursts or variable-bitrate media. It constrains
selected media and encoder budgets rather than changing source declarations.
