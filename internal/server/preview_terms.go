package server

import (
	"errors"

	"github.com/bradleymackey/track-slash/legal"
)

var errPreviewTermsAcceptanceRequired = errors.New("you must agree to the Preview Terms and acknowledge the Privacy Notice")

func (s *Server) previewTermsVersion(accepted bool) (string, error) {
	if !s.previewTermsRequired {
		return "", nil
	}
	if !accepted {
		return "", errPreviewTermsAcceptanceRequired
	}
	return legal.PreviewTermsVersion, nil
}
