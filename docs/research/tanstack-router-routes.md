# Research: TanStack Router file-based vs code-based routes

Resolves [#395](https://github.com/nuxencs/moviepickarr/issues/395), child of
the map [#392](https://github.com/nuxencs/moviepickarr/issues/392). Standing
decision from the map: the rebuild is in place in `web/`, route by route, on one
integration branch. Behavior stays the same.

**Versions.** `web/bun.lock` pins `@tanstack/react-router` 1.170.15,
`@tanstack/router-core` 1.171.13, `@tanstack/react-query` 5.85.5, Vite 8.3.5,
Vitest 5.0.5, TypeScript 5.9.3. npm latest on 2026-10-31: `react-router` 1.170.46,
`router-plugin` 1.168.48.

**Sources.** Router docs and source links point to the commit of tag
`@tanstack/react-router@1.170.15` ([`10a7ff8`][pin]), the version this app runs.
The live site (latest) gives the same recommendation.

**Evidence labels.** "Docs" and "source" are upstream claims. "Spike" is a
measurement on a throwaway copy of `web/` at `develop` `7fab684`, with
`@tanstack/router-plugin` 1.168.18 (the plugin version at the pinned tag). The
spike is not on this branch.

## Recommendation

**Use file-based routes with `autoCodeSplitting: true`. Move the route tree in
one mechanical PR, before the first route rebuild.** Then rebuild route by
route: each rebuild changes the `component` import in one route file.

The facts that drive this:

1. **Upstream default.** "Code-based routing is not recommended for most
   applications" ([code-based-routing.md][code]). File-based is "the preferred
   and recommended way", and "most of the documentation is written from the
   perspective of file-based routing" ([route-trees.md][trees]).
2. **The cost is small and measured.** The spike moved the whole route tree with
   no edit to a page, a consumer, or the test harness. `tsc` passes, 786 of 786
   tests pass, the production build works on Vite 8, and JS output is within 0.3%.
   See [Spike results](#spike-results).
3. **The rebuild adds route-level config.** Each rebuilt route can get a loader,
   a pending state, and an error state. One file per route gives that config a
   location. `router.tsx` would grow with each route. Upstream says that for a
   single file, this "no longer becomes feasible" ([decisions-on-dx.md][dx]).

**Limits of this recommendation.** This is a preference, not a forced move:

- "Both methods support the exact same core features" ([route-trees.md][trees]).
  Code-based is fully supported and has zero cutover cost.
- The upstream boilerplate argument is sized for "~40-50 routes" and "700+ lines"
  ([decisions-on-dx.md][dx]). This app has 20 routes and 269 lines. In the spike,
  hand-written route code falls from 269 lines to 214, and a 472-line generated
  file comes in. The boilerplate gain alone does not justify the move.
- Bundle size does not change. The app already splits every route but Movies.

## Spike results

| Check | Code-based (today) | File-based (spike) |
| --- | --- | --- |
| Route definition files | 1 (`router.tsx`, 269 lines) | 20 in `src/routes` (189 lines) plus `router.tsx` (25 lines) |
| Generated code | None | `src/routeTree.gen.ts`, 472 lines |
| Route ids | 21 with root | 20 with root, all others equal (see below) |
| `tsc -b` | Pass | Pass, no consumer edit |
| `bun run test` | 786 pass | 786 pass, plus 1 temporary id-dump probe. `src/test/providers.tsx` not edited |
| `bun run build` on Vite 8.3.5 | Pass | Pass, with the generated file present |
| JS output | 36 files, 735,127 bytes, 226,542 gzip | 37 files, 736,720 bytes, 227,256 gzip |
| CSS output | 10 files, 157,012 bytes | 10 files, 157,012 bytes |
| Entry chunk | 170.3 kB | 171.6 kB with Movies eager, 101.5 kB with Movies split |
| `eslint` on route files | n/a | 1 warning, for a local component in a route file |

- **The one id difference:** `/_app/admin/integrations` has no component in
  `router.tsx`. The spike left out that file, so the id is gone. Nothing consumes
  it. Add `_app/admin/integrations/route.tsx` for exact parity.
- **Generator output is deterministic:** a regeneration from nothing gave the
  same SHA-1 hash as a rebuild.
- **Dev server works:** the watcher adds new files to the tree and scaffolds an
  empty route file. `?tsr-split=component` modules resolve.
- **Not tested:** Playwright specs, guards in a real browser, intent preload in
  a real browser, and HMR. Run them at cutover.

## Dimension by dimension

### Code splitting: differentiates, but not in output

- **Today:** 16 `lazyRouteComponent` calls in `web/src/router.tsx`, each with an
  import and a named export. Movies is eager by design.
- **File-based:** `autoCodeSplitting` does this from `component: X`. It is "ONLY
  available when you are using file-based routing" with a supported bundler
  ([code-splitting.md][split]). It does not work with the CLI alone.
- **Same mechanism (spike):** the Vite transform emits
  `lazyRouteComponent(importer, "component")`. The `defaultPreload: "intent"`
  reason in the `router.tsx` comment stays valid.
- **What splits:** `component`, `errorComponent`, and `notFoundComponent` by
  default. `pendingComponent` and `loader` do not. Upstream advises against
  splitting `loader` ([automatic-code-splitting.md][auto]).
- **What stays in the entry:** `validateSearch` and `beforeLoad`, as today.
- **Rule:** "Do not export route properties". An exported component is not split
  ([automatic-code-splitting.md][auto]). A route file exports `Route` only.
- **`__root.tsx` is not split** ([code-splitting.md][split]). This is correct
  here: `RootShell` always renders.
- **Code-based alternative:** `Route.lazy()` with `createLazyRoute`
  ([code-splitting.md][split]). The app does not need it.
- **Movies needs an explicit opt-out.** Default splitting moves Movies out of the
  entry. `codeSplitGroupings: []` on that route keeps it eager (spike). The plugin
  accepts the empty array, but the docs show only non-empty values.

### Typed search params: same in both

- `validateSearch` and `search.middlewares` are the same options on
  `createFileRoute` and `createRoute`.
- `validateMembersSearch`, `validateStatsSearch`, `validateAdminRunsSearch`, and
  `stripSearchParams(statsSearchDefaults)` move unchanged (spike).
- All 12 `useSearch`, `useParams`, and `useNavigate` calls with a `from` string
  compile with no edit (spike). `Register` types them in both styles.
- Generated route types come from `FileRouteTypes` in the tree, not from
  `addChildren` inference. Consumers see no difference.
- Inference order matters in both: `validateSearch`, then `search.middlewares`,
  then `beforeLoad`, then `loader` ([create-route-property-order.md][order]).

### Route loaders with TanStack Query: same in both

The app has no loaders today, and 46 `useQuery` call sites. The map lists the
state layer as open, so this section gives constraints, not a decision.

- Upstream pattern: `ensureQueryData` in `loader`, `useSuspenseQuery` in the
  component ([external-data-loading.md][external]).
- With a loader and `defaultPreload: "intent"`, set `defaultPreloadStaleTime: 0`
  so Query owns freshness ([preloading.md][preload]). The official Query examples
  do this ([example][queryexample]). Add it with the first loader.
- A search-driven loader needs `loaderDeps` ([data-loading.md][loading]).

Two behavior traps for this app:

1. **In-page errors.** `ensureQueryData` rejects on a failed fetch, and the route
   error boundary then takes over. The app depends on in-page answers:
   `RosterQueryOptions` says a 403 "is the 'Admins only' answer the page renders",
   and `requireSession` lets non-401 failures reach the page. `prefetchQuery`
   swallows errors ([source][prefetch]) and keeps that behavior.
2. **Stats.** `placeholderData` is not an option of `useSuspenseQuery`
   ([source][suspense]). `StatsGetQueryOptions` uses `keepPreviousData` so
   NumberFlow rolls numbers. Stats must stay on `useQuery`, or find another way
   to keep the numbers mounted.

### Nested admin layouts: small difference

The current tree maps directly. The app uses three layers of layout (`_app`,
`admin`, `integrations/radarr`). A path segment with no component needs no file.

| Route id | File under `web/src/routes/` |
| --- | --- |
| `__root__` | `__root.tsx` |
| `/_app` | `_app.tsx` |
| `/login` | `login.tsx` |
| `/claim/$token` | `claim.$token.tsx` |
| `/_app/` | `_app/index.tsx` |
| `/_app/users`, `/_app/settings`, `/_app/stats` | `_app/users.tsx`, `_app/settings.tsx`, `_app/stats.tsx` |
| `/_app/admin` | `_app/admin/route.tsx` |
| `/_app/admin/` | `_app/admin/index.tsx` (redirect only) |
| `/_app/admin/roster`, `/_app/admin/runs`, `/_app/admin/maintenance` | `_app/admin/roster.tsx`, `_app/admin/runs.tsx`, `_app/admin/maintenance.tsx` |
| `/_app/admin/integrations/` | `_app/admin/integrations/index.tsx` |
| `/_app/admin/integrations/tmdb` | `_app/admin/integrations/tmdb.tsx` |
| `/_app/admin/integrations/radarr` | `_app/admin/integrations/radarr/route.tsx` |
| `/_app/admin/integrations/radarr/` | `_app/admin/integrations/radarr/index.tsx` |
| `.../radarr/acquisitions/$acquisitionID` | `_app/admin/integrations/radarr/acquisitions.$acquisitionID.tsx` |
| `.../radarr/setup`, `.../radarr/webhooks` | `_app/admin/integrations/radarr/setup.tsx`, `.../webhooks.tsx` |

If the admin information architecture changes URLs, the change is a file move in
file-based and a block edit in code-based. The generator rewrites the literal in
`createFileRoute`. It does not rewrite `from` strings in components, but `tsc`
catches those.

### Guards for auth and roles: same in both, one wrinkle

- `beforeLoad` with `throw redirect` is the same option in both. `requireSession`
  and `redirectIfSignedIn` in `web/src/api/authGuard.ts` need no change.
- `/_app` keeps the auth gate. The pathless layout is the upstream pattern
  ([faq.md][faq]).
- **No role guard today, by design:** "Non-admins reach the route and get the
  API's 403 state". Upstream's RBAC pattern redirects in `beforeLoad`
  ([setup-rbac.md][rbac]). That would change behavior, so do not add it in either
  style.
- **Wrinkle: an import cycle.** `appLayoutRoute.beforeLoad` passes a callback that
  calls the module-level `router` for background session expiry. In file-based,
  `routes/_app.tsx` importing `@/router` makes the cycle
  `router.tsx` → `routeTree.gen.ts` → `routes/_app.tsx` → `router.tsx`.
  - `throw redirect` cannot serve this path: expiry happens after `beforeLoad`
    resolves.
  - The `navigate` argument of `beforeLoad` is deprecated ([route.ts][navigate]).
  - The spike put an `onSessionExpired` callback in router context, defined in
    `router.tsx`. This compiles and builds. The spike did not run it in a browser.

### Vite and Vitest: differentiates

Vite:

- Add `@tanstack/router-plugin` as a dev dependency (14 packages in the spike).
- `tanstackRouter()` must come before `react()` ([with-vite.md][vite]).
- Peer range covers Vite 8 ([package.json][pluginpkg]), and the spike confirms it
  on Vite 8.3.5.
- `quoteStyle: "double"` and `semicolons: true` match the repo style.
- The `manualChunks` function needs no change. The `tanstack` and `react-vendor`
  chunks stay within 0.2 kB.
- Chunk names change from `AdminPage-*.js` to `roster-*.js`, and several chunks
  get the name `route-*.js`. This only affects reading of bundle output.
- The plugin and `react-router` must stay in step. Plugin 1.168.48 has the peer
  `@tanstack/react-router ^1.170.46`. Renovate already groups bun minor and patch
  updates in "web dependencies", so they move together.

Build order:

- `bun run build` is `tsc -b && vite build`, so `tsc` runs before the plugin.
  Without a generated file, `tsc` fails with TS2345 on each `createFileRoute`
  literal (spike).
- Commit `routeTree.gen.ts`: it is "part of your application's runtime, not a
  build artifact" ([faq.md][faq]).
- Add a CI drift check: run the generator, then
  `git diff --exit-code web/src/routeTree.gen.ts`. Deterministic output makes this
  safe (spike).
- Add the file to `ignores` in `web/eslint.config.js`
  ([with-vite.md][vite]). The generated header already has `eslint-disable` and
  `@ts-nocheck` ([config.ts][genconfig]).

Vitest:

- `web/vitest.config.ts` is separate from `web/vite.config.ts`, so the plugin does
  not load in tests. No change was necessary (spike). The upstream guide adds the
  plugin to the Vitest config ([test-file-based-routing.md][testing]), but this
  app does not need that while the tree is committed.
- `src/test/providers.tsx` stays hand-built. It avoids guards and lazy components
  on purpose. The real tree would bring them back. It needs the same `/_app/...`
  ids, so **route ids are now a contract** for that harness.
- A test file in `src/routes` gets a generator warning and stays out of the tree
  (spike). Keep tests out of `src/routes`, or use the `-` prefix
  ([config.ts][genconfig]). Thin route files need no tests of their own.

ESLint:

- `react-refresh/only-export-components` does not warn on a route file with an
  imported component (spike). It warns only when the file also defines a local
  component, as `_app/index.tsx` did with `MoviesPage`. Move `MoviesPage` to
  `src/pages/`.
- `@tanstack/eslint-plugin-router` has `create-route-property-order`
  ([docs][order]). This is optional.

### Migration in place: differentiates

| Option | Cutover | Route-by-route after cutover | Verdict |
| --- | --- | --- | --- |
| File-based | One PR, 20 route files, no page edits | One route file per rebuild | **Recommended** |
| Code-based, stay | None | One block in `router.tsx` per rebuild | Viable, keeps a growing file |
| Code-based, split by hand into files | One PR | One file per rebuild | No. Upstream warns of "semi consistent" results ([decisions-on-dx.md][dx]) |
| Virtual file routes | One PR, plus a central `routes.ts` | One file per rebuild | No need. For an "existing route organization" ([virtual-file-routes.md][virtual]). Not spiked |
| Mixed, per route | None | Moves routes one at a time | **No typed path** (see below) |

**The route tree cannot move one route at a time.** The overview lists "Mixed
file-based and code-based routing" ([overview.md][overview]), but no guide at the
pinned tag shows how. The documented mixes are flat with directory routes, and
virtual with physical. Both use file routes.

The spike tried the direct way:
`routeTree.addChildren([...Object.values(routeTree.children ?? {}), codeRoute])`.
The code route registers in `routesById`, but the types do not see it.
`Link to="/legacy"` and `useSearch({ from: "/legacy" })` fail in `tsc`. Registered
types come from the generated `FileRouteTypes`. `addChildren` replaces `children`
([route.ts][addchildren]), so this also depends on generated internals.

**This does not block the rebuild.** The rebuild moves pages, CSS, and components,
not route definitions. One cutover PR is small, and old and new pages sit behind
the same route file afterward.

## Cutover checklist

1. Add the plugin, and add `tanstackRouter()` before `react()` in
   `web/vite.config.ts`, with `autoCodeSplitting: true`, `quoteStyle: "double"`,
   and `semicolons: true`.
2. Create `web/src/routes/` from the table above. Keep the `_app` prefix so all
   12 `from` strings and `src/test/providers.tsx` stay valid. URLs do not change,
   including the fixed `/claim/$token` and `/settings`.
3. Move the two inline validators (`/login`, `/settings`) with their routes.
4. Move `MoviesPage` to `src/pages/`. Set `codeSplitGroupings: []` on `/_app/`, or
   take the decision below.
5. Solve the `router` import cycle in `/_app`.
6. Reduce `web/src/router.tsx` to `createRouter`, `clearMovieModalHistory`, and
   `Register`.
7. Commit `routeTree.gen.ts`, add it to ESLint `ignores`, and add the CI drift
   check.
8. Update references: `docs/DESIGN.md` (line 1086, "defined code-based in
   `router.tsx`"), and comments in `statsSearch.ts`, `LoginPage.tsx`, and
   `StatsPage.tsx`.
9. Run `make lint`, `make test`, and Playwright in Chromium. Check login, session
   expiry, the `/admin` redirect, `/stats` URL filters, and intent preload.

## Questions for the frontend architecture ticket

- **Page location.** Route files stay thin and hold only route options. Where do
  page components live: `src/pages/`, or feature folders? The route tree does not
  force either.
- **Movies chunk.** Keep eager (171.6 kB entry) or split (101.5 kB entry, one more
  request on the landing route)? Today's choice is eager. This is a performance
  decision, not a behavior change.
- **Loader convention.** `ensureQueryData` with `useSuspenseQuery`, `prefetchQuery`
  with `useQuery`, or no loaders. The in-page 403 and Stats traps above limit this
  choice. It belongs to the state layer in #392.
- **Session expiry wiring.** Which mechanism replaces the `router` closure in
  `/_app`.
- **Integration branch.** After cutover, a new route on `develop` lands in
  `router.tsx`, which is gone on the branch, and must be ported by hand as a new
  file. `router.tsx` had 6 commits since 2026-07-31 and 18 in total, so the risk is
  low.

## Sources

Docs and source at the pinned commit:

- [routing/code-based-routing.md][code]
- [routing/route-trees.md][trees]
- [routing/virtual-file-routes.md][virtual]
- [decisions-on-dx.md][dx]
- [overview.md][overview]
- [faq.md][faq]
- [guide/code-splitting.md][split]
- [guide/automatic-code-splitting.md][auto]
- [guide/external-data-loading.md][external]
- [guide/data-loading.md][loading]
- [guide/preloading.md][preload]
- [how-to/setup-rbac.md][rbac]
- [how-to/test-file-based-routing.md][testing]
- [installation/with-vite.md][vite]
- [eslint/create-route-property-order.md][order]
- [examples/react/basic-react-query-file-based/src/main.tsx][queryexample]
- [packages/router-core/src/route.ts, `navigate`][navigate]
- [packages/router-core/src/route.ts, `addChildren`][addchildren]
- [packages/router-generator/src/config.ts][genconfig]
- [packages/router-plugin/package.json][pluginpkg]

Query source at v5.85.5:

- [packages/query-core/src/queryClient.ts, `prefetchQuery`][prefetch]
- [packages/react-query/src/types.ts, `UseSuspenseQueryOptions`][suspense]

Live site, latest, read on 2026-10-31:

- <https://tanstack.com/router/latest/docs/framework/react/routing/code-based-routing>
- <https://tanstack.com/router/latest/docs/framework/react/routing/file-based-routing>
- <https://tanstack.com/router/latest/docs/framework/react/guide/code-splitting>

[pin]: https://github.com/TanStack/router/tree/10a7ff833cd96539023d6214c09f0d6b79d69e29
[code]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/routing/code-based-routing.md
[trees]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/routing/route-trees.md
[virtual]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/routing/virtual-file-routes.md
[dx]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/decisions-on-dx.md
[overview]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/overview.md
[faq]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/faq.md
[split]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/guide/code-splitting.md
[auto]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/guide/automatic-code-splitting.md
[external]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/guide/external-data-loading.md
[loading]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/guide/data-loading.md
[preload]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/guide/preloading.md
[rbac]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/how-to/setup-rbac.md
[testing]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/how-to/test-file-based-routing.md
[vite]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/installation/with-vite.md
[order]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/docs/router/eslint/create-route-property-order.md
[queryexample]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/examples/react/basic-react-query-file-based/src/main.tsx
[navigate]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/packages/router-core/src/route.ts#L1139-L1142
[addchildren]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/packages/router-core/src/route.ts#L1841-L1891
[genconfig]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/packages/router-generator/src/config.ts
[pluginpkg]: https://github.com/TanStack/router/blob/10a7ff833cd96539023d6214c09f0d6b79d69e29/packages/router-plugin/package.json#L135-L141
[prefetch]: https://github.com/TanStack/query/blob/v5.85.5/packages/query-core/src/queryClient.ts#L372-L381
[suspense]: https://github.com/TanStack/query/blob/v5.85.5/packages/react-query/src/types.ts#L81-L89
