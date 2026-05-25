package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/bradleymackey/track-slash/internal/store"
)

type Server struct {
	store *store.Store
}

func New(s *store.Store) *Server {
	return &Server{store: s}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Second))

	r.Get("/healthz", s.healthz)

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
	})

	r.Route("/issues", func(r chi.Router) {
		r.Get("/{id}", s.getIssue)
		r.Patch("/{id}", s.updateIssue)
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
