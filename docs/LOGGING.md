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
access line carries `request_id`), and it sits ahead of `recover` so a recovered
panic still yields one line.

Each request logs `request_id`, `ip`, `method`, `path`, `status`, `latency`,
`bytes_sent`, and `error` (only when the chain returned one). Status maps to
level: `5xx → error` ("http server error"), `4xx → warn` ("http client error"),
else `info` ("http request"). The SSE stream (`/api/v1/events`) is skipped — its
"latency" would span the whole session.

The middleware is configured with `FieldsSnakeCase: true` so its field names
match the rest of the codebase (see [Field names](#field-names)); without it
fiberzerolog emits `requestId` and `bytesSent`.

## Request-scoped logging

Handlers log through `h.reqLog(c)`, not `h.log`. It derives a per-request
sub-logger carrying `request_id`, `method`, `route`, plus `member_id` once
`requireSession` has attached one. That correlates an app log line with the
access line for the same request, and means a handler never has to re-add "who
and what" by hand.

Middleware that returns before `c.Next()` omits `route`: Fiber still reports the
middleware prefix at that point, not the endpoint template. Its `request_id`
still joins the line to the access log's concrete `path`.

```go
h.reqLog(c).Error().Err(err).Int("movie_id", id).Msg("advancing next up failed")
```

Use the bare `h.log` only outside a request: background sweeps, warm-up
goroutines, startup.

## Level taxonomy

How levels are used in this codebase:

| Level | Use | Examples |
| --- | --- | --- |
| `debug` | high-volume / expected-noise detail | per-movie enrich result, batch flush, SSE write/flush failure (normal on client disconnect), expired-session sweep counts |
| `info` | lifecycle & access | startup banner, worker started, drain start/summary, graceful shutdown, backup written, 2xx/3xx requests |
| `warn` | recoverable / degraded | invalid env value (default used), enrich queue full, single movie enrich failure, drain interrupted, non-fatal metadata/credits load failure, caller-caused OIDC failures (provider denied, invite expired), 4xx requests |
| `error` | operation failed, process continues | needs-enrichment query failure, SSE marshal failure, watch/next-up transaction failure, shutdown/db-close errors, 5xx requests |
| `fatal` | unrecoverable startup failure only | `main`'s `run()` error — logs then `os.Exit(1)` |

The line between `warn` and `error` is **whose fault it is**, not how loud it
feels. A caller who sends a stale invite or cancels at the identity provider is
a `warn`: nothing is broken and no operator action follows. A failed DB write,
a failed seal, a failed marshal is an `error`: the code or the environment is
wrong and someone should look. Do not log an `error` for a path that already
returns a 4xx.

**Never call `Fatal` inside a handler or the worker** — it skips graceful
shutdown. Return the error instead; only `main`'s outermost frame may `Fatal`.

## Field names

Field keys are `snake_case`, always. A log line's value is being able to filter
on it, and that breaks the moment the same entity is `movieID` in one file and
`movie_id` in the next.

Use the canonical key when one exists rather than inventing a synonym:

| Key | Type | Meaning |
| --- | --- | --- |
| `request_id` | string | Fiber's `X-Request-ID`; joins app lines to the access line |
| `member_id` | int | The acting member (session owner), never a path parameter |
| `movie_id` | int | Local movie row id |
| `tmdb_id` | int | TMDB movie id |
| `invite_id` | int64 | Invite row id |
| `issuer`, `subject` | string | OIDC identity coordinates |
| `intent` | string | Which OIDC flow (login, link, claim) |
| `component`, `subsystem` | string | Set on the sub-logger, not per call |
| `count` | int, int64 | How many of the thing the message names |
| `event` | string | SSE event type |
| `frame` | string | Which SSE frame was in flight |
| `key`, `value` | string | An env var and the value that failed to parse |
| `file` | string | A filesystem path |

Two keys are owned by the HTTP layer and must not be reused for anything else.
`path` is the concrete request URL, written by the access-log middleware.
`route` is the route template (`/api/v1/movies/:movieID`), written by `reqLog`.
They are deliberately separate: the template groups, the URL identifies, and
collapsing them into one key breaks whichever query you were relying on.

Never log a secret or a credential-derived value: no passwords, no session
cookie tokens, no token hashes, no invite tokens, no API keys. `session_id` and
`member_id` identify a session for support purposes without being usable to
assume it. Emails appear only where the log is about identity resolution
(OIDC claims that matched no member) and nowhere else.

## Message phrasing

- Lowercase, no trailing punctuation, no error text interpolated in.
- Name the action that failed, in the order *what* then *what happened*:
  `"advancing next up failed"`, not `"failed to advance next up"` and not
  `"error"`. Past-tense verbs for things that completed
  (`"poster wall warmed"`).
- A message plus its fields must identify the call site uniquely within its
  package. Duplicated wording across the SSE and enrich paths is what makes grep
  useless. Reusing a string is allowed in exactly one case: several call sites
  doing the same thing to different subjects, where a field names the subject.
  The SSE stream does this, with one `"frame marshal failed"` shared by the
  connected, message, and heartbeat frames and a `frame` field saying which. If
  you cannot name the difference in a field, the messages must differ.
- Don't prefix the message with the component (`"enrich: ..."`). The
  `component` field already carries that, and the prefix defeats grepping by
  message.

## Conventions for new logs

- Inside a handler use `h.reqLog(c)`; inside a component use the injected
  sub-logger (`r.log`), not the zerolog global.
- Attach errors with `.Err(err)`, not `%v` in the message. Let `Msg` describe the
  action, not restate the error.
- Add structured fields with typed builders (`.Int("movie_id", id)`,
  `.Dur("ttl", d)`) instead of interpolating into the message — keeps logs
  queryable.
- Before adding a line to a per-client or per-item loop, decide whether it is
  `debug`. SSE write and flush failures fire once per client per disconnect and
  are normal traffic, not incidents.

## Not covered: the frontend

Nothing in `web/` reports to this log. Browser errors stay in the browser
console, and there is no ingest endpoint. That is a deliberate gap, not an
oversight: shipping client errors server-side needs its own decisions about
authentication, rate limiting, and what user content is allowed to travel in a
stack trace. Until those are made, a server log line means something that
happened on the server.
