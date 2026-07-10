package storage

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const (
	maxS3DiagnosticBodyBytes  = 64 << 10
	maxS3DiagnosticFieldBytes = 16 << 10
)

type s3DiagnosticHTTPClient struct {
	client aws.HTTPClient
}

func (c *s3DiagnosticHTTPClient) Do(request *http.Request) (*http.Response, error) {
	response, err := c.client.Do(request)
	if err != nil {
		return response, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		response.Body = newS3DiagnosticBody(response.Body)
	}
	return response, nil
}

type s3DiagnosticBody struct {
	io.ReadCloser
	buffer    bytes.Buffer
	truncated bool
}

func newS3DiagnosticBody(body io.ReadCloser) *s3DiagnosticBody {
	return &s3DiagnosticBody{ReadCloser: body}
}

func (b *s3DiagnosticBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	remaining := maxS3DiagnosticBodyBytes - b.buffer.Len()
	if remaining > 0 {
		capture := min(n, remaining)
		_, _ = b.buffer.Write(p[:capture])
	}
	if n > remaining {
		b.truncated = true
	}
	return n, err
}

func (b *s3DiagnosticBody) bytes() []byte {
	return append([]byte(nil), b.buffer.Bytes()...)
}

func withS3Diagnostics(err error) error {
	var responseErr *smithyhttp.ResponseError
	if !errors.As(err, &responseErr) || responseErr.Response == nil || responseErr.Response.Response == nil {
		return err
	}

	response := responseErr.Response.Response
	fields := make([]string, 0, 20)
	var operationErr *smithy.OperationError
	if errors.As(err, &operationErr) {
		fields = appendDiagnosticField(fields, "operation", operationErr.Operation())
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		fields = appendDiagnosticField(fields, "api_code", apiErr.ErrorCode())
		fields = appendDiagnosticField(fields, "api_message", apiErr.ErrorMessage())
	}
	var serviceErr s3.ResponseError
	if errors.As(err, &serviceErr) {
		fields = appendDiagnosticField(fields, "request_id", serviceErr.ServiceRequestID())
		fields = appendDiagnosticField(fields, "host_id", serviceErr.ServiceHostID())
	}
	fields = append(fields, fmt.Sprintf("status=%d", response.StatusCode))
	fields = appendDiagnosticField(fields, "upload_id", response.Header.Get("X-Guploader-Uploadid"))
	fields = appendDiagnosticField(fields, "response_content_type", response.Header.Get("Content-Type"))
	if response.Request != nil {
		fields = appendS3RequestDiagnostics(fields, response.Request)
	}
	if body, ok := response.Body.(*s3DiagnosticBody); ok {
		fields = appendS3ResponseBodyDiagnostics(fields, body)
	}
	return fmt.Errorf("%w; s3 diagnostics: %s", err, strings.Join(fields, " "))
}

func appendS3RequestDiagnostics(fields []string, request *http.Request) []string {
	credentialScope, signedHeaders := sigV4AuthorizationMetadata(request.Header.Get("Authorization"))
	fields = appendDiagnosticField(fields, "method", request.Method)
	fields = appendDiagnosticField(fields, "host", request.URL.Host)
	fields = appendDiagnosticField(fields, "path", request.URL.EscapedPath())
	fields = appendDiagnosticField(fields, "query_keys", sortedQueryKeys(request.URL.Query()))
	fields = appendDiagnosticField(fields, "credential_scope", credentialScope)
	fields = appendDiagnosticField(fields, "signed_headers", signedHeaders)
	fields = appendDiagnosticField(fields, "accept_encoding", request.Header.Get("Accept-Encoding"))
	return appendDiagnosticField(fields, "generation_match", request.Header.Get("X-Goog-If-Generation-Match"))
}

func appendS3ResponseBodyDiagnostics(fields []string, body *s3DiagnosticBody) []string {
	captured := body.bytes()
	fields = append(fields,
		fmt.Sprintf("response_body_captured_bytes=%d", len(captured)),
		fmt.Sprintf("response_body_truncated=%t", body.truncated),
	)

	var response struct {
		Details          string `xml:"Details"`
		CanonicalRequest string `xml:"CanonicalRequest"`
		StringToSign     string `xml:"StringToSign"`
	}
	if xml.Unmarshal(captured, &response) != nil {
		return fields
	}
	fields = appendDiagnosticField(fields, "details", truncateS3DiagnosticField(strings.TrimSpace(response.Details)))
	fields = appendDiagnosticField(fields, "canonical_request", truncateS3DiagnosticField(redactS3CanonicalRequest(response.CanonicalRequest)))
	return appendDiagnosticField(fields, "string_to_sign", truncateS3DiagnosticField(strings.TrimSpace(response.StringToSign)))
}

func appendDiagnosticField(fields []string, name, value string) []string {
	return append(fields, fmt.Sprintf("%s=%q", name, value))
}

func sortedQueryKeys(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func sigV4AuthorizationMetadata(authorization string) (credentialScope, signedHeaders string) {
	for _, part := range strings.Split(authorization, ",") {
		part = strings.TrimSpace(part)
		if index := strings.Index(part, "Credential="); index >= 0 {
			fields := strings.Fields(part[index+len("Credential="):])
			if len(fields) > 0 {
				if slash := strings.IndexByte(fields[0], '/'); slash >= 0 {
					credentialScope = fields[0][slash+1:]
				}
			}
		}
		if index := strings.Index(part, "SignedHeaders="); index >= 0 {
			fields := strings.Fields(part[index+len("SignedHeaders="):])
			if len(fields) > 0 {
				signedHeaders = fields[0]
			}
		}
	}
	return credentialScope, signedHeaders
}

func redactS3CanonicalRequest(value string) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	if len(lines) > 2 {
		if query, err := url.ParseQuery(lines[2]); err == nil {
			for key := range query {
				lowerKey := strings.ToLower(key)
				if strings.Contains(lowerKey, "credential") || strings.Contains(lowerKey, "signature") || strings.Contains(lowerKey, "token") {
					query.Set(key, "[REDACTED]")
				}
			}
			lines[2] = query.Encode()
		}
	}
	for index, line := range lines {
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		header := strings.ToLower(strings.TrimSpace(line[:colon]))
		if header == "authorization" || header == "cookie" || strings.Contains(header, "security-token") || strings.Contains(header, "customer-key") {
			lines[index] = line[:colon+1] + "[REDACTED]"
		}
	}
	return strings.Join(lines, "\n")
}

func truncateS3DiagnosticField(value string) string {
	if len(value) <= maxS3DiagnosticFieldBytes {
		return value
	}
	return value[:maxS3DiagnosticFieldBytes] + "...[truncated]"
}
