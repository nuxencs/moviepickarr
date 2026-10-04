package server

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/auth"

	"github.com/gofiber/fiber/v2"
)

// sseHeartbeatInterval paces the idle `heartbeat` frame: it keeps proxies from
// reaping the stream, a failed flush detects a dead socket, and its head seq
// lets an idle client detect a gap. 15s stays under nginx's 60s and
// Cloudflare's ~100s idle limits.
const sseHeartbeatInterval = 15 * time.Second

// connectedFrame is the handshake: epoch detects a restart, seq aligns the gap
// cursor, serverNow seeds the clock offset (as GET /movies/current does).
type connectedFrame struct {
	Type      string `json:"type"`
	Epoch     string `json:"epoch"`
	Seq       uint64 `json:"seq"`
	ServerNow string `json:"serverNow"`
}

// heartbeatFrame has no id: line, so it never moves the client's seq cursor.
type heartbeatFrame struct {
	Seq       uint64 `json:"seq"`
	ServerNow string `json:"serverNow"`
}

func (h *handler) handleSSE(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	// Kept for the per-heartbeat recheck that drops a session revoked after the
	// handshake.
	sessionToken := c.Cookies(sessionCookieName)
	// The stream writer outlives c, so copy the request-scoped logger now.
	sseLog := h.reqLog(c).With().Str("subsystem", "sse").Logger()

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		eventChannel, headSeq := h.broker.Subscribe()
		// nil: the broker is closed, and the channel would never be fed.
		if eventChannel == nil {
			return
		}
		defer h.broker.Unsubscribe(eventChannel)

		// emit writes and flushes every frame, so failures log which frame broke.
		emit := func(frame, format string, args ...any) error {
			if _, err := fmt.Fprintf(w, format, args...); err != nil {
				sseLog.Debug().Err(err).Str("frame", frame).Msg("client write failed, closing stream")
				return err
			}
			if err := w.Flush(); err != nil {
				sseLog.Debug().Err(err).Str("frame", frame).Msg("client flush failed, closing stream")
				return err
			}
			return nil
		}

		// retry: covers EventSource until the client's own backoff takes over.
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
		if err := emit("connected", "retry: 3000\nevent: connected\ndata: %s\n\n", connectedData); err != nil {
			return
		}

		ticker := time.NewTicker(h.sseHeartbeatInterval)
		defer ticker.Stop()

		// writeEvent skips an unmarshalable event; a write error ends the stream.
		writeEvent := func(e event) error {
			eventData, err := json.Marshal(e)
			if err != nil {
				sseLog.Error().Err(err).
					Str("frame", "message").
					Str("event", e.Type).
					Uint64("seq", e.Seq).
					Msg("frame marshal failed")
				return nil
			}
			return emit("message", "id: %d\nevent: message\ndata: %s\n\n", e.Seq, eventData)
		}

		for {
			select {
			case e, ok := <-eventChannel:
				if !ok {
					return
				}
				if err := writeEvent(e); err != nil {
					return
				}

			case <-ticker.C:
				// Revalidate, not Authenticate: sliding the idle window would let an
				// open stream keep an idle session alive. The request context is gone here.
				if err := h.sessions.Revalidate(context.Background(), sessionToken); err != nil {
					if errors.Is(err, auth.ErrSessionInvalid) {
						sseLog.Debug().Err(err).Msg("session revoked or expired mid-stream, closing stream")
					} else {
						sseLog.Error().Err(err).Msg("session revalidation failed, closing stream")
					}
					return
				}

				// A heartbeat ahead of buffered events would read as a seq gap.
				if open, err := drainBufferedEvents(eventChannel, writeEvent); err != nil || !open {
					return
				}

				heartbeatNow := time.Now().UTC()
				heartbeatData, err := json.Marshal(heartbeatFrame{
					Seq:       h.broker.HeadSeq(),
					ServerNow: formatTime(&heartbeatNow),
				})
				if err != nil {
					sseLog.Error().Err(err).Str("frame", "heartbeat").Msg("frame marshal failed")
					return
				}
				if err := emit("heartbeat", "event: heartbeat\ndata: %s\n\n", heartbeatData); err != nil {
					return
				}
			}
		}
	})

	return nil
}

// drainBufferedEvents writes the events queued on ch without blocking. It stops
// on a write error or a closed ch (open=false).
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
