package server

import (
	"regexp"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
)

// The remove button carries both an htmx binding and a data- hook handled by
// app.js. htmx is bound directly to the button, so the JS handler's
// preventDefault cannot cancel it and both fire. Only one may be present at a
// time.
func TestAttachmentRemoveBindsOneDeleteMechanism(t *testing.T) {
	t.Parallel()

	body, err := uiTemplateFS.ReadFile("templates/description_components.html")
	if err != nil {
		t.Fatalf("read description_components.html: %v", err)
	}
	source := string(body)

	button := regexp.MustCompile(`(?s)<button[^>]*data-attachment-remove.*?>`).FindString(source)
	if button == "" {
		t.Fatal("no attachment remove button found")
	}
	if !strings.Contains(button, `{{if not $.Editing}}hx-post=`) {
		t.Fatalf("htmx delete is not gated on the editing state: %s", button)
	}
	if strings.Count(button, "hx-post=") != 1 {
		t.Fatalf("unexpected number of hx-post bindings: %s", button)
	}
}

// Rows built on the client only ever land in a list that is already in editing
// mode, so the JS handler always owns them and an htmx binding would only ever
// double-fire.
func TestClientBuiltAttachmentRowsHaveNoHTMXDelete(t *testing.T) {
	t.Parallel()

	body, err := uiTemplateFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	source := string(body)

	row := regexp.MustCompile(`(?s)<button type="button" data-attachment-remove.*?>`).FindString(source)
	if row == "" {
		t.Fatal("no client-built attachment remove button found")
	}
	if strings.Contains(row, "hx-post") {
		t.Fatalf("client-built remove button still binds htmx: %s", row)
	}
	if !strings.Contains(row, "data-attachment-delete-url") {
		t.Fatalf("client-built remove button lost its JS hook: %s", row)
	}
}

// Rendering both states is the check that matters: the editing view must bind
// only the JS handler, and the read-only view only htmx.
func TestAttachmentRemoveRendersOneMechanismPerState(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		editing bool
		wantHX  bool
	}{
		{name: "editing lets the JS handler own the click", editing: true, wantHX: false},
		{name: "read-only re-renders through htmx", editing: false, wantHX: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			data := uiAttachmentListData{
				ID:        "issue-attachments",
				Editing:   tt.editing,
				CanDelete: true,
				Items: []uiDescriptionAttachment{{
					Object:         model.StorageObject{Ref: "object-1", Filename: "notes.txt", ByteSize: 12},
					ContentHref:    "/o/issues/ISS-1/attachments/object-1/content",
					DeleteHref:     "/o/issues/ISS-1/attachments/object-1/delete",
					DeleteJSONHref: "/o/issues/ISS-1/attachments/object-1",
				}},
			}
			if err := uiTemplates.ExecuteTemplate(&out, "description-attachment-list", data); err != nil {
				t.Fatalf("execute: %v", err)
			}
			rendered := out.String()
			if !strings.Contains(rendered, "data-attachment-delete-url=") {
				t.Fatalf("rendered list lost the JS hook: %s", rendered)
			}
			if got := strings.Contains(rendered, "hx-post="); got != tt.wantHX {
				t.Fatalf("hx-post present = %v, want %v: %s", got, tt.wantHX, rendered)
			}
			if got := strings.Contains(rendered, `data-attachment-editing="true"`); got != tt.editing {
				t.Fatalf("editing marker = %v, want %v", got, tt.editing)
			}
		})
	}
}
