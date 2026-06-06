package server

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
)

func TestUIProjectIcon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "Roadmap", key: "TRACK", want: "R"},
		{name: " roadmap", key: "TRACK", want: "R"},
		{name: "", key: "TRACK", want: "T"},
		{name: "", key: "", want: "?"},
	}

	for _, tt := range tests {
		if got := uiProjectIcon(tt.name, tt.key); got != tt.want {
			t.Fatalf("uiProjectIcon(%q, %q) = %q, want %q", tt.name, tt.key, got, tt.want)
		}
	}
}

func TestSafeUINextRootPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: "/"},
		{name: "root", raw: "/", want: "/"},
		{name: "removed work", raw: "/sprint", want: "/"},
		{name: "removed work panel with query", raw: "/sprint/panel?x=1", want: "/"},
		{name: "projects", raw: "/projects", want: "/projects"},
		{name: "projects panel", raw: "/projects/panel", want: "/projects/panel"},
		{name: "issue", raw: "/issues/8cc21ed4-2d69-4d43-9f0c-402736e4aa16", want: "/issues/8cc21ed4-2d69-4d43-9f0c-402736e4aa16"},
		{name: "issue panel with query", raw: "/issues/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/panel?x=1", want: "/issues/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/panel?x=1"},
		{name: "bad issue id", raw: "/issues/nope", want: "/"},
		{name: "bad issue child", raw: "/issues/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/activity", want: "/"},
		{name: "bad issue nested panel", raw: "/issues/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/panel/extra", want: "/"},
		{name: "project", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16", want: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16"},
		{name: "project sprint", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/sprint", want: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/sprint"},
		{name: "project backlog panel with query", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/backlog/panel?x=1", want: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/backlog/panel?x=1"},
		{name: "bad project id", raw: "/projects/nope/sprint", want: "/"},
		{name: "bad project child", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/issues", want: "/"},
		{name: "bad project panel", raw: "/projects/8cc21ed4-2d69-4d43-9f0c-402736e4aa16/sprint/card", want: "/"},
		{name: "api", raw: "/api/v1/projects", want: "/"},
		{name: "legacy app", raw: "/app/sprint", want: "/"},
		{name: "scheme relative", raw: "//evil.example/sprint", want: "/"},
		{name: "relative", raw: "sprint", want: "/"},
	}

	for _, tt := range tests {
		if got := safeUINext(tt.raw); got != tt.want {
			t.Fatalf("%s: safeUINext(%q) = %q, want %q", tt.name, tt.raw, got, tt.want)
		}
	}
}

func TestUIIssuePanelRendersReadonlyDetail(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	linkedID := uuid.MustParse("ae77b9b8-9dcf-4a18-8b69-42b97bd4a4b5")
	userID := uuid.MustParse("23f14acb-6a57-4035-a046-33e93ffbd5bb")
	commentID := uuid.MustParse("d0c74b63-c75c-42b0-b899-6baf6948e3fd")
	linkID := uuid.MustParse("48c98f2e-bad8-4054-89d7-5a45a68af54f")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	assignee := model.User{ID: userID, Name: "Ada Lovelace", Email: "ada@example.com"}
	reporter := model.User{ID: userID, Name: "Ada Lovelace", Email: "ada@example.com"}
	sprint := model.Sprint{ID: uuid.MustParse("d7fc0dbf-845c-41b4-84ab-89f487cc4a08"), ProjectID: projectID, Name: "Planned One", Status: model.SprintStatusPlanned}
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:          issueID,
			ProjectID:   projectID,
			Identifier:  "TRACK-7",
			Title:       "Design issue detail",
			Description: "Readonly description",
			Status:      model.StatusInProgress,
			AssigneeID:  &userID,
			ReporterID:  &userID,
			SprintID:    &sprint.ID,
			CreatedAt:   when,
			UpdatedAt:   when,
		},
		Project:  model.Project{ID: projectID, Key: "TRACK", Name: "Track Slash"},
		Sprint:   &sprint,
		Assignee: &assignee,
		Reporter: &reporter,
		Comments: []uiIssueCommentItem{{
			Comment:     model.Comment{ID: commentID, IssueID: issueID, AuthorID: userID, Body: "Looks ready.", CreatedAt: when, UpdatedAt: when},
			AuthorName:  "Ada Lovelace",
			AuthorEmail: "ada@example.com",
		}},
		Links: []uiIssueLinkItem{{
			Link:        model.IssueLink{ID: linkID, ProjectID: projectID, SourceID: issueID, TargetID: linkedID, LinkType: model.LinkTypeBlocks, CreatedAt: when, UpdatedAt: when},
			LinkedIssue: model.Issue{ID: linkedID, ProjectID: projectID, Identifier: "TRACK-8", Title: "Linked work"},
			HasIssue:    true,
		}},
		BackHref:  "/projects/" + projectID.String() + "/backlog",
		BackHXGet: "/projects/" + projectID.String() + "/backlog/panel",
		BackLabel: "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		"TRACK-7",
		"Design issue detail",
		"Readonly description",
		"In progress",
		"Track Slash",
		"Ada Lovelace",
		"Planned One",
		"Linked issues",
		"Blocks",
		"TRACK-8",
		"Linked work",
		"Comments",
		"Looks ready.",
		`href="/projects/` + projectID.String() + `/backlog"`,
		`hx-get="/projects/` + projectID.String() + `/backlog/panel"`,
		`href="/issues/` + linkedID.String() + `"`,
		`hx-get="/issues/` + linkedID.String() + `/panel"`,
		"Edit issue",
		"Change status",
		"Add link",
		`placeholder="Add a comment"`,
		"disabled",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("issue panel missing %q: %s", want, body)
		}
	}
}

func TestUIIssueBackLink(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	sprintID := uuid.MustParse("d7fc0dbf-845c-41b4-84ab-89f487cc4a08")
	baseIssue := model.Issue{ProjectID: projectID, SprintID: &sprintID}

	tests := []struct {
		name      string
		issue     model.Issue
		sprint    *model.Sprint
		wantHref  string
		wantHXGet string
		wantLabel string
	}{
		{
			name:      "active sprint",
			issue:     baseIssue,
			sprint:    &model.Sprint{ID: sprintID, ProjectID: projectID, Status: model.SprintStatusActive},
			wantHref:  "/projects/" + projectID.String() + "/sprint",
			wantHXGet: "/projects/" + projectID.String() + "/sprint/panel",
			wantLabel: "Sprint",
		},
		{
			name:      "planned sprint",
			issue:     baseIssue,
			sprint:    &model.Sprint{ID: sprintID, ProjectID: projectID, Status: model.SprintStatusPlanned},
			wantHref:  "/projects/" + projectID.String() + "/backlog",
			wantHXGet: "/projects/" + projectID.String() + "/backlog/panel",
			wantLabel: "Backlog",
		},
		{
			name:      "backlog issue",
			issue:     model.Issue{ProjectID: projectID},
			wantHref:  "/projects/" + projectID.String() + "/backlog",
			wantHXGet: "/projects/" + projectID.String() + "/backlog/panel",
			wantLabel: "Backlog",
		},
		{
			name:      "completed sprint",
			issue:     baseIssue,
			sprint:    &model.Sprint{ID: sprintID, ProjectID: projectID, Status: model.SprintStatusCompleted},
			wantHref:  "/projects/" + projectID.String() + "/sprint",
			wantHXGet: "/projects/" + projectID.String() + "/sprint/panel",
			wantLabel: "Sprint",
		},
		{
			name:      "missing sprint",
			issue:     baseIssue,
			wantHref:  "/projects/" + projectID.String() + "/sprint",
			wantHXGet: "/projects/" + projectID.String() + "/sprint/panel",
			wantLabel: "Sprint",
		},
	}

	for _, tt := range tests {
		href, hxGet, label := uiIssueBackLink(projectID, tt.issue, tt.sprint)
		if href != tt.wantHref || hxGet != tt.wantHXGet || label != tt.wantLabel {
			t.Fatalf("%s: got (%q, %q, %q), want (%q, %q, %q)", tt.name, href, hxGet, label, tt.wantHref, tt.wantHXGet, tt.wantLabel)
		}
	}
}

func TestUIIssueLinkLabel(t *testing.T) {
	t.Parallel()

	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	otherID := uuid.MustParse("ae77b9b8-9dcf-4a18-8b69-42b97bd4a4b5")

	tests := []struct {
		name string
		link model.IssueLink
		want string
	}{
		{name: "blocks outgoing", link: model.IssueLink{SourceID: issueID, TargetID: otherID, LinkType: model.LinkTypeBlocks}, want: "Blocks"},
		{name: "blocks incoming", link: model.IssueLink{SourceID: otherID, TargetID: issueID, LinkType: model.LinkTypeBlocks}, want: "Blocked by"},
		{name: "duplicates outgoing", link: model.IssueLink{SourceID: issueID, TargetID: otherID, LinkType: model.LinkTypeDuplicates}, want: "Duplicates"},
		{name: "duplicates incoming", link: model.IssueLink{SourceID: otherID, TargetID: issueID, LinkType: model.LinkTypeDuplicates}, want: "Duplicated by"},
		{name: "relates outgoing", link: model.IssueLink{SourceID: issueID, TargetID: otherID, LinkType: model.LinkTypeRelatesTo}, want: "Relates to"},
		{name: "relates incoming", link: model.IssueLink{SourceID: otherID, TargetID: issueID, LinkType: model.LinkTypeRelatesTo}, want: "Relates to"},
		{name: "clones outgoing", link: model.IssueLink{SourceID: issueID, TargetID: otherID, LinkType: model.LinkTypeClones}, want: "Clones"},
		{name: "clones incoming", link: model.IssueLink{SourceID: otherID, TargetID: issueID, LinkType: model.LinkTypeClones}, want: "Cloned by"},
		{name: "unknown", link: model.IssueLink{SourceID: issueID, TargetID: otherID, LinkType: model.LinkType("custom")}, want: "custom"},
	}

	for _, tt := range tests {
		if got := uiIssueLinkLabel(tt.link, issueID); got != tt.want {
			t.Fatalf("%s: uiIssueLinkLabel = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestUIProjectPanelRendersTabsBelowTitleCard(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel", &uiProjectPanelData{
		Project: model.Project{
			ID:          projectID,
			Key:         "TRACK",
			Name:        "Track Slash",
			Description: "Fast issue tracking.",
		},
		View:        "sprint",
		ProjectTabs: uiProjectTabs(projectID, "sprint"),
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	headerEnd := strings.Index(body, "</header>")
	if headerEnd < 0 {
		t.Fatalf("project panel missing title card header: %s", body)
	}
	titleCard := strings.Index(body, `<header class="rounded-lg`)
	if titleCard < 0 {
		t.Fatalf("project panel missing title card: %s", body)
	}
	backLink := strings.Index(body, `href="/projects"`)
	if backLink < 0 {
		t.Fatalf("project panel missing back link to projects: %s", body)
	}
	if backLink > titleCard {
		t.Fatalf("back link rendered inside or below title card: %s", body)
	}
	tabNav := strings.Index(body, `aria-label="Project views"`)
	if tabNav < 0 {
		t.Fatalf("project panel missing project view tabs: %s", body)
	}
	if tabNav < headerEnd {
		t.Fatalf("project view tabs rendered inside title card: %s", body)
	}
	header := body[:headerEnd]
	for _, notWant := range []string{"Sprints", "Backlog", `/sprint/panel`, `/backlog/panel`} {
		if strings.Contains(header, notWant) {
			t.Fatalf("title card still contains tab control %q: %s", notWant, body)
		}
	}
	if strings.Contains(body, "Back to projects") {
		t.Fatalf("project back link uses verbose label: %s", body)
	}
	for _, want := range []string{"Projects", `hx-get="/projects/panel"`, "Sprints", "Backlog", "border-b-4", `aria-current="page"`, `href="/projects/` + projectID.String() + `/sprint"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project panel missing tab markup %q: %s", want, body)
		}
	}
}

func TestUITabBarComponentRendersReusableTabs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "tab-bar", uiTabBarData{
		Label: "Example views",
		Items: []uiTabItem{
			{Label: "One", Icon: "circle", Href: "/one", HXGet: "/one/panel", HXTarget: "#main", HXPushURL: "/one", Active: true},
			{Label: "Two", Icon: "square", Href: "/two", HXGet: "/two/panel", HXTarget: "#main", HXPushURL: "/two"},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{`aria-label="Example views"`, "border-b-4", `data-lucide="circle"`, `href="/one"`, `hx-get="/one/panel"`, `aria-current="page"`, `href="/two"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("tab bar missing markup %q: %s", want, body)
		}
	}
}
