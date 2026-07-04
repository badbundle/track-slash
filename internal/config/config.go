package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port               string
	DatabaseURL        string
	CORSAllowedOrigins []string
	DevReload          bool
	MCPEnabled         bool
	Storage            StorageConfig
}

type StorageConfig struct {
	Backend        string
	LocalRoot      string
	Bucket         string
	MaxUploadBytes int64
}

func Load() (Config, error) {
	storage, err := loadStorageConfig()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Port:               envOr("PORT", "8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		CORSAllowedOrigins: parseList(os.Getenv("CORS_ALLOWED_ORIGINS")),
		DevReload:          envBool("TRACK_SLASH_DEV_RELOAD"),
		MCPEnabled:         envBool("TRACK_SLASH_MCP_ENABLED"),
		Storage:            storage,
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

func loadStorageConfig() (StorageConfig, error) {
	backend := strings.ToLower(strings.TrimSpace(envOrLookup("TRACK_SLASH_STORAGE_BACKEND", "local")))
	if backend != "local" {
		return StorageConfig{}, errors.New("TRACK_SLASH_STORAGE_BACKEND must be local")
	}
	localRoot := strings.TrimSpace(envOrLookup("TRACK_SLASH_STORAGE_LOCAL_ROOT", "tmp/storage"))
	if localRoot == "" {
		return StorageConfig{}, errors.New("TRACK_SLASH_STORAGE_LOCAL_ROOT is required")
	}
	bucket := strings.TrimSpace(envOrLookup("TRACK_SLASH_STORAGE_BUCKET", "local"))
	if bucket == "" {
		return StorageConfig{}, errors.New("TRACK_SLASH_STORAGE_BUCKET is required")
	}
	maxUploadBytes, err := envPositiveInt64("TRACK_SLASH_STORAGE_MAX_UPLOAD_BYTES", 52428800)
	if err != nil {
		return StorageConfig{}, err
	}
	return StorageConfig{
		Backend:        backend,
		LocalRoot:      localRoot,
		Bucket:         bucket,
		MaxUploadBytes: maxUploadBytes,
	}, nil
}

func envOrLookup(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func envPositiveInt64(key string, fallback int64) (int64, error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return 0, errors.New(key + " must be a positive integer")
	}
	return n, nil
}
