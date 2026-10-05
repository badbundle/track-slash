package server_test

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
)

// A repeating issue's next repetition gets its own copies of the attachments
// its description shows, sharing their bytes until neither is left.
func TestHTTPRepeatCopiesDescriptionAttachments(t *testing.T) {
	t.Parallel()
	e, root := newStorageHTTPEnv(t, 1024)
	code, body := e.do(t, http.MethodPost, e.projectIssuesPath(), map[string]any{"title": "Weekly screenshot", "repeat": "daily"})
	if code != http.StatusCreated {
		t.Fatalf("create code = %d body = %s", code, body)
	}
	issue := decode[model.Issue](t, body)
	upload := func(name string, content []byte) model.IssueAttachment {
		t.Helper()
		code, body := e.doMultipartPath(t, e.authToken, e.issueAttachmentsPath(issue), name, content)
		if code != http.StatusCreated {
			t.Fatalf("upload %s code = %d body = %s", name, code, body)
		}
		return decode[model.IssueAttachment](t, body)
	}
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("screenshot")...)
	shot := upload("shot.png", png)
	upload("unused.txt", []byte("not in the description"))
	description := "![Screenshot](" + shot.Object.Ref + ")"
	if code, body := e.do(t, http.MethodPatch, e.issuePath(issue), map[string]any{"description": description}); code != http.StatusOK {
		t.Fatalf("set description code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, e.issuePath(issue), map[string]any{"status": "done"})
	done := decode[model.Issue](t, body)
	if code != http.StatusOK || done.Repeat == nil || done.Repeat.Next == nil {
		t.Fatalf("complete code = %d body = %s", code, body)
	}
	next, err := e.store.GetIssueByOwnerKeyNumber(e.ctx, done.OwnerUsername, done.ProjectKey, done.Repeat.Next.Number)
	if err != nil {
		t.Fatalf("get next: %v", err)
	}
	code, body = e.do(t, http.MethodGet, e.issueAttachmentsPath(next), nil)
	copies := decodePage[model.IssueAttachment](t, body).Items
	if code != http.StatusOK || len(copies) != 1 {
		t.Fatalf("next repetition's attachments code = %d body = %s", code, body)
	}
	copied := copies[0].Object
	if copied.Ref == shot.Object.Ref || copied.ObjectKey != shot.Object.ObjectKey || next.Description != "![Screenshot]("+copied.Ref+")" {
		t.Fatalf("copy = %+v, description %q", copied, next.Description)
	}

	// The copied description shows the copy, not text.
	if panel := e.uiGet(t, e.issuePath(next)+"/panel", e.authToken); !strings.Contains(panel, `<img src="`+e.issuePath(next)+`/attachments/`+copied.Ref+`/content`) {
		t.Fatalf("next repetition doesn't show the copied image: %s", panel)
	}

	content := func(iss model.Issue, object model.StorageObject) (int, []byte) {
		t.Helper()
		res, body := e.doRaw(t, e.authToken, http.MethodGet, e.issueAttachmentPath(iss, object)+"/content", nil, "")
		return res.StatusCode, body
	}
	if code, got := content(next, copied); code != http.StatusOK || !bytes.Equal(got, png) {
		t.Fatalf("copy content code = %d body = %q", code, got)
	}
	file := filepath.Join(root, filepath.FromSlash(shot.Object.ObjectKey))

	// Deleting the original keeps the bytes the copy uses.
	if code, body := e.do(t, http.MethodDelete, e.issueAttachmentPath(issue, shot.Object), nil); code != http.StatusNoContent {
		t.Fatalf("delete original code = %d body = %s", code, body)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("shared file after deleting the original: %v", err)
	}
	if code, got := content(next, copied); code != http.StatusOK || !bytes.Equal(got, png) {
		t.Fatalf("copy content after deleting the original code = %d body = %q", code, got)
	}
	// Deleting the last one deletes them.
	if code, body := e.do(t, http.MethodDelete, e.issueAttachmentPath(next, copied), nil); code != http.StatusNoContent {
		t.Fatalf("delete copy code = %d body = %s", code, body)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("shared file after deleting every object err = %v, want not exist", err)
	}
}
