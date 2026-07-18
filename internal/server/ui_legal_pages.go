package server

import (
	"html/template"
	"net/http"

	"github.com/bradleymackey/track-slash/legal"
)

type uiLegalData struct {
	Title   string
	Version string
	HTML    template.HTML
}

func (s *Server) uiTermsPage(w http.ResponseWriter, _ *http.Request) {
	renderUILegalDocument(w, legal.Terms)
}

func (s *Server) uiPrivacyPage(w http.ResponseWriter, _ *http.Request) {
	renderUILegalDocument(w, legal.Privacy)
}

func (s *Server) uiSecurityPage(w http.ResponseWriter, _ *http.Request) {
	renderUILegalDocument(w, legal.Security)
}

func renderUILegalDocument(w http.ResponseWriter, document legal.Document) {
	renderUITemplate(w, http.StatusOK, "legal", uiLegalData{
		Title:   document.Title,
		Version: document.Version,
		HTML:    renderMarkdown(document.Markdown, nil),
	})
}
