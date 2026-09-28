package server

import (
	"errors"
	"net/http"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

// uiBrandBackdropData renders the "brand-backdrop" template. Preset is empty
// for the default brand backdrop, so pages that don't know the user, such as
// sign-in and the legal pages, render it exactly as before.
type uiBrandBackdropData struct {
	Variant string
	Preset  model.BackgroundPreset
	// OOB swaps the page's backdrop in place from an htmx response.
	OOB bool
}

func uiBrandBackdrop(variant string, preset model.BackgroundPreset, oob bool) uiBrandBackdropData {
	data := uiBrandBackdropData{Variant: variant, OOB: oob}
	if preset := preset.OrDefault(); preset != model.BackgroundIndigo {
		data.Preset = preset
	}
	return data
}

func uiBackgroundPresets() []model.BackgroundPreset {
	return model.BackgroundPresets
}

// uiBackgroundSwatchClass is the dot beside each preset in the picker: the
// preset's main colour.
func uiBackgroundSwatchClass(preset model.BackgroundPreset) string {
	switch preset {
	case model.BackgroundSlate:
		return "bg-slate-400 dark:bg-slate-500"
	case model.BackgroundEmerald:
		return "bg-emerald-500"
	case model.BackgroundSky:
		return "bg-sky-500"
	case model.BackgroundAmber:
		return "bg-amber-500"
	}
	return "bg-indigo-500"
}

func (s *Server) uiUpdateBackground(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := r.ParseForm(); err != nil {
		s.renderUIProfilePanel(w, r, user, &uiProfilePanelData{BackgroundError: "Unable to read form."})
		return
	}
	background := model.BackgroundPreset(r.Form.Get("background"))
	if err := s.store.SetUserBackground(r.Context(), user.ID, background); err != nil {
		if errors.Is(err, store.ErrInvalidBackgroundPreset) {
			s.renderUIProfilePanel(w, r, user, &uiProfilePanelData{BackgroundError: "Choose one of the backgrounds."})
			return
		}
		writeUIStoreError(w, err)
		return
	}
	// An htmx save leaves the rest of the page in place, so the response
	// carries the new backdrop as well.
	s.renderUIProfilePanel(w, r, user, &uiProfilePanelData{BackgroundSaved: true, BackdropOOB: isHTMXRequest(r)})
}
