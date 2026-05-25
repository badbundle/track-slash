package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusCreated, map[string]any{"hello": "world"})

	if got, want := rec.Code, http.StatusCreated; got != want {
		t.Fatalf("status: got %d, want %d", got, want)
	}
	if got, want := rec.Header().Get("Content-Type"), "application/json"; got != want {
		t.Fatalf("content-type: got %q, want %q", got, want)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["hello"] != "world" {
		t.Fatalf("body: got %v", body)
	}
}

func TestDecodeJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	t.Run("valid", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"name":"alice"}`))
		var p payload
		if err := decodeJSON(r, &p); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if p.Name != "alice" {
			t.Fatalf("got %q, want %q", p.Name, "alice")
		}
	})

	t.Run("unknown field rejected", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"name":"alice","extra":1}`))
		var p payload
		if err := decodeJSON(r, &p); err == nil {
			t.Fatal("expected error for unknown field, got nil")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(nil))
		var p payload
		if err := decodeJSON(r, &p); err == nil {
			t.Fatal("expected error for empty body, got nil")
		}
	})
}
