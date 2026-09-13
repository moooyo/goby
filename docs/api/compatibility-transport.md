# Compatibility query transport

The authenticated Configuration, Devices, Auth/Keys, ScheduledTasks, and
activity/log adapters separate the following query carriers from their business
parameters. Names are case-insensitive:

- `api_key` and `X-Emby-Token` carry an issued Emby login or application token.
- `X-Emby-Client`, `X-Emby-Client-Version`, `X-Emby-Device-Id`, and
  `X-Emby-Device-Name` describe the client; they never establish user authority.

The authentication parser checks query and header carriers together. Matching
repeated values retain that parser's existing policy; conflicting token or
client values are rejected before the endpoint runs. Client metadata is bounded
to 256 UTF-8 bytes per field without NUL bytes. This transport handling does not
authenticate an otherwise anonymous request or make a native administrator
cookie usable in the compatibility namespace.

Each adapter keeps its own allowlist, casing, cardinality, type, and size rules
for business parameters. Unknown `X-Emby-*` names are not discarded. Language and
format hints, including `X-Emby-Language` and `ReqFormat`, remain subject to each
endpoint's separately documented contract; they are not universal exceptions.
Native administrator endpoints retain their existing query restrictions.

This is Goby's implemented transport contract. It does not claim additional
reference-client acceptance or broader endpoint behavior.
