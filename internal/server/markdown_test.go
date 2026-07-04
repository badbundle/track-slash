package server

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
)

func TestRenderIssueDescriptionMarkdownResolvesIssueAttachmentsSafely(t *testing.T) {
	t.Parallel()
	projectID := uuid.MustParse("8cc21ed4-2d69-4d43-9f0c-402736e4aa16")
	issueID := uuid.MustParse("9480828a-47f3-4661-bb64-b21b4f02f27b")
	issue := model.Issue{
		ID:            issueID,
		ProjectID:     projectID,
		OwnerUsername: "bradley",
		ProjectKey:    "TRACK",
		Number:        7,
		Identifier:    "TRACK-7",
		Description: strings.Join([]string{
			"**Bold**",
			"![Screenshot](object-1)",
			"[Download log](object-2)",
			"![Vector](object-3)",
			"![Missing](object-99)",
			"[Unsafe](javascript:alert(1))",
			"<script>alert('x')</script>",
			"![External](https://example.com/image.png)",
			"![External SVG](https://news.ycombinator.com/y18.svg)",
			"![Data URI](data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=)",
		}, "\n\n"),
	}
	attachments := []model.IssueAttachment{
		testMarkdownAttachment(projectID, issueID, 1, "screenshot.png", "image/png"),
		testMarkdownAttachment(projectID, issueID, 2, "log.txt", "text/plain"),
		testMarkdownAttachment(projectID, issueID, 3, "vector.svg", "image/svg+xml"),
	}

	out := string(renderIssueDescriptionMarkdown(issue, attachments))
	for _, want := range []string{
		"<strong>Bold</strong>",
		`<img src="/bradley/issues/TRACK-7/attachments/object-1/content?inline=1" alt="Screenshot">`,
		`<a href="/bradley/issues/TRACK-7/attachments/object-2/content">Download log</a>`,
		`<a href="/bradley/issues/TRACK-7/attachments/object-3/content">Vector</a>`,
		"Missing",
		"Unsafe",
		`<img src="https://example.com/image.png" alt="External">`,
		`<img src="https://news.ycombinator.com/y18.svg" alt="External SVG">`,
		"Data URI",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("markdown output missing %q: %s", want, out)
		}
	}
	for _, notWant := range []string{
		"<script",
		"javascript:",
		`<img src="/bradley/issues/TRACK-7/attachments/object-3/content`,
		`<a href="https://example.com/image.png">External</a>`,
		"data:image",
		`object-99/content`,
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("markdown output included %q: %s", notWant, out)
		}
	}
	if strings.Count(out, "<img ") != 3 {
		t.Fatalf("inline image count = %d output=%s", strings.Count(out, "<img "), out)
	}
}

func TestRenderIssueDescriptionMarkdownEmptySource(t *testing.T) {
	t.Parallel()
	if got := renderIssueDescriptionMarkdown(model.Issue{Description: " \n\t "}, nil); got != "" {
		t.Fatalf("empty markdown = %q, want empty", got)
	}
}

func TestRenderIssueDescriptionMarkdownHeadingsAndTables(t *testing.T) {
	t.Parallel()
	issue := model.Issue{
		Description: strings.Join([]string{
			"# Heading 1",
			"## Heading 2",
			"| Name | Value |",
			"| --- | --- |",
			"| Alpha | One |",
		}, "\n"),
	}
	out := string(renderIssueDescriptionMarkdown(issue, nil))
	for _, want := range []string{
		"<h1>Heading 1</h1>",
		"<h2>Heading 2</h2>",
		"<table>",
		"<th>Name</th>",
		"<th>Value</th>",
		"<td>Alpha</td>",
		"<td>One</td>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("markdown output missing %q: %s", want, out)
		}
	}
}

func TestRenderProjectDescriptionMarkdownSafely(t *testing.T) {
	t.Parallel()
	project := model.Project{
		Description: strings.Join([]string{
			"# Overview",
			"**Bold**",
			"| Name | Value |\n| --- | --- |\n| Alpha | One |",
			"[Docs](/docs)",
			"![External](https://example.com/image.png)",
			"[Unsafe](javascript:alert(1))",
			"<script>alert('x')</script>",
			"[Object](object-1)",
			"![Object image](object-2)",
		}, "\n\n"),
	}

	out := string(renderProjectDescriptionMarkdown(project))
	for _, want := range []string{
		"<h1>Overview</h1>",
		"<strong>Bold</strong>",
		"<table>",
		"<th>Name</th>",
		"<td>One</td>",
		`<a href="/docs">Docs</a>`,
		`<img src="https://example.com/image.png" alt="External">`,
		"Unsafe",
		"Object",
		"Object image",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("project markdown output missing %q: %s", want, out)
		}
	}
	for _, notWant := range []string{
		"<script",
		"javascript:",
		`<a href="object-1"`,
		`<img src="object-2"`,
		`object-1/content`,
		`object-2/content`,
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("project markdown output included %q: %s", notWant, out)
		}
	}
}

func testMarkdownAttachment(projectID, issueID uuid.UUID, number int, filename, contentType string) model.IssueAttachment {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	objectID := uuid.New()
	return model.IssueAttachment{
		ID:              uuid.New(),
		ProjectID:       projectID,
		IssueID:         issueID,
		StorageObjectID: objectID,
		Object: model.StorageObject{
			ID:          objectID,
			ProjectID:   projectID,
			Number:      number,
			Ref:         model.StorageObjectRef(number),
			Backend:     "local",
			Bucket:      "local",
			ObjectKey:   "projects/test/objects/" + objectID.String(),
			Filename:    filename,
			ContentType: contentType,
			ByteSize:    12,
			SHA256:      strings.Repeat("a", 64),
			CreatedByID: uuid.New(),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		CreatedByID: uuid.New(),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
