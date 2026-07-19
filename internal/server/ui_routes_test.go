package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUIRootSupportsHeadHealthChecks(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	New(nil, nil, nil).Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("HEAD / status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("HEAD / body = %q, want empty", recorder.Body.String())
	}
}
