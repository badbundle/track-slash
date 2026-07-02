package server_test

import (
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/google/uuid"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestUIProjectsPageListsVisibleProjectsAndCreatesProject(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-projects")
	hidden, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Hidden UI Project", "")
	if err != nil {
		t.Fatalf("CreateProject hidden: %v", err)
	}

	body := e.uiGet(t, "/projects", token)
	for _, want := range []string{"Projects", "Projects you can access.", `aria-label="New project"`, `href="/projects/new"`, `hx-get="/projects/new/panel"`, e.projKey, "http-test", "inline-flex w-fit justify-self-start", `href="` + e.projectPath() + `/sprint"`, `hx-get="` + e.projectPath() + `/sprint/panel"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("projects body missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{`id="project-key"`, `>Create project<`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("projects body still included project form %q: %s", notWant, body)
		}
	}
	if strings.Contains(body, `href="`+e.projectPath()+`/backlog"`) {
		t.Fatalf("projects body included backlog row action: %s", body)
	}
	if strings.Contains(body, hidden.Name) {
		t.Fatalf("projects body included inaccessible project: %s", body)
	}

	body = e.uiGet(t, "/projects/new", token)
	for _, want := range []string{"New project", "Create project", `action="/projects"`, `id="project-key"`, `id="project-name"`, `id="project-description"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("new project body missing %q: %s", want, body)
		}
	}
	newProjectMain := mainContentBlock(t, body)
	for _, notWant := range []string{`data-lucide="arrow-left"`, `href="/projects"`, `hx-get="/projects/panel"`} {
		if strings.Contains(newProjectMain, notWant) {
			t.Fatalf("new project body should not render back button markup %q: %s", notWant, body)
		}
	}

	form := url.Values{"key": {"bad"}, "name": {"Bad"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/projects", token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Key must match") || !strings.Contains(body, "New project") || !strings.Contains(body, `value="bad"`) {
		t.Fatalf("bad key code = %d body = %s", res.StatusCode, body)
	}

	dupKey := uniqueProjectKey(t)
	if _, err := e.store.CreateProjectForUser(e.ctx, user.ID, dupKey, "Duplicate Source", ""); err != nil {
		t.Fatalf("CreateProjectForUser duplicate source: %v", err)
	}
	form = url.Values{"key": {dupKey}, "name": {"Duplicate"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, "/projects", token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusConflict || !strings.Contains(body, "Project key already exists.") || !strings.Contains(body, "New project") {
		t.Fatalf("duplicate code = %d body = %s", res.StatusCode, body)
	}

	key := uniqueProjectKey(t)
	form = url.Values{"key": {key}, "name": {"Created UI Project"}, "description": {"from UI"}}
	res = e.uiDoNoRedirect(t, http.MethodPost, "/projects", token, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("create code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	loc := res.Header.Get("Location")
	if loc != "/"+user.Username+"/projects/"+key+"/sprint" {
		t.Fatalf("Location = %q", loc)
	}
	body = e.uiGet(t, loc, token)
	if !strings.Contains(body, "Created UI Project") {
		t.Fatalf("created project page missing values: %s", body)
	}
}

func TestUIProjectAboutStats(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-stats")
	todoIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "about stats todo",
		AssigneeID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue todo: %v", err)
	}
	_ = todoIssue
	doneIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      "about stats done",
		AssigneeID: &user.ID,
	})
	if err != nil {
		t.Fatalf("CreateIssue done: %v", err)
	}
	done := model.StatusDone
	if _, err := e.store.UpdateIssue(e.ctx, doneIssue.ID, store.UpdateIssueParams{Status: &done}); err != nil {
		t.Fatalf("UpdateIssue done: %v", err)
	}

	body := e.uiGet(t, e.projectPath()+"/about", token)
	for _, want := range []string{"Issue stats", "All time", "Last 7 days", "Top assignees", "ui-stats", ">2</td>", ">1</td>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("project about stats missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "No assigned issues.") {
		t.Fatalf("project about stats rendered empty assignee state: %s", body)
	}
}

func TestUIRendersProjectSprintBoard(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-board")
	sp, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Board Sprint",
		Goal:      "Focus current sprint goals\nShip board clarity",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	active := model.SprintStatusActive
	if _, err := e.store.UpdateSprint(e.ctx, sp.ID, store.UpdateSprintParams{Status: &active}); err != nil {
		t.Fatalf("UpdateSprint active: %v", err)
	}
	earlyDue, err := model.ParseDate("2099-06-24")
	if err != nil {
		t.Fatalf("ParseDate early: %v", err)
	}
	lateDue, err := model.ParseDate("2099-06-26")
	if err != nil {
		t.Fatalf("ParseDate late: %v", err)
	}
	todo, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "board todo issue", AssigneeID: &user.ID, DueDate: &earlyDue})
	if err != nil {
		t.Fatalf("CreateIssue todo: %v", err)
	}
	laterTodo, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "board later todo issue", Priority: model.PriorityP1, DueDate: &lateDue})
	if err != nil {
		t.Fatalf("CreateIssue later todo: %v", err)
	}
	inProgress, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "board progress issue", Priority: model.PriorityP0})
	if err != nil {
		t.Fatalf("CreateIssue progress: %v", err)
	}
	closedIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "board closed issue"})
	if err != nil {
		t.Fatalf("CreateIssue closed: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, todo.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign todo: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, laterTodo.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign later todo: %v", err)
	}
	inProgressStatus := model.StatusInProgress
	if _, err := e.store.UpdateIssue(e.ctx, inProgress.ID, store.UpdateIssueParams{SprintID: &sp.ID, Status: &inProgressStatus}); err != nil {
		t.Fatalf("assign progress: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, closedIssue.ID, store.UpdateIssueParams{SprintID: &sp.ID}); err != nil {
		t.Fatalf("assign closed: %v", err)
	}
	closedStatus := model.StatusClosed
	closedReason := model.CloseReasonWontDo
	if _, err := e.store.UpdateIssue(e.ctx, closedIssue.ID, store.UpdateIssueParams{Status: &closedStatus, CloseReason: &closedReason}); err != nil {
		t.Fatalf("close issue: %v", err)
	}
	otherProject, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Other UI Project", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, otherProject.ID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess other: %v", err)
	}
	otherSprint, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: otherProject.ID,
		Name:      "Other Sprint",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint other: %v", err)
	}
	if _, err := e.store.UpdateSprint(e.ctx, otherSprint.ID, store.UpdateSprintParams{Status: &active}); err != nil {
		t.Fatalf("UpdateSprint other active: %v", err)
	}
	otherIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: otherProject.ID, Title: "other project sprint issue"})
	if err != nil {
		t.Fatalf("CreateIssue other: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, otherIssue.ID, store.UpdateIssueParams{SprintID: &otherSprint.ID}); err != nil {
		t.Fatalf("assign other: %v", err)
	}

	body := e.uiGet(t, e.projectPath()+"/sprint", token)
	for _, want := range []string{"Sprint", "To do", "In progress", "Done", "Closed", "board todo issue", "board later todo issue", "board progress issue", "board closed issue", "Board Sprint", "Focus current sprint goals\nShip board clarity", `aria-label="Assigned to ui-board"`, ">U</span>", `aria-label="Issue controls"`, "Status", "Priority", "Sort", "Direction", "Due date", "Asc", "Desc", `data-lucide="arrow-up"`, `data-lucide="arrow-down"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("sprint body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "other project sprint issue") || strings.Contains(body, "Active sprint issues across accessible projects.") {
		t.Fatalf("sprint body included wrong scope/copy: %s", body)
	}

	filteredBody := e.uiGet(t, e.projectPath()+"/sprint?status=in_progress&priority=P0", token)
	if !strings.Contains(filteredBody, "board progress issue") {
		t.Fatalf("filtered sprint missing progress issue: %s", filteredBody)
	}
	for _, notWant := range []string{"board todo issue", "board later todo issue", "board closed issue"} {
		if strings.Contains(filteredBody, notWant) {
			t.Fatalf("filtered sprint included %q: %s", notWant, filteredBody)
		}
	}

	dueDescBody := e.uiGet(t, e.projectPath()+"/sprint?sort=due&direction=desc", token)
	laterIdx := strings.Index(dueDescBody, "board later todo issue")
	earlyIdx := strings.Index(dueDescBody, "board todo issue")
	if laterIdx < 0 || earlyIdx < 0 || laterIdx > earlyIdx {
		t.Fatalf("due desc sprint order wrong: later=%d early=%d body=%s", laterIdx, earlyIdx, dueDescBody)
	}
}

func TestUIProjectAssigneeFilterAppliesAcrossProjectSections(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	alice, token := e.mustProjectMemberToken(t, "ui-filter-alice")
	bob, _ := e.mustProjectMemberToken(t, "ui-filter-bob")
	var err error
	alice, err = e.store.UpdateUserProfile(e.ctx, alice.ID, "Alice Filter", alice.Email)
	if err != nil {
		t.Fatalf("UpdateUserProfile alice: %v", err)
	}
	bob, err = e.store.UpdateUserProfile(e.ctx, bob.ID, "Bob Filter", bob.Email)
	if err != nil {
		t.Fatalf("UpdateUserProfile bob: %v", err)
	}
	activeSprint, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Filtered Active Sprint",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint active: %v", err)
	}
	active := model.SprintStatusActive
	if _, err := e.store.UpdateSprint(e.ctx, activeSprint.ID, store.UpdateSprintParams{Status: &active}); err != nil {
		t.Fatalf("UpdateSprint active: %v", err)
	}
	plannedSprint, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Filtered Planned Sprint",
		StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint planned: %v", err)
	}
	aliceSprint := createAssignedIssueForUI(t, e, "alice sprint issue", alice.ID)
	bobSprint := createAssignedIssueForUI(t, e, "bob sprint issue", bob.ID)
	unassignedSprint, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "unassigned sprint issue"})
	if err != nil {
		t.Fatalf("CreateIssue unassigned sprint: %v", err)
	}
	for _, issue := range []model.Issue{aliceSprint, bobSprint, unassignedSprint} {
		if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{SprintID: &activeSprint.ID}); err != nil {
			t.Fatalf("assign active %s: %v", issue.Identifier, err)
		}
	}
	alicePlanned := createAssignedIssueForUI(t, e, "alice planned issue", alice.ID)
	bobPlanned := createAssignedIssueForUI(t, e, "bob planned issue", bob.ID)
	for _, issue := range []model.Issue{alicePlanned, bobPlanned} {
		if _, err := e.store.UpdateIssue(e.ctx, issue.ID, store.UpdateIssueParams{SprintID: &plannedSprint.ID}); err != nil {
			t.Fatalf("assign planned %s: %v", issue.Identifier, err)
		}
	}
	aliceBacklog := createAssignedIssueForUI(t, e, "alice backlog issue", alice.ID)
	bobBacklog := createAssignedIssueForUI(t, e, "bob backlog issue", bob.ID)
	doneStatus := model.StatusDone
	if _, err := e.store.UpdateIssue(e.ctx, bobBacklog.ID, store.UpdateIssueParams{Status: &doneStatus}); err != nil {
		t.Fatalf("set bob backlog done: %v", err)
	}

	aliceQuery := "?assignee_id=" + alice.ID.String()
	body := e.uiGet(t, e.projectPath()+"/sprint"+aliceQuery, token)
	for _, want := range []string{
		`aria-label="Issue controls"`,
		`aria-label="Toggle Alice Filter"`,
		`aria-label="Toggle Bob Filter"`,
		`aria-pressed="true"`,
		"AF",
		"BF",
		"alice sprint issue",
		`href="` + e.projectPath() + `/planned"`,
		`href="` + e.projectPath() + `/all"`,
		`assignee_id=` + bob.ID.String(),
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("filtered sprint missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{"bob sprint issue", "unassigned sprint issue"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("filtered sprint included %q: %s", notWant, body)
		}
	}

	body = e.uiGet(t, e.projectPath()+"/all"+aliceQuery, token)
	for _, want := range []string{"alice sprint issue", "alice planned issue", "alice backlog issue"} {
		if !strings.Contains(body, want) {
			t.Fatalf("filtered all issues missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{"bob sprint issue", "bob planned issue", "bob backlog issue", "unassigned sprint issue"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("filtered all issues included %q: %s", notWant, body)
		}
	}

	multiAllQuery := "?assignee_id=" + alice.ID.String() + "&assignee_id=" + bob.ID.String() + "&status=todo&status=done"
	body = e.uiGet(t, e.projectPath()+"/all"+multiAllQuery, token)
	for _, want := range []string{
		`aria-label="Issue controls"`,
		`aria-pressed="true"`,
		"alice sprint issue",
		"bob sprint issue",
		"alice planned issue",
		"bob planned issue",
		"alice backlog issue",
		"bob backlog issue",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("multi-filter all issues missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "unassigned sprint issue") {
		t.Fatalf("multi-filter all issues included unassigned issue: %s", body)
	}

	body = e.uiGet(t, e.projectPath()+"/sprint"+aliceQuery+"&assignee_id="+bob.ID.String(), token)
	for _, want := range []string{"alice sprint issue", "bob sprint issue"} {
		if !strings.Contains(body, want) {
			t.Fatalf("multi-filter sprint missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "unassigned sprint issue") || aliceBacklog.ID == uuid.Nil || bobBacklog.ID == uuid.Nil {
		t.Fatalf("multi-filter sprint included wrong issue or setup failed: %s", body)
	}
}

func TestUIRendersProjectPlannedAndAll(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-backlog")
	backlogIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "issue still in backlog"})
	if err != nil {
		t.Fatalf("CreateIssue backlog: %v", err)
	}
	firstPlanned, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "First Planned Sprint",
		StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint first planned: %v", err)
	}
	secondPlanned, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Second Planned Sprint",
		StartDate: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint second planned: %v", err)
	}
	if _, err := e.store.ReorderPlannedSprints(e.ctx, store.ReorderPlannedSprintsParams{
		ProjectID: e.projectID,
		SprintIDs: []uuid.UUID{secondPlanned.ID, firstPlanned.ID},
	}); err != nil {
		t.Fatalf("ReorderPlannedSprints: %v", err)
	}
	firstPlannedIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "scheduled first issue"})
	if err != nil {
		t.Fatalf("CreateIssue first planned: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, firstPlannedIssue.ID, store.UpdateIssueParams{SprintID: &firstPlanned.ID}); err != nil {
		t.Fatalf("assign first planned: %v", err)
	}
	secondPlannedIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: e.projectID, Title: "scheduled second issue"})
	if err != nil {
		t.Fatalf("CreateIssue second planned: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, secondPlannedIssue.ID, store.UpdateIssueParams{SprintID: &secondPlanned.ID}); err != nil {
		t.Fatalf("assign second planned: %v", err)
	}

	otherProject, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Other Backlog Project", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, otherProject.ID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess other: %v", err)
	}
	if _, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: otherProject.ID, Title: "other project backlog issue"}); err != nil {
		t.Fatalf("CreateIssue other backlog: %v", err)
	}
	otherPlanned, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: otherProject.ID,
		Name:      "Other Planned Sprint",
		StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateSprint other planned: %v", err)
	}
	otherPlannedIssue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{ProjectID: otherProject.ID, Title: "other project planned issue"})
	if err != nil {
		t.Fatalf("CreateIssue other planned: %v", err)
	}
	if _, err := e.store.UpdateIssue(e.ctx, otherPlannedIssue.ID, store.UpdateIssueParams{SprintID: &otherPlanned.ID}); err != nil {
		t.Fatalf("assign other planned: %v", err)
	}

	body := e.uiGet(t, e.projectPath()+"/planned", token)
	for _, want := range []string{"Planned", "Second Planned Sprint", "First Planned Sprint", "scheduled second issue", "scheduled first issue"} {
		if !strings.Contains(body, want) {
			t.Fatalf("planned body missing %q: %s", want, body)
		}
	}
	secondIdx := strings.Index(body, "scheduled second issue")
	firstIdx := strings.Index(body, "scheduled first issue")
	if secondIdx < 0 || firstIdx < 0 || secondIdx > firstIdx {
		t.Fatalf("planned order wrong: second=%d first=%d body=%s", secondIdx, firstIdx, body)
	}
	if strings.Contains(body, backlogIssue.Title) {
		t.Fatalf("planned body included unscheduled issue: %s", body)
	}
	if strings.Contains(body, "other project backlog issue") || strings.Contains(body, "other project planned issue") || strings.Contains(body, "Other Planned Sprint") || strings.Contains(body, "Backlog issues across accessible projects.") {
		t.Fatalf("planned body included wrong scope/copy: %s", body)
	}

	body = e.uiGet(t, e.projectPath()+"/all", token)
	for _, want := range []string{"All issues", "Issue controls", "Status", "Priority", "Sort", "Updated", "Any", backlogIssue.Title, "scheduled first issue", "scheduled second issue"} {
		if !strings.Contains(body, want) {
			t.Fatalf("all body missing %q: %s", want, body)
		}
	}
	backlogIdx := strings.Index(body, backlogIssue.Title)
	firstIdx = strings.Index(body, "scheduled first issue")
	secondIdx = strings.Index(body, "scheduled second issue")
	if backlogIdx < 0 || firstIdx < 0 || secondIdx < 0 || secondIdx > firstIdx || firstIdx > backlogIdx {
		t.Fatalf("all issue order wrong: backlog=%d first=%d second=%d body=%s", backlogIdx, firstIdx, secondIdx, body)
	}
	if strings.Contains(body, "Other Planned Sprint") || strings.Contains(body, "other project backlog issue") || strings.Contains(body, "other project planned issue") {
		t.Fatalf("all body included wrong scope: %s", body)
	}
}

func TestUIProjectDeletedPageListsAndRestoresIssues(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-deleted")
	live, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "live issue outside deleted tab",
	})
	if err != nil {
		t.Fatalf("CreateIssue live: %v", err)
	}
	deleted, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "deleted tab target issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue deleted: %v", err)
	}
	parent, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "deleted tab parent issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	child, err := e.store.CreateSubIssue(e.ctx, store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         "deleted tab child issue",
	})
	if err != nil {
		t.Fatalf("CreateSubIssue child: %v", err)
	}
	otherProject, err := e.store.CreateProject(e.ctx, uniqueProjectKey(t), "Other Deleted Project", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	if _, err := e.store.GrantProjectAccess(e.ctx, otherProject.ID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess other: %v", err)
	}
	otherDeleted, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: otherProject.ID,
		Title:     "other project deleted issue",
	})
	if err != nil {
		t.Fatalf("CreateIssue other deleted: %v", err)
	}
	if err := e.store.DeleteIssue(e.ctx, deleted.ID); err != nil {
		t.Fatalf("DeleteIssue deleted: %v", err)
	}
	if err := e.store.DeleteIssue(e.ctx, parent.ID); err != nil {
		t.Fatalf("DeleteIssue parent: %v", err)
	}
	if err := e.store.DeleteIssue(e.ctx, otherDeleted.ID); err != nil {
		t.Fatalf("DeleteIssue other: %v", err)
	}

	projectBody := e.uiGet(t, e.projectPath()+"/planned", token)
	for _, want := range []string{
		`aria-label="Project actions"`,
		`data-lucide="more-horizontal"`,
		`href="` + e.projectPath() + `/deleted"`,
		`hx-get="` + e.projectPath() + `/deleted/panel"`,
		"Deleted issues",
	} {
		if !strings.Contains(projectBody, want) {
			t.Fatalf("project body missing deleted menu affordance %q: %s", want, projectBody)
		}
	}
	tabStart := strings.Index(projectBody, `aria-label="Project views"`)
	if tabStart < 0 {
		t.Fatalf("project body missing tab nav: %s", projectBody)
	}
	tabEnd := strings.Index(projectBody[tabStart:], "</nav>")
	if tabEnd < 0 {
		t.Fatalf("project body missing tab nav close: %s", projectBody)
	}
	tabMarkup := projectBody[tabStart : tabStart+tabEnd]
	if strings.Contains(tabMarkup, "Deleted") || strings.Contains(tabMarkup, `/deleted`) {
		t.Fatalf("deleted rendered as project tab: %s", projectBody)
	}

	body := e.uiGet(t, e.projectPath()+"/deleted", token)
	for _, want := range []string{
		"Deleted issues",
		e.projKey,
		deleted.Identifier,
		deleted.Title,
		`href="` + e.issuePath(deleted) + `"`,
		`hx-get="` + e.issuePath(deleted) + `/panel"`,
		parent.Title,
		child.Title,
		"Sub-issue",
		`method="post" action="` + e.issuePath(deleted) + `/restore"`,
		`hx-post="` + e.issuePath(deleted) + `/restore"`,
		`method="post" action="` + e.issuePath(child) + `/restore"`,
		`hx-post="` + e.issuePath(child) + `/restore"`,
		`hx-push-url="` + e.issuePath(child) + `"`,
		`data-lucide="rotate-ccw"`,
		"Restore",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("deleted body missing %q: %s", want, body)
		}
	}
	for _, notWant := range []string{`data-lucide="arrow-left"`, `href="` + e.projectPath() + `/sprint"`, `hx-get="` + e.projectPath() + `/sprint/panel"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("deleted body should not render back button markup %q: %s", notWant, body)
		}
	}
	for _, notWant := range []string{live.Title, otherDeleted.Title, "Issue deleted", `aria-label="Project views"`, `aria-label="Project actions"`} {
		if strings.Contains(body, notWant) {
			t.Fatalf("deleted body included %q: %s", notWant, body)
		}
	}

	res := e.uiDoNoRedirectWithHeaders(t, http.MethodPost, e.issuePath(child)+"/restore", token, nil, map[string]string{
		"HX-Request": "true",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("deleted restore code = %d body = %s", res.StatusCode, body)
	}
	if push := res.Header.Get("HX-Push-Url"); push != e.issuePath(child) {
		t.Fatalf("restore HX-Push-Url = %q", push)
	}
	if strings.Contains(body, "<!doctype html>") || !strings.Contains(body, child.Title) || !strings.Contains(body, "Sub-issue of") || !strings.Contains(body, parent.Title) || !strings.Contains(body, `aria-label="Issue actions"`) {
		t.Fatalf("deleted restore should render restored issue panel: %s", body)
	}
	if _, err := e.store.GetIssue(e.ctx, child.ID); err != nil {
		t.Fatalf("GetIssue child restored from deleted tab: %v", err)
	}
	if _, err := e.store.GetIssue(e.ctx, parent.ID); err != nil {
		t.Fatalf("GetIssue parent restored with child: %v", err)
	}
	body = e.uiGet(t, e.projectPath()+"/deleted", token)
	if strings.Contains(body, parent.Title) || strings.Contains(body, child.Title) {
		t.Fatalf("deleted body kept restored parent/child: %s", body)
	}
	if !strings.Contains(body, deleted.Title) {
		t.Fatalf("deleted body lost remaining deleted issue: %s", body)
	}
}
