# AGENTS.md

## Development

Commands, dev fixtures, and dev logins (`ada` / `devpassword`): `docs/DEVELOPMENT.md`. Seeded movie titles: `internal/devfixtures/data/movies.json`. Start the app with `make dev`.

- Checks: `make test` and `make lint` from the root. Web only: `bun run test`, `bun run lint`, and `bunx tsc -b` in `web/`. There is no Prettier config and no `typecheck` script.
- Playwright serves the Go binary with the embedded `web/dist`. `bun run test:e2e` builds first; a direct `bunx playwright test` does not, so run `bun run build` before it. Use `--project=chromium` locally: Firefox cannot launch in the agent sandbox, so CI covers it.
- Guard layout and motion regressions in Playwright (`boundingBox`, `getComputedStyle`), never with vitest tests that read CSS as text.

## Comments

Applies to Go, TypeScript, and CSS. Default to no comment. Add one only when it earns its place:

- Comment the why, never the what: non-obvious invariants, guards and the bug they prevent, workarounds, dependencies between files (`keep in step with members.css`), deliberate deviations from the expected approach.
- Keep a comment to one line when practical, a few lines at most. Long rationale goes in the issue or PR, `docs/DESIGN.md`, or `docs/adr/`. Link it with the issue number (`#236`) instead of retelling it.
- Delete comments that restate the code, the type, or the name. No step narration (`// return the result`), no section banners that only name the next block.
- Never describe the edit (`// changed to use X`, `// now also handles Y`). That history belongs in the commit message.
- No commented-out code and no unprompted `TODO` or `FIXME`.
- Doc comments on exported identifiers: one short sentence (in Go, start with the name). Expand only when the contract is subtle.
- Test comments: name the scenario only when the test name does not.

## Agent skills

### Issue tracker

Issues live in GitHub Issues on `nuxencs/moviepickarr`, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `GLOSSARY.md` and `docs/adr/` at the repo root, created lazily. See `docs/agents/domain.md`.
