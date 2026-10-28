package family

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Signal kinds.
const (
	SignalBypass      = "bypass"      // VPN, proxy, private DNS or Tor
	SignalQuiet       = "quiet"       // a device stopped using GuardTower's DNS
	SignalUnprotected = "unprotected" // parental control or safe search is off where a person's device resolves
	SignalAllowed     = "allowed"     // adult sites resolved without being blocked
)

// Signal is a hint that filtering may be bypassed or incomplete.
type Signal struct {
	Kind       string `json:"kind"`
	Severity   string `json:"severity"` // info | warn
	PersonID   int64  `json:"person_id,omitempty"`
	Person     string `json:"person,omitempty"`
	Device     string `json:"device,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
	InstanceID int64  `json:"instance_id,omitempty"`
	Message    string `json:"message"`
	Count      int64  `json:"count,omitempty"`
	LastTS     int64  `json:"last_ts,omitempty"`
}

type protectState struct {
	parental, safeSearch bool
	at                   time.Time
	err                  error
}

func who(person, device, name string) string {
	d := device
	if name != "" && name != device {
		d = name + " (" + device + ")"
	}
	if person != "" {
		return person + "'s device " + d
	}
	return "Device " + d
}

// Signals finds bypass and coverage problems for [from, to).
func (s *Service) Signals(ctx context.Context, from, to int64) ([]Signal, error) {
	out := []Signal{}

	// 1. VPN / proxy / private DNS lookups.
	rows, err := s.st.DB.QueryContext(ctx, `SELECT e.device, MAX(e.device_name), COALESCE(pd.person_id, 0), COALESCE(MAX(p.name), ''), COUNT(*), MAX(e.last_ts), SUM(1 - e.blocked)`+
		eventJoin+`WHERE e.category = ? AND e.first_ts >= ? AND e.first_ts < ? GROUP BY e.device`, Bypass, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sg Signal
		var allowed int64
		if err := rows.Scan(&sg.Device, &sg.DeviceName, &sg.PersonID, &sg.Person, &sg.Count, &sg.LastTS, &allowed); err != nil {
			rows.Close()
			return nil, err
		}
		sg.Kind, sg.Severity = SignalBypass, "warn"
		state := "blocked"
		if allowed > 0 {
			state = fmt.Sprintf("%d not blocked", allowed)
		}
		sg.Message = fmt.Sprintf("%s looked up VPN, proxy or private-DNS services %d time%s (%s). These can be used to get around filtering.", who(sg.Person, sg.Device, sg.DeviceName), sg.Count, plural(sg.Count), state)
		out = append(out, sg)
	}
	rows.Close()

	// 2. Adult sites that resolved without being blocked: a filter gap.
	rows, err = s.st.DB.QueryContext(ctx, `SELECT e.device, MAX(e.device_name), COALESCE(pd.person_id, 0), COALESCE(MAX(p.name), ''), COUNT(*), MAX(e.last_ts), MAX(e.instance_id)`+
		eventJoin+`WHERE e.category = ? AND e.blocked = 0 AND e.first_ts >= ? AND e.first_ts < ? GROUP BY e.device`, Adult, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sg Signal
		if err := rows.Scan(&sg.Device, &sg.DeviceName, &sg.PersonID, &sg.Person, &sg.Count, &sg.LastTS, &sg.InstanceID); err != nil {
			rows.Close()
			return nil, err
		}
		sg.Kind, sg.Severity = SignalAllowed, "warn"
		sg.Message = fmt.Sprintf("%s reached %d adult site%s that were not blocked. Turn on parental control or add an adult-content list for its group.", who(sg.Person, sg.Device, sg.DeviceName), sg.Count, plural(sg.Count))
		out = append(out, sg)
	}
	rows.Close()

	// 3. Assigned devices that went quiet while the rest of the network didn't.
	now := s.Now().UnixMilli()
	var recent int64
	_ = s.st.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM query_log WHERE ts >= ?`, now-24*3600*1000).Scan(&recent)
	if recent > 0 {
		ppl, err := s.People(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range ppl {
			for _, d := range p.Devices {
				var last, week int64
				_ = s.st.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(ts), 0), COUNT(*) FROM query_log WHERE (client = ? OR client_id = ?) AND ts >= ?`,
					d.Device, d.Device, now-8*24*3600*1000).Scan(&last, &week)
				if week >= 50 && last < now-24*3600*1000 {
					out = append(out, Signal{Kind: SignalQuiet, Severity: "warn", PersonID: p.ID, Person: p.Name, Device: d.Device, DeviceName: d.Label, LastTS: last,
						Message: fmt.Sprintf("%s hasn't used your DNS servers for over a day, though others have. It may be switched off, away, or using private DNS or a VPN.", who(p.Name, d.Device, d.Label))})
				}
			}
		}
	}

	// 4. Parental control / safe search off on the instance a person's devices use.
	prow, err := s.st.DB.QueryContext(ctx, `SELECT pd.person_id, p.name, e.instance_id, MAX(e.last_ts) FROM family_events e
		JOIN person_devices pd ON pd.device = e.device JOIN people p ON p.id = pd.person_id
		WHERE e.first_ts >= ? GROUP BY pd.person_id, e.instance_id`, from)
	if err != nil {
		return nil, err
	}
	type pi struct {
		pid  int64
		name string
		inst int64
	}
	var seen []pi
	for prow.Next() {
		var x pi
		var last int64
		if err := prow.Scan(&x.pid, &x.name, &x.inst, &last); err != nil {
			prow.Close()
			return nil, err
		}
		seen = append(seen, x)
	}
	prow.Close()
	// One signal per person, naming every instance where protection is off.
	type gap struct {
		name  string
		insts []string
		what  map[string]bool
	}
	gaps := map[int64]*gap{}
	var order []int64
	for _, x := range seen {
		st := s.protection(ctx, x.inst)
		if st.err != nil || (st.parental && st.safeSearch) {
			continue
		}
		g := gaps[x.pid]
		if g == nil {
			g = &gap{name: x.name, what: map[string]bool{}}
			gaps[x.pid] = g
			order = append(order, x.pid)
		}
		name := fmt.Sprintf("#%d", x.inst)
		if in, err := s.fleet.GetInstance(ctx, x.inst); err == nil {
			name = in.Name
		}
		g.insts = append(g.insts, name)
		if !st.parental {
			g.what["parental control"] = true
		}
		if !st.safeSearch {
			g.what["safe search"] = true
		}
	}
	for _, pid := range order {
		g := gaps[pid]
		var what []string
		for _, k := range []string{"parental control", "safe search"} {
			if g.what[k] {
				what = append(what, k)
			}
		}
		sort.Strings(g.insts)
		out = append(out, Signal{Kind: SignalUnprotected, Severity: "info", PersonID: pid, Person: g.name,
			Message: fmt.Sprintf("%s's devices use %s, where %s %s off. Apply the Family profile to the group to turn %s on.",
				g.name, joinAnd(g.insts), joinAnd(what), isAre(len(what)), itThem(len(what)))})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Severity == "warn" && out[j].Severity != "warn" })
	return out, nil
}

// protection reads parental control and safe search state, cached for a minute.
func (s *Service) protection(ctx context.Context, instanceID int64) protectState {
	s.mu.Lock()
	st, ok := s.protect[instanceID]
	s.mu.Unlock()
	if ok && time.Since(st.at) < time.Minute {
		return st
	}
	st = protectState{at: time.Now()}
	in, err := s.fleet.GetInstance(ctx, instanceID)
	if err == nil {
		c, cerr := s.fleet.Client(in)
		if cerr != nil {
			err = cerr
		} else {
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			var p, ss struct {
				Enabled bool `json:"enabled"`
			}
			if err = c.Get(cctx, "parental/status", &p); err == nil {
				err = c.Get(cctx, "safesearch/status", &ss)
			}
			cancel()
			st.parental, st.safeSearch = p.Enabled, ss.Enabled
		}
	}
	st.err = err
	s.mu.Lock()
	s.protect[instanceID] = st
	s.mu.Unlock()
	return st
}

func plural(n int64) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func joinAnd(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

func itThem(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}
