// Package rollout applies desired settings safely: one instance at a time,
// canaries first, with a snapshot before every change, a health check after
// it, and automatic restore of the snapshot when anything fails.
package rollout

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hurseyc/guardtower/internal/agh"
	"github.com/hurseyc/guardtower/internal/config"
	"github.com/hurseyc/guardtower/internal/events"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/health"
	"github.com/hurseyc/guardtower/internal/store"
)

// SettingObserve is the observe-mode switch: while "true", nothing is applied.
const SettingObserve = "mode.observe"

// ErrObserveMode is returned when applying while observe mode is on.
var ErrObserveMode = errors.New("observe mode is on: GuardTower is not allowed to change instances. An admin can turn it off under Admin → Settings")

// ErrBusy is returned when another rollout is running.
var ErrBusy = errors.New("another rollout is in progress; wait for it to finish")

// Target selects instances.
type Target struct {
	Kind string `json:"kind"` // fleet | group | tag | instance
	ID   int64  `json:"id,omitempty"`
	Tag  string `json:"tag,omitempty"`
}

// PlanItem is what would change on one instance.
type PlanItem struct {
	InstanceID  int64           `json:"instance_id"`
	Name        string          `json:"name"`
	Canary      bool            `json:"canary"`
	Online      bool            `json:"online"`
	Changes     []config.Change `json:"changes"`
	Unsupported []string        `json:"unsupported,omitempty"`
	Error       string          `json:"error,omitempty"`
}

// Plan is a dry run: the changes an apply would make, in rollout order.
type Plan struct {
	Target   string     `json:"target"`
	Sections []string   `json:"sections,omitempty"`
	Items    []PlanItem `json:"items"`
	Total    int        `json:"total"`
	Observe  bool       `json:"observe"`
	Warnings []string   `json:"warnings"`
}

// Service runs rollouts.
type Service struct {
	st     *store.Store
	fleet  *fleet.Service
	cfg    *config.Service
	bus    *events.Bus
	log    *slog.Logger
	root   context.Context
	mu     sync.Mutex
	active int64
	// SettleDelay is how long to wait after applying before health checks.
	SettleDelay time.Duration
}

// New creates the rollout service.
func New(st *store.Store, fl *fleet.Service, cfg *config.Service, bus *events.Bus, log *slog.Logger) *Service {
	return &Service{root: context.Background(), st: st, fleet: fl, cfg: cfg, bus: bus, log: log, SettleDelay: 2 * time.Second}
}

// SetRoot sets the context that bounds background rollouts (the app's lifetime).
func (s *Service) SetRoot(ctx context.Context) { s.root = ctx }

// Observing reports whether observe mode is on (the default).
func (s *Service) Observing(ctx context.Context) bool {
	v, err := s.st.GetSetting(ctx, SettingObserve, "true")
	return err != nil || v != "false"
}

func (s *Service) resolve(ctx context.Context, t Target) ([]*fleet.Instance, string, error) {
	all, err := s.fleet.ListInstances(ctx)
	if err != nil {
		return nil, "", err
	}
	var out []*fleet.Instance
	desc := ""
	switch t.Kind {
	case "fleet", "":
		desc = "Whole fleet"
		out = all
	case "group":
		g, err := s.fleet.GetGroup(ctx, t.ID)
		if err != nil {
			return nil, "", err
		}
		desc = "Group " + g.Name
		for _, in := range all {
			if in.GroupID == t.ID {
				out = append(out, in)
			}
		}
	case "tag":
		tag := strings.ToLower(strings.TrimSpace(t.Tag))
		desc = "Tag #" + tag
		for _, in := range all {
			for _, x := range in.Tags {
				if x == tag {
					out = append(out, in)
				}
			}
		}
	case "instance":
		in, err := s.fleet.GetInstance(ctx, t.ID)
		if err != nil {
			return nil, "", err
		}
		desc = "Instance " + in.Name
		out = []*fleet.Instance{in}
	default:
		return nil, "", fmt.Errorf("unknown target kind %q", t.Kind)
	}
	var enabled []*fleet.Instance
	for _, in := range out {
		if in.Enabled {
			enabled = append(enabled, in)
		}
	}
	// Canaries first, then by name, so order is predictable.
	sort.SliceStable(enabled, func(i, j int) bool {
		if enabled[i].IsCanary != enabled[j].IsCanary {
			return enabled[i].IsCanary
		}
		return enabled[i].Name < enabled[j].Name
	})
	return enabled, desc, nil
}

// MakePlan computes what applying would change, without changing anything.
func (s *Service) MakePlan(ctx context.Context, t Target, sections []string) (*Plan, error) {
	insts, desc, err := s.resolve(ctx, t)
	if err != nil {
		return nil, err
	}
	p := &Plan{Target: desc, Sections: sections, Items: []PlanItem{}, Observe: s.Observing(ctx), Warnings: []string{}}
	want := map[string]bool{}
	for _, k := range sections {
		want[k] = true
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	items := make([]PlanItem, len(insts))
	for i, in := range insts {
		wg.Add(1)
		go func(i int, in *fleet.Instance) {
			defer wg.Done()
			it := PlanItem{InstanceID: in.ID, Name: in.Name, Canary: in.IsCanary, Online: in.Status != nil && in.Status.Online, Changes: []config.Change{}}
			states, err := s.cfg.Effective(ctx, in.ID, true)
			if err != nil {
				it.Error = err.Error()
			} else {
				for _, st := range states {
					if len(want) > 0 && !want[st.Section] {
						continue
					}
					if st.LiveError != "" && st.Managed {
						it.Error = fmt.Sprintf("%s: %s", st.Section, st.LiveError)
					}
					it.Changes = append(it.Changes, st.Changes...)
					for _, u := range st.Unsupported {
						it.Unsupported = append(it.Unsupported, st.Section+"."+u)
					}
				}
			}
			mu.Lock()
			items[i] = it
			mu.Unlock()
		}(i, in)
	}
	wg.Wait()
	canaryWithChanges := false
	for _, it := range items {
		p.Items = append(p.Items, it)
		p.Total += len(it.Changes)
		if it.Canary && len(it.Changes) > 0 {
			canaryWithChanges = true
		}
		if !it.Online && len(it.Changes) > 0 {
			p.Warnings = append(p.Warnings, it.Name+" is offline and will be skipped.")
		}
	}
	if p.Total > 0 && !canaryWithChanges && countWithChanges(items) > 1 {
		p.Warnings = append(p.Warnings, "No canary instance is affected: the first instance in the list acts as the canary. Mark your least critical instance as canary to choose it.")
	}
	return p, nil
}

func countWithChanges(items []PlanItem) int {
	n := 0
	for _, it := range items {
		if len(it.Changes) > 0 {
			n++
		}
	}
	return n
}

// Start creates a rollout from a fresh plan and runs it in the background.
func (s *Service) Start(ctx context.Context, t Target, sections []string, by string) (int64, error) {
	if s.Observing(ctx) {
		return 0, ErrObserveMode
	}
	if !s.claim() {
		return 0, ErrBusy
	}
	plan, err := s.MakePlan(ctx, t, sections)
	if err != nil {
		s.release()
		return 0, err
	}
	if plan.Total == 0 {
		s.release()
		return 0, errors.New("nothing to apply: every selected instance already matches its desired settings")
	}
	id, err := s.create(ctx, "apply", plan.Target, sections, by, plan.Items)
	if err != nil {
		s.release()
		return 0, err
	}
	go s.run(id, sections) //nolint:gosec // outlives the request on purpose; run uses the service root context
	return id, nil
}

func (s *Service) claim() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != 0 {
		return false
	}
	s.active = -1
	return true
}

func (s *Service) release() {
	s.mu.Lock()
	s.active = 0
	s.mu.Unlock()
}

func (s *Service) create(ctx context.Context, kind, target string, sections []string, by string, items []PlanItem) (int64, error) {
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	secs, _ := json.Marshal(nonNil(sections))
	res, err := tx.ExecContext(ctx, `INSERT INTO rollouts (created_at, created_by, kind, target, sections, status) VALUES (?, ?, ?, ?, ?, 'running')`,
		time.Now().UnixMilli(), by, kind, target, string(secs))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	ord := 0
	for _, it := range items {
		if len(it.Changes) == 0 && kind == "apply" {
			continue
		}
		status, detail := "pending", ""
		if !it.Online {
			status, detail = "skipped", "instance offline"
		}
		ch, _ := json.Marshal(it.Changes)
		if _, err := tx.ExecContext(ctx, `INSERT INTO rollout_steps (rollout_id, instance_id, ordinal, is_canary, status, changes, detail) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, it.InstanceID, ord, it.Canary, status, string(ch), detail); err != nil {
			return 0, err
		}
		ord++
	}
	s.mu.Lock()
	s.active = id
	s.mu.Unlock()
	return id, tx.Commit()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *Service) run(id int64, sections []string) {
	defer s.release()
	ctx := s.root
	steps, err := s.steps(ctx, id)
	if err != nil {
		s.finish(ctx, id, "failed", err.Error())
		return
	}
	for i, st := range steps {
		if st.Status != "pending" {
			continue
		}
		if ctx.Err() != nil {
			s.finish(context.Background(), id, "cancelled", "GuardTower is shutting down")
			return
		}
		if err := s.runStep(ctx, id, st, sections); err != nil {
			for _, rest := range steps[i+1:] {
				if rest.Status == "pending" {
					s.setStep(ctx, rest.ID, "skipped", "rollout stopped after "+st.InstanceName+" failed")
				}
			}
			status := "rolled_back"
			if errors.Is(err, errRestoreFailed) {
				status = "failed"
			}
			s.finish(ctx, id, status, fmt.Sprintf("%s: %v", st.InstanceName, err))
			return
		}
	}
	s.finish(ctx, id, "succeeded", "")
}

var errRestoreFailed = errors.New("automatic restore failed")

// runStep snapshots, applies, verifies and, on failure, restores one instance.
func (s *Service) runStep(ctx context.Context, rolloutID int64, st Step, sections []string) error {
	in, err := s.fleet.GetInstance(ctx, st.InstanceID)
	if err != nil {
		s.setStep(ctx, st.ID, "failed", err.Error())
		return err
	}
	c, err := s.fleet.Client(in)
	if err != nil {
		s.setStep(ctx, st.ID, "failed", err.Error())
		return err
	}
	s.setStep(ctx, st.ID, "applying", "taking snapshot")
	snap, snapDoc, err := s.snapshot(ctx, in, c, "pre-apply", rolloutID)
	if err != nil {
		s.setStep(ctx, st.ID, "failed", "could not take a snapshot, nothing was changed: "+err.Error())
		return err
	}
	_, _ = s.st.DB.ExecContext(ctx, `UPDATE rollout_steps SET snapshot_id = ? WHERE id = ?`, snap, st.ID)
	s.setStep(ctx, st.ID, "applying", "applying changes")
	_, applyErr := s.cfg.ApplyInstance(ctx, in.ID, sections)
	var verifyErr error
	if applyErr == nil {
		s.setStep(ctx, st.ID, "verifying", "checking DNS and settings")
		verifyErr = s.verify(ctx, in, c, sections)
	}
	if applyErr == nil && verifyErr == nil {
		_, _, _ = s.snapshot(ctx, in, c, "post-apply", rolloutID)
		s.setStep(ctx, st.ID, "succeeded", "applied and verified")
		return nil
	}
	cause := applyErr
	if cause == nil {
		cause = verifyErr
	}
	s.setStep(ctx, st.ID, "failed", "restoring snapshot after: "+cause.Error())
	if rerr := s.restoreDoc(ctx, c, snapDoc); rerr != nil {
		s.setStep(ctx, st.ID, "failed", fmt.Sprintf("%v; then the automatic restore failed too: %v. Restore snapshot #%d manually.", cause, rerr, snap))
		return fmt.Errorf("%w: %w", errRestoreFailed, cause)
	}
	if h := health.Probe(ctx, in, c); !h.DNSOK {
		s.setStep(ctx, st.ID, "failed", fmt.Sprintf("%v; settings were restored but the instance is still unhealthy: %s", cause, h.Error))
		return fmt.Errorf("%w: %w", errRestoreFailed, cause)
	}
	s.setStep(ctx, st.ID, "rolled_back", "rolled back after: "+cause.Error())
	return cause
}

// verify waits for the instance to settle, then requires it to answer DNS
// and to report the applied settings.
func (s *Service) verify(ctx context.Context, in *fleet.Instance, c *agh.Client, sections []string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.SettleDelay):
	}
	if h := health.Probe(ctx, in, c); !h.DNSOK {
		return fmt.Errorf("health check failed: %s", h.Error)
	}
	states, err := s.cfg.Effective(ctx, in.ID, true)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, k := range sections {
		want[k] = true
	}
	for _, st := range states {
		if st.Managed && (len(want) == 0 || want[st.Section]) && len(st.Changes) > 0 {
			return fmt.Errorf("%s: %d setting(s) did not take effect (e.g. %s)", st.Section, len(st.Changes), st.Changes[0].Label)
		}
	}
	return nil
}

func (s *Service) snapshot(ctx context.Context, in *fleet.Instance, c *agh.Client, kind string, rolloutID int64) (int64, map[string]config.Doc, error) {
	doc, err := config.Read(ctx, c)
	if err != nil {
		return 0, nil, err
	}
	b, _ := json.Marshal(doc)
	res, err := s.st.DB.ExecContext(ctx, `INSERT INTO snapshots (instance_id, created_at, kind, rollout_id, doc) VALUES (?, ?, ?, ?, ?)`,
		in.ID, time.Now().UnixMilli(), kind, store.NullInt(rolloutID), string(b))
	if err != nil {
		return 0, nil, err
	}
	id, _ := res.LastInsertId()
	return id, doc, nil
}

// restoreDoc makes the instance match a snapshot for every section that
// differs from it.
func (s *Service) restoreDoc(ctx context.Context, c *agh.Client, doc map[string]config.Doc) error {
	var errs []string
	for _, sec := range config.Sections {
		want, ok := doc[sec.Key]
		if !ok {
			continue
		}
		live, err := config.ReadSection(ctx, c, sec.Key)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if len(config.Diff(sec, want, live)) == 0 {
			continue
		}
		if err := config.ApplySection(ctx, c, sec.Key, want, live); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Restore applies a stored snapshot to its instance as a one-step rollout,
// with its own pre-restore snapshot and automatic revert on failure.
func (s *Service) Restore(ctx context.Context, snapshotID int64, by string) (int64, error) {
	if s.Observing(ctx) {
		return 0, ErrObserveMode
	}
	snap, err := s.GetSnapshot(ctx, snapshotID)
	if err != nil {
		return 0, err
	}
	in, err := s.fleet.GetInstance(ctx, snap.InstanceID)
	if err != nil {
		return 0, err
	}
	if !s.claim() {
		return 0, ErrBusy
	}
	id, err := s.create(ctx, "restore", fmt.Sprintf("Restore %s to snapshot #%d", in.Name, snapshotID), nil, by,
		[]PlanItem{{InstanceID: in.ID, Name: in.Name, Online: in.Status != nil && in.Status.Online}})
	if err != nil {
		s.release()
		return 0, err
	}
	go func() {
		defer s.release()
		ctx := s.root
		steps, err := s.steps(ctx, id)
		if err != nil || len(steps) == 0 {
			s.finish(ctx, id, "failed", "no steps")
			return
		}
		st := steps[0]
		if st.Status == "skipped" {
			s.finish(ctx, id, "failed", in.Name+" is offline")
			return
		}
		c, err := s.fleet.Client(in)
		if err != nil {
			s.setStep(ctx, st.ID, "failed", err.Error())
			s.finish(ctx, id, "failed", err.Error())
			return
		}
		s.setStep(ctx, st.ID, "applying", "taking snapshot")
		_, pre, err := s.snapshot(ctx, in, c, "pre-restore", id)
		if err != nil {
			s.setStep(ctx, st.ID, "failed", "could not take a snapshot, nothing was changed: "+err.Error())
			s.finish(ctx, id, "failed", err.Error())
			return
		}
		s.setStep(ctx, st.ID, "applying", "restoring settings")
		rerr := s.restoreDoc(ctx, c, snap.Doc)
		if rerr == nil {
			s.setStep(ctx, st.ID, "verifying", "checking DNS")
			time.Sleep(s.SettleDelay)
			if h := health.Probe(ctx, in, c); !h.DNSOK {
				rerr = errors.New("health check failed: " + h.Error)
			}
		}
		if rerr == nil {
			s.setStep(ctx, st.ID, "succeeded", "restored and verified")
			s.finish(ctx, id, "succeeded", "")
			return
		}
		if err := s.restoreDoc(ctx, c, pre); err != nil {
			s.setStep(ctx, st.ID, "failed", fmt.Sprintf("%v; reverting also failed: %v", rerr, err))
			s.finish(ctx, id, "failed", rerr.Error())
			return
		}
		s.setStep(ctx, st.ID, "rolled_back", "reverted after: "+rerr.Error())
		s.finish(ctx, id, "rolled_back", rerr.Error())
	}()
	return id, nil
}

// ---- persistence & queries ----

// Rollout is a stored rollout with its steps.
type Rollout struct {
	ID         int64    `json:"id"`
	CreatedAt  int64    `json:"created_at"`
	CreatedBy  string   `json:"created_by"`
	Kind       string   `json:"kind"`
	Target     string   `json:"target"`
	Sections   []string `json:"sections"`
	Status     string   `json:"status"`
	Error      string   `json:"error"`
	FinishedAt int64    `json:"finished_at,omitempty"`
	Steps      []Step   `json:"steps,omitempty"`
}

// Step is one instance in a rollout.
type Step struct {
	ID           int64           `json:"id"`
	InstanceID   int64           `json:"instance_id"`
	InstanceName string          `json:"instance_name"`
	Ordinal      int             `json:"ordinal"`
	Canary       bool            `json:"canary"`
	Status       string          `json:"status"`
	Changes      []config.Change `json:"changes"`
	SnapshotID   int64           `json:"snapshot_id,omitempty"`
	Detail       string          `json:"detail"`
	StartedAt    int64           `json:"started_at,omitempty"`
	FinishedAt   int64           `json:"finished_at,omitempty"`
}

func (s *Service) steps(ctx context.Context, rolloutID int64) ([]Step, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT st.id, st.instance_id, COALESCE(i.name, ''), st.ordinal, st.is_canary, st.status, st.changes,
		COALESCE(st.snapshot_id, 0), st.detail, COALESCE(st.started_at, 0), COALESCE(st.finished_at, 0)
		FROM rollout_steps st LEFT JOIN instances i ON i.id = st.instance_id WHERE st.rollout_id = ? ORDER BY st.ordinal`, rolloutID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Step{}
	for rows.Next() {
		var st Step
		var changes string
		if err := rows.Scan(&st.ID, &st.InstanceID, &st.InstanceName, &st.Ordinal, &st.Canary, &st.Status, &changes, &st.SnapshotID, &st.Detail, &st.StartedAt, &st.FinishedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(changes), &st.Changes)
		if st.Changes == nil {
			st.Changes = []config.Change{}
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Service) setStep(ctx context.Context, stepID int64, status, detail string) {
	now := time.Now().UnixMilli()
	var finished any // stays NULL until the step reaches a final state
	switch status {
	case "succeeded", "failed", "rolled_back", "skipped":
		finished = now
	}
	if _, err := s.st.DB.ExecContext(ctx, `UPDATE rollout_steps SET status = ?, detail = ?, started_at = COALESCE(started_at, ?),
		finished_at = COALESCE(?, finished_at) WHERE id = ?`, status, detail, now, finished, stepID); err != nil {
		s.log.Error("rollout: update step", "err", err)
	}
	var rid int64
	_ = s.st.DB.QueryRowContext(ctx, `SELECT rollout_id FROM rollout_steps WHERE id = ?`, stepID).Scan(&rid)
	s.bus.Publish("rollout.update", map[string]any{"rollout_id": rid, "step_id": stepID, "status": status, "detail": detail})
}

func (s *Service) finish(ctx context.Context, id int64, status, msg string) {
	if _, err := s.st.DB.ExecContext(ctx, `UPDATE rollouts SET status = ?, error = ?, finished_at = ? WHERE id = ?`, status, msg, time.Now().UnixMilli(), id); err != nil {
		s.log.Error("rollout: finish", "err", err)
	}
	_ = s.st.Audit(ctx, store.AuditEntry{Action: "rollout." + status, Target: fmt.Sprintf("rollout #%d", id), Detail: jsonString(map[string]string{"error": msg})})
	s.bus.Publish("rollout.update", map[string]any{"rollout_id": id, "status": status, "error": msg, "finished": true})
	s.cfg.Kick()
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Get returns a rollout with steps.
func (s *Service) Get(ctx context.Context, id int64) (*Rollout, error) {
	r := &Rollout{}
	var secs string
	var fin sql.NullInt64
	err := s.st.DB.QueryRowContext(ctx, `SELECT id, created_at, created_by, kind, target, sections, status, error, finished_at FROM rollouts WHERE id = ?`, id).
		Scan(&r.ID, &r.CreatedAt, &r.CreatedBy, &r.Kind, &r.Target, &secs, &r.Status, &r.Error, &fin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fleet.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.FinishedAt = fin.Int64
	_ = json.Unmarshal([]byte(secs), &r.Sections)
	r.Steps, err = s.steps(ctx, id)
	return r, err
}

// List returns recent rollouts (without steps), newest first.
func (s *Service) List(ctx context.Context, limit int) ([]Rollout, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.st.DB.QueryContext(ctx, `SELECT r.id, r.created_at, r.created_by, r.kind, r.target, r.sections, r.status, r.error, COALESCE(r.finished_at, 0)
		FROM rollouts r ORDER BY r.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rollout{}
	for rows.Next() {
		var r Rollout
		var secs string
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.CreatedBy, &r.Kind, &r.Target, &secs, &r.Status, &r.Error, &r.FinishedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(secs), &r.Sections)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Snapshot is a stored copy of an instance's settings.
type Snapshot struct {
	ID         int64                 `json:"id"`
	InstanceID int64                 `json:"instance_id"`
	CreatedAt  int64                 `json:"created_at"`
	Kind       string                `json:"kind"`
	RolloutID  int64                 `json:"rollout_id,omitempty"`
	Note       string                `json:"note,omitempty"`
	Doc        map[string]config.Doc `json:"doc,omitempty"`
}

// ListSnapshots returns an instance's snapshots, newest first (without docs).
func (s *Service) ListSnapshots(ctx context.Context, instanceID int64, limit int) ([]Snapshot, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.st.DB.QueryContext(ctx, `SELECT id, instance_id, created_at, kind, COALESCE(rollout_id, 0), note FROM snapshots
		WHERE instance_id = ? ORDER BY id DESC LIMIT ?`, instanceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Snapshot{}
	for rows.Next() {
		var sn Snapshot
		if err := rows.Scan(&sn.ID, &sn.InstanceID, &sn.CreatedAt, &sn.Kind, &sn.RolloutID, &sn.Note); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

// GetSnapshot returns one snapshot including its document.
func (s *Service) GetSnapshot(ctx context.Context, id int64) (*Snapshot, error) {
	sn := &Snapshot{}
	var doc string
	err := s.st.DB.QueryRowContext(ctx, `SELECT id, instance_id, created_at, kind, COALESCE(rollout_id, 0), note, doc FROM snapshots WHERE id = ?`, id).
		Scan(&sn.ID, &sn.InstanceID, &sn.CreatedAt, &sn.Kind, &sn.RolloutID, &sn.Note, &doc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fleet.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return sn, json.Unmarshal([]byte(doc), &sn.Doc)
}

// TakeSnapshot stores a manual snapshot of an instance's current settings.
func (s *Service) TakeSnapshot(ctx context.Context, instanceID int64, note string) (int64, error) {
	in, err := s.fleet.GetInstance(ctx, instanceID)
	if err != nil {
		return 0, err
	}
	c, err := s.fleet.Client(in)
	if err != nil {
		return 0, err
	}
	id, _, err := s.snapshot(ctx, in, c, "manual", 0)
	if err == nil && note != "" {
		_, _ = s.st.DB.ExecContext(ctx, `UPDATE snapshots SET note = ? WHERE id = ?`, note, id)
	}
	return id, err
}

// ActiveID returns the running rollout id, or 0.
func (s *Service) ActiveID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active < 0 {
		return 0
	}
	return s.active
}
