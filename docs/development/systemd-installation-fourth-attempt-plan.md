# Fourth systemd installation attempt

Status: the actual r04 runtime passed both nonroot starts, 272 HTTP requests,
six SQL observers and three snapshots. Its sealer failed on a valid Emby 401
plain-text response incorrectly classified as JSON. The final SQL observer did
not run, so installation acceptance remains open. All owned processes and
readers are closed; failure preservation and independent archive/directory/
resource readback passed. The installation paths and PG tmpfs are released.
See the
[actual result](systemd-installation-fourth-attempt.json). The three earlier
attempts remain consumed, privately preserved and independently closed.

This attempt addresses the actual r03 SQL observer failure: PostgreSQL emitted
an uncast OID as a JSON string, while the identity reader required an integer.
The package binary, shipped Type=simple unit and production source are unchanged.

## Demonstrated correction and remaining proof

The controller explicitly casts all five numeric OID projections to bigint.
Runtime and the final sealer share the final observer marker expression and
strict identity reader. One real read-only observer runs before the first APP
start and checks the runtime marker, PostgreSQL client identities and final
sealer marker. A malformed identity stops the sequence before application use.

Twelve [remote synthetic cases](systemd-oid-contract-verification.json) passed using the actual Runtime.sql/run methods
and shared reader with explicit external-dependency substitutes. Independent
review confirmed their saved evidence and source pins. These cases did not
execute PostgreSQL or Seal.final_sql/observer_record; the generated cast text,
sealer reuse and observer inventory also received static review. The later r04
runtime passed the actual prestart query and complete two-start journey. The
HTTP seal and additional final SQL observer remain unaccepted.

The synthetic evidence is retained under
`/opt/goby-test/m6-systemd-oid-contract-20260915/private`:

| Receipt | Bytes | SHA256 |
| --- | ---: | --- |
| checks-01/result.json | 3950 | df4791d25b18567db9f79977725bca0992eeaf27ffcd6465c15c5db9380b44d9 |
| checks-dispatch.json | 771 | e43720e82782428a6b0dd2555318a22ce9b74c25389e8c8a7084068ba99cd57d |
| independent-review.json | 6623 | 7d5f53a7a675dcf170848acff785e8c477af594dba89347247535111925f13ed |

## Frozen fresh scope

This section retains the contract used by the consumed r04 sequence.

Use evidence `/opt/goby-test/m6-systemd-install-20260915-r04`, fixture
`/opt/goby-m6-install-fixture-20260915-r04`, and unit prefix
`goby-m6-install-20260915-r04`. Eight helpers and the preparation input are frozen
before any service starts. Function/class source bridges match the tested OID
candidates or the retained r03 helpers; scope constants and preparation hashes
are separately rebound. Original tool evidence remains at its E12 paths.

The read-only preflight passed eight compile/global checks, seven function/class
bridges, four invalid CLI entries, sixteen closed metadata commands, package
and tool checks, path/unit absence, capacity and unchanged protected state.
It performed no SQL, HTTP, service action or valid actor invocation. At this
checkpoint root free space was 5,252,120,576 bytes and available memory was
6,608,891,904 bytes; the actual preparation rechecks capacity at admission.

Retain the complete [installation acceptance](systemd-package-plan.md): the
exact shipped nonroot installation, fresh private PG cluster and credentials,
independent 768 MiB PG tmpfs/network namespace, seven real media files, two
distinct Goby starts, bootstrap/login and credential closure, embedded assets,
catalog/ACL/UserData preservation, normal stops and independent resource closure.
The amended SQL inventory is six runtime observers, three snapshots and one
independent final sealer observer. No existing candidate or PG is reused.

Run prepare, runtime and seal consecutively from actual preceding receipts.
The phase ceilings remain 660/1020/1200 seconds, two handoffs each allow sixty
seconds, and the final stop reserve is sixty seconds: 3060 seconds in total.
The original anchor lifetime is 3600 seconds and is never reset. Remaining-time
gates are 2400/2340/1260 seconds; the entry-failure close path remains bounded
to 210 seconds. Failure ends further business and uses only its owned closure.

## Review decision and next queue

The [reviewed execution decision](systemd-installation-fourth-execution-decision.json)
preceded the one actual preparation. This input is now consumed. Its original
failed seal cannot qualify for normal acceptance or be replayed. Preservation
and independent readback have closed this scope. No further installation input
is admitted; the failure does not automatically create another attempt.

With this scope closed, the next independent engineering deliverable is a
bounded native scan and concurrent HTTP capacity profile, with declared media,
latency/RSS/resource limits and closure conditions. The previous tiny-file and
SQL baselines do not establish that profile. Prepare actual component/legal
attribution in parallel, retaining the pending project-license choice.

Core movie/episode/subtitle acceptance still blocks new-binary main promotion.
Keep its consumed actors and unresolved errors unchanged until a specific new
question or justified correction supports another scope. Native arm64, GPU,
host durability, upgrade and the rest of M2-M6 remain independent obligations;
M7 stays deferred.
