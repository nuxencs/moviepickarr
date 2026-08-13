# moviepickarr web

The React 19 + Vite + Tailwind v4 frontend. Package manager is **bun**.

```bash
bun install
bun run dev       # Vite dev server (proxies /api + SSE to the Go backend on :3030)
bun run build     # tsc -b && vite build → dist/ (the Go binary embeds this)
bun run lint      # eslint
bun run test      # vitest: the `node` project (pure logic) + `dom` (jsdom render tests)
bun run test:e2e  # Playwright: browser layout and interaction regressions
```

Install the browser once with `bunx playwright install chromium` before the
first end-to-end run.

For the full dev loop (Vite + Go side by side) run `make dev` from the repo root.

## Where things live

- `src/components/moviepickarr/`: the app components and the draw machine
  (`drawMachine.ts` reducer + `drawStore.ts` store, `DrawReel.tsx`).
- `src/hooks/`: `useSSE.ts` plus its pure reducers (`sseConnection.ts`,
  `sseInvalidations.ts`) and the shared `useDismissible.ts` for floating surfaces.
- `src/index.css`: the design-system tokens and component classes (source of truth).

## Docs

- [`../docs/DESIGN.md`](../docs/DESIGN.md): the design system. Read it before any UI work.
- [`../CONTEXT.md`](../CONTEXT.md): the glossary. Use its terms (draw, adder, next up).
- [`../docs/DEVELOPMENT.md`](../docs/DEVELOPMENT.md): the dev/test/lint workflow.
