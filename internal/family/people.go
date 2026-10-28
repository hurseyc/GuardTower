package family

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
)

// Person is someone whose devices are watched.
type Person struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Emoji     string   `json:"emoji"`
	Color     string   `json:"color"`
	Devices   []Device `json:"devices"`
	CreatedAt int64    `json:"created_at"`
}

// Device is a client identifier assigned to a person.
type Device struct {
	ID       int64  `json:"id"`
	PersonID int64  `json:"person_id"`
	Device   string `json:"device"`
	Label    string `json:"label"`
}

// People returns everyone with their devices.
func (s *Service) People(ctx context.Context) ([]Person, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT id, name, emoji, color, created_at FROM people ORDER BY name`)
	if err != nil {
		return nil, err
	}
	out := []Person{}
	idx := map[int64]int{}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.Name, &p.Emoji, &p.Color, &p.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		p.Devices = []Device{}
		idx[p.ID] = len(out)
		out = append(out, p)
	}
	rows.Close()
	drows, err := s.st.DB.QueryContext(ctx, `SELECT id, person_id, device, label FROM person_devices ORDER BY label, device`)
	if err != nil {
		return nil, err
	}
	defer drows.Close()
	for drows.Next() {
		var d Device
		if err := drows.Scan(&d.ID, &d.PersonID, &d.Device, &d.Label); err != nil {
			return nil, err
		}
		if i, ok := idx[d.PersonID]; ok {
			out[i].Devices = append(out[i].Devices, d)
		}
	}
	return out, drows.Err()
}

// SavePerson creates (id 0) or updates a person.
func (s *Service) SavePerson(ctx context.Context, id int64, name, emoji, color string) (*Person, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 {
		return nil, &BadInput{"name is required (up to 60 characters)"}
	}
	if len([]rune(emoji)) > 4 {
		return nil, &BadInput{"emoji must be a single emoji"}
	}
	var err error
	if id == 0 {
		var res sql.Result
		res, err = s.st.DB.ExecContext(ctx, `INSERT INTO people (name, emoji, color, created_at) VALUES (?, ?, ?, ?)`, name, emoji, color, s.Now().UnixMilli())
		if err == nil {
			id, _ = res.LastInsertId()
		}
	} else {
		var res sql.Result
		res, err = s.st.DB.ExecContext(ctx, `UPDATE people SET name = ?, emoji = ?, color = ? WHERE id = ?`, name, emoji, color, id)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				return nil, ErrNotFound
			}
		}
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, &BadInput{"someone called " + name + " already exists"}
		}
		return nil, err
	}
	ps, err := s.People(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, ErrNotFound
}

// DeletePerson removes a person; their devices become unassigned.
func (s *Service) DeletePerson(ctx context.Context, id int64) error {
	_, err := s.st.DB.ExecContext(ctx, `DELETE FROM people WHERE id = ?`, id)
	return err
}

// AssignDevice gives a device to a person (moving it if it belonged to
// someone else). personID 0 unassigns it.
func (s *Service) AssignDevice(ctx context.Context, device, label string, personID int64) error {
	device = strings.TrimSpace(device)
	if device == "" {
		return &BadInput{"device is required"}
	}
	if personID == 0 {
		_, err := s.st.DB.ExecContext(ctx, `DELETE FROM person_devices WHERE device = ?`, device)
		return err
	}
	var one int
	if err := s.st.DB.QueryRowContext(ctx, `SELECT 1 FROM people WHERE id = ?`, personID).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	_, err := s.st.DB.ExecContext(ctx, `INSERT INTO person_devices (person_id, device, label) VALUES (?, ?, ?)
		ON CONFLICT (device) DO UPDATE SET person_id = excluded.person_id, label = CASE WHEN excluded.label = '' THEN person_devices.label ELSE excluded.label END`,
		personID, device, strings.TrimSpace(label))
	return err
}

// Client is a device seen in the query log, for the assignment inbox.
type Client struct {
	Device   string `json:"device"`
	Name     string `json:"name"`
	Queries  int64  `json:"queries"`
	LastSeen int64  `json:"last_seen"`
	PersonID int64  `json:"person_id,omitempty"`
	Events   int64  `json:"events"`
}

// Clients lists devices seen in the last days, busiest first.
func (s *Service) Clients(ctx context.Context, days int) ([]Client, error) {
	since := s.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	rows, err := s.st.DB.QueryContext(ctx, `SELECT CASE WHEN client_id != '' THEN client_id ELSE client END AS dev, MAX(client_name), COUNT(*), MAX(ts)
		FROM query_log WHERE ts >= ? GROUP BY dev ORDER BY 3 DESC LIMIT 300`, since)
	if err != nil {
		return nil, err
	}
	out := []Client{}
	for rows.Next() {
		var c Client
		if err := rows.Scan(&c.Device, &c.Name, &c.Queries, &c.LastSeen); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	owner := map[string]int64{}
	drows, err := s.st.DB.QueryContext(ctx, `SELECT device, person_id FROM person_devices`)
	if err != nil {
		return nil, err
	}
	for drows.Next() {
		var d string
		var p int64
		if err := drows.Scan(&d, &p); err != nil {
			drows.Close()
			return nil, err
		}
		owner[d] = p
	}
	drows.Close()
	counts := map[string]int64{}
	erows, err := s.st.DB.QueryContext(ctx, `SELECT device, COUNT(*) FROM family_events WHERE first_ts >= ? AND category != ? GROUP BY device`, since, SafeSearch)
	if err != nil {
		return nil, err
	}
	defer erows.Close()
	for erows.Next() {
		var d string
		var n int64
		if err := erows.Scan(&d, &n); err != nil {
			return nil, err
		}
		counts[d] = n
	}
	for i := range out {
		out[i].PersonID = owner[out[i].Device]
		out[i].Events = counts[out[i].Device]
	}
	// Unassigned devices with family events first: they need a decision.
	sort.SliceStable(out, func(i, j int) bool {
		ui, uj := out[i].PersonID == 0 && out[i].Events > 0, out[j].PersonID == 0 && out[j].Events > 0
		return ui && !uj
	})
	return out, erows.Err()
}
