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
	"initials":      uiInitials,
	"linkLabel":     uiIssueLinkLabel,
	"projectIcon":   uiProjectIcon,
	"sprintDate":    uiSprintDate,
	"statusClass":   uiStatusClass,
	"statusLabel":   uiStatusLabel,
	"statusRow":     uiStatusRowClass,
	"statusSurface": uiStatusSurfaceClass,
	"tokenTime":     uiTokenTime,
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
	IssuePanel       *uiIssuePanelData
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

type uiIssueCommentItem struct {
	Comment     model.Comment
	AuthorName  string
	AuthorEmail string
}

type uiIssueLinkItem struct {
	Link        model.IssueLink
	LinkedIssue model.Issue
	HasIssue    bool
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

type uiIssuePanelData struct {
	Issue           model.Issue
	Project         model.Project
	Sprint          *model.Sprint
	Assignee        *model.User
	Reporter        *model.User
	Comments        []uiIssueCommentItem
	CommentsHasMore bool
	CommentBody     string
	CommentError    string
	Links           []uiIssueLinkItem
	LinksHasMore    bool
	BackHref        string
	BackHXGet       string
	BackLabel       string
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
		r.Get("/issues/{id}", s.uiIssuePage)
		r.Get("/issues/{id}/panel", s.uiIssuePanel)
		r.Post("/issues/{id}/comments", s.uiCreateComment)
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

func (s *Server) uiIssuePage(w http.ResponseWriter, r *http.Request) {
	issueID, ok := uiIssueID(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	renderUITemplate(w, http.StatusOK, "shell", uiShellData{
		User:             currentUser(r),
		Projects:         projects,
		CurrentProjectID: panel.Project.ID,
		CurrentView:      "projects",
		IssuePanel:       panel,
	})
}

func (s *Server) uiIssuePanel(w http.ResponseWriter, r *http.Request) {
	issueID, ok := uiIssueID(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiCreateComment(w http.ResponseWriter, r *http.Request) {
	issueID, ok := uiIssueID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	body := strings.TrimSpace(r.Form.Get("body"))
	if body == "" || len(body) > 10000 {
		s.renderUIIssuePanelWithCommentError(w, r, issueID, r.Form.Get("body"), "Comment required, max 10000 chars.")
		return
	}
	projectID, err := s.store.ProjectIDForIssue(r.Context(), issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), projectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	if _, err := s.store.CreateComment(r.Context(), store.CreateCommentParams{
		IssueID:  issueID,
		AuthorID: currentUser(r).ID,
		Body:     body,
	}); err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) renderUIIssuePanelWithCommentError(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, body, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.CommentBody = body
	panel.CommentError = message
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
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

func (s *Server) uiBuildIssuePanel(ctx context.Context, r *http.Request, issueID uuid.UUID) (*uiIssuePanelData, error) {
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
	assignee, err := s.uiOptionalUser(ctx, issue.AssigneeID)
	if err != nil {
		return nil, err
	}
	reporter, err := s.uiOptionalUser(ctx, issue.ReporterID)
	if err != nil {
		return nil, err
	}
	sprint, err := s.uiOptionalSprint(ctx, issue.SprintID)
	if err != nil {
		return nil, err
	}
	comments, commentsHasMore, err := s.store.ListCommentsForIssue(ctx, store.ListCommentsForIssueParams{
		IssueID: issueID,
		Limit:   MaxLimit,
	})
	if err != nil {
		return nil, err
	}
	commentItems := make([]uiIssueCommentItem, 0, len(comments))
	for _, comment := range comments {
		author, err := s.uiOptionalUser(ctx, &comment.AuthorID)
		if err != nil {
			return nil, err
		}
		item := uiIssueCommentItem{Comment: comment, AuthorName: "Unknown user"}
		if author != nil {
			item.AuthorName = author.Name
			item.AuthorEmail = author.Email
		}
		commentItems = append(commentItems, item)
	}
	links, linksHasMore, err := s.store.ListIssueLinksForIssue(ctx, store.ListIssueLinksForIssueParams{
		IssueID: issueID,
		Limit:   MaxLimit,
	})
	if err != nil {
		return nil, err
	}
	linkedIssues, err := s.uiLinkedIssues(ctx, issueID, links)
	if err != nil {
		return nil, err
	}
	linkItems := make([]uiIssueLinkItem, 0, len(links))
	for _, link := range links {
		otherID := link.SourceID
		if otherID == issueID {
			otherID = link.TargetID
		}
		item := uiIssueLinkItem{Link: link}
		if linked, ok := linkedIssues[otherID]; ok {
			item.LinkedIssue = linked
			item.HasIssue = true
		}
		linkItems = append(linkItems, item)
	}

	backHref, backHXGet, backLabel := uiIssueBackLink(projectID, issue, sprint)
	return &uiIssuePanelData{
		Issue:           issue,
		Project:         project,
		Sprint:          sprint,
		Assignee:        assignee,
		Reporter:        reporter,
		Comments:        commentItems,
		CommentsHasMore: commentsHasMore,
		Links:           linkItems,
		LinksHasMore:    linksHasMore,
		BackHref:        backHref,
		BackHXGet:       backHXGet,
		BackLabel:       backLabel,
	}, nil
}

func (s *Server) uiOptionalUser(ctx context.Context, id *uuid.UUID) (*model.User, error) {
	if id == nil {
		return nil, nil
	}
	user, err := s.store.GetUser(ctx, *id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (s *Server) uiOptionalSprint(ctx context.Context, id *uuid.UUID) (*model.Sprint, error) {
	if id == nil {
		return nil, nil
	}
	sprint, err := s.store.GetSprint(ctx, *id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &sprint, nil
}

func (s *Server) uiLinkedIssues(ctx context.Context, issueID uuid.UUID, links []model.IssueLink) (map[uuid.UUID]model.Issue, error) {
	seen := map[uuid.UUID]struct{}{}
	ids := make([]uuid.UUID, 0, len(links))
	for _, link := range links {
		otherID := link.SourceID
		if otherID == issueID {
			otherID = link.TargetID
		}
		if _, ok := seen[otherID]; ok {
			continue
		}
		seen[otherID] = struct{}{}
		ids = append(ids, otherID)
	}
	issues, err := s.store.ListIssuesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]model.Issue, len(issues))
	for _, issue := range issues {
		out[issue.ID] = issue
	}
	return out, nil
}

func uiIssueBackLink(projectID uuid.UUID, issue model.Issue, sprint *model.Sprint) (href, hxGet, label string) {
	view := "sprint"
	if issue.SprintID == nil || (sprint != nil && sprint.Status == model.SprintStatusPlanned) {
		view = "backlog"
	}
	base := "/projects/" + projectID.String() + "/" + view
	label = "Sprint"
	if view == "backlog" {
		label = "Backlog"
	}
	return base, base + "/panel", label
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

func uiIssueID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	issueID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid issue id", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return issueID, true
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
	case safeUIIssuePath(path):
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

func safeUIIssuePath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/issues/")
	if !ok || rest == "" {
		return false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 1 && len(parts) != 2 {
		return false
	}
	if _, err := uuid.Parse(parts[0]); err != nil {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	return parts[1] == "panel"
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

func uiStatusClass(s model.Status) string {
	switch s {
	case model.StatusTodo:
		return "border-slate-300 bg-slate-100 text-slate-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
	case model.StatusInProgress:
		return "border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-500/40 dark:bg-amber-950/40 dark:text-amber-200"
	case model.StatusDone:
		return "border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-500/40 dark:bg-emerald-950/40 dark:text-emerald-200"
	default:
		return "border-slate-300 bg-slate-50 text-slate-700 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"
	}
}

func uiStatusRowClass(s model.Status) string {
	switch s {
	case model.StatusTodo:
		return "bg-slate-50/70 hover:bg-slate-100/80 dark:bg-slate-900/30 dark:hover:bg-slate-800/70"
	case model.StatusInProgress:
		return "bg-amber-50/45 hover:bg-amber-50 dark:bg-amber-950/15 dark:hover:bg-amber-950/30"
	case model.StatusDone:
		return "bg-emerald-50/45 hover:bg-emerald-50 dark:bg-emerald-950/15 dark:hover:bg-emerald-950/30"
	default:
		return "bg-white hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800/60"
	}
}

func uiStatusSurfaceClass(s model.Status) string {
	switch s {
	case model.StatusTodo:
		return "bg-slate-50/70 dark:bg-slate-900/30"
	case model.StatusInProgress:
		return "bg-amber-50/45 dark:bg-amber-950/15"
	case model.StatusDone:
		return "bg-emerald-50/45 dark:bg-emerald-950/15"
	default:
		return "bg-white dark:bg-slate-900"
	}
}

func uiIssueColumns() []uiIssueColumn {
	return []uiIssueColumn{
		{Status: model.StatusTodo, Label: uiStatusLabel(model.StatusTodo)},
		{Status: model.StatusInProgress, Label: uiStatusLabel(model.StatusInProgress)},
		{Status: model.StatusDone, Label: uiStatusLabel(model.StatusDone)},
	}
}

func uiIssueLinkLabel(link model.IssueLink, issueID uuid.UUID) string {
	outgoing := link.SourceID == issueID
	switch link.LinkType {
	case model.LinkTypeBlocks:
		if outgoing {
			return "Blocks"
		}
		return "Blocked by"
	case model.LinkTypeDuplicates:
		if outgoing {
			return "Duplicates"
		}
		return "Duplicated by"
	case model.LinkTypeRelatesTo:
		return "Relates to"
	case model.LinkTypeClones:
		if outgoing {
			return "Clones"
		}
		return "Cloned by"
	default:
		return string(link.LinkType)
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
