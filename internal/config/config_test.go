package config

import (
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

func TestLoadStorageDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://track:track@localhost:5432/track?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.Backend != "local" {
		t.Fatalf("Storage.Backend = %q, want local", cfg.Storage.Backend)
	}
	if cfg.Storage.LocalRoot != "tmp/storage" {
		t.Fatalf("Storage.LocalRoot = %q, want tmp/storage", cfg.Storage.LocalRoot)
	}
	if cfg.Storage.Bucket != "local" {
		t.Fatalf("Storage.Bucket = %q, want local", cfg.Storage.Bucket)
	}
	if cfg.Storage.MaxUploadBytes != 52428800 {
		t.Fatalf("Storage.MaxUploadBytes = %d, want 52428800", cfg.Storage.MaxUploadBytes)
	}
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
