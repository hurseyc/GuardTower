package querylog

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ClientFlow is how many queries one client sent to one instance.
type ClientFlow struct {
	Client     string `json:"client"` // "" = all other clients
	Label      string `json:"label"`
	InstanceID int64  `json:"instance_id"`
	Count      int64  `json:"count"`
}

// OutcomeFlow is how many of an instance's queries ended with a status.
type OutcomeFlow struct {
	InstanceID int64  `json:"instance_id"`
	Status     string `json:"status"`
	Count      int64  `json:"count"`
}

// Flows describes traffic from clients through instances to outcomes.
type Flows struct {
	From        int64         `json:"from"`
	To          int64         `json:"to"`
	Clients     []ClientFlow  `json:"clients"`
	Outcomes    []OutcomeFlow `json:"outcomes"`
	FromRollups bool          `json:"from_rollups"`
}

// statusColumns maps stats_hourly columns to query statuses.
var statusColumns = []struct{ col, status string }{
	{"allowed", "allowed"}, {"allowlisted", "allowlisted"}, {"filtered", "blocked"}, {"safebrowsing", "safebrowsing"},
	{"parental", "parental"}, {"safesearch", "safesearch"}, {"rewritten", "rewritten"},
}

// Flows returns the top clients per instance and each instance's outcomes.
// Ranges over a day use hourly rollups; only the top clients of each hour are
// kept there, so the remainder is reported as "other clients".
func (s *Store) Flows(ctx context.Context, f Filter, topClients int) (*Flows, error) {
	if f.To == 0 {
		f.To = time.Now().UnixMilli()
	}
	if f.From == 0 {
		f.From = f.To - 24*3600*1000
	}
	if topClients <= 0 || topClients > 50 {
		topClients = 8
	}
	out := &Flows{From: f.From, To: f.To, Clients: []ClientFlow{}, Outcomes: []OutcomeFlow{}}
	var clients []ClientFlow
	totals := map[int64]int64{}
	if f.To-f.From > 24*3600*1000 {
		out.FromRollups = true
		conds := []string{"hour >= ?", "hour < ?"}
		args := []any{f.From / 1000 / 3600 * 3600, f.To / 1000}
		if len(f.InstanceIDs) > 0 {
			conds = append(conds, "instance_id IN ("+placeholders(len(f.InstanceIDs))+")")
			for _, id := range f.InstanceIDs {
				args = append(args, id)
			}
		}
		where := strings.Join(conds, " AND ")
		// Per-hour top clients exist for completed hours; the rest comes from raw rows.
		done, err := s.topsDoneUntil(ctx)
		if err != nil {
			return nil, err
		}
		rows, err := s.st.DB.QueryContext(ctx, `SELECT key, MAX(label), instance_id, SUM(count) FROM stats_hourly_top WHERE kind = 'client' AND hour < ? AND `+where+` GROUP BY key, instance_id`,
			append([]any{done}, args...)...)
		if err != nil {
			return nil, err
		}
		if clients, err = scanClients(rows, clients); err != nil {
			return nil, err
		}
		tail := f
		if tail.From < done*1000 {
			tail.From = done * 1000
		}
		if tail.From < tail.To {
			tw, targs := tail.where()
			rows, err := s.st.DB.QueryContext(ctx, `SELECT client, MAX(client_name), instance_id, COUNT(*) FROM query_log WHERE `+tw+` GROUP BY client, instance_id`, targs...)
			if err != nil {
				return nil, err
			}
			if clients, err = scanClients(rows, clients); err != nil {
				return nil, err
			}
		}
		cols := make([]string, len(statusColumns))
		for i, c := range statusColumns {
			cols[i] = "COALESCE(SUM(" + c.col + "),0)"
		}
		orows, err := s.st.DB.QueryContext(ctx, `SELECT instance_id, COALESCE(SUM(queries),0), `+strings.Join(cols, ", ")+` FROM stats_hourly WHERE `+where+` GROUP BY instance_id`, args...)
		if err != nil {
			return nil, err
		}
		defer orows.Close()
		for orows.Next() {
			var id, total int64
			vals := make([]int64, len(statusColumns))
			dest := []any{&id, &total}
			for i := range vals {
				dest = append(dest, &vals[i])
			}
			if err := orows.Scan(dest...); err != nil {
				return nil, err
			}
			totals[id] = total
			for i, v := range vals {
				if v > 0 {
					out.Outcomes = append(out.Outcomes, OutcomeFlow{InstanceID: id, Status: statusColumns[i].status, Count: v})
				}
			}
		}
		if err := orows.Err(); err != nil {
			return nil, err
		}
	} else {
		where, args := f.where()
		rows, err := s.st.DB.QueryContext(ctx, `SELECT client, MAX(client_name), instance_id, COUNT(*) FROM query_log WHERE `+where+` GROUP BY client, instance_id`, args...)
		if err != nil {
			return nil, err
		}
		if clients, err = scanClients(rows, clients); err != nil {
			return nil, err
		}
		for _, c := range clients {
			totals[c.InstanceID] += c.Count
		}
		orows, err := s.st.DB.QueryContext(ctx, `SELECT instance_id, status, COUNT(*) FROM query_log WHERE `+where+` GROUP BY instance_id, status`, args...)
		if err != nil {
			return nil, err
		}
		defer orows.Close()
		for orows.Next() {
			var o OutcomeFlow
			if err := orows.Scan(&o.InstanceID, &o.Status, &o.Count); err != nil {
				return nil, err
			}
			out.Outcomes = append(out.Outcomes, o)
		}
		if err := orows.Err(); err != nil {
			return nil, err
		}
	}

	// The same client can appear in both the rollups and the raw tail.
	merged := map[[2]string]*ClientFlow{}
	var order [][2]string
	for _, c := range clients {
		k := [2]string{c.Client, strconv.FormatInt(c.InstanceID, 10)}
		if m, ok := merged[k]; ok {
			m.Count += c.Count
			if m.Label == "" {
				m.Label = c.Label
			}
			continue
		}
		cp := c
		merged[k] = &cp
		order = append(order, k)
	}
	clients = clients[:0]
	for _, k := range order {
		clients = append(clients, *merged[k])
	}

	// Keep the busiest clients overall; fold the rest into "other clients".
	perClient := map[string]int64{}
	for _, c := range clients {
		perClient[c.Client] += c.Count
	}
	keys := make([]string, 0, len(perClient))
	for k := range perClient {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if perClient[keys[i]] != perClient[keys[j]] {
			return perClient[keys[i]] > perClient[keys[j]]
		}
		return keys[i] < keys[j]
	})
	keep := map[string]bool{}
	for i, k := range keys {
		if i < topClients {
			keep[k] = true
		}
	}
	kept := map[int64]int64{}
	for _, c := range clients {
		if keep[c.Client] {
			out.Clients = append(out.Clients, c)
			kept[c.InstanceID] += c.Count
		}
	}
	for id, total := range totals {
		if rest := total - kept[id]; rest > 0 {
			out.Clients = append(out.Clients, ClientFlow{Client: "", Label: "Other clients", InstanceID: id, Count: rest})
		}
	}
	sort.SliceStable(out.Clients, func(i, j int) bool {
		if out.Clients[i].InstanceID != out.Clients[j].InstanceID {
			return out.Clients[i].InstanceID < out.Clients[j].InstanceID
		}
		return out.Clients[i].Count > out.Clients[j].Count
	})
	return out, nil
}

func scanClients(rows *sql.Rows, into []ClientFlow) ([]ClientFlow, error) {
	defer rows.Close()
	for rows.Next() {
		var c ClientFlow
		if err := rows.Scan(&c.Client, &c.Label, &c.InstanceID, &c.Count); err != nil {
			return into, err
		}
		into = append(into, c)
	}
	return into, rows.Err()
}
