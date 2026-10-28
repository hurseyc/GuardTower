// Package config is GuardTower's configuration engine: it models AdGuard
// Home settings as sections, merges layered desired state (base → group →
// instance), compares it with live instances and applies differences.
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hurseyc/guardtower/internal/agh"
)

// Kind is how a field is stored, merged and compared.
type Kind string

const (
	KindBool     Kind = "bool"
	KindInt      Kind = "int"
	KindString   Kind = "string"
	KindEnum     Kind = "enum"
	KindList     Kind = "list"   // ordered strings; a layer replaces the whole list
	KindSet      Kind = "set"    // unordered strings; layers add/remove items
	KindKeyed    Kind = "keyed"  // objects identified by a key; layers upsert/remove items
	KindObject   Kind = "object" // opaque JSON object; a layer replaces it
	KindDuration Kind = "duration_ms"
)

// Field describes one setting.
type Field struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Help     string   `json:"help,omitempty"`
	Kind     Kind     `json:"kind"`
	Enum     []string `json:"enum,omitempty"`
	KeyLabel string   `json:"key_label,omitempty"` // keyed: how to describe an item's key
	// keyFn derives an item's identity for keyed fields.
	keyFn func(item map[string]any) string
	// itemFields are the editable attributes of keyed items (for the UI).
	ItemFields []Field `json:"item_fields,omitempty"`
}

// Doc is a section's values (field name → value). Values are JSON-decoded
// (bool, float64, string, []any, map[string]any).
type Doc map[string]any

// Section is a group of related AdGuard Home settings that are read and
// written together.
type Section struct {
	Key    string  `json:"key"`
	Label  string  `json:"label"`
	Help   string  `json:"help"`
	Fields []Field `json:"fields"`
	// read fetches the section's live values.
	read func(ctx context.Context, c *agh.Client) (Doc, error)
	// apply makes the instance match desired (only managed fields are set);
	// live is the current state as returned by read.
	apply func(ctx context.Context, c *agh.Client, desired, live Doc) error
}

// Field returns a field by name.
func (s *Section) Field(name string) *Field {
	for i := range s.Fields {
		if s.Fields[i].Name == name {
			return &s.Fields[i]
		}
	}
	return nil
}

// Sections is the registry, in display order.
var Sections []*Section

// SectionByKey looks up a section.
func SectionByKey(k string) *Section {
	for _, s := range Sections {
		if s.Key == k {
			return s
		}
	}
	return nil
}

// ---- helpers for reading AGH responses into docs ----

func getMap(ctx context.Context, c *agh.Client, path string) (map[string]any, error) {
	var m map[string]any
	if err := c.Get(ctx, path, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// pick copies the section's fields from raw, normalising values.
func pick(s *Section, raw map[string]any) Doc {
	d := Doc{}
	for _, f := range s.Fields {
		v, ok := raw[f.Name]
		if !ok {
			continue
		}
		d[f.Name] = normalizeValue(f, v)
	}
	return d
}

// normalizeValue gives every value a canonical form so equal settings compare equal.
func normalizeValue(f Field, v any) any {
	switch f.Kind {
	case KindList:
		return toStrings(v)
	case KindSet:
		// Order is kept (it matters for custom rules) but duplicates are dropped;
		// comparisons treat sets as unordered.
		return dedupeOrdered(toStrings(v))
	case KindKeyed:
		items := toItems(v)
		sort.Slice(items, func(i, j int) bool { return f.keyFn(items[i]) < f.keyFn(items[j]) })
		return items
	case KindInt, KindDuration:
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case int64:
			return float64(n)
		}
		return v
	case KindObject:
		if m, ok := v.(map[string]any); ok {
			return m
		}
		return map[string]any{}
	}
	return v
}

func toStrings(v any) []string {
	out := []string{}
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
		}
	case []string:
		for _, s := range x {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func dedupeOrdered(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func toItems(v any) []map[string]any {
	out := []map[string]any{}
	if arr, ok := v.([]any); ok {
		for _, e := range arr {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	if arr, ok := v.([]map[string]any); ok {
		out = append(out, arr...)
	}
	return out
}

func asJSON(v any) any {
	// Round-trip through JSON so Go values match decoded ones in comparisons.
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// merged returns live with desired's managed fields laid over it, for APIs
// that require a complete object.
func merged(live, desired Doc) map[string]any {
	out := map[string]any{}
	for k, v := range live {
		out[k] = v
	}
	for k, v := range desired {
		out[k] = v
	}
	return out
}

func errf(section, format string, a ...any) error {
	return fmt.Errorf(section+": "+format, a...)
}
