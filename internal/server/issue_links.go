package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type createIssueLinkReq struct {
	TargetID uuid.UUID      `json:"target_id"`
	LinkType model.LinkType `json:"link_type"`
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
	sourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue id")
		return
	}
	var req createIssueLinkReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.TargetID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "target_id required")
		return
	}
	if !req.LinkType.Valid() {
		writeError(w, http.StatusBadRequest, "invalid link_type")
		return
	}

	link, err := s.store.CreateIssueLink(r.Context(), store.CreateIssueLinkParams{
		SourceID: sourceID,
		TargetID: req.TargetID,
		LinkType: req.LinkType,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, link)
}

func (s *Server) listIssueLinks(w http.ResponseWriter, r *http.Request) {
	issueID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue id")
		return
	}
	links, err := s.store.ListIssueLinksForIssue(r.Context(), issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	out := make([]issueLinkView, 0, len(links))
	for _, l := range links {
		v := issueLinkView{IssueLink: l}
		if l.SourceID == issueID {
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
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getIssueLink(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	link, err := s.store.GetIssueLink(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, link)
}

func (s *Server) deleteIssueLink(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteIssueLink(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
