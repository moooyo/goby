# Managed provider credentials

The provider configuration singleton stores administrator overrides as one
authenticated encrypted document. The shared protected master file uses a
separate `GPC\x01` envelope and revision-bound associated data. It participates in
master-file admission and backup/recovery validation. Historical archives that
predate the singleton remain valid.

An omitted field keeps its saved state. `replace` stores an explicit value,
`clear` stores an explicit empty override, and `default` removes the override so
the deployment value applies again. Public responses expose only field labels,
presence, and source. Credentials and application identification are never
returned. OpenSubtitles username and password remain a pair.

Successful writes publish one immutable snapshot for new requests and newly
starting workers. Existing work keeps its captured credentials and policy. The
deployment internet gate and managed metadata gate are both still required.
An unconfirmed commit disables provider use until an authorized refresh resolves
the saved state. Direct SQL edits are not a supported hot reload mechanism.

Explicit connection tests use the saved revision. They perform one bounded
request to a fixed official endpoint, use the existing protected transport,
never download subtitles, and never return upstream response bodies. Provider
reachability and authentication do not guarantee future availability or quota.
