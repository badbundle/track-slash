package server_test

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func (e *httpEnv) mustCreateIssue(t *testing.T, title string) model.Issue {
	t.Helper()
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     title,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	return iss
}

func TestHTTPCreateIssueLinkHappy(t *testing.T) {
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "A")
	b := e.mustCreateIssue(t, "B")

	code, body := e.do(t, http.MethodPost, "/issues/"+a.ID.String()+"/links",
		map[string]any{"target_id": b.ID.String(), "link_type": "blocks"})
	if code != http.StatusCreated {
		t.Fatalf("code = %d body = %s", code, body)
	}
	link := decode[model.IssueLink](t, body)
	if link.SourceID != a.ID || link.TargetID != b.ID || link.LinkType != model.LinkTypeBlocks {
		t.Fatalf("link = %+v", link)
	}
}

func TestHTTPCreateIssueLinkDuplicatesClosesSource(t *testing.T) {
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "dup")
	b := e.mustCreateIssue(t, "canon")

	code, _ := e.do(t, http.MethodPost, "/issues/"+a.ID.String()+"/links",
		map[string]any{"target_id": b.ID.String(), "link_type": "duplicates"})
	if code != http.StatusCreated {
		t.Fatalf("code = %d", code)
	}

	code, body := e.do(t, http.MethodGet, "/issues/"+a.ID.String(), nil)
	if code != http.StatusOK {
		t.Fatalf("get code = %d", code)
	}
	if decode[model.Issue](t, body).Status != model.StatusDone {
		t.Fatalf("source not closed")
	}
}

func TestHTTPCreateIssueLinkBadSourceID(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost, "/issues/not-uuid/links",
		map[string]any{"target_id": uuid.New().String(), "link_type": "blocks"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateIssueLinkBadJSON(t *testing.T) {
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "A")
	req, _ := http.NewRequestWithContext(e.ctx, http.MethodPost,
		e.ts.URL+"/issues/"+a.ID.String()+"/links",
		bytes.NewReader([]byte("nope")))
	req.Header.Set("Content-Type", "application/json")
	res, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("code = %d", res.StatusCode)
	}
}

func TestHTTPCreateIssueLinkMissingTarget(t *testing.T) {
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "A")
	code, _ := e.do(t, http.MethodPost, "/issues/"+a.ID.String()+"/links",
		map[string]any{"link_type": "blocks"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateIssueLinkInvalidType(t *testing.T) {
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "A")
	b := e.mustCreateIssue(t, "B")
	code, _ := e.do(t, http.MethodPost, "/issues/"+a.ID.String()+"/links",
		map[string]any{"target_id": b.ID.String(), "link_type": "explodes"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateIssueLinkSelfRejected(t *testing.T) {
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "A")
	code, _ := e.do(t, http.MethodPost, "/issues/"+a.ID.String()+"/links",
		map[string]any{"target_id": a.ID.String(), "link_type": "blocks"})
	if code != http.StatusConflict {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateIssueLinkDuplicateRejected(t *testing.T) {
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "A")
	b := e.mustCreateIssue(t, "B")
	body := map[string]any{"target_id": b.ID.String(), "link_type": "blocks"}
	if code, _ := e.do(t, http.MethodPost, "/issues/"+a.ID.String()+"/links", body); code != http.StatusCreated {
		t.Fatalf("first code = %d", code)
	}
	code, _ := e.do(t, http.MethodPost, "/issues/"+a.ID.String()+"/links", body)
	if code != http.StatusConflict {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateIssueLinkCrossProject(t *testing.T) {
	e := newHTTPEnv(t)
	other, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	a := e.mustCreateIssue(t, "A")
	b, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: other.ID, Title: "B"})
	if err != nil {
		t.Fatalf("CreateIssue other: %v", err)
	}

	code, _ := e.do(t, http.MethodPost, "/issues/"+a.ID.String()+"/links",
		map[string]any{"target_id": b.ID.String(), "link_type": "blocks"})
	if code != http.StatusConflict {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateIssueLinkSourceNotFound(t *testing.T) {
	e := newHTTPEnv(t)
	b := e.mustCreateIssue(t, "B")
	code, _ := e.do(t, http.MethodPost, "/issues/"+uuid.New().String()+"/links",
		map[string]any{"target_id": b.ID.String(), "link_type": "blocks"})
	if code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateIssueLinkTargetNotFound(t *testing.T) {
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "A")
	code, _ := e.do(t, http.MethodPost, "/issues/"+a.ID.String()+"/links",
		map[string]any{"target_id": uuid.New().String(), "link_type": "blocks"})
	if code != http.StatusConflict {
		t.Fatalf("code = %d", code)
	}
}

type linkView struct {
	model.IssueLink
	Direction    string    `json:"direction"`
	DisplayType  string    `json:"display_type"`
	OtherIssueID uuid.UUID `json:"other_issue_id"`
}

func TestHTTPListIssueLinksDirectionalView(t *testing.T) {
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "A")
	b := e.mustCreateIssue(t, "B")
	c := e.mustCreateIssue(t, "C")

	cases := []struct {
		linkType     model.LinkType
		incomingName string
		outgoingName string
	}{
		{model.LinkTypeBlocks, "is_blocked_by", "blocks"},
		{model.LinkTypeRelatesTo, "relates_to", "relates_to"},
		{model.LinkTypeClones, "is_cloned_by", "clones"},
	}

	// Each case: A -> B (outgoing on A, incoming on B). Re-use same A and
	// fresh targets so we exercise all four display names on a single read.
	for i, c := range cases {
		// fresh target so unique constraint isn't tripped
		target, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
			ProjectID: e.projectID, Title: fmt.Sprintf("t-%d", i),
		})
		if err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
		code, _ := e.do(t, http.MethodPost, "/issues/"+a.ID.String()+"/links",
			map[string]any{"target_id": target.ID.String(), "link_type": string(c.linkType)})
		if code != http.StatusCreated {
			t.Fatalf("create %s: code = %d", c.linkType, code)
		}
		_ = b
	}

	// Incoming on A: C -> A (relates_to so the inverse-display path is exercised)
	if code, _ := e.do(t, http.MethodPost, "/issues/"+c.ID.String()+"/links",
		map[string]any{"target_id": a.ID.String(), "link_type": "blocks"}); code != http.StatusCreated {
		t.Fatalf("inbound: code = %d", code)
	}

	code, body := e.do(t, http.MethodGet, "/issues/"+a.ID.String()+"/links", nil)
	if code != http.StatusOK {
		t.Fatalf("list code = %d", code)
	}
	got := decode[[]linkView](t, body)
	if len(got) != len(cases)+1 {
		t.Fatalf("len = %d, want %d", len(got), len(cases)+1)
	}
	outCount, inCount := 0, 0
	for _, v := range got {
		switch v.Direction {
		case "outgoing":
			outCount++
			if v.SourceID != a.ID {
				t.Fatalf("outgoing source = %s, want %s", v.SourceID, a.ID)
			}
			if v.DisplayType != string(v.LinkType) {
				t.Fatalf("outgoing display = %s, want %s", v.DisplayType, v.LinkType)
			}
			if v.OtherIssueID != v.TargetID {
				t.Fatalf("outgoing other = %s, want target %s", v.OtherIssueID, v.TargetID)
			}
		case "incoming":
			inCount++
			if v.TargetID != a.ID {
				t.Fatalf("incoming target = %s, want %s", v.TargetID, a.ID)
			}
			if v.LinkType == model.LinkTypeBlocks && v.DisplayType != "is_blocked_by" {
				t.Fatalf("incoming blocks display = %s", v.DisplayType)
			}
			if v.OtherIssueID != v.SourceID {
				t.Fatalf("incoming other = %s, want source %s", v.OtherIssueID, v.SourceID)
			}
		default:
			t.Fatalf("unknown direction: %s", v.Direction)
		}
	}
	if outCount != len(cases) || inCount != 1 {
		t.Fatalf("outCount = %d, inCount = %d", outCount, inCount)
	}
}

// TestHTTPListIssueLinksIncomingDisplayNames pins down the inverse-display
// table for every link type via incoming-direction lookups.
func TestHTTPListIssueLinksIncomingDisplayNames(t *testing.T) {
	e := newHTTPEnv(t)
	target := e.mustCreateIssue(t, "target")

	cases := []struct {
		linkType model.LinkType
		display  string
	}{
		{model.LinkTypeBlocks, "is_blocked_by"},
		{model.LinkTypeDuplicates, "is_duplicated_by"},
		{model.LinkTypeClones, "is_cloned_by"},
		{model.LinkTypeRelatesTo, "relates_to"},
	}

	for i, c := range cases {
		src, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
			ProjectID: e.projectID, Title: fmt.Sprintf("src-%d", i),
		})
		if err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
		code, _ := e.do(t, http.MethodPost, "/issues/"+src.ID.String()+"/links",
			map[string]any{"target_id": target.ID.String(), "link_type": string(c.linkType)})
		if code != http.StatusCreated {
			t.Fatalf("create %s: code = %d", c.linkType, code)
		}
	}

	code, body := e.do(t, http.MethodGet, "/issues/"+target.ID.String()+"/links", nil)
	if code != http.StatusOK {
		t.Fatalf("list code = %d", code)
	}
	got := decode[[]linkView](t, body)
	displayByType := map[model.LinkType]string{}
	for _, v := range got {
		if v.Direction != "incoming" {
			t.Fatalf("direction = %s, want incoming", v.Direction)
		}
		displayByType[v.LinkType] = v.DisplayType
	}
	for _, c := range cases {
		if got := displayByType[c.linkType]; got != c.display {
			t.Fatalf("incoming %s display = %s, want %s", c.linkType, got, c.display)
		}
	}
}

func TestHTTPListIssueLinksBadID(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, "/issues/not-uuid/links", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListIssueLinksEmpty(t *testing.T) {
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "lone")
	code, body := e.do(t, http.MethodGet, "/issues/"+a.ID.String()+"/links", nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if string(body) != "[]\n" && string(body) != "[]" {
		t.Fatalf("body = %q, want empty array", body)
	}
}

func TestHTTPGetIssueLinkHappy(t *testing.T) {
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "A")
	b := e.mustCreateIssue(t, "B")
	_, createBody := e.do(t, http.MethodPost, "/issues/"+a.ID.String()+"/links",
		map[string]any{"target_id": b.ID.String(), "link_type": "blocks"})
	link := decode[model.IssueLink](t, createBody)

	code, body := e.do(t, http.MethodGet, "/issue-links/"+link.ID.String(), nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	got := decode[model.IssueLink](t, body)
	if got.ID != link.ID {
		t.Fatalf("id mismatch")
	}
}

func TestHTTPGetIssueLinkBadID(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, "/issue-links/zzz", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPGetIssueLinkNotFound(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, "/issue-links/"+uuid.New().String(), nil)
	if code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPDeleteIssueLink(t *testing.T) {
	e := newHTTPEnv(t)
	a := e.mustCreateIssue(t, "A")
	b := e.mustCreateIssue(t, "B")
	_, createBody := e.do(t, http.MethodPost, "/issues/"+a.ID.String()+"/links",
		map[string]any{"target_id": b.ID.String(), "link_type": "blocks"})
	link := decode[model.IssueLink](t, createBody)

	code, _ := e.do(t, http.MethodDelete, "/issue-links/"+link.ID.String(), nil)
	if code != http.StatusNoContent {
		t.Fatalf("code = %d", code)
	}
	code, _ = e.do(t, http.MethodDelete, "/issue-links/"+link.ID.String(), nil)
	if code != http.StatusNotFound {
		t.Fatalf("second delete code = %d", code)
	}
}

func TestHTTPDeleteIssueLinkBadID(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodDelete, "/issue-links/nope", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

