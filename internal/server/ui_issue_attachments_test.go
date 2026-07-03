package server

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
)

func TestUIIssuePanelRendersMarkdownDescriptionAndAttachments(t *testing.T) {
	t.Parallel()
	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	attachment := testMarkdownAttachment(projectID, issueID, 1, "screenshot.png", "image/png")
	attachment.Object.ByteSize = 12

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Number:        7,
			Identifier:    "TRACK-7",
			Title:         "Design issue detail",
			Description:   "**source markdown**",
			Status:        model.StatusTodo,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:         model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		DescriptionHTML: template.HTML(`<p><strong>Rendered markdown</strong></p>`),
		Attachments:     []model.IssueAttachment{attachment},
		BackHref:        "/bradley/projects/TRACK/backlog",
		BackHXGet:       "/bradley/projects/TRACK/backlog/panel",
		BackLabel:       "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`class="markdown-body`,
		`<strong>Rendered markdown</strong>`,
		`id="issue-attachments-list"`,
		`data-attachment-list`,
		`data-attachment-count`,
		`>1</span>`,
		`data-attachment-ref="object-1"`,
		`src="/bradley/issues/TRACK-7/attachments/object-1/content?inline=1"`,
		`loading="lazy"`,
		`screenshot.png`,
		`12 B`,
		`object-1</code>`,
		`data-attachment-copy-markdown`,
		`data-markdown="![screenshot.png](object-1)"`,
		`aria-label="Copy attachment Markdown"`,
		`data-lucide="copy"`,
		`data-copy-label`,
		`href="/bradley/issues/TRACK-7/attachments/object-1/content"`,
		`aria-label="Download attachment"`,
		`data-lucide="download"`,
		`data-attachment-remove`,
		`data-attachment-delete-url="/bradley/issues/TRACK-7/attachments/object-1"`,
		`hx-post="/bradley/issues/TRACK-7/attachments/object-1/delete"`,
		`aria-label="Remove attachment"`,
		`data-lucide="trash-2"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("issue attachment panel missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "<p>**source markdown**</p>") || strings.Contains(body, "&lt;strong&gt;Rendered markdown") {
		t.Fatalf("description did not render trusted Markdown HTML: %s", body)
	}
}

func TestUIIssuePanelDescriptionEditHasAttachmentDropzone(t *testing.T) {
	t.Parallel()
	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	when := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "issue-panel", &uiIssuePanelData{
		Issue: model.Issue{
			ID:            issueID,
			ProjectID:     projectID,
			OwnerUsername: "bradley",
			ProjectKey:    "TRACK",
			Number:        7,
			Identifier:    "TRACK-7",
			Title:         "Design issue detail",
			Description:   "Editable description",
			Status:        model.StatusTodo,
			CreatedAt:     when,
			UpdatedAt:     when,
		},
		Project:         model.Project{ID: projectID, OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		EditDescription: true,
		BackHref:        "/bradley/projects/TRACK/backlog",
		BackHXGet:       "/bradley/projects/TRACK/backlog/panel",
		BackLabel:       "Backlog",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`data-attachment-dropzone`,
		`data-attachment-upload-url="/bradley/issues/TRACK-7/attachments"`,
		`data-attachment-list="#issue-attachments-list"`,
		`data-attachment-status`,
		`id="issue-attachments-list"`,
		`data-attachment-list`,
		`data-attachment-editing="true"`,
		`hidden`,
		`data-attachment-rows`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("description edit attachments missing %q: %s", want, body)
		}
	}
}
