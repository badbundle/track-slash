package server

import (
	"bytes"
	"strings"
	"testing"

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
