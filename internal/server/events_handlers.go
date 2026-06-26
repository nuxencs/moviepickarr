package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
)

// sseHeartbeatInterval is how often the server writes a `: ping` comment frame
// on an otherwise-idle SSE connection. It does two jobs:
//
//   - keep-alive: regular traffic stops intermediaries from reaping an idle
//     stream — nginx's 60s proxy_read_timeout, Cloudflare's edge idle limit, NAT
//     table eviction.
//   - liveness: a failed flush on the ping is how we notice a dead/half-open
//     socket (e.g. a backgrounded tab whose TCP connection died) and unwind to
//     the deferred Unsubscribe within one interval — instead of leaking the
//     subscription until the next domain event happens to fail its write.
//
// 15s leaves a 4x margin under nginx's 60s default and is well under
// Cloudflare's ~100s edge idle limit.
const sseHeartbeatInterval = 15 * time.Second

func (h *handler) handleSSE(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		eventChannel := h.broker.Subscribe()
		// nil means the broker is closed (server shutting down) — don't open a
		// stream that would block on a channel that's never fed or closed.
		if eventChannel == nil {
			return
		}
		defer h.broker.Unsubscribe(eventChannel)

		// One sse sub-logger for the whole stream so every write/flush site is
		// tagged identically (and a future site can't forget the tag).
		sseLog := h.log.With().Str("subsystem", "sse").Logger()

		// retry: hints the reconnect delay EventSource uses in the window before
		// the client's own backoff takes over.
		if _, err := fmt.Fprintf(w, "retry: 3000\nevent: connected\ndata: {\"type\":\"connected\"}\n\n"); err != nil {
			sseLog.Debug().Err(err).Msg("sse client write failed (likely disconnect)")
			return
		}
		if err := w.Flush(); err != nil {
			sseLog.Debug().Err(err).Msg("sse client flush failed (likely disconnect)")
			return
		}

		ticker := time.NewTicker(sseHeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case e, ok := <-eventChannel:
				if !ok {
					// Broker closed the channel (Unsubscribe or server shutdown).
					return
				}

				eventData, err := json.Marshal(e)
				if err != nil {
					sseLog.Error().Err(err).Msg("sse event marshal failed")
					continue
				}

				if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", eventData); err != nil {
					sseLog.Debug().Err(err).Msg("sse client write failed (likely disconnect)")
					return
				}
				if err := w.Flush(); err != nil {
					sseLog.Debug().Err(err).Msg("sse client flush failed (likely disconnect)")
					return
				}

			case <-ticker.C:
				// Comment frame: EventSource ignores `:`-prefixed lines, so this
				// never reaches the client's message handler — it only keeps the
				// pipe warm and surfaces dead sockets. A failed ping is how we
				// notice a dead half-open socket, so it is worth a debug line.
				if _, err := w.WriteString(": ping\n\n"); err != nil {
					sseLog.Debug().Err(err).Msg("sse heartbeat write failed (likely disconnect)")
					return
				}
				if err := w.Flush(); err != nil {
					sseLog.Debug().Err(err).Msg("sse heartbeat flush failed (likely disconnect)")
					return
				}
			}
		}
	})

	return nil
}
