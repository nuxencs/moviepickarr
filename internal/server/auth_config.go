package server

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type authConfig struct {
	CookieName   string
	CookieSecure bool
	SessionTTL   time.Duration
	AdminName    string
	AdminUser    string
	AdminPass    string
}

func authConfigFromEnv() authConfig {
	cfg := authConfig{
		CookieName:   defaultAuthCookieName,
		CookieSecure: false,
		SessionTTL:   7 * 24 * time.Hour,
		AdminName:    "Admin",
	}

	if raw := strings.TrimSpace(os.Getenv("AUTH_COOKIE_NAME")); raw != "" {
		cfg.CookieName = raw
	}

	if raw := strings.TrimSpace(os.Getenv("AUTH_COOKIE_SECURE")); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			cfg.CookieSecure = parsed
		}
	}

	if raw := strings.TrimSpace(os.Getenv("AUTH_SESSION_TTL")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			cfg.SessionTTL = parsed
		}
	}

	if raw := strings.TrimSpace(os.Getenv("AUTH_ADMIN_NAME")); raw != "" {
		cfg.AdminName = raw
	}
	cfg.AdminUser = strings.TrimSpace(os.Getenv("AUTH_ADMIN_USERNAME"))
	cfg.AdminPass = os.Getenv("AUTH_ADMIN_PASSWORD")

	return cfg
}
