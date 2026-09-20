# Managed HTTP binding

The managed network setting contains `BindHost` and `HttpPort`. A saved change
selects the next application generation's binding. Saving does not open a socket,
probe a future port, mutate the running listener or restart the process.

Managed hosts are the empty wildcard or a canonical IP literal without a zone
or embedded port. Managed ports are integers from 1 through 65535. A null override
inherits its deployment value. The deployment `GOBY_LISTEN` default may retain a
hostname or ephemeral port 0; an empty address also retains the historical
ephemeral wildcard behavior for direct construction. These deployment-only forms
do not expand the administrator write contract. Numeric service ports are
required. `GOBY_PUBLIC_URL`, secure-cookie policy and proxy trust remain separate
deployment settings.

## Generation ownership

After `Server.New` has loaded its committed settings, `StartupHTTPBinding` freezes
the resolved binding and revision. The generation reserves that exact TCP address,
then calls `PublishHTTPBinding` with the observed listener address and the same
startup receipt. Only then can `Serve` expose the application. An occupied desired
address fails startup; there is no old-port fallback or new restart daemon.

Every non-nil reservation belongs to the generation's existing joined cleanup
pipeline, including a reservation whose publication fails. Stopping ingress
withdraws the runtime observation after closing and joining the listener. A
retired generation cannot publish another listener.

`ManagedHTTPBindingState` accepts the caller's committed desired values. It never
reads a newer settings snapshot during projection. Its detached `Active` value
contains the configured host/port, observed bound host/port and decimal startup
revision. `Active` is null when no reservation was published or ingress was
withdrawn. Runtime observations are never writable or archived.

Pending restart compares desired values with the active generation's configured
values, not with a kernel-assigned ephemeral port. Reverting to that configured
choice clears pending restart without restarting. A successful new generation
reports its new observed port and clears the pending difference. Startup revision
is diagnostic provenance; unrelated settings revisions do not imply a restart.

`ReconnectURL` is supplied only for an explicit, non-wildcard IP host and a known
port. IPv6 is bracketed and mapped IPv4 uses its IPv4 browser address. An unchanged
ephemeral binding can use its observed port. Wildcards and inherited deployment
hostnames do not produce guessed URLs. Neither request `Host` nor proxy headers
participate in this calculation. A future reconnect URL describes a desired
address; it does not assert that a future reservation will succeed.

## Native origin authority

The configured `PublicURL` remains an exact trusted origin, including an HTTPS
reverse proxy. The generation additionally installs `HTTPConnectionContext` as
`http.Server.ConnContext`. It records a concrete TCP local endpoint belonging to
the published listener in a private, generation-owned context value.

A direct origin must be HTTP, have the same port, and contain an IP literal equal
to that observed accepted local IP. The literal hostname `localhost` is admitted
only for an actual loopback local endpoint. Credentials, paths, queries, fragments,
zones, wildcard hosts and arbitrary DNS names are rejected. This check never
resolves an origin hostname and never trusts `Host`, `Forwarded` or any
`X-Forwarded-*` header. A wildcard reservation does not grant wildcard browser
authority: each accepted connection provides its concrete local IP.

Multiple Origin fields are rejected. Cross-site fetch metadata, authentication,
cookie policy and native CSRF validation remain enforced. Adding direct HTTP
origin authority does not turn off secure cookies; an operator using plain HTTP
must already have selected the applicable deployment cookie policy.

## Verification sources

Pure test sources cover frozen startup receipts, desired/active/revert/restart
projection, ephemeral defaults, IPv4 and IPv6, publication rejection, detached
state, retirement, strict origin syntax, request-header forgery and generation
context ownership. Generation tests also cover exact reservation order, no
fallback and ownership of a reservation after publication failure.

The Linux real-listener tests require both the isolated PostgreSQL fixture
environment and `GOBY_TEST_HTTP_BINDING=1`. They use real native cookie-jar login
and CSRF writes, retain an occupied future endpoint while saving, prove that the
old listener remains usable, exercise restart failure and cleanup, then release
the target and authenticate/write on the exact persisted port. A separate IPv6
loopback journey performs native login and a protected write. These sources are
acceptance preparation; pass/fail evidence belongs to the consolidated Phase 4
verification receipt.
