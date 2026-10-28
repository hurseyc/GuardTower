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
	kick  chan struct{}
}

// NewService creates the config service.
func NewService(st *store.Store, fl *fleet.Service, bus *events.Bus, log *slog.Logger) *Service {
	return &Service{st: st, fleet: fl, bus: bus, log: log, kick: make(chan struct{}, 1)}
}

// Kick asks the background checker to compare every instance now.
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
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
					if cs := Diff(sec, st.Desired, d); cs != nil {
						st.Changes = cs
					}
					st.Unsupported = Unsupported(sec, st.Desired, d)
				}
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// ImportSummary says what an import stored, per section: the number of
// fields set at the target level (0 means the level inherits everything and
// its layer for that section was removed).
type ImportSummary struct {
	Sections map[string]int `json:"sections"`
}

// ImportBaseline reads an instance's live settings for the chosen sections
// and stores them as the layers of target, so that target's effective
// settings equal the instance's. Into a group or instance, only what differs
// from the levels below is stored. Existing layers for those sections are
// replaced.
func (s *Service) ImportBaseline(ctx context.Context, instanceID int64, target ScopeRef, sections []string, by string) error {
	_, err := s.ImportFromInstance(ctx, instanceID, target, sections, by)
	return err
}

// ImportFromInstance is ImportBaseline, reporting what was stored.
func (s *Service) ImportFromInstance(ctx context.Context, instanceID int64, target ScopeRef, sections []string, by string) (*ImportSummary, error) {
	if err := target.validate(); err != nil {
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
	if len(sections) == 0 {
		for _, sec := range Sections {
			sections = append(sections, sec.Key)
		}
	}
	lower, err := s.lowerLayers(ctx, target)
	if err != nil {
		return nil, err
	}
	sum := &ImportSummary{Sections: map[string]int{}}
	for _, key := range sections {
		sec := SectionByKey(key)
		if sec == nil {
			return nil, fmt.Errorf("unknown section %q", key)
		}
		live, err := sec.read(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("%s: read from %s: %w", key, in.Name, err)
		}
		inherited, _ := Merge(sec, lower[key])
		d := LayerToMatch(sec, inherited, live)
		if _, err := s.SetLayer(ctx, target, key, d, by); err != nil {
			return nil, err
		}
		sum.Sections[key] = len(d)
	}
	return sum, nil
}

// lowerLayers returns the layers below target, lowest first: nothing for
// Base, Base for a group, Base and the instance's group for an instance.
func (s *Service) lowerLayers(ctx context.Context, target ScopeRef) (map[string][]Layer, error) {
	refs := []ScopeRef{}
	names := []string{}
	switch target.Scope {
	case ScopeGroup:
		refs, names = append(refs, ScopeRef{Scope: ScopeBase}), append(names, "Base")
	case ScopeInstance:
		refs, names = append(refs, ScopeRef{Scope: ScopeBase}), append(names, "Base")
		in, err := s.fleet.GetInstance(ctx, target.ID)
		if err != nil {
			return nil, err
		}
		if in.GroupID > 0 {
			refs, names = append(refs, ScopeRef{Scope: ScopeGroup, ID: in.GroupID}), append(names, "group")
		}
	}
	out := map[string][]Layer{}
	for i, r := range refs {
		ls, err := s.GetLayers(ctx, r)
		if err != nil {
			return nil, err
		}
		for sec, li := range ls {
			out[sec] = append(out[sec], Layer{Scope: r.Scope, ID: r.ID, Name: names[i], Doc: li.Doc})
		}
	}
	return out, nil
}

// SyncSummary is the stored comparison result for one instance.
type SyncSummary struct {
	InstanceID int64 `json:"instance_id"`
	// Drifted lists sections changed outside GuardTower since it last applied them.
	Drifted   []string          `json:"drifted"`
	Sections  map[string]int    `json:"sections"` // section → number of differences
	Errors    map[string]string `json:"errors,omitempty"`
	Total     int               `json:"total"`
	CheckedAt int64             `json:"checked_at"`
}

// CheckInstance compares desired and live state for one instance and stores the result.
func (s *Service) CheckInstance(ctx context.Context, in *fleet.Instance) (*SyncSummary, error) {
	states, err := s.Effective(ctx, in.ID, true)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	sum := &SyncSummary{InstanceID: in.ID, Sections: map[string]int{}, Errors: map[string]string{}, CheckedAt: now, Drifted: []string{}}
	lastApplied := s.lastApplied(ctx, in.ID)
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
		drifted := false
		if applied, ok := lastApplied[st.Section]; ok && st.Live != nil && len(st.Changes) > 0 {
			// Compare live against what GuardTower last applied, on managed fields only.
			ref := Doc{}
			for k := range st.Desired {
				if v, ok := applied[k]; ok {
					ref[k] = v
				}
			}
			drifted = len(Diff(SectionByKey(st.Section), ref, st.Live)) > 0
		}
		if drifted {
			sum.Drifted = append(sum.Drifted, st.Section)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sync_status (instance_id, section, changes, error, checked_at, drifted) VALUES (?, ?, ?, ?, ?, ?)`,
			in.ID, st.Section, string(b), st.LiveError, now, drifted); err != nil {
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

// lastApplied returns the section docs from the instance's most recent
// post-apply snapshot (empty if GuardTower never applied anything).
func (s *Service) lastApplied(ctx context.Context, instanceID int64) map[string]Doc {
	var raw string
	err := s.st.DB.QueryRowContext(ctx, `SELECT doc FROM snapshots WHERE instance_id = ? AND kind = 'post-apply' ORDER BY id DESC LIMIT 1`, instanceID).Scan(&raw)
	out := map[string]Doc{}
	if err != nil {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// ListSync returns stored comparison results for every instance.
func (s *Service) ListSync(ctx context.Context) ([]SyncSummary, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT instance_id, section, changes, error, checked_at, drifted FROM sync_status ORDER BY instance_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[int64]*SyncSummary{}
	var order []int64
	for rows.Next() {
		var id, at int64
		var section, changes, errText string
		var drifted bool
		if err := rows.Scan(&id, &section, &changes, &errText, &at, &drifted); err != nil {
			return nil, err
		}
		sum := byID[id]
		if sum == nil {
			sum = &SyncSummary{InstanceID: id, Sections: map[string]int{}, Errors: map[string]string{}, Drifted: []string{}}
			byID[id] = sum
			order = append(order, id)
		}
		var cs []Change
		_ = json.Unmarshal([]byte(changes), &cs)
		sum.Sections[section] = len(cs)
		if drifted {
			sum.Drifted = append(sum.Drifted, section)
		}
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
func (s *Service) RunChecks(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	last := map[int64]int{}
	lastDrift := map[int64]int{}
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
				prevDrift := lastDrift[in.ID]
				lastDrift[in.ID] = len(sum.Drifted)
				mu.Unlock()
				if !seen || prev != sum.Total {
					s.bus.Publish("sync.changed", map[string]any{"instance_id": in.ID, "name": in.Name, "total": sum.Total, "previous": prev, "first": !seen})
				}
				if len(sum.Drifted) > prevDrift {
					s.bus.Publish("drift.detected", map[string]any{"instance_id": in.ID, "name": in.Name, "sections": sum.Drifted})
					_ = s.st.Audit(ctx, store.AuditEntry{Action: "drift.detected", Target: in.Name, Detail: mustJSON(map[string]any{"sections": sum.Drifted})})
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
		case <-s.kick:
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

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
