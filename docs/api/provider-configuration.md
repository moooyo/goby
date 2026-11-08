# Administrator provider configuration

The native administrator interface manages the existing TMDB, MusicBrainz,
and OpenSubtitles clients. Deployment configuration determines whether online
providers are permitted. The managed internet-provider setting independently
controls their current use.

## Endpoints

All paths below are relative to /admin/v1. Every endpoint requires a current
administrator cookie. Writes and connection tests also require same-origin
requests and the current CSRF token.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | /providers/configuration | Returns the exact string Revision, deployment/current enablement, and provider field descriptors. |
| PUT | /providers/{providerId}/configuration | Applies a CAS update to the selected provider and publishes a new immutable credential snapshot. |
| POST | /providers/{providerId}/test | Tests the saved configuration at the supplied Revision. |

Supported provider IDs are tmdb, musicbrainz, and opensubtitles.
Field descriptors contain Id, Label, Secret, HasValue, and Source.
Original values are never returned, including identification and username
fields that may contain private operator information.

An update contains Revision and Fields. Omitting a field keeps it unchanged.
Each supplied field selects Mode replace, clear, or default. Replace supplies
Value; clear stores an explicitly empty override; default removes the override
and uses its deployment value. OpenSubtitles username and password remain a
pair. Saved credentials are encrypted with an authenticated, revision-bound
document under the existing protected application-key master.

## Connection tests

Tests use the saved snapshot and fixed official endpoints. The total deadline
is fifteen seconds, with no redirect following or automatic retry.
Tests do not reserve subtitle download quota or fetch a subtitle payload.
Responses classify passed, disabled, not_configured, authentication_failed,
quota, or unavailable, and include CheckedAt. They do not return upstream
response bodies, authorization values, or private credentials.

## Editing and recovery

New requests and newly started tasks read the saved configuration.
Existing in-flight requests retain the snapshot they already captured.
The interface clears replacement inputs after an acknowledged save and
requires explicit readback after an unknown result or revision conflict.

Backup validation authenticates the encrypted provider document in the same
snapshot as the database export. Missing master-key evidence cannot create a
replacement when encrypted configuration already exists. Historical archives
without this table retain their existing handling.
