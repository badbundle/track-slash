package server

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"net/http"

	"github.com/bradleymackey/track-slash/internal/store"
)

//go:embed templates/*.html
var uiTemplateFS embed.FS

const uiAuthCookieName = "track_slash_ui_token"

var errUIForbidden = errors.New("forbidden")
var errUIBadRequest = errors.New("bad request")

var uiTemplates = template.Must(template.New("ui").Funcs(template.FuncMap{
	"initials":                     uiInitials,
	"userAvatar":                   uiUserAvatar,
	"byteSize":                     uiByteSize,
	"issueAssignee":                uiIssueAssigneePath,
	"issueAssigneeEdit":            uiIssueAssigneeEditPath,
	"issueAttachments":             uiIssueAttachmentsPath,
	"issueAttachmentContent":       uiIssueAttachmentContentPath,
	"issueAttachmentDelete":        uiIssueAttachmentDeletePath,
	"issueAttachmentImage":         uiIssueAttachmentImage,
	"issueAttachmentInlineContent": uiIssueAttachmentInlineContentPath,
	"issueAttachmentMarkdown":      uiIssueAttachmentMarkdown,
	"issueComment":                 uiIssueCommentPath,
	"issueCommentEdit":             uiIssueCommentEditPath,
	"issueComments":                uiIssueCommentsPath,
	"issueContext":                 uiIssueContextPath,
	"issueContextDelete":           uiIssueContextDeletePath,
	"issueContextEdit":             uiIssueContextEditPath,
	"issueContextItem":             uiIssueContextItemPath,
	"issueContextLinkNew":          uiIssueContextLinkNewPath,
	"issueContextNew":              uiIssueContextNewPath,
	"issueCloseReason":             uiIssueCloseReasonPath,
	"issueCloseReasonEdit":         uiIssueCloseReasonEditPath,
	"issueDelete":                  uiIssueDeletePath,
	"issueDescription":             uiIssueDescriptionPath,
	"issueDescriptionEdit":         uiIssueDescriptionEditPath,
	"issueHref":                    uiIssuePath,
	"issueLink":                    uiIssueLinkPath,
	"issueLinkDelete":              uiIssueLinkDeletePath,
	"issueLinkEdit":                uiIssueLinkEditPath,
	"issueLinkNew":                 uiIssueLinkNewPath,
	"issueLinks":                   uiIssueLinksPath,
	"issuePanel":                   uiIssuePanelPath,
	"issuePriority":                uiIssuePriorityPath,
	"issuePriorityEdit":            uiIssuePriorityEditPath,
	"issueDueDate":                 uiIssueDueDatePath,
	"issueDueDateEdit":             uiIssueDueDateEditPath,
	"issueReporter":                uiIssueReporterPath,
	"issueReporterEdit":            uiIssueReporterEditPath,
	"issueRestore":                 uiIssueRestorePath,
	"issueSprint":                  uiIssueSprintPath,
	"issueSprintEdit":              uiIssueSprintEditPath,
	"issueStatus":                  uiIssueStatusPath,
	"issueStatusEdit":              uiIssueStatusEditPath,
	"issueSubIssues":               uiIssueSubIssuesPath,
	"issueSubIssuesNew":            uiIssueSubIssuesNewPath,
	"issueTags":                    uiIssueTagsPath,
	"issueTagDelete":               uiIssueTagDeletePath,
	"issueTitle":                   uiIssueTitlePath,
	"issueTitleEdit":               uiIssueTitleEditPath,
	"issueAssigneeAutocomplete":    uiIssueAssigneeAutocomplete,
	"issueReporterAutocomplete":    uiIssueReporterAutocomplete,
	"issueSprintAutocomplete":      uiIssueSprintAutocomplete,
	"newIssueProjectAutocomplete":  uiNewIssueProjectAutocomplete,
	"autocompleteOptionSearchText": uiAutocompleteOptionSearchText,
	"linkLabel":                    uiIssueLinkLabel,
	"linkOptions":                  uiIssueLinkOptions,
	"linkedIssueProgress":          uiLinkedIssueProgress,
	"closeReasonLabel":             uiCloseReasonLabel,
	"closeReasonModal":             uiCloseReasonModal,
	"issueContextModal":            uiIssueContextModal,
	"issueTagsModal":               uiIssueTagsModal,
	"issueCloseReasonDropdown":     uiIssueCloseReasonDropdown,
	"issueStatusDropdown":          uiIssueStatusDropdown,
	"closeReasonOptions":           uiCloseReasonOptions,
	"issueColumnCount":             uiIssueColumnCount,
	"priorityClass":                uiPriorityClass,
	"priorityLabel":                uiPriorityLabel,
	"priorityOptions":              uiPriorityOptions,
	"newIssueSelectedPriority":     uiNewIssueSelectedPriority,
	"issues":                       uiIssuesPath,
	"issueNew":                     uiIssueNewPath,
	"issueNewPanel":                uiIssueNewPanelPath,
	"issueNewProjectOptions":       uiIssueNewProjectOptionsPath,
	"newIssueProjectSelected":      uiNewIssueProjectSelected,
	"newIssueProjectInput":         uiNewIssueProjectInput,
	"newIssueProjectLabel":         uiNewIssueProjectLabel,
	"canEditIssueSprint":           uiCanEditIssueSprint,
	"projectIssues":                uiProjectIssuesPath,
	"projectIssueNew":              uiProjectIssueNewPath,
	"projectIssueNewPanel":         uiProjectIssueNewPanelPath,
	"projectName":                  uiProjectNamePath,
	"projectNameEdit":              uiProjectNameEditPath,
	"projectDescription":           uiProjectDescriptionPath,
	"projectDescriptionEdit":       uiProjectDescriptionEditPath,
	"projectFavorite":              uiProjectFavoritePath,
	"projectPanel":                 uiProjectPanelPath,
	"projectSprint":                uiProjectSprintPath,
	"projectSprintActivate":        uiProjectSprintActivatePath,
	"projectSprintComplete":        uiProjectSprintCompletePath,
	"projectSprintDelete":          uiProjectSprintDeletePath,
	"projectSprintEdit":            uiProjectSprintEditPath,
	"projectSprintIssueNew":        uiProjectSprintIssueNewPath,
	"projectSprintIssues":          uiProjectSprintIssuesPath,
	"projectSprintMoveDown":        uiProjectSprintMoveDownPath,
	"projectSprintMoveUp":          uiProjectSprintMoveUpPath,
	"projectSprintNew":             uiProjectSprintNewPath,
	"projectSprints":               uiProjectSprintsPath,
	"projectContext":               uiProjectContextPath,
	"projectContextDelete":         uiProjectContextDeletePath,
	"projectContextEdit":           uiProjectContextEditPath,
	"projectContextIssueDelete":    uiProjectContextIssueDeletePath,
	"projectContextIssueNew":       uiProjectContextIssueNewPath,
	"projectContextIssues":         uiProjectContextIssuesPath,
	"projectContextNew":            uiProjectContextNewPath,
	"projectContexts":              uiProjectContextsPath,
	"projectTags":                  uiProjectTagsPath,
	"projectTag":                   uiProjectTagPath,
	"projectTagDelete":             uiProjectTagDeletePath,
	"projectView":                  uiProjectViewPath,
	"projectIcon":                  uiProjectIcon,
	"changelogActor":               uiChangelogActor,
	"changelogIcon":                uiChangelogIcon,
	"changelogTargetHref":          uiChangelogTargetHref,
	"dueBadgeClass":                uiDueBadgeClass,
	"dueBadgeIcon":                 uiDueBadgeIcon,
	"dueBadgeLabel":                uiDueBadgeLabel,
	"dueDateFull":                  uiDueDateFull,
	"dueDateShort":                 uiDueDateShort,
	"dueDateValue":                 uiDueDateValue,
	"sprintDate":                   uiSprintDate,
	"sprintDateRange":              uiSprintDateRange,
	"sprintLabel":                  uiSprintLabel,
	"statusClass":                  uiStatusClass,
	"statusLabel":                  uiStatusLabel,
	"statusOptions":                uiStatusOptions,
	"statusRow":                    uiStatusRowClass,
	"statusSurface":                uiStatusSurfaceClass,
	"statusValue":                  uiStatusValue,
	"subIssueProgress":             uiSubIssueProgress,
	"tagClass":                     uiTagClass,
	"tagColors":                    uiTagColors,
	"tagDotClass":                  uiTagDotClass,
	"issueVisibleTags":             uiIssueVisibleTags,
	"issueHiddenTagCount":          uiIssueHiddenTagCount,
	"issueAttachmentIcon":          uiIssueAttachmentIcon,
	"tokenTime":                    uiTokenTime,
}).ParseFS(uiTemplateFS, "templates/*.html"))

func renderUITemplate(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := uiTemplates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeUIStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUIBadRequest):
		http.Error(w, "bad request", http.StatusBadRequest)
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "conflict", http.StatusConflict)
	case errors.Is(err, errUIForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
