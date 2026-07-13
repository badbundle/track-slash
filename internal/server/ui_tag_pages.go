package server

import (
	"context"
	"errors"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

func (s *Server) uiProjectTagsPage(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	s.renderUIProjectTagManager(w, r, project.ID, nil)
}

func (s *Server) uiCreateProjectTag(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	nameInput := r.Form.Get("name")
	color := model.IssueTagColor(r.Form.Get("color"))
	name, err := model.NormalizeIssueTagName(nameInput)
	if err != nil {
		s.renderUIProjectTagManager(w, r, project.ID, func(panel *uiTagManagerData) {
			panel.NameInput = nameInput
			panel.ColorInput = uiIssueTagColorOrDefault(color)
			panel.TagError = err.Error()
		})
		return
	}
	color = uiIssueTagColorOrDefault(color)
	if !color.Valid() {
		s.renderUIProjectTagManager(w, r, project.ID, func(panel *uiTagManagerData) {
			panel.NameInput = nameInput
			panel.ColorInput = color
			panel.TagError = "Invalid color."
		})
		return
	}
	if _, err := s.store.CreateIssueTag(r.Context(), store.CreateIssueTagParams{ProjectID: project.ID, Name: name, Color: color}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIProjectTagManager(w, r, project.ID, func(panel *uiTagManagerData) {
				panel.NameInput = nameInput
				panel.ColorInput = color
				panel.TagError = "Tag already exists or is invalid."
			})
			return
		}
		writeUIStoreError(w, err)
		return
	}
	uiSetHXReplaceURL(w, r, uiProjectTagsPath(project))
	s.renderUIProjectTagManager(w, r, project.ID, nil)
}

func (s *Server) uiEditProjectTag(w http.ResponseWriter, r *http.Request) {
	project, tag, ok := s.uiProjectTagFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIProjectTagManager(w, r, project.ID, func(panel *uiTagManagerData) {
		panel.EditTagID = tag.ID
		panel.EditName = tag.DisplayName
		panel.EditColor = tag.Color
	})
}

func (s *Server) uiUpdateProjectTag(w http.ResponseWriter, r *http.Request) {
	project, tag, ok := s.uiProjectTagFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	nameInput := r.Form.Get("name")
	color := model.IssueTagColor(r.Form.Get("color"))
	name, err := model.NormalizeIssueTagName(nameInput)
	if err != nil {
		s.renderUIProjectTagEditError(w, r, project.ID, tag.ID, nameInput, color, err.Error())
		return
	}
	color = uiIssueTagColorOrDefault(color)
	if !color.Valid() {
		s.renderUIProjectTagEditError(w, r, project.ID, tag.ID, nameInput, color, "Invalid color.")
		return
	}
	if _, err := s.store.UpdateIssueTag(r.Context(), store.UpdateIssueTagParams{ID: tag.ID, Name: &name, Color: &color}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIProjectTagEditError(w, r, project.ID, tag.ID, nameInput, color, "Tag already exists or is invalid.")
			return
		}
		writeUIStoreError(w, err)
		return
	}
	uiSetHXReplaceURL(w, r, uiProjectTagsPath(project))
	s.renderUIProjectTagManager(w, r, project.ID, nil)
}

func (s *Server) uiDeleteProjectTag(w http.ResponseWriter, r *http.Request) {
	project, tag, ok := s.uiProjectTagFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	if err := s.store.DeleteIssueTag(r.Context(), tag.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	uiSetHXReplaceURL(w, r, uiProjectTagsPath(project))
	s.renderUIProjectTagManager(w, r, project.ID, nil)
}

func (s *Server) uiIssueTagsPage(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	s.renderUIIssuePanelWithTagModal(w, r, issue.ID, "", "")
}

func (s *Server) uiAttachIssueTag(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	input := strings.TrimSpace(r.Form.Get("tag"))
	name, err := model.NormalizeIssueTagName(input)
	if err != nil {
		s.renderUIIssuePanelWithTagModal(w, r, issue.ID, input, "Choose a tag.")
		return
	}
	tag, err := s.store.GetIssueTagByProjectName(r.Context(), issue.ProjectID, name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderUIIssuePanelWithTagModal(w, r, issue.ID, input, "Tag not found.")
			return
		}
		writeUIStoreError(w, err)
		return
	}
	if _, err := s.store.CreateIssueTagLink(r.Context(), store.CreateIssueTagLinkParams{IssueID: issue.ID, TagID: tag.ID}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIIssuePanelWithTagModal(w, r, issue.ID, input, "Tag already attached.")
			return
		}
		writeUIStoreError(w, err)
		return
	}
	uiSetHXReplaceURL(w, r, uiIssuePath(issue))
	s.renderUIIssuePanelWithTagModal(w, r, issue.ID, "", "")
}

func (s *Server) uiDetachIssueTag(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	number, ok := parseTypedRefParam(w, r, "tagRef", "tag")
	if !ok {
		return
	}
	tag, err := s.store.GetIssueTagByProjectNumber(r.Context(), issue.ProjectID, number)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if err := s.store.DeleteIssueTagLink(r.Context(), issue.ID, tag.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	uiSetHXReplaceURL(w, r, uiIssuePath(issue))
	s.renderUIIssuePanelWithTagModal(w, r, issue.ID, "", "")
}

func (s *Server) renderUIProjectTagManager(w http.ResponseWriter, r *http.Request, projectID uuid.UUID, mutate func(*uiTagManagerData)) {
	panel, err := s.uiBuildProjectTagManager(r.Context(), r, projectID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if mutate != nil {
		mutate(panel)
	}
	s.renderUITagManager(w, r, panel)
}

func (s *Server) renderUIIssueTagManager(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, mutate func(*uiTagManagerData)) {
	panel, err := s.uiBuildIssueTagManager(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if mutate != nil {
		mutate(panel)
	}
	s.renderUITagManager(w, r, panel)
}

func (s *Server) renderUIIssuePanelWithTagModal(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, input, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if err := s.uiPopulateIssueTagModal(r.Context(), panel, input, message); err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIIssuePanelResponse(w, r, panel)
}

func (s *Server) renderUIIssuePanelResponse(w http.ResponseWriter, r *http.Request, panel *uiIssuePanelData) {
	if isHTMXRequest(r) {
		renderUITemplate(w, http.StatusOK, "issue-panel", panel)
		return
	}
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		writeUIInternalError(w, "ui issue panel visible projects", err)
		return
	}
	s.renderUIShell(w, r, http.StatusOK, uiShellData{
		User:          currentUser(r),
		Projects:      projects,
		SidebarActive: uiSidebarState{View: "project", ProjectID: panel.Project.ID},
		IssuePanel:    panel,
	})
}

func (s *Server) renderUITagManager(w http.ResponseWriter, r *http.Request, panel *uiTagManagerData) {
	if isHTMXRequest(r) {
		renderUITemplate(w, http.StatusOK, "tag-manager-panel", panel)
		return
	}
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		writeUIInternalError(w, "ui tag manager visible projects", err)
		return
	}
	s.renderUIShell(w, r, http.StatusOK, uiShellData{
		User:          currentUser(r),
		Projects:      projects,
		SidebarActive: uiSidebarState{View: "project", ProjectID: panel.Project.ID},
		TagManager:    panel,
	})
}

func (s *Server) uiBuildProjectTagManager(ctx context.Context, r *http.Request, projectID uuid.UUID) (*uiTagManagerData, error) {
	if err := s.uiRequireProjectAccess(ctx, currentUser(r), projectID); err != nil {
		return nil, err
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	permissions, err := s.uiProjectPermissions(ctx, currentUser(r), projectID)
	if err != nil {
		return nil, err
	}
	tags, _, err := s.store.ListIssueTags(ctx, store.ListIssueTagsParams{ProjectID: projectID, Limit: MaxLimit})
	if err != nil {
		return nil, err
	}
	return &uiTagManagerData{
		Mode:       "project",
		Project:    project,
		CanWrite:   permissions.CanWrite,
		Tags:       tags,
		ColorInput: model.TagColorBlue,
		BackHref:   uiProjectViewPath(project, "about"),
		BackHXGet:  uiProjectPanelPath(project, "about"),
		BackLabel:  "About",
	}, nil
}

func (s *Server) uiBuildIssueTagManager(ctx context.Context, r *http.Request, issueID uuid.UUID) (*uiTagManagerData, error) {
	projectID, err := s.store.ProjectIDForIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}
	if err := s.uiRequireProjectAccess(ctx, currentUser(r), projectID); err != nil {
		return nil, err
	}
	issue, err := s.store.GetIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	permissions, err := s.uiProjectPermissions(ctx, currentUser(r), projectID)
	if err != nil {
		return nil, err
	}
	currentTags, available, err := s.uiIssueTagAttachmentOptions(ctx, issue)
	if err != nil {
		return nil, err
	}
	return &uiTagManagerData{
		Mode:      "issue",
		Project:   project,
		Issue:     issue,
		HasIssue:  true,
		CanWrite:  permissions.CanWrite,
		Tags:      currentTags,
		Available: available,
		BackHref:  uiIssuePath(issue),
		BackHXGet: uiIssuePanelPath(issue),
		BackLabel: "Issue",
	}, nil
}

func (s *Server) uiPopulateIssueTagModal(ctx context.Context, panel *uiIssuePanelData, input, message string) error {
	attached, available, err := s.uiIssueTagAttachmentOptions(ctx, panel.Issue)
	if err != nil {
		return err
	}
	panel.EditTags = true
	panel.TagModalAttached = attached
	panel.TagModalAvailable = available
	panel.TagInput = input
	panel.TagError = message
	panel.Issue.Tags = attached
	return nil
}

func (s *Server) uiIssueTagAttachmentOptions(ctx context.Context, issue model.Issue) ([]model.IssueTag, []model.IssueTag, error) {
	allTags, _, err := s.store.ListIssueTags(ctx, store.ListIssueTagsParams{ProjectID: issue.ProjectID, Limit: MaxLimit})
	if err != nil {
		return nil, nil, err
	}
	currentTags, _, err := s.store.ListTagsForIssue(ctx, store.ListTagsForIssueParams{IssueID: issue.ID, Limit: MaxLimit})
	if err != nil {
		return nil, nil, err
	}
	attached := make(map[uuid.UUID]struct{}, len(currentTags))
	for _, tag := range currentTags {
		attached[tag.ID] = struct{}{}
	}
	available := make([]model.IssueTag, 0, len(allTags))
	for _, tag := range allTags {
		if _, ok := attached[tag.ID]; !ok {
			available = append(available, tag)
		}
	}
	return currentTags, available, nil
}

func (s *Server) renderUIProjectTagEditError(w http.ResponseWriter, r *http.Request, projectID, tagID uuid.UUID, name string, color model.IssueTagColor, message string) {
	s.renderUIProjectTagManager(w, r, projectID, func(panel *uiTagManagerData) {
		panel.EditTagID = tagID
		panel.EditName = name
		panel.EditColor = uiIssueTagColorOrDefault(color)
		panel.EditError = message
	})
}

func (s *Server) uiProjectTagFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, model.IssueTag, bool) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return model.Project{}, model.IssueTag{}, false
	}
	number, ok := parseTypedRefParam(w, r, "tagRef", "tag")
	if !ok {
		return model.Project{}, model.IssueTag{}, false
	}
	tag, err := s.store.GetIssueTagByProjectNumber(r.Context(), project.ID, number)
	if err != nil {
		writeUIStoreError(w, err)
		return model.Project{}, model.IssueTag{}, false
	}
	return project, tag, true
}

func uiIssueTagColorOrDefault(color model.IssueTagColor) model.IssueTagColor {
	return model.IssueTagColorOrDefault(color)
}
