# AGENTS.md

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

Single-context: `CONTEXT.md` and `docs/adr/` at the repo root, created lazily. See `docs/agents/domain.md`.
