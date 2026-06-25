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
	"initials":                  uiInitials,
	"issueAssignee":             uiIssueAssigneePath,
	"issueAssigneeEdit":         uiIssueAssigneeEditPath,
	"issueComment":              uiIssueCommentPath,
	"issueCommentEdit":          uiIssueCommentEditPath,
	"issueComments":             uiIssueCommentsPath,
	"issueContext":              uiIssueContextPath,
	"issueContextDelete":        uiIssueContextDeletePath,
	"issueContextLinkNew":       uiIssueContextLinkNewPath,
	"issueContextModal":         uiIssueContextModal,
	"issueContextNew":           uiIssueContextNewPath,
	"issueCloseReason":          uiIssueCloseReasonPath,
	"issueCloseReasonEdit":      uiIssueCloseReasonEditPath,
	"issueDelete":               uiIssueDeletePath,
	"issueDescription":          uiIssueDescriptionPath,
	"issueDescriptionEdit":      uiIssueDescriptionEditPath,
	"issueHref":                 uiIssuePath,
	"issueLink":                 uiIssueLinkPath,
	"issueLinkDelete":           uiIssueLinkDeletePath,
	"issueLinkEdit":             uiIssueLinkEditPath,
	"issueLinkNew":              uiIssueLinkNewPath,
	"issueLinks":                uiIssueLinksPath,
	"issuePanel":                uiIssuePanelPath,
	"issuePriority":             uiIssuePriorityPath,
	"issuePriorityEdit":         uiIssuePriorityEditPath,
	"issueDueDate":              uiIssueDueDatePath,
	"issueDueDateEdit":          uiIssueDueDateEditPath,
	"issueReporter":             uiIssueReporterPath,
	"issueReporterEdit":         uiIssueReporterEditPath,
	"issueRestore":              uiIssueRestorePath,
	"issueSprint":               uiIssueSprintPath,
	"issueSprintEdit":           uiIssueSprintEditPath,
	"issueStatus":               uiIssueStatusPath,
	"issueStatusEdit":           uiIssueStatusEditPath,
	"issueSubIssues":            uiIssueSubIssuesPath,
	"issueSubIssuesNew":         uiIssueSubIssuesNewPath,
	"issueAssigneeAutocomplete": uiIssueAssigneeAutocomplete,
	"issueReporterAutocomplete": uiIssueReporterAutocomplete,
	"issueSprintAutocomplete":   uiIssueSprintAutocomplete,
	"linkLabel":                 uiIssueLinkLabel,
	"linkOptions":               uiIssueLinkOptions,
	"linkedIssueProgress":       uiLinkedIssueProgress,
	"closeReasonLabel":          uiCloseReasonLabel,
	"closeReasonModal":          uiCloseReasonModal,
	"issueCloseReasonDropdown":  uiIssueCloseReasonDropdown,
	"issueStatusDropdown":       uiIssueStatusDropdown,
	"closeReasonOptions":        uiCloseReasonOptions,
	"issueColumnCount":          uiIssueColumnCount,
	"priorityClass":             uiPriorityClass,
	"priorityLabel":             uiPriorityLabel,
	"priorityOptions":           uiPriorityOptions,
	"projectPanel":              uiProjectPanelPath,
	"projectContext":            uiProjectContextPath,
	"projectContextDelete":      uiProjectContextDeletePath,
	"projectContextEdit":        uiProjectContextEditPath,
	"projectContextIssueDelete": uiProjectContextIssueDeletePath,
	"projectContextIssues":      uiProjectContextIssuesPath,
	"projectContextModal":       uiProjectContextModal,
	"projectContextNew":         uiProjectContextNewPath,
	"projectContexts":           uiProjectContextsPath,
	"projectView":               uiProjectViewPath,
	"projectIcon":               uiProjectIcon,
	"dueBadgeClass":             uiDueBadgeClass,
	"dueBadgeIcon":              uiDueBadgeIcon,
	"dueBadgeLabel":             uiDueBadgeLabel,
	"dueDateFull":               uiDueDateFull,
	"dueDateShort":              uiDueDateShort,
	"dueDateValue":              uiDueDateValue,
	"sprintDate":                uiSprintDate,
	"statusClass":               uiStatusClass,
	"statusLabel":               uiStatusLabel,
	"statusOptions":             uiStatusOptions,
	"statusRow":                 uiStatusRowClass,
	"statusSurface":             uiStatusSurfaceClass,
	"subIssueProgress":          uiSubIssueProgress,
	"tokenTime":                 uiTokenTime,
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
	NewProjectPanel   *uiNewProjectPanelData
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

type uiProjectStatusFilterItem struct {
	Label  string
	Href   string
	HXGet  string
	HXPush string
	Active bool
}

type uiProjectPriorityFilterItem struct {
	Priority model.IssuePriority
	Label    string
	Href     string
	HXGet    string
	HXPush   string
	Active   bool
}

type uiProjectSortOptionItem struct {
	Label  string
	Href   string
	HXGet  string
	HXPush string
	Active bool
}

type uiProjectAllIssuePageData struct {
	Issues    []model.Issue
	NextHXGet string
}

type uiIssueCommentItem struct {
	Comment     model.Comment
	AuthorName  string
	AuthorEmail string
	CanEdit     bool
}

type uiIssueLinkItem struct {
	Link        model.IssueLink
	LinkedIssue model.Issue
	HasIssue    bool
}

type uiProjectContextItem struct {
	Context             model.ProjectContextSummary
	LinkedIssues        []model.Issue
	LinkedIssuesHasMore bool
	LinkIssueInput      string
	LinkIssueError      string
}

type uiProjectContextOption struct {
	Value string
	Label string
}

type uiIssueSprintOption struct {
	Value string
	Label string
}

type uiAutocompleteOption struct {
	Value string
	Label string
}

type uiOptionDropdownData struct {
	Action       string
	HXTarget     string
	HXPushURL    string
	CancelHXGet  string
	ToggleLabel  string
	ListLabel    string
	Name         string
	CurrentValue string
	CurrentLabel string
	CurrentClass string
	Error        string
	Options      []uiOptionDropdownOption
}

type uiOptionDropdownOption struct {
	Value string
	Label string
	Class string
}

type uiAutocompleteEditData struct {
	Label       string
	Action      string
	PanelPath   string
	IssueHref   string
	Name        string
	Value       string
	Placeholder string
	SaveLabel   string
	CancelLabel string
	Error       string
	Options     []uiAutocompleteOption
}

type uiModalData struct {
	ID              string
	Title           string
	Description     string
	WidthClass      string
	CancelLabel     string
	CancelHXGet     string
	CancelHXPushURL string
	Badges          []uiModalBadge
}

type uiModalBadge struct {
	Label string
	Class string
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
	AllIssues            []model.Issue
	AllIssuePage         uiProjectAllIssuePageData
	AllStatusFilters     []uiProjectStatusFilterItem
	AllPriorityFilters   []uiProjectPriorityFilterItem
	AllSortOptions       []uiProjectSortOptionItem
	ContextItems         []uiProjectContextItem
	ContextHasMore       bool
	AddContext           bool
	ContextTitle         string
	ContextBody          string
	ContextError         string
	ContextUploadError   string
	EditContextID        uuid.UUID
	ContextEditTitle     string
	ContextEditBody      string
	ContextEditError     string
	DeleteNotice         *uiIssueDeleteNotice
	SprintIssuesHasMore  bool
	PlannedHasMore       bool
}

const uiProjectAllDefaultSort = store.ListIssuesSortUpdated

type uiProjectAllQuery struct {
	Statuses    []model.Status
	Priorities  []model.IssuePriority
	Sort        store.ListIssuesSort
	AssigneeIDs []uuid.UUID
	Cursor      string
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
	Issue              model.Issue
	Project            model.Project
	ParentIssue        *model.Issue
	Sprint             *model.Sprint
	Assignee           *model.User
	Reporter           *model.User
	EditDescription    bool
	EditStatus         bool
	PendingCloseReason bool
	EditCloseReason    bool
	EditPriority       bool
	EditDueDate        bool
	EditAssignee       bool
	EditReporter       bool
	EditSprint         bool
	CanEditSprint      bool
	AssigneeInput      string
	ReporterInput      string
	SprintInput        string
	DueDateInput       string
	CloseReasonInput   string
	AssigneeError      string
	ReporterError      string
	SprintError        string
	DueDateError       string
	CloseReasonError   string
	MemberOptions      []model.User
	SprintOptions      []uiIssueSprintOption
	SubIssues          []model.Issue
	SubIssuesHasMore   bool
	AddSubIssue        bool
	SubIssueTitle      string
	SubIssueError      string
	Comments           []uiIssueCommentItem
	CommentsHasMore    bool
	CommentBody        string
	CommentError       string
	EditCommentID      uuid.UUID
	CommentEditBody    string
	CommentEditError   string
	Links              []uiIssueLinkItem
	LinksHasMore       bool
	AddLink            bool
	EditLinkID         uuid.UUID
	LinkTarget         string
	LinkRelation       string
	LinkError          string
	Contexts           []model.ProjectContext
	ContextsHasMore    bool
	AddContext         bool
	ContextMode        string
	ContextOptions     []uiProjectContextOption
	ContextInput       string
	ContextTitle       string
	ContextBody        string
	ContextError       string
	ContextCreateError string
	ContextUploadError string
	BackHref           string
	BackHXGet          string
	BackLabel          string
	DeleteNotice       *uiIssueDeleteNotice
}

type uiProjectsPanelData struct {
	Projects []model.Project
	HasMore  bool
}

type uiNewProjectPanelData struct {
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
		r.Get("/projects/new", s.uiNewProjectPage)
		r.Get("/projects/new/panel", s.uiNewProjectPanel)
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
		r.Get("/{owner}/issues/{issueRef}/close-reason/edit", s.uiEditIssueCloseReason)
		r.Post("/{owner}/issues/{issueRef}/close-reason", s.uiUpdateIssueCloseReason)
		r.Get("/{owner}/issues/{issueRef}/priority/edit", s.uiEditIssuePriority)
		r.Post("/{owner}/issues/{issueRef}/priority", s.uiUpdateIssuePriority)
		r.Get("/{owner}/issues/{issueRef}/due-date/edit", s.uiEditIssueDueDate)
		r.Post("/{owner}/issues/{issueRef}/due-date", s.uiUpdateIssueDueDate)
		r.Get("/{owner}/issues/{issueRef}/assignee/edit", s.uiEditIssueAssignee)
		r.Post("/{owner}/issues/{issueRef}/assignee", s.uiUpdateIssueAssignee)
		r.Get("/{owner}/issues/{issueRef}/reporter/edit", s.uiEditIssueReporter)
		r.Post("/{owner}/issues/{issueRef}/reporter", s.uiUpdateIssueReporter)
		r.Get("/{owner}/issues/{issueRef}/sprint/edit", s.uiEditIssueSprint)
		r.Post("/{owner}/issues/{issueRef}/sprint", s.uiUpdateIssueSprint)
		r.Get("/{owner}/issues/{issueRef}/links/new", s.uiNewIssueLink)
		r.Post("/{owner}/issues/{issueRef}/links", s.uiCreateIssueLink)
		r.Get("/{owner}/issues/{issueRef}/links/{linkRef}/edit", s.uiEditIssueLink)
		r.Post("/{owner}/issues/{issueRef}/links/{linkRef}", s.uiUpdateIssueLink)
		r.Post("/{owner}/issues/{issueRef}/links/{linkRef}/delete", s.uiDeleteIssueLink)
		r.Get("/{owner}/issues/{issueRef}/sub-issues/new", s.uiNewSubIssue)
		r.Post("/{owner}/issues/{issueRef}/sub-issues", s.uiCreateSubIssue)
		r.Post("/{owner}/issues/{issueRef}/comments", s.uiCreateComment)
		r.Get("/{owner}/issues/{issueRef}/comments/{commentRef}/edit", s.uiEditComment)
		r.Post("/{owner}/issues/{issueRef}/comments/{commentRef}", s.uiUpdateComment)
		r.Get("/{owner}/issues/{issueRef}/context", s.uiViewIssueContext)
		r.Get("/{owner}/issues/{issueRef}/context/link", s.uiNewIssueContextLink)
		r.Get("/{owner}/issues/{issueRef}/context/new", s.uiNewIssueContext)
		r.Post("/{owner}/issues/{issueRef}/context", s.uiCreateIssueContextLink)
		r.Post("/{owner}/issues/{issueRef}/context/{contextRef}/delete", s.uiDeleteIssueContextLink)
		r.Get("/{owner}/projects/{key}", s.uiProjectPage)
		r.Get("/{owner}/projects/{key}/about", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPage(w, r, "about") })
		r.Get("/{owner}/projects/{key}/about/panel", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPanel(w, r, "about") })
		r.Get("/{owner}/projects/{key}/context/new", s.uiNewProjectContext)
		r.Post("/{owner}/projects/{key}/context", s.uiCreateProjectContext)
		r.Get("/{owner}/projects/{key}/context/{contextRef}/edit", s.uiEditProjectContext)
		r.Post("/{owner}/projects/{key}/context/{contextRef}", s.uiUpdateProjectContext)
		r.Post("/{owner}/projects/{key}/context/{contextRef}/delete", s.uiDeleteProjectContext)
		r.Post("/{owner}/projects/{key}/context/{contextRef}/issues", s.uiCreateProjectContextIssueLink)
		r.Post("/{owner}/projects/{key}/context/{contextRef}/issues/{issueRef}/delete", s.uiDeleteProjectContextIssueLink)
		r.Get("/{owner}/projects/{key}/sprint", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPage(w, r, "sprint") })
		r.Get("/{owner}/projects/{key}/sprint/panel", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPanel(w, r, "sprint") })
		r.Get("/{owner}/projects/{key}/planned", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPage(w, r, "planned") })
		r.Get("/{owner}/projects/{key}/planned/panel", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPanel(w, r, "planned") })
		r.Get("/{owner}/projects/{key}/all", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPage(w, r, "all") })
		r.Get("/{owner}/projects/{key}/all/panel", func(w http.ResponseWriter, r *http.Request) { s.uiProjectWorkPanel(w, r, "all") })
		r.Get("/{owner}/projects/{key}/all/page", s.uiProjectAllIssuePage)
		r.Get("/{owner}/projects/{key}/backlog", func(w http.ResponseWriter, r *http.Request) { s.uiProjectLegacyBacklog(w, r, false) })
		r.Get("/{owner}/projects/{key}/backlog/panel", func(w http.ResponseWriter, r *http.Request) { s.uiProjectLegacyBacklog(w, r, true) })
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
	s.renderUIProjects(w, r, http.StatusOK)
}

func (s *Server) uiProjectsPanel(w http.ResponseWriter, r *http.Request) {
	panel, err := s.uiBuildProjectsPanel(r.Context(), currentUser(r))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	renderUITemplate(w, http.StatusOK, "projects-panel", panel)
}

func (s *Server) uiNewProjectPage(w http.ResponseWriter, r *http.Request) {
	s.renderUINewProject(w, r, http.StatusOK, "", "", "", "")
}

func (s *Server) uiNewProjectPanel(w http.ResponseWriter, r *http.Request) {
	renderUITemplate(w, http.StatusOK, "new-project-panel", uiNewProjectPanelData{})
}

func (s *Server) uiCreateProject(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderUINewProject(w, r, http.StatusBadRequest, "Unable to read form.", "", "", "")
		return
	}
	key := strings.TrimSpace(r.Form.Get("key"))
	name := strings.TrimSpace(r.Form.Get("name"))
	description := r.Form.Get("description")
	if !projectKeyRe.MatchString(key) {
		s.renderUINewProject(w, r, http.StatusBadRequest, "Key must match ^[A-Z][A-Z0-9]{1,9}$.", key, name, description)
		return
	}
	if name == "" {
		s.renderUINewProject(w, r, http.StatusBadRequest, "Name required.", key, name, description)
		return
	}
	project, err := s.store.CreateProjectForUser(r.Context(), currentUser(r).ID, key, name, description)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUINewProject(w, r, http.StatusConflict, "Project key already exists.", key, name, description)
			return
		}
		writeUIStoreError(w, err)
		return
	}
	http.Redirect(w, r, uiProjectViewPath(project, "sprint"), http.StatusSeeOther)
}

func (s *Server) renderUIProjects(w http.ResponseWriter, r *http.Request, status int) {
	panel, err := s.uiBuildProjectsPanel(r.Context(), currentUser(r))
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

func (s *Server) renderUINewProject(w http.ResponseWriter, r *http.Request, status int, message, key, name, description string) {
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	renderUITemplate(w, status, "shell", uiShellData{
		User:        currentUser(r),
		Projects:    projects,
		CurrentView: "projects",
		NewProjectPanel: &uiNewProjectPanelData{
			Error:       message,
			Key:         key,
			Name:        name,
			Description: description,
		},
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

func (s *Server) uiNewProjectContext(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIProjectAboutPanel(w, r, project.ID, func(panel *uiProjectPanelData) {
		panel.AddContext = true
	})
}

func (s *Server) uiCreateProjectContext(w http.ResponseWriter, r *http.Request) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}

	var params store.CreateProjectContextParams
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxProjectContextUploadBytes+1024*1024)
		upload, err := readProjectContextUpload(r)
		if err != nil {
			s.renderUIProjectAboutPanel(w, r, project.ID, func(panel *uiProjectPanelData) {
				panel.AddContext = true
				panel.ContextUploadError = err.Error()
			})
			return
		}
		params = store.CreateProjectContextParams{
			ProjectID:      project.ID,
			Title:          upload.Title,
			Kind:           model.ProjectContextKindText,
			ContentType:    upload.ContentType,
			Body:           upload.Body,
			SourceFilename: upload.SourceFilename,
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "unable to read form", http.StatusBadRequest)
			return
		}
		titleInput := r.Form.Get("title")
		bodyInput := r.Form.Get("body")
		title, err := validateProjectContextTitle(titleInput)
		if err != nil {
			s.renderUIProjectAboutPanel(w, r, project.ID, func(panel *uiProjectPanelData) {
				panel.AddContext = true
				panel.ContextTitle = titleInput
				panel.ContextBody = bodyInput
				panel.ContextError = err.Error()
			})
			return
		}
		body, err := validateProjectContextBody(bodyInput)
		if err != nil {
			s.renderUIProjectAboutPanel(w, r, project.ID, func(panel *uiProjectPanelData) {
				panel.AddContext = true
				panel.ContextTitle = titleInput
				panel.ContextBody = bodyInput
				panel.ContextError = err.Error()
			})
			return
		}
		params = store.CreateProjectContextParams{
			ProjectID:   project.ID,
			Title:       title,
			Kind:        model.ProjectContextKindText,
			ContentType: "text/plain; charset=utf-8",
			Body:        body,
		}
	}
	params.CreatedByID = currentUser(r).ID
	if _, err := s.store.CreateProjectContext(r.Context(), params); err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIProjectAboutPanel(w, r, project.ID, nil)
}

func (s *Server) uiEditProjectContext(w http.ResponseWriter, r *http.Request) {
	project, contextItem, ok := s.uiProjectContextFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIProjectAboutPanel(w, r, project.ID, func(panel *uiProjectPanelData) {
		panel.EditContextID = contextItem.ID
		panel.ContextEditTitle = contextItem.Title
		panel.ContextEditBody = contextItem.Body
	})
}

func (s *Server) uiUpdateProjectContext(w http.ResponseWriter, r *http.Request) {
	project, contextItem, ok := s.uiProjectContextFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	titleInput := r.Form.Get("title")
	bodyInput := r.Form.Get("body")
	title, err := validateProjectContextTitle(titleInput)
	if err != nil {
		s.renderUIProjectContextEditError(w, r, project.ID, contextItem.ID, titleInput, bodyInput, err.Error())
		return
	}
	body, err := validateProjectContextBody(bodyInput)
	if err != nil {
		s.renderUIProjectContextEditError(w, r, project.ID, contextItem.ID, titleInput, bodyInput, err.Error())
		return
	}
	if _, err := s.store.UpdateProjectContext(r.Context(), store.UpdateProjectContextParams{
		ID:          contextItem.ID,
		Title:       &title,
		Body:        &body,
		UpdatedByID: currentUser(r).ID,
	}); err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIProjectAboutPanel(w, r, project.ID, nil)
}

func (s *Server) uiDeleteProjectContext(w http.ResponseWriter, r *http.Request) {
	project, contextItem, ok := s.uiProjectContextFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	if err := s.store.DeleteProjectContext(r.Context(), contextItem.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIProjectAboutPanel(w, r, project.ID, nil)
}

func (s *Server) uiCreateProjectContextIssueLink(w http.ResponseWriter, r *http.Request) {
	project, contextItem, ok := s.uiProjectContextFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	input := strings.TrimSpace(r.Form.Get("issue"))
	issue, message, err := s.uiProjectContextIssueInput(r.Context(), project, input)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIProjectContextLinkError(w, r, project.ID, contextItem.ID, input, message)
		return
	}
	if _, err := s.store.CreateIssueContextLink(r.Context(), issue.ID, contextItem.ID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIProjectContextLinkError(w, r, project.ID, contextItem.ID, input, "Issue already linked.")
			return
		}
		writeUIStoreError(w, err)
		return
	}
	s.renderUIProjectAboutPanel(w, r, project.ID, nil)
}

func (s *Server) uiDeleteProjectContextIssueLink(w http.ResponseWriter, r *http.Request) {
	project, contextItem, ok := s.uiProjectContextFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), project.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	ref, err := parseIssueRef(chi.URLParam(r, "issueRef"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	issue, err := s.store.GetIssueByOwnerKeyNumber(r.Context(), project.OwnerUsername, ref.ProjectKey, ref.Number)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if issue.ProjectID != project.ID {
		writeUIStoreError(w, store.ErrNotFound)
		return
	}
	if err := s.store.DeleteIssueContextLink(r.Context(), issue.ID, contextItem.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	s.renderUIProjectAboutPanel(w, r, project.ID, nil)
}

func (s *Server) renderUIProjectAboutPanel(w http.ResponseWriter, r *http.Request, projectID uuid.UUID, mutate func(*uiProjectPanelData)) {
	panel, err := s.uiBuildProjectPanel(r.Context(), r, projectID, "about")
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if mutate != nil {
		mutate(panel)
	}
	renderUITemplate(w, http.StatusOK, "project-panel", panel)
}

func (s *Server) renderUIProjectContextEditError(w http.ResponseWriter, r *http.Request, projectID, contextID uuid.UUID, title, body, message string) {
	s.renderUIProjectAboutPanel(w, r, projectID, func(panel *uiProjectPanelData) {
		panel.EditContextID = contextID
		panel.ContextEditTitle = title
		panel.ContextEditBody = body
		panel.ContextEditError = message
	})
}

func (s *Server) renderUIProjectContextLinkError(w http.ResponseWriter, r *http.Request, projectID, contextID uuid.UUID, input, message string) {
	s.renderUIProjectAboutPanel(w, r, projectID, func(panel *uiProjectPanelData) {
		for i := range panel.ContextItems {
			if panel.ContextItems[i].Context.ID == contextID {
				panel.ContextItems[i].LinkIssueInput = input
				panel.ContextItems[i].LinkIssueError = message
				return
			}
		}
	})
}

func (s *Server) uiProjectContextIssueInput(ctx context.Context, project model.Project, raw string) (model.Issue, string, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return model.Issue{}, "Issue required.", nil
	}
	ref, err := parseIssueRef(input)
	if err != nil {
		return model.Issue{}, "Choose an issue in this project.", nil
	}
	issue, err := s.store.GetIssueByOwnerKeyNumber(ctx, project.OwnerUsername, ref.ProjectKey, ref.Number)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return model.Issue{}, "Issue not found.", nil
		}
		return model.Issue{}, "", err
	}
	if issue.ProjectID != project.ID {
		return model.Issue{}, "Issue must be in this project.", nil
	}
	return issue, "", nil
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
	if status == model.StatusClosed && issue.CloseReason == nil {
		panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
		if err != nil {
			writeUIStoreError(w, err)
			return
		}
		panel.PendingCloseReason = true
		panel.Issue.Status = model.StatusClosed
		panel.CanEditSprint = false
		renderUITemplate(w, http.StatusOK, "issue-panel", panel)
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

func (s *Server) uiEditIssueCloseReason(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if issue.Status == model.StatusClosed {
		panel.EditCloseReason = true
	}
	if issue.CloseReason != nil {
		panel.CloseReasonInput = string(*issue.CloseReason)
	} else if issue.Status != model.StatusClosed {
		panel.PendingCloseReason = true
		panel.Issue.Status = model.StatusClosed
		panel.CanEditSprint = false
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueCloseReason(w http.ResponseWriter, r *http.Request) {
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
	reason := model.IssueCloseReason(strings.TrimSpace(r.Form.Get("close_reason")))
	if !reason.Valid() {
		s.renderUIIssuePanelWithCloseReasonError(w, r, issue.ID, string(reason), "Choose a close reason.")
		return
	}
	params := store.UpdateIssueParams{
		CloseReason: &reason,
	}
	if issue.Status != model.StatusClosed {
		status := model.StatusClosed
		params.Status = &status
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, params)
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

func (s *Server) uiEditIssueDueDate(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditDueDate = true
	panel.DueDateInput = uiDueDateValue(panel.Issue.DueDate)
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueDueDate(w http.ResponseWriter, r *http.Request) {
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
	input := strings.TrimSpace(r.Form.Get("due_date"))
	params := store.UpdateIssueParams{}
	if input == "" {
		params.ClearDueDate = true
	} else {
		dueDate, err := model.ParseDate(input)
		if err != nil {
			s.renderUIIssuePanelWithDueDateError(w, r, issue.ID, input, "Use YYYY-MM-DD.")
			return
		}
		params.DueDate = &dueDate
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, params)
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

func (s *Server) uiEditIssueSprint(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if !panel.CanEditSprint {
		renderUITemplate(w, http.StatusOK, "issue-panel", panel)
		return
	}
	panel.EditSprint = true
	panel.SprintInput = uiIssueSprintInput(panel.Sprint)
	if err := s.uiPopulateIssueSprintOptions(r.Context(), panel); err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateIssueSprint(w http.ResponseWriter, r *http.Request) {
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
	if issue.ParentIssueID != nil || issue.Status.CountsAsDone() {
		writeUIStoreError(w, store.ErrConflict)
		return
	}
	input := r.Form.Get("sprint")
	sprintID, clear, message, err := s.uiIssueSprintID(r.Context(), issue.ProjectID, input)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIIssuePanelWithSprintError(w, r, issue.ID, input, message)
		return
	}
	updated, err := s.store.UpdateIssue(r.Context(), issue.ID, store.UpdateIssueParams{
		SprintID:    sprintID,
		ClearSprint: clear,
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

func (s *Server) uiCreateIssueContextLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}

	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxProjectContextUploadBytes+1024*1024)
		upload, err := readProjectContextUpload(r)
		if err != nil {
			s.renderUIIssuePanelWithContextUploadError(w, r, issue.ID, err.Error())
			return
		}
		if _, err := s.store.CreateIssueContext(r.Context(), store.CreateIssueContextParams{
			IssueID:        issue.ID,
			Title:          upload.Title,
			Kind:           model.ProjectContextKindText,
			ContentType:    upload.ContentType,
			Body:           upload.Body,
			SourceFilename: upload.SourceFilename,
			CreatedByID:    currentUser(r).ID,
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
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	if r.Form.Get("mode") == "create" {
		titleInput := r.Form.Get("title")
		bodyInput := r.Form.Get("body")
		title, err := validateProjectContextTitle(titleInput)
		if err != nil {
			s.renderUIIssuePanelWithContextCreateError(w, r, issue.ID, titleInput, bodyInput, err.Error())
			return
		}
		body, err := validateProjectContextBody(bodyInput)
		if err != nil {
			s.renderUIIssuePanelWithContextCreateError(w, r, issue.ID, titleInput, bodyInput, err.Error())
			return
		}
		if _, err := s.store.CreateIssueContext(r.Context(), store.CreateIssueContextParams{
			IssueID:     issue.ID,
			Title:       title,
			Kind:        model.ProjectContextKindText,
			ContentType: "text/plain; charset=utf-8",
			Body:        body,
			CreatedByID: currentUser(r).ID,
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
		return
	}

	input := strings.TrimSpace(r.Form.Get("context"))
	contextItem, message, err := s.uiProjectContextInput(r.Context(), issue.ProjectID, input)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if message != "" {
		s.renderUIIssuePanelWithContextError(w, r, issue.ID, input, message)
		return
	}
	if _, err := s.store.CreateIssueContextLink(r.Context(), issue.ID, contextItem.ID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIIssuePanelWithContextError(w, r, issue.ID, input, "Context already linked.")
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

func (s *Server) uiNewIssueContextLink(w http.ResponseWriter, r *http.Request) {
	s.uiRenderIssueContextMode(w, r, "attach")
}

func (s *Server) uiNewIssueContext(w http.ResponseWriter, r *http.Request) {
	s.uiRenderIssueContextMode(w, r, "create")
}

func (s *Server) uiViewIssueContext(w http.ResponseWriter, r *http.Request) {
	s.uiRenderIssueContextMode(w, r, "view")
}

func (s *Server) uiRenderIssueContextMode(w http.ResponseWriter, r *http.Request, mode string) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.AddContext = mode == "attach" || mode == "create"
	panel.ContextMode = mode
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiDeleteIssueContextLink(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := s.uiRequireProjectAccess(r.Context(), currentUser(r), issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	number, err := parseTypedRef(chi.URLParam(r, "contextRef"), "context")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	contextItem, err := s.store.GetProjectContextByProjectNumber(r.Context(), issue.ProjectID, number)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if err := s.store.DeleteIssueContextLink(r.Context(), issue.ID, contextItem.ID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	if contextItem.Scope == model.ProjectContextScopeIssue {
		if err := s.store.DeleteProjectContext(r.Context(), contextItem.ID); err != nil {
			writeUIStoreError(w, err)
			return
		}
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
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

func (s *Server) uiNewSubIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.AddSubIssue = true
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

func (s *Server) uiEditComment(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	user := currentUser(r)
	if err := s.uiRequireProjectAccess(r.Context(), user, issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	comment, ok := s.uiCommentFromRoute(w, r, issue)
	if !ok {
		return
	}
	if comment.AuthorID != user.ID {
		writeUIStoreError(w, errUIForbidden)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issue.ID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditCommentID = comment.ID
	panel.CommentEditBody = comment.Body
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiUpdateComment(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.uiIssueFromRoute(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unable to read form", http.StatusBadRequest)
		return
	}
	user := currentUser(r)
	if err := s.uiRequireProjectAccess(r.Context(), user, issue.ProjectID); err != nil {
		writeUIStoreError(w, err)
		return
	}
	comment, ok := s.uiCommentFromRoute(w, r, issue)
	if !ok {
		return
	}
	if comment.AuthorID != user.ID {
		writeUIStoreError(w, errUIForbidden)
		return
	}
	body := strings.TrimSpace(r.Form.Get("body"))
	if body == "" || len(body) > 10000 {
		s.renderUIIssuePanelWithCommentEditError(w, r, issue.ID, comment.ID, r.Form.Get("body"), "Comment required, max 10000 chars.")
		return
	}
	updated, err := s.store.UpdateComment(r.Context(), store.UpdateCommentParams{
		ID:       comment.ID,
		AuthorID: user.ID,
		Body:     body,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.renderUIIssuePanelWithCommentEditError(w, r, issue.ID, comment.ID, r.Form.Get("body"), "Comment required, max 10000 chars.")
			return
		}
		writeUIStoreError(w, err)
		return
	}
	panel, err := s.uiBuildIssuePanel(r.Context(), r, updated.IssueID)
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
	panel.AddSubIssue = true
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

func (s *Server) renderUIIssuePanelWithCommentEditError(w http.ResponseWriter, r *http.Request, issueID, commentID uuid.UUID, body, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditCommentID = commentID
	panel.CommentEditBody = body
	panel.CommentEditError = message
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

func (s *Server) renderUIIssuePanelWithSprintError(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, input, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditSprint = true
	panel.SprintInput = input
	panel.SprintError = message
	if err := s.uiPopulateIssueSprintOptions(r.Context(), panel); err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) renderUIIssuePanelWithDueDateError(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, input, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.EditDueDate = true
	panel.DueDateInput = input
	panel.DueDateError = message
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) renderUIIssuePanelWithCloseReasonError(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, input, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	if panel.Issue.Status == model.StatusClosed {
		panel.EditCloseReason = true
	} else {
		panel.PendingCloseReason = true
		panel.Issue.Status = model.StatusClosed
		panel.CanEditSprint = false
	}
	panel.CloseReasonInput = input
	panel.CloseReasonError = message
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

func (s *Server) uiPopulateIssueSprintOptions(ctx context.Context, panel *uiIssuePanelData) error {
	active, _, err := s.store.ListSprints(ctx, store.ListSprintsParams{
		ProjectID: panel.Project.ID,
		Status:    model.SprintStatusActive,
		Limit:     MaxLimit,
	})
	if err != nil {
		return err
	}
	planned, _, err := s.store.ListSprints(ctx, store.ListSprintsParams{
		ProjectID: panel.Project.ID,
		Status:    model.SprintStatusPlanned,
		Limit:     MaxLimit,
	})
	if err != nil {
		return err
	}
	panel.SprintOptions = make([]uiIssueSprintOption, 0, len(active)+len(planned))
	for _, sprint := range active {
		panel.SprintOptions = append(panel.SprintOptions, uiIssueSprintOptionFor(sprint, "Active"))
	}
	for _, sprint := range planned {
		panel.SprintOptions = append(panel.SprintOptions, uiIssueSprintOptionFor(sprint, "Planned"))
	}
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

func (s *Server) uiIssueSprintID(ctx context.Context, projectID uuid.UUID, raw string) (*uuid.UUID, bool, string, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return nil, true, "", nil
	}
	number, err := parseTypedRef(input, "sprint")
	if err != nil {
		return nil, false, "Choose an active or planned sprint.", nil
	}
	sprint, err := s.store.GetSprintByProjectNumber(ctx, projectID, number)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, "Choose an active or planned sprint.", nil
		}
		return nil, false, "", err
	}
	if sprint.Status == model.SprintStatusCompleted {
		return nil, false, "Choose an active or planned sprint.", nil
	}
	return &sprint.ID, false, "", nil
}

func uiIssueSprintInput(sprint *model.Sprint) string {
	if sprint == nil {
		return ""
	}
	return uiIssueSprintRef(*sprint)
}

func uiIssueSprintOptionFor(sprint model.Sprint, status string) uiIssueSprintOption {
	ref := uiIssueSprintRef(sprint)
	name := strings.TrimSpace(sprint.Name)
	if name == "" {
		name = ref
	}
	return uiIssueSprintOption{
		Value: ref,
		Label: fmt.Sprintf("%s - %s - %s-%s", status, name, uiSprintDate(sprint.StartDate), uiSprintDate(sprint.EndDate)),
	}
}

func uiIssueAssigneeAutocomplete(panel *uiIssuePanelData) uiAutocompleteEditData {
	return uiIssueMemberAutocomplete(panel, "Assignee", uiIssueAssigneePath(panel.Issue), "assignee", panel.AssigneeInput, "Unassigned", "Save assignee", "Cancel editing assignee", panel.AssigneeError)
}

func uiIssueReporterAutocomplete(panel *uiIssuePanelData) uiAutocompleteEditData {
	return uiIssueMemberAutocomplete(panel, "Reporter", uiIssueReporterPath(panel.Issue), "reporter", panel.ReporterInput, "No reporter", "Save reporter", "Cancel editing reporter", panel.ReporterError)
}

func uiIssueMemberAutocomplete(panel *uiIssuePanelData, label, action, name, value, placeholder, saveLabel, cancelLabel, message string) uiAutocompleteEditData {
	return uiAutocompleteEditData{
		Label:       label,
		Action:      action,
		PanelPath:   uiIssuePanelPath(panel.Issue),
		IssueHref:   uiIssuePath(panel.Issue),
		Name:        name,
		Value:       value,
		Placeholder: placeholder,
		SaveLabel:   saveLabel,
		CancelLabel: cancelLabel,
		Error:       message,
		Options:     uiMemberAutocompleteOptions(panel.MemberOptions),
	}
}

func uiIssueSprintAutocomplete(panel *uiIssuePanelData) uiAutocompleteEditData {
	return uiAutocompleteEditData{
		Label:       "Sprint",
		Action:      uiIssueSprintPath(panel.Issue),
		PanelPath:   uiIssuePanelPath(panel.Issue),
		IssueHref:   uiIssuePath(panel.Issue),
		Name:        "sprint",
		Value:       panel.SprintInput,
		Placeholder: "None",
		SaveLabel:   "Save sprint",
		CancelLabel: "Cancel editing sprint",
		Error:       panel.SprintError,
		Options:     uiSprintAutocompleteOptions(panel.SprintOptions),
	}
}

func uiMemberAutocompleteOptions(users []model.User) []uiAutocompleteOption {
	options := make([]uiAutocompleteOption, 0, len(users))
	for _, user := range users {
		label := strings.TrimSpace(user.Name)
		if user.Email != "" {
			if label == "" {
				label = user.Email
			} else {
				label += " - " + user.Email
			}
		}
		if label == "" {
			label = "@" + user.Username
		}
		options = append(options, uiAutocompleteOption{
			Value: "@" + user.Username,
			Label: label,
		})
	}
	return options
}

func uiSprintAutocompleteOptions(sprints []uiIssueSprintOption) []uiAutocompleteOption {
	options := make([]uiAutocompleteOption, 0, len(sprints))
	for _, sprint := range sprints {
		options = append(options, uiAutocompleteOption{
			Value: sprint.Value,
			Label: sprint.Label,
		})
	}
	return options
}

func uiIssueSprintRef(sprint model.Sprint) string {
	if sprint.Ref != "" {
		return sprint.Ref
	}
	if sprint.Number > 0 {
		return model.SprintRef(sprint.Number)
	}
	return ""
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

func (s *Server) renderUIIssuePanelWithContextError(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, input, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.ContextInput = input
	panel.ContextError = message
	panel.AddContext = true
	panel.ContextMode = "attach"
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) renderUIIssuePanelWithContextCreateError(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, title, body, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.ContextTitle = title
	panel.ContextBody = body
	panel.ContextCreateError = message
	panel.AddContext = true
	panel.ContextMode = "create"
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) renderUIIssuePanelWithContextUploadError(w http.ResponseWriter, r *http.Request, issueID uuid.UUID, message string) {
	panel, err := s.uiBuildIssuePanel(r.Context(), r, issueID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	panel.ContextUploadError = message
	panel.AddContext = true
	panel.ContextMode = "create"
	renderUITemplate(w, http.StatusOK, "issue-panel", panel)
}

func (s *Server) uiProjectContextInput(ctx context.Context, projectID uuid.UUID, raw string) (model.ProjectContext, string, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return model.ProjectContext{}, "Context required.", nil
	}
	number, err := parseTypedRef(input, "context")
	if err != nil {
		return model.ProjectContext{}, "Choose project context.", nil
	}
	contextItem, err := s.store.GetProjectContextByProjectNumber(ctx, projectID, number)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return model.ProjectContext{}, "Context not found.", nil
		}
		return model.ProjectContext{}, "", err
	}
	if contextItem.Scope != model.ProjectContextScopeProject {
		return model.ProjectContext{}, "Context not found.", nil
	}
	return contextItem, "", nil
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
	var assigneeIDs []uuid.UUID
	var allQuery uiProjectAllQuery
	if view == "sprint" {
		assigneeIDs, err = uiParseAssigneeFilter(r)
		if err != nil {
			return nil, err
		}
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
		ProjectTabs:          uiProjectTabs(project, view, assigneeIDs),
		AssigneeFilterActive: len(assigneeIDs) > 0,
		ClearAssigneeHref:    uiProjectViewPath(project, view),
		ClearAssigneeHXGet:   uiProjectPanelPath(project, view),
		ClearAssigneeHXPush:  uiProjectViewPath(project, view),
		DeleteNotice:         deleteNotice,
	}
	if view == "sprint" || view == "all" {
		assignees, err := s.store.ListProjectAssignees(ctx, projectID)
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
			panel.AssigneeFilters = uiProjectAssigneeFilters(project, view, assignees, assigneeIDs)
		}
	}

	switch view {
	case "about":
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
	case "all":
		pageData, err := s.uiBuildProjectAllIssuePage(ctx, r, project)
		if err != nil {
			return nil, err
		}
		panel.AllIssues = pageData.Issues
		panel.AllIssuePage = pageData
		panel.AllStatusFilters = uiProjectAllStatusFilters(project, allQuery)
		panel.AllPriorityFilters = uiProjectAllPriorityFilters(project, allQuery)
		panel.AllSortOptions = uiProjectAllSortOptions(project, allQuery)
	default:
		return nil, store.ErrNotFound
	}

	return panel, nil
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
		AssigneeIDs:      allQuery.AssigneeIDs,
		Cursor:           cursor,
		Limit:            DefaultLimit,
		Sort:             allQuery.Sort,
		IncludeSubIssues: true,
	})
	if err != nil {
		return uiProjectAllIssuePageData{}, err
	}
	pageData := uiProjectAllIssuePageData{Issues: issues}
	if hasMore {
		last := issues[len(issues)-1]
		nextQuery := allQuery
		nextQuery.Cursor = encodeCursor(uiProjectAllIssueCursor(last, allQuery.Sort))
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
		IssueID:     issueID,
		Limit:       MaxLimit,
		NewestFirst: true,
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
		item := uiIssueCommentItem{
			Comment:    comment,
			AuthorName: "Unknown user",
			CanEdit:    comment.AuthorID == currentUser(r).ID,
		}
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
	contexts, contextsHasMore, err := s.store.ListContextsForIssue(ctx, store.ListContextsForIssueParams{
		IssueID: issueID,
		Limit:   MaxLimit,
	})
	if err != nil {
		return nil, err
	}
	contextSummaries, _, err := s.store.ListProjectContexts(ctx, store.ListProjectContextsParams{
		ProjectID: projectID,
		Limit:     MaxLimit,
	})
	if err != nil {
		return nil, err
	}
	contextOptions := make([]uiProjectContextOption, 0, len(contextSummaries))
	for _, contextItem := range contextSummaries {
		contextOptions = append(contextOptions, uiProjectContextOption{
			Value: contextItem.Ref,
			Label: contextItem.Ref + " - " + contextItem.Title,
		})
	}

	backHref, backHXGet, backLabel := uiIssueBackLink(project, issue, parentIssue, sprint)
	return &uiIssuePanelData{
		Issue:            issue,
		Project:          project,
		ParentIssue:      parentIssue,
		Sprint:           sprint,
		Assignee:         assignee,
		Reporter:         reporter,
		CanEditSprint:    issue.ParentIssueID == nil && !issue.Status.CountsAsDone(),
		SubIssues:        subIssues,
		SubIssuesHasMore: subIssuesHasMore,
		Comments:         commentItems,
		CommentsHasMore:  commentsHasMore,
		Links:            linkItems,
		LinksHasMore:     linksHasMore,
		Contexts:         contexts,
		ContextsHasMore:  contextsHasMore,
		ContextOptions:   contextOptions,
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
	if issue.SprintID == nil {
		view = "all"
	} else if sprint != nil && sprint.Status == model.SprintStatusPlanned {
		view = "all"
	}
	base := uiProjectViewPath(project, view)
	label = "Sprint"
	switch view {
	case "all":
		label = "All"
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
	if panel.Issue.SprintID == nil {
		view = "all"
	} else if panel.Sprint != nil && panel.Sprint.Status == model.SprintStatusPlanned {
		view = "all"
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
	var sprintAssigneeIDs []uuid.UUID
	if view == "sprint" {
		sprintAssigneeIDs = assigneeIDs
	}
	return uiTabBarData{
		Label: "Project views",
		Items: []uiTabItem{
			{
				Label:     "Sprint",
				Icon:      "person-standing",
				Href:      uiProjectViewPath(project, "sprint", sprintAssigneeIDs),
				HXGet:     uiProjectPanelPath(project, "sprint", sprintAssigneeIDs),
				HXTarget:  "#main",
				HXPushURL: uiProjectViewPath(project, "sprint", sprintAssigneeIDs),
				Active:    view == "sprint",
			},
			{
				Label:     "Planned",
				Icon:      "calendar-range",
				Href:      uiProjectViewPath(project, "planned"),
				HXGet:     uiProjectPanelPath(project, "planned"),
				HXTarget:  "#main",
				HXPushURL: uiProjectViewPath(project, "planned"),
				Active:    view == "planned",
			},
			{
				Label:     "All",
				Icon:      "list-filter",
				Href:      uiProjectViewPath(project, "all"),
				HXGet:     uiProjectPanelPath(project, "all"),
				HXTarget:  "#main",
				HXPushURL: uiProjectViewPath(project, "all"),
				Active:    view == "all",
			},
			{
				Label:     "About",
				Icon:      "info",
				Href:      uiProjectViewPath(project, "about"),
				HXGet:     uiProjectPanelPath(project, "about"),
				HXTarget:  "#main",
				HXPushURL: uiProjectViewPath(project, "about"),
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

func uiProjectAllAssigneeFilters(project model.Project, assignees []model.ProjectAssignee, query uiProjectAllQuery) []uiAssigneeFilterItem {
	out := make([]uiAssigneeFilterItem, 0, len(assignees))
	for _, assignee := range assignees {
		nextQuery := query
		nextIDs, selected := uiToggleAssigneeIDs(query.AssigneeIDs, assignee.ID)
		nextQuery.AssigneeIDs = nextIDs
		out = append(out, uiAssigneeFilterItem{
			Assignee: assignee,
			Selected: selected,
			Href:     uiProjectAllViewPath(project, nextQuery),
			HXGet:    uiProjectAllPanelPath(project, nextQuery),
			HXPush:   uiProjectAllViewPath(project, nextQuery),
		})
	}
	return out
}

func uiProjectAllStatusFilters(project model.Project, query uiProjectAllQuery) []uiProjectStatusFilterItem {
	options := []struct {
		Label  string
		Status model.Status
	}{
		{Label: "Any", Status: ""},
		{Label: uiStatusLabel(model.StatusTodo), Status: model.StatusTodo},
		{Label: uiStatusLabel(model.StatusInProgress), Status: model.StatusInProgress},
		{Label: uiStatusLabel(model.StatusDone), Status: model.StatusDone},
		{Label: uiStatusLabel(model.StatusClosed), Status: model.StatusClosed},
	}
	out := make([]uiProjectStatusFilterItem, 0, len(options))
	for _, option := range options {
		nextQuery := query
		active := false
		if option.Status == "" {
			nextQuery.Statuses = nil
			active = len(query.Statuses) == 0
		} else {
			var selected bool
			nextQuery.Statuses, selected = uiToggleStatuses(query.Statuses, option.Status)
			active = selected
		}
		out = append(out, uiProjectStatusFilterItem{
			Label:  option.Label,
			Href:   uiProjectAllViewPath(project, nextQuery),
			HXGet:  uiProjectAllPanelPath(project, nextQuery),
			HXPush: uiProjectAllViewPath(project, nextQuery),
			Active: active,
		})
	}
	return out
}

func uiProjectAllPriorityFilters(project model.Project, query uiProjectAllQuery) []uiProjectPriorityFilterItem {
	options := []struct {
		Label    string
		Priority model.IssuePriority
	}{
		{Label: "Any", Priority: ""},
		{Label: uiPriorityLabel(model.PriorityP0), Priority: model.PriorityP0},
		{Label: uiPriorityLabel(model.PriorityP1), Priority: model.PriorityP1},
		{Label: uiPriorityLabel(model.PriorityP2), Priority: model.PriorityP2},
		{Label: uiPriorityLabel(model.PriorityP3), Priority: model.PriorityP3},
		{Label: uiPriorityLabel(model.PriorityP4), Priority: model.PriorityP4},
	}
	out := make([]uiProjectPriorityFilterItem, 0, len(options))
	for _, option := range options {
		nextQuery := query
		active := false
		if option.Priority == "" {
			nextQuery.Priorities = nil
			active = len(query.Priorities) == 0
		} else {
			var selected bool
			nextQuery.Priorities, selected = uiTogglePriorities(query.Priorities, option.Priority)
			active = selected
		}
		out = append(out, uiProjectPriorityFilterItem{
			Priority: option.Priority,
			Label:    option.Label,
			Href:     uiProjectAllViewPath(project, nextQuery),
			HXGet:    uiProjectAllPanelPath(project, nextQuery),
			HXPush:   uiProjectAllViewPath(project, nextQuery),
			Active:   active,
		})
	}
	return out
}

func uiProjectAllSortOptions(project model.Project, query uiProjectAllQuery) []uiProjectSortOptionItem {
	currentSort := query.Sort
	if currentSort == "" {
		currentSort = uiProjectAllDefaultSort
	}
	options := []struct {
		Label string
		Sort  store.ListIssuesSort
	}{
		{Label: "Updated", Sort: store.ListIssuesSortUpdated},
		{Label: "Created", Sort: store.ListIssuesSortCreated},
		{Label: "Status", Sort: store.ListIssuesSortStatus},
		{Label: "Priority", Sort: store.ListIssuesSortPriority},
	}
	out := make([]uiProjectSortOptionItem, 0, len(options))
	for _, option := range options {
		nextQuery := query
		nextQuery.Sort = option.Sort
		out = append(out, uiProjectSortOptionItem{
			Label:  option.Label,
			Href:   uiProjectAllViewPath(project, nextQuery),
			HXGet:  uiProjectAllPanelPath(project, nextQuery),
			HXPush: uiProjectAllViewPath(project, nextQuery),
			Active: currentSort == option.Sort,
		})
	}
	return out
}

func uiParseProjectAllQuery(r *http.Request) (uiProjectAllQuery, error) {
	assigneeIDs, err := uiParseAssigneeFilter(r)
	if err != nil {
		return uiProjectAllQuery{}, err
	}
	statuses, err := uiParseProjectIssueStatusFilters(r)
	if err != nil {
		return uiProjectAllQuery{}, err
	}
	priorities, err := uiParseProjectIssuePriorityFilters(r)
	if err != nil {
		return uiProjectAllQuery{}, err
	}
	sort, err := uiParseProjectIssueSort(r)
	if err != nil {
		return uiProjectAllQuery{}, err
	}
	return uiProjectAllQuery{
		Statuses:    statuses,
		Priorities:  priorities,
		Sort:        sort,
		AssigneeIDs: assigneeIDs,
	}, nil
}

func uiParseProjectIssueStatusFilters(r *http.Request) ([]model.Status, error) {
	raws := r.URL.Query()["status"]
	statuses := make([]model.Status, 0, len(raws))
	seen := make(map[model.Status]struct{}, len(raws))
	for _, raw := range raws {
		status := model.Status(strings.TrimSpace(raw))
		if status == "" {
			continue
		}
		if !status.Valid() {
			return nil, fmt.Errorf("invalid status: %w", errUIBadRequest)
		}
		if _, ok := seen[status]; ok {
			continue
		}
		seen[status] = struct{}{}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func uiParseProjectIssuePriorityFilters(r *http.Request) ([]model.IssuePriority, error) {
	raws := r.URL.Query()["priority"]
	priorities := make([]model.IssuePriority, 0, len(raws))
	seen := make(map[model.IssuePriority]struct{}, len(raws))
	for _, raw := range raws {
		priority := model.IssuePriority(strings.TrimSpace(raw))
		if priority == "" {
			continue
		}
		if !priority.Valid() {
			return nil, fmt.Errorf("invalid priority: %w", errUIBadRequest)
		}
		if _, ok := seen[priority]; ok {
			continue
		}
		seen[priority] = struct{}{}
		priorities = append(priorities, priority)
	}
	return priorities, nil
}

func uiParseProjectIssueSort(r *http.Request) (store.ListIssuesSort, error) {
	sort := store.ListIssuesSort(strings.TrimSpace(r.URL.Query().Get("sort")))
	if sort == "" {
		return uiProjectAllDefaultSort, nil
	}
	switch sort {
	case store.ListIssuesSortCreated, store.ListIssuesSortUpdated, store.ListIssuesSortStatus, store.ListIssuesSortPriority:
		return sort, nil
	default:
		return "", fmt.Errorf("invalid sort: %w", errUIBadRequest)
	}
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

func uiToggleStatuses(statuses []model.Status, status model.Status) ([]model.Status, bool) {
	selected := false
	for _, current := range statuses {
		if current == status {
			selected = true
			break
		}
	}
	if !selected {
		out := make([]model.Status, 0, len(statuses)+1)
		out = append(out, statuses...)
		out = append(out, status)
		return out, false
	}
	out := make([]model.Status, 0, len(statuses)-1)
	for _, current := range statuses {
		if current != status {
			out = append(out, current)
		}
	}
	return out, true
}

func uiTogglePriorities(priorities []model.IssuePriority, priority model.IssuePriority) ([]model.IssuePriority, bool) {
	selected := false
	for _, current := range priorities {
		if current == priority {
			selected = true
			break
		}
	}
	if !selected {
		out := make([]model.IssuePriority, 0, len(priorities)+1)
		out = append(out, priorities...)
		out = append(out, priority)
		return out, false
	}
	out := make([]model.IssuePriority, 0, len(priorities)-1)
	for _, current := range priorities {
		if current != priority {
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

func uiProjectAllIssueCursor(issue model.Issue, sort store.ListIssuesSort) store.IssuesCursor {
	cursor := store.IssuesCursor{Number: issue.Number}
	switch sort {
	case store.ListIssuesSortCreated:
		cursor.CreatedAt = issue.CreatedAt
	case store.ListIssuesSortUpdated:
		cursor.UpdatedAt = issue.UpdatedAt
	case store.ListIssuesSortStatus:
		cursor.Status = issue.Status
	case store.ListIssuesSortPriority:
		cursor.Priority = issue.Priority
	}
	return cursor
}

func uiProjectAllViewPath(project model.Project, query uiProjectAllQuery) string {
	return uiProjectAllPath(uiProjectPath(project)+"/all", query)
}

func uiProjectAllPanelPath(project model.Project, query uiProjectAllQuery) string {
	return uiProjectAllPath(uiProjectPath(project)+"/all/panel", query)
}

func uiProjectAllPagePath(project model.Project, query uiProjectAllQuery) string {
	return uiProjectAllPath(uiProjectPath(project)+"/all/page", query)
}

func uiProjectAllPath(path string, query uiProjectAllQuery) string {
	values := url.Values{}
	for _, status := range query.Statuses {
		values.Add("status", string(status))
	}
	for _, priority := range query.Priorities {
		values.Add("priority", string(priority))
	}
	if query.Sort != "" && query.Sort != uiProjectAllDefaultSort {
		values.Set("sort", string(query.Sort))
	}
	for _, id := range query.AssigneeIDs {
		values.Add("assignee_id", id.String())
	}
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	if len(values) == 0 {
		return path
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

func (s *Server) uiProjectContextFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, model.ProjectContext, bool) {
	project, ok := s.uiProjectFromRoute(w, r)
	if !ok {
		return model.Project{}, model.ProjectContext{}, false
	}
	number, err := parseTypedRef(chi.URLParam(r, "contextRef"), "context")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return model.Project{}, model.ProjectContext{}, false
	}
	contextItem, err := s.store.GetProjectContextByProjectNumber(r.Context(), project.ID, number)
	if err != nil {
		writeUIStoreError(w, err)
		return model.Project{}, model.ProjectContext{}, false
	}
	if contextItem.Scope != model.ProjectContextScopeProject {
		writeUIStoreError(w, store.ErrNotFound)
		return model.Project{}, model.ProjectContext{}, false
	}
	return project, contextItem, true
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

func (s *Server) uiCommentFromRoute(w http.ResponseWriter, r *http.Request, issue model.Issue) (model.Comment, bool) {
	number, err := parseTypedRef(chi.URLParam(r, "commentRef"), "comment")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return model.Comment{}, false
	}
	comment, err := s.store.GetCommentForIssueByNumber(r.Context(), issue.ID, number)
	if err != nil {
		writeUIStoreError(w, err)
		return model.Comment{}, false
	}
	return comment, true
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

func uiProjectContextsPath(project model.Project) string {
	return uiProjectPath(project) + "/context"
}

func uiProjectContextPath(project model.Project, contextItem any) string {
	return uiProjectContextsPath(project) + "/" + uiProjectContextRef(contextItem)
}

func uiProjectContextNewPath(project model.Project) string {
	return uiProjectContextsPath(project) + "/new"
}

func uiProjectContextEditPath(project model.Project, contextItem any) string {
	return uiProjectContextPath(project, contextItem) + "/edit"
}

func uiProjectContextDeletePath(project model.Project, contextItem any) string {
	return uiProjectContextPath(project, contextItem) + "/delete"
}

func uiProjectContextIssuesPath(project model.Project, contextItem any) string {
	return uiProjectContextPath(project, contextItem) + "/issues"
}

func uiProjectContextIssueDeletePath(project model.Project, contextItem any, issue any) string {
	return uiProjectContextIssuesPath(project, contextItem) + "/" + uiIssueValue(issue).Identifier + "/delete"
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

func uiIssueContextPath(issue any) string {
	return uiIssuePath(issue) + "/context"
}

func uiIssueContextNewPath(issue any) string {
	return uiIssueContextPath(issue) + "/new"
}

func uiIssueContextLinkNewPath(issue any) string {
	return uiIssueContextPath(issue) + "/link"
}

func uiIssueContextDeletePath(issue any, contextItem any) string {
	return uiIssueContextPath(issue) + "/" + uiProjectContextRef(contextItem) + "/delete"
}

func uiIssueCommentPath(issue any, comment any) string {
	return uiIssueCommentsPath(issue) + "/" + uiCommentRef(comment)
}

func uiIssueCommentEditPath(issue any, comment any) string {
	return uiIssueCommentPath(issue, comment) + "/edit"
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

func uiIssueCloseReasonPath(issue any) string {
	return uiIssuePath(issue) + "/close-reason"
}

func uiIssueCloseReasonEditPath(issue any) string {
	return uiIssueCloseReasonPath(issue) + "/edit"
}

func uiIssuePriorityPath(issue any) string {
	return uiIssuePath(issue) + "/priority"
}

func uiIssuePriorityEditPath(issue any) string {
	return uiIssuePriorityPath(issue) + "/edit"
}

func uiIssueDueDatePath(issue any) string {
	return uiIssuePath(issue) + "/due-date"
}

func uiIssueDueDateEditPath(issue any) string {
	return uiIssueDueDatePath(issue) + "/edit"
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

func uiIssueSprintPath(issue any) string {
	return uiIssuePath(issue) + "/sprint"
}

func uiIssueSprintEditPath(issue any) string {
	return uiIssueSprintPath(issue) + "/edit"
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

func uiIssueSubIssuesNewPath(issue any) string {
	return uiIssueSubIssuesPath(issue) + "/new"
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

func uiProjectContextRef(v any) string {
	switch contextItem := v.(type) {
	case model.ProjectContext:
		if contextItem.Ref != "" {
			return contextItem.Ref
		}
		if contextItem.Number > 0 {
			return model.ProjectContextRef(contextItem.Number)
		}
	case *model.ProjectContext:
		if contextItem != nil {
			return uiProjectContextRef(*contextItem)
		}
	case model.ProjectContextSummary:
		if contextItem.Ref != "" {
			return contextItem.Ref
		}
		if contextItem.Number > 0 {
			return model.ProjectContextRef(contextItem.Number)
		}
	case *model.ProjectContextSummary:
		if contextItem != nil {
			return uiProjectContextRef(*contextItem)
		}
	}
	return "context-0"
}

func uiCommentRef(v any) string {
	var comment model.Comment
	switch c := v.(type) {
	case model.Comment:
		comment = c
	case *model.Comment:
		if c != nil {
			comment = *c
		}
	}
	if comment.Ref != "" {
		return comment.Ref
	}
	if comment.Number > 0 {
		return model.CommentRef(comment.Number)
	}
	return "comment-0"
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
	case path == "/", path == "/me", path == "/me/panel", path == "/projects", path == "/projects/panel", path == "/projects/new", path == "/projects/new/panel", path == "/settings", path == "/tokens":
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
	if len(parts) < 3 || len(parts) > 8 {
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
	if parts[3] == "context" {
		if len(parts) == 4 {
			return true
		}
		if len(parts) == 5 && parts[4] == "new" {
			return true
		}
		if _, err := parseTypedRef(parts[4], "context"); err != nil {
			return false
		}
		if len(parts) == 5 {
			return true
		}
		if len(parts) == 6 {
			return parts[5] == "edit" || parts[5] == "delete" || parts[5] == "issues"
		}
		if len(parts) == 8 && parts[5] == "issues" && parts[7] == "delete" {
			_, err := parseIssueRef(parts[6])
			return err == nil
		}
		return false
	}
	if parts[3] != "about" && parts[3] != "sprint" && parts[3] != "planned" && parts[3] != "all" && parts[3] != "backlog" && parts[3] != "deleted" {
		return false
	}
	if len(parts) == 4 {
		return true
	}
	return parts[4] == "panel" || (parts[3] == "all" && parts[4] == "page")
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
		return parts[3] == "panel" || parts[3] == "links" || parts[3] == "context" || parts[3] == "delete" || parts[3] == "restore"
	}
	if len(parts) == 5 {
		return ((parts[3] == "description" || parts[3] == "status" || parts[3] == "close-reason" || parts[3] == "priority" || parts[3] == "due-date" || parts[3] == "assignee" || parts[3] == "reporter" || parts[3] == "sprint") && parts[4] == "edit") ||
			(parts[3] == "links" && parts[4] == "new") ||
			(parts[3] == "sub-issues" && parts[4] == "new") ||
			(parts[3] == "context" && (parts[4] == "new" || parts[4] == "link"))
	}
	if parts[3] != "links" || parts[5] != "edit" {
		if parts[3] == "context" && parts[5] == "delete" {
			_, err := parseTypedRef(parts[4], "context")
			return err == nil
		}
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
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "conflict", http.StatusConflict)
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

func uiDueDateValue(d *model.Date) string {
	if d == nil {
		return ""
	}
	return d.String()
}

func uiDueDateShort(d *model.Date) string {
	if d == nil {
		return ""
	}
	return d.Time().Format("Jan 2")
}

func uiDueDateFull(d *model.Date) string {
	if d == nil {
		return ""
	}
	return d.Time().Format("Jan 2, 2006")
}

func uiDueBadgeClass(issue model.Issue) string {
	if uiIssueOverdue(issue, time.Now()) {
		return "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-900/70 dark:bg-rose-950/30 dark:text-rose-200"
	}
	if uiIssueDueSoon(issue, time.Now()) {
		return "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200"
	}
	return "border-slate-200 bg-white text-slate-600 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-300"
}

func uiDueBadgeIcon(issue model.Issue) string {
	if uiIssueDueSoon(issue, time.Now()) {
		return "clock"
	}
	return "calendar"
}

func uiDueBadgeLabel(issue model.Issue) string {
	if issue.DueDate == nil {
		return ""
	}
	if days, ok := uiIssueDueDays(issue, time.Now()); ok && days >= 0 && days < 7 {
		if days == 0 {
			return "Today"
		}
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	return uiDueDateShort(issue.DueDate)
}

func uiIssueOverdue(issue model.Issue, now time.Time) bool {
	days, ok := uiIssueDueDays(issue, now)
	return ok && days < 0
}

func uiIssueDueSoon(issue model.Issue, now time.Time) bool {
	days, ok := uiIssueDueDays(issue, now)
	return ok && days >= 0 && days < 7
}

func uiIssueDueDays(issue model.Issue, now time.Time) (int, bool) {
	if issue.DueDate == nil || issue.Status.CountsAsDone() {
		return 0, false
	}
	current := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	due := issue.DueDate.Time()
	due = time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, current.Location())
	return int(due.Sub(current).Hours() / 24), true
}

func uiStatusLabel(s model.Status) string {
	switch s {
	case model.StatusTodo:
		return "To do"
	case model.StatusInProgress:
		return "In progress"
	case model.StatusDone:
		return "Done"
	case model.StatusClosed:
		return "Closed"
	default:
		return string(s)
	}
}

func uiStatusClass(s model.Status) string {
	switch s {
	case model.StatusTodo:
		return "border-slate-300 bg-slate-100 text-slate-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
	case model.StatusInProgress:
		return "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-500/40 dark:bg-blue-950/40 dark:text-blue-200"
	case model.StatusDone:
		return "border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-500/40 dark:bg-emerald-950/40 dark:text-emerald-200"
	case model.StatusClosed:
		return "border-zinc-300 bg-zinc-100 text-zinc-700 dark:border-zinc-600 dark:bg-zinc-900 dark:text-zinc-200"
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
		{Status: model.StatusClosed, Label: uiStatusLabel(model.StatusClosed)},
	}
}

func uiIssueStatusDropdown(panel *uiIssuePanelData) uiOptionDropdownData {
	options := make([]uiOptionDropdownOption, 0, len(uiStatusOptions()))
	for _, option := range uiStatusOptions() {
		options = append(options, uiOptionDropdownOption{
			Value: string(option.Status),
			Label: option.Label,
			Class: uiStatusClass(option.Status),
		})
	}
	return uiOptionDropdownData{
		Action:       uiIssueStatusPath(panel.Issue),
		HXTarget:     "#main",
		HXPushURL:    uiIssuePath(panel.Issue),
		CancelHXGet:  uiIssuePanelPath(panel.Issue),
		ToggleLabel:  "Change status",
		ListLabel:    "Issue status",
		Name:         "status",
		CurrentValue: string(panel.Issue.Status),
		CurrentLabel: uiStatusLabel(panel.Issue.Status),
		CurrentClass: uiStatusClass(panel.Issue.Status),
		Options:      options,
	}
}

func uiCloseReasonLabel(v any) string {
	var reason model.IssueCloseReason
	switch r := v.(type) {
	case model.IssueCloseReason:
		reason = r
	case *model.IssueCloseReason:
		if r == nil {
			return ""
		}
		reason = *r
	default:
		return fmt.Sprint(v)
	}
	switch reason {
	case model.CloseReasonDuplicate:
		return "Duplicate"
	case model.CloseReasonWontDo:
		return "Won't Do"
	case model.CloseReasonInvalid:
		return "Invalid"
	default:
		return string(reason)
	}
}

type uiCloseReasonOption struct {
	Reason model.IssueCloseReason
	Label  string
}

func uiCloseReasonOptions() []uiCloseReasonOption {
	return []uiCloseReasonOption{
		{Reason: model.CloseReasonDuplicate, Label: uiCloseReasonLabel(model.CloseReasonDuplicate)},
		{Reason: model.CloseReasonWontDo, Label: uiCloseReasonLabel(model.CloseReasonWontDo)},
		{Reason: model.CloseReasonInvalid, Label: uiCloseReasonLabel(model.CloseReasonInvalid)},
	}
}

func uiIssueCloseReasonDropdown(panel *uiIssuePanelData) uiOptionDropdownData {
	currentValue := strings.TrimSpace(panel.CloseReasonInput)
	if currentValue == "" && panel.Issue.CloseReason != nil {
		currentValue = string(*panel.Issue.CloseReason)
	}
	currentReason := model.IssueCloseReason(currentValue)
	currentLabel := "Close reason"
	currentClass := "border-slate-200 bg-white text-slate-700 dark:border-slate-800 dark:bg-slate-950 dark:text-slate-200"
	if currentReason.Valid() {
		currentLabel = uiCloseReasonLabel(currentReason)
		currentClass = uiCloseReasonClass(currentReason)
	}
	options := make([]uiOptionDropdownOption, 0, len(uiCloseReasonOptions()))
	for _, option := range uiCloseReasonOptions() {
		options = append(options, uiOptionDropdownOption{
			Value: string(option.Reason),
			Label: option.Label,
			Class: uiCloseReasonClass(option.Reason),
		})
	}
	return uiOptionDropdownData{
		Action:       uiIssueCloseReasonPath(panel.Issue),
		HXTarget:     "#main",
		HXPushURL:    uiIssuePath(panel.Issue),
		CancelHXGet:  uiIssuePanelPath(panel.Issue),
		ToggleLabel:  "Choose close reason",
		ListLabel:    "Close reason",
		Name:         "close_reason",
		CurrentValue: currentValue,
		CurrentLabel: currentLabel,
		CurrentClass: currentClass,
		Error:        panel.CloseReasonError,
		Options:      options,
	}
}

func uiCloseReasonClass(model.IssueCloseReason) string {
	return "border-zinc-300 bg-white text-zinc-700 dark:border-zinc-700 dark:bg-slate-950 dark:text-zinc-200"
}

func uiCloseReasonModal(panel *uiIssuePanelData) uiModalData {
	return uiModalData{
		ID:              "close-reason",
		Title:           "Close issue",
		Description:     fmt.Sprintf("Choose a reason to close %s.", panel.Issue.Identifier),
		WidthClass:      "max-w-sm",
		CancelLabel:     "Cancel editing close reason",
		CancelHXGet:     uiIssuePanelPath(panel.Issue),
		CancelHXPushURL: uiIssuePath(panel.Issue),
		Badges: []uiModalBadge{
			{
				Label: uiStatusLabel(model.StatusClosed),
				Class: uiStatusClass(model.StatusClosed),
			},
		},
	}
}

func uiProjectContextModal(panel *uiProjectPanelData) uiModalData {
	return uiModalData{
		ID:              "project-context",
		Title:           "Add context",
		WidthClass:      "max-w-2xl",
		CancelLabel:     "Cancel adding context",
		CancelHXGet:     uiProjectPanelPath(panel.Project, "about"),
		CancelHXPushURL: uiProjectViewPath(panel.Project, "about"),
	}
}

func uiIssueContextModal(panel *uiIssuePanelData) uiModalData {
	title := "Add context"
	cancelLabel := "Cancel adding context"
	if panel.ContextMode == "attach" {
		title = "Attach context"
		cancelLabel = "Cancel attaching context"
	}
	if panel.ContextMode == "view" {
		title = "Context"
		cancelLabel = "Close context"
	}
	return uiModalData{
		ID:              "issue-context",
		Title:           title,
		WidthClass:      "max-w-2xl",
		CancelLabel:     cancelLabel,
		CancelHXGet:     uiIssuePanelPath(panel.Issue),
		CancelHXPushURL: uiIssuePath(panel.Issue),
	}
}

type uiSubIssueProgressData struct {
	Total             int
	Todo              int
	InProgress        int
	Done              int
	DonePercent       string
	InProgressPercent string
	TodoPercent       string
	Label             string
}

func uiSubIssueProgress(issues []model.Issue) uiSubIssueProgressData {
	out := uiSubIssueProgressData{Total: len(issues)}
	for _, issue := range issues {
		switch {
		case issue.Status.CountsAsDone():
			out.Done++
		case issue.Status == model.StatusInProgress:
			out.InProgress++
		default:
			out.Todo++
		}
	}
	out.DonePercent = uiPercent(out.Done, out.Total)
	out.InProgressPercent = uiPercent(out.InProgress, out.Total)
	out.TodoPercent = uiPercent(out.Todo, out.Total)
	if out.Total == 0 {
		out.Label = "Sub-issue progress: no sub-issues"
	} else {
		out.Label = fmt.Sprintf("Sub-issue progress: %d done, %d in progress, %d to do", out.Done, out.InProgress, out.Todo)
	}
	return out
}

func uiLinkedIssueProgress(links []uiIssueLinkItem) uiSubIssueProgressData {
	out := uiSubIssueProgressData{}
	for _, link := range links {
		if !link.HasIssue {
			continue
		}
		out.Total++
		switch {
		case link.LinkedIssue.Status.CountsAsDone():
			out.Done++
		case link.LinkedIssue.Status == model.StatusInProgress:
			out.InProgress++
		default:
			out.Todo++
		}
	}
	out.DonePercent = uiPercent(out.Done, out.Total)
	out.InProgressPercent = uiPercent(out.InProgress, out.Total)
	out.TodoPercent = uiPercent(out.Todo, out.Total)
	if out.Total == 0 {
		if len(links) == 0 {
			out.Label = "Linked issue progress: no linked issues"
		} else {
			out.Label = "Linked issue progress: no available linked issues"
		}
	} else {
		out.Label = fmt.Sprintf("Linked issue progress: %d done, %d in progress, %d to do", out.Done, out.InProgress, out.Todo)
	}
	return out
}

func uiIssueColumnCount(columns []uiIssueColumn) int {
	total := 0
	for _, column := range columns {
		total += len(column.Issues)
	}
	return total
}

func uiPercent(part, total int) string {
	if total <= 0 || part <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%.2f%%", (float64(part)/float64(total))*100)
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
		return "bg-blue-50/45 hover:bg-blue-50 dark:bg-blue-950/15 dark:hover:bg-blue-950/30"
	case model.StatusDone:
		return "bg-emerald-50/45 hover:bg-emerald-50 dark:bg-emerald-950/15 dark:hover:bg-emerald-950/30"
	case model.StatusClosed:
		return "bg-zinc-50/70 hover:bg-zinc-100/80 dark:bg-zinc-900/35 dark:hover:bg-zinc-800/70"
	default:
		return "bg-white hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800/60"
	}
}

func uiStatusSurfaceClass(s model.Status) string {
	switch s {
	case model.StatusTodo:
		return "bg-slate-50/70 dark:bg-slate-900/30"
	case model.StatusInProgress:
		return "bg-blue-50/45 dark:bg-blue-950/15"
	case model.StatusDone:
		return "bg-emerald-50/45 dark:bg-emerald-950/15"
	case model.StatusClosed:
		return "bg-zinc-50/70 dark:bg-zinc-900/35"
	default:
		return "bg-white dark:bg-slate-900"
	}
}

func uiIssueColumnStatus(s model.Status) model.Status {
	if s.CountsAsDone() {
		return model.StatusDone
	}
	return s
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
