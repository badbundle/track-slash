package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/bradleymackey/track-slash/internal/realtime"
	"github.com/bradleymackey/track-slash/internal/store"
)

type Server struct {
	store              *store.Store
	hub                *realtime.Hub
	corsAllowedOrigins []string
}

// New constructs a Server. corsAllowedOrigins is a list of exact origins
// (scheme + host + port) allowed by CORS and by the WebSocket origin check;
// a nil/empty slice disables CORS entirely and leaves the WS open (dev mode).
func New(s *store.Store, hub *realtime.Hub, corsAllowedOrigins []string) *Server {
	return &Server{
		store:              s,
		hub:                hub,
		corsAllowedOrigins: corsAllowedOrigins,
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(exposeRequestID)
	if len(s.corsAllowedOrigins) > 0 {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   s.corsAllowedOrigins,
			AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Authorization", "Content-Type", "If-Match"},
			ExposedHeaders:   []string{"X-Request-ID"},
			AllowCredentials: false,
			MaxAge:           300,
		}))
	}

	s.mountUIRoutes(r)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/healthz", s.healthz)
		r.Post("/accounts", s.createAccount)
		r.Post("/session", s.createSession)

		// WebSocket endpoint sits outside the request-timeout group: the
		// connection is long-lived and would otherwise be killed mid-stream.
		if s.hub != nil {
			r.Method(http.MethodGet, "/ws", s.authMiddleware(s.hub.Handler(s.corsAllowedOrigins, s.authorizeTopic)))
		}

		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)
			r.Use(middleware.Timeout(15 * time.Second))

			r.Get("/me", s.getMe)
			r.Patch("/me/settings", s.updateMySettings)
			r.Get("/me/tokens", s.listMyTokens)
			r.Post("/me/tokens", s.createMyToken)
			r.Delete("/me/tokens/{id}", s.revokeMyToken)
			r.Delete("/tokens/{id}", s.revokeToken)

			r.Route("/users", func(r chi.Router) {
				r.Post("/", s.createUser)
				r.Get("/", s.listUsers)
				r.Get("/{id}", s.getUser)
				r.Delete("/{id}", s.deleteUser)
				r.Post("/{id}/tokens", s.createUserToken)
				r.Get("/{id}/tokens", s.listUserTokens)
			})

			r.Route("/projects", func(r chi.Router) {
				r.Post("/", s.createProject)
				r.Get("/", s.listProjects)
			})

			r.Route("/{owner}", func(r chi.Router) {
				r.Route("/projects/{key}", func(r chi.Router) {
					r.Get("/", s.getProject)
					r.Delete("/", s.deleteProject)
					r.Get("/members", s.listProjectMembers)
					r.Put("/members/{username}", s.grantProjectMember)
					r.Delete("/members/{username}", s.revokeProjectMember)
					r.Post("/issues", s.createIssue)
					r.Get("/issues", s.listIssues)
					r.Patch("/sprints/planned-order", s.reorderPlannedSprints)
					r.Post("/sprints", s.createSprint)
					r.Get("/sprints", s.listProjectSprints)
					r.Get("/sprints/{sprintRef}", s.getSprint)
					r.Patch("/sprints/{sprintRef}", s.updateSprint)
					r.Delete("/sprints/{sprintRef}", s.deleteSprint)
					r.Post("/sprints/{sprintRef}/complete", s.completeSprint)
					r.Get("/links/{linkRef}", s.getIssueLink)
					r.Delete("/links/{linkRef}", s.deleteIssueLink)
				})
				r.Route("/issues", func(r chi.Router) {
					r.Get("/", s.batchIssues)
					r.Get("/{issueRef}", s.getIssue)
					r.Patch("/{issueRef}", s.updateIssue)
					r.Delete("/{issueRef}", s.deleteIssue)
					r.Post("/{issueRef}/sub-issues", s.createSubIssue)
					r.Get("/{issueRef}/sub-issues", s.listSubIssues)
					r.Post("/{issueRef}/comments", s.createComment)
					r.Get("/{issueRef}/comments", s.listComments)
					r.Get("/{issueRef}/comments/{commentRef}", s.getComment)
					r.Patch("/{issueRef}/comments/{commentRef}", s.updateComment)
					r.Delete("/{issueRef}/comments/{commentRef}", s.deleteComment)
					r.Post("/{issueRef}/links", s.createIssueLink)
					r.Get("/{issueRef}/links", s.listIssueLinks)
				})
			})
		})
	})

	return r
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "db unreachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// exposeRequestID copies chi's RequestID context value into the response so
// clients can quote it back in bug reports. Cheap and aids debugging.
func exposeRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := middleware.GetReqID(r.Context()); id != "" {
			w.Header().Set("X-Request-ID", id)
		}
		next.ServeHTTP(w, r)
	})
}
