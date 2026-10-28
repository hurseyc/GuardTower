package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hurseyc/guardtower/internal/collector"
	"github.com/hurseyc/guardtower/internal/querylog"
)

// filterFromRequest builds a query-log filter from URL parameters:
// instance (repeatable or comma list), group, tag, client, domain, contains,
// status (comma list), blocked=1, from/to (unix ms) or range (e.g. 24h, 7d).
func (s *Server) filterFromRequest(r *http.Request) (querylog.Filter, error) {
	q := r.URL.Query()
	f := querylog.Filter{
		Client:      q.Get("client"),
		Domain:      q.Get("domain"),
		Contains:    q.Get("contains"),
		BlockedOnly: q.Get("blocked") == "1",
	}
	f.Statuses = append(f.Statuses, splitList(q["status"])...)
	ids := map[int64]bool{}
	for _, v := range splitList(q["instance"]) {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return f, userError{status: 400, msg: "invalid instance id"}
		}
		ids[id] = true
	}
	groupID, _ := strconv.ParseInt(q.Get("group"), 10, 64)
	tag := strings.ToLower(strings.TrimSpace(q.Get("tag")))
	if groupID > 0 || tag != "" {
		insts, err := s.Fleet.ListInstances(r.Context())
		if err != nil {
			return f, err
		}
		matched := false
		for _, in := range insts {
			if groupID > 0 && in.GroupID != groupID {
				continue
			}
			if tag != "" && !contains(in.Tags, tag) {
				continue
			}
			if len(ids) > 0 && !ids[in.ID] {
				continue
			}
			f.InstanceIDs = append(f.InstanceIDs, in.ID)
			matched = true
		}
		if !matched {
			f.InstanceIDs = []int64{-1} // nothing matches
		}
	} else {
		for id := range ids {
			f.InstanceIDs = append(f.InstanceIDs, id)
		}
	}
	now := time.Now().UnixMilli()
	f.From, _ = strconv.ParseInt(q.Get("from"), 10, 64)
	f.To, _ = strconv.ParseInt(q.Get("to"), 10, 64)
	if rg := q.Get("range"); rg != "" {
		d, err := parseRange(rg)
		if err != nil {
			return f, userError{status: 400, msg: err.Error()}
		}
		f.To = now
		f.From = now - d.Milliseconds()
	}
	return f, nil
}

func parseRange(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n < 1 || n > 3660 {
			return 0, errBadRange
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, errBadRange
	}
	return d, nil
}

var errBadRange = userError{status: 400, msg: "invalid range: use e.g. 1h, 24h, 7d"}

func splitList(vals []string) []string {
	var out []string
	for _, v := range vals {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Server) handleQueryLog(w http.ResponseWriter, r *http.Request) {
	f, err := s.filterFromRequest(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var cur *querylog.Cursor
	if ts, id := queryInt(r, "after_ts", 0), queryInt(r, "after_id", 0); ts > 0 && id > 0 {
		cur = &querylog.Cursor{TS: ts, ID: id}
	}
	start := time.Now()
	page, err := s.QueryLog.Search(r.Context(), f, cur, int(queryInt(r, "limit", 100)))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("X-Query-Time-Ms", strconv.FormatInt(time.Since(start).Milliseconds(), 10))
	writeJSON(w, 200, page)
}

func (s *Server) handleStatsSummary(w http.ResponseWriter, r *http.Request) {
	f, err := s.filterFromRequest(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sum, err := s.QueryLog.Summarize(r.Context(), f, int(queryInt(r, "top", 10)))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, sum)
}

func (s *Server) handleCollectorStates(w http.ResponseWriter, r *http.Request) {
	states, err := s.QueryLog.ListStates(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, states)
}

// ---- settings ----

// settingDefs lists the settings editable through the API, with validation.
// Every key here must be documented in docs/admin/settings.md.
var settingDefs = map[string]struct {
	def      string
	min, max int
}{
	collector.SettingRetentionDays:       {strconv.Itoa(collector.DefaultRetentionDays), 1, 3650},
	collector.SettingRollupRetentionDays: {strconv.Itoa(collector.DefaultRollupRetentionDays), 7, 3650},
}

// SettingKeys returns the editable setting keys (used by the docs test).
func SettingKeys() []string {
	keys := make([]string, 0, len(settingDefs))
	for k := range settingDefs {
		keys = append(keys, k)
	}
	return keys
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	out := map[string]string{}
	for k, d := range settingDefs {
		v, err := s.Store.GetSetting(r.Context(), k, d.def)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out[k] = v
	}
	var size int64
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&size)
	var rows int64
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM query_log`).Scan(&rows)
	writeJSON(w, 200, map[string]any{"settings": out, "database_bytes": size, "querylog_rows": rows})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	for k, v := range body {
		d, ok := settingDefs[k]
		if !ok {
			writeErr(w, 400, "unknown setting "+k)
			return
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < d.min || n > d.max {
			writeErr(w, 400, k+" must be a whole number between "+strconv.Itoa(d.min)+" and "+strconv.Itoa(d.max))
			return
		}
	}
	for k, v := range body {
		if err := s.Store.SetSetting(r.Context(), k, strings.TrimSpace(v)); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	s.audit(r, "settings.update", "", body)
	s.handleGetSettings(w, r)
}
