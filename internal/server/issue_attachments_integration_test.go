package server_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func (e *httpEnv) issueAttachmentsPath(iss model.Issue) string {
	return e.issuePath(iss) + "/attachments"
}

func (e *httpEnv) issueAttachmentPath(iss model.Issue, object model.StorageObject) string {
	return e.issueAttachmentsPath(iss) + "/" + object.Ref
}

func (e *httpEnv) doMultipartPath(t *testing.T, token, path, filename string, content []byte) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}
	req, err := http.NewRequestWithContext(e.ctx, http.MethodPost, e.ts.URL+apiPath(path), &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do multipart: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res.StatusCode, body
}

func (e *httpEnv) doMultipartPathWithoutFile(t *testing.T, token, path string) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("title", "no file"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}
	req, err := http.NewRequestWithContext(e.ctx, http.MethodPost, e.ts.URL+apiPath(path), &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do multipart: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res.StatusCode, body
}

func TestHTTPIssueAttachmentCRUD(t *testing.T) {
	t.Parallel()
	e, root := newStorageHTTPEnv(t, 1024)
	issue := e.mustCreateIssue(t, "Attachment API")
	content := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("image-bytes")...)
	sum := sha256.Sum256(content)
	wantSHA := hex.EncodeToString(sum[:])

	code, body := e.doMultipartPath(t, e.authToken, e.issueAttachmentsPath(issue), "photo.png", content)
	if code != http.StatusCreated {
		t.Fatalf("upload code = %d body = %s", code, body)
	}
	attachment := decode[model.IssueAttachment](t, body)
	if attachment.IssueID != issue.ID || attachment.ProjectID != e.projectID || attachment.CreatedByID != e.adminID || attachment.StorageObjectID != attachment.Object.ID {
		t.Fatalf("attachment = %+v", attachment)
	}
	if attachment.Object.Ref != "object-1" || attachment.Object.Filename != "photo.png" || attachment.Object.ContentType != "image/png" || attachment.Object.ByteSize != int64(len(content)) || attachment.Object.SHA256 != wantSHA {
		t.Fatalf("attachment object = %+v", attachment.Object)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(attachment.Object.ObjectKey))); err != nil {
		t.Fatalf("stored file stat: %v", err)
	}

	code, body = e.doMultipartPath(t, e.authToken, e.issueAttachmentsPath(issue), "notes.txt", []byte("second"))
	if code != http.StatusCreated {
		t.Fatalf("second upload code = %d body = %s", code, body)
	}
	second := decode[model.IssueAttachment](t, body)
	if second.Object.Ref != "object-2" {
		t.Fatalf("second object ref = %q, want object-2", second.Object.Ref)
	}

	code, body = e.do(t, http.MethodGet, e.issueAttachmentsPath(issue)+"?limit=1", nil)
	if code != http.StatusOK {
		t.Fatalf("list code = %d body = %s", code, body)
	}
	page := decodePage[model.IssueAttachment](t, body)
	if len(page.Items) != 1 || page.Items[0].ID != attachment.ID || page.NextCursor == nil {
		t.Fatalf("page = %+v", page)
	}
	code, body = e.do(t, http.MethodGet, e.issueAttachmentsPath(issue)+"?cursor="+*page.NextCursor+"&limit=1", nil)
	if code != http.StatusOK {
		t.Fatalf("list page2 code = %d body = %s", code, body)
	}
	page = decodePage[model.IssueAttachment](t, body)
	if len(page.Items) != 1 || page.Items[0].ID != second.ID || page.NextCursor != nil {
		t.Fatalf("page2 = %+v", page)
	}

	res, body := e.doRaw(t, e.authToken, http.MethodGet, e.issueAttachmentPath(issue, attachment.Object)+"/content", nil, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("content code = %d body = %s", res.StatusCode, body)
	}
	if !bytes.Equal(body, content) {
		t.Fatalf("content body = %q, want upload bytes", body)
	}
	if got := res.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if got := res.Header.Get("Content-Length"); got != strconv.Itoa(len(content)) {
		t.Fatalf("Content-Length = %q, want %d", got, len(content))
	}
	if got := res.Header.Get("ETag"); got != `"`+wantSHA+`"` {
		t.Fatalf("ETag = %q, want sha", got)
	}
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := res.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") || !strings.Contains(got, "photo.png") {
		t.Fatalf("Content-Disposition = %q, want attachment filename", got)
	}
	res, body = e.doRaw(t, e.authToken, http.MethodGet, e.issueAttachmentPath(issue, attachment.Object)+"/content?inline=1", nil, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("inline content code = %d body = %s", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "inline") || !strings.Contains(got, "photo.png") {
		t.Fatalf("inline Content-Disposition = %q, want inline filename", got)
	}
	res, body = e.doRaw(t, e.authToken, http.MethodGet, e.issueAttachmentPath(issue, second.Object)+"/content?inline=1", nil, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("text inline content code = %d body = %s", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Fatalf("text inline Content-Disposition = %q, want attachment", got)
	}

	code, body = e.do(t, http.MethodDelete, e.issueAttachmentPath(issue, attachment.Object), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete code = %d body = %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(attachment.Object.ObjectKey))); !os.IsNotExist(err) {
		t.Fatalf("deleted file stat err = %v, want not exist", err)
	}
	code, body = e.do(t, http.MethodGet, e.issueAttachmentPath(issue, attachment.Object)+"/content", nil)
	if code != http.StatusNotFound {
		t.Fatalf("content deleted code = %d body = %s", code, body)
	}
	if _, err := e.store.GetStorageObjectByProjectNumber(e.ctx, e.projectID, attachment.Object.Number); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetStorageObjectByProjectNumber deleted err = %v, want ErrNotFound", err)
	}
}

func TestHTTPIssueAttachmentAccessAndValidation(t *testing.T) {
	t.Parallel()
	e, _ := newStorageHTTPEnv(t, 8)
	issue := e.mustCreateIssue(t, "Attachment validation")
	code, body := e.doMultipartPath(t, e.authToken, e.issueAttachmentsPath(issue), "photo.png", []byte("12345678"))
	if code != http.StatusCreated {
		t.Fatalf("admin upload code = %d body = %s", code, body)
	}
	attachment := decode[model.IssueAttachment](t, body)

	code, body = e.doUnauth(t, http.MethodGet, e.issueAttachmentsPath(issue), nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("unauth list code = %d body = %s", code, body)
	}
	code, body = e.doMultipartPath(t, "", e.issueAttachmentsPath(issue), "photo.png", []byte("12345678"))
	if code != http.StatusUnauthorized {
		t.Fatalf("unauth upload code = %d body = %s", code, body)
	}
	_, outsiderToken := e.mustUserToken(t, "attachment-outsider")
	code, body = e.doWithToken(t, outsiderToken, http.MethodGet, e.issueAttachmentsPath(issue), nil)
	if code != http.StatusForbidden {
		t.Fatalf("outsider list code = %d body = %s", code, body)
	}
	code, body = e.doMultipartPath(t, outsiderToken, e.issueAttachmentsPath(issue), "photo.png", []byte("12345678"))
	if code != http.StatusForbidden {
		t.Fatalf("outsider upload code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, outsiderToken, http.MethodGet, e.issueAttachmentPath(issue, attachment.Object)+"/content", nil)
	if code != http.StatusForbidden {
		t.Fatalf("outsider content code = %d body = %s", code, body)
	}
	code, body = e.doWithToken(t, outsiderToken, http.MethodDelete, e.issueAttachmentPath(issue, attachment.Object), nil)
	if code != http.StatusForbidden {
		t.Fatalf("outsider delete code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPost, e.issueAttachmentsPath(issue), map[string]any{"file": "nope"})
	if code != http.StatusBadRequest {
		t.Fatalf("json upload code = %d body = %s", code, body)
	}
	code, body = e.doMultipartPathWithoutFile(t, e.authToken, e.issueAttachmentsPath(issue))
	if code != http.StatusBadRequest {
		t.Fatalf("missing file code = %d body = %s", code, body)
	}
	code, body = e.doMultipartPath(t, e.authToken, e.issueAttachmentsPath(issue), "big.bin", []byte("123456789"))
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload code = %d body = %s", code, body)
	}
	code, body = e.doMultipartPath(t, e.authToken, e.issueAttachmentsPath(issue), "../", []byte("1"))
	if code != http.StatusBadRequest {
		t.Fatalf("bad filename code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, e.issueAttachmentsPath(issue)+"?cursor=not-base64", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad cursor code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, e.issueAttachmentsPath(issue)+"?limit=0", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad limit code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, e.issueAttachmentsPath(issue)+"/not-an-object/content", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad ref code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, e.issueAttachmentsPath(issue)+"/object-999/content", nil)
	if code != http.StatusNotFound {
		t.Fatalf("missing ref code = %d body = %s", code, body)
	}

	disabled := newHTTPEnv(t)
	disabledIssue := disabled.mustCreateIssue(t, "Disabled attachment")
	disabledObject, err := disabled.store.CreateStorageObject(disabled.ctx, store.CreateStorageObjectParams{
		ID:          uuid.New(),
		ProjectID:   disabled.projectID,
		Backend:     "local",
		Bucket:      "local",
		ObjectKey:   "projects/disabled/objects/attachment",
		Filename:    "disabled.bin",
		ContentType: "application/octet-stream",
		ByteSize:    1,
		SHA256:      strings.Repeat("c", 64),
		CreatedByID: disabled.adminID,
	})
	if err != nil {
		t.Fatalf("CreateStorageObject disabled: %v", err)
	}
	disabledAttachment, err := disabled.store.CreateIssueAttachment(disabled.ctx, store.CreateIssueAttachmentParams{
		IssueID:         disabledIssue.ID,
		StorageObjectID: disabledObject.ID,
		CreatedByID:     disabled.adminID,
	})
	if err != nil {
		t.Fatalf("CreateIssueAttachment disabled: %v", err)
	}
	code, body = disabled.doMultipartPath(t, disabled.authToken, disabled.issueAttachmentsPath(disabledIssue), "photo.png", []byte("12345678"))
	if code != http.StatusServiceUnavailable {
		t.Fatalf("disabled upload code = %d body = %s", code, body)
	}
	code, body = disabled.do(t, http.MethodGet, disabled.issueAttachmentPath(disabledIssue, disabledAttachment.Object)+"/content", nil)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("disabled content code = %d body = %s", code, body)
	}
	code, body = disabled.do(t, http.MethodDelete, disabled.issueAttachmentPath(disabledIssue, disabledAttachment.Object), nil)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("disabled delete code = %d body = %s", code, body)
	}
}

func TestUIIssueAttachmentRoutes(t *testing.T) {
	t.Parallel()
	e, _ := newStorageHTTPEnv(t, 1024)
	issue := e.mustCreateIssue(t, "Attachment UI")

	res := e.uiDoMultipartContext(t, e.issueAttachmentsPath(issue), e.authToken, nil, "ui.txt", "ui file")
	body := readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("ui upload code = %d body = %s", res.StatusCode, body)
	}
	attachment := decode[model.IssueAttachment](t, []byte(body))
	if attachment.IssueID != issue.ID || attachment.Object.Ref != "object-1" || attachment.Object.Filename != "ui.txt" {
		t.Fatalf("ui attachment = %+v", attachment)
	}

	res = e.uiDoNoRedirect(t, http.MethodGet, e.issueAttachmentPath(issue, attachment.Object)+"/content", e.authToken, nil)
	body = readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || body != "ui file" {
		t.Fatalf("ui content code = %d body = %q", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") || !strings.Contains(got, "ui.txt") {
		t.Fatalf("ui content disposition = %q", got)
	}

	res = e.uiDoNoRedirect(t, http.MethodDelete, e.issueAttachmentPath(issue, attachment.Object), e.authToken, nil)
	body = readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("ui delete code = %d body = %s", res.StatusCode, body)
	}

	res = e.uiDoMultipartContext(t, e.issueAttachmentsPath(issue), e.authToken, nil, "panel.txt", "panel file")
	body = readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("ui second upload code = %d body = %s", res.StatusCode, body)
	}
	second := decode[model.IssueAttachment](t, []byte(body))
	res = e.uiDoNoRedirect(t, http.MethodPost, e.issueAttachmentPath(issue, second.Object)+"/delete", e.authToken, nil)
	body = readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("ui htmx delete code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Attachment UI") || strings.Contains(body, "panel.txt") {
		t.Fatalf("ui htmx delete panel body unexpected: %s", body)
	}
}

func TestHTTPIssueAttachmentCleansUpObjectWhenLinkFails(t *testing.T) {
	t.Parallel()
	e, root := newStorageHTTPEnv(t, 1024)
	issue := e.mustCreateIssue(t, "Attachment cleanup")
	if _, err := e.pool.Exec(e.ctx, `
		CREATE FUNCTION fail_issue_attachment_insert() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'forced issue attachment failure';
		END;
		$$ LANGUAGE plpgsql;
	`); err != nil {
		t.Fatalf("install failing function: %v", err)
	}
	if _, err := e.pool.Exec(e.ctx, `
		CREATE TRIGGER fail_issue_attachment_insert
		BEFORE INSERT ON issue_attachments
		FOR EACH ROW EXECUTE FUNCTION fail_issue_attachment_insert();
	`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}

	code, body := e.doMultipartPath(t, e.authToken, e.issueAttachmentsPath(issue), "photo.png", []byte("image"))
	if code != http.StatusInternalServerError {
		t.Fatalf("upload code = %d body = %s", code, body)
	}
	objects, more, err := e.store.ListStorageObjects(e.ctx, store.ListStorageObjectsParams{ProjectID: e.projectID, Limit: 10})
	if err != nil {
		t.Fatalf("ListStorageObjects: %v", err)
	}
	if len(objects) != 0 || more {
		t.Fatalf("storage objects after failed link = %+v more=%v", objects, more)
	}
	if files := regularFilesUnder(t, root); len(files) != 0 {
		t.Fatalf("regular files after failed link = %v, want none", files)
	}
}

func regularFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	return files
}
