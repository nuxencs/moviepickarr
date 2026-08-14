# ADR 0009: Keep one floating-surface lifecycle

Status: accepted (2026-08-14)

Moviepickarr uses CSS motion and `useDismissible` as one lifecycle for modals,
menus, filter dropdowns, and the profile panel. Introducing Motion for one
surface would create a second exit and unmount contract, with different timing
for focus restoration, layer removal, interrupted exits, and parent cleanup.

Keep CSS and `useDismissible` as the single floating-surface lifecycle. Do not
adopt Motion for one surface in isolation. If Motion is adopted later, migrate
every floating surface together and replace the shared exit timers with presence
completion while preserving dismissal, focus, stacking, history, and scroll-lock
behavior.
