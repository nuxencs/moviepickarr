# Research: adopting shadcn for the `web/` rebuild

Resolves [#394](https://github.com/nuxencs/moviepickarr/issues/394). Child of
the map [#392](https://github.com/nuxencs/moviepickarr/issues/392). shadcn as
the one base is decided. This file records what adoption involves.

State as of 2026-10-31:

| Package | Version | Source commit read |
| --- | --- | --- |
| `shadcn` CLI | 4.23.0 | `shadcn-ui/ui` `24a1642` |
| `@base-ui/react` | 1.9.0 | `mui/base-ui` `4e89b3a` (newer than 1.9.0; behavior tests used npm 1.9.0) |
| `radix-ui` / `@radix-ui/react-dialog` | 1.8.1 / 1.3.1 | `radix-ui/primitives` `eb3a2b7` |
| `@tanstack/react-form` | 1.33.5 | `TanStack/form` main |
| `sonner` | 2.0.8 | `emilkowalski/sonner` `8e4662b` |
| `tw-animate-css`, `cn`, `zod` | 1.4.0, 0.5.1, 4.6.7 | npm only |

Claim labels: **[source]** is read from upstream code or docs. **[tested]** is
measured in a real browser or with the real CLI (see
[Method and limits](#method-and-limits)). **[repo]** is read from this repo.

## Recommendation (summary)

1. **Primitive layer: Base UI.** It is the shadcn default since 2026-07-02. The
   deciding reason is ADR 0008, not the default. `Dialog.Backdrop` and
   `Dialog.Viewport` are siblings, and `modal="trap-focus"` keeps the focus trap
   without a library scroll lock. Radix's scroll pattern puts `Content` inside
   `Overlay`, and its `modal` prop is one boolean.
2. **Never run `shadcn init`, `shadcn apply`, or a preset here.** `init` replaced
   the gold `--accent` with `oklch(0.97 0 0)` and changed about 90 lines in
   `index.css` [tested]. Edit `components.json` by hand, then use only
   `shadcn add`. `add` changed 0 lines in `index.css` for 13 components [tested].
3. **Keep the token layer.** The `@theme inline` block already serves
   the shadcn utilities. Add a small set of raw `:root` aliases and the
   missing radius steps. The gold `--accent` can stay: the registry never reads
   `var(--accent)` [source].
4. **Overlays can respect ADR 0008, with three owned-source edits.** Add
   `Dialog.Viewport`, keep `lockPageScroll`, and keep the `mg-*` backdrop
   keyframes. The stock shadcn dialog meets none of these three.
5. **Overlays can replace ADR 0009.** Base UI presence replaces `useDismissible`
   for all five users. An ADR must supersede 0009. Delete `useDismissible` and the
   module-level layer stacks. Keep `exitDelayMs` for `useFlipRail` only.
6. **Forms: `createFormHook` with pre-bound field components** over shadcn
   `Field`, `Input`, and related parts. shadcn has no adapter: you wire the props.
   No need for Zod to start.
7. **Upgrades are manual and read-only by default:** `shadcn add <c> --diff`,
   then merge by hand. `@base-ui/react` moves through Renovate.

## 1. Primitive layer

### What shadcn supports

- **Three bases:** `base`, `radix`, `aria`, chosen with `shadcn init --base`
  [source: [cli v4](https://ui.shadcn.com/docs/changelog/2026-03-cli-v4),
  [React Aria](https://ui.shadcn.com/docs/changelog/2026-07-react-aria)].
- **Base UI is the default** for new projects and the docs since 2026-07-02.
  **Radix is not deprecated:** "every update and new component will ship for both
  libraries (unless a component only exists in Base UI)" [source:
  [Base UI as the Default](https://ui.shadcn.com/docs/changelog/2026-07-base-ui-default)].
- `Toast` exists only for Base UI [source:
  [Toast](https://ui.shadcn.com/docs/changelog/2026-07-toast)].
- **This repo resolves to Radix today.** `style: "new-york"` reports
  `base: radix`, and `add dialog` plans `@radix-ui/react-dialog` [tested].
  `tailwind.config` points at a `tailwind.config.js` that does not exist [repo].
  shadcn says to leave it blank on Tailwind 4 [source:
  [components.json](https://ui.shadcn.com/docs/components-json)].

### Which to pick

Both packages are active (last commits 2026-10-30 and 2026-10-14). Both accept
React 19. The difference is in the overlay contract:

| Need in this repo | Base UI 1.9.0 | Radix 1.3.1 |
| --- | --- | --- |
| Focus trap without a library scroll lock | `modal="trap-focus"` | `modal` is boolean. `false` also drops the trap ([`dialog.tsx`](https://github.com/radix-ui/primitives/blob/eb3a2b7/packages/react/dialog/src/dialog.tsx)) |
| Veil scroll mode (ADR 0008) | `Dialog.Viewport`, a sibling of `Backdrop` | [Docs](https://www.radix-ui.com/primitives/docs/components/dialog): "Move the content inside the overlay", so the overlay becomes the dialog's ancestor |
| Exit completion | `onOpenChangeComplete`, from `element.getAnimations()`. Covers animations and transitions | `Presence` compares `animation-name` and waits for `animationend`. No completion callback on `Dialog.Root` |
| Reason for a close | `eventDetails.reason`, `eventDetails.cancel()` | Per-event props (`onEscapeKeyDown`, `onPointerDownOutside`) |
| Render composition | `render` prop | `asChild` |

**Decision: Base UI.** No Radix code exists in this repo
([DESIGN §4](../DESIGN.md)), so there is no migration cost. The
choice can be reversed one component at a time with the `shadcn/ui` skill
[source: Base UI as the Default]. The React Aria base was not assessed: it
is available, but nothing in this repo asks for it.

### Registry coverage

`dialog`, `alert-dialog`, `sheet`, `drawer`, `dropdown-menu`, `popover`,
`tooltip`, `select`, `combobox`, `field`, `input`, `checkbox`, `switch`, and
`radio-group` all exist on the Base UI side [source:
[`bases/base/ui`](https://github.com/shadcn-ui/ui/tree/24a1642/apps/v4/registry/bases/base/ui)].

### Style

The style is an install-time choice. `add` bakes it into class strings, so each
installed component is plain Tailwind [tested].

- **Use `base-nova`** unless the prototype shows a reason to change. Nova has 5
  `rounded-none`; Lyra has 129 [source: `registry/styles`]. Radius utilities map to
  our 2px to 6px tokens only when they are not `rounded-none`.
- **Control height does not match.** Nova `Button` is `h-8` and Vega is `h-9`. Our
  tokens are `--control-h-sm` 34px, `--control-h` 42px, `--control-h-lg` 46px.
  Edit the `size` variants in the owned `button.tsx` and `input.tsx`.

## 2. Theming with OKLCH tokens

### The model

shadcn defines `--background`, `--primary`, and so on in `:root` and `.dark`, then
maps them with `--color-background: var(--background)` in `@theme inline` [source:
[Theming](https://ui.shadcn.com/docs/theming)]. Tailwind's `inline` writes the
referenced variable directly into the utility [source:
[Tailwind theme](https://tailwindcss.com/docs/theme)].

Our block has the same shape with one level less: `--color-background: var(--bg)`
[repo: `web/src/index.css`]. Because utilities resolve through `var(--bg)`, both
themes work with no `.dark` token block. The repo's `.light` class and the
`:root, .dark` default stay as they are.

### What already maps

| shadcn utility | Our `@theme inline` value | State |
| --- | --- | --- |
| `background`, `foreground` | `--bg`, `--ink` | ok |
| `card`, `popover` | `--surface` | ok |
| `primary` | `--accent` (gold) | ok |
| `secondary`, `muted` | `--surface-2` | ok |
| `muted-foreground` | `--ink-3` | ok. Clears 4.5:1 per DESIGN guardrail 3 |
| `accent` | `--surface-3` | ok. This is the shadcn hover surface, not gold |
| `destructive` | `--danger` | ok. `--color-destructive-foreground` is now unused upstream |
| `border`, `input` | `--line` | Visual check needed. `dark:bg-input/30` applies 30% to an 8% white |
| `ring` | `--accent` | See [focus ring](#decisions-for-the-adr) |
| `radius-sm/md/lg` | `--r-sm/md/lg` | ok |
| `radius-xl` and up | missing | **Gap.** Nova dialog uses `rounded-xl`, which falls back to the Tailwind default |
| `chart-*`, `sidebar-*` | missing | Add only if `chart` or `sidebar` is installed |

### Raw variables

`@theme inline` covers utilities. It does not cover class strings or styles that
read a variable directly. The 13 components installed in the CLI test read these
[tested]:

| Variable | Read by | Action |
| --- | --- | --- |
| `--radius-md` | `button` | Theme variable. Tailwind emits used theme variables [source: Tailwind theme]. Confirm in the prototype |
| `--radius` | `sonner` | Add to `:root`: `--radius: var(--r-lg)` |
| `--secondary`, `--foreground` | `button` `secondary` hover | Add aliases |
| `--popover`, `--popover-foreground`, `--border` | `sonner` | Add aliases, or drop them in the owned `sonner.tsx` |

`var(--accent)` has **zero** hits in `registry/bases/base/ui` and
`registry/styles` [source]. So the gold `--accent` is safe at runtime, and no
rename is forced.

Proposed additions:

```css
:root {
  --radius: var(--r-lg);
  --foreground: var(--ink);
  --secondary: var(--surface-2);
  --popover: var(--surface);
  --popover-foreground: var(--ink);
  --border: var(--line);
}

@theme inline {
  --radius-xl: var(--r-xl);
  --radius-2xl: var(--r-xl);
}
```

Add only the steps that the installed source uses. Fonts stay open on the map, and
no installed Nova component reads `font-heading` [tested].

### Why `init` is banned

Real `init --preset nova --base base --force` on a copy of `web/` [tested]:

- `--accent` in `:root` changed from gold to `oklch(0.97 0 0)`. A new `.dark` block
  also set it. All 125 gold references in `web/src` would turn grey.
- Added unused token sets for light and dark, because `@theme inline` keeps our
  existing `--color-*` lines and reads none of them.
- Added `@apply` lines into our `@layer base` rules for `*`, `html`, and `body`.
- Added `@import "tw-animate-css"`, `@import "shadcn/tailwind.css"`, and
  `@import "@fontsource-variable/geist"`.
- Added runtime dependencies: `shadcn` (32 direct dependencies of its own),
  `@fontsource-variable/geist`, `tw-animate-css`.

Cause: `init` installs a style item. The CLI overwrites existing variables only for
`registry:theme`, `registry:style`, and `registry:font` items. Plain components
never overwrite [source:
[`add-components.ts`](https://github.com/shadcn-ui/ui/blob/24a1642/packages/registry/src/add-components.ts),
[`update-css-vars.ts`](https://github.com/shadcn-ui/ui/blob/24a1642/packages/registry/src/updaters/update-css-vars.ts)].
`apply` also updates "your theme, colors, CSS variables, fonts, and icons" [source:
[apply](https://ui.shadcn.com/docs/changelog/2026-04-shadcn-apply)].

### The hand-written setup

`components.json`:

```json
{
  "$schema": "https://ui.shadcn.com/schema.json",
  "style": "base-nova",
  "rsc": false,
  "tsx": true,
  "tailwind": { "config": "", "css": "src/index.css", "baseColor": "neutral", "cssVariables": true, "prefix": "" },
  "aliases": { "components": "@/components", "utils": "@/lib/utils", "ui": "@/components/ui", "lib": "@/lib", "hooks": "@/hooks" },
  "iconLibrary": "lucide"
}
```

The docs call `style` immutable after init. That matters only for installed
components, and this repo has none. `add` worked with this file [tested].

Steps that `init` would have done, and that `add` does not do [tested]:

- `bun add class-variance-authority`. `button` imports it, but the `add` dry run
  does not list it.
- Variants. Base UI sets presence attributes, and Tailwind `data-open:` matches
  presence [source: [Tailwind](https://tailwindcss.com/docs/hover-focus-and-other-states)].
  `shadcn/tailwind.css` (637 lines) should not be needed. Confirm in the prototype.
  If a variant is missing, copy that one rule. Do not take the `shadcn` package as
  a dependency for CSS.
- `tw-animate-css`. Skip it, see [motion](#exit-motion).

## 3. Forms with TanStack Form

- **No adapter exists.** The Base UI `field.tsx` is plain markup plus `Label`,
  `Separator`, and `cva`. It does not use Base UI `Field` [source:
  [`field.tsx`](https://github.com/shadcn-ui/ui/blob/24a1642/apps/v4/registry/bases/base/ui/field.tsx)].
- **The documented pattern** is `useForm` plus `form.Field`, with `data-invalid` on
  `<Field>`, `aria-invalid` on the control, and
  `isTouched && !isValid` as the error gate [source:
  [shadcn TanStack Form](https://ui.shadcn.com/docs/forms/tanstack-form)].
- **The docs are Radix-shaped.** The demo uses `new-york-v4`, and `<SelectContent
  position="item-aligned">` is a Radix prop. The Base UI equivalent is
  `alignItemWithTrigger`, on by default [source: `base/ui/select.tsx`]. Base UI
  `Select` has a nullable value (`defaultValue = null`) [source: `SelectRoot.tsx`].
- **`FieldError` takes objects:** `errors?: Array<{ message?: string } | undefined>`.
  Standard Schema issues fit. A plain validator that returns a string does not.
  TanStack docs use string validators, so return `{ message }` from ours or
  normalize in the wrapper [source: `field.tsx`,
  [TanStack validation](https://tanstack.com/form/latest/docs/framework/react/guides/validation)].
- **Zod is optional.** TanStack Form takes Standard Schema or plain functions. The
  shadcn page shows Zod v3; npm latest is 4.6.7. Zod is not a dependency here
  [repo]. Skip it until client validation needs it.
- **Server errors:** `validators.onSubmitAsync` can return
  `{ fields: { name: ... } }` to place API refusals on fields [source: TanStack
  validation].

**Recommendation:** avoid the render prop in 15 form files [repo]. Use
`createFormHookContexts` and `createFormHook` with pre-bound `TextField`,
`SelectField`, `SwitchField`, and `SubmitButton`. One wrapper then holds the
`aria-invalid`, `data-invalid`, and `FieldError` wiring [source:
[Form Composition](https://tanstack.com/form/latest/docs/framework/react/guides/form-composition)].
Do not add Base UI `Form` or `Field.Root`: TanStack owns the state, and Base UI
says its `<Form>` is not needed [source:
[Base UI forms](https://base-ui.com/react/handbook/forms)].

## 4. Overlays: what shadcn offers

| Surface today | Use | Primitive | Must-do |
| --- | --- | --- | --- |
| `Modal` (18 files) | `Dialog`, source edited | `Dialog` | See [section 5](#5-adr-0008-and-adr-0009) |
| `DeletionDialog` | `Dialog`, **not** `AlertDialog` | `Dialog` | `AlertDialog` forces `disablePointerDismissal` [source: `useRenderDialogRoot.tsx`]. DESIGN §5 keeps outside dismiss |
| `Menu` | `DropdownMenu` | `Menu` | Set `modal={false}`, `align="end"`, and remove `w-(--anchor-width)` |
| `FilterBar` dropdowns, `DateRange`, `ProfilePanel` | `Popover`, or `Select` for a plain choice | `Popover` | Default `modal` is `false`. They portal now, so their absolute positions go |
| Sonner `Toaster` | `sonner` | Sonner | Remove `next-themes`, see below |
| `title` hints (up to 49 `title={` matches) | `Tooltip` for visual labels only | `Tooltip` | Disabled on touch, see below |
| No current user | `Sheet` (a positioned `Dialog`), `Drawer` (Base UI `Drawer`, gestures) | `Dialog`, `Drawer` | [Dialog docs](https://base-ui.com/react/components/dialog): use Drawer for gestures |

- **Menu `modal`:** Base UI defaults to `true` [source: `MenuRoot.tsx`]. A modal
  menu ate the outside click (`outside 0`). `modal={false}` let it through and still
  closed the menu [tested]. The current `Menu` lets it through [repo: `Menu.tsx`].
- **Menu width:** `DropdownMenuContent` sets `w-(--anchor-width)` [source]. An icon
  trigger would make the menu icon-wide.
- **Tooltip:** "tooltips are disabled on touch devices" [source:
  [Base UI Tooltip](https://base-ui.com/react/components/tooltip)]. The `aria-disabled`
  refusal rule in DESIGN §6 needs the reason in the accessible name. Keep that. A
  tooltip can add to it but cannot carry it alone.
- **Toast:** the file `ui/toast.tsx` exists here. `add toast` would overwrite it
  [tested]. Keep Sonner for now: it is the working behavior, the `toast-api.ts` seam
  covers 16 files [repo], and the Base UI component is three months old.
  `add sonner` writes `ui/sonner.tsx`, which imports `next-themes`. Replace that
  with `useTheme` from our `ThemeProvider`, and do not install `next-themes`.
  The `.mg-toast` overrides are unlayered and `!important` (DESIGN §11), which the
  map's styling rule does not allow. Theme through Sonner's `classNames` with
  Tailwind utilities, or through its `--normal-*` variables as shadcn does.
- **Stacking:** Base UI's quick start asks for `isolation: isolate` on the app root
  [source: [Quick start](https://base-ui.com/react/overview/quick-start)]. Sonner's
  `aria-live` region stays exposed while a dialog hides outside content:
  `markOthers` skips `[aria-live]` [source: `markOthers.ts`].

## 5. ADR 0008 and ADR 0009

### Behavior against the current contract

Measured on Base UI 1.9.0 in Chromium and WebKit with this app's `html` and `body`
scroll CSS. Results match across both engines.

| Current contract (repo source) | Result | How |
| --- | --- | --- |
| Backdrop is never a dialog ancestor (ADR 0008) | **Pass** | Portal holds `backdrop`, then `veil > popup`. `filteredAncestors: []` |
| No filter on `.app` (ADR 0008) | **Pass** | Base UI adds none. Keep our rule |
| Lock in first commit (ADR 0008) | **Pass with `lockPageScroll`** | Layout effect inside `Popup` sees the popup in the DOM |
| Lock `data-page-scroll-owner` (ADR 0008) | **Fail stock, pass with `lockPageScroll`** | Base UI never touches them |
| Refcount across nested dialogs (ADR 0008) | **Pass with `lockPageScroll`** | Hidden after the child closes, released after the last |
| Scroll stays locked through exit | **Pass with `lockPageScroll`** | Unlock at `complete` |
| Exit waits for motion (ADR 0009) | **Pass** | 300ms keyframe, `complete` at 302ms to 319ms |
| Reopen during exit keeps the node (`Modal`) | **Pass** | Same node, no `complete(false)` |
| Reduced motion exits at once (DESIGN §6) | **Pass** | 22ms in Chromium, 10ms in WebKit |
| Esc closes only the top (#220) | **Pass** | Menu in dialog, React-nested, and React-sibling dialogs |
| Focus trap (`Modal`) | **Pass** | Tab cycles in `modal` and `trap-focus`, also nested |
| Covered dialog hidden from AT (`Modal`) | **Pass** | Parent gets `aria-hidden`. `inert` is not set |
| Veil press and release both on veil (#310) | **Pass** | Reason `outside-press` |
| Popup to veil, veil to popup drag | **Pass** | No close |
| Veil scrollbar press is not a dismiss | **Pass [source only]** | `useDismiss.ts` checks scrollbars. Headless has overlay scrollbars |
| Focus returns to opener | **Pass** | Plain open, and menu to dialog returns to the menu trigger |
| First field gets focus, not the close X (DESIGN §5) | **Fail stock** | Default is the first tabbable, the close X. Pass an `initialFocus` function |
| Opener gone: fall back to its region (`Modal`) | **Not tested** | Base UI falls back to previous focus. Pass a `finalFocus` function if a spec needs it |

### ADR 0008: can it hold?

**Yes, with edits. It stays a constraint.** The compositor symptoms themselves are
not proven, see [limits](#method-and-limits).

**Finding: Base UI's scroll lock does nothing in this app.** `html { overflow: hidden }`
[repo] makes `getViewportScroller` pick `html`. `isPageScrollLocked` then reads
`hidden`, treats the page as locked by someone else, and waits on a
`MutationObserver` forever [source:
[`useScrollLock.ts`](https://github.com/mui/base-ui/blob/4e89b3a/packages/utils/src/useScrollLock.ts)].
Measured with `modal={true}`: `body` stays `overflow-y: auto` with no inline style.
Control with no `html` overflow: `body` gets `overflow: hidden` [tested].

Three more reasons not to rely on it, even if `html` changes:

- It covers the viewport only, never `data-page-scroll-owner` [source].
- It acquires in a layout effect but locks in `setTimeout(0)`, which breaks "the
  first frame paints locked" [source].
- It releases when `open` goes false, at exit start [source: `useDialogRoot.ts`].

**Honest limit:** a wheel over the fixed veil did not scroll `body` in either engine,
even with no lock [tested]. So the gap is a contract gap (no `overflow: hidden`,
scrollbar stays), not a shown scroll bug.

**Required edits to the owned `dialog.tsx`:**

1. **Add `Dialog.Viewport`.** Stock `DialogContent` has no Viewport and is `fixed
   top-1/2 left-1/2 -translate-*` [source]. The veil scroll mode and `capped` mode
   need it. Base UI documents both as "outside scroll" and "inside scroll".
2. **Use `modal="trap-focus"`.** The library then has no scroll lock, so
   `lockPageScroll` is the one owner. Focus trap, `aria-hidden`, Esc, and outside
   press still work [tested]. The full-screen Viewport covers the page.
3. **Mount the lock in the popup.** A null component with a layout effect around
   `lockPageScroll()`. It mounts in the first commit and unmounts after exit.
4. **Render every backdrop:** `forceRender` on `Dialog.Backdrop`. By default a
   nested dialog has none [tested: 1 vs 2]. Two reasons: it keeps today's stacked dim,
   and with `trap-focus` and no backdrop `outsidePressEvent` becomes `sloppy`, which
   dismisses on press, not release [source: `useDialogRoot.ts`].
5. **Replace the overlay classes.** Stock Nova is `bg-black/10`,
   `supports-backdrop-filter:backdrop-blur-xs`, and an opacity fade at `duration-100`
   [tested]. ADR 0008 needs the dark fill and blur on `mg-backdropIn` and
   `mg-backdropOut`.
6. **Keep the selection guard (#310).** `onMouseDown` on Viewport with
   `preventDefault` when `target === currentTarget`. Dismiss still works [tested].

Shape:

```tsx
<DialogPrimitive.Root
  open={open}
  modal="trap-focus"
  onOpenChange={(next, details) => {
    if (!next && !dismissible) return details.cancel();
    onOpenChange(next, details);
  }}
  onOpenChangeComplete={(isOpen) => { if (!isOpen) onClosed?.(); }}
  disablePointerDismissal={!dismissible}
>
  <DialogPrimitive.Portal>
    <DialogPrimitive.Backdrop forceRender className="..." />
    <DialogPrimitive.Viewport className="..." onMouseDown={guardVeilSelection}>
      <DialogPrimitive.Popup initialFocus={firstFieldOrPopup} className="...">
        <PageScrollLock />
        {children}
      </DialogPrimitive.Popup>
    </DialogPrimitive.Viewport>
  </DialogPrimitive.Portal>
</DialogPrimitive.Root>
```

ADR 0008 asks for render and stylesheet tests. Repo rules now require Playwright
for layout (`boundingBox`, `getComputedStyle`), so port them there.

### Exit motion

Base UI sets `data-open` and `data-closed` and waits for `getAnimations()` on the
popup [source: [Animation](https://base-ui.com/react/handbook/animation)]. Keyframes
work [tested]. So the `mg-*` keyframes and token durations stay, and motion stays
CSS.

- **Do not use `tw-animate-css`.** Installed overlays use `data-open:animate-in
  fade-in-0 zoom-in-95` and `duration-100` [tested]. Without the import these classes
  do nothing. With it they break the DESIGN §6 token durations and `--ease-exit`.
  Replace them.
- **Use theme animations.** Declare `--animate-mg-scale-in:
  mg-scaleIn var(--dur-base) var(--ease)` and the other pairs in `@theme`, then write
  `data-open:animate-mg-scale-in data-closed:animate-mg-scale-out`. This fits
  the map's rule of tokens and named keyframes only [source: Tailwind theme].
- **The popup ends the exit, not the backdrop.** A 600ms backdrop exit under a 300ms
  popup exit was cut at about 320ms [tested]. Keep `exit(backdrop) <= exit(popup)`.
  Today both use `--dur-fast`.
- **Base UI recommends transitions,** because they cancel smoothly. Reopen during a
  keyframe exit restarts the entrance, as today's classes do. Stay with keyframes.

### ADR 0009: can it be replaced?

**Yes, and it must be superseded.** ADR 0009 says to migrate all surfaces together
and replace exit timers with presence completion. Base UI presence does this with no
Motion dependency.

| Today | After |
| --- | --- |
| `useDismissible` (`Modal`, `Menu`, `FilterBar`, `StatsTab`, `ProfilePanel`) | Base UI root state. Delete the hook |
| `layers` and `isTopmost()` | Floating tree. Delete |
| `modalLayers`, `syncModalLayers` | `markOthers`. Delete |
| Hand-written trap and `FOCUSABLE` | `FloatingFocusManager`. Delete |
| `exitDelayMs()` for overlays | `onOpenChangeComplete`. Keep the file for `useFlipRail` and check `DateRange` |
| `.modal--closing` classes | `data-closed:` |
| `Menu` placement code | `Menu.Positioner` |
| `lockPageScroll` | **Keep** (ADR 0008) |
| `useMovieModalHistory` | **Keep** |

**Movie modal history:** `onOpenChange(false)` calls `router.history.back()` and does
not set state. `open` goes false from the history entry, exit runs, then
`onOpenChangeComplete(false)` calls `onClosed`. A second Esc can request a close while
`open` is still true, so keep the "at most once per open interval" guard from
`Modal.onRequestClose`. A second `back()` would pop the entry behind the modal. Not
tested.

**Mid-save pin:** `disablePointerDismissal` covers outside press only. Esc needs
`details.cancel()` [source: `createBaseUIEventDetails.ts`]. See the shape above.

## 6. Upgrade path

Owned source has no automatic update. Each layer moves differently:

| Layer | How it moves | Command |
| --- | --- | --- |
| `web/src/components/ui/*` | By hand. Read the upstream change, then merge | `bunx shadcn@latest add <c> --diff <file>` |
| New component | Add, then apply the token edits | `bunx shadcn@latest add <c> --dry-run`, then without the flag |
| `@base-ui/react` | Semver, through Renovate as today | Read the changelog on minors. 1.0.0 to 1.9.0 shipped about monthly since 2025-12 |
| Base swap | Per-component skill, not a codemod. "You own the code" | `npx skills add shadcn/ui` |

- **`--diff`, `--dry-run`, `--view` are read-only** [source:
  [cli v4](https://ui.shadcn.com/docs/changelog/2026-03-cli-v4)]. Use `bunx` per
  repo rule.
- **`add --overwrite` replaces our edits.** Do not use it on an edited file.
  `apply` reinstalls components, and with `--only theme` it still rewrites tokens.
  `apply` is banned with `init`.
- **Keep edits small and in one place** to keep `--diff` readable. Prefer token
  aliases and variant edits. Wrap in `components/moviepickarr` for behavior that
  is product-specific.
- **Record the ban:** the ADR should name `init`, `apply`, and presets. `AGENTS.md`
  should repeat it, because the shadcn docs and skill both suggest `init`.

## Decisions for the ADR

The superseding ADR needs these. The evidence is above.

1. **`--accent` name.** Keep (safe at runtime, 125 references, ban on `init`) or
   rename to `--gold` when the CSS is gone. Recommendation: keep during the rebuild,
   rename in one late pass if `bg-accent` versus gold confuses reviewers.
2. **Focus ring.** Installed components use `outline-none` plus
   `focus-visible:ring-3 ring-ring/50`. `outline-none` in `utilities` beats the base
   `:focus-visible` rule [repo: `index.css`]. This is a second focus language, and
   DESIGN §6 says accessibility rules are inherited. Recommendation: remove the ring
   utilities in owned source and keep the 2px gold outline.
3. **Toast engine.** Sonner or Base UI `Toast` behind `toast-api.ts`. Recommendation:
   Sonner first. A later swap changes one file.
4. **Style.** `base-nova`, unless the prototype shows a better start.
5. **Supersede:** DESIGN §4 "no shadcn primitives", guardrails 1 and 2, and ADR 0009.

## For the prototype and frontend tickets

- Set `components.json` by hand first. Run `add` only. Install
  `class-variance-authority` yourself.
- Copy the [dialog shape](#adr-0008-can-it-hold). Stock `dialog.tsx` meets
  neither ADR 0008 nor DESIGN §5.
- `DropdownMenu` needs `modal={false}`, `align="end"`, and a width override.
- Use `Dialog` for `DeletionDialog`, not `AlertDialog`.
- Rename `ui/toast.tsx` before any `add toast`. Remove `next-themes` from
  `ui/sonner.tsx`.
- **Hand-check in Firefox and Safari:** open a modal on the Members page and on a
  movie record. Firefox must not drop the navigation for a frame. Safari must keep
  the scrollbar above the artwork. No automated check here covers this.
- Check by hand: menu Tab behavior (today it closes and returns focus), raw
  `var(--radius-md)` output, `data-open:` without `shadcn/tailwind.css`, and input
  contrast on `--line`.
- At 375px, check `Popover` and `DropdownMenu` collision with the bottom nav.

## Method and limits

- **Sources:** shallow clones of the upstream repos at the commits above, and the
  live docs. The changelog and docs were read from `apps/v4/content/docs`.
- **CLI test:** a copy of `web/` config and `index.css`, `shadcn@4.23.0`: `info`,
  `add --dry-run`, `add --diff`, a real `init`, and a real `add` of 13 components.
- **Browser test:** a throwaway React 19.2 page on `@base-ui/react` 1.9.0 with
  CSS copied from this repo (`html`, `body`, a `data-page-scroll-owner` element,
  `lockPageScroll`, backdrop and popup keyframes). Run with Playwright 1.64.1 on
  Chromium and WebKit. Not committed.
- **Tab timing:** a Tab loop with no gap between keys outran the focus guard's
  redirect and reached `body`. With a 40ms to 60ms gap the trap held in all cases,
  so this is a test artifact and not a Base UI defect.
- **Not covered:**
  - Firefox. Playwright Firefox hangs at launch here (see `AGENTS.md`).
  - Compositor painting. Tests prove structure, not frames.
  - Touch and screen readers.
  - Repo components on Base UI. The test page has its shape, not the real `Modal`.
  - A real `vite build` of adopted components with our tokens.

## Sources

shadcn: [Base UI as the Default](https://ui.shadcn.com/docs/changelog/2026-07-base-ui-default),
[React Aria](https://ui.shadcn.com/docs/changelog/2026-07-react-aria),
[Toast](https://ui.shadcn.com/docs/changelog/2026-07-toast),
[CLI v4](https://ui.shadcn.com/docs/changelog/2026-03-cli-v4),
[eject](https://ui.shadcn.com/docs/changelog/2026-05-shadcn-eject),
[cn](https://ui.shadcn.com/docs/changelog/2026-09-cn),
[apply](https://ui.shadcn.com/docs/changelog/2026-04-shadcn-apply),
[CLI](https://ui.shadcn.com/docs/cli),
[Theming](https://ui.shadcn.com/docs/theming),
[Tailwind v4](https://ui.shadcn.com/docs/tailwind-v4),
[components.json](https://ui.shadcn.com/docs/components-json),
[TanStack Form](https://ui.shadcn.com/docs/forms/tanstack-form).

Base UI: [Dialog](https://base-ui.com/react/components/dialog),
[Tooltip](https://base-ui.com/react/components/tooltip),
[Animation](https://base-ui.com/react/handbook/animation),
[Forms](https://base-ui.com/react/handbook/forms),
[`useScrollLock.ts`](https://github.com/mui/base-ui/blob/4e89b3a/packages/utils/src/useScrollLock.ts),
[`useDialogRoot.ts`](https://github.com/mui/base-ui/blob/4e89b3a/packages/react/src/dialog/root/useDialogRoot.ts),
[`useDismiss.ts`](https://github.com/mui/base-ui/blob/4e89b3a/packages/react/src/utils/popups/interactions/useDismiss.ts),
[`markOthers.ts`](https://github.com/mui/base-ui/blob/4e89b3a/packages/react/src/utils/popups/focus/markOthers.ts).

Radix: [Dialog](https://www.radix-ui.com/primitives/docs/components/dialog),
[`presence.tsx`](https://github.com/radix-ui/primitives/blob/eb3a2b7/packages/react/presence/src/presence.tsx).

TanStack Form: [Form Composition](https://tanstack.com/form/latest/docs/framework/react/guides/form-composition),
[Validation](https://tanstack.com/form/latest/docs/framework/react/guides/validation).

Tailwind: [Theme variables](https://tailwindcss.com/docs/theme),
[Data attributes](https://tailwindcss.com/docs/hover-focus-and-other-states).
