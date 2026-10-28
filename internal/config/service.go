package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hurseyc/guardtower/internal/agh"
	"github.com/hurseyc/guardtower/internal/events"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/store"
)

// Service manages desired-state layers and compares them with instances.
type Service struct {
	st    *store.Store
	fleet *fleet.Service
	bus   *events.Bus
	log   *slog.Logger
}

// NewService creates the config service.
func NewService(st *store.Store, fl *fleet.Service, bus *events.Bus, log *slog.Logger) *Service {
	return &Service{st: st, fleet: fl, bus: bus, log: log}
}

// ScopeRef identifies a layer owner.
type ScopeRef struct {
	Scope string `json:"scope"`
	ID    int64  `json:"id"`
}

func (r ScopeRef) validate() error {
	switch r.Scope {
	case ScopeBase:
		if r.ID != 0 {
			return errors.New("base scope has no id")
		}
	case ScopeGroup, ScopeInstance:
		if r.ID <= 0 {
			return errors.New("group and instance scopes need an id")
		}
	default:
		return errors.New("scope must be base, group or instance")
	}
	return nil
}

// LayerInfo is a stored layer with metadata.
type LayerInfo struct {
	ScopeRef
	Section   string `json:"section"`
	Doc       Doc    `json:"doc"`
	UpdatedAt int64  `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

// GetLayers returns every layer for a scope, keyed by section.
func (s *Service) GetLayers(ctx context.Context, ref ScopeRef) (map[string]LayerInfo, error) {
	if err := ref.validate(); err != nil {
		return nil, err
	}
	rows, err := s.st.DB.QueryContext(ctx, `SELECT section, doc, updated_at, updated_by FROM config_layers WHERE scope = ? AND scope_id = ?`, ref.Scope, ref.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]LayerInfo{}
	for rows.Next() {
		li := LayerInfo{ScopeRef: ref}
		var doc string
		if err := rows.Scan(&li.Section, &doc, &li.UpdatedAt, &li.UpdatedBy); err != nil {
			return nil, err
		}
		if li.Doc, err = ParseDoc([]byte(doc)); err != nil {
			return nil, fmt.Errorf("corrupt layer %s/%d/%s: %w", ref.Scope, ref.ID, li.Section, err)
		}
		out[li.Section] = li
	}
	return out, rows.Err()
}

// SetLayer validates and stores a layer. An empty document deletes it.
func (s *Service) SetLayer(ctx context.Context, ref ScopeRef, section string, d Doc, by string) (Doc, error) {
	if err := ref.validate(); err != nil {
		return nil, err
	}
	sec := SectionByKey(section)
	if sec == nil {
		return nil, fmt.Errorf("unknown section %q", section)
	}
	if len(d) == 0 {
		_, err := s.st.DB.ExecContext(ctx, `DELETE FROM config_layers WHERE scope = ? AND scope_id = ? AND section = ?`, ref.Scope, ref.ID, section)
		return Doc{}, err
	}
	clean, err := ValidateLayer(sec, d)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(clean)
	_, err = s.st.DB.ExecContext(ctx, `INSERT INTO config_layers (scope, scope_id, section, doc, updated_at, updated_by) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (scope, scope_id, section) DO UPDATE SET doc = excluded.doc, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		ref.Scope, ref.ID, section, string(b), time.Now().Unix(), by)
	return clean, err
}

// DeleteScope removes every layer of a scope (when a group or instance is deleted).
func (s *Service) DeleteScope(ctx context.Context, ref ScopeRef) error {
	_, err := s.st.DB.ExecContext(ctx, `DELETE FROM config_layers WHERE scope = ? AND scope_id = ?`, ref.Scope, ref.ID)
	return err
}

// layersFor returns the ordered layers (base, group, instance) that apply to an instance.
func (s *Service) layersFor(ctx context.Context, in *fleet.Instance) (map[string][]Layer, error) {
	refs := []struct {
		ref  ScopeRef
		name string
	}{{ScopeRef{ScopeBase, 0}, "Base"}}
	if in.GroupID > 0 {
		name := fmt.Sprintf("group %d", in.GroupID)
		if g, err := s.fleet.GetGroup(ctx, in.GroupID); err == nil {
			name = g.Name
		}
		refs = append(refs, struct {
			ref  ScopeRef
			name string
		}{ScopeRef{ScopeGroup, in.GroupID}, name})
	}
	refs = append(refs, struct {
		ref  ScopeRef
		name string
	}{ScopeRef{ScopeInstance, in.ID}, in.Name})
	out := map[string][]Layer{}
	for _, r := range refs {
		ls, err := s.GetLayers(ctx, r.ref)
		if err != nil {
			return nil, err
		}
		for sec, li := range ls {
			out[sec] = append(out[sec], Layer{Scope: r.ref.Scope, ID: r.ref.ID, Name: r.name, Doc: li.Doc})
		}
	}
	return out, nil
}

// SectionState is the effective and live state of one section on one instance.
type SectionState struct {
	Section   string   `json:"section"`
	Managed   bool     `json:"managed"`
	Desired   Doc      `json:"desired,omitempty"`
	Origins   Origins  `json:"origins,omitempty"`
	Live      Doc      `json:"live,omitempty"`
	Changes   []Change `json:"changes"`
	LiveError string   `json:"live_error,omitempty"`
	// Unsupported lists managed fields this instance's AdGuard Home version lacks.
	Unsupported []string `json:"unsupported,omitempty"`
}

// Effective computes desired state for an instance; with live=true it also
// reads the instance and diffs.
func (s *Service) Effective(ctx context.Context, instanceID int64, live bool) ([]SectionState, error) {
	in, err := s.fleet.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	layers, err := s.layersFor(ctx, in)
	if err != nil {
		return nil, err
	}
	var c *agh.Client
	if live {
		if c, err = s.fleet.Client(in); err != nil {
			return nil, err
		}
	}
	out := make([]SectionState, 0, len(Sections))
	for _, sec := range Sections {
		st := SectionState{Section: sec.Key, Changes: []Change{}}
		if ls := layers[sec.Key]; len(ls) > 0 {
			st.Managed = true
			st.Desired, st.Origins = Merge(sec, ls)
		}
		if c != nil {
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			d, err := sec.read(cctx, c)
			cancel()
			if err != nil {
				st.LiveError = err.Error()
			} else {
				st.Live = d
				if st.Managed {
					st.Changes = Diff(sec, st.Desired, d)
					st.Unsupported = Unsupported(sec, st.Desired, d)
				}
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// ImportBaseline reads an instance's live settings for the chosen sections
// and stores them as the layers of target (usually base). Existing layers
// for those sections are replaced.
func (s *Service) ImportBaseline(ctx context.Context, instanceID int64, target ScopeRef, sections []string, by string) error {
	in, err := s.fleet.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	c, err := s.fleet.Client(in)
	if err != nil {
		return err
	}
	if len(sections) == 0 {
		for _, sec := range Sections {
			sections = append(sections, sec.Key)
		}
	}
	for _, key := range sections {
		sec := SectionByKey(key)
		if sec == nil {
			return fmt.Errorf("unknown section %q", key)
		}
		live, err := sec.read(ctx, c)
		if err != nil {
			return fmt.Errorf("%s: read from %s: %w", key, in.Name, err)
		}
		if _, err := s.SetLayer(ctx, target, key, LayerFromLive(sec, live), by); err != nil {
			return err
		}
	}
	return nil
}

// SyncSummary is the stored comparison result for one instance.
type SyncSummary struct {
	InstanceID int64             `json:"instance_id"`
	Sections   map[string]int    `json:"sections"` // section → number of differences
	Errors     map[string]string `json:"errors,omitempty"`
	Total      int               `json:"total"`
	CheckedAt  int64             `json:"checked_at"`
}

// CheckInstance compares desired and live state for one instance and stores the result.
func (s *Service) CheckInstance(ctx context.Context, in *fleet.Instance) (*SyncSummary, error) {
	states, err := s.Effective(ctx, in.ID, true)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	sum := &SyncSummary{InstanceID: in.ID, Sections: map[string]int{}, Errors: map[string]string{}, CheckedAt: now}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sync_status WHERE instance_id = ?`, in.ID); err != nil {
		return nil, err
	}
	for _, st := range states {
		if !st.Managed {
			continue
		}
		b, _ := json.Marshal(st.Changes)
		if _, err := tx.ExecContext(ctx, `INSERT INTO sync_status (instance_id, section, changes, error, checked_at) VALUES (?, ?, ?, ?, ?)`,
			in.ID, st.Section, string(b), st.LiveError, now); err != nil {
			return nil, err
		}
		sum.Sections[st.Section] = len(st.Changes)
		sum.Total += len(st.Changes)
		if st.LiveError != "" {
			sum.Errors[st.Section] = st.LiveError
		}
	}
	return sum, tx.Commit()
}

// ListSync returns stored comparison results for every instance.
func (s *Service) ListSync(ctx context.Context) ([]SyncSummary, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT instance_id, section, changes, error, checked_at FROM sync_status ORDER BY instance_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[int64]*SyncSummary{}
	var order []int64
	for rows.Next() {
		var id, at int64
		var section, changes, errText string
		if err := rows.Scan(&id, &section, &changes, &errText, &at); err != nil {
			return nil, err
		}
		sum := byID[id]
		if sum == nil {
			sum = &SyncSummary{InstanceID: id, Sections: map[string]int{}, Errors: map[string]string{}}
			byID[id] = sum
			order = append(order, id)
		}
		var cs []Change
		_ = json.Unmarshal([]byte(changes), &cs)
		sum.Sections[section] = len(cs)
		sum.Total += len(cs)
		if errText != "" {
			sum.Errors[section] = errText
		}
		if at > sum.CheckedAt {
			sum.CheckedAt = at
		}
	}
	out := make([]SyncSummary, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, rows.Err()
}

// RunChecks compares every instance periodically and publishes
// "sync.changed" when an instance's number of differences changes.
func (s *Service) RunChecks(ctx context.Context, every time.Duration, kick <-chan struct{}) {
	t := time.NewTicker(every)
	defer t.Stop()
	last := map[int64]int{}
	var mu sync.Mutex
	check := func() {
		insts, err := s.fleet.ListInstances(ctx)
		if err != nil {
			return
		}
		var wg sync.WaitGroup
		for _, in := range insts {
			if !in.Enabled || in.Status == nil || !in.Status.Online {
				continue
			}
			wg.Add(1)
			go func(in *fleet.Instance) {
				defer wg.Done()
				sum, err := s.CheckInstance(ctx, in)
				if err != nil {
					if ctx.Err() == nil {
						s.log.Debug("config check failed", "instance", in.Name, "err", err)
					}
					return
				}
				mu.Lock()
				prev, seen := last[in.ID]
				last[in.ID] = sum.Total
				mu.Unlock()
				if !seen || prev != sum.Total {
					s.bus.Publish("sync.changed", map[string]any{"instance_id": in.ID, "name": in.Name, "total": sum.Total, "previous": prev, "first": !seen})
				}
			}(in)
		}
		wg.Wait()
	}
	check()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-kick:
		}
		check()
	}
}

// ApplyInstance pushes desired state for the given sections (all managed
// sections when empty) to one instance, without snapshots or health checks.
// Rollouts (package rollout) wrap this with safety.
func (s *Service) ApplyInstance(ctx context.Context, instanceID int64, sections []string) ([]Change, error) {
	states, err := s.Effective(ctx, instanceID, true)
	if err != nil {
		return nil, err
	}
	in, err := s.fleet.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	c, err := s.fleet.Client(in)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, k := range sections {
		want[k] = true
	}
	var applied []Change
	for _, st := range states {
		if !st.Managed || len(st.Changes) == 0 || (len(want) > 0 && !want[st.Section]) {
			continue
		}
		if st.LiveError != "" {
			return applied, fmt.Errorf("%s: cannot read current settings: %s", st.Section, st.LiveError)
		}
		if err := ApplySection(ctx, c, st.Section, st.Desired, st.Live); err != nil {
			return applied, err
		}
		applied = append(applied, st.Changes...)
	}
	return applied, nil
}
