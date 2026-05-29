package server_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bradleymackey/track-slash/internal/migrations"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/server"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return os.Getenv("DATABASE_URL")
}

type httpEnv struct {
	ctx       context.Context
	ts        *httptest.Server
	store     *store.Store
	projectID uuid.UUID
	projKey   string
	adminID   uuid.UUID
	authToken string
}

func newHTTPEnv(t *testing.T) *httpEnv {
	t.Helper()
	dbURL := testDatabaseURL()
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL / DATABASE_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	sqlDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := migrations.Up(sqlDB); err != nil {
		t.Fatalf("migrations.Up: %v", err)
	}
	testutil.CleanDatabase(t, sqlDB)
	t.Cleanup(func() { testutil.CleanDatabase(t, sqlDB) })

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	s := store.New(pool)
	// Hub is nil — none of these handlers need realtime fanout; /ws route
	// is just skipped when hub == nil (see server.go:35).
	srv := server.New(s, nil, nil)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	key := uniqueProjectKey(t)
	proj, err := s.CreateProject(ctx, key, "http-test", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	admin, err := s.CreateOrUpdateAdminUser(ctx, "admin-"+key+"@example.com", "Admin")
	if err != nil {
		t.Fatalf("CreateOrUpdateAdminUser: %v", err)
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
		ctx: ctx, ts: ts, store: s, projectID: proj.ID, projKey: key,
		adminID: admin.ID, authToken: token.RawToken,
	}
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
	req, err := http.NewRequestWithContext(e.ctx, method, e.ts.URL+path, rdr)
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
	e := newHTTPEnv(t)
	code, body := e.do(t, http.MethodPost,
		fmt.Sprintf("/projects/%s/sprints", e.projectID),
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
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost, "/projects/not-a-uuid/sprints",
		map[string]any{"name": "S", "start_date": "2026-06-01", "end_date": "2026-06-14"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateSprintBadJSON(t *testing.T) {
	e := newHTTPEnv(t)
	req, _ := http.NewRequestWithContext(e.ctx, http.MethodPost,
		fmt.Sprintf("%s/projects/%s/sprints", e.ts.URL, e.projectID),
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
	e := newHTTPEnv(t)
	cases := []map[string]any{
		{"start_date": "2026/06/01", "end_date": "2026-06-14"},
		{"start_date": "2026-06-01", "end_date": "tomorrow"},
	}
	for _, body := range cases {
		body["name"] = "x"
		code, _ := e.do(t, http.MethodPost,
			fmt.Sprintf("/projects/%s/sprints", e.projectID), body)
		if code != http.StatusBadRequest {
			t.Fatalf("body=%v code=%d", body, code)
		}
	}
}

func TestHTTPCreateSprintEndBeforeStart(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost,
		fmt.Sprintf("/projects/%s/sprints", e.projectID),
		map[string]any{"name": "x", "start_date": "2026-06-14", "end_date": "2026-06-01"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateSprintNameTooLong(t *testing.T) {
	e := newHTTPEnv(t)
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	code, _ := e.do(t, http.MethodPost,
		fmt.Sprintf("/projects/%s/sprints", e.projectID),
		map[string]any{"name": string(long), "start_date": "2026-06-01", "end_date": "2026-06-14"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateSprintGoalTooLong(t *testing.T) {
	e := newHTTPEnv(t)
	long := make([]byte, 2001)
	for i := range long {
		long[i] = 'g'
	}
	code, _ := e.do(t, http.MethodPost,
		fmt.Sprintf("/projects/%s/sprints", e.projectID),
		map[string]any{"name": "x", "goal": string(long), "start_date": "2026-06-01", "end_date": "2026-06-14"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateSprintProjectNotFound(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost,
		fmt.Sprintf("/projects/%s/sprints", uuid.New()),
		map[string]any{"name": "x", "start_date": "2026-06-01", "end_date": "2026-06-14"})
	if code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

// ---------- listProjectSprints ----------

func TestHTTPListSprintsAndStatusFilter(t *testing.T) {
	e := newHTTPEnv(t)
	for i, name := range []string{"S1", "S2"} {
		e.do(t, http.MethodPost, fmt.Sprintf("/projects/%s/sprints", e.projectID),
			map[string]any{
				"name":       name,
				"start_date": fmt.Sprintf("2026-06-%02d", 1+i*14),
				"end_date":   fmt.Sprintf("2026-06-%02d", 14+i*14),
			})
	}
	code, body := e.do(t, http.MethodGet, fmt.Sprintf("/projects/%s/sprints", e.projectID), nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	sprints := decodePage[model.Sprint](t, body).Items
	if len(sprints) != 2 {
		t.Fatalf("len = %d", len(sprints))
	}

	code, _ = e.do(t, http.MethodGet,
		fmt.Sprintf("/projects/%s/sprints?status=completed", e.projectID), nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListSprintsBadStatus(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet,
		fmt.Sprintf("/projects/%s/sprints?status=banana", e.projectID), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListSprintsBadProjectID(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, "/projects/zzz/sprints", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

// ---------- getSprint ----------

func TestHTTPGetSprint(t *testing.T) {
	e := newHTTPEnv(t)
	_, body := e.do(t, http.MethodPost, fmt.Sprintf("/projects/%s/sprints", e.projectID),
		map[string]any{"name": "S", "start_date": "2026-06-01", "end_date": "2026-06-14"})
	sp := decode[model.Sprint](t, body)

	code, body := e.do(t, http.MethodGet, "/sprints/"+sp.ID.String(), nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	got := decode[model.Sprint](t, body)
	if got.ID != sp.ID {
		t.Fatalf("id mismatch")
	}
}

func TestHTTPGetSprintBadID(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, "/sprints/not-uuid", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPGetSprintNotFound(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, "/sprints/"+uuid.New().String(), nil)
	if code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

// ---------- updateSprint ----------

func createSprintFor(t *testing.T, e *httpEnv, name, start, end string) model.Sprint {
	t.Helper()
	_, body := e.do(t, http.MethodPost, fmt.Sprintf("/projects/%s/sprints", e.projectID),
		map[string]any{"name": name, "start_date": start, "end_date": end})
	return decode[model.Sprint](t, body)
}

func TestHTTPUpdateSprintAllFields(t *testing.T) {
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, body := e.do(t, http.MethodPatch, "/sprints/"+sp.ID.String(), map[string]any{
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
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, body := e.do(t, http.MethodPatch, "/sprints/"+sp.ID.String(),
		map[string]any{"status": "active"})
	if code != http.StatusOK {
		t.Fatalf("code = %d body = %s", code, body)
	}
	if decode[model.Sprint](t, body).Status != model.SprintStatusActive {
		t.Fatal("not active")
	}
}

func TestHTTPUpdateSprintRejectsCompleted(t *testing.T) {
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, _ := e.do(t, http.MethodPatch, "/sprints/"+sp.ID.String(),
		map[string]any{"status": "completed"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateSprintInvalidStatus(t *testing.T) {
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, _ := e.do(t, http.MethodPatch, "/sprints/"+sp.ID.String(),
		map[string]any{"status": "potato"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateSprintBadDate(t *testing.T) {
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	cases := []map[string]any{
		{"start_date": "yesterday"},
		{"end_date": "tomorrow"},
	}
	for _, body := range cases {
		code, _ := e.do(t, http.MethodPatch, "/sprints/"+sp.ID.String(), body)
		if code != http.StatusBadRequest {
			t.Fatalf("body=%v code=%d", body, code)
		}
	}
}

func TestHTTPUpdateSprintNameAndGoalTooLong(t *testing.T) {
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")

	bigName := string(bytes.Repeat([]byte("a"), 201))
	bigGoal := string(bytes.Repeat([]byte("g"), 2001))
	for _, body := range []map[string]any{{"name": bigName}, {"goal": bigGoal}} {
		code, _ := e.do(t, http.MethodPatch, "/sprints/"+sp.ID.String(), body)
		if code != http.StatusBadRequest {
			t.Fatalf("body=%v code=%d", body, code)
		}
	}
}

func TestHTTPUpdateSprintBadJSON(t *testing.T) {
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	req, _ := http.NewRequestWithContext(e.ctx, http.MethodPatch,
		e.ts.URL+"/sprints/"+sp.ID.String(), bytes.NewReader([]byte("nope")))
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
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPatch, "/sprints/not-uuid",
		map[string]any{"name": "x"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateSprintNotFound(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPatch, "/sprints/"+uuid.New().String(),
		map[string]any{"name": "x"})
	if code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateSprintActivationConflict(t *testing.T) {
	e := newHTTPEnv(t)
	a := createSprintFor(t, e, "A", "2026-06-01", "2026-06-14")
	b := createSprintFor(t, e, "B", "2026-06-15", "2026-06-28")
	if code, _ := e.do(t, http.MethodPatch, "/sprints/"+a.ID.String(),
		map[string]any{"status": "active"}); code != http.StatusOK {
		t.Fatalf("activate A code = %d", code)
	}
	code, _ := e.do(t, http.MethodPatch, "/sprints/"+b.ID.String(),
		map[string]any{"status": "active"})
	if code != http.StatusConflict {
		t.Fatalf("code = %d", code)
	}
}

// ---------- completeSprint ----------

func TestHTTPCompleteSprintHappy(t *testing.T) {
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	e.do(t, http.MethodPatch, "/sprints/"+sp.ID.String(), map[string]any{"status": "active"})

	code, body := e.do(t, http.MethodPost, "/sprints/"+sp.ID.String()+"/complete", nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d body = %s", code, body)
	}
	got := decode[model.Sprint](t, body)
	if got.Status != model.SprintStatusCompleted {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestHTTPCompleteSprintConflictNonActive(t *testing.T) {
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, _ := e.do(t, http.MethodPost, "/sprints/"+sp.ID.String()+"/complete", nil)
	if code != http.StatusConflict {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCompleteSprintBadID(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost, "/sprints/zzz/complete", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCompleteSprintNotFound(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost, "/sprints/"+uuid.New().String()+"/complete", nil)
	if code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

// ---------- issue PATCH + list with sprint filters ----------

func TestHTTPPatchIssueSetSprint(t *testing.T) {
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")

	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID, Title: "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	code, body := e.do(t, http.MethodPatch, "/issues/"+iss.ID.String(),
		map[string]any{"sprint_id": sp.ID.String()})
	if code != http.StatusOK {
		t.Fatalf("code = %d body = %s", code, body)
	}
	got := decode[model.Issue](t, body)
	if got.SprintID == nil || *got.SprintID != sp.ID {
		t.Fatalf("sprint_id = %v", got.SprintID)
	}

	code, body = e.do(t, http.MethodPatch, "/issues/"+iss.ID.String(),
		map[string]any{"clear_sprint": true})
	if code != http.StatusOK {
		t.Fatalf("clear code = %d body = %s", code, body)
	}
	got = decode[model.Issue](t, body)
	if got.SprintID != nil {
		t.Fatalf("sprint_id = %v, want nil", got.SprintID)
	}
}

func TestHTTPPatchIssueCrossProjectSprint(t *testing.T) {
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

	code, _ := e.do(t, http.MethodPatch, "/issues/"+iss.ID.String(),
		map[string]any{"sprint_id": otherSp.ID.String()})
	if code != http.StatusConflict {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListIssuesBacklogAndSprintFilters(t *testing.T) {
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
		fmt.Sprintf("/projects/%s/issues?sprint=backlog", e.projectID), nil)
	if code != http.StatusOK {
		t.Fatalf("backlog code = %d", code)
	}
	got := decodePage[model.Issue](t, body).Items
	if len(got) != 1 || got[0].ID != backlog.ID {
		t.Fatalf("backlog list = %+v, want only %s", got, backlog.ID)
	}

	code, body = e.do(t, http.MethodGet,
		fmt.Sprintf("/projects/%s/issues?sprint_id=%s", e.projectID, sp.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("by sprint code = %d", code)
	}
	got = decodePage[model.Issue](t, body).Items
	if len(got) != 1 || got[0].ID != inSprint.ID {
		t.Fatalf("by sprint = %+v, want only %s", got, inSprint.ID)
	}
}

func TestHTTPListIssuesMutuallyExclusive(t *testing.T) {
	e := newHTTPEnv(t)
	sp := createSprintFor(t, e, "S", "2026-06-01", "2026-06-14")
	code, _ := e.do(t, http.MethodGet,
		fmt.Sprintf("/projects/%s/issues?sprint=backlog&sprint_id=%s", e.projectID, sp.ID), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListIssuesBadSprintParam(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet,
		fmt.Sprintf("/projects/%s/issues?sprint=potato", e.projectID), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListIssuesBadSprintID(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet,
		fmt.Sprintf("/projects/%s/issues?sprint_id=zzz", e.projectID), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

// ---------- pre-existing issue handlers (light coverage so sprint_id wiring
// in those handlers is exercised too) ----------

func TestHTTPCreateIssueAndGet(t *testing.T) {
	e := newHTTPEnv(t)
	code, body := e.do(t, http.MethodPost,
		fmt.Sprintf("/projects/%s/issues", e.projectID),
		map[string]any{"title": "first"})
	if code != http.StatusCreated {
		t.Fatalf("create code = %d body = %s", code, body)
	}
	iss := decode[model.Issue](t, body)
	if iss.SprintID != nil {
		t.Fatalf("new issue should default to backlog, got sprint_id %v", iss.SprintID)
	}

	code, body = e.do(t, http.MethodGet, "/issues/"+iss.ID.String(), nil)
	if code != http.StatusOK {
		t.Fatalf("get code = %d", code)
	}
	got := decode[model.Issue](t, body)
	if got.ID != iss.ID {
		t.Fatal("id mismatch")
	}
}

func TestHTTPCreateIssueBadProject(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost, "/projects/not-uuid/issues",
		map[string]any{"title": "x"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPCreateIssueMissingTitle(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPost,
		fmt.Sprintf("/projects/%s/issues", e.projectID),
		map[string]any{"title": ""})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateIssueBadID(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodPatch, "/issues/not-uuid",
		map[string]any{"title": "x"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateIssueBadStatus(t *testing.T) {
	e := newHTTPEnv(t)
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "t"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	code, _ := e.do(t, http.MethodPatch, "/issues/"+iss.ID.String(),
		map[string]any{"status": "blocked"})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPUpdateIssueTitleTooLong(t *testing.T) {
	e := newHTTPEnv(t)
	iss, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "t"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	long := string(bytes.Repeat([]byte("a"), 201))
	code, _ := e.do(t, http.MethodPatch, "/issues/"+iss.ID.String(),
		map[string]any{"title": long})
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPGetIssueBadID(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, "/issues/zzz", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListIssuesBadProjectID(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet, "/projects/not-uuid/issues", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

func TestHTTPListIssuesBadStatus(t *testing.T) {
	e := newHTTPEnv(t)
	code, _ := e.do(t, http.MethodGet,
		fmt.Sprintf("/projects/%s/issues?status=banana", e.projectID), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}
