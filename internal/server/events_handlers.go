package server

import (
	"bufio"
	"context"
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
// offset (mirrors the value GET /movies/current returns for the active draw).
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

	// Capture the session token now, while the request context is live, so the
	// stream can revalidate it on every heartbeat. requireSession already accepted
	// this token to reach here (401 before the stream ever opens); the per-heartbeat
	// recheck is what drops a session revoked AFTER the handshake.
	sessionToken := c.Cookies(sessionCookieName)
	// Same reason: c.Locals is only valid while the request context lives, and
	// the body-stream writer below outlives it.
	memberID := actorMemberID(c)

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		eventChannel, headSeq := h.broker.Subscribe()
		// nil means the broker is closed (server shutting down) — don't open a
		// stream that would block on a channel that's never fed or closed.
		if eventChannel == nil {
			return
		}
		defer h.broker.Unsubscribe(eventChannel)

		// One sse sub-logger for the whole stream so every write/flush site is
		// tagged identically (and a future site can't forget the tag). member_id
		// is attached here rather than per line: a stream belongs to exactly one
		// member for its whole life, and "whose stream just died" is the first
		// thing you want to know.
		sseLog := h.log.With().
			Str("subsystem", "sse").
			Int("member_id", memberID).
			Logger()

		// emit writes one fully formatted frame and flushes it. Every frame goes
		// through here: the write and flush failures used to be six call sites
		// sharing two message strings, so a broken pipe told you nothing about
		// which frame was in flight. Now there is one of each, plus a frame field.
		emit := func(frame, payload string) error {
			if _, err := w.WriteString(payload); err != nil {
				sseLog.Debug().Err(err).Str("frame", frame).Msg("client write failed, closing stream")
				return err
			}
			if err := w.Flush(); err != nil {
				sseLog.Debug().Err(err).Str("frame", frame).Msg("client flush failed, closing stream")
				return err
			}
			return nil
		}

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
			sseLog.Error().Err(err).Str("frame", "connected").Msg("frame marshal failed")
			return
		}
		if emit("connected", fmt.Sprintf("retry: 3000\nevent: connected\ndata: %s\n\n", connectedData)) != nil {
			return
		}

		ticker := time.NewTicker(h.sseHeartbeatInterval)
		defer ticker.Stop()

		// writeEvent serialises one domain event to the stream. A marshal error
		// skips that event (logged, non-fatal); a write/flush error is fatal to
		// the stream and unwinds to the deferred Unsubscribe.
		writeEvent := func(e event) error {
			eventData, err := json.Marshal(e)
			if err != nil {
				// Which event type is unmarshalable is the entire diagnostic
				// here — without it this line names a closure, not a bug.
				sseLog.Error().Err(err).
					Str("frame", "message").
					Str("event", e.Type).
					Uint64("seq", e.Seq).
					Msg("frame marshal failed")
				return nil
			}
			// id: persists the seq in the browser; the client also reads it from
			// the JSON body for gap detection.
			return emit("message", fmt.Sprintf("id: %d\nevent: message\ndata: %s\n\n", e.Seq, eventData))
		}

		for {
			select {
			case e, ok := <-eventChannel:
				if !ok {
					// Broker closed the channel (Unsubscribe or server shutdown).
					return
				}
				if err := writeEvent(e); err != nil {
					return
				}

			case <-ticker.C:
				// Revalidate the session before doing any heartbeat work: a session
				// revoked (logout-everywhere, admin reset, password change) or expired
				// after the handshake must stop receiving updates within one interval.
				// Revalidate (not Authenticate) is deliberate: it must not slide the
				// idle window, or a long-held stream would keep an idle session alive.
				// Best-effort context: the request's is gone once the writer runs.
				if err := h.sessions.Revalidate(context.Background(), sessionToken); err != nil {
					sseLog.Debug().Err(err).Msg("session revoked or expired mid-stream, closing stream")
					return
				}

				// Flush any events already queued for this client BEFORE the
				// heartbeat. The heartbeat carries the broker's global head seq, so
				// emitting it ahead of events still buffered for this client would
				// leapfrog them: the client advances its cursor to the head, then
				// reads the trailing buffered events as a seq gap and resyncs twice.
				// Draining first keeps the head the client sees consistent with what
				// it has actually been sent.
				if open, err := drainBufferedEvents(eventChannel, writeEvent); err != nil || !open {
					return
				}

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
					sseLog.Error().Err(err).Str("frame", "heartbeat").Msg("frame marshal failed")
					return
				}
				if emit("heartbeat", fmt.Sprintf("event: heartbeat\ndata: %s\n\n", heartbeatData)) != nil {
					return
				}
			}
		}
	})

	return nil
}

// drainBufferedEvents writes every event currently queued on ch, in seq order,
// via write, then returns as soon as ch is empty. It stops early if write
// reports a fatal error (returned) or ch is closed (open=false). Called before
// a heartbeat so the heartbeat's global head seq is never written ahead of
// events already buffered for this client, which the client would otherwise
// read as a seq gap and resync over needlessly.
func drainBufferedEvents(ch <-chan event, write func(event) error) (open bool, err error) {
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return false, nil
			}
			if werr := write(e); werr != nil {
				return true, werr
			}
		default:
			return true, nil
		}
	}
}
