package server

import (
	"errors"
	"log"
	"net/http"

	"github.com/bradleymackey/track-slash/internal/store"
)

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

func logInternalError(source string, err error) {
	if err == nil {
		return
	}
	log.Printf("internal error: source=%q type=%T error=%q", source, err, err.Error())
}

func writeInternalError(w http.ResponseWriter, source string, err error) {
	logInternalError(source, err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeUIInternalError(w http.ResponseWriter, source string, err error) {
	logInternalError(source, err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrUnauthorized):
		writeUnauthorized(w)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeInternalError(w, "store", err)
	}
}
