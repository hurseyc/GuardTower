package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hurseyc/guardtower/internal/config"
)

func (s *Server) kickSync() { s.Config.Kick() }

func (s *Server) handleConfigSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, config.Sections)
}

func scopeFromRequest(r *http.Request) config.ScopeRef {
	scope := chi.URLParam(r, "scope")
	if scope == "" {
		scope = r.URL.Query().Get("scope")
	}
	idStr := chi.URLParam(r, "scopeID")
	if idStr == "" {
		idStr = r.URL.Query().Get("id")
	}
	id, _ := strconv.ParseInt(idStr, 10, 64)
	ref := config.ScopeRef{Scope: scope, ID: id}
	if scope == config.ScopeBase {
		ref.ID = 0
	}
	return ref
}

func (s *Server) handleGetLayers(w http.ResponseWriter, r *http.Request) {
	ref := scopeFromRequest(r)
	ls, err := s.Config.GetLayers(r.Context(), ref)
	if err != nil {
		s.fail(w, r, badRequest(err))
		return
	}
	writeJSON(w, 200, ls)
}

func (s *Server) handlePutLayer(w http.ResponseWriter, r *http.Request) {
	ref := scopeFromRequest(r)
	section := chi.URLParam(r, "section")
	var d config.Doc
	if err := decode(r, &d); err != nil {
		s.fail(w, r, err)
		return
	}
	u := userFrom(r.Context())
	clean, err := s.Config.SetLayer(r.Context(), ref, section, d, u.Username)
	if err != nil {
		s.fail(w, r, badRequest(err))
		return
	}
	s.audit(r, "config.layer", ref.Scope+"/"+strconv.FormatInt(ref.ID, 10)+"/"+section, map[string]any{"fields": len(clean)})
	s.kickSync()
	writeJSON(w, 200, clean)
}

func (s *Server) handleImportBaseline(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceID int64    `json:"instance_id"`
		Scope      string   `json:"scope"`
		ID         int64    `json:"id"`
		Sections   []string `json:"sections"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	if body.Scope == "" {
		body.Scope = config.ScopeBase
	}
	u := userFrom(r.Context())
	sum, err := s.Config.ImportFromInstance(r.Context(), body.InstanceID, config.ScopeRef{Scope: body.Scope, ID: body.ID}, body.Sections, u.Username)
	if err != nil {
		s.fail(w, r, badRequest(err))
		return
	}
	s.audit(r, "config.import", body.Scope, body)
	s.kickSync()
	writeJSON(w, 200, sum)
}

func (s *Server) handleInstanceConfig(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	states, err := s.Config.Effective(r.Context(), id, r.URL.Query().Get("live") != "0")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, states)
}

func (s *Server) handleListSync(w http.ResponseWriter, r *http.Request) {
	sums, err := s.Config.ListSync(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, sums)
}

func (s *Server) handleCheckSync(w http.ResponseWriter, r *http.Request) {
	s.kickSync()
	writeJSON(w, 202, map[string]bool{"ok": true})
}

func (s *Server) handleExportYAML(w http.ResponseWriter, r *http.Request) {
	b, err := s.Config.ExportYAML(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="guardtower-settings.yaml"`)
	_, _ = w.Write(b)
}

// handleImportYAML takes the raw YAML body. ?dry_run=1 only reports what
// would change; ?prune=1 also deletes layers the file doesn't mention.
func (s *Server) handleImportYAML(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	dry, prune := r.URL.Query().Get("dry_run") == "1", r.URL.Query().Get("prune") == "1"
	u := userFrom(r.Context())
	res, err := s.Config.ImportYAML(r.Context(), data, dry, prune, u.Username)
	if err != nil {
		var ie *config.ImportError
		if errors.As(err, &ie) {
			writeErr(w, http.StatusBadRequest, ie.Msg)
			return
		}
		s.fail(w, r, err)
		return
	}
	if res.Applied {
		s.audit(r, "config.import_yaml", "settings layers", map[string]any{"changes": len(res.Changes), "prune": prune})
	}
	writeJSON(w, 200, res)
}
