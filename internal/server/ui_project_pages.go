package server

import (
	"context"
	"fmt"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

func (s *Server) uiProjectPage(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	http.Redirect(w, r, uiProjectViewPath(project, "sprint"), http.StatusSeeOther)
}

func (s *Server) uiProjectWorkPage(w http.ResponseWriter, r *http.Request, view string) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		writeUIInternalError(w, "ui project visible projects", err)
		return
	}
	panel, err := s.uiBuildProjectPanel(r.Context(), r, project.ID, view)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIShell(w, r, http.StatusOK, uiShellData{
		User:             currentUser(r),
		Projects:         projects,
		CurrentProjectID: project.ID,
		CurrentView:      "projects",
		ProjectPanel:     panel,
	})
}

func (s *Server) uiProjectWorkPanel(w http.ResponseWriter, r *http.Request, view string) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildProjectPanel(r.Context(), r, project.ID, view)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "project-panel", panel)
}

func (s *Server) uiToggleProjectFavorite(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeUIStoreError(w, errUIBadRequest)
		return
	}
	favorite, err := s.store.IsProjectFavorite(r.Context(), currentUser(r).ID, project.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if favorite {
		err = s.store.UnfavoriteProject(r.Context(), currentUser(r).ID, project.ID)
	} else {
		err = s.store.FavoriteProject(r.Context(), currentUser(r).ID, project.ID)
	}
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	view := uiProjectFavoriteView(r.Form.Get("view"))
	if !isHTMXRequest(r) {
		http.Redirect(w, r, uiProjectViewPath(project, view), http.StatusSeeOther)
		return
	}
	favorites, err := s.uiFavoriteProjects(r.Context(), currentUser(r))
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "project-favorite-toggle-response", uiProjectFavoriteData{
		Project:  project,
		View:     view,
		Favorite: !favorite,
		Sidebar: uiSidebarFavoritesData{
			Projects:         favorites,
			CurrentProjectID: project.ID,
			OOB:              true,
		},
	})
}

func (s *Server) uiProjectAllIssuePage(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	pageData, err := s.uiBuildProjectAllIssuePage(r.Context(), r, project)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "project-all-issue-page", pageData)
}

func uiProjectFavoriteView(raw string) string {
	switch raw {
	case "about", "sprint", "planned", "all", "changelog":
		return raw
	default:
		return "sprint"
	}
}

func (s *Server) uiProjectLegacyBacklog(w http.ResponseWriter, r *http.Request, panel bool) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	target := uiProjectViewPath(project, "all")
	if panel {
		target = uiProjectPanelPath(project, "all")
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) uiProjectDeletedPage(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		writeUIInternalError(w, "ui deleted issues visible projects", err)
		return
	}
	panel, err := s.uiBuildDeletedIssuesPanel(r.Context(), r, project.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIShell(w, r, http.StatusOK, uiShellData{
		User:             currentUser(r),
		Projects:         projects,
		CurrentProjectID: project.ID,
		CurrentView:      "projects",
		DeletedPanel:     panel,
	})
}

func (s *Server) uiProjectDeletedPanel(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildDeletedIssuesPanel(r.Context(), r, project.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "deleted-issues-panel", panel)
}

func (s *Server) uiBuildWorkPanel(ctx context.Context, r *http.Request, user model.User, view string) (*uiWorkPanelData, error) {
	projects, err := s.uiVisibleProjects(ctx, user)
	if err != nil {
		return nil, err
	}
	query, err := uiParseIssueListQuery(r)
	if err != nil {
		return nil, err
	}
	query.TagNames = nil
	panel := &uiWorkPanelData{
		View:          view,
		Title:         "Me",
		ProjectCount:  len(projects),
		WorkTabs:      uiWorkTabs(view, query),
		IssueControls: uiWorkIssueControls(view, query),
	}
	switch view {
	case "active":
		panel.Subtitle = "Active sprint issues assigned to you across accessible projects."
		panel.IssueListLabel = "Active sprint issues"
		panel.Issues, panel.HasMore, err = s.uiAssignedActiveSprintIssues(ctx, projects, user.ID, query)
	case "all":
		panel.Subtitle = "Issues assigned to you across accessible projects."
		panel.IssueListLabel = "All assigned issues"
		panel.Issues, panel.HasMore, err = s.uiAssignedIssues(ctx, projects, user.ID, query)
	default:
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return panel, nil
}

func (s *Server) uiBuildProjectsPanel(ctx context.Context, user model.User) (*uiProjectsPanelData, error) {
	var all []model.Project
	var hasMore bool
	var cursor *store.ProjectsCursor
	for {
		projects, more, err := s.store.ListProjects(ctx, store.ListProjectsParams{
			Cursor:        cursor,
			Limit:         MaxLimit,
			VisibleToUser: visibleProjectUser(user),
		})
		if err != nil {
			return nil, err
		}
		all = append(all, projects...)
		hasMore = hasMore || more
		if !more {
			break
		}
		last := projects[len(projects)-1]
		cursor = &store.ProjectsCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return &uiProjectsPanelData{
		Projects: all,
		HasMore:  hasMore,
	}, nil
}

func (s *Server) uiAssignedIssues(ctx context.Context, projects []model.Project, userID uuid.UUID, query uiIssueListQuery) ([]uiIssueItem, bool, error) {
	var out []uiIssueItem
	var hasMore bool
	for _, project := range projects {
		issues, more, err := s.store.ListIssues(ctx, store.ListIssuesParams{
			ProjectID:        project.ID,
			Statuses:         query.Statuses,
			Priorities:       query.Priorities,
			AssigneeIDs:      []uuid.UUID{userID},
			Limit:            MaxLimit,
			Sort:             query.Sort,
			Direction:        query.Direction,
			IncludeSubIssues: true,
		})
		if err != nil {
			return nil, false, err
		}
		hasMore = hasMore || more
		for _, issue := range issues {
			out = append(out, uiIssueItem{Issue: issue, Project: project})
		}
	}
	uiSortIssueItems(out, query.Sort, query.Direction)
	return out, hasMore, nil
}

func (s *Server) uiAssignedActiveSprintIssues(ctx context.Context, projects []model.Project, userID uuid.UUID, query uiIssueListQuery) ([]uiIssueItem, bool, error) {
	var out []uiIssueItem
	var hasMore bool
	for _, project := range projects {
		activeSprints, _, err := s.store.ListSprints(ctx, store.ListSprintsParams{
			ProjectID: project.ID,
			Status:    model.SprintStatusActive,
			Limit:     1,
		})
		if err != nil {
			return nil, false, err
		}
		if len(activeSprints) == 0 {
			continue
		}
		sprint := activeSprints[0]
		issues, more, err := s.store.ListIssues(ctx, store.ListIssuesParams{
			ProjectID:   project.ID,
			Statuses:    query.Statuses,
			Priorities:  query.Priorities,
			AssigneeIDs: []uuid.UUID{userID},
			SprintID:    &sprint.ID,
			Limit:       MaxLimit,
			Sort:        query.Sort,
			Direction:   query.Direction,
		})
		if err != nil {
			return nil, false, err
		}
		hasMore = hasMore || more
		for _, issue := range issues {
			out = append(out, uiIssueItem{Issue: issue, Project: project, Sprint: &sprint})
		}
	}
	uiSortIssueItems(out, query.Sort, query.Direction)
	return out, hasMore, nil
}

func (s *Server) uiBuildNewIssuePanel(ctx context.Context, r *http.Request, input uiNewIssuePanelData) (*uiNewIssuePanelData, error) {
	user := currentUser(r)
	projects, err := s.uiVisibleProjects(ctx, user)
	if err != nil {
		return nil, err
	}
	input.ProjectOptions = uiFilterNewIssueProjects(projects, input.ProjectInput)
	if strings.TrimSpace(input.ProjectID) != "" {
		project, ok, message, err := s.uiProjectFromNewIssueSelection(ctx, user, input.ProjectID)
		if err != nil {
			return nil, err
		}
		if ok && input.ProjectInput != "" && !strings.EqualFold(strings.TrimSpace(input.ProjectInput), uiNewIssueProjectLabel(project)) {
			ok = false
			input.ProjectID = ""
		}
		if ok {
			members, err := s.store.SearchProjectMembers(ctx, store.SearchProjectMembersParams{
				ProjectID: project.ID,
				Limit:     MaxLimit,
			})
			if err != nil {
				return nil, err
			}
			input.Project = project
			input.HasProject = true
			input.ProjectID = project.ID.String()
			if input.ProjectInput == "" {
				input.ProjectInput = uiNewIssueProjectLabel(project)
			}
			input.MemberOptions = members
		} else if input.Error == "" {
			input.Error = message
		}
	}
	input.ProjectSearchOpen = input.ProjectInput != "" && !input.HasProject
	if input.BackHref == "" {
		if input.HasProject {
			input.BackHref = uiProjectViewPath(input.Project, "all")
		} else {
			input.BackHref = "/me"
		}
	}
	if input.BackHXGet == "" {
		if input.HasProject {
			input.BackHXGet = uiProjectPanelPath(input.Project, "all")
		} else {
			input.BackHXGet = "/me/panel"
		}
	}
	return &input, nil
}

func (s *Server) uiBuildProjectPanel(ctx context.Context, r *http.Request, projectID uuid.UUID, view string) (*uiProjectPanelData, error) {
	if err := s.uiRequireProjectAccess(ctx, currentUser(r), projectID); err != nil {
		return nil, err
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	favorite, err := s.store.IsProjectFavorite(ctx, currentUser(r).ID, projectID)
	if err != nil {
		return nil, err
	}
	deleteNotice, err := s.uiDeletedIssueNotice(ctx, r, project.OwnerUsername, projectID)
	if err != nil {
		return nil, err
	}
	var assigneeIDs []uuid.UUID
	var sprintQuery uiIssueListQuery
	var allQuery uiProjectAllQuery
	var assignees []model.ProjectAssignee
	var tags []model.IssueTag
	if view == "sprint" {
		sprintQuery, err = uiParseProjectAllQuery(r)
		if err != nil {
			return nil, err
		}
		assigneeIDs = sprintQuery.AssigneeIDs
	} else if view == "all" {
		allQuery, err = uiParseProjectAllQuery(r)
		if err != nil {
			return nil, err
		}
		assigneeIDs = allQuery.AssigneeIDs
	}

	panel := &uiProjectPanelData{
		Project:              project,
		View:                 view,
		Favorite:             favorite,
		ProjectTabs:          uiProjectTabs(project, view, assigneeIDs),
		AssigneeFilterActive: len(assigneeIDs) > 0,
		ClearAssigneeHref:    uiProjectViewPath(project, view),
		ClearAssigneeHXGet:   uiProjectPanelPath(project, view),
		ClearAssigneeHXPush:  uiProjectViewPath(project, view),
		DeleteNotice:         deleteNotice,
	}
	if view == "sprint" || view == "all" {
		assignees, err = s.store.ListProjectAssignees(ctx, projectID)
		if err != nil {
			return nil, err
		}
		tags, _, err = s.store.ListIssueTags(ctx, store.ListIssueTagsParams{
			ProjectID: projectID,
			Limit:     MaxLimit,
		})
		if err != nil {
			return nil, err
		}
		if view == "all" {
			panel.AssigneeFilters = uiProjectAllAssigneeFilters(project, assignees, allQuery)
			clearAssigneeQuery := allQuery
			clearAssigneeQuery.AssigneeIDs = nil
			panel.ClearAssigneeHref = uiProjectAllViewPath(project, clearAssigneeQuery)
			panel.ClearAssigneeHXGet = uiProjectAllPanelPath(project, clearAssigneeQuery)
			panel.ClearAssigneeHXPush = panel.ClearAssigneeHref
		} else {
			panel.AssigneeFilters = uiProjectSprintAssigneeFilters(project, assignees, sprintQuery)
			clearAssigneeQuery := sprintQuery
			clearAssigneeQuery.AssigneeIDs = nil
			panel.ClearAssigneeHref = uiProjectSprintViewPath(project, clearAssigneeQuery)
			panel.ClearAssigneeHXGet = uiProjectSprintPanelPath(project, clearAssigneeQuery)
			panel.ClearAssigneeHXPush = panel.ClearAssigneeHref
		}
	}

	switch view {
	case "about":
		attachments, attachmentsHasMore, err := s.store.ListProjectAttachments(ctx, store.ListProjectAttachmentsParams{ProjectID: projectID, Limit: MaxLimit})
		if err != nil {
			return nil, err
		}
		panel.ProjectAttachments = attachments
		panel.ProjectAttachmentsHasMore = attachmentsHasMore
		panel.ProjectDescriptionHTML = renderProjectDescriptionMarkdown(project, attachments)
		stats, err := s.store.GetProjectStats(ctx, store.ProjectStatsParams{ProjectID: projectID})
		if err != nil {
			return nil, err
		}
		panel.ProjectStats = stats
		projectTags, _, err := s.store.ListIssueTags(ctx, store.ListIssueTagsParams{
			ProjectID: projectID,
			Limit:     MaxLimit,
		})
		if err != nil {
			return nil, err
		}
		panel.Tags = projectTags
		contexts, contextHasMore, err := s.store.ListProjectContexts(ctx, store.ListProjectContextsParams{
			ProjectID: projectID,
			Limit:     MaxLimit,
		})
		if err != nil {
			return nil, err
		}
		panel.ContextHasMore = contextHasMore
		panel.ContextItems = make([]uiProjectContextItem, 0, len(contexts))
		for _, contextItem := range contexts {
			issues, issuesHasMore, err := s.store.ListIssuesForContext(ctx, store.ListIssuesForContextParams{
				ContextID: contextItem.ID,
				Limit:     MaxLimit,
			})
			if err != nil {
				return nil, err
			}
			panel.ContextItems = append(panel.ContextItems, uiProjectContextItem{
				Context:             contextItem,
				LinkedIssues:        issues,
				LinkedIssuesHasMore: issuesHasMore,
			})
		}
		return panel, nil
	case "sprint":
		activeStatus := model.SprintStatusActive
		activeSprints, _, err := s.store.ListSprints(ctx, store.ListSprintsParams{
			ProjectID: projectID,
			Status:    activeStatus,
			Limit:     1,
		})
		if err != nil {
			return nil, err
		}
		panel.SprintColumns = uiIssueColumns()
		panel.SprintControls = uiProjectSprintIssueControls(project, sprintQuery, tags, panel.AssigneeFilters, panel.AssigneeFilterActive, panel.ClearAssigneeHref, panel.ClearAssigneeHXGet, panel.ClearAssigneeHXPush)
		if len(activeSprints) == 0 {
			return panel, nil
		}
		panel.ActiveSprint = &activeSprints[0]
		activeAttachments, _, err := s.store.ListSprintAttachments(ctx, store.ListSprintAttachmentsParams{
			SprintID: panel.ActiveSprint.ID,
			Limit:    MaxLimit,
		})
		if err != nil {
			return nil, err
		}
		panel.ActiveSprintDescription = uiSprintDescriptionData{
			Project:         project,
			Sprint:          *panel.ActiveSprint,
			AttachmentCount: len(activeAttachments),
			DescriptionHTML: renderSprintDescriptionMarkdown(project, *panel.ActiveSprint, activeAttachments),
		}
		sprintIssues, sprintHasMore, err := s.store.ListIssues(ctx, store.ListIssuesParams{
			ProjectID:   projectID,
			Statuses:    sprintQuery.Statuses,
			Priorities:  sprintQuery.Priorities,
			TagNames:    sprintQuery.TagNames,
			AssigneeIDs: assigneeIDs,
			SprintID:    &panel.ActiveSprint.ID,
			Limit:       MaxLimit,
			Sort:        sprintQuery.Sort,
			Direction:   sprintQuery.Direction,
		})
		if err != nil {
			return nil, err
		}
		panel.SprintIssuesHasMore = sprintHasMore
		assigneesByID := uiProjectAssigneeMap(assignees)
		for _, issue := range sprintIssues {
			item := uiIssueItem{Issue: issue, Project: project, Sprint: panel.ActiveSprint, Assignee: uiIssueItemAssignee(issue, assigneesByID)}
			for i := range panel.SprintColumns {
				if panel.SprintColumns[i].Status == uiIssueColumnStatus(issue.Status) {
					panel.SprintColumns[i].Issues = append(panel.SprintColumns[i].Issues, item)
					break
				}
			}
		}
	case "planned":
		plannedStatus := model.SprintStatusPlanned
		planned, plannedHasMore, err := s.store.ListSprints(ctx, store.ListSprintsParams{
			ProjectID: projectID,
			Status:    plannedStatus,
			Limit:     MaxLimit,
		})
		if err != nil {
			return nil, err
		}
		panel.PlannedHasMore = plannedHasMore
		panel.PlannedSprints = make([]uiPlannedSprint, 0, len(planned))
		for _, sprint := range planned {
			attachments, _, err := s.store.ListSprintAttachments(ctx, store.ListSprintAttachmentsParams{
				SprintID: sprint.ID,
				Limit:    MaxLimit,
			})
			if err != nil {
				return nil, err
			}
			issues, issuesHasMore, err := s.store.ListIssues(ctx, store.ListIssuesParams{
				ProjectID: projectID,
				SprintID:  &sprint.ID,
				Limit:     MaxLimit,
			})
			if err != nil {
				return nil, err
			}
			panel.PlannedSprints = append(panel.PlannedSprints, uiPlannedSprint{
				Project:         project,
				Sprint:          sprint,
				Issues:          issues,
				HasMore:         issuesHasMore,
				AttachmentCount: len(attachments),
				DescriptionHTML: renderSprintDescriptionMarkdown(project, sprint, attachments),
			})
		}
	case "all":
		pageData, err := s.uiBuildProjectAllIssuePage(ctx, r, project)
		if err != nil {
			return nil, err
		}
		panel.AllIssues = pageData.Issues
		panel.AllIssuePage = pageData
		panel.AllControls = uiProjectAllIssueControls(project, allQuery, tags, panel.AssigneeFilters, panel.AssigneeFilterActive, panel.ClearAssigneeHref, panel.ClearAssigneeHXGet, panel.ClearAssigneeHXPush)
	case "changelog":
		pageData, err := s.uiBuildProjectChangelogPage(ctx, r, project)
		if err != nil {
			return nil, err
		}
		panel.ChangelogPage = pageData
	default:
		return nil, store.ErrNotFound
	}

	return panel, nil
}

func (s *Server) uiProjectChangelogPage(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	pageData, err := s.uiBuildProjectChangelogPage(r.Context(), r, project)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "project-changelog-page", pageData)
}

func (s *Server) uiBuildProjectChangelogPage(ctx context.Context, r *http.Request, project model.Project) (uiProjectChangelogPageData, error) {
	var cursor *store.ProjectChangelogCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.ProjectChangelogCursor
		if err := decodeCursor(raw, &c); err != nil {
			return uiProjectChangelogPageData{}, fmt.Errorf("invalid changelog cursor: %w", errUIBadRequest)
		}
		cursor = &c
	}
	entries, hasMore, err := s.store.ListProjectChangelog(ctx, store.ListProjectChangelogParams{
		ProjectID: project.ID,
		Cursor:    cursor,
		Limit:     DefaultLimit,
	})
	if err != nil {
		return uiProjectChangelogPageData{}, err
	}
	pageData := uiProjectChangelogPageData{
		Project: project,
		Entries: entries,
		HasMore: hasMore,
	}
	if hasMore {
		last := entries[len(entries)-1]
		pageData.NextCursor = encodeCursor(store.ProjectChangelogCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return pageData, nil
}

func (s *Server) uiBuildProjectAllIssuePage(ctx context.Context, r *http.Request, project model.Project) (uiProjectAllIssuePageData, error) {
	if err := s.uiRequireProjectAccess(ctx, currentUser(r), project.ID); err != nil {
		return uiProjectAllIssuePageData{}, err
	}
	allQuery, err := uiParseProjectAllQuery(r)
	if err != nil {
		return uiProjectAllIssuePageData{}, err
	}
	var cursor *store.IssuesCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.IssuesCursor
		if err := decodeCursor(raw, &c); err != nil {
			return uiProjectAllIssuePageData{}, fmt.Errorf("invalid all issues cursor: %w", errUIBadRequest)
		}
		cursor = &c
	}
	issues, hasMore, err := s.store.ListIssues(ctx, store.ListIssuesParams{
		ProjectID:        project.ID,
		Statuses:         allQuery.Statuses,
		Priorities:       allQuery.Priorities,
		TagNames:         allQuery.TagNames,
		AssigneeIDs:      allQuery.AssigneeIDs,
		Cursor:           cursor,
		Limit:            DefaultLimit,
		Sort:             allQuery.Sort,
		Direction:        allQuery.Direction,
		IncludeSubIssues: true,
	})
	if err != nil {
		return uiProjectAllIssuePageData{}, err
	}
	pageData := uiProjectAllIssuePageData{Issues: issues}
	if hasMore {
		last := issues[len(issues)-1]
		nextQuery := allQuery
		nextQuery.Cursor = encodeCursor(issueListCursor(last, allQuery.Sort))
		pageData.NextHXGet = uiProjectAllPagePath(project, nextQuery)
	}
	return pageData, nil
}

func (s *Server) uiBuildDeletedIssuesPanel(ctx context.Context, r *http.Request, projectID uuid.UUID) (*uiDeletedIssuesPanelData, error) {
	if err := s.uiRequireProjectAccess(ctx, currentUser(r), projectID); err != nil {
		return nil, err
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	deleted, hasMore, err := s.store.ListDeletedIssues(ctx, store.ListDeletedIssuesParams{
		ProjectID: projectID,
		Limit:     MaxLimit,
	})
	if err != nil {
		return nil, err
	}
	return &uiDeletedIssuesPanelData{
		Project: project,
		Issues:  deleted,
		HasMore: hasMore,
	}, nil
}

func (s *Server) uiBuildDeletedIssuePanel(ctx context.Context, r *http.Request, issue model.Issue) (*uiDeletedIssuePanelData, error) {
	if err := s.uiRequireProjectAccess(ctx, currentUser(r), issue.ProjectID); err != nil {
		return nil, err
	}
	project, err := s.store.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return nil, err
	}
	return &uiDeletedIssuePanelData{
		Issue:     issue,
		Project:   project,
		BackHref:  uiProjectViewPath(project, "deleted"),
		BackHXGet: uiProjectPanelPath(project, "deleted"),
	}, nil
}
