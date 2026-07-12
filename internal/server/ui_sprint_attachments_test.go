package server

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
)

func testSprintAttachment(sprintID uuid.UUID) model.SprintAttachment {
	objectID := uuid.New()
	return model.SprintAttachment{
		ID:              uuid.New(),
		SprintID:        sprintID,
		StorageObjectID: objectID,
		Object: model.StorageObject{
			ID:          objectID,
			Number:      3,
			Ref:         "object-3",
			Filename:    "roadmap.png",
			ContentType: "image/png",
			ByteSize:    42,
		},
	}
}

func TestUIActiveSprintRendersSharedMarkdownAndAttachmentsAtTop(t *testing.T) {
	t.Parallel()
	project := model.Project{OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	sprint := model.Sprint{ID: uuid.New(), Ref: "sprint-2", Name: "Active", Goal: "**Ship it**", Status: model.SprintStatusActive}
	attachment := testSprintAttachment(sprint.ID)

	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel-sprint", &uiProjectPanelData{
		Project:                        project,
		ActiveSprint:                   &sprint,
		ActiveSprintDescriptionHTML:    template.HTML("<p><strong>Ship it</strong></p>"),
		ActiveSprintAttachments:        []model.SprintAttachment{attachment},
		ActiveSprintAttachmentsHasMore: false,
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`<strong>Ship it</strong>`,
		`id="sprint-attachments-sprint-2"`,
		`data-attachment-ref="object-3"`,
		`src="/bradley/projects/TRACK/sprints/sprint-2/attachments/object-3/content?inline=1"`,
		`data-markdown="![roadmap.png](object-3)"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("active sprint missing %q: %s", want, body)
		}
	}
	if strings.Index(body, "<strong>Ship it</strong>") > strings.Index(body, `aria-label="Issue controls"`) {
		t.Fatalf("active sprint description rendered below controls: %s", body)
	}
}

func TestUIPlannedSprintDescriptionPreviewAndExpansion(t *testing.T) {
	t.Parallel()
	project := model.Project{OwnerUsername: "bradley", Key: "TRACK"}
	sprint := model.Sprint{ID: uuid.New(), Ref: "sprint-7", Goal: "Preview **markdown**", Status: model.SprintStatusPlanned}

	var collapsed bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&collapsed, "planned-sprint-description", uiPlannedSprint{Project: project, Sprint: sprint, AttachmentCount: 1})
	if err != nil {
		t.Fatalf("collapsed template: %v", err)
	}
	body := collapsed.String()
	for _, want := range []string{"line-clamp-2", "Preview **markdown**", "See more", `hx-get="/bradley/projects/TRACK/sprints/sprint-7/description?expanded=1"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("collapsed planned description missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "roadmap.png") {
		t.Fatalf("collapsed planned description rendered attachments: %s", body)
	}

	attachment := testSprintAttachment(sprint.ID)
	var expanded bytes.Buffer
	err = uiTemplates.ExecuteTemplate(&expanded, "planned-sprint-description", uiPlannedSprint{
		Project:             project,
		Sprint:              sprint,
		DescriptionExpanded: true,
		DescriptionHTML:     template.HTML("<p>Expanded markdown</p>"),
		Attachments:         []model.SprintAttachment{attachment},
	})
	if err != nil {
		t.Fatalf("expanded template: %v", err)
	}
	body = expanded.String()
	for _, want := range []string{"Expanded markdown", "roadmap.png", "See less", `hx-get="/bradley/projects/TRACK/sprints/sprint-7/description?expanded=0"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expanded planned description missing %q: %s", want, body)
		}
	}
}

func TestUISprintEditUsesSharedAttachmentDropzone(t *testing.T) {
	t.Parallel()
	project := model.Project{OwnerUsername: "bradley", Key: "TRACK"}
	sprint := model.Sprint{ID: uuid.New(), Ref: "sprint-9", Name: "Edit", Goal: "Source", Status: model.SprintStatusActive}
	var buf bytes.Buffer
	err := uiTemplates.ExecuteTemplate(&buf, "project-panel-sprint", &uiProjectPanelData{
		Project:            project,
		ActiveSprint:       &sprint,
		ActiveSprintAction: "edit",
		ActiveSprintForm:   uiSprintFormData{GoalInput: sprint.Goal},
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`data-attachment-dropzone`,
		`data-attachment-upload-url="/bradley/projects/TRACK/sprints/sprint-9/attachments"`,
		`data-attachment-list="#sprint-attachments-sprint-9"`,
		`id="sprint-attachments-sprint-9"`,
		`data-attachment-editing="true"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("sprint edit missing %q: %s", want, body)
		}
	}
}
