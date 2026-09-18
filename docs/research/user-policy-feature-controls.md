# User policy quality and feature controls

Research date: **2026-09-19, Asia/Shanghai**. This record is based on read-only
retrieval of the pinned SDK, official Emby documentation, and official hosted Web
application source. No application JavaScript was executed, no Emby Server was
contacted, and no test, build, or runtime verification was performed.

## Evidence and version boundaries

The pinned API baseline is Emby SDK **4.9.5.0**, revision
[`bdd0dd7c0801f6e069dff2795d80cddae6f91791`](https://github.com/MediaBrowser/Emby.SDK/commit/bdd0dd7c0801f6e069dff2795d80cddae6f91791).
Its [local snapshot](../sources/emby-sdk-openapi.snapshot.json) declares
`UserPolicy.AutoRemoteQuality` as an `int32` and `RestrictedFeatures` as an array of
strings. Neither property has a description, default, range, or enumeration.
The [official plugin API model](https://dev.emby.media/reference/pluginapi/MediaBrowser.Model.Users.UserPolicy.html)
likewise supplies types without behavioral descriptions.

The inspected [official Web application entry point](https://app.emby.media/index.html)
identifies version **26.0.25**. Its static source provides actual readers and
writers for both properties. It is current client evidence, not a capture of the
fixed 4.9.5 server implementation. The quality control explicitly requires server
version 4.9.2.9 or later, which includes the pinned baseline. The feature-list
client supports servers from 4.8.0.20. These version gates are compatibility
claims in the inspected client; they do not establish every server validation
rule.

## AutoRemoteQuality

**Confirmed meaning:** a per-user override of the automatically selected remote
streaming bitrate. It is a client-consumed playback preference, not evidence of an
unconditional server bitrate restriction.

The [official English strings](https://app.emby.media/strings/en-US.json) label the
control as an automatic remote streaming quality value in Mbps. Its help text
limits the effect to players using automatic quality and describes an override of
automatic quality detection across devices.

The [profile editor source](https://app.emby.media/users/profiletab.js) displays
the policy value divided by 1,000,000 and stores the entered decimal value
multiplied by 1,000,000, converted to an integer. Empty input becomes zero.
Consequently the API unit is **bits per second**, and **zero means no override**.
The [editor template](https://app.emby.media/users/profiletab.html) allows
0 through 200 Mbps in steps of 0.25 Mbps. This is an observed UI constraint; the
pinned API does not establish that it is the complete server-side numeric domain.

The actual consumer is `detectBitrateWithEndpointInfo` in the
[official API client](https://app.emby.media/modules/emby-apiclient/apiclient.js).
For a network type other than `lan`, a nonzero cached
`user.Policy.AutoRemoteQuality` is returned directly. Otherwise the client uses
its default quality selection. The automatic bitrate branch in
[the playback manager](https://app.emby.media/modules/common/playback/playbackmanager.js)
calls `apiClient.detectBitrate` and uses the result as the streaming bitrate
request. Manually selected player quality has a different branch.

**Implementation consequence:** persisting this integer and returning it in the
normal user-policy DTO implements the server's part of the observed contract.
The management UI may reproduce the documented Mbps conversion and input limits.
It must describe an automatic-quality preference rather than a hard limit.
`RemoteClientBitrateLimit` remains a separate server-side control. There is no
evidence supporting a boolean interpretation or an integer-to-quality enum.

Unknowns are server validation beyond the schema, behavior of malformed or
out-of-range stored values, and whether every Emby client consumes this field.

## RestrictedFeatures

**Confirmed meaning:** a deny list of dynamically registered user-feature IDs.
It is a feature permission mechanism, not merely an uninterpreted client setting.

The [official profile editor](https://app.emby.media/users/profiletab.js) calls
`getFeatures({FeatureType: "User"})`. Each returned `FeatureInfo.Id` becomes a
checkbox identity; a checkbox is checked when that identity is absent from
`UserPolicy.RestrictedFeatures`. Saving writes the identities of unchecked
dynamic checkboxes. Thus an empty list restricts none of the registered features.
The editor hides feature IDs containing a period. That presentation filter does
not prove such IDs are invalid on the server.

The editor also inserts Live TV and Live TV management controls with IDs
`livetv` and `livetv_manage`. Those controls use separate checkbox classes and
write their dedicated policy booleans. They are **not evidence** that these
strings belong in `RestrictedFeatures`.

The pinned SDK and [official FeatureService](https://dev.emby.media/reference/RestAPI/FeatureService/getFeatures.html)
define administrator-only `GET /Features`, returning `FeatureInfo` objects with
`Name`, `Id`, and `FeatureType`. `FeatureType` has `System` and `User` values.
The current official API client passes a feature-type query through this route;
the pinned REST operation does not enumerate that query parameter.

The [official feature factory interface](https://dev.emby.media/reference/pluginapi/Emby.Features.IFeatureFactory.html)
returns feature descriptions, and
[IFeatureManager](https://dev.emby.media/reference/pluginapi/Emby.Features.IFeatureManager.html)
provides both feature enumeration and `IsGrantedAccess(User, string featureId)`.
Together with the editor's direct deny-list manipulation, these interfaces support
a server-side feature registry and permission checks. They do not expose the
concrete implementation or an exhaustive installed ID list.

The similarly named
[SecurityExceptionFeature constants](https://dev.emby.media/reference/pluginapi/MediaBrowser.Controller.Net.SecurityExceptionFeature.html)
must not be treated as a `RestrictedFeatures` enumeration: the documentation does
not establish that mapping, and many correspond to separate policy booleans.
The inspected repository's existing 4.9.5 user-policy fixtures contain empty
restricted-feature lists; they do not establish any nonempty allowed ID.

## Proposed closed registry for Goby

This is an implementation proposal based on the dynamic protocol, **not a claim
about Emby's built-in feature IDs**:

1. Create a finite registry owned by Goby. Every entry has a stable ID, display
   name, `User` or `System` type, and concrete server operation consumers.
   Register only behavior already implemented by Goby. Avoid a generic editable
   string list with no corresponding enforcement.
2. Implement authenticated administrator `GET /emby/Features`, with optional
   `FeatureType` filtering supported by the observed client. Return only locally
   registered entries. A client learns valid IDs from this endpoint, so the SDK
   does not require inventing a universal Emby feature enumeration.
3. A small initial registry can represent **collection management** and
   **playlist management**, using explicitly Goby-owned IDs such as
   `goby_manage_collections` and `goby_manage_playlists`. These are proposed Goby
   identifiers. They contain no periods, so the inspected official editor can
   display them. Names must accurately describe which operations are restricted.
4. Apply a restricted management feature to all associated mutations: creation,
   membership changes, ordering where supported, and deletion. Reading or playing
   existing content remains governed by catalog visibility and playback policy.
   These checks are additional denials and never grant ownership, role, or other
   permissions. Revalidate the deny list in the mutation transaction.
5. Validate writes against the registry's user-feature IDs; reject unknown or
   system-only IDs explicitly. Build the management UI from the same registry and
   preserve checkbox inversion. Do not equate a dynamic ID to a separate boolean
   unless the server deliberately documents and implements that relationship.

A future compatibility capture may establish Emby-specific IDs and consumers.
Until then, Goby-owned registry identifiers should remain clearly documented.
No claim of live behavioral acceptance follows from this source research.
