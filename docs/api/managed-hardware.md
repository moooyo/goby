# Managed CPU and AMD execution selection

Administrators select separate Decode and Encode axes (`software` or `vaapi`)
and an opaque DeviceId. A managed request cannot submit a device path, loader
environment, FFmpeg argument or a device discovered outside the deployment's
authorization list. These choices apply to new conversion admissions.

## Deployment authorization

`GOBY_ALLOWED_AMD_DEVICES` is a comma-separated list of at most eight unique
canonical `/dev/dri/renderD128` through `/dev/dri/renderD255` paths. Whitespace,
empty entries, aliases, traversal and duplicate paths are rejected. An existing
explicit startup VAAPI node joins that list; the historical renderD128 default
joins it only when a startup VAAPI axis already selects that default. The union
is also limited to eight. A CPU deployment with no authorized nodes does not
implicitly authorize every device under `/dev/dri`.

The process captures a fixed inventory when its generation starts. Each node
must be a nonsymlink character device with the corresponding DRM device number.
Its sysfs association must prove AMD vendor `0x1002`. Node, sysfs association and
vendor-file identities are observed again after the bounded vendor read. The
inventory retains this original identity; a later appearing or replaced node
requires a new process generation before it becomes available.

Opaque IDs are stable for authorized paths, independent of list order. The
management projection includes only DeviceId, a generated label, availability
and a closed reason code. Device paths and filesystem identities remain inside
the server. Saving a known authorized ID is distinct from proving it currently
available. A persisted ID missing from a new deployment keeps administration
available and is reported unavailable; it grants no new device authorization.

The existing non-AMD deployment backend tuple can be reused only as that exact
startup default. It is not a new managed choice or an expanded hardware support
promise. Resetting an override may restore that deployment-owned default.

## Admission, cache and execution

One request snapshot supplies the hardware tuple, thread setting, CPU quality
and tone-map policy alongside its committed output limits. A settings revision
or ServerName change is not itself an encoder evidence cache key. Hardware
evidence remains keyed by the actual tool, loader, device/driver identity and
exact encoder output tuple; compiled support never sets HardwareVerified.

The fixed authorized identity is rechecked before admission, after waiting for
the bounded hardware-probe slot, before reusing cached evidence, and immediately
before an encoder starts after its queue wait. A missing/replaced device cannot
enter the ordinary failed-output-tuple software fallback route. The existing
authorized fallback remains limited to its original source, codec, processing,
profile and permission rules, and captures software quality from the same full
request settings snapshot.

Static HLS, generated HLS, progressive video/audio and existing dynamic-source
plans retain their admitted execution context. Revalidation still checks current
authorization and source facts. Dynamic revisions also retain their hardware
and execution preferences while checking the existing current safety ceilings;
reading an old URL after a settings change does not reselect its encoder.
Explicitly requesting a new dynamic negotiation retains its established
replacement semantics.

A configured media diagnostic captures its current managed profile and settings
revision when the run is admitted. That run never reads a later hardware choice.
Diagnostics remain subject to the deployment's existing opt-in, cgroup, scratch,
toolchain, authority and process-closure controls. A settings save never starts
a diagnostic or claims successful GPU media execution.

## Verification boundary

The managed hardware unit sources use injected device identities and a bounded
fake Linux filesystem to exercise rejected paths, foreign vendors, replacement,
missing IDs and opaque projections. Server sources cover request snapshots,
CPU fallback quality, hardware-cache invalidation after authorization changes,
waiting admissions, retained dynamic revisions and configured diagnostic
capture. These are test sources, not a claim of executed validation or actual
AMD codec support. The phase acceptance record identifies the real execution
environment and observed codec/filter outcomes.
