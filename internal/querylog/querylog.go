// Package querylog stores DNS query-log rows collected from AdGuard Home
// instances and answers search and statistics queries over them.
package querylog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hurseyc/guardtower/internal/agh"
	"github.com/hurseyc/guardtower/internal/store"
)

// Status values, derived from AdGuard Home's "reason" field.
const (
	StatusAllowed      = "allowed"
	StatusAllowlisted  = "allowlisted"
	StatusBlocked      = "blocked"
	StatusSafeBrowsing = "safebrowsing"
	StatusParental     = "parental"
	StatusSafeSearch   = "safesearch"
	StatusRewritten    = "rewritten"
)

// Statuses lists every status in display order.
var Statuses = []string{StatusAllowed, StatusAllowlisted, StatusBlocked, StatusSafeBrowsing, StatusParental, StatusSafeSearch, StatusRewritten}

// Classify maps an AdGuard Home reason to a status and whether the query was blocked.
func Classify(reason string) (status string, blocked bool) {
	switch reason {
	case "FilteredBlackList", "FilteredBlockedService", "FilteredInvalid":
		return StatusBlocked, true
	case "FilteredSafeBrowsing":
		return StatusSafeBrowsing, true
	case "FilteredParental":
		return StatusParental, true
	case "FilteredSafeSearch":
		return StatusSafeSearch, false
	case "NotFilteredAllowList", "NotFilteredWhiteList":
		return StatusAllowlisted, false
	case "Rewrite", "RewriteEtcHosts", "RewriteRule", "ReasonRewrite":
		return StatusRewritten, false
	default:
		return StatusAllowed, false
	}
}

// Row is one stored query.
type Row struct {
	ID         int64   `json:"id"`
	InstanceID int64   `json:"instance_id"`
	TS         int64   `json:"ts"`
	Client     string  `json:"client"`
	ClientName string  `json:"client_name"`
	ClientID   string  `json:"client_id,omitempty"`
	Domain     string  `json:"domain"`
	QType      string  `json:"qtype"`
	Status     string  `json:"status"`
	Blocked    bool    `json:"blocked"`
	Reason     string  `json:"reason"`
	FilterID   int64   `json:"filter_id,omitempty"`
	Rule       string  `json:"rule,omitempty"`
	Service    string  `json:"service,omitempty"`
	Upstream   string  `json:"upstream,omitempty"`
	ElapsedMs  float64 `json:"elapsed_ms"`
	Cached     bool    `json:"cached"`
	Proto      string  `json:"proto,omitempty"`
	Answer     string  `json:"answer,omitempty"`
}

// FromEntry converts an AdGuard Home query-log entry.
func FromEntry(instanceID int64, e agh.QueryLogEntry) Row {
	r := Row{
		InstanceID: instanceID,
		TS:         e.Time.UnixMilli(),
		Client:     e.Client,
		ClientID:   e.ClientID,
		Domain:     strings.TrimSuffix(strings.ToLower(e.Question.Name), "."),
		QType:      e.Question.Type,
		Reason:     e.Reason,
		Service:    e.ServiceName,
		Upstream:   e.Upstream,
		Cached:     e.Cached,
		Proto:      e.ClientProto,
	}
	if e.Question.UnicodeName != "" {
		r.Domain = strings.TrimSuffix(strings.ToLower(e.Question.UnicodeName), ".")
	}
	if e.ClientInfo != nil {
		r.ClientName = e.ClientInfo.Name
	}
	r.Status, r.Blocked = Classify(e.Reason)
	if len(e.Rules) > 0 {
		r.FilterID = e.Rules[0].FilterListID
		r.Rule = e.Rules[0].Text
	}
	_, _ = fmt.Sscanf(e.ElapsedMs, "%g", &r.ElapsedMs)
	if len(e.Answer) > 0 {
		vals := make([]string, 0, len(e.Answer))
		for i, a := range e.Answer {
			if i == 4 {
				break
			}
			switch v := a.Value.(type) {
			case string:
				vals = append(vals, v)
			default:
				b, _ := json.Marshal(v)
				vals = append(vals, string(b))
			}
		}
		r.Answer = strings.Join(vals, ", ")
	}
	return r
}

// ReverseDomain reverses the labels of a domain ("www.google.com" → "com.google.www").
func ReverseDomain(d string) string {
	parts := strings.Split(d, ".")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, ".")
}

// Store persists and queries rows.
type Store struct {
	st *store.Store
}

// New creates a query-log store.
func New(st *store.Store) *Store { return &Store{st: st} }

type rollupKey struct {
	instance int64
	hour     int64
}

type rollup struct {
	queries, blocked, cached int64
	elapsed                  float64
	byStatus                 map[string]int64
}

// Insert stores rows, ignoring duplicates, and updates hourly rollups for
// the rows that were new. It returns the rows actually inserted.
func (s *Store) Insert(ctx context.Context, rows []Row) ([]Row, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO query_log (instance_id, ts, client, client_name, client_id, domain, rdomain,
		qtype, status, blocked, reason, filter_id, rule, service, upstream, elapsed_ms, cached, proto, answer)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	inserted := make([]Row, 0, len(rows))
	rolls := map[rollupKey]*rollup{}
	seenDomains := map[string]int64{}
	for _, r := range rows {
		res, err := stmt.ExecContext(ctx, r.InstanceID, r.TS, r.Client, r.ClientName, r.ClientID, r.Domain, ReverseDomain(r.Domain),
			r.QType, r.Status, r.Blocked, r.Reason, r.FilterID, r.Rule, r.Service, r.Upstream, r.ElapsedMs, r.Cached, r.Proto, r.Answer)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		r.ID, _ = res.LastInsertId()
		inserted = append(inserted, r)
		k := rollupKey{r.InstanceID, r.TS / 1000 / 3600 * 3600}
		ru := rolls[k]
		if ru == nil {
			ru = &rollup{byStatus: map[string]int64{}}
			rolls[k] = ru
		}
		ru.byStatus[r.Status]++
		if r.TS > seenDomains[r.Domain] {
			seenDomains[r.Domain] = r.TS
		}
		ru.queries++
		if r.Blocked {
			ru.blocked++
		}
		if r.Cached {
			ru.cached++
		}
		ru.elapsed += r.ElapsedMs
	}
	for k, ru := range rolls {
		b := ru.byStatus
		if _, err := tx.ExecContext(ctx, `INSERT INTO stats_hourly (instance_id, hour, queries, blocked, cached, elapsed_sum,
			allowed, allowlisted, safebrowsing, parental, safesearch, rewritten, filtered)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (instance_id, hour) DO UPDATE SET
			queries = queries + excluded.queries, blocked = blocked + excluded.blocked,
			cached = cached + excluded.cached, elapsed_sum = elapsed_sum + excluded.elapsed_sum,
			allowed = allowed + excluded.allowed, allowlisted = allowlisted + excluded.allowlisted,
			safebrowsing = safebrowsing + excluded.safebrowsing, parental = parental + excluded.parental,
			safesearch = safesearch + excluded.safesearch, rewritten = rewritten + excluded.rewritten,
			filtered = filtered + excluded.filtered`,
			k.instance, k.hour, ru.queries, ru.blocked, ru.cached, ru.elapsed,
			b[StatusAllowed], b[StatusAllowlisted], b[StatusSafeBrowsing], b[StatusParental], b[StatusSafeSearch], b[StatusRewritten], b[StatusBlocked]); err != nil {
			return nil, err
		}
	}
	for d, ts := range seenDomains {
		if _, err := tx.ExecContext(ctx, `INSERT INTO domains (domain, first_seen, last_seen) VALUES (?, ?, ?)
			ON CONFLICT (domain) DO UPDATE SET last_seen = MAX(last_seen, excluded.last_seen)`, d, ts, ts); err != nil {
			return nil, err
		}
	}
	return inserted, tx.Commit()
}

// State is a collector cursor.
type State struct {
	InstanceID int64  `json:"instance_id"`
	LastTS     int64  `json:"last_ts"`
	LastRunAt  int64  `json:"last_run_at"`
	LastError  string `json:"last_error"`
	RowsTotal  int64  `json:"rows_total"`
}

// GetState returns the collector state for an instance (zero value if none).
func (s *Store) GetState(ctx context.Context, instanceID int64) (State, error) {
	st := State{InstanceID: instanceID}
	err := s.st.DB.QueryRowContext(ctx, `SELECT last_ts, last_run_at, last_error, rows_total FROM collector_state WHERE instance_id = ?`, instanceID).
		Scan(&st.LastTS, &st.LastRunAt, &st.LastError, &st.RowsTotal)
	if err == sql.ErrNoRows {
		return st, nil
	}
	return st, err
}

// ListStates returns all collector states.
func (s *Store) ListStates(ctx context.Context) ([]State, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT instance_id, last_ts, last_run_at, last_error, rows_total FROM collector_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []State{}
	for rows.Next() {
		var st State
		if err := rows.Scan(&st.InstanceID, &st.LastTS, &st.LastRunAt, &st.LastError, &st.RowsTotal); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// SaveState records the outcome of a collection run.
func (s *Store) SaveState(ctx context.Context, instanceID, lastTS int64, added int64, runErr error) error {
	msg := ""
	if runErr != nil {
		msg = runErr.Error()
	}
	_, err := s.st.DB.ExecContext(ctx, `INSERT INTO collector_state (instance_id, last_ts, last_run_at, last_error, rows_total)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT (instance_id) DO UPDATE SET
		last_ts = MAX(collector_state.last_ts, excluded.last_ts), last_run_at = excluded.last_run_at,
		last_error = excluded.last_error, rows_total = collector_state.rows_total + excluded.rows_total`,
		instanceID, lastTS, time.Now().UnixMilli(), msg, added)
	return err
}

// Prune deletes raw rows older than rawDays and rollups older than rollupDays.
func (s *Store) Prune(ctx context.Context, rawDays, rollupDays int) (int64, error) {
	cut := time.Now().Add(-time.Duration(rawDays) * 24 * time.Hour).UnixMilli()
	var total int64
	// Delete in chunks so a large backlog doesn't hold the write lock for long.
	for {
		res, err := s.st.DB.ExecContext(ctx, `DELETE FROM query_log WHERE id IN (SELECT id FROM query_log WHERE ts < ? LIMIT 20000)`, cut)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < 20000 {
			break
		}
	}
	hcut := time.Now().Add(-time.Duration(rollupDays)*24*time.Hour).Unix() / 3600 * 3600
	if _, err := s.st.DB.ExecContext(ctx, `DELETE FROM stats_hourly WHERE hour < ?`, hcut); err != nil {
		return total, err
	}
	// Top-N rollups and the domain dictionary follow raw retention.
	if _, err := s.st.DB.ExecContext(ctx, `DELETE FROM stats_hourly_top WHERE hour < ?`, cut/1000); err != nil {
		return total, err
	}
	_, err := s.st.DB.ExecContext(ctx, `DELETE FROM domains WHERE last_seen < ?`, cut)
	return total, err
}
