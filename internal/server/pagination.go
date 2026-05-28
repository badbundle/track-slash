package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// page is the JSON envelope returned by every list endpoint.
type page struct {
	Items      any     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// errInvalidLimit and errInvalidCursor map to 400 at the handler boundary.
var (
	errInvalidLimit  = errors.New("limit must be a positive integer")
	errInvalidCursor = errors.New("invalid cursor")
)

// parseLimit reads ?limit= and returns a clamped, validated limit. An empty
// param yields DefaultLimit; values above MaxLimit are clamped down; zero or
// negative values are rejected so a buggy client sees the bug instead of an
// empty page.
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return DefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, errInvalidLimit
	}
	if n > MaxLimit {
		n = MaxLimit
	}
	return n, nil
}

// decodeCursor base64-decodes raw into v. An empty cursor is a no-op so the
// caller can pass through "first page" uniformly.
func decodeCursor(raw string, v any) error {
	if raw == "" {
		return nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return errInvalidCursor
	}
	if err := json.Unmarshal(data, v); err != nil {
		return errInvalidCursor
	}
	return nil
}

// encodeCursor marshals v to base64 JSON. Cursors are opaque to clients;
// the schema is server-private so ordering can change without breaking them.
func encodeCursor(v any) string {
	data, _ := json.Marshal(v) // defensive: stdlib only fails on cyclic/unsupported types
	return base64.RawURLEncoding.EncodeToString(data)
}

func writePage(w http.ResponseWriter, items any, nextCursor *string) {
	writeJSON(w, http.StatusOK, page{Items: items, NextCursor: nextCursor})
}
