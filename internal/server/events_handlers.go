package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func (h *handler) handleSSE(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		eventChannel := h.broker.Subscribe()
		defer h.broker.Unsubscribe(eventChannel)

		if _, err := fmt.Fprintf(w, "event: connected\ndata: {\"type\":\"connected\"}\n\n"); err != nil {
			log.Printf("error writing to client: %v", err)
			return
		}
		if err := w.Flush(); err != nil {
			log.Printf("error flushing client: %v", err)
			return
		}

		for e := range eventChannel {
			eventData, err := json.Marshal(e)
			if err != nil {
				log.Printf("error marshalling event: %v", err)
				continue
			}

			if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", eventData); err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}
		}
	})

	return nil
}
