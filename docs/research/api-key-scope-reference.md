# Application-Key Target Scope Reference

This third independent application-key study covers target-user library
visibility and separately controlled playback policy flags on official Emby
Server `4.9.5.0`. At `2026-09-09T20:56:11Z`, it added **27 sanitized records:
26 complete HTTP exchanges and one audit observation**. The preceding 1101
records remain unchanged; the corpus now contains 1128 records. Evidence uses
the `keys-scope-m5d-` prefix in the
[reference fixture directory](../../tests/compatibility/fixtures/reference/emby-4.9.5.0).

The tested user-scoped catalog routes obeyed the explicit target user's folder
permissions, even when authenticated by an application key. In separate profile
controls, disabling the user alone or disabling media playback alone did not
remove the negotiated conversion capability when transcoding/remux permissions
remained enabled. These findings narrow the combined-policy observations in
the [preceding context study](api-key-context-reference.md).

## Capture boundaries

The [recorder](../../scripts/test-env/reference-api-key-scope.py) ran only
through root SSH on `test-env`, inside the existing reference service's
private network namespace. It revalidated PID `3131777`, root ownership,
`PrivateNetwork=yes`, the ownership markers, the namespace distinction from
the host, and root ownership/mode `0600` of the existing credential file.
Requests used only the established loopback listener `127.0.0.1:18097`.

One fresh administrator login created one ordinary user named
`reference-keys-scope-m5d-20260910-01` and one application key with AppName
`Goby Keys Scope M5d 20260910 01 Alpha`. User creation returned `200` and
confirmed the initial policy: IsAdministrator and IsDisabled were false;
EnableAllFolders, EnableMediaPlayback, EnableAudioPlaybackTranscoding,
EnableVideoPlaybackTranscoding, and EnablePlaybackRemuxing were true.
Only this fresh user's policy, the new key, and the fresh login were mutated.
No password or login was created for the temporary user. No old user, library,
source, or metadata was changed.

The hard bound was 26 HTTP requests, 256 KiB per response, and 2 MiB in total.
Actual response bodies totaled **53,822 bytes**, all complete. Profile requests
used the existing two-second synthetic video item `"18"`, with known source
`"mediasource_18"`. No media bytes were fetched and no returned conversion URL
was followed. This study therefore establishes negotiation and catalog
behavior, not conversion execution or actual playback enforcement.

## Explicit user catalog scope follows folder permissions

The key was supplied in `X-Emby-Token` with no cookies or client authorization
metadata. The first two requests used the fresh user's initial all-folders
policy. The administrator then posted a full copy of that initial policy with
only `EnableAllFolders: false` and `EnabledFolders: []` changed. This policy
update returned `204`. The same catalog routes were immediately requested
again, followed by a key-only global Items control.

| Target policy and route | HTTP | Returned Items length | TotalRecordCount | UserData |
| --- | --- | --- | --- | --- |
| All folders: `GET /Users/{id}/Views` | `200` | 9 | 9 | Present on all returned views |
| All folders: `GET /Users/{id}/Items?Recursive=true&Limit=2` | `200` | 2 | 34 | Present on both returned items |
| No folders: `GET /Users/{id}/Views` | `200` | 0 | 0 | No items |
| No folders: `GET /Users/{id}/Items?Recursive=true&Limit=2` | `200` | 0 | 0 | No items |
| Same key, no UserId: `GET /Items?Recursive=true&Limit=2` | `200` | 2 | 54 | Omitted on both returned items |

All five replies were JSON with
`Content-Type: application/json; charset=utf-8`. Evidence:
`folders-initial-{views,items}`, `folders-denied-set-policy`,
`folders-denied-{views,items}`, and `folders-denied-no-user-items`.
The exact two empty scoped responses were:

```json
{
  "Items": [],
  "TotalRecordCount": 0
}
```

The initial scoped Items page contained a Folder (`"9"`) and a Series
(`"12"`), both with UserData. The global control contained a Genre (`"21"`)
and the same Folder (`"9"`) with UserData omitted. The total-count difference
between the user-scoped and global routes is an observed query result, not a
claim that they enumerate identical item populations.

The folder-denied policy was not separately fetched between its `204` response
and the catalog checks; the captured posted policy and both empty catalog
results establish this controlled change. The subsequent independent playback
policies restored the initial all-folders setting, as confirmed by their user
DTOs. No single-folder grant, item detail outside permitted libraries,
subfolder exclusion, rating/tag filter, or media-delivery ACL was tested.

The reference result is incompatible with universally bypassing target-user
library visibility just because the authenticating credential is an API key.
It is also distinct from restricting the key's userless global catalog request
to a recently queried user's library permissions.

## No-user profile baseline fails before policy changes

Before any policy update, the key posted the same valid forced-conversion
profile used in the preceding study, with UserId omitted. The request included
`IsPlayback: true`, `AutoOpenLiveStream: false`, both direct-play flags false,
EnableTranscoding true, both copy flags false, and an HLS/TS H.264/AAC
TranscodingProfile. No conversion URL was requested.

The response was `500`, `text/plain`, with the exact body:

```text
Object reference not set to an instance of an object.
```

Evidence: `profile-initial-no-user`. The immediately following request added
the fresh user's UserId and returned `200` with a negotiated
HLS URL and SupportsTranscoding true. Evidence:
`profile-initial-explicit-user`.

This removes the earlier study's ordering ambiguity: the no-user failure also
occurs before a test-user restriction is applied. It does not establish that
every DeviceProfile, all no-user negotiation, or the minimal profile-less
requests fail; the [first playback study](api-key-playback-reference.md)
observed successful minimal no-user negotiation.

## Independent disabled and playback-disabled controls

Each control posted a full copy of the fresh user's original policy with only
one boolean changed. Both reset EnableAllFolders to true and retained all
three transcoding/remux permissions as true. Each policy POST returned `204`,
and the subsequent key-authenticated user GET returned `200` and confirmed
the exact flags before negotiation.

| Explicit-user profile case | IsDisabled | EnableMediaPlayback | Audio/video transcoding and remux | HTTP | SupportsTranscoding | Direct support flags | TranscodingUrl |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Initial policy | false | true | All true | `200` | true | Both false | Present |
| Only user disabled | true | true | All true | `200` | true | Both false | Present |
| Only media playback disabled | false | false | All true | `200` | true | Both false | Present |

Evidence: `profile-initial-explicit-user`,
`user-disabled-only-{set-policy,user,profile}`, and
`playback-disabled-only-{set-policy,user,profile}`. The exact profile is
defined in the [context recorder](../../scripts/test-env/reference-api-key-context.py)
and reproduced in the [context research document](api-key-context-reference.md).

All three successful responses had the same capability flags, source ID,
DefaultAudioStreamIndex `1`, HLS subprotocol, TS transcoding container, and
conversion options. Each returned its own PlaySessionId. ErrorCode was omitted.
The relative TranscodingUrl specified H.264/AAC, both copy flags false, and
`TranscodeReasons=ContainerNotSupported,DirectPlayError`; it was never followed.

Compared with the previous study, the loss of negotiated conversion capability
under combined restrictions cannot be attributed to IsDisabled or
EnableMediaPlayback alone. The prior cases also disabled audio transcoding,
video transcoding, and remuxing. This matrix does not isolate those three
permissions from each other, and does not establish how actual conversion or
stream requests enforce the independently disabled flags.

## Preservation and cleanup audit

The owned user DELETE, owned key DELETE, and administrator logout each
returned `204`. Final user membership and every preexisting user's policy
matched the initial records; the final key list matched the initially empty
list. The service PID and isolation checks still matched. Evidence:
`user-delete`, `users-final`, `cleanup-key-0`, `keys-final`, `admin-logout`,
and `audit`.

All **2202 preceding raw/export files**, **240 known source paths**, and
**1498 existing private files** retained their SHA-256 hashes. These sets
overlap and their counts are not additive. Every new raw/export pair passed
the inherited secret-redaction and mode-`0600` checks. Raw evidence remains
under `/opt/goby-test/exec-scratch/keys-scope-m5d/private`; only sanitized
exports were copied into the repository.

No old capture or prior recorder source was overwritten. No local tests,
validators, builds, runtime probes, or git operations ran. Source checks and
response analysis ran only through SSH on `test-env`. No product
implementation was changed by this study.
