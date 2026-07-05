package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestS3BackendPutOpenDelete(t *testing.T) {
	fake := newFakeS3(t)
	backend := newTestS3Backend(t, fake.URL)

	info, err := backend.Put(context.Background(), "projects/p1/objects/o1", strings.NewReader("hello"), 10)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	wantHash := sha256.Sum256([]byte("hello"))
	if info.Size != 5 || info.SHA256 != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("info = %+v", info)
	}

	rc, err := backend.Open(context.Background(), "projects/p1/objects/o1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q, want hello", body)
	}

	if err := backend.Delete(context.Background(), "projects/p1/objects/o1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := backend.Open(context.Background(), "projects/p1/objects/o1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open deleted err = %v, want ErrNotFound", err)
	}
}

func TestS3BackendValidation(t *testing.T) {
	if _, err := NewS3Backend(context.Background(), "", S3Config{Endpoint: "http://example.com"}); err == nil {
		t.Fatal("NewS3Backend empty bucket err = nil, want error")
	}
	if _, err := NewS3Backend(context.Background(), "bucket", S3Config{}); err == nil {
		t.Fatal("NewS3Backend empty endpoint err = nil, want error")
	}

	fake := newFakeS3(t)
	backend := newTestS3Backend(t, fake.URL)
	for _, key := range []string{"", "/absolute", "../escape", "projects\\bad"} {
		if _, err := backend.Put(context.Background(), key, strings.NewReader("x"), 10); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Put key %q err = %v, want ErrInvalidKey", key, err)
		}
		if _, err := backend.Open(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Open key %q err = %v, want ErrInvalidKey", key, err)
		}
		if err := backend.Delete(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Delete key %q err = %v, want ErrInvalidKey", key, err)
		}
	}
	if _, err := backend.Put(context.Background(), "projects/p1/objects/o1", strings.NewReader("x"), 0); err == nil {
		t.Fatal("Put max 0 err = nil, want error")
	}
}

func TestS3BackendTooLargeDoesNotStore(t *testing.T) {
	fake := newFakeS3(t)
	backend := newTestS3Backend(t, fake.URL)
	key := "projects/p1/objects/large"
	if _, err := backend.Put(context.Background(), key, strings.NewReader("toolong"), 3); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Put too large err = %v, want ErrTooLarge", err)
	}
	if fake.has("bucket", key) {
		t.Fatal("too-large object was stored")
	}
}

func TestS3BackendReadErrorDoesNotStore(t *testing.T) {
	fake := newFakeS3(t)
	backend := newTestS3Backend(t, fake.URL)
	key := "projects/p1/objects/read-error"
	if _, err := backend.Put(context.Background(), key, errReader{}, 10); err == nil || !strings.Contains(err.Error(), "read storage source") {
		t.Fatalf("Put read error = %v, want wrapped read error", err)
	}
	if fake.has("bucket", key) {
		t.Fatal("read-error object was stored")
	}
}

func TestS3BackendPreventsOverwrite(t *testing.T) {
	fake := newFakeS3(t)
	backend := newTestS3Backend(t, fake.URL)
	key := "projects/p1/objects/o1"
	if _, err := backend.Put(context.Background(), key, strings.NewReader("first"), 10); err != nil {
		t.Fatalf("Put first: %v", err)
	}
	if _, err := backend.Put(context.Background(), key, strings.NewReader("second"), 10); !errors.Is(err, ErrExists) {
		t.Fatalf("Put overwrite err = %v, want ErrExists", err)
	}
	if got := fake.get("bucket", key); string(got) != "first" {
		t.Fatalf("stored body = %q, want first", got)
	}
}

func TestS3BackendOpenDeleteMissing(t *testing.T) {
	fake := newFakeS3(t)
	backend := newTestS3Backend(t, fake.URL)
	if _, err := backend.Open(context.Background(), "projects/p1/objects/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open missing err = %v, want ErrNotFound", err)
	}
	if err := backend.Delete(context.Background(), "projects/p1/objects/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete missing err = %v, want ErrNotFound", err)
	}
}

func newTestS3Backend(t *testing.T, endpoint string) *S3Backend {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	backend, err := NewS3Backend(context.Background(), "bucket", S3Config{
		Endpoint:       endpoint,
		ForcePathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewS3Backend: %v", err)
	}
	return backend
}

type fakeS3 struct {
	*httptest.Server
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeS3(t *testing.T) *fakeS3 {
	t.Helper()
	f := &fakeS3{objects: make(map[string][]byte)}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeS3) handle(w http.ResponseWriter, r *http.Request) {
	bucket, key, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if !ok || bucket == "" || key == "" {
		writeFakeS3Error(w, http.StatusNotFound, "NoSuchKey")
		return
	}
	storeKey := bucket + "/" + key

	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		if _, exists := f.objects[storeKey]; exists && r.Header.Get("If-None-Match") == "*" {
			writeFakeS3Error(w, http.StatusPreconditionFailed, "PreconditionFailed")
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeFakeS3Error(w, http.StatusInternalServerError, "InternalError")
			return
		}
		f.objects[storeKey] = body
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		body, exists := f.objects[storeKey]
		if !exists {
			writeFakeS3Error(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	case http.MethodHead:
		body, exists := f.objects[storeKey]
		if !exists {
			writeFakeS3Error(w, http.StatusNotFound, "NotFound")
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		if _, exists := f.objects[storeKey]; !exists {
			writeFakeS3Error(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		delete(f.objects, storeKey)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeFakeS3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func (f *fakeS3) has(bucket, key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[bucket+"/"+key]
	return ok
}

func (f *fakeS3) get(bucket, key string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.objects[bucket+"/"+key]...)
}

func writeFakeS3Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code><Message>%s</Message></Error>", code, code)
}
