// Package notify sends short messages to ntfy, Discord or a generic webhook.
package notify

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/store"
)

// Target kinds.
const (
	KindNtfy    = "ntfy"
	KindDiscord = "discord"
	KindWebhook = "webhook"
)

// Target is a destination for notifications.
type Target struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	HasToken  bool   `json:"has_token"`
	Enabled   bool   `json:"enabled"`
	Family    bool   `json:"family"`
	CreatedAt int64  `json:"created_at"`
}

// TargetInput is the create/update payload. Token nil keeps the stored one.
type TargetInput struct {
	Name    string  `json:"name"`
	Kind    string  `json:"kind"`
	URL     string  `json:"url"`
	Token   *string `json:"token"`
	Enabled bool    `json:"enabled"`
	Family  bool    `json:"family"`
}

// Message is one notification.
type Message struct {
	Title    string         `json:"title"`
	Body     string         `json:"body"`
	Priority string         `json:"priority,omitempty"` // low | default | high
	Tags     []string       `json:"tags,omitempty"`
	Event    string         `json:"event"`
	Link     string         `json:"link,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

// Service stores targets and delivers messages.
type Service struct {
	st   *store.Store
	box  *crypto.Box
	HTTP *http.Client
}

// New creates the service.
func New(st *store.Store, box *crypto.Box) *Service {
	return &Service{st: st, box: box, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// BadInput is a validation error.
type BadInput struct{ Msg string }

func (e *BadInput) Error() string { return e.Msg }

// ErrNotFound is returned for unknown targets.
var ErrNotFound = errors.New("notification target not found")

type row struct {
	Target
	token []byte
}

func (s *Service) list(ctx context.Context, where string, args ...any) ([]row, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT id, name, kind, url, token_enc, enabled, family, created_at FROM notify_targets `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind, &r.URL, &r.token, &r.Enabled, &r.Family, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.HasToken = len(r.token) > 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// List returns all targets (never tokens).
func (s *Service) List(ctx context.Context) ([]Target, error) {
	rs, err := s.list(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]Target, len(rs))
	for i, r := range rs {
		out[i] = r.Target
	}
	return out, nil
}

func validate(in *TargetInput) error {
	in.Name, in.URL = strings.TrimSpace(in.Name), strings.TrimSpace(in.URL)
	if in.Name == "" {
		return &BadInput{"name is required"}
	}
	switch in.Kind {
	case KindNtfy, KindDiscord, KindWebhook:
	default:
		return &BadInput{"kind must be ntfy, discord or webhook"}
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return &BadInput{"URL must be an http(s) URL, e.g. https://ntfy.sh/my-family-topic"}
	}
	return nil
}

// Save creates (id 0) or updates a target.
func (s *Service) Save(ctx context.Context, id int64, in TargetInput) (*Target, error) {
	if err := validate(&in); err != nil {
		return nil, err
	}
	var tok []byte
	keep := in.Token == nil
	if in.Token != nil && strings.TrimSpace(*in.Token) != "" {
		var err error
		if tok, err = s.box.Seal(strings.TrimSpace(*in.Token)); err != nil {
			return nil, err
		}
	}
	if id == 0 {
		res, err := s.st.DB.ExecContext(ctx, `INSERT INTO notify_targets (name, kind, url, token_enc, enabled, family, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			in.Name, in.Kind, in.URL, tok, in.Enabled, in.Family, time.Now().UnixMilli())
		if err != nil {
			return nil, err
		}
		id, _ = res.LastInsertId()
	} else {
		q := `UPDATE notify_targets SET name = ?, kind = ?, url = ?, enabled = ?, family = ?, token_enc = ? WHERE id = ?`
		args := []any{in.Name, in.Kind, in.URL, in.Enabled, in.Family, tok, id}
		if keep {
			q = `UPDATE notify_targets SET name = ?, kind = ?, url = ?, enabled = ?, family = ? WHERE id = ?`
			args = []any{in.Name, in.Kind, in.URL, in.Enabled, in.Family, id}
		}
		res, err := s.st.DB.ExecContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
	}
	rs, err := s.list(ctx, "WHERE id = ?", id)
	if err != nil || len(rs) == 0 {
		return nil, err
	}
	return &rs[0].Target, nil
}

// Delete removes a target.
func (s *Service) Delete(ctx context.Context, id int64) error {
	_, err := s.st.DB.ExecContext(ctx, `DELETE FROM notify_targets WHERE id = ?`, id)
	return err
}

// Test sends a test message to one target.
func (s *Service) Test(ctx context.Context, id int64) error {
	rs, err := s.list(ctx, "WHERE id = ?", id)
	if err != nil {
		return err
	}
	if len(rs) == 0 {
		return ErrNotFound
	}
	return s.deliver(ctx, rs[0], Message{Title: "GuardTower test", Body: "Notifications from GuardTower reach this channel.", Event: "test", Tags: []string{"white_check_mark"}})
}

// SendFamily delivers to every enabled target that receives family alerts.
// Delivery errors are joined; one failing target doesn't stop the others.
func (s *Service) SendFamily(ctx context.Context, m Message) error {
	rs, err := s.list(ctx, "WHERE enabled = 1 AND family = 1")
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range rs {
		if err := s.deliver(ctx, r, m); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) deliver(ctx context.Context, r row, m Message) error {
	token := ""
	if len(r.token) > 0 {
		var err error
		if token, err = s.box.Open(r.token); err != nil {
			return err
		}
	}
	var body []byte
	var ctype string
	headers := map[string]string{}
	switch r.Kind {
	case KindNtfy:
		body, ctype = []byte(m.Body), "text/plain; charset=utf-8"
		headers["Title"] = m.Title
		if m.Priority != "" {
			headers["Priority"] = m.Priority
		}
		if len(m.Tags) > 0 {
			headers["Tags"] = strings.Join(m.Tags, ",")
		}
		if m.Link != "" {
			headers["Click"] = m.Link
		}
		if token != "" {
			headers["Authorization"] = "Bearer " + token
		}
	case KindDiscord:
		content := "**" + m.Title + "**\n" + m.Body
		if m.Link != "" {
			content += "\n" + m.Link
		}
		if len(content) > 1900 {
			content = content[:1900] + "…"
		}
		body, _ = json.Marshal(map[string]any{"content": content, "allowed_mentions": map[string]any{"parse": []string{}}})
		ctype = "application/json"
	default:
		body, _ = json.Marshal(m)
		ctype = "application/json"
		if token != "" {
			headers["Authorization"] = "Bearer " + token
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("User-Agent", "GuardTower")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// IsNotFound reports whether err is ErrNotFound or sql.ErrNoRows.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) }
