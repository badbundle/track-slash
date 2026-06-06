package server

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type createIssueLinkReq struct {
	TargetIssue string         `json:"target_issue"`
	LinkType    model.LinkType `json:"link_type"`
}

// issueLinkView wraps a stored link with direction-aware fields so clients
// render "is blocked by FOO-12" without recomputing the inverse name.
type issueLinkView struct {
	model.IssueLink
	Direction    string    `json:"direction"`
	DisplayType  string    `json:"display_type"`
	OtherIssueID uuid.UUID `json:"other_issue_id"`
}

func (s *Server) createIssueLink(w http.ResponseWriter, r *http.Request) {
	source, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, source.ProjectID) {
		return
	}
	var req createIssueLinkReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	targetRef, err := parseIssueRef(req.TargetIssue)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !req.LinkType.Valid() {
		writeError(w, http.StatusBadRequest, "invalid link_type")
		return
	}
	target, err := s.store.GetIssueByOwnerKeyNumber(r.Context(), source.OwnerUsername, targetRef.ProjectKey, targetRef.Number)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	link, err := s.store.CreateIssueLink(r.Context(), store.CreateIssueLinkParams{
		SourceID: source.ID,
		TargetID: target.ID,
		LinkType: req.LinkType,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, link)
}

func (s *Server) listIssueLinks(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, issue.ProjectID) {
		return
	}

	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cursor *store.IssueLinksCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var c store.IssueLinksCursor
		if err := decodeCursor(raw, &c); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &c
	}

	links, hasMore, err := s.store.ListIssueLinksForIssue(r.Context(), store.ListIssueLinksForIssueParams{
		IssueID: issue.ID,
		Cursor:  cursor,
		Limit:   limit,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	out := make([]issueLinkView, 0, len(links))
	for _, l := range links {
		v := issueLinkView{IssueLink: l}
		if l.SourceID == issue.ID {
			v.Direction = "outgoing"
			v.DisplayType = outgoingDisplayName(l.LinkType)
			v.OtherIssueID = l.TargetID
		} else {
			v.Direction = "incoming"
			v.DisplayType = incomingDisplayName(l.LinkType)
			v.OtherIssueID = l.SourceID
		}
		out = append(out, v)
	}
	var next *string
	if hasMore {
		last := links[len(links)-1]
		enc := encodeCursor(store.IssueLinksCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		next = &enc
	}
	writePage(w, out, next)
}

func (s *Server) getIssueLink(w http.ResponseWriter, r *http.Request) {
	project, link, ok := s.issueLinkFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	writeJSON(w, http.StatusOK, link)
}

func (s *Server) deleteIssueLink(w http.ResponseWriter, r *http.Request) {
	project, link, ok := s.issueLinkFromRoute(w, r)
	if !ok {
		return
	}
	if !s.requireProjectAccess(w, r, project.ID) {
		return
	}
	if err := s.store.DeleteIssueLink(r.Context(), link.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) issueLinkFromRoute(w http.ResponseWriter, r *http.Request) (model.Project, model.IssueLink, bool) {
	project, ok := s.projectFromRoute(w, r)
	if !ok {
		return model.Project{}, model.IssueLink{}, false
	}
	number, ok := parseTypedRefParam(w, r, "linkRef", "link")
	if !ok {
		return model.Project{}, model.IssueLink{}, false
	}
	link, err := s.store.GetIssueLinkByProjectNumber(r.Context(), project.ID, number)
	if err != nil {
		writeStoreError(w, err)
		return model.Project{}, model.IssueLink{}, false
	}
	return project, link, true
}

func outgoingDisplayName(t model.LinkType) string {
	return string(t)
}

func incomingDisplayName(t model.LinkType) string {
	switch t {
	case model.LinkTypeBlocks:
		return "is_blocked_by"
	case model.LinkTypeDuplicates:
		return "is_duplicated_by"
	case model.LinkTypeClones:
		return "is_cloned_by"
	case model.LinkTypeRelatesTo:
		return "relates_to"
	}
	return string(t) // defensive: handler validates LinkType before this is called
}
