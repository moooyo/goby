# Administrator codec and directory access observations

## Codec interfaces

`GET /admin/v1/hardware/codecs` uses the native administrator cookie. It does
not execute FFmpeg, start a media job, or open a GPU. It returns the current
saved hardware selection, opaque authorized device identities and availability,
a bounded codec matrix, and retained exact encoder-sample observations.

`POST /admin/v1/hardware/codecs/refresh` requires the same administrator cookie,
same-origin checks and CSRF token. Its body must be an empty JSON object. It
enumerates the deployment-selected FFmpeg interfaces with the existing bounded
capability reader. The executable, arguments and device paths are never request
inputs. Enumeration shares the existing finite hardware-probe process slot;
a busy slot returns `409 codec_inventory_busy` rather than creating a queue.

The compiled-interface cache is bound to the executable identity before and
after enumeration and expires after fifteen minutes. `CompiledState` is
`unknown`, `current` or `stale`. Every interface's `Compiled` value is null
unless the observation is current. The management response does not return
the executable or device filesystem paths, bearer tokens or loader environment.

Each matrix row distinguishes:

- `Managed`: the current server offers that direction and output codec.
- `Compiled`: the required FFmpeg interface was enumerated; null is unknown.
- `Selected`: the saved backend selection for that direction.
- `EncodingProofs`: unexpired exact encoder tuples from actual existing
  runtime checks, bound to the current authorized device and tool identities.

The managed output set remains H.264, HEVC and AV1. Compiled FFmpeg names do
not expand that output policy. The VA-API decode column establishes that a
decoder interface and the VA-API integration are compiled, not that a device
supports that codec or profile. Device availability and a sample's success
remain separate facts. A sample does not establish support for another source,
profile, bit depth, resolution, filter chain or device. Existing media
diagnostics remain the explicit workflow for running their fixed samples.

The administrator UI reads the snapshot when opened and requires an explicit
refresh to enumerate interfaces. Failed refreshes preserve the last visible
observation with a stale notice and do not replay requests automatically.

The upstream equivalent is the Emby
[video codec information API](https://dev.emby.media/reference/RestAPI/EncodingInfoService/getEncodingCodecinformationVideo.html).
The Goby response above is an independent native management contract, not a
claim of implementing that Emby DTO.

## Directory access

`GET /admin/v1/storage/roots` retains the configured order, spelling, duplicates,
canonical-anchor checks, finite storage admission and directory-read behavior.
Each item additionally reports `Readable` and nullable `Writable`.
`Available` retains its existing directory-read meaning.

On Linux, write access uses the held directory descriptor, effective process
credentials and ACLs through `faccessat2`, together with the held filesystem's
read-only mount flag. No probe file is created, modified, renamed or removed.
An unsupported access syscall or uncertain observation returns null; it does
not become a read-only claim. Missing or unreadable directories retain their
negative availability observation and unknown write access.

Emby `POST /Environment/ValidatePath` now accepts `ValidateWriteable=true` for
an allowed directory. It uses the same canonical root, primary-I/O admission
and administrator revalidation as directory browsing. Denied write access
returns `422 directory_not_writable`; uncertain access remains an unavailable
result. Network credentials and file targets remain outside this directory
contract.

These observations describe current service access, not future write success,
directory quotas, or associations between differently spelled paths and a
filesystem volume. Capacity continues to use the independent system-status
measurements and exact-path matching.
