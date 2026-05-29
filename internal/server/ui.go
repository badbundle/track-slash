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
	"sprintDate":  uiSprintDate,
	"statusLabel": uiStatusLabel,
}).ParseFS(uiTemplateFS, "templates/*.html"))

type uiLoginData struct {
	Error string
	Next  string
}

type uiShellData struct {
	User             model.User
	Projects         []model.Project
	CurrentProjectID uuid.UUID
	ProjectPanel     *uiProjectPanelData
}

type uiProjectPanelData struct {
	Project             model.Project
	ActiveSprint        *model.Sprint
	SprintIssues        []model.Issue
	BacklogIssues       []model.Issue
	SprintIssuesHasMore bool
	BacklogHasMore      bool
}

func (s *Server) mountUIRoutes(r chi.Router) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
	})
	r.Get("/app/login", s.uiLoginPage)
	r.Post("/app/login", s.uiLogin)
	r.Post("/app/logout", s.uiLogout)

	r.Route("/app", func(r chi.Router) {
		r.Use(s.uiAuthMiddleware)
		r.Get("/", s.uiHome)
		r.Get("/projects/{id}", s.uiProjectPage)
		r.Get("/projects/{id}/panel", s.uiProjectPanel)
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
	raw := strings.TrimSpace(r.Form.Get("token"))
	next := safeUINext(r.Form.Get("next"))
	if raw == "" {
		renderUITemplate(w, http.StatusUnauthorized, "login", uiLoginData{Error: "Token required.", Next: next})
		return
	}
	auth, err := s.store.AuthenticateToken(r.Context(), raw)
	if err != nil {
		if errors.Is(err, store.ErrUnauthorized) {
			renderUITemplate(w, http.StatusUnauthorized, "login", uiLoginData{Error: "Token not accepted.", Next: next})
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	cookie := &http.Cookie{
		Name:     uiAuthCookieName,
		Value:    raw,
		Path:     "/app",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
	if auth.Token.ExpiresAt != nil {
		cookie.Expires = *auth.Token.ExpiresAt
	}
	http.SetCookie(w, cookie)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) uiLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     uiAuthCookieName,
		Value:    "",
		Path:     "/app",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	http.Redirect(w, r, "/app/login", http.StatusSeeOther)
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
					Path:     "/app",
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

func (s *Server) uiHome(w http.ResponseWriter, r *http.Request) {
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(projects) > 0 {
		http.Redirect(w, r, "/app/projects/"+projects[0].ID.String(), http.StatusSeeOther)
		return
	}
	renderUITemplate(w, http.StatusOK, "shell", uiShellData{
		User:     currentUser(r),
		Projects: projects,
	})
}

func (s *Server) uiProjectPage(w http.ResponseWriter, r *http.Request) {
	projectID, ok := uiProjectID(w, r)
	if !ok {
		return
	}
	projects, err := s.uiVisibleProjects(r.Context(), currentUser(r))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	panel, err := s.uiBuildProjectPanel(r.Context(), r, projectID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "shell", uiShellData{
		User:             currentUser(r),
		Projects:         projects,
		CurrentProjectID: projectID,
		ProjectPanel:     panel,
	})
}

func (s *Server) uiProjectPanel(w http.ResponseWriter, r *http.Request) {
	projectID, ok := uiProjectID(w, r)
	if !ok {
		return
	}
	panel, err := s.uiBuildProjectPanel(r.Context(), r, projectID)
	if err != nil {
		writeUIStoreError(w, err)
		return
	}
	renderUITemplate(w, http.StatusOK, "project-panel", panel)
}

func (s *Server) uiBuildProjectPanel(ctx context.Context, r *http.Request, projectID uuid.UUID) (*uiProjectPanelData, error) {
	if err := s.uiRequireProjectAccess(ctx, currentUser(r), projectID); err != nil {
		return nil, err
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	activeStatus := model.SprintStatusActive
	activeSprints, _, err := s.store.ListSprints(ctx, store.ListSprintsParams{
		ProjectID: projectID,
		Status:    activeStatus,
		Limit:     1,
	})
	if err != nil {
		return nil, err
	}

	var activeSprint *model.Sprint
	var sprintIssues []model.Issue
	var sprintHasMore bool
	if len(activeSprints) > 0 {
		activeSprint = &activeSprints[0]
		sprintIssues, sprintHasMore, err = s.store.ListIssues(ctx, store.ListIssuesParams{
			ProjectID: projectID,
			SprintID:  &activeSprint.ID,
			Limit:     MaxLimit,
		})
		if err != nil {
			return nil, err
		}
	}
	backlog, backlogHasMore, err := s.store.ListIssues(ctx, store.ListIssuesParams{
		ProjectID: projectID,
		Backlog:   true,
		Limit:     MaxLimit,
	})
	if err != nil {
		return nil, err
	}

	return &uiProjectPanelData{
		Project:             project,
		ActiveSprint:        activeSprint,
		SprintIssues:        sprintIssues,
		BacklogIssues:       backlog,
		SprintIssuesHasMore: sprintHasMore,
		BacklogHasMore:      backlogHasMore,
	}, nil
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
	http.Redirect(w, r, "/app/login?next="+next, http.StatusSeeOther)
}

func safeUINext(raw string) string {
	if raw == "" {
		return "/app"
	}
	if !strings.HasPrefix(raw, "/app") || strings.HasPrefix(raw, "//") {
		return "/app"
	}
	return raw
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
