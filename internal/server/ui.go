package server

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

//go:embed templates/*.html
var uiTemplateFS embed.FS

const uiAuthCookieName = "track_slash_ui_token"

var errUIForbidden = errors.New("forbidden")

var uiTemplates = template.Must(template.New("ui").Funcs(template.FuncMap{
	"initials":    uiInitials,
	"projectIcon": uiProjectIcon,
	"sprintDate":  uiSprintDate,
	"statusLabel": uiStatusLabel,
	"tokenTime":   uiTokenTime,
}).ParseFS(uiTemplateFS, "templates/*.html"))

type uiLoginData struct {
	Error string
	Next  string
}

type uiSignupData struct {
	Error string
	Next  string
}

type uiShellData struct {
	User             model.User
	Projects         []model.Project
	CurrentProjectID uuid.UUID
	CurrentView      string
	WorkPanel        *uiWorkPanelData
	ProjectsPanel    *uiProjectsPanelData
	ProjectPanel     *uiProjectPanelData
	TokenPanel       *uiTokenPanelData
	SettingsPanel    *uiSettingsPanelData
}

type uiIssueItem struct {
	Issue   model.Issue
	Project model.Project
	Sprint  *model.Sprint
}

type uiIssueColumn struct {
	Status model.Status
	Label  string
	Issues []uiIssueItem
}

type uiPlannedSprint struct {
	Sprint  model.Sprint
	Issues  []model.Issue
	HasMore bool
}

type uiTabBarData struct {
	Label string
	Items []uiTabItem
}

type uiTabItem struct {
	Label     string
	Icon      string
	Href      string
	HXGet     string
	HXTarget  string
	HXPushURL string
	Active    bool
}

type uiWorkPanelData struct {
	View         string
	Title        string
	Subtitle     string
	Issues       []uiIssueItem
	Columns      []uiIssueColumn
	HasMore      bool
	ProjectCount int
}

type uiProjectPanelData struct {
	Project             model.Project
	View                string
	ProjectTabs         uiTabBarData
	ActiveSprint        *model.Sprint
	SprintColumns       []uiIssueColumn
	PlannedSprints      []uiPlannedSprint
	BacklogIssues       []model.Issue
	SprintIssuesHasMore bool
	PlannedHasMore      bool
	BacklogHasMore      bool
}

type uiProjectsPanelData struct {
	Projects    []model.Project
	HasMore     bool
	Error       string
	Key         string
	Name        string
	Description string
}

type uiTokenPanelData struct {
	Tokens  []model.AuthToken
	Error   string
	Created string
}

type uiSettingsPanelData struct {
	User            model.User
	ProfileError    string
	ProfileSaved    bool
	PasswordError   string
	PasswordChanged bool
}

func (s *Server) mountUIRoutes(r chi.Router) {
	r.Get("/login", s.uiLoginPage)
	r.Post("/login", s.uiLogin)
	r.Get("/signup", s.uiSignupPage)
	r.Post("/signup", s.uiSignup)
	r.Post("/logout", s.uiLogout)

	r.Group(func(r chi.Router) {
		r.Use(s.uiAuthMiddleware)
		r.Get("/", s.uiHome)
		r.Get("/me", func(w http.ResponseWriter, r *http.Request) { s.uiWorkPage(w, r, "me") })
		r.Get("/me/panel", func(w http.ResponseWriter, r *http.Request) { s.uiWorkPanel(w, r, "me") })
		r.Get("/projects", s.uiProjectsPage)
		r.Get("/projects/panel", s.uiProjectsPanel)
		r.Post("/projects", s.uiCreateProject)
		r.Get("/settings", s.uiSettingsPage)
		r.Post("/settings/profile", s.uiUpdateProfile)
		r.Post("/settings/password", s.uiUpdatePassword)
		r.Get("/tokens", s.uiTokensPage)
		r.Post("/tokens", s.uiCreateToken)
		r.Post("/tokens/{id}/revoke", s.uiRevokeToken)
		r.Get("/projects/{id}", s.uiProjectPage)
		r.Get("/projects/{id}/sprint", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPage(w, r, "sprint") })
		r.Get("/projects/{id}/sprint/panel", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPanel(w, r, "sprint") })
		r.Get("/projects/{id}/backlog", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPage(w, r, "backlog") })
		r.Get("/projects/{id}/backlog/panel", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPanel(w, r, "backlog") })
	})
}

func (s *Server) uiLoginPage(w http.ResponseWriter, r *http.Request) {
	renderUITemplate(w, http.StatusOK, "login", uiLoginData{
		Next: safeUINext(r.URL.Query().Get("next")),
	})
}

func (s *Server) uiLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderUITemplate(w, http.StatusBadRequest, "login", uiLoginData{Error: "Unable to read form."})
		return
	}
	username := strings.TrimSpace(r.Form.Get("username"))
	password := r.Form.Get("password")
	next := safeUINext(r.Form.Get("next"))
	if username == "" || password == "" {
		renderUITemplate(w, http.StatusUnauthorized, "login", uiLoginData{Error: "Username and password required.", Next: next})
		return
	}
	u, err := s.store.AuthenticatePassword(r.Context(), username, password)
	if err != nil {
		if errors.Is(err, store.ErrUnauthorized) {
			renderUITemplate(w, http.StatusUnauthorized, "login", uiLoginData{Error: "Username or password not accepted.", Next: next})
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	created, err := s.store.CreateAuthToken(r.Context(), store.CreateAuthTokenParams{
		UserID: u.ID,
		Kind:   model.AuthTokenKindSession,
		Name:   "web session",
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	setUISessionCookie(w, r, created.RawToken, created.Token.ExpiresAt)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) uiSignupPage(w http.ResponseWriter, r *http.Request) {
	renderUITemplate(w, http.StatusOK, "signup", uiSignupData{
		Next: safeUINext(r.URL.Query().Get("next")),
	})
}

func (s *Server) uiSignup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderUITemplate(w, http.StatusBadRequest, "signup", uiSignupData{Error: "Unable to read form."})
		return
	}
	next := safeUINext(r.Form.Get("next"))
	u, err := s.store.CreateAccount(r.Context(), store.CreateAccountParams{
		Username: r.Form.Get("username"),
		Password: r.Form.Get("password"),
		Name:     r.Form.Get("name"),
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			renderUITemplate(w, http.StatusConflict, "signup", uiSignupData{Error: "Username already exists.", Next: next})
			return
		}
		renderUITemplate(w, http.StatusBadRequest, "signup", uiSignupData{Error: err.Error(), Next: next})
		return
	}
	created, err := s.store.CreateAuthToken(r.Context(), store.CreateAuthTokenParams{
		UserID: u.ID,
		Kind:   model.AuthTokenKindSession,
		Name:   "web session",
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	setUISessionCookie(w, r, created.RawToken, created.Token.ExpiresAt)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) uiLogout(w http.ResponseWriter, r *http.Request) {
	clearUISessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func setUISessionCookie(w http.ResponseWriter, r *http.Request, raw string, expiresAt *time.Time) {
	cookie := &http.Cookie{
		Name:     uiAuthCookieName,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
	if expiresAt != nil {
		cookie.Expires = *expiresAt
	}
	http.SetCookie(w, cookie)
}

func clearUISessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     uiAuthCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
}

func (s *Server) uiAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(uiAuthCookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			redirectUILogin(w, r)
			return
		}
		auth, err := s.store.AuthenticateToken(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, store.ErrUnauthorized) {
				http.SetCookie(w, &http.Cookie{
					Name:     uiAuthCookieName,
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					Expires:  time.Unix(0, 0),
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
					Secure:   r.TLS != nil,
				})
				redirectUILogin(w, r)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		ctx := context.WithValue(r.Context(), authContextKey{}, authContext{User: auth.User, Token: auth.Token})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) uiTokensPage(w http.ResponseWriter, r *http.Request) {
	s.renderUITokens(w, r, "", "")
}

func (s *Server) uiSettingsPage(w http.ResponseWriter, r *http.Request) {
	s.renderUISettings(w, r, currentUser(r), "", false, "", false)
}

func (s *Server) uiUpdateProfile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderUISettings(w, r, currentUser(r), "Unable to read form.", false, "", false)
		return
	}
	user, err := s.store.UpdateUserProfile(r.Context(), currentUser(r).ID, r.Form.Get("name"), r.Form.Get("email"))
	if err != nil {
		s.renderUISettings(w, r, currentUser(r), err.Error(), false, "", false)
		return
	}
	s.renderUISettings(w, r, user, "", true, "", false)
}

func (s *Server) uiUpdatePassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderUISettings(w, r, currentUser(r), "", false, "Unable to read form.", false)
		return
	}
	if err := s.store.ChangePassword(r.Context(), currentUser(r).ID, r.Form.Get("current_password"), r.Form.Get("new_password")); err != nil {
		if errors.Is(err, store.ErrUnauthorized) {
			s.renderUISettings(w, r, currentUser(r), "", false, "Current password not accepted.", false)
			return
		}
		s.renderUISettings(w, r, currentUser(r), "", false, err.Error(), false)
		return
	}
	s.renderUISettings(w, r, currentUser(r), "", false, "", true)
}

func (s *Server) renderUISettings(w http.ResponseWriter, r *http.Request, user model.User, profileError string, profileSaved bool, passwordError string, passwordChanged bool) {
	projects, err := s.uiVisibleProjects(r.Context(), user)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	renderUITemplate(w, http.StatusOK, "shell", uiShellData{
		User:        user,
		Projects:    projects,
		CurrentView: "settings",
		SettingsPanel: &uiSettingsPanelData{
			User:            user,
			ProfileError:    profileError,
			ProfileSaved:    profileSaved,
			PasswordError:   passwordError,
			PasswordChanged: passwordChanged,
		},
	})
}

func (s *Server) uiCreateToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderUITokens(w, r, "Unable to read form.", "")
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	if name == "" || len(name) > 200 {
		s.renderUITokens(w, r, "Name required, max 200 chars.", "")
		return
	}
	created, err := s.store.CreateAuthToken(r.Context(), store.CreateAuthTokenParams{
		UserID: currentUser(r).ID,
		Kind:   model.AuthTokenKindAPI,
		Name:   name,
	})
	if err != nil {
		s.renderUITokens(w, r, "Unable to create token.", "")
		return
	}
	s.renderUITokens(w, r, "", created.RawToken)
}

func (s *Server) uiRevokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid token id", http.StatusBadRequest)
		return
	}
	if err := s.store.RevokeAuthTokenForUser(r.Context(), currentUser(r).ID, id); err != nil {
		writeUIStoreError(w, err)
		return
	}
	if currentAuth(r).Token.ID == id {
		clearUISessionCookie(w, r)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/tokens", http.StatusSeeOther)
}

func (s *Server) renderUITokens(w http.ResponseWriter, r *http.Request, message, created string) {
	tokens, err := s.store.ListAuthTokens(r.Context(), currentUser(r).ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	renderUITemplate(w, http.StatusOK, "shell", uiShellData{
		User:        currentUser(r),
		Projects:    projects,
		CurrentView: "tokens",
		TokenPanel:  &uiTokenPanelData{Tokens: tokens, Error: message, Created: created},
	})
}

func (s *Server) uiHome(w http.ResponseWriter, r *http.Request) {
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(projects) == 0 {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/projects/"+projects[0].ID.String()+"/sprint", http.StatusSeeOther)
}

func (s *Server) uiWorkPage(w http.ResponseWriter, r *http.Request, view string) {
	panel, err := s.uiBuildWorkPanel(r.Context(), currentUser(r), view)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	renderUITemplate(w, http.StatusOK, "shell", uiShellData{
		User:        currentUser(r),
		Projects:    projects,
		CurrentView: view,
		WorkPanel:   panel,
	})
}

func (s *Server) uiWorkPanel(w http.ResponseWriter, r *http.Request, view string) {
	panel, err := s.uiBuildWorkPanel(r.Context(), currentUser(r), view)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	renderUITemplate(w, http.StatusOK, "work-panel", panel)
}

func (s *Server) uiProjectsPage(w http.ResponseWriter, r *http.Request) {
	s.renderUIProjects(w, r, http.StatusOK, "", "", "", "")
}

func (s *Server) uiProjectsPanel(w http.ResponseWriter, r *http.Request) {
	panel, err := s.uiBuildProjectsPanel(r.Context(), currentUser(r), "", "", "", "")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	renderUITemplate(w, http.StatusOK, "projects-panel", panel)
}

func (s *Server) uiCreateProject(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderUIProjects(w, r, http.StatusBadRequest, "Unable to read form.", "", "", "")
		return
	}
	key := strings.TrimSpace(r.Form.Get("key"))
	name := strings.TrimSpace(r.Form.Get("name"))
	description := r.Form.Get("description")
	if !projectKeyRe.MatchString(key) {
		s.renderUIProjects(w, r, http.StatusBadRequest, "Key must match ^[A-Z][A-Z0-9]{1,9}$.", key, name, description)
		return
	}
	if name == "" {
		s.renderUIProjects(w, r, http.StatusBadRequest, "Name required.", key, name, description)
		return
	}
	project, err := s.store.CreateProjectForUser(r.Context(), currentUser(r).ID, key, name, description)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIProjects(w, r, http.StatusConflict, "Project key already exists.", key, name, description)
			return
		}
		writeUIStoreError(w, err)
		return
	}
	http.Redirect(w, r, "/projects/"+project.ID.String()+"/sprint", http.StatusSeeOther)
}

func (s *Server) renderUIProjects(w http.ResponseWriter, r *http.Request, status int, message, key, name, description string) {
	panel, err := s.uiBuildProjectsPanel(r.Context(), currentUser(r), message, key, name, description)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	renderUITemplate(w, status, "shell", uiShellData{
		User:          currentUser(r),
		Projects:      panel.Projects,
		CurrentView:   "projects",
		ProjectsPanel: panel,
	})
}

func (s *Server) uiProjectPage(w http.ResponseWriter, r *http.Request) {
	projectID, ok := uiProjectID(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), projectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	http.Redirect(w, r, "/projects/"+projectID.String()+"/sprint", http.StatusSeeOther)
}

func (s *Server) uiProjectWorkPage(w http.ResponseWriter, r *http.Request, view string) {
	projectID, ok := uiProjectID(w, r)
	if !ok {
		return
	}
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	panel, err := s.uiBuildProjectPanel(r.Context(), r, projectID, view)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "shell", uiShellData{
		User:             currentUser(r),
		Projects:         projects,
		CurrentProjectID: projectID,
		CurrentView:      "projects",
		ProjectPanel:     panel,
	})
}

func (s *Server) uiProjectWorkPanel(w http.ResponseWriter, r *http.Request, view string) {
	projectID, ok := uiProjectID(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildProjectPanel(r.Context(), r, projectID, view)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "project-panel", panel)
}

func (s *Server) uiBuildWorkPanel(ctx context.Context, user model.User, view string) (*uiWorkPanelData, error) {
	projects, err := s.uiVisibleProjects(ctx, user)
	if err != nil {
		return nil, err
	}
	panel := &uiWorkPanelData{
		View:         view,
		ProjectCount: len(projects),
	}
	switch view {
	case "me":
		panel.Title = "Me"
		panel.Subtitle = "Issues assigned to you across accessible projects."
		panel.Issues, panel.HasMore, err = s.uiAssignedIssues(ctx, projects, user.ID)
	default:
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return panel, nil
}

func (s *Server) uiBuildProjectsPanel(ctx context.Context, user model.User, message, key, name, description string) (*uiProjectsPanelData, error) {
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
		Projects:    all,
		HasMore:     hasMore,
		Error:       message,
		Key:         key,
		Name:        name,
		Description: description,
	}, nil
}

func (s *Server) uiAssignedIssues(ctx context.Context, projects []model.Project, userID uuid.UUID) ([]uiIssueItem, bool, error) {
	var out []uiIssueItem
	var hasMore bool
	for _, project := range projects {
		issues, more, err := s.store.ListIssues(ctx, store.ListIssuesParams{ProjectID: project.ID, Limit: MaxLimit})
		if err != nil {
			return nil, false, err
		}
		hasMore = hasMore || more
		for _, issue := range issues {
			if issue.AssigneeID != nil && *issue.AssigneeID == userID {
				out = append(out, uiIssueItem{Issue: issue, Project: project})
			}
		}
	}
	return out, hasMore, nil
}

func (s *Server) uiBuildProjectPanel(ctx context.Context, r *http.Request, projectID uuid.UUID, view string) (*uiProjectPanelData, error) {
	if err := s.uiRequireProjectAccess(ctx, currentUser(r), projectID); err != nil {
		return nil, err
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	panel := &uiProjectPanelData{
		Project:     project,
		View:        view,
		ProjectTabs: uiProjectTabs(projectID, view),
	}

	switch view {
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
		if len(activeSprints) == 0 {
			return panel, nil
		}
		panel.ActiveSprint = &activeSprints[0]
		sprintIssues, sprintHasMore, err := s.store.ListIssues(ctx, store.ListIssuesParams{
			ProjectID: projectID,
			SprintID:  &panel.ActiveSprint.ID,
			Limit:     MaxLimit,
		})
		if err != nil {
			return nil, err
		}
		panel.SprintIssuesHasMore = sprintHasMore
		for _, issue := range sprintIssues {
			item := uiIssueItem{Issue: issue, Project: project, Sprint: panel.ActiveSprint}
			for i := range panel.SprintColumns {
				if panel.SprintColumns[i].Status == issue.Status {
					panel.SprintColumns[i].Issues = append(panel.SprintColumns[i].Issues, item)
					break
				}
			}
		}
	case "backlog":
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
			issues, issuesHasMore, err := s.store.ListIssues(ctx, store.ListIssuesParams{
				ProjectID: projectID,
				SprintID:  &sprint.ID,
				Limit:     MaxLimit,
			})
			if err != nil {
				return nil, err
			}
			panel.PlannedSprints = append(panel.PlannedSprints, uiPlannedSprint{
				Sprint:  sprint,
				Issues:  issues,
				HasMore: issuesHasMore,
			})
		}

		backlog, backlogHasMore, err := s.store.ListIssues(ctx, store.ListIssuesParams{
			ProjectID: projectID,
			Backlog:   true,
			Limit:     MaxLimit,
		})
		if err != nil {
			return nil, err
		}
		panel.BacklogIssues = backlog
		panel.BacklogHasMore = backlogHasMore
	default:
		return nil, store.ErrNotFound
	}

	return panel, nil
}

func uiProjectTabs(projectID uuid.UUID, view string) uiTabBarData {
	base := "/projects/" + projectID.String()
	return uiTabBarData{
		Label: "Project views",
		Items: []uiTabItem{
			{
				Label:     "Sprints",
				Icon:      "kanban",
				Href:      base + "/sprint",
				HXGet:     base + "/sprint/panel",
				HXTarget:  "#main",
				HXPushURL: base + "/sprint",
				Active:    view == "sprint",
			},
			{
				Label:     "Backlog",
				Icon:      "archive",
				Href:      base + "/backlog",
				HXGet:     base + "/backlog/panel",
				HXTarget:  "#main",
				HXPushURL: base + "/backlog",
				Active:    view == "backlog",
			},
		},
	}
}

func (s *Server) uiVisibleProjects(ctx context.Context, user model.User) ([]model.Project, error) {
	var all []model.Project
	var cursor *store.ProjectsCursor
	for {
		projects, hasMore, err := s.store.ListProjects(ctx, store.ListProjectsParams{
			Cursor:        cursor,
			Limit:         MaxLimit,
			VisibleToUser: visibleProjectUser(user),
		})
		if err != nil {
			return nil, err
		}
		all = append(all, projects...)
		if !hasMore {
			return all, nil
		}
		last := projects[len(projects)-1]
		cursor = &store.ProjectsCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}

func (s *Server) uiRequireProjectAccess(ctx context.Context, user model.User, projectID uuid.UUID) error {
	if user.IsAdmin {
		return nil
	}
	if _, err := s.store.GetProject(ctx, projectID); err != nil {
		return err
	}
	ok, err := s.store.UserCanAccessProject(ctx, user, projectID)
	if err != nil {
		return err
	}
	if !ok {
		return errUIForbidden
	}
	return nil
}

func uiProjectID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid project id", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return projectID, true
}

func redirectUILogin(w http.ResponseWriter, r *http.Request) {
	next := url.QueryEscape(safeUINext(r.URL.RequestURI()))
	http.Redirect(w, r, "/login?next="+next, http.StatusSeeOther)
}

func safeUINext(raw string) string {
	if raw == "" {
		return "/"
	}
	if strings.HasPrefix(raw, "//") || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "/api/v1") {
		return "/"
	}
	path, _, _ := strings.Cut(raw, "?")
	switch {
	case path == "/", path == "/me", path == "/me/panel", path == "/projects", path == "/projects/panel", path == "/settings", path == "/tokens":
		return raw
	case safeUIProjectPath(path):
		return raw
	default:
		return "/"
	}
}

func safeUIProjectPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/projects/")
	if !ok || rest == "" {
		return false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 1 && len(parts) != 2 && len(parts) != 3 {
		return false
	}
	if _, err := uuid.Parse(parts[0]); err != nil {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	if parts[1] != "sprint" && parts[1] != "backlog" {
		return false
	}
	if len(parts) == 2 {
		return true
	}
	return parts[2] == "panel"
}

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
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, errUIForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func uiInitials(name, email string) string {
	source := strings.TrimSpace(name)
	if source == "" {
		source = strings.TrimSpace(email)
	}
	if source == "" {
		return "?"
	}
	parts := strings.Fields(source)
	if len(parts) == 1 {
		return strings.ToUpper(string([]rune(parts[0])[0]))
	}
	first := []rune(parts[0])
	last := []rune(parts[len(parts)-1])
	return strings.ToUpper(string(first[0]) + string(last[0]))
}

func uiProjectIcon(name, key string) string {
	source := strings.TrimSpace(name)
	if source == "" {
		source = strings.TrimSpace(key)
	}
	if source == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(source)[0]))
}

func uiSprintDate(t time.Time) string {
	return t.Format("Jan 2")
}

func uiStatusLabel(s model.Status) string {
	switch s {
	case model.StatusTodo:
		return "To do"
	case model.StatusInProgress:
		return "In progress"
	case model.StatusDone:
		return "Done"
	default:
		return string(s)
	}
}

func uiIssueColumns() []uiIssueColumn {
	return []uiIssueColumn{
		{Status: model.StatusTodo, Label: uiStatusLabel(model.StatusTodo)},
		{Status: model.StatusInProgress, Label: uiStatusLabel(model.StatusInProgress)},
		{Status: model.StatusDone, Label: uiStatusLabel(model.StatusDone)},
	}
}

func uiTokenTime(v any) string {
	if v == nil {
		return "-"
	}
	switch t := v.(type) {
	case time.Time:
		return t.Format("Jan 2, 2006 15:04")
	case *time.Time:
		if t == nil {
			return "-"
		}
		return t.Format("Jan 2, 2006 15:04")
	default:
		return "-"
	}
}
