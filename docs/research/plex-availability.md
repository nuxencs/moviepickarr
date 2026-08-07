# Research: Plex movie availability by TMDB ID

Researched 2026-08-07 against first-party Plex sources.

## Scope

Plex is deferred. It is not part of the current Radarr implementation. The
current implementation must use Radarr `hasFile` as its downloaded signal. This
note records a possible later Plex integration only.

## Direct match

Plex documents a single local-server endpoint for matching external content to
library content. For a movie, send metadata type `1`, the external TMDB GUID,
and the normal title and year hints:

```http
GET /library/matches?type=1&guid=tmdb%3A%2F%2F272&title=Batman%20Begins&year=2005&includeFullMetadata=1
Accept: application/json
X-Plex-Token: <token>
X-Plex-Client-Identifier: moviepickarr
X-Plex-Pms-Api-Version: 1.0.0
```

`guid` is the identity hint. `title` is documented as required when no file
path is supplied, and `year` reduces ambiguity. The endpoint can return more
than one result. Each result has a score from 0 to 100, and Plex treats scores
above 85 as positive. A positive result supplies a `ratingKey` and a metadata
`key`, such as `/library/metadata/667573`.
[Plex library match API](https://developer.plex.tv/pms/#tag/Library/operation/libraryGetMatches)

Plex documents `tmdb://105` as the external-ID form and lists `tmdb` as an
internally supported provider. External IDs are in the case-sensitive `Guid[]`
array. The top-level, lower-case `guid` is the item's Plex-compatible primary
identifier, often a `plex://movie/...` value, so it is not the TMDB check.
[Plex metadata response](https://developer.plex.tv/pms/#section/API-Info/Metadata-Response)

After a positive match, verify that `type == "movie"` and that a returned
`Guid[].id` equals `tmdb://<expected-id>`. Do not accept a title-and-year match
as exact when this external-ID check disagrees.

## File availability

`/library/matches` proves that Plex matched a library metadata item. It does
not by itself prove that Plex can still read the media file. Plex can retain an
item while its expected file is missing, moved, unmounted, or unreadable.
[Plex unavailable-content explanation](https://support.plex.tv/articles/201806463-why-does-plex-media-server-say-my-content-is-unavailable/)

Follow the returned metadata key and request a synchronous existence check:

```http
GET /library/metadata/{ratingKey}?checkFileAvailability=1&skipRefresh=1
```

Plex defines `checkFileAvailability=1` as a synchronous file-existence check.
The returned `Media[]` objects represent media instances, and their `Part[]`
objects represent playable files. Require at least one media part before
reporting the movie as available.
[Plex metadata item API](https://developer.plex.tv/pms/#tag/Content/operation/libraryMetadataGetSlash)

The published schema does not define one stable `available` result field for
the existence check. A later implementation must test missing, unmounted, and
permission-denied files against its minimum supported PMS version. If a strict
probe is needed, Plex documents the returned part `key` as the streaming path
and documents HTTP 404 as "the part doesn't exist". A normal GET starts a
stream, so it should not be the routine polling mechanism.
[Plex media-part API](https://developer.plex.tv/pms/#tag/Library/operation/libraryGetPartsPartChangestampFilename)

## Compatibility and fallback

- The match endpoint accepts a `guid` URI, but its own parameter description
  says that allowed schemes are still to be defined. The separate metadata
  contract documents `tmdb://...`. Treat direct TMDB matching as a capability
  to test when the integration is configured, not as an unconditional server
  guarantee.
  [Match API](https://developer.plex.tv/pms/#tag/Library/operation/libraryGetMatches),
  [external-ID contract](https://developer.plex.tv/pms/#section/API-Info/Metadata-Response)
- `Guid[]` is optional. The current Plex Movie agent supports TMDB IDs and was
  introduced with PMS 1.20, but Plex also supports legacy, personal, and NFO
  agents with different identifier behavior. An absent TMDB GUID is therefore
  `unknown`, not proof that the movie is absent.
  [Plex Movie agent](https://support.plex.tv/articles/advanced-settings-plex-movie-agent/),
  [TMDB-ID movie matching](https://support.plex.tv/articles/naming-and-organizing-your-movie-media-files/),
  [agent types](https://support.plex.tv/articles/200241558-agents/),
  [NFO identifier behavior](https://support.plex.tv/articles/using-nfo-metadata-files-with-plex/)
- Fallback order: retry `/library/matches` with title and year, verify every
  candidate through `Guid[]`, then, if needed, build a cached, paginated index
  from `/library/all?type=1` and fetch candidate metadata by its returned key.
  If no exact external ID is available, return `unknown` for admin review.
  [All-library API](https://developer.plex.tv/pms/#tag/Library/operation/libraryGetAll),
  [Plex pagination](https://developer.plex.tv/pms/#section/API-Info/Pagination)
- Use the published API contract header. API `1.0.0` requires PMS 1.41.9 or
  newer. PMS assumes legacy API `0.0` when the header is absent.
  [Plex API versioning](https://developer.plex.tv/pms/#section/API-Info/API-Versioning)

## Configuration and credentials

A future integration needs a PMS base URL, a Plex token that can read the
target movie libraries, and one stable opaque client identifier. Send the
token as `X-Plex-Token`, request JSON explicitly, and store the token as an
encrypted secret. Plex states that both the token and client identifier are
typically required. A TMDB API key is not needed for this PMS lookup.
[Plex headers and authentication](https://developer.plex.tv/pms/#section/API-Info/Headers)

The token-copy method in Plex Web produces a temporary token. Plex directs
long-lived third-party tools to its application authentication flow instead.
[Plex token guidance](https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/)
