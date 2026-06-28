package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
)

// sseHeartbeatInterval is how often the server writes a named `heartbeat` frame
// on an otherwise-idle SSE connection. It does three jobs:
//
//   - keep-alive: regular traffic stops intermediaries from reaping an idle
//     stream — nginx's 60s proxy_read_timeout, Cloudflare's edge idle limit, NAT
//     table eviction.
//   - liveness: a failed flush on the heartbeat is how we notice a dead/half-open
//     socket (e.g. a backgrounded tab whose TCP connection died) and unwind to
//     the deferred Unsubscribe within one interval — instead of leaking the
//     subscription until the next domain event happens to fail its write.
//   - passive gap detection: the frame carries the current head seq, so an idle
//     client whose cursor trails it (a frame was dropped on a full buffer) knows
//     to resync without waiting for the next domain event — plus a serverNow the
//     client can use to refresh its choreography clock offset.
//
// 15s leaves a 4x margin under nginx's 60s default and is well under
// Cloudflare's ~100s edge idle limit.
const sseHeartbeatInterval = 15 * time.Second

// connectedFrame is the one-shot handshake sent when a stream opens. epoch lets a
// client detect a server restart; seq is the head at subscribe time, so the
// client aligns its gap-detection cursor; serverNow seeds the choreography clock
// offset (mirrors the value GET /movies/current returns for the active pick).
type connectedFrame struct {
	Type      string `json:"type"`
	Epoch     string `json:"epoch"`
	Seq       uint64 `json:"seq"`
	ServerNow string `json:"serverNow"`
}

// heartbeatFrame is the idle keep-alive. It carries no id: line (so it never
// perturbs the client's seq cursor) but does carry the head seq for passive gap
// detection and serverNow for clock-offset refresh.
type heartbeatFrame struct {
	Seq       uint64 `json:"seq"`
	ServerNow string `json:"serverNow"`
}

func (h *handler) handleSSE(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		eventChannel, headSeq := h.broker.Subscribe()
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
		// the client's own backoff takes over. The handshake carries epoch (restart
		// detection), the head seq (cursor alignment) and serverNow (clock offset).
		connectedNow := time.Now().UTC()
		connectedData, err := json.Marshal(connectedFrame{
			Type:      "connected",
			Epoch:     h.broker.Epoch(),
			Seq:       headSeq,
			ServerNow: formatTime(&connectedNow),
		})
		if err != nil {
			sseLog.Error().Err(err).Msg("sse connected frame marshal failed")
			return
		}
		if _, err := fmt.Fprintf(w, "retry: 3000\nevent: connected\ndata: %s\n\n", connectedData); err != nil {
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

				// id: persists the seq in the browser; the client also reads it from
				// the JSON body for gap detection.
				if _, err := fmt.Fprintf(w, "id: %d\nevent: message\ndata: %s\n\n", e.Seq, eventData); err != nil {
					sseLog.Debug().Err(err).Msg("sse client write failed (likely disconnect)")
					return
				}
				if err := w.Flush(); err != nil {
					sseLog.Debug().Err(err).Msg("sse client flush failed (likely disconnect)")
					return
				}

			case <-ticker.C:
				// Named heartbeat (no id: line, so it never advances the seq cursor).
				// It reaches a dedicated client listener — never the message handler —
				// and does triple duty: keep the pipe warm, surface dead sockets (a
				// failed flush is how we notice a half-open socket, worth a debug
				// line), and carry the head seq + serverNow for passive gap detection
				// and clock refresh.
				heartbeatNow := time.Now().UTC()
				heartbeatData, err := json.Marshal(heartbeatFrame{
					Seq:       h.broker.HeadSeq(),
					ServerNow: formatTime(&heartbeatNow),
				})
				if err != nil {
					sseLog.Error().Err(err).Msg("sse heartbeat frame marshal failed")
					return
				}
				if _, err := fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", heartbeatData); err != nil {
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
