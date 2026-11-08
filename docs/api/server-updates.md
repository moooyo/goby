# Server update observations

Goby can check an operator-selected Goby release feed without replacing the
running Docker image. There is no built-in public release source. An empty
`GOBY_UPDATE_FEED_URL` is an explicit unconfigured state, not evidence that the
running version is current. Plugin packages, image installation, automatic
application updates, and container-runtime control are outside this contract.

## Deployment source

Set `GOBY_UPDATE_FEED_URL` to the HTTPS JSON document for the trusted Goby image
profile and release stream selected by the deployment owner. A deployment
restart applies a changed source. The administrator API does not accept or
persist a source URL.

```text
GOBY_UPDATE_FEED_URL=https://releases.example.com/goby/software/stable.json
```

The address above is a placeholder, not a published Goby service. The
[example feed](../examples/goby-update-feed.json) is also illustrative; replace
its URLs and artifact identity with an actual release before configuring it.
The current offline image-archive catalog is not automatically an online feed.

A source URL must have a DNS hostname, use HTTPS on port 443, and contain no
credentials, query, or fragment. Literal and legacy numeric IP forms are
rejected. Production checks ignore environment HTTP proxies, require normal TLS
certificate verification and TLS 1.2 or newer, resolve the configured hostname,
reject the complete answer if any address is private, local, reserved, or
otherwise non-public, and dial only the checked literal addresses. Foreign
hosts and ports cannot enter that dialer. Redirects are not followed.

The complete fetch has a ten-second budget, response headers are bounded to
16 KiB, and the uncompressed UTF-8 JSON document is limited to 64 KiB. Only an
HTTP 200 `application/json` response with an optional UTF-8 charset is accepted.
Credentials from administrator or Emby requests are never sent to the source.
Release details and artifact URLs are validated as HTTPS information but are
never fetched or installed by the update checker.

## Feed format

The exact, case-sensitive version-1 JSON shape is:

```json
{
  "schemaVersion": 1,
  "product": "goby",
  "release": {
    "version": "0.2.0",
    "classification": "Release",
    "publishedAt": "2026-11-06T08:00:00Z",
    "description": "Release notes as plain text.",
    "infoUrl": "https://releases.example.com/goby/0.2.0",
    "sourceUrl": "https://releases.example.com/goby-0.2.0-linux-amd64-image.tar",
    "sha256": "0000000000000000000000000000000000000000000000000000000000000000",
    "targetFilename": "goby-0.2.0-linux-amd64-image.tar"
  }
}
```

All fields are required; unknown fields, duplicate names, case aliases, nulls,
trailing JSON documents, invalid UTF-8, and unpaired escaped UTF-16 code units
are rejected. `description` may be empty, is bounded to 4096 UTF-8 bytes, and is
rendered as plain text. The publication timestamp must parse as RFC 3339 and
remain within years 1 through 9999 after UTC normalization. URLs have the same
syntax restrictions as the feed URL. `sha256` is exactly 64 lowercase hexadecimal
characters. `targetFilename` is an ASCII basename of at most 128 characters,
starts with a letter or digit, and otherwise contains only letters, digits,
periods, underscores, and hyphens.

`classification` is the publisher's actual `Release`, `Beta`, or `Dev` package
classification. It does not expose a server-side release-channel mutation.
Choose a different deployment-controlled source when selecting another stream.
The checker does not assume that a stream exists simply because the enum does.

`version` and the running Goby product version use complete Semantic Versioning
precedence: three numeric components, optional prerelease identifiers, and
optional build metadata. One leading `v` is accepted. Build metadata does not
affect precedence; numeric identifiers are compared without integer overflow.
The Emby compatibility baseline in `SystemInfo.Version` never participates in
this comparison. A development build with an incomparable version is reported
as unknown even when the feed was fetched successfully.

An artifact checksum is publisher-provided release information. It is not a
claim that Goby downloaded, verified, or applied that image. Deployment tooling
retains responsibility for authenticating and deploying the selected artifact.

## Native administrator API

- `GET /admin/v1/server-updates` reads the cached observation without network I/O.
- `POST /admin/v1/server-updates/check` accepts exactly an empty JSON object,
  requires the administrator cookie, same-origin policy, and CSRF header, and
  performs a release observation.

Both operations return non-cacheable JSON. They require a fresh current
administrator check. Network operations check authority before I/O and again
before publishing a result, without holding database authority locks across the
remote request. A revoked or expired actor cannot publish the result. A canceled
request does not overwrite the cached observation. Concurrent checks return
HTTP 409 `update_check_busy` instead of starting another network request.

```text
CanSelfUpdate: false
Delivery: "docker"
ConfiguredSource: boolean
CurrentVersion: string
State: unconfigured | not_checked | failed | current | available | version_unknown
CheckedAt: RFC3339 UTC timestamp | null
Release: release information | null
ErrorCode: "update_check_failed" | "current_version_unknown" | null
```

Native release fields are `Version`, `Classification`, `PublishedAt`,
`Description`, `InfoUrl`, `SourceUrl`, `SHA256`, and `TargetFilename`.

| State | Meaning |
| --- | --- |
| `unconfigured` | No deployment release source; no check timestamp or release. |
| `not_checked` | Source configured; this process has not completed a check. |
| `failed` | The attempted observation failed; timestamp present, release absent. |
| `current` | A successful comparison found no offered version newer than the running product version. |
| `available` | A successful comparison found a newer offered product version. |
| `version_unknown` | Feed fetched successfully, but the running product version cannot be compared. |

The native POST returns HTTP 200 for these complete status observations,
including `failed` and `unconfigured`. Authentication, authorization,
malformed-input, and concurrent-operation failures remain HTTP errors. Failed
checks remove a previous successful release assertion. The cache is process
local; restarting returns it to `unconfigured` or `not_checked`.

The general policy response retains the four-field `Updates` summary with a
dynamic `ConfiguredSource`. The update panel reads the separate full status,
shows the last attempt and actual publisher classification, and offers a check
and a cached-status reload. It has no editable source, install, automatic-update,
or synthetic channel selector.

## Emby core compatibility API

`GET /emby/Packages/Updates?PackageType=System` requires current Emby administrator
authority and performs the same bounded source observation. The namespace also
accepts the existing unprefixed and route-literal case aliases. Transport
authentication and supported language parameters retain their normal handling.
`PackageType` is required exactly once; unknown business parameters are rejected.

After a successful `current` comparison it returns `[]`. An `available` result
returns one `PackageVersionInfo` object with:

- `name="Goby Server"`, `versionStr`, and the actual `classification`;
- `description`, `sourceUrl`, `checksum`, `targetFilename`, `infoUrl`, and
  `timestamp` from the validated Goby feed.

An unconfigured source returns HTTP 503 `update_source_unconfigured`; a failed
check returns HTTP 503 `update_check_failed`; an incomparable running build
returns HTTP 409 `current_version_unknown`. None of these becomes an empty
successful update list. `PackageType=UserInstalled` returns HTTP 501
`plugin_updates_unsupported`; plugin distribution is excluded.

The administrator `SystemInfo` projection includes `HasUpdateAvailable` only
after a successful `current` or `available` comparison. Unconfigured, unchecked,
failed, and unknown-version states omit it. `CanSelfUpdate` remains false.

The upstream core contract is documented in
[getPackagesUpdates](https://dev.emby.media/reference/RestAPI/PackageService/getPackagesUpdates.html)
and [SystemInfo](https://dev.emby.media/reference/RestAPI/SystemService/getSystemInfo.html).
The official [Emby Docker image instructions](https://hub.docker.com/r/emby/embyserver)
upgrade the server through image replacement and do not establish an in-process
Docker image installer.

## Verification scope

Focused unit and HTTP cases cover semantic precedence, strict feed parsing,
private and mixed DNS answers, checked-literal dialing, normal TLS through an
explicit fixture transport, redirect refusal, size and format budgets,
unconfigured and failed observations, current and available compatibility
mapping, product-version separation, cancellation, concurrent checks, and
credential revocation while the feed request is pending. Browser cases cover
all six states, CSRF, outcome reload, duplicate-click prevention, timeout,
unconfirmed results, bilingual display, and narrow-screen rendering. Execution
receipts belong to the task's verification evidence.
