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
			AllowedHeaders:   []string{"Content-Type", "If-Match"},
			ExposedHeaders:   []string{"X-Request-ID"},
			AllowCredentials: false,
			MaxAge:           300,
		}))
	}

	r.Get("/healthz", s.healthz)

	// WebSocket endpoint sits outside the request-timeout group: the
	// connection is long-lived and would otherwise be killed mid-stream.
	if s.hub != nil {
		r.Method(http.MethodGet, "/ws", s.hub.Handler(s.corsAllowedOrigins))
	}

	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(15 * time.Second))

		r.Route("/users", func(r chi.Router) {
			r.Post("/", s.createUser)
			r.Get("/", s.listUsers)
			r.Get("/{id}", s.getUser)
		})

		r.Route("/projects", func(r chi.Router) {
			r.Post("/", s.createProject)
			r.Get("/", s.listProjects)
			r.Get("/{id}", s.getProject)
			r.Post("/{projectID}/issues", s.createIssue)
			r.Get("/{projectID}/issues", s.listIssues)
			r.Post("/{projectID}/sprints", s.createSprint)
			r.Get("/{projectID}/sprints", s.listProjectSprints)
		})

		r.Route("/issues", func(r chi.Router) {
			r.Get("/", s.batchIssues)
			r.Get("/{id}", s.getIssue)
			r.Patch("/{id}", s.updateIssue)
			r.Post("/{id}/comments", s.createComment)
			r.Get("/{id}/comments", s.listComments)
			r.Post("/{id}/links", s.createIssueLink)
			r.Get("/{id}/links", s.listIssueLinks)
		})

		r.Route("/comments", func(r chi.Router) {
			r.Get("/{id}", s.getComment)
			r.Patch("/{id}", s.updateComment)
			r.Delete("/{id}", s.deleteComment)
		})

		r.Route("/sprints", func(r chi.Router) {
			r.Get("/{id}", s.getSprint)
			r.Patch("/{id}", s.updateSprint)
			r.Post("/{id}/complete", s.completeSprint)
		})

		r.Route("/issue-links", func(r chi.Router) {
			r.Get("/{id}", s.getIssueLink)
			r.Delete("/{id}", s.deleteIssueLink)
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
