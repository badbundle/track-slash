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
		`<a href="https://example.com/image.png">External</a>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("markdown output missing %q: %s", want, out)
		}
	}
	for _, notWant := range []string{
		"<script",
		"javascript:",
		`<img src="/bradley/issues/TRACK-7/attachments/object-3/content`,
		`<img src="https://example.com/image.png`,
		`object-99/content`,
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("markdown output included %q: %s", notWant, out)
		}
	}
	if strings.Count(out, "<img ") != 1 {
		t.Fatalf("inline image count = %d output=%s", strings.Count(out, "<img "), out)
	}
}

func TestRenderIssueDescriptionMarkdownEmptySource(t *testing.T) {
	t.Parallel()
	if got := renderIssueDescriptionMarkdown(model.Issue{Description: " \n\t "}, nil); got != "" {
		t.Fatalf("empty markdown = %q, want empty", got)
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
