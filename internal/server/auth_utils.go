package server

import (
	"moviepickarr/internal/auth"

	"github.com/gofiber/fiber/v2"
)

func loginMetadataFromFiber(c *fiber.Ctx) auth.LoginMetadata {
	return auth.LoginMetadata{
		UserAgent: c.Get("User-Agent"),
		IP:        c.IP(),
	}
}
