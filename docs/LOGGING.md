# Logging

moviepickarr logs with [zerolog](https://github.com/rs/zerolog). One root logger
is built once at startup; everything else is a component sub-logger derived from
it. HTTP access logs come from the
[`fiberzerolog`](https://github.com/gofiber/contrib/tree/main/fiberzerolog)
middleware, so app logs and access logs share one writer, format, and level.

## Configuration

Two environment variables (read at startup, `.env` honoured):

| Variable | Values | Default | Effect |
| --- | --- | --- | --- |
| `LOG_LEVEL` | `trace`·`debug`·`info`·`warn`·`error`·`fatal` | `info` | Global floor; events below it are dropped. Unrecognised → `info`. |
| `LOG_FORMAT` | `json` · `console` | `json` | `json`: one structured line per event (zero-alloc, production). `console`: colourised, human-readable, with a `file:line` caller (local dev). |

`console` colour auto-disables when stderr is not a terminal (piped to a file,
CI, journald), so captured logs stay free of escape codes. `console` is slower
than `json` — keep it for dev only.

```
# dev
LOG_FORMAT=console LOG_LEVEL=debug ./bin/moviepickarr
# production (defaults)
./bin/moviepickarr
```

## Architecture

- **Root logger** — `internal/logger.New(logger.FromEnv())` builds it (writer +
  format + timestamp + console-only caller) and applies `LOG_LEVEL` via
  `zerolog.SetGlobalLevel`. Called once in `server.Run`.
- **Global mirror** — the root is assigned to zerolog's package global
  (`zerolog/log`) so package-level call sites (e.g. the env parsers in
  `enrich_worker.go`) and `main`'s fatal exit log through the same writer.
- **Component sub-loggers** — long-lived components get an injected sub-logger
  carrying a `component` field, derived with
  `root.With().Str("component", …).Logger()`:
  - `component=http` — the HTTP handler (`handler.log`) and the access-log
    middleware.
  - `component=enrich` — the background enrichment worker (`enrichRunner.log`).
  - SSE sites additionally tag `subsystem=sse`.

Sub-loggers are plain `zerolog.Logger` values (cheap to copy); tests pass
`zerolog.Nop()`.

## HTTP access logs

`fiberzerolog` replaces Fiber's text logger. Middleware order is
`requestid → fiberzerolog → recover`: `requestid` sets the `X-Request-ID`
response header on the way in, the logger reads it on the way out (so every
access line carries `requestId`), and it sits ahead of `recover` so a recovered
panic still yields one line.

Each request logs `requestId`, `ip`, `method`, `path`, `status`, `latency`,
`bytesSent`, and `error` (only when the chain returned one). Status maps to
level: `5xx → error` ("http server error"), `4xx → warn` ("http client error"),
else `info` ("http request"). The SSE stream (`/api/v1/events`) is skipped — its
"latency" would span the whole session.

## Level taxonomy

How levels are used in this codebase:

| Level | Use | Examples |
| --- | --- | --- |
| `debug` | high-volume / expected-noise detail | per-movie enrich result, batch flush, SSE write/flush failure (normal on client disconnect) |
| `info` | lifecycle & access | startup banner, worker started, drain start/summary, graceful shutdown, 2xx/3xx requests |
| `warn` | recoverable / degraded | invalid env value (default used), enrich queue full, single movie enrich failure, drain interrupted, non-fatal metadata/credits load failure, 4xx requests |
| `error` | operation failed, process continues | needs-enrichment query failure, SSE marshal failure, watch/next-up transaction failure (`watch current movie and advance next up failed`), shutdown/db-close errors, 5xx requests (`http server error`) |
| `fatal` | unrecoverable startup failure only | `main`'s `run()` error — logs then `os.Exit(1)` |

**Never call `Fatal` inside a handler or the worker** — it skips graceful
shutdown. Return the error instead; only `main`'s outermost frame may `Fatal`.

## Conventions for new logs

- Use the injected sub-logger (`h.log`, `r.log`), not the global, inside
  components.
- Attach errors with `.Err(err)`, not `%v` in the message. Let `Msg` describe the
  action (`"failed to load movie credits"`), not restate the error.
- Add structured fields with typed builders (`.Int("movieID", id)`,
  `.Dur("ttl", d)`) instead of interpolating into the message — keeps logs
  queryable.
