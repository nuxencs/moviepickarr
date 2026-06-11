---
name: verify-frontend
description: >-
  Verify front-end changes in moviepickarr by actually running the app and
  driving it in a real browser — screenshots vs. intent, clean console/network,
  responsive viewports, and the real interaction flow. Use this whenever the
  user has edited anything under web/ and wants to confirm it works, looks
  right, or didn't break — phrases like "verify the frontend", "check the UI",
  "does my change look right", "make sure the redesign works", "test this in the
  browser", "did I break anything", or right before pushing/handing off a web
  change. Prefer this over eyeballing the diff: front-end regressions (empty
  data, CORS, layout breaks at a breakpoint, console errors) are invisible in
  source and only show up when the app is running.
---

# Verify front-end changes (moviepickarr)

This project is a Go (Fiber) backend on `:3030` that serves an embedded
`web/dist`, plus a React 19 + Vite 7 + Tailwind v4 SPA. The frontend is a
**single page** — there is no router. "Views" are reached by interacting
(clicking pick/next, opening menus, toggling settings), not by visiting URLs.

The job: bring the real app up, drive the part the change touched, and report a
tight pass/fail with screenshots — fixing obvious breakage in a loop rather than
just narrating it.

Read `docs/DESIGN.md` first (it's the design system + the dev-server wiring of
record). The Movies hero must stay dimensionally static across picks — watch for
layout shift there specifically.

## Step 0 — Scope the change from the diff

Run `git --no-pager diff --stat` and `git --no-pager diff` scoped to `web/`.
Identify which components changed, then reason about **which on-screen state(s)
exercise them**. A change to the hero → the Movies pick view; a change to the
menu component → open the menu; a settings change → open settings. If the diff
is empty (already committed), diff against the base branch (`develop`).

If you genuinely can't tell which view a component renders in, say so and ask —
don't guess and verify the wrong screen.

## Step 1 — Bring the app up (backend + frontend)

Mirror what `make dev` does, minus tmux — run both servers in the background so
you keep control of the shell:

1. Backend: `go run main.go` (serves `:3030`, `/api/v1/*`).
2. Frontend: `bun run --cwd ./web dev` (Vite, defaults to `:5173`).

Launch each with `run_in_background`, then **wait for readiness** — poll the
port / tail the log until Vite prints its `Local:` URL and Go is listening.
Don't navigate before both are up; a blank first paint is usually just a race.

**Capture the actual Vite port — don't assume `:5173`.** If a dev server (or any
process) already holds `:5173`, Vite doesn't fail — it silently climbs to
`:5174`, `:5175`, and so on. Hard-coding `:5173` then points you at a *stale*
server: you'd "verify" someone else's old build and never see your change. So
parse the real URL from Vite's `Local:` line and reuse that exact port
everywhere — navigation, screenshots, and teardown:

```bash
# the port Vite actually bound (read it from the log, don't guess)
VITE_URL=$(grep -oE 'http://localhost:[0-9]+' /tmp/mpa-vite.log | head -1)
```

If `VITE_URL` isn't `:5173`, mention it in the report — a climbed port usually
means a leftover dev server is still running, which is worth surfacing.

**Data loads on `:5173` directly — no CORS detour.** In DEV both
`APIClient.baseURL()` and `useSSE`'s `baseURL()` return `""`, so API + SSE
requests hit `/api/...` **same-origin** and ride the Vite proxy → `:3030` (see
`vite.config.ts`). Same-origin requests don't preflight, so there's no CORS step
to fail. (Fixed in `59f2f80`; the base used to hardcode `http://localhost:3030`,
which *did* trip CORS — that gotcha no longer applies.)

So if the page renders but data is **empty**, it's almost never CORS. Check the
network tab: the `/api/v1/*` requests should target your Vite origin (e.g.
`:5173`) and return `200`. Empty data usually means the **backend isn't up** on
`:3030`, the proxy `target` is wrong, or the DB has no rows — fix that, not an
imagined CORS issue. A *genuine* CORS error on `:5173` means the proxy/`baseURL`
wiring regressed (e.g. the DEV base went back to an absolute `:3030`) — flag it.

To check the **production-like** path (Go serving the embedded build instead of
Vite), run `bun run --cwd ./web build` then `go run main.go` and open **`:3030`**.
That's an embedded-`dist` verification, not a CORS workaround — note it if used.

## Step 2 — Pick the browser tool

Either driver works; load its tools with ToolSearch before calling them:

- **chrome-devtools-mcp** (`mcp__plugin_chrome-devtools-mcp_chrome-devtools__*`)
  — strongest for console messages, network requests, performance, and
  `emulate`/`resize_page` for viewports. Prefer this when you need to inspect
  errors or measure.
- **claude-in-chrome** (`mcp__claude-in-chrome__*`) — fine for navigate +
  screenshot + click flows. Call `tabs_context_mcp` first, then create a fresh
  tab (don't reuse old tab IDs).

Whichever you choose, open a **new** tab/page for the run.

## Step 3 — Run the checks

For each affected view, in this order:

1. **Console + network clean.** Read console messages and failed requests
   *before* judging anything visual — a red console explains a broken render.
   Quote any real errors verbatim. (A CORS error is *not* expected on the
   `:5173` proxy path — if one appears, the dev base/proxy wiring regressed;
   treat it as a real finding, not noise.)
2. **Visual vs. intent.** Screenshot the changed view at desktop width first.
   Compare against what the diff was *trying* to do, not just "does it look
   fine." Call out layout shift, overflow, misalignment, wrong spacing/color.
3. **Interaction.** Actually drive the flow the change touched — click pick /
   next, open the menu, toggle the setting — and confirm the result. A change
   isn't verified until the behavior is observed, not just the static paint.
   Run this as a tight loop per interactive element the change added or moved:
   1. **Drive the change** — interact with the new/changed element and confirm
      it renders and behaves as expected (opens, toggles, navigates, submits).
   2. **Screenshot and analyze it** — capture the resulting state and actually
      read the image: did the right thing happen, is anything mispositioned,
      did an overlay/menu land where it should.
   3. **Check the console for errors** — an interaction can throw without any
      visual tell (a handler that crashes, a failed mutation). Re-read console
      messages *after* driving, not just on first paint.
4. **Responsive.** Re-screenshot the changed view at **375 / 768 / 1440 /
   1920**. Most regressions hide at the edges — 375 (clipping, stacking) and
   1920 (stretched/centered). Flag anything that breaks per-breakpoint.

Capture a couple of extra frames around interactions if recording a GIF so
playback is smooth.

## Step 3b — Mobile performance audit (Core Web Vitals)

Layout can look right and still regress *performance* — a heavy hero image, an
unmemoized list, a font swap that shifts content. Catch that with a mobile audit
(mobile is the harsher budget, so it's the one that matters):

1. **Open the URL in a new page via the Chrome DevTools MCP** — use
   `new_page`, and `emulate` a mobile profile (throttled CPU/network) so the
   numbers reflect a real phone, not your dev machine.
2. **Run a performance trace and audit Core Web Vitals** —
   `performance_start_trace` (with reload) → `performance_stop_trace`, then read
   the insights / `performance_analyze_insight` for **LCP**, **CLS**, and
   **INP**. Flag a regression if the change pushed any of them out of the good
   band (LCP ≤ 2.5s, CLS ≤ 0.1, INP ≤ 200ms) — and tie it back to what the diff
   touched (e.g. an unsized image driving CLS, a new blocking script hurting
   LCP). The hero is the usual LCP element here; watch it specifically.

Right-size this: a one-line copy/style tweak doesn't need a full trace — note
you skipped it and why. A change to the hero, image loading, a large list, or
anything in the initial render path *does* warrant the audit.

## Step 4 — Fix and re-verify (don't just report breakage)

If a check fails and the cause is an obvious front-end mistake (typo'd class,
missing prop, wrong Tailwind token, off-by-one breakpoint, unguarded null),
**fix it in `web/` and re-run the failing check** — HMR makes this fast on the
`:5173` path; rebuild on the `:3030` path. Loop until green.

Stop and hand back (with the screenshot + quoted error + your root-cause read
and file:line) when: the fix is non-obvious, it implies a product/design
decision, it's backend behavior, or you've looped ~3 times without progress.
Don't thrash.

When you touch code, keep edits small and match surrounding style. Only bespoke
MG components — never reintroduce shadcn/Radix primitives (see `docs/DESIGN.md`).

## Step 5 — Report, then tear down

Keep it concise — a verdict per check, evidence inline:

```
## Verify: <short description of the change>
Path: :5173 (dev) | :3030 (built)   ·   Views: <which screens>

- Visual          PASS / FAIL  <one line; screenshot>
- Console/network PASS / FAIL  <clean | quoted error>
- Interaction     PASS / FAIL  <what you drove, what happened>
- Responsive      PASS / FAIL  375 / 768 / 1440 / 1920  <which broke, if any>
- Mobile perf     PASS / SKIP  LCP / CLS / INP  <values, or "skipped — trivial change">

Fixes applied: <files touched, or none>
Verdict: PASS / NEEDS WORK — <one line>
```

Then **tear down the servers you started** so you don't leave ports held. A bare
`pkill -f 'go run main.go'` / `pkill -f vite` isn't enough — both spawn children
(the compiled Go binary, esbuild) that keep holding the ports after the parent
dies. Free the ports directly, using the **actual** Vite port you captured in
Step 1 (not a hard-coded `:5173`) so you kill *your* server and not a pre-existing
one on a different port:

```bash
lsof -ti :3030 | xargs kill -9 2>/dev/null            # Go backend
echo "$VITE_URL" | grep -oE '[0-9]+$' \
  | xargs -I{} sh -c 'lsof -ti :{} | xargs kill -9' 2>/dev/null   # the Vite port you bound
```

Then confirm the ports are free. **Only tear down what you brought up** — if a
server was already running when you started (Go was up, or Vite had climbed past
a port someone else owns), leave that one alone.

## Gate (when verifying before a handoff/push)

If the user is verifying *prior to shipping*, also run the project gate from
`docs/DESIGN.md`: `bunx tsc -b` + `bun run lint` + `bunx vite build` (from
`web/`). A green browser plus a red typecheck is not verified.
