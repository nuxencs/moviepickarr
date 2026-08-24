# moviepickarr web

The React 19 + Vite + Tailwind v4 frontend. Package manager is **Bun 1.4+**.
Dependency installs use Bun's isolated linker and shared global package store.

```bash
bun install
bun run dev       # Vite dev server (proxies /api + SSE to the Go backend on :3030)
bun run build     # tsc -b && vite build → dist/ (the Go binary embeds this)
bun run lint      # eslint
bun run test      # vitest: the `node` project (pure logic) + `dom` (jsdom render tests)
bun run test:e2e  # Playwright: production app in Chromium, Firefox, and WebKit
```

Install the engines once with `bunx playwright install chromium firefox webkit`
before the first end-to-end run.

For the full dev loop (Vite + Go side by side) run `make dev` from the repo root.

## Where things live

- `src/components/moviepickarr/`: the app components and the draw machine
  (`drawMachine.ts` reducer + `drawStore.ts` store, `DrawReel.tsx`).
- `src/hooks/`: `useSSE.ts` plus its pure reducers (`sseConnection.ts`,
  `sseInvalidations.ts`) and the shared `useDismissible.ts` for floating surfaces.
- `src/index.css`: shared design-system tokens and component classes. A component can
  import a co-located stylesheet for styles that have no other consumer.

## Docs

- [`../docs/DESIGN.md`](../docs/DESIGN.md): the design system. Read it before any UI work.
- [`../CONTEXT.md`](../CONTEXT.md): the glossary. Use its terms (draw, adder, next up).
- [`../docs/DEVELOPMENT.md`](../docs/DEVELOPMENT.md): the dev/test/lint workflow.
