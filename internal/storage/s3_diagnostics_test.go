package storage

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/smithy-go"
)

func TestS3ErrorIncludesCredentialSafeDiagnostics(t *testing.T) {
	responseBody, err := xml.Marshal(struct {
		XMLName      xml.Name `xml:"Error"`
		Code         string   `xml:"Code"`
		Message      string   `xml:"Message"`
		Details      string   `xml:"Details"`
		Canonical    string   `xml:"CanonicalRequest"`
		StringToSign string   `xml:"StringToSign"`
		RequestID    string   `xml:"RequestId"`
		HostID       string   `xml:"HostId"`
	}{
		Code:         "SignatureDoesNotMatch",
		Message:      "Access denied.",
		Details:      "The request signature did not match.",
		Canonical:    "PUT\n/bucket/key\nX-Amz-Credential=visible-access-id&X-Amz-Signature=visible-signature&X-Amz-Security-Token=visible-query-token&x-id=PutObject\nauthorization:visible-authorization\ncookie:visible-cookie\nhost:storage.googleapis.com\nx-amz-security-token:visible-header-token\nx-amz-server-side-encryption-customer-key:visible-customer-key\n\nhost\ncontent-hash",
		StringToSign: "AWS4-HMAC-SHA256\n20260710T120000Z\n20260710/auto/s3/aws4_request\nsafe-hash",
		RequestID:    "request-123",
		HostID:       "host-456",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("X-Guploader-Uploadid", "upload-789")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write(responseBody)
	}))
	t.Cleanup(server.Close)
	t.Setenv("AWS_ACCESS_KEY_ID", "diagnostic-access-id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "diagnostic-secret")

	backend, err := NewS3Backend(context.Background(), "bucket", S3Config{
		Endpoint:       server.URL,
		Region:         "auto",
		ForcePathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewS3Backend: %v", err)
	}
	_, err = backend.Put(context.Background(), "key", strings.NewReader("hello"), 10)
	if err == nil {
		t.Fatal("Put err = nil, want SignatureDoesNotMatch")
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Put err = %T, want wrapped smithy.APIError", err)
	}

	got := err.Error()
	for _, want := range []string{
		`operation="PutObject"`,
		`method="PUT"`,
		`path="/bucket/key"`,
		`status=403`,
		`api_code="SignatureDoesNotMatch"`,
		`request_id="request-123"`,
		`host_id="host-456"`,
		`upload_id="upload-789"`,
		`credential_scope="`,
		`signed_headers="`,
		`details="The request signature did not match."`,
		`canonical_request="PUT\n/bucket/key`,
		`string_to_sign="AWS4-HMAC-SHA256`,
		`%5BREDACTED%5D`,
		`authorization:[REDACTED]`,
		`cookie:[REDACTED]`,
		`x-amz-security-token:[REDACTED]`,
		`x-amz-server-side-encryption-customer-key:[REDACTED]`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("error missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{
		"diagnostic-access-id",
		"diagnostic-secret",
		"visible-access-id",
		"visible-signature",
		"visible-query-token",
		"visible-authorization",
		"visible-cookie",
		"visible-header-token",
		"visible-customer-key",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("error leaked %q: %s", forbidden, got)
		}
	}
}

func TestS3DiagnosticHTTPClientReturnsTransportError(t *testing.T) {
	wantErr := errors.New("transport failed")
	client := &s3DiagnosticHTTPClient{client: httpClientFunc(func(*http.Request) (*http.Response, error) {
		return nil, wantErr
	})}

	response, err := client.Do(httptest.NewRequest(http.MethodGet, "http://example.com", nil))
	if response != nil || !errors.Is(err, wantErr) {
		t.Fatalf("Do = response %#v, err %v; want nil, %v", response, err, wantErr)
	}
}

func TestS3DiagnosticBodyCaptureLimit(t *testing.T) {
	want := strings.Repeat("x", maxS3DiagnosticBodyBytes+10)
	body := newS3DiagnosticBody(io.NopCloser(strings.NewReader(want)))
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != want {
		t.Fatalf("read body length = %d, want %d", len(got), len(want))
	}
	if len(body.bytes()) != maxS3DiagnosticBodyBytes || !body.truncated {
		t.Fatalf("captured length = %d, truncated = %v", len(body.bytes()), body.truncated)
	}

	fields := appendS3ResponseBodyDiagnostics(nil, body)
	joined := strings.Join(fields, " ")
	if !strings.Contains(joined, "response_body_captured_bytes=65536") || !strings.Contains(joined, "response_body_truncated=true") {
		t.Fatalf("diagnostic fields = %q", joined)
	}
	if strings.Contains(joined, "details=") || strings.Contains(joined, "canonical_request=") || strings.Contains(joined, "string_to_sign=") {
		t.Fatalf("invalid XML produced parsed diagnostics: %q", joined)
	}
}

func TestS3DiagnosticHelpers(t *testing.T) {
	plainErr := errors.New("plain")
	if got := withS3Diagnostics(plainErr); got != plainErr {
		t.Fatalf("withS3Diagnostics(plain) = %v, want original error", got)
	}

	query := url.Values{"z": {"1"}, "a": {"2"}}
	if got := sortedQueryKeys(query); got != "a,z" {
		t.Fatalf("sortedQueryKeys = %q, want a,z", got)
	}

	scope, headers := sigV4AuthorizationMetadata("AWS4-HMAC-SHA256 Credential=access/20260710/auto/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=secret")
	if scope != "20260710/auto/s3/aws4_request" || headers != "host;x-amz-date" {
		t.Fatalf("authorization metadata = %q, %q", scope, headers)
	}
	scope, headers = sigV4AuthorizationMetadata("Credential=, SignedHeaders=")
	if scope != "" || headers != "" {
		t.Fatalf("malformed authorization metadata = %q, %q", scope, headers)
	}

	short := "short"
	if got := truncateS3DiagnosticField(short); got != short {
		t.Fatalf("short diagnostic = %q", got)
	}
	long := strings.Repeat("x", maxS3DiagnosticFieldBytes+1)
	if got := truncateS3DiagnosticField(long); len(got) <= maxS3DiagnosticFieldBytes || !strings.HasSuffix(got, "...[truncated]") {
		t.Fatalf("long diagnostic was not marked truncated")
	}
}

type httpClientFunc func(*http.Request) (*http.Response, error)

func (f httpClientFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}
