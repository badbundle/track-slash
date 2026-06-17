package server

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
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
var errUIBadRequest = errors.New("bad request")

var uiTemplates = template.Must(template.New("ui").Funcs(template.FuncMap{
	"initials":             uiInitials,
	"issueAssignee":        uiIssueAssigneePath,
	"issueAssigneeEdit":    uiIssueAssigneeEditPath,
	"issueComments":        uiIssueCommentsPath,
	"issueDelete":          uiIssueDeletePath,
	"issueDescription":     uiIssueDescriptionPath,
	"issueDescriptionEdit": uiIssueDescriptionEditPath,
	"issueHref":            uiIssuePath,
	"issueLink":            uiIssueLinkPath,
	"issueLinkDelete":      uiIssueLinkDeletePath,
	"issueLinkEdit":        uiIssueLinkEditPath,
	"issueLinkNew":         uiIssueLinkNewPath,
	"issueLinks":           uiIssueLinksPath,
	"issuePanel":           uiIssuePanelPath,
	"issuePriority":        uiIssuePriorityPath,
	"issuePriorityEdit":    uiIssuePriorityEditPath,
	"issueReporter":        uiIssueReporterPath,
	"issueReporterEdit":    uiIssueReporterEditPath,
	"issueRestore":         uiIssueRestorePath,
	"issueStatus":          uiIssueStatusPath,
	"issueStatusEdit":      uiIssueStatusEditPath,
	"issueSubIssues":       uiIssueSubIssuesPath,
	"linkLabel":            uiIssueLinkLabel,
	"linkOptions":          uiIssueLinkOptions,
	"priorityClass":        uiPriorityClass,
	"priorityLabel":        uiPriorityLabel,
	"priorityOptions":      uiPriorityOptions,
	"projectPanel":         uiProjectPanelPath,
	"projectView":          uiProjectViewPath,
	"projectIcon":          uiProjectIcon,
	"sprintDate":           uiSprintDate,
	"statusClass":          uiStatusClass,
	"statusLabel":          uiStatusLabel,
	"statusOptions":        uiStatusOptions,
	"statusRow":            uiStatusRowClass,
	"statusSurface":        uiStatusSurfaceClass,
	"tokenTime":            uiTokenTime,
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
	User              model.User
	Projects          []model.Project
	CurrentProjectID  uuid.UUID
	CurrentView       string
	WorkPanel         *uiWorkPanelData
	ProjectsPanel     *uiProjectsPanelData
	ProjectPanel      *uiProjectPanelData
	DeletedPanel      *uiDeletedIssuesPanelData
	DeletedIssuePanel *uiDeletedIssuePanelData
	IssuePanel        *uiIssuePanelData
	TokenPanel        *uiTokenPanelData
	SettingsPanel     *uiSettingsPanelData
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

type uiAssigneeFilterItem struct {
	Assignee model.ProjectAssignee
	Selected bool
	Href     string
	HXGet    string
	HXPush   string
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

type uiIssueDeleteNotice struct {
	Issue model.Issue
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
	Project              model.Project
	View                 string
	ProjectTabs          uiTabBarData
	AssigneeFilters      []uiAssigneeFilterItem
	AssigneeFilterActive bool
	ClearAssigneeHref    string
	ClearAssigneeHXGet   string
	ClearAssigneeHXPush  string
	ActiveSprint         *model.Sprint
	SprintColumns        []uiIssueColumn
	PlannedSprints       []uiPlannedSprint
	BacklogIssues        []model.Issue
	DeleteNotice         *uiIssueDeleteNotice
	SprintIssuesHasMore  bool
	PlannedHasMore       bool
	BacklogHasMore       bool
}

type uiDeletedIssuesPanelData struct {
	Project model.Project
	Issues  []model.Issue
	HasMore bool
}

type uiDeletedIssuePanelData struct {
	Issue     model.Issue
	Project   model.Project
	BackHref  string
	BackHXGet string
}

type uiIssuePanelData struct {
	Issue            model.Issue
	Project          model.Project
	ParentIssue      *model.Issue
	Sprint           *model.Sprint
	Assignee         *model.User
	Reporter         *model.User
	EditDescription  bool
	EditStatus       bool
	EditPriority     bool
	EditAssignee     bool
	EditReporter     bool
	AssigneeInput    string
	ReporterInput    string
	AssigneeError    string
	ReporterError    string
	MemberOptions    []model.User
	SubIssues        []model.Issue
	SubIssuesHasMore bool
	SubIssueTitle    string
	SubIssueError    string
	Comments         []uiIssueCommentItem
	CommentsHasMore  bool
	CommentBody      string
	CommentError     string
	Links            []uiIssueLinkItem
	LinksHasMore     bool
	AddLink          bool
	EditLinkID       uuid.UUID
	LinkTarget       string
	LinkRelation     string
	LinkError        string
	BackHref         string
	BackHXGet        string
	BackLabel        string
	DeleteNotice     *uiIssueDeleteNotice
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
		r.Get("/{owner}/issues/{issueRef}", s.uiIssuePage)
		r.Get("/{owner}/issues/{issueRef}/panel", s.uiIssuePanel)
		r.Post("/{owner}/issues/{issueRef}/delete", s.uiDeleteIssue)
		r.Post("/{owner}/issues/{issueRef}/restore", s.uiRestoreIssue)
		r.Get("/{owner}/issues/{issueRef}/description/edit", s.uiEditIssueDescription)
		r.Post("/{owner}/issues/{issueRef}/description", s.uiUpdateIssueDescription)
		r.Get("/{owner}/issues/{issueRef}/status/edit", s.uiEditIssueStatus)
		r.Post("/{owner}/issues/{issueRef}/status", s.uiUpdateIssueStatus)
		r.Get("/{owner}/issues/{issueRef}/priority/edit", s.uiEditIssuePriority)
		r.Post("/{owner}/issues/{issueRef}/priority", s.uiUpdateIssuePriority)
		r.Get("/{owner}/issues/{issueRef}/assignee/edit", s.uiEditIssueAssignee)
		r.Post("/{owner}/issues/{issueRef}/assignee", s.uiUpdateIssueAssignee)
		r.Get("/{owner}/issues/{issueRef}/reporter/edit", s.uiEditIssueReporter)
		r.Post("/{owner}/issues/{issueRef}/reporter", s.uiUpdateIssueReporter)
		r.Get("/{owner}/issues/{issueRef}/links/new", s.uiNewIssueLink)
		r.Post("/{owner}/issues/{issueRef}/links", s.uiCreateIssueLink)
		r.Get("/{owner}/issues/{issueRef}/links/{linkRef}/edit", s.uiEditIssueLink)
		r.Post("/{owner}/issues/{issueRef}/links/{linkRef}", s.uiUpdateIssueLink)
		r.Post("/{owner}/issues/{issueRef}/links/{linkRef}/delete", s.uiDeleteIssueLink)
		r.Post("/{owner}/issues/{issueRef}/sub-issues", s.uiCreateSubIssue)
		r.Post("/{owner}/issues/{issueRef}/comments", s.uiCreateComment)
		r.Get("/{owner}/projects/{key}", s.uiProjectPage)
		r.Get("/{owner}/projects/{key}/about", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPage(w, r, "about") })
		r.Get("/{owner}/projects/{key}/about/panel", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPanel(w, r, "about") })
		r.Get("/{owner}/projects/{key}/sprint", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPage(w, r, "sprint") })
		r.Get("/{owner}/projects/{key}/sprint/panel", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPanel(w, r, "sprint") })
		r.Get("/{owner}/projects/{key}/backlog", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPage(w, r, "backlog") })
		r.Get("/{owner}/projects/{key}/backlog/panel", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPanel(w, r, "backlog") })
		r.Get("/{owner}/projects/{key}/deleted", s.uiProjectDeletedPage)
		r.Get("/{owner}/projects/{key}/deleted/panel", s.uiProjectDeletedPanel)
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
	http.Redirect(w, r, uiProjectViewPath(projects[0], "sprint"), http.StatusSeeOther)
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
	http.Redirect(w, r, uiProjectViewPath(project, "sprint"), http.StatusSeeOther)
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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	panel, err := s.uiBuildProjectPanel(r.Context(), r, project.ID, view)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "shell", uiShellData{
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

func (s *Server) uiProjectDeletedPage(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	panel, err := s.uiBuildDeletedIssuesPanel(r.Context(), r, project.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "shell", uiShellData{
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

func (s *Server) uiIssuePage(w http.ResponseWriter, r *http.Request) {
	issue, deleted, ok := s.uiIssueFromRouteIncludingDeleted(w, r)
	if !ok {
		return
	}
	if deleted {
		panel, err := s.uiBuildDeletedIssuePanel(r.Context(), r, issue)
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
			User:              currentUser(r),
			Projects:          projects,
			CurrentProjectID:  panel.Project.ID,
			CurrentView:       "projects",
			DeletedIssuePanel: panel,
		})
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
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
	issue, deleted, ok := s.uiIssueFromRouteIncludingDeleted(w, r)
	if !ok {
		return
	}
	if deleted {
		panel, err := s.uiBuildDeletedIssuePanel(r.Context(), r, issue)
		if err != nil {
			writeUIStoreError(w, err)
			return
		}
		renderUITemplate(w, http.StatusOK, "deleted-issue-panel", panel)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiDeleteIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if err := s.store.DeleteIssue(r.Context(), issue.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	backHref := uiAppendDeletedIssueQuery(panel.BackHref, issue.Identifier)
	if !isHTMXRequest(r) {
		http.Redirect(w, r, backHref, http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", backHref)
	s.renderUIIssueBackTarget(w, r, panel, &uiIssueDeleteNotice{Issue: issue})
}

func (s *Server) uiRestoreIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiDeletedIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	restored, err := s.store.RestoreIssue(r.Context(), issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if !isHTMXRequest(r) {
		http.Redirect(w, r, uiIssuePath(restored), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", uiIssuePath(restored))
	panel, err := s.uiBuildIssuePanel(r.Context(), r, restored.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueDescription(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditDescription = true
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueDescription(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	description := r.Form.Get("description")
	if strings.TrimSpace(description) == "" {
		description = ""
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		Description: &description,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueStatus(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditStatus = true
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueStatus(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	status := model.Status(strings.TrimSpace(r.Form.Get("status")))
	if !status.Valid() {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		Status: &status,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssuePriority(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditPriority = true
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssuePriority(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	priority := model.IssuePriority(strings.TrimSpace(r.Form.Get("priority")))
	if !priority.Valid() {
		http.Error(w, "invalid priority", http.StatusBadRequest)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		Priority: &priority,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueAssignee(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditAssignee = true
	panel.AssigneeInput = uiIssueUserInput(panel.Assignee)
	if err := s.uiPopulateIssueMemberOptions(r.Context(), panel); err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueAssignee(w http.ResponseWriter, r *http.Request) {
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
	input := r.Form.Get("assignee")
	assigneeID, clear, message, err := s.uiIssuePersonID(r.Context(), issue.ProjectID, input)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIIssuePanelWithAssigneeError(w, r, issue.ID, input, message)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		AssigneeID:    assigneeID,
		ClearAssignee: clear,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueReporter(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditReporter = true
	panel.ReporterInput = uiIssueUserInput(panel.Reporter)
	if err := s.uiPopulateIssueMemberOptions(r.Context(), panel); err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueReporter(w http.ResponseWriter, r *http.Request) {
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
	input := r.Form.Get("reporter")
	reporterID, clear, message, err := s.uiIssuePersonID(r.Context(), issue.ProjectID, input)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIIssuePanelWithReporterError(w, r, issue.ID, input, message)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		ReporterID:    reporterID,
		ClearReporter: clear,
	})
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiNewIssueLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.AddLink = true
	panel.LinkRelation = string(model.LinkTypeRelatesTo)
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiCreateIssueLink(w http.ResponseWriter, r *http.Request) {
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
	target := strings.TrimSpace(r.Form.Get("target_issue"))
	relation := strings.TrimSpace(r.Form.Get("relation"))
	params, message, err := s.uiIssueLinkFormParams(r.Context(), issue, target, relation)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIIssuePanelWithLinkError(w, r, issue.ID, uuid.Nil, target, relation, message)
		return
	}
	if _, err := s.store.CreateIssueLink(r.Context(), store.CreateIssueLinkParams(params)); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIIssuePanelWithLinkError(w, r, issue.ID, uuid.Nil, target, relation, "Link already exists or cannot be created.")
			return
		}
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiEditIssueLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	link, ok := s.uiIssueLinkFromRoute(w, r, issue)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditLinkID = link.ID
	panel.LinkRelation = uiIssueLinkRelation(link, issue.ID)
	panel.LinkTarget = s.uiIssueLinkTargetIdentifier(r.Context(), issue.ID, link)
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueLink(w http.ResponseWriter, r *http.Request) {
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
	link, ok := s.uiIssueLinkFromRoute(w, r, issue)
	if !ok {
		return
	}
	target := strings.TrimSpace(r.Form.Get("target_issue"))
	relation := strings.TrimSpace(r.Form.Get("relation"))
	params, message, err := s.uiIssueLinkFormParams(r.Context(), issue, target, relation)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIIssuePanelWithLinkError(w, r, issue.ID, link.ID, target, relation, message)
		return
	}
	if _, err := s.store.UpdateIssueLink(r.Context(), link.ID, params); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIIssuePanelWithLinkError(w, r, issue.ID, link.ID, target, relation, "Link already exists or cannot be updated.")
			return
		}
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiDeleteIssueLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	link, ok := s.uiIssueLinkFromRoute(w, r, issue)
	if !ok {
		return
	}
	if err := s.store.DeleteIssueLink(r.Context(), link.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiCreateSubIssue(w http.ResponseWriter, r *http.Request) {
	parent, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.Form.Get("title"))
	if title == "" || len(title) > 200 {
		s.renderUIIssuePanelWithSubIssueError(w, r, parent.ID, r.Form.Get("title"), "Title required, max 200 chars.")
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), parent.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	reporterID := currentUser(r).ID
	if _, err := s.store.CreateSubIssue(r.Context(), store.CreateSubIssueParams{
		ParentIssueID: parent.ID,
		Title:         title,
		Priority:      model.PriorityP2,
		ReporterID:    &reporterID,
	}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIIssuePanelWithSubIssueError(w, r, parent.ID, r.Form.Get("title"), "Sub-issue could not be created for this issue.")
			return
		}
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, parent.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiCreateComment(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	body := strings.TrimSpace(r.Form.Get("body"))
	if body == "" || len(body) > 10000 {
		s.renderUIIssuePanelWithCommentError(w, r, issue.ID, r.Form.Get("body"), "Comment required, max 10000 chars.")
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	if _, err := s.store.CreateComment(r.Context(), store.CreateCommentParams{
		IssueID:  issue.ID,
		AuthorID: currentUser(r).ID,
		Body:     body,
	}); err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) renderUIIssuePanelWithSubIssueError(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, title, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.SubIssueTitle = title
	panel.SubIssueError = message
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

func (s *Server) renderUIIssuePanelWithAssigneeError(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, input, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditAssignee = true
	panel.AssigneeInput = input
	panel.AssigneeError = message
	if err := s.uiPopulateIssueMemberOptions(r.Context(), panel); err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) renderUIIssuePanelWithReporterError(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, input, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditReporter = true
	panel.ReporterInput = input
	panel.ReporterError = message
	if err := s.uiPopulateIssueMemberOptions(r.Context(), panel); err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiPopulateIssueMemberOptions(ctx context.Context, panel *uiIssuePanelData) error {
	users, err := s.store.SearchProjectMembers(ctx, store.SearchProjectMembersParams{
		ProjectID: panel.Project.ID,
		Limit:     MaxLimit,
	})
	if err != nil {
		return err
	}
	panel.MemberOptions = users
	return nil
}

func (s *Server) uiIssuePersonID(ctx context.Context, projectID uuid.UUID, raw string) (*uuid.UUID, bool, string, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return nil, true, "", nil
	}
	username, err := store.NormalizeUsername(strings.TrimPrefix(input, "@"))
	if err != nil {
		return nil, false, "Choose a project member.", nil
	}
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, "Choose a project member.", nil
		}
		return nil, false, "", err
	}
	users, err := s.store.SearchProjectMembers(ctx, store.SearchProjectMembersParams{
		ProjectID: projectID,
		Query:     username,
		Limit:     MaxLimit,
	})
	if err != nil {
		return nil, false, "", err
	}
	for _, member := range users {
		if member.ID == user.ID {
			return &user.ID, false, "", nil
		}
	}
	return nil, false, "Choose a project member.", nil
}

func (s *Server) renderUIIssuePanelWithLinkError(w http.ResponseWriter, r *http.Request, issueID, editLinkID uuid.UUID, target, relation, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.AddLink = editLinkID == uuid.Nil
	panel.EditLinkID = editLinkID
	panel.LinkTarget = target
	panel.LinkRelation = relation
	panel.LinkError = message
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
		issues, more, err := s.store.ListIssues(ctx, store.ListIssuesParams{
			ProjectID:        project.ID,
			Limit:            MaxLimit,
			IncludeSubIssues: true,
		})
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
	deleteNotice, err := s.uiDeletedIssueNotice(ctx, r, project.OwnerUsername, projectID)
	if err != nil {
		return nil, err
	}
	assigneeIDs, err := uiParseAssigneeFilter(r)
	if err != nil {
		return nil, err
	}

	panel := &uiProjectPanelData{
		Project:              project,
		View:                 view,
		ProjectTabs:          uiProjectTabs(project, view, assigneeIDs),
		AssigneeFilterActive: len(assigneeIDs) > 0,
		ClearAssigneeHref:    uiProjectViewPath(project, view),
		ClearAssigneeHXGet:   uiProjectPanelPath(project, view),
		ClearAssigneeHXPush:  uiProjectViewPath(project, view),
		DeleteNotice:         deleteNotice,
	}
	assignees, err := s.store.ListProjectAssignees(ctx, projectID)
	if err != nil {
		return nil, err
	}
	panel.AssigneeFilters = uiProjectAssigneeFilters(project, view, assignees, assigneeIDs)

	switch view {
	case "about":
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
		if len(activeSprints) == 0 {
			return panel, nil
		}
		panel.ActiveSprint = &activeSprints[0]
		sprintIssues, sprintHasMore, err := s.store.ListIssues(ctx, store.ListIssuesParams{
			ProjectID:   projectID,
			AssigneeIDs: assigneeIDs,
			SprintID:    &panel.ActiveSprint.ID,
			Limit:       MaxLimit,
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
				ProjectID:   projectID,
				AssigneeIDs: assigneeIDs,
				SprintID:    &sprint.ID,
				Limit:       MaxLimit,
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
			ProjectID:   projectID,
			AssigneeIDs: assigneeIDs,
			Backlog:     true,
			Limit:       MaxLimit,
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
	deleteNotice, err := s.uiDeletedIssueNotice(ctx, r, project.OwnerUsername, projectID)
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
	var parentIssue *model.Issue
	if issue.ParentIssueID != nil {
		parent, err := s.store.GetIssue(ctx, *issue.ParentIssueID)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
		} else {
			parentIssue = &parent
		}
	}
	var sprint *model.Sprint
	if issue.ParentIssueID == nil {
		sprint, err = s.uiOptionalSprint(ctx, issue.SprintID)
		if err != nil {
			return nil, err
		}
	}
	var subIssues []model.Issue
	var subIssuesHasMore bool
	if issue.ParentIssueID == nil {
		subIssues, subIssuesHasMore, err = s.store.ListSubIssuesForIssue(ctx, store.ListSubIssuesForIssueParams{
			ParentIssueID: issueID,
			Limit:         MaxLimit,
		})
		if err != nil {
			return nil, err
		}
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

	backHref, backHXGet, backLabel := uiIssueBackLink(project, issue, parentIssue, sprint)
	return &uiIssuePanelData{
		Issue:            issue,
		Project:          project,
		ParentIssue:      parentIssue,
		Sprint:           sprint,
		Assignee:         assignee,
		Reporter:         reporter,
		SubIssues:        subIssues,
		SubIssuesHasMore: subIssuesHasMore,
		Comments:         commentItems,
		CommentsHasMore:  commentsHasMore,
		Links:            linkItems,
		LinksHasMore:     linksHasMore,
		BackHref:         backHref,
		BackHXGet:        backHXGet,
		BackLabel:        backLabel,
		DeleteNotice:     deleteNotice,
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

func uiIssueBackLink(project model.Project, issue model.Issue, parent *model.Issue, sprint *model.Sprint) (href, hxGet, label string) {
	if parent != nil {
		base := uiIssuePath(*parent)
		return base, base + "/panel", "Parent issue"
	}
	view := "sprint"
	if issue.SprintID == nil || (sprint != nil && sprint.Status == model.SprintStatusPlanned) {
		view = "backlog"
	}
	base := uiProjectViewPath(project, view)
	label = "Sprint"
	if view == "backlog" {
		label = "Backlog"
	}
	return base, base + "/panel", label
}

func (s *Server) renderUIIssueBackTarget(w http.ResponseWriter, r *http.Request, panel *uiIssuePanelData, notice *uiIssueDeleteNotice) {
	if panel.ParentIssue != nil {
		parentPanel, err := s.uiBuildIssuePanel(r.Context(), r, panel.ParentIssue.ID)
		if err != nil {
			writeUIStoreError(w, err)
			return
		}
		parentPanel.DeleteNotice = notice
		renderUITemplate(w, http.StatusOK, "issue-panel", parentPanel)
		return
	}
	view := "sprint"
	if panel.Issue.SprintID == nil || (panel.Sprint != nil && panel.Sprint.Status == model.SprintStatusPlanned) {
		view = "backlog"
	}
	projectPanel, err := s.uiBuildProjectPanel(r.Context(), r, panel.Project.ID, view)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	projectPanel.DeleteNotice = notice
	renderUITemplate(w, http.StatusOK, "project-panel", projectPanel)
}

func uiProjectTabs(project model.Project, view string, assigneeIDs []uuid.UUID) uiTabBarData {
	return uiTabBarData{
		Label: "Project views",
		Items: []uiTabItem{
			{
				Label:     "Sprints",
				Icon:      "kanban",
				Href:      uiProjectViewPath(project, "sprint", assigneeIDs),
				HXGet:     uiProjectPanelPath(project, "sprint", assigneeIDs),
				HXTarget:  "#main",
				HXPushURL: uiProjectViewPath(project, "sprint", assigneeIDs),
				Active:    view == "sprint",
			},
			{
				Label:     "Backlog",
				Icon:      "inbox",
				Href:      uiProjectViewPath(project, "backlog", assigneeIDs),
				HXGet:     uiProjectPanelPath(project, "backlog", assigneeIDs),
				HXTarget:  "#main",
				HXPushURL: uiProjectViewPath(project, "backlog", assigneeIDs),
				Active:    view == "backlog",
			},
			{
				Label:     "About",
				Icon:      "info",
				Href:      uiProjectViewPath(project, "about", assigneeIDs),
				HXGet:     uiProjectPanelPath(project, "about", assigneeIDs),
				HXTarget:  "#main",
				HXPushURL: uiProjectViewPath(project, "about", assigneeIDs),
				Active:    view == "about",
			},
		},
	}
}

func uiParseAssigneeFilter(r *http.Request) ([]uuid.UUID, error) {
	raws := r.URL.Query()["assignee_id"]
	ids := make([]uuid.UUID, 0, len(raws))
	seen := make(map[uuid.UUID]struct{}, len(raws))
	for _, raw := range raws {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("invalid assignee_id: %w", errUIBadRequest)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func uiProjectAssigneeFilters(project model.Project, view string, assignees []model.ProjectAssignee, selectedIDs []uuid.UUID) []uiAssigneeFilterItem {
	out := make([]uiAssigneeFilterItem, 0, len(assignees))
	for _, assignee := range assignees {
		nextIDs, selected := uiToggleAssigneeIDs(selectedIDs, assignee.ID)
		out = append(out, uiAssigneeFilterItem{
			Assignee: assignee,
			Selected: selected,
			Href:     uiProjectViewPath(project, view, nextIDs),
			HXGet:    uiProjectPanelPath(project, view, nextIDs),
			HXPush:   uiProjectViewPath(project, view, nextIDs),
		})
	}
	return out
}

func uiToggleAssigneeIDs(ids []uuid.UUID, id uuid.UUID) ([]uuid.UUID, bool) {
	selected := false
	for _, current := range ids {
		if current == id {
			selected = true
			break
		}
	}
	if !selected {
		out := make([]uuid.UUID, 0, len(ids)+1)
		out = append(out, ids...)
		out = append(out, id)
		return out, false
	}
	out := make([]uuid.UUID, 0, len(ids)-1)
	for _, current := range ids {
		if current != id {
			out = append(out, current)
		}
	}
	return out, true
}

func uiAppendAssigneeQuery(path string, assigneeIDs []uuid.UUID) string {
	if len(assigneeIDs) == 0 {
		return path
	}
	values := url.Values{}
	for _, id := range assigneeIDs {
		values.Add("assignee_id", id.String())
	}
	return path + "?" + values.Encode()
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

func (s *Server) uiProjectFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, bool) {
	owner, err := store.NormalizeUsername(chi.URLParam(r, "owner"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return model.Project{}, false
	}
	key := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "key")))
	if !projectKeyRe.MatchString(key) {
		http.Error(w, "invalid project key", http.StatusBadRequest)
		return model.Project{}, false
	}
	project, err := s.store.GetProjectByOwnerKey(r.Context(), owner, key)
	if err != nil {
		writeUIStoreError(w, err)
		return model.Project{}, false
	}
	return project, true
}

func (s *Server) uiIssueFromRoute(w http.ResponseWriter, r *http.Request) (model.Issue, bool) {
	owner, ref, ok := uiIssueRouteOwnerRef(w, r)
	if !ok {
		return model.Issue{}, false
	}
	issue, err := s.store.GetIssueByOwnerKeyNumber(r.Context(), owner, ref.ProjectKey, ref.Number)
	if err != nil {
		writeUIStoreError(w, err)
		return model.Issue{}, false
	}
	return issue, true
}

func (s *Server) uiDeletedIssueFromRoute(w http.ResponseWriter, r *http.Request) (model.Issue, bool) {
	owner, ref, ok := uiIssueRouteOwnerRef(w, r)
	if !ok {
		return model.Issue{}, false
	}
	issue, err := s.store.GetDeletedIssueByOwnerKeyNumber(r.Context(), owner, ref.ProjectKey, ref.Number)
	if err != nil {
		writeUIStoreError(w, err)
		return model.Issue{}, false
	}
	return issue, true
}

func (s *Server) uiIssueFromRouteIncludingDeleted(w http.ResponseWriter, r *http.Request) (model.Issue, bool, bool) {
	owner, ref, ok := uiIssueRouteOwnerRef(w, r)
	if !ok {
		return model.Issue{}, false, false
	}
	issue, err := s.store.GetIssueByOwnerKeyNumber(r.Context(), owner, ref.ProjectKey, ref.Number)
	if err == nil {
		return issue, false, true
	}
	if !errors.Is(err, store.ErrNotFound) {
		writeUIStoreError(w, err)
		return model.Issue{}, false, false
	}
	issue, err = s.store.GetDeletedIssueByOwnerKeyNumber(r.Context(), owner, ref.ProjectKey, ref.Number)
	if err != nil {
		writeUIStoreError(w, err)
		return model.Issue{}, false, false
	}
	return issue, true, true
}

func uiIssueRouteOwnerRef(w http.ResponseWriter, r *http.Request) (string, issueRef, bool) {
	owner, err := store.NormalizeUsername(chi.URLParam(r, "owner"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return "", issueRef{}, false
	}
	ref, err := parseIssueRef(chi.URLParam(r, "issueRef"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return "", issueRef{}, false
	}
	return owner, ref, true
}

func (s *Server) uiDeletedIssueNotice(ctx context.Context, r *http.Request, owner string, projectID uuid.UUID) (*uiIssueDeleteNotice, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("deleted_issue"))
	if raw == "" {
		return nil, nil
	}
	ref, err := parseIssueRef(raw)
	if err != nil {
		return nil, nil
	}
	issue, err := s.store.GetDeletedIssueByOwnerKeyNumber(ctx, owner, ref.ProjectKey, ref.Number)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if issue.ProjectID != projectID {
		return nil, nil
	}
	return &uiIssueDeleteNotice{Issue: issue}, nil
}

func (s *Server) uiIssueLinkFromRoute(w http.ResponseWriter, r *http.Request, issue model.Issue) (model.IssueLink, bool) {
	number, err := parseTypedRef(chi.URLParam(r, "linkRef"), "link")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return model.IssueLink{}, false
	}
	link, err := s.store.GetIssueLinkByProjectNumber(r.Context(), issue.ProjectID, number)
	if err != nil {
		writeUIStoreError(w, err)
		return model.IssueLink{}, false
	}
	if link.SourceID != issue.ID && link.TargetID != issue.ID {
		writeUIStoreError(w, store.ErrNotFound)
		return model.IssueLink{}, false
	}
	return link, true
}

func (s *Server) uiIssueLinkFormParams(ctx context.Context, issue model.Issue, targetRaw, relation string) (store.UpdateIssueLinkParams, string, error) {
	if targetRaw == "" {
		return store.UpdateIssueLinkParams{}, "Linked issue required.", nil
	}
	targetRef, err := parseIssueRef(targetRaw)
	if err != nil {
		return store.UpdateIssueLinkParams{}, err.Error(), nil
	}
	target, err := s.store.GetIssueByOwnerKeyNumber(ctx, issue.OwnerUsername, targetRef.ProjectKey, targetRef.Number)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.UpdateIssueLinkParams{}, "Linked issue not found.", nil
		}
		return store.UpdateIssueLinkParams{}, "", err
	}
	if target.ProjectID != issue.ProjectID {
		return store.UpdateIssueLinkParams{}, "Linked issue must be in this project.", nil
	}
	if target.ID == issue.ID {
		return store.UpdateIssueLinkParams{}, "Choose a different issue.", nil
	}
	sourceID, targetID, linkType, ok := uiIssueLinkRelationParams(issue.ID, target.ID, relation)
	if !ok {
		return store.UpdateIssueLinkParams{}, "Choose a valid relationship.", nil
	}
	return store.UpdateIssueLinkParams{SourceID: sourceID, TargetID: targetID, LinkType: linkType}, "", nil
}

func (s *Server) uiIssueLinkTargetIdentifier(ctx context.Context, issueID uuid.UUID, link model.IssueLink) string {
	otherID := link.SourceID
	if otherID == issueID {
		otherID = link.TargetID
	}
	linked, err := s.store.GetIssue(ctx, otherID)
	if err != nil {
		return ""
	}
	return linked.Identifier
}

func uiProjectPath(project model.Project) string {
	return "/" + project.OwnerUsername + "/projects/" + project.Key
}

func uiProjectViewPath(project model.Project, view string, assigneeIDs ...[]uuid.UUID) string {
	ids := []uuid.UUID(nil)
	if len(assigneeIDs) > 0 {
		ids = assigneeIDs[0]
	}
	return uiAppendAssigneeQuery(uiProjectPath(project)+"/"+view, ids)
}

func uiProjectPanelPath(project model.Project, view string, assigneeIDs ...[]uuid.UUID) string {
	ids := []uuid.UUID(nil)
	if len(assigneeIDs) > 0 {
		ids = assigneeIDs[0]
	}
	return uiAppendAssigneeQuery(uiProjectPath(project)+"/"+view+"/panel", ids)
}

func uiIssuePath(v any) string {
	issue := uiIssueValue(v)
	return "/" + issue.OwnerUsername + "/issues/" + issue.Identifier
}

func uiIssuePanelPath(issue any) string {
	return uiIssuePath(issue) + "/panel"
}

func uiIssueDeletePath(issue any) string {
	return uiIssuePath(issue) + "/delete"
}

func uiIssueRestorePath(issue any) string {
	return uiIssuePath(issue) + "/restore"
}

func uiIssueCommentsPath(issue any) string {
	return uiIssuePath(issue) + "/comments"
}

func uiIssueDescriptionPath(issue any) string {
	return uiIssuePath(issue) + "/description"
}

func uiIssueDescriptionEditPath(issue any) string {
	return uiIssueDescriptionPath(issue) + "/edit"
}

func uiIssueStatusPath(issue any) string {
	return uiIssuePath(issue) + "/status"
}

func uiIssueStatusEditPath(issue any) string {
	return uiIssueStatusPath(issue) + "/edit"
}

func uiIssuePriorityPath(issue any) string {
	return uiIssuePath(issue) + "/priority"
}

func uiIssuePriorityEditPath(issue any) string {
	return uiIssuePriorityPath(issue) + "/edit"
}

func uiIssueAssigneePath(issue any) string {
	return uiIssuePath(issue) + "/assignee"
}

func uiIssueAssigneeEditPath(issue any) string {
	return uiIssueAssigneePath(issue) + "/edit"
}

func uiIssueReporterPath(issue any) string {
	return uiIssuePath(issue) + "/reporter"
}

func uiIssueReporterEditPath(issue any) string {
	return uiIssueReporterPath(issue) + "/edit"
}

func uiIssueLinksPath(issue any) string {
	return uiIssuePath(issue) + "/links"
}

func uiIssueLinkNewPath(issue any) string {
	return uiIssueLinksPath(issue) + "/new"
}

func uiIssueLinkPath(issue any, link any) string {
	return uiIssueLinksPath(issue) + "/" + uiIssueLinkRef(link)
}

func uiIssueLinkEditPath(issue any, link any) string {
	return uiIssueLinkPath(issue, link) + "/edit"
}

func uiIssueLinkDeletePath(issue any, link any) string {
	return uiIssueLinkPath(issue, link) + "/delete"
}

func uiIssueSubIssuesPath(issue any) string {
	return uiIssuePath(issue) + "/sub-issues"
}

func uiIssueValue(v any) model.Issue {
	switch issue := v.(type) {
	case model.Issue:
		return issue
	case *model.Issue:
		if issue != nil {
			return *issue
		}
	}
	return model.Issue{}
}

func uiIssueLinkRef(v any) string {
	var link model.IssueLink
	switch l := v.(type) {
	case model.IssueLink:
		link = l
	case *model.IssueLink:
		if l != nil {
			link = *l
		}
	}
	if link.Ref != "" {
		return link.Ref
	}
	if link.Number > 0 {
		return model.IssueLinkRef(link.Number)
	}
	return "link-0"
}

func redirectUILogin(w http.ResponseWriter, r *http.Request) {
	next := url.QueryEscape(safeUINext(r.URL.RequestURI()))
	http.Redirect(w, r, "/login?next="+next, http.StatusSeeOther)
}

func isHTMXRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("HX-Request"), "true")
}

func uiAppendDeletedIssueQuery(path, issueRef string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "deleted_issue=" + url.QueryEscape(issueRef)
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

func uiIssueUserInput(user *model.User) string {
	if user == nil {
		return ""
	}
	return "@" + user.Username
}

func safeUIProjectPath(path string) bool {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 3 && len(parts) != 4 && len(parts) != 5 {
		return false
	}
	if _, err := store.NormalizeUsername(parts[0]); err != nil {
		return false
	}
	if parts[1] != "projects" {
		return false
	}
	key := strings.ToUpper(parts[2])
	if !projectKeyRe.MatchString(key) {
		return false
	}
	if len(parts) == 3 {
		return true
	}
	if parts[3] != "about" && parts[3] != "sprint" && parts[3] != "backlog" && parts[3] != "deleted" {
		return false
	}
	if len(parts) == 4 {
		return true
	}
	return parts[4] == "panel"
}

func safeUIIssuePath(path string) bool {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) < 3 || len(parts) > 6 {
		return false
	}
	if _, err := store.NormalizeUsername(parts[0]); err != nil {
		return false
	}
	if parts[1] != "issues" {
		return false
	}
	if _, err := parseIssueRef(parts[2]); err != nil {
		return false
	}
	if len(parts) == 3 {
		return true
	}
	if len(parts) == 4 {
		return parts[3] == "panel" || parts[3] == "links" || parts[3] == "delete" || parts[3] == "restore"
	}
	if len(parts) == 5 {
		return ((parts[3] == "description" || parts[3] == "status" || parts[3] == "priority" || parts[3] == "assignee" || parts[3] == "reporter") && parts[4] == "edit") ||
			(parts[3] == "links" && parts[4] == "new")
	}
	if parts[3] != "links" || parts[5] != "edit" {
		return false
	}
	_, err := parseTypedRef(parts[4], "link")
	return err == nil
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
	case errors.Is(err, errUIBadRequest):
		http.Error(w, "bad request", http.StatusBadRequest)
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

type uiStatusOption struct {
	Status model.Status
	Label  string
}

func uiStatusOptions() []uiStatusOption {
	return []uiStatusOption{
		{Status: model.StatusTodo, Label: uiStatusLabel(model.StatusTodo)},
		{Status: model.StatusInProgress, Label: uiStatusLabel(model.StatusInProgress)},
		{Status: model.StatusDone, Label: uiStatusLabel(model.StatusDone)},
	}
}

func uiPriorityClass(priority model.IssuePriority) string {
	switch priority {
	case model.PriorityP0:
		return "bg-red-600"
	case model.PriorityP1:
		return "bg-orange-500"
	case model.PriorityP2:
		return "bg-amber-500"
	case model.PriorityP3:
		return "bg-yellow-500"
	case model.PriorityP4:
		return "bg-gray-500"
	case "":
		return "bg-amber-500"
	default:
		return "bg-gray-500"
	}
}

func uiPriorityLabel(priority model.IssuePriority) string {
	if priority == "" {
		return string(model.PriorityP2)
	}
	return string(priority)
}

type uiPriorityOption struct {
	Priority model.IssuePriority
}

func uiPriorityOptions() []uiPriorityOption {
	return []uiPriorityOption{
		{Priority: model.PriorityP0},
		{Priority: model.PriorityP1},
		{Priority: model.PriorityP2},
		{Priority: model.PriorityP3},
		{Priority: model.PriorityP4},
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

type uiIssueLinkOption struct {
	Value string
	Label string
}

func uiIssueLinkOptions() []uiIssueLinkOption {
	return []uiIssueLinkOption{
		{Value: string(model.LinkTypeRelatesTo), Label: "Relates to"},
		{Value: string(model.LinkTypeBlocks), Label: "Blocks"},
		{Value: "blocked_by", Label: "Blocked by"},
		{Value: string(model.LinkTypeDuplicates), Label: "Duplicates"},
		{Value: "duplicated_by", Label: "Duplicated by"},
		{Value: string(model.LinkTypeClones), Label: "Clones"},
		{Value: "cloned_by", Label: "Cloned by"},
	}
}

func uiIssueLinkRelation(link model.IssueLink, issueID uuid.UUID) string {
	if link.SourceID == issueID {
		return string(link.LinkType)
	}
	switch link.LinkType {
	case model.LinkTypeBlocks:
		return "blocked_by"
	case model.LinkTypeDuplicates:
		return "duplicated_by"
	case model.LinkTypeClones:
		return "cloned_by"
	default:
		return string(link.LinkType)
	}
}

func uiIssueLinkRelationParams(issueID, otherID uuid.UUID, relation string) (uuid.UUID, uuid.UUID, model.LinkType, bool) {
	switch model.LinkType(relation) {
	case model.LinkTypeRelatesTo:
		return issueID, otherID, model.LinkTypeRelatesTo, true
	case model.LinkTypeBlocks:
		return issueID, otherID, model.LinkTypeBlocks, true
	case model.LinkTypeDuplicates:
		return issueID, otherID, model.LinkTypeDuplicates, true
	case model.LinkTypeClones:
		return issueID, otherID, model.LinkTypeClones, true
	}
	switch relation {
	case "blocked_by":
		return otherID, issueID, model.LinkTypeBlocks, true
	case "duplicated_by":
		return otherID, issueID, model.LinkTypeDuplicates, true
	case "cloned_by":
		return otherID, issueID, model.LinkTypeClones, true
	default:
		return uuid.Nil, uuid.Nil, "", false
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
