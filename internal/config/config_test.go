package config

import (
	"os"
	"reflect"
	"testing"
)

func TestParseList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{",,,", nil},
		{"https://a.com", []string{"https://a.com"}},
		{"https://a.com,https://b.com", []string{"https://a.com", "https://b.com"}},
		{"  https://a.com  ,  https://b.com  ", []string{"https://a.com", "https://b.com"}},
		{"a,,b", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := parseList(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseList(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("X_TEST_DEFINED", "value")
	if got := envOr("X_TEST_DEFINED", "fallback"); got != "value" {
		t.Errorf("envOr defined: got %q, want %q", got, "value")
	}
	if got := envOr("X_TEST_UNDEFINED_KEY_QWERTY", "fallback"); got != "fallback" {
		t.Errorf("envOr undefined: got %q, want %q", got, "fallback")
	}
}

func TestEnvBool(t *testing.T) {
	trueValues := []string{"1", "true", "TRUE", "t", "yes", "y", "on", " on "}
	for _, value := range trueValues {
		t.Setenv("X_TEST_BOOL", value)
		if !envBool("X_TEST_BOOL") {
			t.Fatalf("envBool(%q) = false, want true", value)
		}
	}

	falseValues := []string{"", "0", "false", "off", "no", "unexpected"}
	for _, value := range falseValues {
		t.Setenv("X_TEST_BOOL", value)
		if envBool("X_TEST_BOOL") {
			t.Fatalf("envBool(%q) = true, want false", value)
		}
	}
}

func TestLoadDatabaseOnly(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://track:track@localhost:5432/track?sslmode=disable")
	t.Setenv("TRACK_SLASH_STORAGE_BACKEND", "s3")

	cfg, err := LoadDatabase()
	if err != nil {
		t.Fatalf("LoadDatabase: %v", err)
	}
	if cfg.DatabaseURL != "postgres://track:track@localhost:5432/track?sslmode=disable" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestLoadDatabaseRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if _, err := LoadDatabase(); err == nil {
		t.Fatal("LoadDatabase err = nil, want error")
	}
}

func TestLoadDevReload(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://track:track@localhost:5432/track?sslmode=disable")
	t.Setenv("TRACK_SLASH_DEV_RELOAD", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.DevReload {
		t.Fatal("DevReload = false, want true")
	}
}

func TestLoadAutoMigrateDefault(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://track:track@localhost:5432/track?sslmode=disable")
	t.Setenv("TRACK_SLASH_STORAGE_BACKEND", "local")
	t.Setenv("TRACK_SLASH_STORAGE_LOCAL_ROOT", "tmp/storage")
	t.Setenv("TRACK_SLASH_STORAGE_BUCKET", "local")
	unsetenv(t, "TRACK_SLASH_AUTO_MIGRATE")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.AutoMigrate {
		t.Fatal("AutoMigrate = false, want true")
	}
}

func TestLoadAutoMigrateOverride(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://track:track@localhost:5432/track?sslmode=disable")
	t.Setenv("TRACK_SLASH_AUTO_MIGRATE", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AutoMigrate {
		t.Fatal("AutoMigrate = true, want false")
	}
}

func TestLoadAutoMigrateValidation(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://track:track@localhost:5432/track?sslmode=disable")
	t.Setenv("TRACK_SLASH_AUTO_MIGRATE", "sometimes")

	if _, err := Load(); err == nil {
		t.Fatal("Load err = nil, want error")
	}
}

func TestLoadPublicOrigin(t *testing.T) {
	unsetenv(t, "TRACK_SLASH_PUBLIC_ORIGIN")
	got, err := loadPublicOrigin()
	if err != nil {
		t.Fatalf("loadPublicOrigin unset: %v", err)
	}
	if got != "" {
		t.Fatalf("unset public origin = %q, want empty", got)
	}

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "https", raw: " https://Track.Example.COM ", want: "https://track.example.com"},
		{name: "https port", raw: "https://track.example.com:8443/", want: "https://track.example.com:8443"},
		{name: "localhost http", raw: "http://localhost:8080", want: "http://localhost:8080"},
		{name: "loopback http", raw: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TRACK_SLASH_PUBLIC_ORIGIN", tc.raw)
			got, err := loadPublicOrigin()
			if err != nil {
				t.Fatalf("loadPublicOrigin: %v", err)
			}
			if got != tc.want {
				t.Fatalf("origin = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadPublicOriginRejectsInvalidOrigins(t *testing.T) {
	for _, raw := range []string{
		"track.example.com",
		"http://track.example.com",
		"https://track.example.com/app",
		"https://track.example.com?x=1",
		"https://user@track.example.com",
		"ftp://track.example.com",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("TRACK_SLASH_PUBLIC_ORIGIN", raw)
			if _, err := loadPublicOrigin(); err == nil {
				t.Fatal("loadPublicOrigin err = nil, want error")
			}
		})
	}
}

func TestLoadStorageDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://track:track@localhost:5432/track?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.Backend != "local" {
		t.Fatalf("Storage.Backend = %q, want local", cfg.Storage.Backend)
	}
	if cfg.Storage.LocalRoot != DefaultLocalStorageRoot {
		t.Fatalf("Storage.LocalRoot = %q, want %q", cfg.Storage.LocalRoot, DefaultLocalStorageRoot)
	}
	if cfg.Storage.Bucket != "local" {
		t.Fatalf("Storage.Bucket = %q, want local", cfg.Storage.Bucket)
	}
	if cfg.Storage.MaxUploadBytes != 52428800 {
		t.Fatalf("Storage.MaxUploadBytes = %d, want 52428800", cfg.Storage.MaxUploadBytes)
	}
	if cfg.Storage.S3Endpoint != "" || cfg.Storage.S3Region != "" || cfg.Storage.S3ForcePathStyle {
		t.Fatalf("S3 storage defaults = endpoint %q region %q force=%v, want zero values", cfg.Storage.S3Endpoint, cfg.Storage.S3Region, cfg.Storage.S3ForcePathStyle)
	}
}

func unsetenv(t *testing.T, key string) {
	t.Helper()
	old, ok := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%q): %v", key, err)
	}
	t.Cleanup(func() {
		if ok {
			if err := os.Setenv(key, old); err != nil {
				t.Fatalf("Setenv(%q): %v", key, err)
			}
			return
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv(%q): %v", key, err)
		}
	})
}

func TestLoadStorageOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://track:track@localhost:5432/track?sslmode=disable")
	t.Setenv("TRACK_SLASH_STORAGE_BACKEND", " LOCAL ")
	t.Setenv("TRACK_SLASH_STORAGE_LOCAL_ROOT", " /var/lib/track-slash/storage ")
	t.Setenv("TRACK_SLASH_STORAGE_BUCKET", " media ")
	t.Setenv("TRACK_SLASH_STORAGE_MAX_UPLOAD_BYTES", "1234")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.Backend != "local" || cfg.Storage.LocalRoot != "/var/lib/track-slash/storage" || cfg.Storage.Bucket != "media" || cfg.Storage.MaxUploadBytes != 1234 {
		t.Fatalf("Storage = %+v", cfg.Storage)
	}
}

func TestLoadS3Storage(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://track:track@localhost:5432/track?sslmode=disable")
	t.Setenv("TRACK_SLASH_STORAGE_BACKEND", " S3 ")
	t.Setenv("TRACK_SLASH_STORAGE_BUCKET", " track-slash ")
	t.Setenv("TRACK_SLASH_STORAGE_S3_ENDPOINT", " http://garage:3900 ")
	t.Setenv("TRACK_SLASH_STORAGE_MAX_UPLOAD_BYTES", "1234")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.Backend != "s3" || cfg.Storage.Bucket != "track-slash" || cfg.Storage.S3Endpoint != "http://garage:3900" || cfg.Storage.S3Region != "us-east-1" || !cfg.Storage.S3ForcePathStyle || cfg.Storage.MaxUploadBytes != 1234 {
		t.Fatalf("Storage = %+v", cfg.Storage)
	}
}

func TestLoadS3StorageOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://track:track@localhost:5432/track?sslmode=disable")
	t.Setenv("TRACK_SLASH_STORAGE_BACKEND", "s3")
	t.Setenv("TRACK_SLASH_STORAGE_BUCKET", "track-slash")
	t.Setenv("TRACK_SLASH_STORAGE_S3_ENDPOINT", "http://garage:3900")
	t.Setenv("TRACK_SLASH_STORAGE_S3_REGION", "eu-west-1")
	t.Setenv("TRACK_SLASH_STORAGE_S3_FORCE_PATH_STYLE", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.S3Region != "eu-west-1" || cfg.Storage.S3ForcePathStyle {
		t.Fatalf("S3 config = region %q force=%v, want eu-west-1/false", cfg.Storage.S3Region, cfg.Storage.S3ForcePathStyle)
	}
}

func TestLoadStorageValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		val  string
	}{
		{name: "invalid backend", key: "TRACK_SLASH_STORAGE_BACKEND", val: "gcs"},
		{name: "empty local root", key: "TRACK_SLASH_STORAGE_LOCAL_ROOT", val: ""},
		{name: "empty bucket", key: "TRACK_SLASH_STORAGE_BUCKET", val: ""},
		{name: "bad max", key: "TRACK_SLASH_STORAGE_MAX_UPLOAD_BYTES", val: "nope"},
		{name: "zero max", key: "TRACK_SLASH_STORAGE_MAX_UPLOAD_BYTES", val: "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://track:track@localhost:5432/track?sslmode=disable")
			t.Setenv(tc.key, tc.val)
			if _, err := Load(); err == nil {
				t.Fatal("Load err = nil, want error")
			}
		})
	}
}

func TestLoadS3StorageValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
	}{
		{
			name: "missing bucket",
			env: map[string]string{
				"TRACK_SLASH_STORAGE_S3_ENDPOINT": "http://garage:3900",
			},
		},
		{
			name: "missing endpoint",
			env: map[string]string{
				"TRACK_SLASH_STORAGE_BUCKET": "track-slash",
			},
		},
		{
			name: "empty region",
			env: map[string]string{
				"TRACK_SLASH_STORAGE_BUCKET":      "track-slash",
				"TRACK_SLASH_STORAGE_S3_ENDPOINT": "http://garage:3900",
				"TRACK_SLASH_STORAGE_S3_REGION":   "",
			},
		},
		{
			name: "bad force path style",
			env: map[string]string{
				"TRACK_SLASH_STORAGE_BUCKET":              "track-slash",
				"TRACK_SLASH_STORAGE_S3_ENDPOINT":         "http://garage:3900",
				"TRACK_SLASH_STORAGE_S3_FORCE_PATH_STYLE": "sometimes",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://track:track@localhost:5432/track?sslmode=disable")
			t.Setenv("TRACK_SLASH_STORAGE_BACKEND", "s3")
			for key, val := range tc.env {
				t.Setenv(key, val)
			}
			if _, err := Load(); err == nil {
				t.Fatal("Load err = nil, want error")
			}
		})
	}
}
