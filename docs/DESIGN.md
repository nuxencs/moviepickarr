# Movie Gang — Design System & Decisions

The web UI is a single, bespoke design language called **Movie Gang (MG)**. This
document is the durable record of how it works and the decisions behind it, so we
don't re-litigate them each session.

**Source of truth for values:** `web/src/index.css` (tokens in `:root`/`.dark`/`.light`,
component classes under `@layer components`). This doc explains the system and the
rules; the CSS holds the exact numbers. When they disagree, fix one to match.

Register: **product** (design serves the task; earned familiarity over novelty).

---

## 1. Philosophy

Cinematic, dark-primary, restrained. One gold accent does the talking; everything
else is a tight neutral ramp. Squared, tight geometry. The poster is the only real
"container." The tool should disappear into the task.

The single most important rule: **there is ONE design language.** Every button,
input, modal, and popover comes from the MG vocabulary below. Do not pull in a
second system (see §4).

---

## 2. Tokens (OKLCH)

All color is OKLCH. Themes are driven entirely by tokens, so components that use
tokens adapt to light/dark automatically.

**Accent (fixed gold):** `--accent: oklch(0.82 0.13 84)`, plus `--accent-soft`
(0.16 alpha), `--accent-line` (0.42 alpha), `--accent-ink: oklch(0.24 0.05 84)`
(text on gold). Gold is for primary actions, current selection, and state only —
never decoration.

**Danger (single source):** `--danger: oklch(0.62 0.21 22)`, `--danger-ink:
oklch(0.98 0.01 22)`. All destructive UI derives from `--danger` (the `.btn--danger`
fill and the `.iconbtn--danger` hover use it via `color-mix`). Do not hand-roll
other reds.

**Neutral ramp** (dark / light):
- `--bg` 0.16 / 0.97, `--bg-deep` 0.12 / 0.93
- `--surface` 0.205 / 1.0, `--surface-2` 0.245 / 0.975, `--surface-3` 0.30 / 0.95
- `--line` white@0.08 / ink@0.10, `--line-2` white@0.14 / ink@0.18
- `--ink` 0.97 / 0.22, `--ink-2` 0.78 / 0.42, `--ink-3` 0.68 / 0.50

**Radii (tight, squared):** `--r-sm 2px`, `--r-md 3px`, `--r-lg 4px`, `--r-xl 6px`.
Modals use `--r-xl`. Don't exceed the scale.

**Motion:** a 3-step duration scale — `--dur-fast 0.14s` (pointer feedback: hover,
press, color), `--dur-base 0.22s` (state changes, crossfades), `--dur-slow 0.4s`
(entrances) — plus `--dur-reveal 0.6s` (the hero pick-reveal). `--dur` remains as a
legacy alias of `--dur-base`. Easing: `--ease` (decelerating ease-out, no bounce) for
entrances, `--ease-exit` (accelerate-away) for exits, `--ease-reveal` (expo) for the
pick-reveal. Shadows: `--shadow` (deep, floating surfaces), `--shadow-sm`.

### Contrast (WCAG AA, non-negotiable)
`--ink-3` is the floor for body/meta/placeholder text and is tuned to clear **4.5:1**:
dark `0.68` (4.7:1 on `--surface-3`), light `0.50` (5.5:1 on `--bg`). If you darken
`--ink-3`, re-check both themes against `--surface-2`, `--surface-3`, and `--bg`.
Placeholders use `--ink-3` and must stay legible.

---

## 3. Typography

Three families, by role:
- `--font-ui` **Geist** — all UI: body, labels, buttons, inputs.
- `--font-mono` **Geist Mono** — data: ratings, dates, counts, axis labels, eyebrows.
- `--font-display` **Hubot Sans** — display only: nav wordmark/tabs, section `h2`,
  hero title, stat/section headings. Never use the display font for dense UI labels.

Rules: fixed rem/px scale (no per-element fluid type) for product UI — large screens step
the whole UI up together via a discrete root `zoom` ramp (§13), never fluid `clamp` on
type; weight contrast for hierarchy;
sentence case for section/modal headings (nav tabs are the deliberate uppercase
exception). The mono uppercase tracked "eyebrow" (`.eyebrow`) is a real label element
(timezone, "Pool", "Next picker"), not a decorative kicker on every section.

---

## 4. Component vocabulary — ONE system

All interactive UI uses these MG classes (defined in `index.css`). This replaced the
old shadcn primitives.

- **Buttons:** `.btn` + `.btn--accent` (primary gold), `.btn--ghost` (secondary,
  translucent surface-3 fill), `.btn--danger` (destructive), `.btn--sm` (small).
  Hover brightens (`filter: brightness(1.06)`, no lift); `:active` presses
  (`translateY(1px) scale(0.99)`). Verb+object labels. Buttons deliberately do **not**
  lift on hover — the press on `:active` is the only positional feedback.
- **Inputs:** `.field` — a 42px filled wrapper with a leading icon and gold
  focus-within border. All text inputs use this; there is no bare/hollow input style.
- **Icon buttons:** `.iconbtn` (34px), `.iconbtn--danger` for destructive.
- **Segmented control:** `.seg` (+ `.seg--accent` for the active-gold variant).
  Inside `.statsfilters` the seg is restyled to the chips' dialect (see Stats
  filter row below).
- **Filter bar:** `.filterbar` + `.filterchip` (`FilterBar.tsx`) — a wrapping row of
  mono 12px chip-trigger selects (Genre / Actors / Crew / Picked by / Release year, stats-only).
  The chip wraps two real buttons — the trigger and, when active, a clear X — so both
  stay keyboard-reachable without nesting buttons. The dropdown reuses the `.mg-menu`
  surface and motion as a listbox (`.filtermenu` scopes its extras: an inline `.field`
  search for long people lists and a scrollable option list). An active chip is
  gold-tinted (`--accent-soft` fill, `--accent-line` border) — gold = state, not
  decoration. Chip triggers grow toward the touch minimum under `pointer: coarse`
  (§13). One shared internal base (`FilterChipMenu`) powers three wrappers:
  single-select `FilterSelect` (pick closes; re-pick clears); multi-select
  `FilterMultiSelect` (Actors/Crew people lists, sorted A→Z by name — picks toggle and
  the menu stays open, selected rows get a fixed-slot `CheckIcon` (`.filtermenu__check`)
  so selection is never gold alone, the chip reads `Actors · Keanu Reeves` for one
  selection or `Actors · 2` beyond, and the clear X clears the whole group); and the
  Release-year `ReleaseYearSelect` — a grouped single-select listing selectable decade
  headers (`.filtermenu__item--decade`) with their years indented beneath
  (`.filtermenu__item--year`), where a decade and an exact year are mutually exclusive
  (picking one clears the other) and the chip reads `Release year · 1990s` or `· 1994`.
- **Stats filter row:** `.statsfilters` — ONE filter system (time presets, watch-year
  quick-select, genre, actors, crew, picked by, release year) in a single wrapping row under the
  stats header. The seg stays a seg (presets are mutually exclusive) but drops its
  filled-container costume for the chips' dialect: transparent, `--line-2` outline,
  `--r-sm`, mono 12px, 30px rhythm, hairline dividers between segments, and the
  active preset wears the chips' gold tint (`--accent-soft` + inset `--accent-line`)
  instead of solid gold — presets and filters read as one control family. The
  `.filterbar` inside is flattened with `display: contents` (the wrapper is not
  focusable — §6's rule targets focusable triggers) so chips wrap as row items beside
  the seg instead of as one indivisible block.
- **Avatar:** `.avatar` — the square, hue-derived initials block. An optional `src`
  photo (TMDB headshot, `.avatar__img`) layers over the initials gradient and falls
  back to the initials when missing or failing to load; the size contract is
  identical either way. While the photo loads the initials gradient is the
  placeholder under a shimmer sweep (`.avatar__shimmer`, reusing `mg-shimmer`) and
  the photo crossfades in (`.avatar--loading`).
- **Cast strip:** `.castrow` + `.castcard` (movie modal) — a horizontally scrollable
  row of 2:3 headshot cards: TMDB profile photo (the Avatar initials gradient,
  stretched to fill the frame, as fallback) with mono 12px name + character below
  (`.castcard__caption`). Each card is a link to the person's TMDB page (new tab);
  hover/`:focus-visible` lifts the name to gold. The strip is hidden entirely when a
  movie has no cast.
- **People rail:** `.peoplerail` + `.peoplecard` (stats) — the castcard visuals as a
  horizontally scrollable drill-down strip for most-watched directors/actors, with
  the count on the caption line ("4 movies"). Two SIBLING interactive layers per
  card, never nested: `.peoplecard__toggle` (a real button; `aria-pressed`) toggles
  the person in the matching multi-select filter, and `.peoplecard__ext` — a small,
  always-visible corner scrim chip (hover-only would strand touch) with its own tab
  stop — opens their TMDB page without touching the filter. Active = gold ring on
  the photo + gold name (gold is state).
- **Films-in-filter-view rail:** `.movierail` + `.movietile` (stats, under the KPI strip,
  heading "Films in Filter View") — the concrete films behind the count (the active
  window AND all filters) as a horizontally scrollable strip of `Poster` tiles (title +
  year·picker caption), each a button that opens the movie modal. Same inline-padding / negative-margin edge trick as `.peoplerail` so the
  first tile's hover/focus ring isn't clipped; hidden when nothing matches. The set
  comes from the stats endpoint's `matchedMovieIDs` joined to the cached watched list,
  so its count can't drift from the KPI.
- **Genre donut:** `.genredonut` + `.donut` + `.donut-legend` (stats) — a pure-CSS
  `conic-gradient` disc (hole cut with a `radial-gradient` mask, so any background
  shows through) of the top genres + "Other". Segments use ONE hue: the accent at
  stepped alphas (the `--donut-*` ramp on `.genredonut`), keeping §2's "one gold"
  rule; the legend (swatch + name + mono count) is the accessible representation —
  the disc is `aria-hidden`, so color is never the only channel. Conic geometry
  can't tween, so the disc gets an `mg-fadeUp` entrance only.
- **Decade chart:** `.hourchart--decades` (stats) — the hourly-chart pattern at
  decade granularity: chronological left-aligned columns (capped width), skipped
  decades render as zero columns (a gap is information), counts on non-empty
  columns stay permanently visible, and the axis labels every column ("1990s").
- **Poster:** `.poster` — the one container; real TMDB art or a deterministic
  procedural duotone (`lib.ts`), with a rating badge. While the photo loads the
  duotone is the placeholder under a sweeping shimmer (`.poster__shimmer`,
  `mg-shimmer` keyframe) and the photo crossfades in over it (`.poster--loading`);
  the duotone is the permanent art only when there's no/failed image.
- **Meta chips:** `.metachips` — a wrapping row reading `year · runtime · ★rating`
  (mono, dot-separated) `│` genre chips `│` external links. A `.metasep` vertical
  rule divides the rating facts from the genres, and the genres from the links.
  `.metalink` items are mono external links (IMDb / TMDB / Letterboxd) derived from a
  movie's stable ids via `externalLinks()` (`lib.ts`). Rendered by `MetaChips`
  (`Bits.tsx`), shared by the hero and the movie modal — the **hero passes `links`**
  so they sit inline after the genres; the **modal omits them** and renders its own
  `.moviemodal__links` button block instead (no duplication).
- **Modal:** the bespoke `Modal` component (§5).

### Decision: no shadcn primitives
The redesign began with shadcn `Button`/`Input`/`AlertDialog` still powering the
edit/delete dialogs, which created two visibly different design languages (different
button weight, input height, focus ring, backdrop, radius, motion) that a user hit at
the moment of editing/deleting. We removed them:
- `EditMovieDialog` and `DeletionDialog` were re-authored on the bespoke `Modal` with
  `.field` + `.btn`.
- `web/src/components/ui/{alert-dialog,button,input}.tsx` were **deleted**.
- The user "more actions" menu is the bespoke `Menu` (`movie-gang/Menu.tsx`): a
  portalled floating surface (`.mg-menu`) on the shared `mg-scaleIn` / `mg-scaleOut`
  motion, scaling from the trigger corner. It owns its focus behaviour — Esc/Tab
  return focus to the trigger, but selecting an item does not (the action's `Modal`
  takes focus), so the trigger can't sit focused behind a dialog where Enter would
  reopen it. This replaced the last Radix primitive (`ui/dropdown-menu.tsx`, deleted)
  and dropped `@radix-ui/react-dropdown-menu` — **no Radix remains**.

`web/src/index.css` keeps an `@theme inline` block mapping shadcn-style `--color-*` /
`--radius-*` aliases onto MG tokens; Tailwind colour utilities (e.g. `text-destructive`)
still resolve through it, but no control system depends on it.

**Guardrail:** do not reintroduce shadcn `Button`/`Input`/`AlertDialog` or any second
control system. Extend `.btn`/`.field`/`.modal` instead.

---

## 5. Modals & overlays

One overlay system: the bespoke `Modal` (`web/src/components/movie-gang/Modal.tsx`).
- Portalled; dark blurred veil (`rgba(5,6,10,0.62)` + `blur(8px)`); `--r-xl` corners;
  matching `mg-scaleIn` enter / `mg-scaleOut` exit.
- Dismiss via Esc, veil click, or an explicit top-right close X (`.iconbtn`).
- Has a **focus trap + focus restore** and `role="dialog"` / `aria-modal`, so it is a
  proper accessible dialog. On open, focus moves to the first form field (else the
  surface), not the close X.
- Layout slots: `.modal__head` (title + description + close), `.modal__body`
  (`padding: 22px 26px 0`), `.modal__foot` (`padding: 20px 26px 24px`, right-aligned
  buttons). Widths: `.modal` 960px, `.modal--movie` 560px, `.modal--form` 460px.

Destructive confirms (`DeletionDialog`) use the same `Modal`: dismissing (Esc / veil /
Cancel) is the safe choice, so outside-click dismiss is intentional; only the explicit
`.btn--danger` confirms.

---

## 6. Motion & accessibility

- **Duration scale (tokens, §2):** feedback `--dur-fast`, state `--dur-base`, entrance
  `--dur-slow`, the hero pick-reveal `--dur-reveal`. Pointer feedback uses the fast
  token; exits use `--ease-exit` and run one step faster than their enter. No
  bounce/elastic. Motion conveys state, not decoration. Hardcode no new durations —
  reach for the scale.
- **CSS-only.** No JS animation library; everything is CSS keyframes (prefixed `mg-`) +
  transitions. Do not add framer-motion / gsap / etc.
- **Stat bars** are sized by real geometry, NOT `transform` scale: horizontal
  member/weekday bars use `width: calc(--p * 100%)`, vertical hourly bars use
  `height: calc(--p * 88%)`. Scaling a rounded box squishes its `border-radius` on short
  bars (`2px × scale`), and Chrome/Firefox rasterize the squished corners differently
  (short bars looked a different style in Firefox); real width/height draws the radius at
  true size everywhere. A timeframe toggle tweens each bar between its old and new size
  (`transition: width` / `transition: height`); the `mg-growBfill`/`mg-growHcol`
  `from`-only keyframes are the from-0 entrance on mount only. The hourly count label sits
  just above its bar (flex column, bottom-aligned) so it hugs the tip. The release-decades
  chart rides the same pattern (`.hourchart--decades`, §4). The genre donut is the one
  non-bar visualization: a `conic-gradient` can't tween its stops cheaply, so it fades in
  (`mg-fadeUp`) and re-renders on window changes instead of animating between them.
- **Floating surfaces** (the `Modal` and the `Menu` via `.mg-menu`) share one
  enter/exit: `mg-scaleIn` (base) / `mg-scaleOut` (fast, `--ease-exit`). Both read the
  same exported `exitDelayMs` for the JS unmount delay (`--dur-fast`, dropping to 0
  under reduced motion) so CSS and JS never desync.
- **Tab underline** is one shared `.tab__ink` (not one element per tab): `NavBar.tsx`
  measures the active button and drives the indicator's `left`/`width`, and CSS
  transitions the slide so it glides between tabs instead of disappearing and
  reappearing. It re-measures on resize and on `document.fonts.ready` (font swap
  changes label width). The reduced-motion guard collapses the slide to an instant
  jump for free.
- **Image load:** poster and avatar photos crossfade in over the duotone/initials
  placeholder under an `mg-shimmer` sweep (`.poster__shimmer` / `.avatar__shimmer`) —
  both reduced-motion-guarded to `animation: none`.
- **`prefers-reduced-motion`**: the global block zeroes animation/transition *duration
  AND delay* (so staggered reveals don't pop in) and collapses iteration counts.
  Loaders are the one exception — `.mg-spin` keeps spinning (essential motion). Any new
  motion must degrade to an instant state via this block; never gate visible content on
  a keyframe RM kills. Reduced motion is not optional.
- **Focus:** global `:focus-visible` is a 2px gold outline. Every focusable element
  must render a box (do NOT use `display: contents` on a focusable trigger — it leaves
  the outline nothing to draw on).
- **Hover-revealed controls** (row actions, etc.) must also reveal on `:focus-within`
  so keyboard users can reach them. Card/tile hover effects must also fire on
  `:focus-visible`.

---

## 7. The Hero (static-layout contract)

The Movies-tab hero must be **dimensionally static across picks**: changing the pick, its
metadata, the title length, or the tagline length must not move any component or resize
the banner. (Static across *picks*, not across *viewports* — the geometry steps by
breakpoint; see the phone and large-screen notes below.) How it's built (`Hero.tsx` +
`.hero__*` in `index.css`):

- `.hero__inner` carries generous vertical padding (`56px` top / `60px` bottom) for a
  cinematic, roomy banner. Horizontal padding stays `32px`. The body height (below) is
  unchanged — the extra room is breathing space around the content, not added to it.
- `.hero__body` is a fixed-height flex column (`min-height: var(--hero-body-h)`, default
  18.5rem), sized for the worst case so the banner never resizes.
- **Poster** column width is `calc(var(--hero-body-h) * 2 / 3)` — derived from the body
  height (`--hero-body-h`) and the 2/3 poster ratio so the poster's bottom edge lines up
  exactly with the pinned action button. The dependency is real: the large-screen step
  changes only `--hero-body-h` and the poster width tracks it automatically.
- The hero poster **omits the rating badge** (it passes no `voteAverage`): the rating
  already appears in the meta row, so the badge would be redundant. The badge still
  renders on tile-grid posters, where the meta row carries no rating.
- **Top group** (eyebrow + title) is anchored to the top. The title is clamped to 2
  lines; a long title gains a second line that grows *downward* into the negative
  space without moving anything else.
- **Bottom group** (tagline → meta → actions) is pushed to the bottom via
  `margin-top: auto` on `.hero__tag`. The action buttons are effectively pinned to the
  bottom of the hero.
- **Tagline** sits in a fixed 2-line slot (`min-height: 2.8em` + `-webkit-line-clamp:
  2` + `overflow: hidden`). Data showed ~80% of taglines are 1 line, ~16% 2 lines,
  ~3% 3+ lines. A 1-line tagline reserves the second line (whitespace is acceptable);
  a **3+ line tagline truncates to 2 lines with an ellipsis and moves nothing.**
- **Meta slot** carries the `.metachips` row (§4): facts `│` genres `│` external
  links, in white-on-backdrop. Links derive from ids present on every enriched pick,
  so the row's presence is consistent pick-to-pick and does not break the contract.
- The hero stays a **dark "island"** in light theme by design (cinematic scrim over a
  backdrop). Its scrim/text are intentionally dark/white in both themes; only fix the
  genuinely-broken light overlays, not the hero's darkness.

**Pick-reveal (motion).** When the pick changes (or clears), the banner reveals the new
state without breaking the static contract: a two-layer `.hero__bg-stack` crossfades the
incoming backdrop (preloaded + `decode()`d in `Hero.tsx` so it never flashes blank) with
a slow settle-scale (`mg-bgReveal`), and the poster + each text slot fade-settle in,
lightly staggered via `--i` (`mg-revealPick`). It is **transform/opacity only** — the
reserved slots keep their boxes, so nothing reflows. The content is keyed on the pick id
so the reveal replays per pick; reduced-motion collapses it to an instant swap. The Pick
/ Mark-Watched buttons show in-flight state (`Loader2Icon` + "Picking…" / "Marking…").

Verified static: empty vs populated and a 173-char injected tagline all leave
`eyebrowTop`, `meta top`, `actions top`, and hero height identical.

On phones (≤700px) the hero drops the fixed height and stacks: a 120px poster above
the eyebrow → title → tagline → meta → actions, full width. The static-layout contract
governs the desktop banner only. The rest of the responsive system lives in §13.

On large screens (≥1728px) the hero steps *up*: `--hero-body-h` grows (18.5rem → 21rem,
and the poster width tracks it via the `calc()` above), `.hero__inner` padding opens up,
and the title clamp ceiling rises (54 → 64px) — so the centerpiece feels grander, not
merely bigger. Like the phone stack this is per-breakpoint geometry, not a break of the
contract: within the breakpoint the banner stays dimensionally static as the pick changes.
The whole hero also rides the global `zoom` ramp (§13) on top of this step.

---

## 8. Empty states

All "nothing here" / placeholder copy uses the single `.empty` class (centered,
`--ink-2`, 32px vertical padding). Do not hand-roll per-tab padding/alignment.

---

## 9. Copy & microcopy

- **Buttons:** verb + object ("Save changes", "Add User", "Mark as Watched").
- **Error toasts:** one voice, "Failed to <verb> <thing>" ("Failed to update movie").
  Not "Error <x>ing".
- **Success toasts:** terse, past tense, no exclamation ("Movie picked", "Marked as
  watched", "<title> updated").
- **Loading labels:** present participle + ellipsis ("Searching…", "Adding…").
- **Modal headers:** sentence case ("Edit movie", "Delete movie").
- **Pluralization:** use `plural(n, noun, pluralForm?)` from `lib.ts` for any
  count + noun, so "1 movie" / "2 movies" (never "1 movies"). Covers films, people, etc.
- **No em dashes** anywhere in UI copy. Use periods, commas, colons, or parentheses.

---

## 10. Iconography (lucide)

- `PlusIcon` = "add / open the add flow" (board "Add to stash" button, "Add User").
  Note: empty **pool** slots are non-interactive placeholders (no `+`) — movies enter
  the pool only by being promoted from the stash, so a `+` there would falsely imply a
  direct pool add.
- `SearchIcon` = actual search/filter input fields only.
- `MoveUpIcon` = promote to pool, `MoveDownIcon` = demote to stash.
- Keep one icon per concept across the surface.

---

## 11. Toasts

Sonner, themed to MG in `index.css` via the `.mg-toast*` classes (applied through
`toastOptions.classNames` in `ui/toast.tsx`): `--surface` background, `--line-2`
border, `--r-md`, `--shadow`, Geist; success icon gold, error icon `--danger`. The
overrides are unlayered + `!important` so they beat Sonner's injected styles, and they
use tokens so they follow the theme.

---

## 12. Stack & build notes

- React 19 + Vite 7 + Tailwind **v4** (`@tailwindcss/vite`), TanStack Query, Sonner,
  lucide. No Radix/shadcn. Package manager: **bun** (`web/`).
- Tailwind v4 has no config file; theme/aliases live in `index.css` (`@theme inline`).
- Gate before handoff: `bunx tsc -b` + `bun run lint` + `bunx vite build` (all from
  `web/`). The editor's inline TS diagnostics often fail to resolve the `@/*` path
  aliases and show false "cannot find module" errors; trust `tsc -b`, not them.
- Backend: Go (Fiber) on `:3030`, serves the embedded `web/dist` and `/api/v1/*`.
  Run with `web/dist` built (`go run main.go` re-embeds the current dist at compile).
- **Dev data path:** `APIClient.baseURL()` and `useSSE`'s `baseURL()` return `""` in
  DEV, so API + SSE calls hit `/api/...` **same-origin** and ride the Vite proxy
  (`vite.config.ts`) → `:3030`. Same-origin means no preflight, no CORS — data loads
  on `:5173` directly. (Fixed in `59f2f80`; the base used to hardcode `:3030`, which
  the browser blocked as a cross-origin/CORS call.) Empty data on `:5173` ⇒ the Go
  backend isn't up, the proxy `target` is wrong, or the DB is empty — not CORS.

---

## 13. Responsive & touch

Desktop-first, with a dedicated phone/touch pass below and a large-screen scale-up above.
Breakpoints are content-driven but drawn from one tidy scale — **560 / 640 / 700 / 760 /
900** down, **1728 / 2240 / 2560 / 3200 / 3840** up — documented inline in `index.css`
(the "Responsive — phone & touch adaptations" and "Large-screen scale ramp" blocks).
What each does:

- **560** — the movie modal's internal poster + info split stacks.
- **640** — the *phone* breakpoint. Navigation moves to a fixed **bottom tab bar**
  (below); section headers (`.sec-head`) stack to a title row + full-width controls;
  user boards go single-column; the stats window control spans the row and the
  picked-by-member bars shrink their name column (`188px → 112px`) so the track keeps
  width; poster grids become denser thumbnail grids; modal chrome tightens.
- **700** — the hero stacks (poster above text) and page / top-nav padding tightens.
- **760** — the stat strip drops 3 columns → 2.
- **900** — the stat strip drops 6 columns → 3, and the stats two-column sections
  (weekday | hourly, genres | decades) collapse to one.

**Large screens.** A mirror of the phone pass, scaling *up*. Above the 1240px column the
whole UI steps up through a discrete root `zoom` ramp (`:root { zoom }` at 1728 / 2240 /
2560 / 3200 / 3840px) so type, posters, modals, menus, grids and the column all grow
together — keeping the centered cinematic composition instead of leaving content adrift on
a 2K/4K panel. It lives on `:root` (not `.app`) because modals, menus and toasts portal to
`<body>`; only a root-level scale reaches them. The top steps carry a `min-height` guard
so ultrawide-but-short panels (e.g. 3440×1440) don't over-scale. Stepped, not fluid
`clamp` — the discrete-scale ethos (§3) holds. The **hero** takes an extra large-screen
step on top of the zoom (taller `--hero-body-h`, roomier padding, a higher title ceiling)
so the centerpiece feels grander, not merely bigger (§7).

**Bottom tab bar.** Below 640px the top-bar tabs (`.nav__tabs`) hide and a fixed
`.navbar-bottom` renders the 3 tabs in thumb reach; the wordmark (a home control: click it
to return to the Movies tab from any tab) + theme toggle stay in the top bar. The active tab is gold-tinted — there is no sliding underline there (the
`.tab__ink` slider is desktop-only, and `NavBar`'s `measure()` skips when the top tabs
are `display:none`). The bar carries `padding-bottom: env(safe-area-inset-bottom)`, the
`.shell` reserves clearance for it, and `index.html` sets `viewport-fit=cover`; the
bottom-right toaster is lifted above the bar on phones.

**Touch (`@media (hover: none)`).** Every hover-revealed control must be reachable
without a pointer. On touch: watched/stash row actions (`.wr-actions`, `.sr-actions`)
are always visible; the pool **demote** control becomes a small persistent bottom-right
button instead of the full-cover hover scrim (poster art stays visible); and search
results show a persistent **Add** button (the hover overlay's caption is dropped, since
the `.r-info` caption already sits below the poster). The desktop hover-reveal is
unchanged.

**Tap targets (`@media (pointer: coarse)`).** `.iconbtn`, `.btn--sm`, and segmented
buttons grow toward the 44px touch minimum.

---

## 14. Guardrails (the short list)

1. One design language. No shadcn `Button`/`Input`/`AlertDialog`; extend `.btn`/
   `.field`/`.modal`.
2. All dialogs/confirms use the bespoke `Modal`.
3. Body/meta/placeholder text must clear 4.5:1 in both themes (`--ink-3` is the floor).
4. Keep `prefers-reduced-motion` working; every focusable has a visible focus box;
   hover-revealed controls also reveal on focus.
5. The hero stays dimensionally static (§7); 3+ line taglines truncate.
6. Copy: verb+object buttons, "Failed to X" errors, `plural()` for counts, no em dashes.
7. Gold = action/selection/state only. Danger = the single `--danger` ramp.
8. Responsiveness is structural (bottom nav + single-column reflows below 640; a discrete
   root `zoom` ramp scales the whole UI up on large screens — §13), never per-element fluid
   type. Every hover-revealed action needs a `hover: none` fallback so touch users can
   reach it (§13).
