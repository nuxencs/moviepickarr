# Frontend Performance Review - Admin integrations

Date: 2026-08-05
Scope: static review, live transition and scroll QA against the development fixture, and a production bundle build

## Live Metrics (Track A)

Core Web Vitals were not measured. The target is an authenticated Admin route,
and the connected Chrome session does not expose Lighthouse or React commit-hook
data. The live checks below used the Vite development server, so their timing is
directional and is not field performance data.

## Findings

Severity tags:

- Critical: directly fails a Core Web Vital threshold, or has a render or bundle cost that scales badly.
- Warning: measurable cost that does not currently break a threshold.
- Info: low-impact behavior or a future scale concern.

### Critical

None.

### Warning

- [Timer, fixed] `web/src/pages/AdminTMDBPage.tsx:66` previously passed the
  complete wait until the next scheduled check to `setTimeout`. Browser timers
  overflow above 2,147,483,647 ms, about 24.8 days. The page now splits a long
  wait into capped chunks through
  `web/src/pages/tmdbRunDiscovery.ts:1`, rechecks the due time, and sends no
  request before that time. The boundary has a focused regression test.
- [Layout shift, fixed] The persistent Admin body previously changed its top
  padding when Integrations became active. TanStack kept the outgoing outlet
  mounted during the route change, so that page moved 16 px before replacement.
  All Admin routes now share one body inset.
- [Scroll shift, fixed] `web/src/components/moviepickarr/admin/AdminLayout.tsx:28`
  previously reset the shared scroller from the pending location. A deferred
  Runs route moved the still-visible member roster from scrollTop 240 to 0. The
  reset now follows the committed leaf match, while the navigation state still
  responds immediately.
- [Layout shift, fixed] Setting help previously rendered as a second in-flow
  grid row. Opening one control grew a measured 62 px row to 161.7 px and added
  100 px to the scroll surface. Help now portals to a fixed viewport layer.
- [Hit testing, fixed] The info control's grid item stretched to 487.5 px while
  its visible button remained 22 px. The wrapper now shrink-wraps the button,
  including the 48 px coarse-pointer target, without adding runtime work.

### Info

- [Transition] The final 1440 by 900 Chrome trace held the body inset at 16 px
  and the outgoing route root at y=119 until replacement in both directions.
  The persistent indicator now grows from a 22 px line to a 66 px line across
  the active Integrations branch. It animates transform and height on one
  absolute 2 px element, so it does not reflow route content or add a layout
  observer. The one-child grid transition remains the only bounded nav reflow.
- [Scrolling] At a 1440 by 600 viewport, the roster body exposed 697 px of
  content inside a 467 px internal scroller while the document remained exactly
  600 px tall. At 742 px wide, the document regained scrolling and the Admin
  index reported `overflow-y: hidden` with no vertical scroll range.
- [Layout] `web/src/components/moviepickarr/admin/admin-layout.css:104` animates
  one grid track when TMDB appears. That performs bounded layout work for one
  child. Replace it with a transform or clip strategy only if the branch grows
  into a long integration list.
- [Routing] `web/src/components/moviepickarr/admin/AdminLayout.tsx:41` opens the
  TMDB leaf route directly from the Integrations trigger. It deliberately skips
  intent preloading: the live request trace showed focus and pointer preloads
  repeating the fresh authorization check before navigation. The direct action
  leaves the TMDB child as the only `aria-current="page"` marker.
- [Polling] `web/src/pages/AdminTMDBPage.tsx:16` polls every two seconds only
  while a run is active and the document is visible. An overlap guard prevents
  concurrent refetches.
- [Rendering] `web/src/components/moviepickarr/admin/TMDBSettingsForm.tsx:1008`
  compares configuration revisions, so status polling does not rerender the
  settings draft.
- [Help overlay] One open help surface reads the trigger and tooltip rectangles
  for viewport placement. Scroll and resize updates are coalesced to one pass
  per animation frame. Closed help controls attach no listeners.
- [Lists] `web/src/pages/AdminRunsPage.tsx:58` caps failure detail at 25 subjects.
  The API and page cap run history at 50 rows.
- [Scale seam] `web/src/components/moviepickarr/admin/RosterSection.tsx:259`
  updates relative invite wording once a minute, which rerenders the household
  roster. Split or virtualize the rows only if rosters with hundreds of members
  become valid.
- [Backend scale seam] `internal/server/tmdb_runtime_gateway.go:220`
  materializes candidates and a second subject slice before a re-enrich-all run.
  Startup latency and peak memory remain O(library size), acceptable for the
  product's household library scope.

## Runtime re-renders (Track A / DevTools commit hook, if run)

Runtime commit profiling was not run. React Compiler is not enabled. Static
review found no render-time layout reads, unbounded lists, or polling overlap.
The help overlay's geometry reads and scroll listener exist only while it is
open and are animation-frame coalesced.

## Bundle notes (if applicable)

- Admin layout: 2.95 kB JavaScript, 1.11 kB gzip; 5.30 kB CSS, 1.47 kB gzip.
- Admin run history: 6.46 kB JavaScript, 2.11 kB gzip; 5.32 kB CSS, 1.33 kB gzip.
- TMDB settings: 30.87 kB JavaScript, 8.91 kB gzip; 9.04 kB CSS, 2.22 kB gzip.
- App entry: 143.62 kB JavaScript, 46.05 kB gzip.
- Existing vendor chunks stay separate: TanStack 144.41 kB and React 185.33 kB.
- No dependency was added.

## Summary

No open critical or warning-level issue remains. The long-timer overflow found
during the review is fixed. Current navigation keeps persistent geometry stable
and route lists bounded. The indicator adds no measurement loop, and the info
hit-area correction adds no JavaScript. Tooltip placement pays its layout cost
only while help is open.
