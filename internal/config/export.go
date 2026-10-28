package config

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// File is the YAML form of every settings layer, keyed by name so it can be
// kept in git and moved between GuardTower installs.
type File struct {
	Version   int                       `yaml:"version"`
	Base      map[string]Doc            `yaml:"base,omitempty"`
	Groups    map[string]map[string]Doc `yaml:"groups,omitempty"`
	Instances map[string]map[string]Doc `yaml:"instances,omitempty"`
}

const fileHeader = `# GuardTower desired settings (layers): Base, then each group, then
# per-instance overrides. Lists use {add, remove} and keyed lists
# {upsert, remove}; see "Configuration & settings layers" in the docs.
# Import with: Configuration → Import YAML (or POST /api/v1/config/yaml).
`

type storedLayer struct {
	ref     ScopeRef
	section string
	doc     Doc
}

func (s *Service) allLayers(ctx context.Context) ([]storedLayer, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT scope, scope_id, section, doc FROM config_layers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedLayer
	for rows.Next() {
		var l storedLayer
		var doc string
		if err := rows.Scan(&l.ref.Scope, &l.ref.ID, &l.section, &doc); err != nil {
			return nil, err
		}
		if l.doc, err = ParseDoc([]byte(doc)); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ExportYAML returns every layer as YAML.
func (s *Service) ExportYAML(ctx context.Context) ([]byte, error) {
	layers, err := s.allLayers(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := s.fleet.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	ins, err := s.fleet.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	gname, iname := map[int64]string{}, map[int64]string{}
	for _, g := range groups {
		gname[g.ID] = g.Name
	}
	for _, in := range ins {
		iname[in.ID] = in.Name
	}
	f := File{Version: 1, Base: map[string]Doc{}, Groups: map[string]map[string]Doc{}, Instances: map[string]map[string]Doc{}}
	for _, l := range layers {
		switch l.ref.Scope {
		case ScopeBase:
			f.Base[l.section] = l.doc
		case ScopeGroup:
			n, ok := gname[l.ref.ID]
			if !ok {
				continue
			}
			if f.Groups[n] == nil {
				f.Groups[n] = map[string]Doc{}
			}
			f.Groups[n][l.section] = l.doc
		case ScopeInstance:
			n, ok := iname[l.ref.ID]
			if !ok {
				continue
			}
			if f.Instances[n] == nil {
				f.Instances[n] = map[string]Doc{}
			}
			f.Instances[n][l.section] = l.doc
		}
	}
	b, err := yaml.Marshal(f)
	if err != nil {
		return nil, err
	}
	return append([]byte(fileHeader+fmt.Sprintf("# Exported %s\n", time.Now().UTC().Format(time.RFC3339))), b...), nil
}

// ImportChange is one layer an import would create, update or delete.
type ImportChange struct {
	Scope   string `json:"scope"` // base | group | instance
	Name    string `json:"name,omitempty"`
	Section string `json:"section"`
	Action  string `json:"action"` // create | update | delete
}

// ImportResult describes an import (or a dry run of one).
type ImportResult struct {
	Changes   []ImportChange `json:"changes"`
	Unchanged int            `json:"unchanged"`
	Applied   bool           `json:"applied"`
}

// ImportError is a problem with the file; nothing was changed.
type ImportError struct{ Msg string }

func (e *ImportError) Error() string { return e.Msg }

// normalize turns YAML-decoded values into the JSON shapes layers use
// (map[string]any, []any, float64), so validation and comparison work.
func normalize(v any) (Doc, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return ParseDoc(b)
}

// ImportYAML sets layers from a YAML file. Layers in the file replace the
// stored ones for the same scope and section. With prune, stored layers the
// file doesn't mention are deleted. With dryRun nothing is written. Groups
// and instances are matched by name and must already exist.
func (s *Service) ImportYAML(ctx context.Context, data []byte, dryRun, prune bool, by string) (*ImportResult, error) {
	var f File
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, &ImportError{"not a GuardTower settings file: " + err.Error()}
	}
	if f.Version != 1 {
		return nil, &ImportError{fmt.Sprintf("unsupported file version %d (expected 1)", f.Version)}
	}
	groups, err := s.fleet.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	ins, err := s.fleet.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	gid, iid := map[string]int64{}, map[string]int64{}
	gname, iname := map[int64]string{}, map[int64]string{}
	for _, g := range groups {
		gid[g.Name], gname[g.ID] = g.ID, g.Name
	}
	for _, in := range ins {
		iid[in.Name], iname[in.ID] = in.ID, in.Name
	}

	type want struct {
		ref     ScopeRef
		name    string
		section string
		doc     Doc
	}
	var wants []want
	var problems []string
	add := func(ref ScopeRef, name string, secs map[string]Doc) {
		for sec, d := range secs {
			def := SectionByKey(sec)
			if def == nil {
				problems = append(problems, fmt.Sprintf("%s %q: unknown section %q", ref.Scope, name, sec))
				continue
			}
			nd, err := normalize(d)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s %q %s: %v", ref.Scope, name, sec, err))
				continue
			}
			clean, err := ValidateLayer(def, nd)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s %q %s: %v", ref.Scope, name, sec, err))
				continue
			}
			wants = append(wants, want{ref, name, sec, clean})
		}
	}
	add(ScopeRef{Scope: ScopeBase}, "", f.Base)
	for n, secs := range f.Groups {
		id, ok := gid[n]
		if !ok {
			problems = append(problems, fmt.Sprintf("group %q doesn't exist here; create it first", n))
			continue
		}
		add(ScopeRef{Scope: ScopeGroup, ID: id}, n, secs)
	}
	for n, secs := range f.Instances {
		id, ok := iid[n]
		if !ok {
			problems = append(problems, fmt.Sprintf("instance %q doesn't exist here; add it first", n))
			continue
		}
		add(ScopeRef{Scope: ScopeInstance, ID: id}, n, secs)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, &ImportError{"nothing was imported: " + strings.Join(problems, "; ")}
	}

	existing, err := s.allLayers(ctx)
	if err != nil {
		return nil, err
	}
	type key struct {
		ref     ScopeRef
		section string
	}
	have := map[key]Doc{}
	for _, l := range existing {
		have[key{l.ref, l.section}] = l.doc
	}
	res := &ImportResult{Changes: []ImportChange{}}
	seen := map[key]bool{}
	for _, w := range wants {
		k := key{w.ref, w.section}
		seen[k] = true
		old, ok := have[k]
		switch {
		case !ok:
			res.Changes = append(res.Changes, ImportChange{Scope: w.ref.Scope, Name: w.name, Section: w.section, Action: "create"})
		case !reflect.DeepEqual(asJSON(old), asJSON(w.doc)):
			res.Changes = append(res.Changes, ImportChange{Scope: w.ref.Scope, Name: w.name, Section: w.section, Action: "update"})
		default:
			res.Unchanged++
		}
	}
	var deletes []key
	if prune {
		for k := range have {
			if seen[k] {
				continue
			}
			name := gname[k.ref.ID]
			if k.ref.Scope == ScopeInstance {
				name = iname[k.ref.ID]
			}
			deletes = append(deletes, k)
			res.Changes = append(res.Changes, ImportChange{Scope: k.ref.Scope, Name: name, Section: k.section, Action: "delete"})
		}
	}
	sort.Slice(res.Changes, func(i, j int) bool {
		a, b := res.Changes[i], res.Changes[j]
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Section < b.Section
	})
	if dryRun || len(res.Changes) == 0 {
		return res, nil
	}
	for _, w := range wants {
		if _, err := s.SetLayer(ctx, w.ref, w.section, w.doc, by); err != nil {
			return nil, err
		}
	}
	for _, k := range deletes {
		if _, err := s.SetLayer(ctx, k.ref, k.section, Doc{}, by); err != nil {
			return nil, err
		}
	}
	res.Applied = true
	s.Kick()
	return res, nil
}
