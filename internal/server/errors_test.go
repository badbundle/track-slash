package server

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteStoreErrorLogsInternalDetails(t *testing.T) {
	logBuffer := captureStandardLog(t)

	recorder := httptest.NewRecorder()
	writeStoreError(recorder, errors.New("backend exploded"))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if strings.Contains(recorder.Body.String(), "backend exploded") {
		t.Fatalf("response leaked internal error detail: %q", recorder.Body.String())
	}

	logOutput := logBuffer.String()
	if !strings.Contains(logOutput, `source="store"`) {
		t.Fatalf("log output missing source: %q", logOutput)
	}
	if !strings.Contains(logOutput, `error="backend exploded"`) {
		t.Fatalf("log output missing error detail: %q", logOutput)
	}
}

func TestWriteStorageErrorLogsInternalDetails(t *testing.T) {
	logBuffer := captureStandardLog(t)

	recorder := httptest.NewRecorder()
	writeStorageError(recorder, errors.New("signature mismatch"))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if strings.Contains(recorder.Body.String(), "signature mismatch") {
		t.Fatalf("response leaked internal error detail: %q", recorder.Body.String())
	}

	logOutput := logBuffer.String()
	if !strings.Contains(logOutput, `source="object storage"`) {
		t.Fatalf("log output missing source: %q", logOutput)
	}
	if !strings.Contains(logOutput, `error="signature mismatch"`) {
		t.Fatalf("log output missing error detail: %q", logOutput)
	}
}

func captureStandardLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logBuffer bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&logBuffer)
	t.Cleanup(func() {
		log.SetOutput(originalOutput)
	})
	return &logBuffer
}
