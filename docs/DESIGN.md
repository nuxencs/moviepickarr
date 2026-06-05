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

**Motion:** `--ease: cubic-bezier(0.22, 0.61, 0.36, 1)` (ease-out, no bounce),
`--dur: 0.28s`. Shadows: `--shadow` (deep, floating surfaces), `--shadow-sm`.

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

Rules: fixed rem/px scale (not fluid) for product UI; weight contrast for hierarchy;
sentence case for section/modal headings (nav tabs are the deliberate uppercase
exception). The mono uppercase tracked "eyebrow" (`.eyebrow`) is a real label element
(timezone, "Pool", "Next picker"), not a decorative kicker on every section.

---

## 4. Component vocabulary — ONE system

All interactive UI uses these MG classes (defined in `index.css`). This replaced the
old shadcn primitives.

- **Buttons:** `.btn` + `.btn--accent` (primary gold), `.btn--ghost` (secondary,
  translucent surface-3 fill), `.btn--danger` (destructive), `.btn--sm` (small).
  Hover lifts (`translateY(-1px)` + brightness); `:active` presses. Verb+object labels.
- **Inputs:** `.field` — a 42px filled wrapper with a leading icon and gold
  focus-within border. All text inputs use this; there is no bare/hollow input style.
- **Icon buttons:** `.iconbtn` (34px), `.iconbtn--danger` for destructive.
- **Segmented control:** `.seg` (+ `.seg--accent` for the active-gold variant).
- **Poster:** `.poster` — the one container; real TMDB art or a deterministic
  procedural duotone (`lib.ts`), with a rating badge.
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
- The Radix dropdown (`ui/dropdown-menu.tsx`, used by the user "more actions" menu) is
  kept for its a11y but restyled to MG (`border-line-2`, `[box-shadow:var(--shadow)]`).

`web/src/index.css` has an `@theme inline` block that remaps shadcn `--color-*` /
`--radius-*` aliases onto MG tokens, so any remaining Radix primitive (the dropdown)
inherits MG colors/radius automatically.

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

- Durations 150–280ms, ease-out, no bounce. Motion conveys state, not decoration.
- **`prefers-reduced-motion`**: a global block neutralizes all animations/transitions
  and the infinite hero "live" pulse. Any new keyframe must survive this (it already
  does via the universal rule). Reduced motion is not optional.
- **Focus:** global `:focus-visible` is a 2px gold outline. Every focusable element
  must render a box (do NOT use `display: contents` on a focusable trigger — it leaves
  the outline nothing to draw on).
- **Hover-revealed controls** (row actions, etc.) must also reveal on `:focus-within`
  so keyboard users can reach them. Card/tile hover effects must also fire on
  `:focus-visible`.

---

## 7. The Hero (static-layout contract)

The Movies-tab hero must be **dimensionally static**: changing the pick, its metadata,
the title length, or the tagline length must not move any component or resize the
banner. How it's built (`Hero.tsx` + `.hero__*` in `index.css`):

- `.hero__body` is a fixed-height flex column (`min-height: 18.5rem`), sized for the
  worst case so the banner never resizes.
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

Verified static: empty vs populated and a 173-char injected tagline all leave
`eyebrowTop`, `meta top`, `actions top`, and hero height identical.

Mobile is intentionally exempt from the fixed height (handled in the `max-width: 700px`
block); responsive polish is a separate pass.

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

- `PlusIcon` = "add / open the add flow" (board add button, empty pool slot).
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

- React 19 + Vite 7 + Tailwind **v4** (`@tailwindcss/vite`), TanStack Query, Radix
  (dropdown only), Sonner, lucide. Package manager: **bun** (`web/`).
- Tailwind v4 has no config file; theme/aliases live in `index.css` (`@theme inline`).
- Gate before handoff: `bunx tsc -b` + `bun run lint` + `bunx vite build` (all from
  `web/`). The editor's inline TS diagnostics often fail to resolve the `@/*` path
  aliases and show false "cannot find module" errors; trust `tsc -b`, not them.
- Backend: Go (Fiber) on `:3030`, serves the embedded `web/dist` and `/api/v1/*`.
  Run with `web/dist` built (`go run main.go` re-embeds the current dist at compile).
- **Dev gotcha:** `APIClient.baseURL()` hardcodes `http://localhost:3030` in DEV, so
  the Vite dev server (`:5173`) makes a cross-origin call that the browser blocks
  (CORS) and renders empty. To see real data while developing, view on `:3030` (build
  + run the Go server) or fix the dev base to use the Vite `/api` proxy.

---

## 13. Guardrails (the short list)

1. One design language. No shadcn `Button`/`Input`/`AlertDialog`; extend `.btn`/
   `.field`/`.modal`.
2. All dialogs/confirms use the bespoke `Modal`.
3. Body/meta/placeholder text must clear 4.5:1 in both themes (`--ink-3` is the floor).
4. Keep `prefers-reduced-motion` working; every focusable has a visible focus box;
   hover-revealed controls also reveal on focus.
5. The hero stays dimensionally static (§7); 3+ line taglines truncate.
6. Copy: verb+object buttons, "Failed to X" errors, `plural()` for counts, no em dashes.
7. Gold = action/selection/state only. Danger = the single `--danger` ramp.
