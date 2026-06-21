package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/server"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

type httpEnv struct {
	ctx           context.Context
	ts            *httptest.Server
	store         *store.Store
	projectID     uuid.UUID
	projKey       string
	ownerUsername string
	adminID       uuid.UUID
	authToken     string
}

func newHTTPEnv(t *testing.T) *httpEnv {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	db := testutil.NewMigratedDatabase(t)
	s := store.New(db.Pool)
	// Hub is nil — none of these handlers need realtime fanout; /api/v1/ws route
	// is just skipped when hub == nil.
	srv := server.New(s, nil, nil)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	key := uniqueProjectKey(t)
	admin, err := s.CreateOrUpdateAdminUser(ctx, "admin-"+key+"@example.com", "Admin")
	if err != nil {
		t.Fatalf("CreateOrUpdateAdminUser: %v", err)
	}
	proj, err := s.CreateProjectForUser(ctx, admin.ID, key, "http-test", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser: %v", err)
	}
	token, err := s.CreateAuthToken(ctx, store.CreateAuthTokenParams{
		UserID: admin.ID,
		Kind:   model.AuthTokenKindAPI,
		Name:   "test",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	return &httpEnv{
		ctx: ctx, ts: ts, store: s, projectID: proj.ID, projKey: key, ownerUsername: admin.Username,
		adminID: admin.ID, authToken: token.RawToken,
	}
}

func (e *httpEnv) projectPath() string {
	return "/" + e.ownerUsername + "/projects/" + e.projKey
}

func (e *httpEnv) projectIssuesPath() string {
	return e.projectPath() + "/issues"
}

func (e *httpEnv) projectSprintsPath() string {
	return e.projectPath() + "/sprints"
}

func (e *httpEnv) issuePath(iss model.Issue) string {
	return "/" + iss.OwnerUsername + "/issues/" + iss.Identifier
}

func (e *httpEnv) issueCommentsPath(iss model.Issue) string {
	return e.issuePath(iss) + "/comments"
}

func (e *httpEnv) issueLinksPath(iss model.Issue) string {
	return e.issuePath(iss) + "/links"
}

func (e *httpEnv) issueSubIssuesPath(iss model.Issue) string {
	return e.issuePath(iss) + "/sub-issues"
}

func (e *httpEnv) sprintPath(sp model.Sprint) string {
	return e.projectSprintsPath() + "/" + sp.Ref
}

func (e *httpEnv) projectLinkPath(link model.IssueLink) string {
	return e.projectPath() + "/links/" + link.Ref
}

func uniqueProjectKey(t *testing.T) string {
	t.Helper()
	n := time.Now().UnixNano()
	out := make([]byte, 9)
	for i := 8; i >= 0; i-- {
		out[i] = byte('0' + (n % 10))
		n /= 10
	}
	return "H" + string(out)
}

// do performs a request against the test server and returns status + body.
func (e *httpEnv) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	return e.doWithToken(t, e.authToken, method, path, body)
}

func (e *httpEnv) doUnauth(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	return e.doWithToken(t, "", method, path, body)
}

func (e *httpEnv) doWithToken(t *testing.T, token, method, path string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(e.ctx, method, e.ts.URL+apiPath(path), rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res.StatusCode, out
}

func apiPath(path string) string {
	return "/api/v1" + path
}

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	return v
}

// ---------- createSprint ----------

func TestHTTPCreateSprintHappy(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, body := e.do(t, http.MethodPost,
		e.projectSprintsPath(),
		map[string]any{"name": "S1", "start_date": "2026-06-01", "end_date": "2026-06-14"},
	)
	if code != http.StatusCreated {
		t.Fatalf("code = %d, body = %s", code, body)
	}
	sp := decode[model.Sprint](t, body)
	if sp.Name != "S1" || sp.Status != model.SprintStatusPlanned {
		t.Fatalf("bad sprint: %+v", sp)
	}
}

func TestHTTPCreateSprintBadProjectID(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost, "/bad!/projects/TRACK/sprints",
		map[string]any{"name": "S", "start_date": "2026-06-01", "end_date": "2026-06-14"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateSprintBadJSON(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	req, _ := http.NewRequestWithContext(e.ctx, http.MethodPost,
		e.ts.URL+apiPath(e.projectSprintsPath()),
		bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.authToken)
	res, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("code = %d", res.StatusCode)
	}
}

func TestHTTPCreateSprintBadDateFormat(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	cases := []map[string]any{
		{"start_date": "2026/06/01", "end_date": "2026-06-14"},
		{"start_date": "2026-06-01", "end_date": "tomorrow"},
	}
	for _, body := range cases {
		body["name"] = "x"
		code, _ := e.do(t, http.MethodPost,
			e.projectSprintsPath(), body)
		if code != http.StatusBadRequest {
			t.Fatalf("body=%v code=%d", body, code)
		}
	}
}

func TestHTTPCreateSprintEndBeforeStart(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost,
		e.projectSprintsPath(),
		map[string]any{"name": "x", "start_date": "2026-06-14", "end_date": "2026-06-01"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateSprintNameTooLong(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	code, _ := e.do(t, http.MethodPost,
		e.projectSprintsPath(),
		map[string]any{"name": string(long), "start_date": "2026-06-01", "end_date": "2026-06-14"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateSprintGoalTooLong(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	long := make([]byte, 2001)
	for i := range long {
		long[i] = 'g'
	}
	code, _ := e.do(t, http.MethodPost,
		e.projectSprintsPath(),
		map[string]any{"name": "x", "goal": string(long), "start_date": "2026-06-01", "end_date": "2026-06-14"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateSprintProjectNotFound(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost,
		"/"+e.ownerUsername+"/projects/"+uniqueProjectKey(t)+"/sprints",
		map[string]any{"name": "x", "start_date": "2026-06-01", "end_date": "2026-06-14"})
	if code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

// ---------- listProjectSprints ----------

func TestHTTPListSprintsAndStatusFilter(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	for i, name := range []string{"S1", "S2"} {
		e.do(t, http.MethodPost, e.projectSprintsPath(),
			map[string]any{
				"name":       name,
				"start_date": fmt.Sprintf("2026-06-%02d", 1+i*14),
				"end_date":   fmt.Sprintf("2026-06-%02d", 14+i*14),
			})
	}
	code, body := e.do(t, http.MethodGet, e.projectSprintsPath(), nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	sprints := decodePage[model.Sprint](t, body).Items
	if len(sprints) != 2 {
		t.Fatalf("len = %d", len(sprints))
	}

	code, _ = e.do(t, http.MethodGet,
		e.projectSprintsPath()+"?status=completed", nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListSprintsBadStatus(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet,
		e.projectSprintsPath()+"?status=banana", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListSprintsBadProjectID(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, "/bad!/projects/TRACK/sprints", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

// ---------- reorderPlannedSprints ----------

func TestHTTPReorderPlannedSprints(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	a := createSprintFor(t, e, "A", "2026-06-01", "2026-06-14")
	b := createSprintFor(t, e, "B", "2026-06-15", "2026-06-28")
	c := createSprintFor(t, e, "C", "2026-06-29", "2026-07-12")

	code, body := e.do(t, http.MethodPatch,
		e.projectSprintsPath()+"/planned-order",
		map[string]any{"sprint_refs": []string{c.Ref, a.Ref, b.Ref}},
	)
	if code != http.StatusOK {
		t.Fatalf("code = %d body = %s", code, body)
	}
	got := decode[[]model.Sprint](t, body)
	want := []uuid.UUID{c.ID, a.ID, b.ID}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("position %d = %s, want %s", i, got[i].ID, id)
		}
		if got[i].PlannedOrder == nil || *got[i].PlannedOrder != int64(i+1) {
			t.Fatalf("position %d planned_order = %v", i, got[i].PlannedOrder)
		}
	}
}

func TestHTTPReorderPlannedSprintsRejectsBadRequests(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	a := createSprintFor(t, e, "A", "2026-06-01", "2026-06-14")
	b := createSprintFor(t, e, "B", "2026-06-15", "2026-06-28")
	active := createSprintFor(t, e, "active", "2026-06-29", "2026-07-12")
	if code, _ := e.do(t, http.MethodPatch, e.sprintPath(active), map[string]any{"status": "active"}); code != http.StatusOK {
		t.Fatalf("activate code = %d", code)
	}

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{name: "missing", body: map[string]any{"sprint_refs": []string{a.Ref}}, want: http.StatusConflict},
		{name: "duplicate", body: map[string]any{"sprint_refs": []string{a.Ref, a.Ref}}, want: http.StatusConflict},
		{name: "active", body: map[string]any{"sprint_refs": []string{a.Ref, active.Ref}}, want: http.StatusConflict},
		{name: "unknown", body: map[string]any{"sprint_refs": []string{a.Ref, "sprint-999999"}}, want: http.StatusNotFound},
		{name: "extra", body: map[string]any{"sprint_refs": []string{a.Ref, b.Ref, "sprint-999999"}}, want: http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _ := e.do(t, http.MethodPatch,
				e.projectSprintsPath()+"/planned-order", tc.body)
			if code != tc.want {
				t.Fatalf("code = %d, want %d", code, tc.want)
			}
		})
	}
}

func TestHTTPReorderPlannedSprintsBadIDAndBody(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPatch, "/bad!/projects/TRACK/sprints/planned-order",
		map[string]any{"sprint_refs": []string{}})
	if code != http.StatusBadRequest {
		t.Fatalf("bad id code = %d", code)
	}

	req, _ := http.NewRequestWithContext(e.ctx, http.MethodPatch,
		e.ts.URL+apiPath(e.projectSprintsPath()+"/planned-order"),
		bytes.NewReader([]byte("nope")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.authToken)
	res, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad body code = %d", res.StatusCode)
	}

	code, _ = e.do(t, http.MethodPatch, "/"+e.ownerUsername+"/projects/"+uniqueProjectKey(t)+"/sprints/planned-order",
		map[string]any{"sprint_refs": []string{}})
	if code != http.StatusNotFound {
		t.Fatalf("not found code = %d", code)
	}
}

// ---------- getSprint ----------

func TestHTTPGetSprint(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, body := e.do(t, http.MethodPost, e.projectSprintsPath(),
		map[string]any{"name": "S", "start_date": "2026-06-01", "end_date": "2026-06-14"})
	sp := decode[model.Sprint](t, body)

	code, body := e.do(t, http.MethodGet, e.sprintPath(sp), nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	got := decode[model.Sprint](t, body)
	if got.ID != sp.ID {
		t.Fatalf("id mismatch")
	}
}

func TestHTTPGetSprintBadID(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, e.projectSprintsPath()+"/not-a-sprint", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPGetSprintNotFound(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, e.projectSprintsPath()+"/sprint-999999", nil)
	if code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

// ---------- updateSprint ----------

func createSprintFor(t *testing.T, e *httpEnv, name, start, end string) model.Sprint {
	t.Helper()
	_, body := e.do(t, http.MethodPost, e.projectSprintsPath(),
		map[string]any{"name": name, "start_date": start, "end_date": end})
	return decode[model.Sprint](t, body)
}

func TestHTTPUpdateSprintAllFields(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, body := e.do(t, http.MethodPatch, e.sprintPath(sp), map[string]any{
		"name":       "renamed",
		"goal":       "ship",
		"start_date": "2026-06-08",
		"end_date":   "2026-06-22",
	})
	if code != http.StatusOK {
		t.Fatalf("code = %d body = %s", code, body)
	}
	got := decode[model.Sprint](t, body)
	if got.Name != "renamed" || got.Goal != "ship" {
		t.Fatalf("got = %+v", got)
	}
}

func TestHTTPUpdateSprintActivate(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, body := e.do(t, http.MethodPatch, e.sprintPath(sp),
		map[string]any{"status": "active"})
	if code != http.StatusOK {
		t.Fatalf("code = %d body = %s", code, body)
	}
	if decode[model.Sprint](t, body).Status != model.SprintStatusActive {
		t.Fatal("not active")
	}
}

func TestHTTPUpdateSprintRejectsCompleted(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, _ := e.do(t, http.MethodPatch, e.sprintPath(sp),
		map[string]any{"status": "completed"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateSprintInvalidStatus(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, _ := e.do(t, http.MethodPatch, e.sprintPath(sp),
		map[string]any{"status": "potato"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateSprintBadDate(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	cases := []map[string]any{
		{"start_date": "yesterday"},
		{"end_date": "tomorrow"},
	}
	for _, body := range cases {
		code, _ := e.do(t, http.MethodPatch, e.sprintPath(sp), body)
		if code != http.StatusBadRequest {
			t.Fatalf("body=%v code=%d", body, code)
		}
	}
}

func TestHTTPUpdateSprintNameAndGoalTooLong(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")

	bigName := string(bytes.Repeat([]byte("a"), 201))
	bigGoal := string(bytes.Repeat([]byte("g"), 2001))
	for _, body := range []map[string]any{{"name": bigName}, {"goal": bigGoal}} {
		code, _ := e.do(t, http.MethodPatch, e.sprintPath(sp), body)
		if code != http.StatusBadRequest {
			t.Fatalf("body=%v code=%d", body, code)
		}
	}
}

func TestHTTPUpdateSprintBadJSON(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	req, _ := http.NewRequestWithContext(e.ctx, http.MethodPatch,
		e.ts.URL+apiPath(e.sprintPath(sp)), bytes.NewReader([]byte("nope")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.authToken)
	res, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("code = %d", res.StatusCode)
	}
}

func TestHTTPUpdateSprintBadID(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPatch, e.projectSprintsPath()+"/not-a-sprint",
		map[string]any{"name": "x"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateSprintNotFound(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPatch, e.projectSprintsPath()+"/sprint-999999",
		map[string]any{"name": "x"})
	if code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateSprintActivationConflict(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	a := createSprintFor(t, e, "A", "2026-06-01", "2026-06-14")
	b := createSprintFor(t, e, "B", "2026-06-15", "2026-06-28")
	if code, _ := e.do(t, http.MethodPatch, e.sprintPath(a),
		map[string]any{"status": "active"}); code != http.StatusOK {
		t.Fatalf("activate A code = %d", code)
	}
	code, _ := e.do(t, http.MethodPatch, e.sprintPath(b),
		map[string]any{"status": "active"})
	if code != http.StatusConflict {
		t.Fatalf("code = %d", code)
	}
}

// ---------- completeSprint ----------

func TestHTTPCompleteSprintHappy(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	e.do(t, http.MethodPatch, e.sprintPath(sp), map[string]any{"status": "active"})

	code, body := e.do(t, http.MethodPost, e.sprintPath(sp)+"/complete", nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d body = %s", code, body)
	}
	got := decode[model.Sprint](t, body)
	if got.Status != model.SprintStatusCompleted {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestHTTPCompleteSprintMovesUnfinishedToNextPlannedSprint(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	active := createSprintFor(t, e, "Active", "2026-06-01", "2026-06-14")
	next := createSprintFor(t, e, "Next", "2026-06-15", "2026-06-28")
	createSprintFor(t, e, "Later", "2026-06-29", "2026-07-12")
	e.do(t, http.MethodPatch, e.sprintPath(active), map[string]any{"status": "active"})

	todo, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "todo rollover"})
	if err != nil {
		t.Fatalf("CreateIssue todo: %v", err)
	}
	inProgress, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "progress rollover"})
	if err != nil {
		t.Fatalf("CreateIssue progress: %v", err)
	}
	doneIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "done stays"})
	if err != nil {
		t.Fatalf("CreateIssue done: %v", err)
	}
	progressStatus := model.StatusInProgress
	doneStatus := model.StatusDone
	for _, issue := range []model.Issue{todo, inProgress, doneIssue} {
		if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{SprintID: &active.ID}); err != nil {
			t.Fatalf("assign issue %s: %v", issue.ID, err)
		}
	}
	if _, err := e.store.UpdateIssue(e.ctx, inProgress.ID, store.UpdateIssueParams{Status: &progressStatus}); err != nil {
		t.Fatalf("mark progress: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, doneIssue.ID, store.UpdateIssueParams{Status: &doneStatus}); err != nil {
		t.Fatalf("mark done: %v", err)
	}

	code, body := e.do(t, http.MethodPost, e.sprintPath(active)+"/complete", nil)
	if code != http.StatusOK {
		t.Fatalf("complete code = %d body = %s", code, body)
	}
	for _, id := range []uuid.UUID{todo.ID, inProgress.ID} {
		got, err := e.store.GetIssue(e.ctx, id)
		if err != nil {
			t.Fatalf("GetIssue %s: %v", id, err)
		}
		if got.SprintID == nil || *got.SprintID != next.ID {
			t.Fatalf("issue %s sprint = %v, want %s", id, got.SprintID, next.ID)
		}
	}
	gotDone, err := e.store.GetIssue(e.ctx, doneIssue.ID)
	if err != nil {
		t.Fatalf("GetIssue done: %v", err)
	}
	if gotDone.SprintID == nil || *gotDone.SprintID != active.ID {
		t.Fatalf("done sprint = %v, want %s", gotDone.SprintID, active.ID)
	}
}

func TestHTTPCompletedSprintRenameOnly(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	e.do(t, http.MethodPatch, e.sprintPath(sp), map[string]any{"status": "active"})
	e.do(t, http.MethodPost, e.sprintPath(sp)+"/complete", nil)

	code, body := e.do(t, http.MethodPatch, e.sprintPath(sp), map[string]any{"name": "renamed"})
	if code != http.StatusOK {
		t.Fatalf("rename code = %d body = %s", code, body)
	}
	if got := decode[model.Sprint](t, body); got.Name != "renamed" || got.Status != model.SprintStatusCompleted {
		t.Fatalf("got = %+v", got)
	}

	for _, body := range []map[string]any{
		{"goal": "nope"},
		{"start_date": "2026-06-02"},
		{"end_date": "2026-06-15"},
		{"status": "active"},
	} {
		code, _ := e.do(t, http.MethodPatch, e.sprintPath(sp), body)
		if code != http.StatusConflict {
			t.Fatalf("body=%v code=%d", body, code)
		}
	}
}

func TestHTTPCompleteSprintConflictNonActive(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, _ := e.do(t, http.MethodPost, e.sprintPath(sp)+"/complete", nil)
	if code != http.StatusConflict {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCompleteSprintBadID(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost, e.projectSprintsPath()+"/zzz/complete", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCompleteSprintNotFound(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost, e.projectSprintsPath()+"/sprint-999999/complete", nil)
	if code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

// ---------- issue PATCH + list with sprint filters ----------

func TestHTTPPatchIssueSetSprint(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")

	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID, Title: "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	code, body := e.do(t, http.MethodPatch, e.issuePath(iss),
		map[string]any{"sprint": sp.Ref})
	if code != http.StatusOK {
		t.Fatalf("code = %d body = %s", code, body)
	}
	got := decode[model.Issue](t, body)
	if got.SprintID == nil || *got.SprintID != sp.ID {
		t.Fatalf("sprint_id = %v", got.SprintID)
	}

	code, body = e.do(t, http.MethodPatch, e.issuePath(iss),
		map[string]any{"clear_sprint": true})
	if code != http.StatusOK {
		t.Fatalf("clear code = %d body = %s", code, body)
	}
	got = decode[model.Issue](t, body)
	if got.SprintID != nil {
		t.Fatalf("sprint_id = %v, want nil", got.SprintID)
	}
}

func TestHTTPPatchDoneIssueRejectsSprintEdit(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	current := createSprintFor(t, e, "Current", "2026-06-01", "2026-06-14")
	next := createSprintFor(t, e, "Next", "2026-06-15", "2026-06-28")
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "done issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	done := model.StatusDone
	if _, err := e.store.UpdateIssue(e.ctx, iss.ID, store.UpdateIssueParams{SprintID: &current.ID}); err != nil {
		t.Fatalf("assign done issue: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, iss.ID, store.UpdateIssueParams{Status: &done}); err != nil {
		t.Fatalf("mark done issue: %v", err)
	}

	code, body := e.do(t, http.MethodPatch, e.issuePath(iss), map[string]any{"sprint": next.Ref})
	if code != http.StatusConflict {
		t.Fatalf("set code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodPatch, e.issuePath(iss), map[string]any{"clear_sprint": true})
	if code != http.StatusConflict {
		t.Fatalf("clear code = %d body = %s", code, body)
	}
	got, err := e.store.GetIssue(e.ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.SprintID == nil || *got.SprintID != current.ID {
		t.Fatalf("SprintID = %v, want %s", got.SprintID, current.ID)
	}

	becomingDone, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "done plus sprint",
	})
	if err != nil {
		t.Fatalf("CreateIssue becoming done: %v", err)
	}
	code, body = e.do(t, http.MethodPatch, e.issuePath(becomingDone), map[string]any{
		"status": string(model.StatusDone),
		"sprint": next.Ref,
	})
	if code != http.StatusConflict {
		t.Fatalf("status plus sprint code = %d body = %s", code, body)
	}
}

func TestHTTPPatchIssueSetAndClearPeople(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	member, err := e.store.CreateUser(e.ctx, "issue-person-"+uniqueProjectKey(t)+"@example.com", "Issue Person")
	if err != nil {
		t.Fatalf("CreateUser member: %v", err)
	}
	nonMember, err := e.store.CreateUser(e.ctx, "issue-outsider-"+uniqueProjectKey(t)+"@example.com", "Issue Outsider")
	if err != nil {
		t.Fatalf("CreateUser nonmember: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, member.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "people",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	code, body := e.do(t, http.MethodPatch, e.issuePath(iss), map[string]any{
		"assignee_id": member.ID,
		"reporter_id": member.ID,
	})
	if code != http.StatusOK {
		t.Fatalf("set people code = %d body = %s", code, body)
	}
	got := decode[model.Issue](t, body)
	if got.AssigneeID == nil || *got.AssigneeID != member.ID || got.ReporterID == nil || *got.ReporterID != member.ID {
		t.Fatalf("people = assignee %v reporter %v, want %s", got.AssigneeID, got.ReporterID, member.ID)
	}

	code, body = e.do(t, http.MethodPatch, e.issuePath(iss), map[string]any{
		"clear_assignee": true,
		"clear_reporter": true,
	})
	if code != http.StatusOK {
		t.Fatalf("clear people code = %d body = %s", code, body)
	}
	got = decode[model.Issue](t, body)
	if got.AssigneeID != nil || got.ReporterID != nil {
		t.Fatalf("cleared people = assignee %v reporter %v, want nil", got.AssigneeID, got.ReporterID)
	}

	code, body = e.do(t, http.MethodPatch, e.issuePath(iss), map[string]any{"assignee_id": nonMember.ID})
	if code != http.StatusNotFound {
		t.Fatalf("non-member assignee code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, e.issuePath(iss), map[string]any{"reporter_id": uuid.NewString()})
	if code != http.StatusNotFound {
		t.Fatalf("missing reporter code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodPatch, e.issuePath(iss), map[string]any{"assignee_id": "not-a-uuid"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad assignee id code = %d body = %s", code, body)
	}
}

func TestHTTPPatchIssueCrossProjectSprint(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	other, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	otherSp, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: other.ID, Name: "x",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint other: %v", err)
	}
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID, Title: "t",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	code, _ := e.do(t, http.MethodPatch, e.issuePath(iss),
		map[string]any{"sprint": otherSp.Ref})
	if code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPPatchIssueCompletedSprintRejected(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	e.do(t, http.MethodPatch, e.sprintPath(sp), map[string]any{"status": "active"})
	e.do(t, http.MethodPost, e.sprintPath(sp)+"/complete", nil)
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	code, _ := e.do(t, http.MethodPatch, e.issuePath(iss),
		map[string]any{"sprint": sp.Ref})
	if code != http.StatusConflict {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListIssuesBacklogAndSprintFilters(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	inSprint, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "in"})
	if err != nil {
		t.Fatalf("CreateIssue in: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, inSprint.ID,
		store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	backlog, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "backlog"})
	if err != nil {
		t.Fatalf("CreateIssue backlog: %v", err)
	}

	code, body := e.do(t, http.MethodGet,
		e.projectIssuesPath()+"?sprint=backlog", nil)
	if code != http.StatusOK {
		t.Fatalf("backlog code = %d", code)
	}
	got := decodePage[model.Issue](t, body).Items
	if len(got) != 1 || got[0].ID != backlog.ID {
		t.Fatalf("backlog list = %+v, want only %s", got, backlog.ID)
	}

	code, body = e.do(t, http.MethodGet,
		e.projectIssuesPath()+"?sprint="+sp.Ref, nil)
	if code != http.StatusOK {
		t.Fatalf("by sprint code = %d", code)
	}
	got = decodePage[model.Issue](t, body).Items
	if len(got) != 1 || got[0].ID != inSprint.ID {
		t.Fatalf("by sprint = %+v, want only %s", got, inSprint.ID)
	}
}

func TestHTTPListIssuesAssigneeFilters(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	alice, _ := e.mustUserToken(t, "assignee-alice")
	bob, _ := e.mustUserToken(t, "assignee-bob")
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")

	aliceIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "alice sprint issue",
		AssigneeID: &alice.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue alice: %v", err)
	}
	bobIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "bob sprint issue",
		AssigneeID: &bob.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue bob: %v", err)
	}
	unassigned, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "unassigned sprint issue"})
	if err != nil {
		t.Fatalf("CreateIssue unassigned: %v", err)
	}
	aliceBacklog, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "alice backlog issue",
		AssigneeID: &alice.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue alice backlog: %v", err)
	}
	for _, issue := range []model.Issue{aliceIssue, bobIssue, unassigned} {
		if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
			t.Fatalf("assign %s: %v", issue.Identifier, err)
		}
	}
	done := model.StatusDone
	if _, err := e.store.UpdateIssue(e.ctx, bobIssue.ID, store.UpdateIssueParams{Status: &done}); err != nil {
		t.Fatalf("set bob done: %v", err)
	}

	code, body := e.do(t, http.MethodGet,
		e.projectIssuesPath()+"?sprint="+sp.Ref+"&assignee_id="+alice.ID.String()+"&assignee_id="+bob.ID.String(), nil)
	if code != http.StatusOK {
		t.Fatalf("assignee code = %d body = %s", code, body)
	}
	got := decodePage[model.Issue](t, body).Items
	if len(got) != 2 || got[0].ID != aliceIssue.ID || got[1].ID != bobIssue.ID {
		t.Fatalf("assignee list = %+v, want alice/bob sprint issues", got)
	}
	for _, notWant := range []uuid.UUID{unassigned.ID, aliceBacklog.ID} {
		for _, issue := range got {
			if issue.ID == notWant {
				t.Fatalf("assignee filter included %s in %+v", notWant, got)
			}
		}
	}

	code, body = e.do(t, http.MethodGet,
		e.projectIssuesPath()+"?sprint="+sp.Ref+"&status=done&assignee_id="+bob.ID.String(), nil)
	if code != http.StatusOK {
		t.Fatalf("assignee+status code = %d body = %s", code, body)
	}
	got = decodePage[model.Issue](t, body).Items
	if len(got) != 1 || got[0].ID != bobIssue.ID {
		t.Fatalf("assignee+status list = %+v, want bob done", got)
	}

	code, _ = e.do(t, http.MethodGet, e.projectIssuesPath()+"?assignee_id=nope", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad assignee_id code = %d", code)
	}
}

func TestHTTPListProjectAssignees(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	member, memberToken := e.mustProjectMemberToken(t, "project-assignee-member")
	assigned, _ := e.mustUserToken(t, "project-assignee-assigned")
	unrelated, _ := e.mustUserToken(t, "project-assignee-unrelated")
	if _, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "assigned issue",
		AssigneeID: &assigned.ID,
	}); err != nil {
		t.Fatalf("CreateIssue assigned: %v", err)
	}

	code, body := e.doWithToken(t, memberToken, http.MethodGet, e.projectPath()+"/assignees", nil)
	if code != http.StatusOK {
		t.Fatalf("assignees code = %d body = %s", code, body)
	}
	got := decode[[]model.ProjectAssignee](t, body)
	if !httpProjectAssigneesContain(got, member.ID) || !httpProjectAssigneesContain(got, assigned.ID) {
		t.Fatalf("assignees missing member/assigned: %+v", got)
	}
	if httpProjectAssigneesContain(got, unrelated.ID) {
		t.Fatalf("assignees included unrelated user: %+v", got)
	}

	_, deniedToken := e.mustUserToken(t, "project-assignee-denied")
	code, _ = e.doWithToken(t, deniedToken, http.MethodGet, e.projectPath()+"/assignees", nil)
	if code != http.StatusForbidden {
		t.Fatalf("denied assignees code = %d", code)
	}
}

func httpProjectAssigneesContain(in []model.ProjectAssignee, id uuid.UUID) bool {
	for _, assignee := range in {
		if assignee.ID == id {
			return true
		}
	}
	return false
}

func TestHTTPListIssuesMutuallyExclusive(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, _ := e.do(t, http.MethodGet,
		e.projectIssuesPath()+"?sprint=backlog&sprint_id="+sp.ID.String(), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListIssuesBadSprintParam(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet,
		e.projectIssuesPath()+"?sprint=potato", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListIssuesBadSprintID(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet,
		e.projectIssuesPath()+"?sprint_id=zzz", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

// ---------- pre-existing issue handlers (light coverage so sprint_id wiring
// in those handlers is exercised too) ----------

func TestHTTPCreateIssueAndGet(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, body := e.do(t, http.MethodPost,
		e.projectIssuesPath(),
		map[string]any{"title": "first"})
	if code != http.StatusCreated {
		t.Fatalf("create code = %d body = %s", code, body)
	}
	iss := decode[model.Issue](t, body)
	if iss.SprintID != nil {
		t.Fatalf("new issue should default to backlog, got sprint_id %v", iss.SprintID)
	}

	code, body = e.do(t, http.MethodGet, e.issuePath(iss), nil)
	if code != http.StatusOK {
		t.Fatalf("get code = %d", code)
	}
	got := decode[model.Issue](t, body)
	if got.ID != iss.ID {
		t.Fatal("id mismatch")
	}
}

func TestHTTPCreateAndUpdateIssuePriority(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, body := e.do(t, http.MethodPost,
		e.projectIssuesPath(),
		map[string]any{"title": "default priority"})
	if code != http.StatusCreated {
		t.Fatalf("create default code = %d body = %s", code, body)
	}
	defaultIssue := decode[model.Issue](t, body)
	if defaultIssue.Priority != model.PriorityP2 {
		t.Fatalf("default Priority = %q, want %q", defaultIssue.Priority, model.PriorityP2)
	}

	code, body = e.do(t, http.MethodPost,
		e.projectIssuesPath(),
		map[string]any{"title": "urgent", "priority": string(model.PriorityP0)})
	if code != http.StatusCreated {
		t.Fatalf("create explicit code = %d body = %s", code, body)
	}
	urgent := decode[model.Issue](t, body)
	if urgent.Priority != model.PriorityP0 {
		t.Fatalf("explicit Priority = %q, want %q", urgent.Priority, model.PriorityP0)
	}

	code, body = e.do(t, http.MethodPatch, e.issuePath(urgent),
		map[string]any{"priority": string(model.PriorityP4)})
	if code != http.StatusOK {
		t.Fatalf("patch priority code = %d body = %s", code, body)
	}
	updated := decode[model.Issue](t, body)
	if updated.Priority != model.PriorityP4 {
		t.Fatalf("updated Priority = %q, want %q", updated.Priority, model.PriorityP4)
	}

	code, _ = e.do(t, http.MethodPost,
		e.projectIssuesPath(),
		map[string]any{"title": "bad priority", "priority": "p0"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad create priority code = %d, want %d", code, http.StatusBadRequest)
	}

	code, _ = e.do(t, http.MethodPatch, e.issuePath(urgent),
		map[string]any{"priority": "P5"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad update priority code = %d, want %d", code, http.StatusBadRequest)
	}
	got, err := e.store.GetIssue(e.ctx, urgent.ID)
	if err != nil {
		t.Fatalf("GetIssue after bad priority: %v", err)
	}
	if got.Priority != model.PriorityP4 {
		t.Fatalf("bad priority changed Priority = %q, want %q", got.Priority, model.PriorityP4)
	}
}

func TestHTTPCreateAndUpdateIssueDueDate(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, body := e.do(t, http.MethodPost,
		e.projectIssuesPath(),
		map[string]any{"title": "due api", "due_date": "2026-06-24"})
	if code != http.StatusCreated {
		t.Fatalf("create due date code = %d body = %s", code, body)
	}
	iss := decode[model.Issue](t, body)
	if iss.DueDate == nil || iss.DueDate.String() != "2026-06-24" {
		t.Fatalf("create DueDate = %v", iss.DueDate)
	}

	code, body = e.do(t, http.MethodGet, e.issuePath(iss), nil)
	if code != http.StatusOK {
		t.Fatalf("get due date code = %d body = %s", code, body)
	}
	got := decode[model.Issue](t, body)
	if got.DueDate == nil || got.DueDate.String() != "2026-06-24" {
		t.Fatalf("get DueDate = %v", got.DueDate)
	}

	code, body = e.do(t, http.MethodGet, e.projectIssuesPath(), nil)
	if code != http.StatusOK {
		t.Fatalf("list due date code = %d body = %s", code, body)
	}
	listed := decodePage[model.Issue](t, body).Items
	if len(listed) != 1 || listed[0].DueDate == nil || listed[0].DueDate.String() != "2026-06-24" {
		t.Fatalf("listed = %+v", listed)
	}

	code, body = e.do(t, http.MethodPatch, e.issuePath(iss),
		map[string]any{"due_date": "2026-06-26"})
	if code != http.StatusOK {
		t.Fatalf("patch due date code = %d body = %s", code, body)
	}
	updated := decode[model.Issue](t, body)
	if updated.DueDate == nil || updated.DueDate.String() != "2026-06-26" {
		t.Fatalf("updated DueDate = %v", updated.DueDate)
	}

	code, body = e.do(t, http.MethodPost, e.issueSubIssuesPath(iss),
		map[string]any{"title": "due sub", "due_date": "2026-06-27"})
	if code != http.StatusCreated {
		t.Fatalf("create sub due date code = %d body = %s", code, body)
	}
	child := decode[model.Issue](t, body)
	if child.DueDate == nil || child.DueDate.String() != "2026-06-27" {
		t.Fatalf("child DueDate = %v", child.DueDate)
	}

	code, body = e.do(t, http.MethodPatch, e.issuePath(iss),
		map[string]any{"clear_due_date": true})
	if code != http.StatusOK {
		t.Fatalf("clear due date code = %d body = %s", code, body)
	}
	cleared := decode[model.Issue](t, body)
	if cleared.DueDate != nil {
		t.Fatalf("cleared DueDate = %v, want nil", cleared.DueDate)
	}

	code, _ = e.do(t, http.MethodPost,
		e.projectIssuesPath(),
		map[string]any{"title": "bad due", "due_date": "2026/06/24"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad create due date code = %d, want %d", code, http.StatusBadRequest)
	}

	code, _ = e.do(t, http.MethodPatch, e.issuePath(iss),
		map[string]any{"due_date": "tomorrow"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad update due date code = %d, want %d", code, http.StatusBadRequest)
	}
}

func TestHTTPCreateIssueBadProject(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost, "/bad!/projects/TRACK/issues",
		map[string]any{"title": "x"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateIssueMissingTitle(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost,
		e.projectIssuesPath(),
		map[string]any{"title": ""})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateIssueBadID(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPatch, "/"+e.ownerUsername+"/issues/not-uuid",
		map[string]any{"title": "x"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateIssueBadStatus(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "t"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	code, _ := e.do(t, http.MethodPatch, e.issuePath(iss),
		map[string]any{"status": "blocked"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateIssueTitleTooLong(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "t"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	long := string(bytes.Repeat([]byte("a"), 201))
	code, _ := e.do(t, http.MethodPatch, e.issuePath(iss),
		map[string]any{"title": long})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPGetIssueBadID(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, "/"+e.ownerUsername+"/issues/zzz", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListIssuesBadProjectID(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, "/bad!/projects/TRACK/issues", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListIssuesBadStatus(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet,
		e.projectIssuesPath()+"?status=banana", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}
