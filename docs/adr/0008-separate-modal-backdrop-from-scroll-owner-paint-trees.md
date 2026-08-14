# ADR 0008: Separate the modal backdrop from scroll-owner paint trees

Status: accepted (2026-08-14)

## Context

Movie modals need a dark, blurred page backdrop while their own scrollbars and
artwork remain stable. The Members page replaces document scrolling with three
bounded page scroll owners. Opening any modal locks every page scroll owner.

Filtering `.app` puts the shared navigation and those bounded owners in one
Firefox compositor layer. When the modal locks the owners, Firefox can omit the
navigation for a frame while it rebuilds that layer. Putting `backdrop-filter`
on the modal veil instead makes the filter an ancestor of the movie detail
scroll owner. Safari can then paint descendant artwork above its native
scrollbar.

## Decision

Render one fixed `.modal-backdrop` beside `.modal` inside the portalled veil.
The backdrop alone owns the dark fill and `backdrop-filter`. It is never an
ancestor of the dialog and no modal state filters `.app`.

Animate the backdrop's dark fill and blur together on that element. Do not fade
the parent veil. A browser can composite the fixed filtered child separately and
show its full blur on the first frame even while the ancestor opacity fades.

Lock the body and every element marked `data-page-scroll-owner` in a layout
effect. The first modal frame therefore paints with its final page-scroll state.
Keep the lock refcounted so nested dialogs release page scrolling only after the
last dialog closes.

Cover the structural contract with render and stylesheet tests. The tests must
fail if the backdrop becomes a dialog ancestor, `.app` receives the movie-modal
filter again, or the modal veil becomes the movie backdrop filter.

## Consequences

All modal callers use the same backdrop without knowing about browser-specific
compositor behavior. Movie detail scrollbars stay in their native paint plane,
and bounded routes can change scroll ownership without sharing a filtered layer
with navigation.

The veil and backdrop remain separate elements. Future overlay changes must
preserve that structure. Backdrop motion must continue to interpolate its dark
fill and blur on the backdrop itself.
