package server_test

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func (e *httpEnv) projectTagsPath() string {
	return e.projectPath() + "/tags"
}

func (e *httpEnv) projectTagPath(tag model.IssueTag) string {
	return e.projectTagsPath() + "/" + tag.Ref
}

func (e *httpEnv) issueTagsPath(iss model.Issue) string {
	return e.issuePath(iss) + "/tags"
}

func (e *httpEnv) issueTagPath(iss model.Issue, tag model.IssueTag) string {
	return e.issueTagsPath(iss) + "/" + tag.Ref
}

func TestHTTPIssueTagsCRUDAttachAndFilter(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	issueA, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "Customer issue"})
	if err != nil {
		t.Fatalf("CreateIssue A: %v", err)
	}
	issueB, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "Launch issue"})
	if err != nil {
		t.Fatalf("CreateIssue B: %v", err)
	}
	plain, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "Plain issue"})
	if err != nil {
		t.Fatalf("CreateIssue plain: %v", err)
	}

	code, body := e.do(t, http.MethodPost, e.projectTagsPath(), map[string]any{
		"name":  " #Customer   Beta ",
		"color": "green",
	})
	if code != http.StatusCreated {
		t.Fatalf("create customer code = %d body = %s", code, body)
	}
	customer := decode[model.IssueTag](t, body)
	if customer.Name != "Customer Beta" || customer.DisplayName != "#Customer Beta" || customer.Color != model.TagColorGreen {
		t.Fatalf("customer tag mismatch: %+v", customer)
	}

	code, body = e.do(t, http.MethodPost, e.projectTagsPath(), map[string]any{"name": "#Q3 Launch"})
	if code != http.StatusCreated {
		t.Fatalf("create launch code = %d body = %s", code, body)
	}
	launch := decode[model.IssueTag](t, body)

	code, _ = e.do(t, http.MethodPost, e.projectTagsPath(), map[string]any{"name": "#Customer   Beta"})
	if code != http.StatusConflict {
		t.Fatalf("duplicate create code = %d, want 409", code)
	}
	code, _ = e.do(t, http.MethodPost, e.projectTagsPath(), map[string]any{"name": "Bad Color", "color": "mauve"})
	if code != http.StatusBadRequest {
		t.Fatalf("invalid color code = %d, want 400", code)
	}
	code, _ = e.do(t, http.MethodGet, e.projectTagsPath()+"/not-a-tag", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad tag ref code = %d, want 400", code)
	}

	code, body = e.do(t, http.MethodPatch, e.projectTagPath(customer), map[string]any{
		"name":  "#Customer Beta FY26",
		"color": "pink",
	})
	if code != http.StatusOK {
		t.Fatalf("patch customer code = %d body = %s", code, body)
	}
	customer = decode[model.IssueTag](t, body)
	if customer.Name != "Customer Beta FY26" || customer.Color != model.TagColorPink {
		t.Fatalf("patched customer mismatch: %+v", customer)
	}

	code, body = e.do(t, http.MethodGet, e.projectTagsPath(), nil)
	if code != http.StatusOK {
		t.Fatalf("list tags code = %d body = %s", code, body)
	}
	if got := decodePage[model.IssueTag](t, body).Items; len(got) != 2 || got[0].ID != customer.ID || got[1].ID != launch.ID {
		t.Fatalf("listed tags = %+v", got)
	}

	code, body = e.do(t, http.MethodPost, e.issueTagsPath(issueA), map[string]any{"tag": "Customer Beta FY26"})
	if code != http.StatusCreated {
		t.Fatalf("attach by name code = %d body = %s", code, body)
	}
	code, _ = e.do(t, http.MethodPost, e.issueTagsPath(issueA), map[string]any{"tag_ref": customer.Ref})
	if code != http.StatusConflict {
		t.Fatalf("duplicate attach code = %d, want 409", code)
	}
	code, body = e.do(t, http.MethodPost, e.issueTagsPath(issueB), map[string]any{"tag_ref": launch.Ref})
	if code != http.StatusCreated {
		t.Fatalf("attach by ref code = %d body = %s", code, body)
	}
	code, _ = e.do(t, http.MethodPost, e.issueTagsPath(issueB), map[string]any{"tag": "missing"})
	if code != http.StatusNotFound {
		t.Fatalf("missing tag attach code = %d, want 404", code)
	}

	code, body = e.do(t, http.MethodGet, e.issueTagsPath(issueA), nil)
	if code != http.StatusOK {
		t.Fatalf("list issue tags code = %d body = %s", code, body)
	}
	if got := decodePage[model.IssueTag](t, body).Items; len(got) != 1 || got[0].ID != customer.ID {
		t.Fatalf("issue A tags = %+v", got)
	}

	code, body = e.do(t, http.MethodGet, e.issuePath(issueA), nil)
	if code != http.StatusOK {
		t.Fatalf("get issue code = %d body = %s", code, body)
	}
	gotIssue := decode[model.Issue](t, body)
	if len(gotIssue.Tags) != 1 || gotIssue.Tags[0].DisplayName != "#Customer Beta FY26" {
		t.Fatalf("hydrated issue tags = %+v", gotIssue.Tags)
	}

	code, body = e.do(t, http.MethodGet, e.projectIssuesPath()+"?tag="+url.QueryEscape("#Customer Beta FY26"), nil)
	if code != http.StatusOK {
		t.Fatalf("filter customer code = %d body = %s", code, body)
	}
	if got := issueIDs(decodePage[model.Issue](t, body).Items); !reflect.DeepEqual(got, []uuid.UUID{issueA.ID}) {
		t.Fatalf("customer filter IDs = %v", got)
	}

	filter := e.projectIssuesPath() + "?tag=" + url.QueryEscape("Customer Beta FY26") + "&tag=" + url.QueryEscape("#Q3 Launch")
	code, body = e.do(t, http.MethodGet, filter, nil)
	if code != http.StatusOK {
		t.Fatalf("filter OR code = %d body = %s", code, body)
	}
	if got := issueIDs(decodePage[model.Issue](t, body).Items); !reflect.DeepEqual(got, []uuid.UUID{issueA.ID, issueB.ID}) {
		t.Fatalf("OR filter IDs = %v, plain issue was %s", got, plain.ID)
	}

	code, _ = e.do(t, http.MethodDelete, e.issueTagPath(issueA, customer), nil)
	if code != http.StatusNoContent {
		t.Fatalf("detach code = %d, want 204", code)
	}
	code, body = e.do(t, http.MethodGet, e.issueTagsPath(issueA), nil)
	if code != http.StatusOK {
		t.Fatalf("list after detach code = %d body = %s", code, body)
	}
	if got := decodePage[model.IssueTag](t, body).Items; len(got) != 0 {
		t.Fatalf("issue A tags after detach = %+v", got)
	}

	code, _ = e.do(t, http.MethodDelete, e.projectTagPath(launch), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete tag code = %d, want 204", code)
	}
	code, _ = e.do(t, http.MethodGet, e.projectTagPath(launch), nil)
	if code != http.StatusNotFound {
		t.Fatalf("get deleted tag code = %d, want 404", code)
	}
}

func issueIDs(issues []model.Issue) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(issues))
	for _, issue := range issues {
		out = append(out, issue.ID)
	}
	return out
}
