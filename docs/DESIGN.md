# moviepickarr — Design System & Decisions

The web UI is a single, bespoke design language called **moviepickarr (MG)**. This
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
(entrances), plus `--dur-reveal 0.6s` (the hero draw-reveal) and `--dur-spin 6.5s`
(the slot-machine draw reel, §7). `--dur` remains as a legacy alias of `--dur-base`. Easing: `--ease` (decelerating ease-out, no bounce) for
entrances, `--ease-exit` (accelerate-away) for exits, `--ease-reveal` (expo) for the
draw-reveal, `--ease-reel` (easeOutCubic) for the longer draw reel. Shadows: `--shadow` (deep, floating surfaces), `--shadow-sm`.

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
(timezone, "Pool", "Next up"), not a decorative kicker on every section.

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
- **Section head:** `.sec-head` holds a `.sec-title` (the `h2` plus its data) on the left
  and the section's controls on the right. The data slot is mono `--ink-3` text: a count
  (`.sec-count`, 14px) and, where a page also has round state to report, a status line
  (`.sec-status`, 12px, `+0.01em`, `nowrap` released at 760) beside it. Two spans, not
  one, and the status steps down a size, because two mono spans at the same size on one
  baseline read as a run-on string. Movies fuses its count and round word into a single
  14px `.sec-count` and stays that way; Members splits them (`4 people` + `9 of 12 slots
  filled · round closed`). A count slot states no number until there is one to state: a
  head that says `0 people` while the query is in flight is wrong about the one thing it
  is there for.
- **Segmented control:** `.seg` — neutral surface-3 active (the Movies watched
  grid/list toggle, filled to match the `.field` search beside it). Inside
  `.statsfilters` the seg is restyled to the chips' dialect — transparent with a
  gold-tint active — so the time presets read as one family with the filter chips
  (see Stats filter row below).
- **Filter bar:** `.filterbar` + `.filterchip` (`FilterBar.tsx`) — a wrapping row of
  mono 12px chip-trigger selects (Genre / Actors / Crew / Added by / Release year, stats-only).
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
- **Long lists are virtualized (`@tanstack/react-virtual`).** Two lists grow with the
  library and both would otherwise render every row: the Movies **watched grid/list**
  and the **filter menus** (1500+ actors on a mature library). Each feeds its search
  box through `useDeferredValue` into a memoized filter, so a keystroke updates the
  input immediately and the list at React's convenience, then renders the matches
  through a virtualizer — a screenful of DOM instead of the whole result set, flat per
  keystroke however large the library gets. Two things to know before touching them:
  the watched grid keeps its `.tile-grid` class and reads the *resolved*
  `gridTemplateColumns` + gaps back out via `useGridMetrics`, so the responsive
  `repeat(auto-fill, minmax(…))` ramp stays in the stylesheet and is never restated in
  JS (the list view resolves to one lane); and the filter menu's option list is
  keyboard-navigated **by index**, not by DOM order — the arrows (plus Home/End once
  focus is in the list, so they stay caret keys in the search field) scroll the target
  option into view and focus it once rendered, since the option being moved to usually
  does not exist yet. Options carry `aria-setsize`/`aria-posinset` so the full match
  count is announced rather than the rendered window, and an option scrolled out from
  under the keyboard hands focus back to the list instead of dropping it to `<body>`.
  Don't put `content-visibility` on virtualized rows: it reports
  `contain-intrinsic-size` instead of the real height and feeds row measurement a lie.
- **Shorter lists keep typing off the rows instead.** Where a list is small enough that
  a virtualizer would be machinery for nothing, the rule is that a keystroke must not
  reach the rows. Two shapes do that. A text field that lives above a list gets pushed
  into its own child that owns the field state (`AddMemberForm` in the admin roster), so
  the character re-renders the form and the roster table sits still. A filter that has
  to live in the parent because it drives the list (`Board`'s stash search) instead
  memoizes the row (`StashRow`), whose props are the movie straight out of the query
  cache plus primitives, so only rows entering or leaving the match set do any work.
- **Stats filter row:** `.statsfilters` — ONE filter system (time presets, watch-year
  quick-select, genre, actors, crew, added by, release year) in a single wrapping row under the
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
  stop — opens their TMDB page without touching the filter. Active = gold name (gold
  is state). A ring on the photo doesn't work here: `.avatar` is positioned and
  opaque, so it paints over the outline of its non-positioned parent, and an outward
  ring gets shaved by the rail's overflow clip at the edge cards.
- **Films-in-filter-view rail:** `.movierail` + `.movietile` (stats, under the KPI strip,
  heading "Films in Filter View") — the concrete films behind the count (the active
  window AND all filters) as a horizontally scrollable strip of `Poster` tiles (title +
  year·adder caption), each a button that opens the movie modal. Same inline-padding / negative-margin edge trick as `.peoplerail` so the
  first tile's hover/focus ring isn't clipped. The set
  comes from the stats endpoint's `matchedMovieIDs` joined to the cached watched list,
  so its count can't drift from the KPI. The rail always renders and owns the filter
  view's single empty state: when the count is zero it keeps its heading and shows one
  `.empty` placeholder (worded by whether a filter is narrowing it), and it is the only
  thing under the KPI strip — the member leaderboard, activity charts and people rails
  all drop away with it, because zeroed bars under an empty filter view are noise, not
  information. (A non-zero count with no cached posters yet is the transient join lag,
  so the placeholder reads "Loading films…".)
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
  `.moviemodal__links` column in the rail instead (no duplication).
- **Movie detail modal:** `.moviemodal__*` (`MovieModal.tsx`) — a film's record on an
  880px capped surface (§5): a 260px backdrop (190px narrow), then a 172px **rail** (`.moviemodal__rail`)
  of poster + the links out, beside the reading column. The links are quiet mono lines
  with a small icon, not ghost buttons: three buttons read as three things to do, three
  mono lines read as reference material attached to the film. The **credit block**
  (`.moviemodal__credit`) puts "Directed by / Written by" and the attribution
  (`.moviemodal__by` — added by, plus the watch date on a watched film) side by side,
  split by a rule that spans the whole block (`align-items: stretch`), because both are
  the same kind of line: who is responsible for this. Below 700px the rail becomes a row
  (links bottom-aligned beside the poster, clearing the backdrop the poster overlaps) and
  the credit columns stack with the rule turning into a top border.
- **Movie actions:** `.moviemodal__actions` (`MovieActions` in `MovieModal.tsx`) — rename
  and delete as two labelled rows (`.moviemodal__act`) at the foot of the rail, stacked
  under the links at every width and sharing their 13px icon column. They read as
  controls, not references: the label is sans where the links are mono, and the row
  lights a background on hover and focus where a link only changes colour, so the
  destructive red arrives on the pointer instead of sitting at rest. Whether it is drawn
  is derived from the movie object and
  never passed in: the adder gets it, everyone else gets nothing, on every surface the
  modal opens on. It arrives with the detail, since the film's status is a detail field,
  and the delete waits for the pool-lock query too rather than defaulting to unlocked.
  A refused delete (a pooled film while the round is locked or a draw is out) stays in
  place and goes inert with `aria-disabled`, the reason on both the accessible name and
  the tooltip (`Delete, round closed`) — the same treatment, and the same words, as a
  board tile's refused move (`refusals.ts`). Below 700px, where the rail is a row, the
  links and the actions pair off beside the poster inside `.moviemodal__railfoot` (a
  `display: contents` wrapper in the rail's column layout): tops flush so the first
  action sits on the first link, a vertical rule between them in place of the stacked
  one above, and the poster shrinks (to 84px at the floor) rather than the row breaking.
- **Modal:** the bespoke `Modal` component (§5).

### Decision: no shadcn primitives
The redesign began with shadcn `Button`/`Input`/`AlertDialog` still powering the
edit/delete dialogs, which created two visibly different design languages (different
button weight, input height, focus ring, backdrop, radius, motion) that a user hit at
the moment of editing/deleting. We removed them:
- `EditMovieDialog` and `DeletionDialog` were re-authored on the bespoke `Modal` with
  `.field` + `.btn`.
- `web/src/components/ui/{alert-dialog,button,input}.tsx` were **deleted**.
- The user "more actions" menu is the bespoke `Menu` (`moviepickarr/Menu.tsx`): a
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

One overlay system: the bespoke `Modal` (`web/src/components/moviepickarr/Modal.tsx`).
- Portalled; dark blurred veil (`rgba(5,6,10,0.62)` + `blur(8px)`); `--r-xl` corners;
  matching `mg-scaleIn` enter / `mg-scaleOut` exit.
- Dismiss via Esc, veil click, or an explicit top-right close X (`.iconbtn`).
- Has a **focus trap + focus restore** and `role="dialog"` / `aria-modal`, so it is a
  proper accessible dialog. On open, focus moves to the first form field (else the
  surface), not the close X.
- The Esc / outside-click / focus-restore / exit-timer behaviour is not hand-rolled
  per surface: `Modal`, the `Menu`, and the stats filter dropdowns all ride one shared
  machine, `useDismissible` (`web/src/hooks/useDismissible.ts`), so every floating
  surface dismisses the same way. The `ProfilePanel` rides it too; its inline
  `VolumeControl` owns no dismissal of its own.
- **Esc, outside-click and the focus trap belong to the topmost surface only.** A
  dialog opened from inside another one (a confirm over the movie modal) portals in
  as a sibling, so neither surface can see the other through the DOM. `useDismissible`
  keeps a stack of the surfaces on screen and hands out `isTopmost()`; a surface with
  something over it lets those gestures pass. A surface holds its place on the stack
  through its exit motion, so a second Esc mid-close can't take the one underneath
  with it. The body-scroll lock is refcounted for the same reason: the page gets its
  scroll back when the last dialog closes, not the first.
- Layout slots: `.modal__head` (title + description + close), `.modal__body`
  (`padding: 22px 26px 0`), `.modal__foot` (`padding: 20px 26px 24px`, right-aligned
  buttons). Widths: `.modal` 960px, `.modal--movie` 880px, `.modal--form` 460px.
- **Two scroll modes.** By default the veil scrolls and the surface grows to its
  content. Its 56px of breathing room is two spacer items in a flex column rather
  than block padding, because a scroll container's bottom padding is not part of its
  scrollable overflow and a tall dialog would otherwise park flush against the window
  edge. `<Modal capped>` switches to the other mode: the veil stops scrolling
  (`:has(.modal--capped)` centers it and hides the spacers), the surface caps at
  `min(900px, 100dvh - 96px)` and lays out as a flex column, and the part marked
  `.modal__scroll` scrolls inside it with `overscroll-behavior: contain` so the page
  behind never chains. Chrome outside that region (a close X, a head) stays put while
  the content moves. Opt-in: form dialogs are short and size to their content. The movie
  detail modal is the capped one — its close X is pinned to the surface, so it holds its
  corner while the backdrop scrolls under it.

Destructive confirms (`DeletionDialog`) use the same `Modal`: dismissing (Esc / veil /
Cancel) is the safe choice, so outside-click dismiss is intentional; only the explicit
`.btn--danger` confirms.

---

## 6. Motion & accessibility

- **Duration scale (tokens, §2):** feedback `--dur-fast`, state `--dur-base`, entrance
  `--dur-slow`, the hero draw-reveal `--dur-reveal`. Pointer feedback uses the fast
  token; exits use `--ease-exit` and run one step faster than their enter. No
  bounce/elastic. Motion conveys state, not decoration. Hardcode no new durations —
  reach for the scale.
- **CSS-first, with one sanctioned exception.** Visual motion (entrances, state
  changes, overlays, charts) is CSS keyframes (prefixed `mg-`) + transitions — do not
  reach for a general-purpose animation library (framer-motion / gsap / etc.) for those.
  The one exception is **animated number transitions**, which CSS can't do for arbitrary
  formats. NumberFlow is a Web Component that measures glyph geometry on mount, so it is
  scoped to the two above-the-fold counts where the roll actually reads: the **KPI strip**
  and the **"Films in Filter View"** heading count use **NumberFlow** (`@number-flow/react`,
  via the `StatNumber` / `MovieCount` / `RuntimeCount` wrappers in `StatsTab.tsx`), tuned to
  the MG scale (`NUMBER_TIMING` = `--dur-slow` duration + `--ease`, no bounce) and honoring
  `prefers-reduced-motion` (it renders instantly). The below-fold panel counts — the
  leaderboard + weekday bar values, the genre-donut legend, and the people-rail counts —
  render as **static text**: they only ever rolled on a window/filter change, never on
  mount, so on a page visit they animated nothing while ~45 NumberFlow elements mounting at
  once cost a ~50ms main-thread block. They now sit in scroll-gated panels
  (`content-visibility` + an IntersectionObserver mount gate) that only render as they near
  the viewport. The dense hourly + decade axis counts stay static too (too many to roll at
  once reads as noise). The KPI strip counts up from 0 on mount (`animateOnMount`, matching
  the bars' from-0 entrance); the "Films in Filter View" count stays static on mount and
  rolls only on change. The stats query keeps the previous result
  (`placeholderData: keepPreviousData`) so an uncached filter change rolls in place
  instead of blanking to "Loading stats…" and remounting. Any new motion must still
  degrade to an instant state under RM. **Alignment gotcha:** `<number-flow-react>` is
  `display:inline-block; line-height:1` with internal vertical mask padding
  `round(0.25em/2,1px)`. The mask is **visual-only** — it does *not* move the glyph
  baseline — so a number used **inline with text** (the "Films in Filter View · N" title,
  the "avg 1h 53m" runtime sub) baseline-aligns on its own at every `:root` zoom step;
  leave it at `vertical-align: baseline` and add no nudge. The one place that needs a fix
  is the **KPI value cell** (`.statitem__val`, `align-items: flex-end`): `flex-end` aligns
  box *bottoms*, not baselines, so the mask padding lets the numeral float ~4px above where
  the plain-text values (Top adder / Busiest day) land. Compensated with
  `.statitem__val number-flow-react { margin-bottom: -4px }` — at the fixed 29px value size
  the padding is exactly 4px CSS-px and zoom-invariant. (A previous attempt also added
  `vertical-align` lifts to the inline title/sub; that pushed those numerals *off* the text
  baseline and was reverted — the trap is measuring the box, not the glyph.) Standalone
  number-flows (`.b-val`, donut legend, people-rail counts) have no adjacent text and need
  nothing.
- **Stats rail motion (on filter change) — FLIP.** The three rails — the films "Films
  in Filter View" rail, the people rails (directors/actors), and the "Added by member"
  leaderboard — animate by how each item's box *actually moved* between the old and new
  layout, via `useFlipRail` (`hooks/useFlipRail.ts`), not a blanket fade-up replay:
  - an item whose position is **unchanged** gets a zero delta and never moves — so the
    30d films that are a prefix of the 1y set stay dead still (the "skip the overlap"
    case is free, not special-cased);
  - an item that **moved** (a rerank, or survivors closing the gap after a removal)
    **glides** to its new spot (a JS-driven `transform: translate` released under
    `transition: transform --dur-base --ease`);
  - a **new** item pops in with `mg-fadeUp` (`data-flip-enter`, per-newcomer stagger
    capped at 12 × 40ms);
  - a **dropped** item fades out in place (`data-flip-exit` → `mg-fadeOut`, all drops
    batched onto one `exitDelayMs` timer so the gap closes in a single clean glide),
    then unmounts.
  Whether an item is *new* is decided by key membership in the previous render
  (`prevKeys`), never by whether a position happens to be recorded — so a churned/late
  node stays put instead of wrongly fading in. Positions are measured **container-
  relative**, so a reflow ABOVE a rail (the films rail tripling in height) slides the
  whole rail without animating every card; and deltas are divided by `effectiveZoom`
  (`moviepickarr/zoom.ts`) so the glide lands exactly on target under the `:root` zoom ramp
  (§13). No React remount, so NumberFlow counts keep rolling; reduced-motion skips every
  transform/entrance and drops exits instantly.
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
  both reduced-motion-guarded to `animation: none`. The **movie-modal hero** is the
  wide twin (`HeroBackdrop` in `MovieModal.tsx`): the procedural `backdropBg(hue)`
  duotone is painted underneath as the instant first frame, so a slow TMDB CDN fetch
  never flashes the surface through (pure white in light mode) — then the real backdrop
  `<img>` crossfades in (`.moviemodal__hero__img` / `--loading` / `__shimmer`).
- **Skeletons:** the detail modal lazy-loads its heavy fields (overview, credits, cast
  row) from `GET /movies/:id`; while that's in flight they render `Skeleton`/`SkeletonText`
  shimmer blocks (`.skel`, reusing `mg-shimmer`) instead of popping in all at once —
  gated on the query being *pending AND* the field absent, so a full (pool) payload shows
  its data immediately and a genuinely empty field renders nothing, not a perma-skeleton.
  Each placeholder holds the space its real content will take, so nothing grows under the
  reader: the credit rows are reserved at one line height each
  (`.moviemodal__credits__ghost`, `height: 1lh`), not at the height of the skeleton bar.
  Both halves of the block are themed (`--skel-bg`, `--skel-sweep`), and both flip: on a
  dark page the block sits *above* the surface under a white sweep, on a light page it
  sits *below* under a dark one. It has to work that way because this is the one shimmer
  that sweeps the page's own surface. In light, `--surface-2` is 0.005 off `--bg`, so the
  old fixed pair left the whole loading state invisible (#219). The sibling shimmers
  (poster, avatar, modal hero) keep their hard-coded white: they sweep an opaque duotone
  or initials gradient, which reads the same in both themes. One wrinkle, since the cast
  card composes both classes: `.castcard__photo` declares its own frame background later
  in the file, so `.castcard__photo.skel` restates the fill to win the tie.
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
- **Roving tabindex where the run is unbounded, plain tab stops where it is a handful.**
  A wall of films is as long as somebody's stash, so it gets one tab stop and arrow keys
  (`nextCell` in `stashWall.ts`; left/right step through the list, up/down step by a row,
  Home/End go to the ends). A rail of six member rows stays six ordinary tab stops. The
  Members page runs both side by side and is the pattern. A roving list is a **list**, not
  a `role="grid"`: the Members wall's column count is a container-query artifact of the
  cell width, so grid coordinates would be announcing the stylesheet. The column count is
  read back off the resolved `grid-template-columns` rather than computed in JS, since a
  JS pixel and a CSS pixel are different sizes under the root zoom ramp (§13). The roving
  index resets to the first cell on any content change — a filter, a switch of subject —
  which also decides where Tab out of the field above it lands.
- **Focus moves only when the thing it is sitting on goes away**, and then to the nearest
  thing that is still there: the item taking the vacated index, else the one before it,
  else the region's heading (focusable at `tabIndex={-1}`, never a tab stop). Removing a
  focused node fires no blur, so the recovery keys on focus having reached the document
  body with the region still believing it holds focus — a click that moved focus somewhere
  real is a departure and is left alone. Where the move is a mutation, note the vacated
  index in `onMutate` and land focus when the item is actually gone from the list, not
  when the request returns: the two are separate round trips and SSE routinely wins.
- **A live region announces the part worth interrupting for, not the whole line.** Where
  a status line mixes facts that change at different rates, split it: the visible span
  carries every clause and stays silent, and a second `.vis-hidden` `role="status"`
  carries only the clauses that change what the page's controls will do. The Members
  status line is the pattern (`membersStatus` in `poolLock.ts` composes both strings and
  documents the split): occupancy ticks on every other member's promote arriving over
  SSE, so a single region over all three clauses would re-read the string each time,
  while round and draw state is rare and worth hearing. `.vis-hidden` is the app's one
  off-screen utility and must stay the clip-rect recipe: `display: none` and
  `visibility: hidden` both pull the node out of the accessibility tree, which turns any
  region wearing it into dead code that still looks correct in the markup.

---

## 7. The Hero (static-layout contract)

The Movies-tab hero must be **dimensionally static across draws**: changing the draw, its
metadata, the title length, or the tagline length must not move any component or resize
the banner. (Static across *draws*, not across *viewports*: the geometry steps by
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
  links, in white-on-backdrop. Links derive from ids present on every enriched draw,
  so the row's presence is consistent draw-to-draw and does not break the contract.
- The hero stays a **dark "island"** in light theme by design (cinematic scrim over a
  backdrop). Its scrim/text are intentionally dark/white in both themes; only fix the
  genuinely-broken light overlays, not the hero's darkness.

**Draw reel (slot-machine spin).** A draw first plays a slot-machine **reel**: a
takeover overlay *inside* the hero footprint (`DrawReel.tsx`, `.drawreel*`) that scrolls
a strip of pool-candidate posters past a centre reticle, decelerates onto the
server-chosen winner, then **settles and holds** — handing off to the reveal only on
confirmation (see Draw confirm). It animates a result the
**server already decided** (`ORDER BY RANDOM()`), so the randomness stays honest — the
reel only adds anticipation. Only **pool candidates** scroll (every tile is a real
possibility; never the watched library), and the strip is deduped at the landing seam so
no poster sits beside an identical copy of itself. Motion is the **measure-then-transition**
idiom (§6): JS measures the winner tile and glides the track there with a CSS transition
over `--dur-spin` (6.5s) / `--ease-reel` (easeOutCubic — a higher-order ease-out whose
deceleration tapers off so the reel floats to a stop rather than braking at a constant rate;
still short of `--ease-reveal`'s expo, whose front-loading would stall a multi-second spin
within ~1s), with a within-tile **jitter** so the landing feels live. The `movie:drawn` SSE event drives it so **every connected client
spins**, not just the clicker; it skips for a **pool of one** or under reduced motion
(straight to the reveal) and **resumes** server-relative on a reload while the draw is
still unrevealed, while the hero **holds its commit** so the reveal never fires mid-spin.
The **pool holds the winner** for as long as the draw is unrevealed: the draw flips the
row to `current` immediately, so the server hands the movie back in every pool read
(`Pooled` / `PooledByUserID`, `withHeldDraw`) until the reveal. Without that, a reload
mid-spin — or any client opening the board during a draw — fetched the post-draw pool and
the missing tile gave the winner away behind the reel. The client-side pool hold (the draw
machine releasing the pool on land) still covers cached clients; the two agree. While the
draw is held the **pool is frozen**: demote and delete are refused for every pool tile
(the held winner included, so no answer singles it out) and the board's demote control goes
inert in place, named "Move back to stash, a draw is in progress" on all three tiles alike;
the stash is unaffected, so promoting stays live through a draw.
The reel is a **pure reducer + store**: `drawMachine.ts` folds `movie:drawn` /
`movie:revealed` / scroll-done / confirm / tick events into `[state, commands]`, and
`drawStore.ts` (a `useSyncExternalStore` singleton) runs the effects. `Hero.tsx`,
`DrawReel.tsx`, and `useSSE.ts` all read the one store, so every surface agrees on the
draw state.

**Reel remounts resume, they don't replay.** The reel is rendered by the Hero, which lives
on the Movies tab, so switching tabs unmounts it and switching back mounts a new one against
the same draw. Scroll progress is component state and dies with it, so the elapsed time lives
on the spin descriptor instead (`startedAtMs`, stamped from `DrawEnv.now`), and every mount
asks `reelResume(spin, phase, now)` where to pick up: the whole scroll for a fresh draw, only
the time that's left mid-scroll, and no scroll at all once the phase is past `spinning` (the
track snaps onto the winner and the confirm is up immediately). Without that the reel replayed
the full 6.5s on every return, which ate the confirm window and let the server's reveal
deadline close the reel mid-replay, so the OK never appeared.

**Draw confirm (hold-and-reveal).** The settled reel does **not** auto-close. It waits
for confirmation, so the group sees the result land together. Only the **drawer** (the
client whose stable `mp-client-id` matches the draw's `drawClientId`) gets an **OK**
button; its fill doubles as a countdown, and it runs to the **server's reveal deadline**
(`spin.deadlineAtMs`), not the `--dur-confirm` token (which is only the
fallback/preview default). The deadline is an **instant, not a length**: every draw
payload carries `revealAt` and the `serverNow` it was stamped with, and the client
anchors `revealAt − serverNow` to the moment the payload arrived, so client clock skew
never enters. The bar reads what's left when it appears rather than a fixed per-draw
length, because it doesn't always start at the same point in the draw: **Skip** lands
the reel early and a tab switch can mount it late, while the deadline never moves. Both
of those used to leave the bar visibly out of step (a skip finished it ~5.6s early, a
tab switch restarted it with only ~7s to go). Pressing OK (or letting it fill) confirms, which
`POST`s `/movies/current/reveal`; the server flips the draw to `revealed` and broadcasts
**`movie:revealed`**, so **every** client's reel closes and reveals in lockstep. The
**auto-reveal is server-owned**: the movie service arms a timer at `revealAt`
(`DefaultAutoRevealDelay` 16.5s) and, if no client confirms first, reveals the draw itself
and broadcasts the same `movie:revealed`, so a reel never hangs if the drawer leaves,
without each client racing its own timer. The close is **flash-free**: the winner backdrop
(preloaded during the spin) is decoded while the reel still covers the hero, then the reel
drops and the reveal commits in one batched frame — no placeholder leaks through. A reload
keys off the server `revealed` flag (not a timer): unrevealed re-opens the settled reel
(the drawer keeps OK, since the client id persists), revealed shows the result directly.

**Draw sound.** As the reel starts, a Wheel-of-Fortune **click train** plays:
an original, fully **synthesized** sound (no audio file ships, so nothing to
license), each tick a short bandpass-filtered noise burst on the native **Web
Audio API** (no library) in `web/src/lib/sound.ts`. The clicks are **synced to the
posters**: `DrawReel` computes the exact instant each gap crosses the reticle (it
inverts `--ease-reel` per gap, `reelEaseTimeAt`) and hands the offsets to
`playDrawJingle`, so the ticks land on the gaps you see and decelerate on the same
curve — then go quiet as the winner coasts under the reticle (no gaps left to
tick). A `SYNC_OFFSET_MS` trims the fixed audio↔compositor lag by ear; the
relative timing is exact. The `AudioProvider` owns the on/off and 0..1 volume prefs
(localStorage `mp-sound` / `mp-volume`) plus a one-time autoplay **unlock** (an AudioContext
resume) on first interaction, so SSE-driven clients that never click Draw can still play. It mounts with the app
chrome (`AppLayout`), not at the root, so the login and claim screens never build an audio graph they can't use. The draw-sound control (`VolumeControl.tsx`) lives inline in the profile panel's
Preferences section: a mute toggle, a volume slider with its percentage, and a
play/stop button to audition the (fallback) wheel. A fresh draw
always sounds; a reload-resume joins only if audio is already running (a cold
reload's context is suspended, so it's visually in-sync but silent), and reduced
motion (no reel) is silent for free.

**Draw-reveal (motion).** When the draw changes (or clears), the banner reveals the new
state without breaking the static contract: a two-layer `.hero__bg-stack` crossfades the
incoming backdrop (preloaded + `decode()`d in `Hero.tsx` so it never flashes blank) with
a slow settle-scale (`mg-bgReveal`), and the poster + each text slot fade-settle in,
lightly staggered via `--i` (`mg-revealDraw`). It is **transform/opacity only**: the
reserved slots keep their boxes, so nothing reflows. The content is keyed on the draw id
so the reveal replays per draw; reduced-motion collapses it to an instant swap. The Draw
/ Mark-Watched buttons show in-flight state (`Loader2Icon` + "Drawing…" / "Marking…").

Verified static: empty vs populated and a 173-char injected tagline all leave
`eyebrowTop`, `meta top`, `actions top`, and hero height identical.

On phones (≤700px) the hero drops the fixed height and stacks: a 120px poster above
the eyebrow → title → tagline → meta → actions, full width. The static-layout contract
governs the desktop banner only. The rest of the responsive system lives in §13.

On large screens (≥1728px) the hero steps *up*: `--hero-body-h` grows (18.5rem → 21rem,
and the poster width tracks it via the `calc()` above), `.hero__inner` padding opens up,
and the title clamp ceiling rises (54 → 64px) — so the centerpiece feels grander, not
merely bigger. Like the phone stack this is per-breakpoint geometry, not a break of the
contract: within the breakpoint the banner stays dimensionally static as the draw changes.
The whole hero also rides the global `zoom` ramp (§13) on top of this step.

---

## 8. Empty states

All "nothing here" / placeholder copy uses the single `.empty` class (centered,
`--ink-2`, 32px vertical padding). Do not hand-roll per-tab padding/alignment.

On the **stats** tab the empty state is anchored to the films-in-filter-view rail
(it is the expansion of the "In window" KPI): an empty filter view collapses the whole
body to that one rail placeholder rather than showing zeroed leaderboards and charts.
The member leaderboard and activity charts therefore render only when the filter view
actually has movies.

---

## 9. Copy & microcopy

- **Buttons:** verb + object, sentence case ("Save changes", "Add user", "Mark as
  watched", "Draw random movie", "Lock pool"). Only the object keeps any proper-noun
  capital ("Add to Felix's stash"). Nav tabs are the deliberate uppercase exception.
- **Error toasts:** one voice, "Failed to <verb> <thing>" ("Failed to update movie").
  Not "Error <x>ing".
- **Success toasts:** terse, past tense, no exclamation ("Marked as watched",
  "<title> updated"). The draw itself shows no toast: the reel is its feedback.
- **Loading labels:** present participle + ellipsis ("Searching…", "Adding…").
- **Modal headers:** sentence case ("Edit movie", "Delete movie").
- **Pluralization:** use `plural(n, noun, pluralForm?)` from `lib.ts` for any
  count + noun, so "1 movie" / "2 movies" (never "1 movies"). Covers films, people, etc.
- **No em dashes** anywhere in UI copy. Use periods, commas, colons, or parentheses.

---

## 10. Iconography (lucide)

- `PlusIcon` = "add / open the add flow" (board "Add to <name>'s stash" button, "Add user").
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

- React 19 + Vite 7 + Tailwind **v4** (`@tailwindcss/vite`), TanStack Query +
  **TanStack Router** (code-based, no route-gen plugin), **TanStack Virtual** (the
  watched grid and the filter menus, §4), Sonner, lucide. **Vitest**
  for unit tests, in two projects: `node` for pure reducers and helpers
  (`drawMachine`, `sseConnection`, `sseInvalidations`, `sseInvalidationQueue`,
  `search`, `useGridMetrics`) and `dom` (jsdom + Testing Library,
  `*.render.test.tsx`) for behaviour that only exists once a component renders,
  such as the reel's remount resume. The pure seam stays the first choice; render
  tests are the fallback when there isn't one.
  No Radix/shadcn. Package manager: **bun** (`web/`).
- Tailwind v4 has no config file; theme/aliases live in `index.css` (`@theme inline`).
- Gate before handoff: `bunx tsc -b` + `bun run lint` + `bun run test` (vitest) +
  `bunx vite build` (all from `web/`). The editor's inline TS diagnostics often fail to resolve the `@/*` path
  aliases and show false "cannot find module" errors; trust `tsc -b`, not them.
- Backend: Go (Fiber) on `:3030`, serves the embedded `web/dist` and `/api/v1/*`.
  Run with `web/dist` built (`go run main.go` re-embeds the current dist at compile).
- **Dev data path:** `APIClient.baseURL()` and `useSSE`'s `baseURL()` return `""` in
  DEV, so API + SSE calls hit `/api/...` **same-origin** and ride the Vite proxy
  (`vite.config.ts`) → `:3030`. Same-origin means no preflight, no CORS — data loads
  on `:5173` directly. (Fixed in `59f2f80`; the base used to hardcode `:3030`, which
  the browser blocked as a cross-origin/CORS call.) Empty data on `:5173` ⇒ the Go
  backend isn't up, the proxy `target` is wrong, or the DB is empty — not CORS.
- **SSE invalidation coalescing.** Every key `useSSE` invalidates (per-event rows
  and the reconnect resync alike) goes through `sseInvalidationQueue.ts`: keys land
  in a set and flush once per distinct key after a 50ms window measured from the
  first key. A bulk add/move/delete emitting one event per item costs one refetch
  of the board and pool lists instead of one per item; a lone event pays the 50ms
  and nothing else. The window never re-arms mid-burst, so a sustained stream still
  flushes on time, and the queue schedules nothing while idle. The pool hold (the
  draw reveal, §4) moved to the flush callback: a spin can start inside the window,
  so the phase has to be read when the invalidation actually fires.
- **Routing & URL state (TanStack Router).** Three path routes — `/` (Movies),
  `/stats`, `/users` — defined code-based in `router.tsx`. The root route is the app
  shell (NavBar + `<Outlet/>` + Toaster) and owns the single `useSSE()` mount, so the
  stream opens once and survives tab navigation rather than reconnecting per switch;
  `NavBar` derives the active tab from the router and renders `<Link>`s. The **Stats
  tab keeps its entire filter state in typed URL search params** (`statsSearch.ts`:
  window, custom range, genre, actors/crew/adders id-lists, release year/decade).
  `validateStatsSearch` is a total, never-throwing validator (caps id lists at 25,
  sorts + de-dupes to a canonical form, parses custom-range dates as *local* `ymd`)
  and `stripSearchParams` trims defaults so the default view stays a clean `/stats`.
  Net: every filter is shareable, bookmarkable, and survives reload + back/forward,
  and the genre/year chips in the hero + movie modal (`MetaChips`) deep-link straight
  into a pre-filtered `/stats` (genre → `?genre=`, release year → `?year=`).
- **Route code splitting.** Every route except the movies landing page loads its
  component through `lazyRouteComponent`, so `/stats`, `/users`, `/admin`,
  `/settings`, `/login` and `/claim/$token` each ship their own chunk (the
  Shell-wrapped ones live in `src/pages/`). Because the router owns the import,
  `defaultPreload: "intent"` warms the chunk on nav-link hover, so the click still
  paints without a round-trip. Movies stays eager: it's the landing route, where a
  deferred chunk would only delay the first paint. Alongside that, `vite.config.ts`
  pins react/react-dom/scheduler into `react-vendor` and everything under
  `@tanstack` into `tanstack` using `manualChunks`' **function** form. The array
  form matches bare specifiers, so it missed the `react-dom/client` the entry
  imports and react-dom leaked into the app chunk.
- **Backend SPA fallback.** The Fiber file server uses `NotFoundFile: "index.html"`
  so a hard refresh or shared deep-link on a client route (`/stats`, `/users`)
  resolves to the SPA instead of 404ing, with an `/api` JSON-404 catch-all above it
  so unknown API paths still return `application/problem+json`, not the SPA shell.

---

## 13. Responsive & touch

Desktop-first, with a dedicated phone/touch pass below and a large-screen scale-up above.
Breakpoints are content-driven but drawn from one tidy scale — **640 / 700 / 760 /
900** down, **1728 / 2240 / 2560 / 3200 / 3840** up — documented inline in `index.css`
(the "Responsive — phone & touch adaptations" and "Large-screen scale ramp" blocks).
What each does:

- **640** — the *phone* breakpoint. Section headers (`.sec-head`) stack to a title row + full-width controls;
  user boards go single-column; the stats window control spans the row and the
  added-by-member bars shrink their name column (`188px → 112px`) so the track keeps
  width; poster grids become denser thumbnail grids; modal chrome tightens.
- **700** — the hero stacks (poster above text), page / top-nav padding tightens, and the
  movie modal's rail becomes a row with its credit columns stacked (§4). It replaced the
  old 560 stack point when the modal went from 560px to 880px wide.
- **760** — the stat strip drops 3 columns → 2.
- **900** — navigation moves to a fixed **bottom tab bar** (below); the stat strip drops
  6 columns → 3, and the stats two-column sections (weekday | hourly, genres | decades)
  collapse to one.

**Large screens.** A mirror of the phone pass, scaling *up*. Above the 1240px column the
whole UI steps up through a discrete root `zoom` ramp (`:root { zoom }` at 1728 / 2240 /
2560 / 3200 / 3840px) so type, posters, modals, menus, grids and the column all grow
together — keeping the centered cinematic composition instead of leaving content adrift on
a 2K/4K panel. It lives on `:root` (not `.app`) because modals, toasts and the portalled
`Menu` (the "more actions" surface, which must escape its row's scroll clip) reach `<body>`;
only a root-level scale reaches them. The top steps carry a `min-height` guard
so ultrawide-but-short panels (e.g. 3440×1440) don't over-scale. Stepped, not fluid
`clamp` — the discrete-scale ethos (§3) holds. **Overlay placement under the ramp:** any
overlay positioned by JS from `getBoundingClientRect` and written as inline `top/left` on a
`position:fixed` portal child drifts under the ramp — the zoomed viewport coords get scaled
by the inherited zoom a second time. So prefer CSS-anchored overlays (the stats filter
dropdowns and the `DateRange` popover anchor to their trigger in CSS, sharing its space and
riding the ramp for free); where a portal is unavoidable (the row `Menu` escaping its
`overflow` clip), divide the GBCR coords by the element's `currentCSSZoom` before writing them. The **hero** takes an extra large-screen
step on top of the zoom (taller `--hero-body-h`, roomier padding, a higher title ceiling)
so the centerpiece feels grander, not merely bigger (§7).

**Bottom tab bar.** Below 900px the top-bar tabs (`.nav__tabs`) hide and a fixed
`.navbar-bottom` renders the tabs (4 for an admin, Roster included) in thumb reach; the
wordmark (a home control: click it to return to the Movies tab from any tab) + the profile
avatar stay in the top bar. The active tab is gold-tinted — there is no sliding underline
there (the `.tab__ink` slider is desktop-only, and `NavBar`'s `measure()` skips when the top
tabs are `display:none`). The bar carries `padding-bottom: env(safe-area-inset-bottom)`, the
`.shell` reserves clearance for it, and `index.html` sets `viewport-fit=cover`; the
bottom-right toaster is lifted above the bar at the same width.

The 900 is a clearance number, not a device class: nothing in `.nav__inner` shrinks, so once
the top row stops fitting, the avatar (and with it sign-out, theme, volume, preferences) is
pushed off the right edge with no way to scroll to it. Measured against the fixture data the
row runs out of room at 794px for an admin and 677px for a member, so 900 clears both with
slack for longer labels. A fifth tab moves the number again; the top bar is never made to
compress instead, since that trades a loud break for a quiet one.

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
8. Responsiveness is structural (bottom nav below 900, single-column reflows below 640; a discrete
   root `zoom` ramp scales the whole UI up on large screens — §13), never per-element fluid
   type. Every hover-revealed action needs a `hover: none` fallback so touch users can
   reach it (§13).
