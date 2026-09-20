# Catalog field projection and client transport hints

The media-analysis/resilience increment extends the existing catalog adapters.
Its execution record, rather than this contract, determines which checks have
actually run. These options never change catalog membership or grant access.

## Field selection

Item, music/entity and discovery responses accept case-insensitive `Fields` and
`ExcludeFields` query names and case-insensitive field names. The same rule
applies to the existing `EnableImages`, `EnableUserData`, `ImageTypeLimit` and
`EnableImageTypes` projection options. Other business-query identifiers retain
their own existing contracts; this is not a global case-insensitive query rewrite.

`Fields` retains list/detail behavior: list responses add the requested supported
optional fields, while details normally include their applicable metadata.
`ExcludeFields` wins over both explicit selection and detail defaults. It removes
matching optional response keys. Excluding `MediaStreams`, `Chapters` or `Path`
also removes that fact from each retained `MediaSources` entry. Excluding
`MediaSources` removes the collection itself. Subtitle credential projection and
indexed image postprocessing cannot restore excluded facts.

The stable identity/classification keys `Id`, `Name`, `Type`, `IsFolder`,
`ServerId` and `MediaType` remain available. Unknown field names retain the
existing forward-compatible hint behavior and do not imply implementation of
those fields. Field exclusion does not remove source data, change negotiated
playback sources or authorize media access.

Comma-separated field lists allow up to 128 entries each, with at most 8 KiB of
decoded projection values in total. Names contain ASCII letters, digits or
underscores and are at most 64 bytes. Empty lists are valid. Controls, malformed
encoding and competing case aliases return `400 invalid_item_projection` after
authentication. Repeated lists with identical parameter spelling are combined;
Boolean and limit options require one value. Field lists remain independent of
filter counts, ordering and pagination.

The original-client lowercase `fields` requests are retained in
`docs/development/m3e-reference-movie-client.json`. The explicit
`ExcludeFields=MediaStreams` request is retained in
`docs/development/m3e-reference-music-navigation-contract.json`; that artifact's
diagnostic status does not constitute a new passing acceptance result.

## UI language hint

The shared compatibility query parser accepts one case-insensitive
`X-Emby-Language` query parameter. It is a UI hint, not a user selector, metadata
language override, authentication carrier or locale-specific sorting rule. The
value is valid UTF-8, at most 256 bytes, with no controls or surrounding
whitespace. Empty values are admitted. Duplicate spellings/case aliases fail.
Unknown `X-Emby-*` names remain endpoint input and receive that endpoint's
validation; `ReqFormat` keeps its separate endpoint-specific contract.

The bounded language hint and ordinary field projection do not cause the
unpaged episode playback queue to fall back to a truncated browse page.
Explicit paging and other business selectors retain their existing behavior.
Native administrator APIs do not gain a general compatibility-query exception.
