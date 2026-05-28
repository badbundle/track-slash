package server

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseLimit(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"", DefaultLimit, false},
		{"1", 1, false},
		{"50", 50, false},
		{"200", 200, false},
		{"201", MaxLimit, false}, // clamped
		{"999999", MaxLimit, false},
		{"0", 0, true},
		{"-1", 0, true},
		{"abc", 0, true},
		{"1.5", 0, true},
	}
	for _, c := range cases {
		got, err := parseLimit(c.in)
		if c.wantErr {
			if !errors.Is(err, errInvalidLimit) {
				t.Errorf("parseLimit(%q) err = %v, want errInvalidLimit", c.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseLimit(%q) unexpected err = %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseLimit(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	type sample struct {
		N int    `json:"n"`
		S string `json:"s"`
	}
	in := sample{N: 7, S: "hi"}
	enc := encodeCursor(in)
	if enc == "" {
		t.Fatal("empty encoding")
	}
	var out sample
	if err := decodeCursor(enc, &out); err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip: got %+v, want %+v", out, in)
	}
}

func TestDecodeCursorErrors(t *testing.T) {
	var v map[string]any
	if err := decodeCursor("", &v); err != nil {
		t.Errorf("empty cursor: err = %v, want nil", err)
	}
	if err := decodeCursor("!!!not-base64!!!", &v); !errors.Is(err, errInvalidCursor) {
		t.Errorf("bad base64: err = %v, want errInvalidCursor", err)
	}
	notJSON := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	if err := decodeCursor(notJSON, &v); !errors.Is(err, errInvalidCursor) {
		t.Errorf("bad json: err = %v, want errInvalidCursor", err)
	}
}

func TestWritePage(t *testing.T) {
	t.Run("with cursor", func(t *testing.T) {
		rec := httptest.NewRecorder()
		next := "abc"
		writePage(rec, []int{1, 2, 3}, &next)
		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d", rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q", got)
		}
		got := rec.Body.String()
		want := `{"items":[1,2,3],"next_cursor":"abc"}` + "\n"
		if got != want {
			t.Fatalf("body = %q, want %q", got, want)
		}
	})
	t.Run("nil cursor", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writePage(rec, []int{}, nil)
		got := rec.Body.String()
		want := `{"items":[],"next_cursor":null}` + "\n"
		if got != want {
			t.Fatalf("body = %q, want %q", got, want)
		}
	})
}
