package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	Port               string
	DatabaseURL        string
	CORSAllowedOrigins []string
	DevReload          bool
}

func Load() (Config, error) {
	cfg := Config{
		Port:               envOr("PORT", "8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		CORSAllowedOrigins: parseList(os.Getenv("CORS_ALLOWED_ORIGINS")),
		DevReload:          envBool("TRACK_SLASH_DEV_RELOAD"),
	}
	if cfg.DatabaseURL == "" {
		return cfg, errors.New("DATABASE_URL is required")
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string) bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv(key))) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// parseList splits a comma-separated env var, trims each element, and drops
// empties. Returns nil for "" so callers can do `len() == 0` to detect off.
func parseList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
