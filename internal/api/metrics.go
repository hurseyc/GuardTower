package api

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/hurseyc/guardtower/internal/version"
)

// Prometheus text exposition, written by hand: a handful of gauges doesn't
// justify a client library.

type metricWriter struct {
	b    strings.Builder
	seen map[string]bool
}

func (m *metricWriter) head(name, help, kind string) {
	if m.seen == nil {
		m.seen = map[string]bool{}
	}
	if m.seen[name] {
		return
	}
	m.seen[name] = true
	fmt.Fprintf(&m.b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
}

func labelValue(v string) string {
	r := strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
	return r.Replace(v)
}

func (m *metricWriter) gauge(name, help string, value float64, labels ...string) {
	m.head(name, help, "gauge")
	m.b.WriteString(name)
	if len(labels) > 0 {
		m.b.WriteByte('{')
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				m.b.WriteByte(',')
			}
			fmt.Fprintf(&m.b, `%s="%s"`, labels[i], labelValue(labels[i+1]))
		}
		m.b.WriteByte('}')
	}
	fmt.Fprintf(&m.b, " %g\n", value)
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// handleMetrics serves Prometheus metrics when GT_METRICS_TOKEN is set; the
// scraper must send it as a bearer token. Family DNS data is never exported.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.MetricsToken == "" {
		http.NotFound(w, r)
		return
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(s.MetricsToken)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="guardtower-metrics"`)
		writeErr(w, http.StatusUnauthorized, "metrics require the bearer token from GT_METRICS_TOKEN")
		return
	}
	ctx := r.Context()
	var m metricWriter
	m.gauge("guardtower_build_info", "GuardTower build information.", 1, "version", version.Version, "commit", version.Commit)

	ins, err := s.Fleet.ListInstances(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	groups, _ := s.Fleet.ListGroups(ctx)
	groupName := map[int64]string{}
	for _, g := range groups {
		groupName[g.ID] = g.Name
	}
	since := time.Now().Add(-time.Hour).Unix() / 3600 * 3600
	hourly := map[int64][2]float64{}
	if rows, err := s.Store.DB.QueryContext(ctx, `SELECT instance_id, SUM(queries), SUM(blocked) FROM stats_hourly WHERE hour >= ? GROUP BY instance_id`, since); err == nil {
		for rows.Next() {
			var id int64
			var q, b float64
			if rows.Scan(&id, &q, &b) == nil {
				hourly[id] = [2]float64{q, b}
			}
		}
		rows.Close()
	}
	collected := map[int64][2]float64{}
	if states, err := s.QueryLog.ListStates(ctx); err == nil {
		for _, st := range states {
			collected[st.InstanceID] = [2]float64{float64(st.LastRunAt) / 1000, float64(st.RowsTotal)}
		}
	}
	pending := map[int64][2]float64{}
	if sums, err := s.Config.ListSync(ctx); err == nil {
		for _, sm := range sums {
			pending[sm.InstanceID] = [2]float64{float64(sm.Total), float64(len(sm.Drifted))}
		}
	}
	upgradable := map[int64]bool{}
	if ov, err := s.Upgrade.Overview(ctx); err == nil {
		for _, st := range ov.Instances {
			upgradable[st.InstanceID] = st.UpdateAvailable
		}
	}
	sort.Slice(ins, func(i, j int) bool { return ins[i].Name < ins[j].Name })
	for _, in := range ins {
		if !in.Enabled {
			continue
		}
		l := []string{"instance", in.Name, "group", groupName[in.GroupID]}
		up, latency, protection, ver := false, 0.0, false, ""
		if in.Status != nil {
			up, latency, protection, ver = in.Status.Online, float64(in.Status.LatencyMs), in.Status.ProtectionEnabled, in.Status.Version
		}
		m.gauge("guardtower_instance_up", "1 if the AdGuard Home instance answered its last status check.", b2f(up), l...)
		m.gauge("guardtower_instance_latency_milliseconds", "API latency of the last status check.", latency, l...)
		m.gauge("guardtower_instance_protection_enabled", "1 if filtering is enabled on the instance.", b2f(protection), l...)
		m.gauge("guardtower_instance_info", "AdGuard Home version of the instance.", 1, append(l, "version", ver)...)
		h := hourly[in.ID]
		m.gauge("guardtower_instance_queries_current_hour", "DNS queries answered since the start of the previous hour.", h[0], l...)
		m.gauge("guardtower_instance_blocked_current_hour", "Blocked DNS queries since the start of the previous hour.", h[1], l...)
		c := collected[in.ID]
		m.gauge("guardtower_collector_last_run_timestamp_seconds", "When the query log was last collected from the instance.", c[0], l...)
		m.gauge("guardtower_collector_rows", "Queries stored for the instance.", c[1], l...)
		p := pending[in.ID]
		m.gauge("guardtower_config_pending_changes", "Differences between the instance and its desired settings.", p[0], l...)
		m.gauge("guardtower_config_drifted_sections", "Settings sections changed outside GuardTower.", p[1], l...)
		m.gauge("guardtower_upgrade_available", "1 if a newer AdGuard Home version can be installed.", b2f(upgradable[in.ID]), l...)
	}
	if rep, err := s.Resilience.Report(ctx); err == nil {
		for _, g := range rep.Groups {
			for _, sc := range []string{"redundant", "at_risk", "spof", "down", "unknown"} {
				m.gauge("guardtower_group_redundancy", "1 for the group's current redundancy score.", b2f(g.Score == sc), "group", g.Name, "score", sc)
			}
		}
	}
	m.gauge("guardtower_rollout_active", "1 while a rollout or upgrade is running.", b2f(s.Rollout.ActiveID() != 0))
	m.gauge("guardtower_observe_mode", "1 while observe mode is on.", b2f(s.Rollout.Observing(ctx)))
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(m.b.String()))
}
