package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hurseyc/guardtower/internal/fleet"
)

// apiError is the JSON error envelope.
type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// fail maps an error to an HTTP response. Validation-type errors are shown to
// the user; unexpected ones are logged and hidden.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ue userError
	switch {
	case errors.Is(err, fleet.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.As(err, &ue):
		writeErr(w, ue.status, ue.msg)
	default:
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

// userError is an error whose message is safe to return to the client.
type userError struct {
	status int
	msg    string
}

func (e userError) Error() string { return e.msg }

func badRequest(err error) error {
	if err == nil {
		return nil
	}
	return userError{status: http.StatusBadRequest, msg: err.Error()}
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return userError{status: http.StatusBadRequest, msg: "invalid JSON body: " + err.Error()}
	}
	return nil
}

func idParam(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, userError{status: http.StatusBadRequest, msg: "invalid id"}
	}
	return id, nil
}

func queryInt(r *http.Request, key string, def int64) int64 {
	v, err := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	if err != nil {
		return def
	}
	return v
}
