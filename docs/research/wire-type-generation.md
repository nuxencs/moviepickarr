# Research: wire type generation for Go and Fiber

Resolves [#393](https://github.com/nuxencs/moviepickarr/issues/393), child of
map [#392](https://github.com/nuxencs/moviepickarr/issues/392). This file is on
a throwaway branch and is never merged.

Constraints from the map: Fiber v2 stays, wire types are generated from Go,
behavior stays the same, no API versioning (client and server ship in one
binary), and type generation comes first so the frontend and backend tracks can
run independently.

## Recommendation

**Generate TypeScript from exported Go DTOs with
[guts](https://github.com/coder/guts), through a small `cmd/apitypings` command.
Commit the output. Fail CI on a diff. Change no handler.**

1. Move the DTOs to a new `internal/wire` package with exported names. Use the
   current TypeScript names in Go (`MovieTile`, `MovieDetail`), so `web/` changes
   only an import path.
2. Give every wire value a name: inline request bodies, `fiber.Map` payloads, SSE
   payloads, bare-string enums.
3. `go run ./cmd/apitypings > web/src/types/wire.gen.ts` replaces
   `Response.ts`, `SSEEvent.ts`, and the type blocks in `radarr.ts` and
   `integrations.ts`.
4. The Go CI job runs the command and then `git diff --exit-code`. The Web job
   already runs `tsc -b` through `bun run build`, which catches broken consumers.

Cost does not scale with routes. It scales with about 75 types. Estimate: about
one week (see [Cost](#cost)). No handler rewrite and no behavior change, so both
tracks can start after it lands.

**What this does not do.** It stops field drift, not pairing drift. It generates
types only. Which route returns which type stays hand-written in
`APIClient.ts`, and so do query parameters and status codes. The map says client
and server "cannot drift". This recommendation meets that for shapes, not for
route pairing. [What stays hand-written](#what-stays-hand-written) names the
later step if pairing must also close.

**Confidence.** No third-party tool ran in this research: the permission system
refused to build and run tygo as external code, and that decision applies equally
to the others. Tool claims come from source and golden fixtures at the commits
linked below. Two checks did run, with the standard library only (see
[json/v2](#encodingjsonv2-changes-the-comparison)). The repository counts are from
this repository. The first adoption step is therefore a spike
([Open items](#open-items)).

## Comparison

| Criterion | guts | tygo | Huma | swag | oapi-codegen |
| --- | --- | --- | --- | --- | --- |
| Source of truth | Go types | Go types | Go handlers | Go comments | YAML |
| Fiber v2 | Not involved | Not involved | `humafiber.NewV2` | UI through `gofiber/swagger` | `fiber-server`, strict |
| Per-route work | None | None | Rewrite each handler | Comment block on each handler | Hand spec for each route |
| Stale annotation | Not possible, no annotation | Tags can be missing, output stays valid | Not possible for success bodies | Silent | Compile error, but the mirror moves to YAML |
| Enums | Const groups in generated packages | Const groups in generated packages | No const access, tag per field | Not checked | Spec |
| `*T` not omitted | `T \| null` | `T?`, wrong without a tag | `T \| null` | Not checked | Spec |
| Lean and full DTOs | `extends`, no tag | Tag on each embed | Flattened, exported embeds only | Annotations | `allOf` |
| Error bodies | Types only | Types only | Per operation | Annotations | Per operation |
| SSE payloads | Named types, union by mutation | Named types, union by hand | `sse` changes the frame format | No | `io.Reader` |
| CI drift check | Regenerate and diff | Regenerate and diff | Regenerate spec and diff | Regenerate and diff, cannot see handler lies | Compile |
| Output | Types | Types | OpenAPI, then a TS tool | Swagger or OpenAPI, then a TS tool | Go, then a TS tool |
| Typed client | No | No | Yes, through OpenAPI tools | Yes, through OpenAPI tools | Yes, through OpenAPI tools |
| Latest release | v1.7.1, 2026-06-03 | v0.2.21, 2026-02-03 | v2.40.0, 2026-10-27 | v2.0.0-rc6, 2026-09-13 | v2.8.1, 2026-10-24 |

## What the API carries today

Counts are from `develop` at `7fab684`, non-test files in `internal/server`.

- **Routes:** 90 handler registrations plus 4 SSO-off stubs in `server.go`. The
  map's 91 stays the working number. 5 are not JSON: `/events` and the 4 OIDC
  redirects.
- **DTOs:** 81 tagged structs, all unexported. 7 are upstream TMDB client structs,
  so about 75 wire types including the generic `settingResponse[T]`.
- **Untyped payloads:** 12 `fiber.Map` sites, 2 `map[string]any` sites, 20 inline
  request structs, 17 query reads, 11 path reads.
- **SSE:** `event.Data` is `any` (`events.go:14`). There are 16 event types. 7 have
  untyped payloads: `user:role-changed`, `user:deleted`, `movie:deleted`,
  `movie:moved`, `movie:revealed`, `wildcard:canceled`,
  `settings:next-up-changed`.
- **Errors:** 4 body shapes. `problemDetails` (`errors.go:13`),
  `integrationProblemDetails`, `radarrProblemResponse`, and the login failure
  `{"error": ...}` (`auth_handlers.go:18`).
- **Enums:** 23 exported TypeScript aliases, mostly string unions. Only 6 DTO
  fields use a typed Go enum (`domain.MovieStatus`, `domain.Role`,
  `domain.RemoveOutcome`, `repository.RadarrRemoveOutcome`). The rest are bare
  `string` in Go, for example `claimResponse.Mode`.
- **Embeds:** 6 sites. `libraryMovie`, `fullMovie`, `drawnPayload`,
  `createMemberResponse`, and the 2 problem types.
- **Pointers without omit:** 16 sites. 2 are responses (`username`,
  `initiatedBy`) and must stay `T | null`. 14 are request fields that mean
  "optional".
- **Client:** no runtime validation library. Hand types are in 4 files.

## Drift that exists now

A text comparison of Go tags and TypeScript interfaces found the candidates. Only
the items below were then confirmed by reading both sides.

| Item | Go | TypeScript | Effect |
| --- | --- | --- | --- |
| `settings:next-up-changed` | `{id, name}` (`settings_handlers.go:100`, `handler_base.go:153`) | `SSEEvent<Settings>` (`SSEEvent.ts:102`) | Wrong type. No failure today, because the consumer only invalidates. |
| `user:role-changed` | `{userID, role}` | In the union, no payload interface | Untyped |
| `watchedAt` | `string`, always sent (`models.go:40`) | `watchedAt?: string` (`Response.ts:26`) | `""`, never absent |
| `candidates` | `[]leanMovieTile`, no omit (`movies_handlers.go:348`) | `candidates?`, comment says the server may omit it (`Response.ts:79`) | Stale. A nil slice encodes as `[]`. |
| `RadarrTag.name` | Absent (`radarr_handlers.go:37`) | `name?` (`radarr.ts:55`) | Field does not exist |
| `RadarrIdentityResult.overview` | Absent (`radarr_handlers.go:184`) | `overview?` (`radarr.ts:229`) | Field does not exist |
| `IntegrationSummary.attentionCount` | `*int`, `omitzero` (`integration_handlers.go:21`) | Absent (`integrations.ts:10`) | Field not visible to the client |
| Radarr attention | `{count}` (`radarr_handlers.go:20`) | Accepts `number`, `count`, `attentionCount`, `unresolved` (`radarr.ts:326`) | Client guesses at the shape |

The text comparison also flags about 35 response fields that TypeScript marks
optional and Go always sends. A sample was confirmed, not all. Generated types
will make those fields required, which is correct and can remove `??` fallbacks.

## encoding/json/v2 changes the comparison

`server.go:191` sets `encoding/json/v2` as the Fiber encoder. It compiles here on
Go 1.27.3 with no `GOEXPERIMENT` setting. A standard-library test gave these
results for a zero value:

```text
{"movieID":0,"watchedAt":"","genres":[],"pool":{},"name":null,"status":"pool"}
```

So a nil slice encodes as `[]`, a nil map as `{}`, and a nil pointer as `null`.
Generated types must say `T[]`, not `T[] | null`. The same test confirmed that
`reflect` reports the embedded `leanTile` field as `exported=false`, while the
encoder still flattens it.

## Options

### guts (recommended)

Library, not a CLI. Uses full type information
([convert.go:59](https://github.com/coder/guts/blob/415b080100f683a136544ce712ab7171e6f07466/convert.go#L59)),
so it reads Go the way the compiler does. `coder/coder` generates its API types
with it
([scripts/apitypings/main.go](https://github.com/coder/coder/blob/main/scripts/apitypings/main.go)),
which is the reference wiring for `cmd/apitypings`.

- **Embeds:** an embed with no JSON tag becomes `extends`, with no annotation
  ([convert.go:827](https://github.com/coder/guts/blob/415b080100f683a136544ce712ab7171e6f07466/convert.go#L827),
  [fixture](https://github.com/coder/guts/blob/415b080100f683a136544ce712ab7171e6f07466/testdata/inheritance/inheritance.ts)).
  This fits `MovieTile`, `MovieDetail`, and `MovieDrawPayload` directly.
- **Optional and nullable:** `omitempty` and `omitzero` give `?`
  ([convert.go:921](https://github.com/coder/guts/blob/415b080100f683a136544ce712ab7171e6f07466/convert.go#L921)).
  `*T` gives `T | null`
  ([convert.go:1165](https://github.com/coder/guts/blob/415b080100f683a136544ce712ab7171e6f07466/convert.go#L1165)).
  `SimplifyOptional` removes `null` from `?` fields
  ([mutations.go:21](https://github.com/coder/guts/blob/415b080100f683a136544ce712ab7171e6f07466/config/mutations.go#L21)).
  The 2 response pointers are then correct with no tag.
- **Request pointers:** the 14 request fields would come out as required
  `T | null`. Add `omitzero` to them. The option acts only when marshaling
  (`encoding/json/v2/doc.go:72`), so decoding does not change.
- **Enums:** `EnumAsTypes` turns a const group into a string union
  ([fixture](https://github.com/coder/guts/blob/415b080100f683a136544ce712ab7171e6f07466/testdata/enumtypes/enumtypes.ts)).
- **Maps:** apply `NotNullMaps`, which matches `{}` under json/v2.
- **`readonly` is opt-in:** it is the `ReadOnly` mutation
  ([mutations.go:71](https://github.com/coder/guts/blob/415b080100f683a136544ce712ab7171e6f07466/config/mutations.go#L71)).
  Leave it off so types stay drop-in. The frontend can turn it on later.
- **Failure mode is visible:** an external struct gives `unknown` and a comment
  ([convert.go:1155](https://github.com/coder/guts/blob/415b080100f683a136544ce712ab7171e6f07466/convert.go#L1155)),
  and an anonymous nested struct gives `unknown` too
  ([convert.go:1034](https://github.com/coder/guts/blob/415b080100f683a136544ce712ab7171e6f07466/convert.go#L1034)).
  `claimResponse.Options` must get a name. Consumers fail in `tsc`, not at run
  time.
- **Cost of the choice:** about 30 lines of generator, and guts pulls in goja and
  an embedded TypeScript compiler. Only `cmd/apitypings` imports it, so the
  release binary does not grow. It skips unexported types
  ([convert.go:362](https://github.com/coder/guts/blob/415b080100f683a136544ce712ab7171e6f07466/convert.go#L362)),
  so exporting is required.

### tygo

Viable and the simplest to install, but it reads syntax only
([generator.go:41](https://github.com/gzuidhof/tygo/blob/0b55b7f6509c82f14206cc704eb4a230ece9efe4/tygo/generator.go#L41))
and pins `x/tools` v0.1.9, against v0.40.0 in guts. Each gap below is a per-field
chore that guts solves from type information, and each one fails silently with
valid TypeScript:

- A cross-package type not in `type_mappings` becomes `any /* domain.MovieStatus */`
  ([write.go:97](https://github.com/gzuidhof/tygo/blob/0b55b7f6509c82f14206cc704eb4a230ece9efe4/tygo/write.go#L97)).
  It also needs a frontmatter import and a generated definition.
- Every `*T` becomes optional
  ([write.go:376](https://github.com/gzuidhof/tygo/blob/0b55b7f6509c82f14206cc704eb4a230ece9efe4/tygo/write.go#L376)).
  Correct for the 14 request fields, wrong for the 2 response fields.
  `optional_type: null` is global, so it breaks `omitempty` fields.
- An embed without `tstype:",extends"` becomes a nested property named after the
  type, and an unexported embed disappears
  ([write_toplevel.go:655](https://github.com/gzuidhof/tygo/blob/0b55b7f6509c82f14206cc704eb4a230ece9efe4/tygo/write_toplevel.go#L655)).
  A future DTO author must remember the tag, which is the hand-mirror problem in
  a smaller form.

It also skips unexported types
([write_toplevel.go:360](https://github.com/gzuidhof/tygo/blob/0b55b7f6509c82f14206cc704eb4a230ece9efe4/tygo/write_toplevel.go#L360)).
It is the fallback if guts fails the spike.

### Huma

The only route-level option that needs no hand spec, and Fiber v2 is supported:
`humafiber.NewV2` and `NewV2WithGroup`
([humafiber_v2.go:286](https://github.com/danielgtaylor/huma/blob/606bcc2789bcf2efcc64a74dec94843804e662da/adapters/humafiber/humafiber_v2.go#L286)).
Compatibility is not the blocker. Rewriting about 85 handlers and changing
behavior are, and it cannot come first, because the backend track owns those
handlers. Each handler becomes `func(ctx, *I) (*O, error)`.

Adoption checklist if it is chosen later:

- `DefaultArrayNullable` is true
  ([schema.go:31](https://github.com/danielgtaylor/huma/blob/606bcc2789bcf2efcc64a74dec94843804e662da/schema.go#L31)).
  Set it false, or every slice is typed `T[] | null`.
- `DefaultJSONFormat` uses v1 `encoding/json`
  ([defaults.go:16](https://github.com/danielgtaylor/huma/blob/606bcc2789bcf2efcc64a74dec94843804e662da/defaults.go#L16)).
  Replace it through `Config.Formats`, or wire bytes change.
- `DefaultConfig` adds `$schema` to responses
  ([defaults.go:85](https://github.com/danielgtaylor/huma/blob/606bcc2789bcf2efcc64a74dec94843804e662da/defaults.go#L85)).
  Turn it off.
- A field with no omit option is required
  ([schema.go:1093](https://github.com/danielgtaylor/huma/blob/606bcc2789bcf2efcc64a74dec94843804e662da/schema.go#L1093)).
  Requests then fail on the 14 optional pointers until they are tagged.
- Schema failures return 422
  ([huma.go:2186](https://github.com/danielgtaylor/huma/blob/606bcc2789bcf2efcc64a74dec94843804e662da/huma.go#L2186)).
  `WriteErr` reads the status back from the replaceable `NewErrorWithContext`
  ([error.go:270](https://github.com/danielgtaylor/huma/blob/606bcc2789bcf2efcc64a74dec94843804e662da/error.go#L270)),
  so status and body can be remapped globally. The validation still runs before
  the handler, with Huma wording in `detail`, and components toast that text.
- `getFields` skips an unexported embed before it checks `Anonymous`
  ([schema.go:894](https://github.com/danielgtaylor/huma/blob/606bcc2789bcf2efcc64a74dec94843804e662da/schema.go#L894)).
  `fullMovie` would lose every tile field, so DTOs must be exported here too.
- Reflection cannot see const groups, so each enum needs `enum` tags or a
  `SchemaTransformer`
  ([schema.go:932](https://github.com/danielgtaylor/huma/blob/606bcc2789bcf2efcc64a74dec94843804e662da/schema.go#L932)).
- `/movies/current` and `/movies/wildcard` return JSON `null`. Check how a nil
  body is written before this is relied on. This was not traced.
- `huma/sse` maps each Go type to an `event:` name
  ([sse.go:91](https://github.com/danielgtaylor/huma/blob/606bcc2789bcf2efcc64a74dec94843804e662da/sse/sse.go#L91)).
  This app sends `event: message` with `{seq, type, data}`, a `connected` frame,
  and a `heartbeat` frame, with session recheck in the stream. `/events` stays
  plain Fiber.

### swag

Ruled out. Annotations are comments, and nothing compares `@Success` with what
`c.JSON` sends. On `master` it generates Swagger 2.0
([README](https://github.com/swaggo/swag/blob/master/README.md#how-to-use-it-with-gin)).
OpenAPI 3.1 is only in the v2 release candidates
([README at rc6](https://github.com/swaggo/swag/blob/v2.0.0-rc6/README.md#open-api-v310)).
`master` has no `omitzero` handling. It would add 90 comment blocks that can go
stale with no signal.

### oapi-codegen

Ruled out. Fiber v2 and strict server exist at v2.8.1
([docs](https://github.com/oapi-codegen/oapi-codegen/blob/15f571472ff8ad02665635d9254ada6c1f32c214/docs/fiber-server.md)),
and the strict interface would be a real compile-time check. But the spec is
written by hand for 90 routes, which moves the mirror from TypeScript to YAML and
conflicts with the map's "generated from Go". Go DTOs would also become generated
code, so every `toLeanTile`-style mapper changes.

## How it fits this codebase

### TanStack Query

Nothing changes in `queries.ts`, `query_keys.tsx`, or the `queryOptions`
factories. `APIClient.ts` keeps its body and changes one import.

guts generates no client. An OpenAPI path would reach generated clients, but the
keys matter: `openapi-react-query` uses `[method, path, init]`
([index.ts:197](https://github.com/openapi-ts/openapi-typescript/blob/d46a319ae2efab30e135ba9b223a3cffd1a67eab/packages/openapi-react-query/src/index.ts#L197)),
and this app has hierarchical factories that `sseInvalidations.ts` and
`poolStateCache.ts` depend on. If OpenAPI arrives later, use `openapi-fetch` inside
the existing `queryOptions`, not generated hooks. Hey API and Orval also generate
TanStack Query code (README only, not inspected).

### SSE

Keep the wire format. Add a Go table in `internal/wire` that pairs each event
constant with its payload struct, and make the broadcaster generic, so a site
cannot send the wrong payload. `cmd/apitypings` reads the table and builds a union
that discriminates on `type`. `DiscriminatedChatMessagePart` in Coder's generator
is the precedent for building a union with `SetNode` and `ReplaceNode`.

### Errors

guts emits the problem types, and `title` values can become a const group
(`ProblemCode`). It does not type which route returns which error.
`APIClient.ts:129` discards the body on 400, 404, and 500, so `ApiError.code` is
empty for those statuses today. That is separate from this work.

### CI

- Add one step to the `go` job in `.github/workflows/ci.yml`: run the command, then
  `git diff --exit-code web/src/types/wire.gen.ts`. No new toolchain, and no
  `web/dist` placeholder, because `internal/wire` does not embed it.
- A `make generate` target for local use.
- Add the generated file to ESLint ignores.
- `routes_contract_test.go` stays the route guard.

## What stays hand-written

- Route to type pairing: `appClient.Get<MovieTile[]>("api/v1/movies/pool")`.
- Query parameters: `StatsQuery` in `APIClient.ts`.
- Status codes and per-route errors.

If pairing must also close, there are two later steps. Both reuse all the work
above:

1. **Route contract table.** A Go table of method, path, request type, and response
   type, checked against `app.GetRoutes()` as `routes_contract_test.go` already
   does, and emitted as a TypeScript map. No handler rewrite. A handler could still
   send another type than its row says, unless tests check responses.
2. **Huma handlers** during the backend track, with the checklist above. Pairing is
   then true by construction.

The route review ticket should choose. Neither blocks the type work.

## Cost

Estimates, for one engineer. Nothing here scales with 91 routes.

| Work | Size | Estimate |
| --- | --- | --- |
| Spike: run guts on real DTOs (see below) | 2 to 4 types | 2 hours |
| Create `internal/wire`, move and rename DTOs, keep mappers in `internal/server` | About 75 types | 1 day |
| Name inline bodies and `fiber.Map` payloads | 20 + 14 sites | 1 day |
| Type the SSE table and generic broadcaster | 16 events | 0.5 day |
| Type bare-string enums | About 15 aliases | 0.5 day |
| `cmd/apitypings`, `make generate`, CI step, ESLint ignore | 1 command | 0.5 day |
| Swap web imports, delete hand types, fix what `tsc` finds | 4 files plus consumers | 1 day |
| **Total** | | **About 5 days** |

For comparison, by the same counts: Huma adds about 85 handler rewrites, 72 `.JSON`
sites, and 184 `writeError` and `writeProblem` sites to review. oapi-codegen adds a
hand spec for 85 operations and about 75 schemas.

## Open items

Run these first. They are why the spike comes before the move.

1. **Run guts on real DTOs.** Confirm it loads this module on Go 1.27.3 with
   `x/tools` v0.40.0 or newer. Confirm `MovieDrawPayload` and `settingResponse[T]`.
2. **Enums in referenced packages.** `IncludeReference` skips objects that are not
   referenced
   ([convert.go:371](https://github.com/coder/guts/blob/415b080100f683a136544ce712ab7171e6f07466/convert.go#L371)),
   and a field references the type, not its constants. So `domain.MovieStatus` may
   come out as `string` with a comment, not a union. If so, move the enum constants
   to a small leaf package included with `IncludeGenerate`, and alias them from
   `domain`.
3. **Fallback.** If guts fails item 1, use tygo with `fallback_type: unknown`, and
   add the tags listed above. All of the `internal/wire` work carries over.
