package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Scope values for layers, in merge order.
const (
	ScopeBase     = "base"
	ScopeGroup    = "group"
	ScopeInstance = "instance"
)

// Layer is one level of desired state for a section. Its Doc holds only the
// fields that layer sets:
//   - scalar, list, object and duration fields: the value (replaces lower layers)
//   - set fields:   {"add": [..], "remove": [..]}
//   - keyed fields: {"upsert": [items], "remove": [keys]}
type Layer struct {
	Scope string `json:"scope"`
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Doc   Doc    `json:"doc"`
}

// Origin says which layer a value came from.
type Origin struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`
}

// Origins maps field → origin, and for set/keyed fields "field\x00item" → origin.
type Origins map[string]Origin

// ItemKey builds the Origins key for a set or keyed item.
func ItemKey(field, item string) string { return field + "\x00" + item }

// Merge combines layers (lowest first) into the effective desired document.
// Only fields set by at least one layer appear in the result: unset fields
// are unmanaged and left alone on instances.
func Merge(sec *Section, layers []Layer) (Doc, Origins) {
	out := Doc{}
	orig := Origins{}
	for _, l := range layers {
		o := Origin{Scope: l.Scope, Name: l.Name}
		for _, f := range sec.Fields {
			v, ok := l.Doc[f.Name]
			if !ok {
				continue
			}
			switch f.Kind {
			case KindSet:
				ops := asMap(v)
				cur := toStrings(out[f.Name])
				for _, add := range toStrings(ops["add"]) {
					if !containsStr(cur, add) {
						cur = append(cur, add)
					}
					orig[ItemKey(f.Name, add)] = o
				}
				for _, rm := range toStrings(ops["remove"]) {
					cur = removeStr(cur, rm)
					delete(orig, ItemKey(f.Name, rm))
				}
				out[f.Name] = cur
				if _, set := orig[f.Name]; !set {
					orig[f.Name] = o
				}
			case KindKeyed:
				ops := asMap(v)
				items := map[string]map[string]any{}
				for _, it := range toItems(out[f.Name]) {
					items[f.keyFn(it)] = it
				}
				for _, it := range toItems(ops["upsert"]) {
					k := f.keyFn(it)
					items[k] = it
					orig[ItemKey(f.Name, k)] = o
				}
				for _, k := range toStrings(ops["remove"]) {
					delete(items, k)
					delete(orig, ItemKey(f.Name, k))
				}
				keys := make([]string, 0, len(items))
				for k := range items {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				list := make([]map[string]any, 0, len(keys))
				for _, k := range keys {
					list = append(list, items[k])
				}
				out[f.Name] = list
				if _, set := orig[f.Name]; !set {
					orig[f.Name] = o
				}
			default:
				out[f.Name] = normalizeValue(f, v)
				orig[f.Name] = o
			}
		}
	}
	// Canonicalise (JSON round trip) so comparisons with live docs are exact.
	for k, v := range out {
		out[k] = asJSON(v)
	}
	return out, orig
}

// LayerFromLive converts a live section document into layer form, used when
// importing an instance's current settings as a baseline.
func LayerFromLive(sec *Section, live Doc) Doc {
	d := Doc{}
	for _, f := range sec.Fields {
		v, ok := live[f.Name]
		if !ok {
			continue
		}
		switch f.Kind {
		case KindSet:
			d[f.Name] = map[string]any{"add": toStrings(v), "remove": []string{}}
		case KindKeyed:
			d[f.Name] = map[string]any{"upsert": toItems(v), "remove": []string{}}
		default:
			d[f.Name] = v
		}
	}
	return Doc(asJSON(d).(map[string]any))
}

// LayerToMatch returns the smallest layer that, on top of inherited (the
// merged lower layers), makes the effective value equal live. Fields the
// lower layers already set to the same value are left out, so an import into
// a group doesn't copy Base; list items Base adds but live lacks are removed.
func LayerToMatch(sec *Section, inherited, live Doc) Doc {
	d := Doc{}
	for _, f := range sec.Fields {
		v, ok := live[f.Name]
		if !ok {
			continue
		}
		inh, managed := inherited[f.Name]
		switch f.Kind {
		case KindSet:
			want, have := toStrings(v), toStrings(inh)
			add, remove := []string{}, []string{}
			for _, x := range want {
				if !containsStr(have, x) {
					add = append(add, x)
				}
			}
			for _, x := range have {
				if !containsStr(want, x) {
					remove = append(remove, x)
				}
			}
			if !managed || len(add)+len(remove) > 0 {
				d[f.Name] = map[string]any{"add": add, "remove": remove}
			}
		case KindKeyed:
			haveBy := map[string]any{}
			for _, it := range toItems(inh) {
				haveBy[f.keyFn(it)] = asJSON(it)
			}
			upsert, remove := []map[string]any{}, []string{}
			seen := map[string]bool{}
			for _, it := range toItems(v) {
				k := f.keyFn(it)
				seen[k] = true
				if h, ok := haveBy[k]; !ok || !reflect.DeepEqual(h, asJSON(it)) {
					upsert = append(upsert, it)
				}
			}
			for _, it := range toItems(inh) {
				if k := f.keyFn(it); !seen[k] {
					remove = append(remove, k)
				}
			}
			sort.Strings(remove)
			if !managed || len(upsert)+len(remove) > 0 {
				d[f.Name] = map[string]any{"upsert": upsert, "remove": remove}
			}
		default:
			if !managed || !reflect.DeepEqual(asJSON(normalizeValue(f, v)), asJSON(inh)) {
				d[f.Name] = v
			}
		}
	}
	return Doc(asJSON(d).(map[string]any))
}

// ValidateLayer checks a layer document against the section schema and
// returns it in canonical form.
func ValidateLayer(sec *Section, d Doc) (Doc, error) {
	out := Doc{}
	for name, v := range d {
		f := sec.Field(name)
		if f == nil {
			return nil, fmt.Errorf("%s: unknown field %q", sec.Key, name)
		}
		switch f.Kind {
		case KindBool:
			if _, ok := v.(bool); !ok {
				return nil, fmt.Errorf("%s.%s must be true or false", sec.Key, name)
			}
		case KindInt, KindDuration:
			n, ok := v.(float64)
			if !ok || n != float64(int64(n)) || n < 0 {
				return nil, fmt.Errorf("%s.%s must be a whole number ≥ 0", sec.Key, name)
			}
		case KindString:
			if _, ok := v.(string); !ok {
				return nil, fmt.Errorf("%s.%s must be text", sec.Key, name)
			}
		case KindEnum:
			s, _ := v.(string)
			if !containsStr(f.Enum, s) {
				return nil, fmt.Errorf("%s.%s must be one of %s", sec.Key, name, strings.Join(f.Enum, ", "))
			}
		case KindList:
			if _, ok := v.([]any); !ok {
				return nil, fmt.Errorf("%s.%s must be a list", sec.Key, name)
			}
			v = toStrings(v)
		case KindObject:
			if _, ok := v.(map[string]any); !ok {
				return nil, fmt.Errorf("%s.%s must be an object", sec.Key, name)
			}
		case KindSet:
			m, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf(`%s.%s must be {"add": [...], "remove": [...]}`, sec.Key, name)
			}
			v = map[string]any{"add": dedupeOrdered(toStrings(m["add"])), "remove": dedupeOrdered(toStrings(m["remove"]))}
		case KindKeyed:
			m, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf(`%s.%s must be {"upsert": [...], "remove": [...]}`, sec.Key, name)
			}
			items := toItems(m["upsert"])
			for _, it := range items {
				if f.keyFn(it) == "" || strings.HasPrefix(f.keyFn(it), " → ") {
					return nil, fmt.Errorf("%s.%s: every item needs a %s", sec.Key, name, strings.ToLower(f.KeyLabel))
				}
			}
			v = map[string]any{"upsert": items, "remove": toStrings(m["remove"])}
		}
		out[name] = v
	}
	return Doc(asJSON(out).(map[string]any)), nil
}

// Change is one difference between desired and live state.
type Change struct {
	Section string `json:"section"`
	Field   string `json:"field"`
	Label   string `json:"label"`
	Op      string `json:"op"`             // set | add | remove | update
	Item    string `json:"item,omitempty"` // set value or keyed-item key
	From    any    `json:"from,omitempty"`
	To      any    `json:"to,omitempty"`
}

// Diff lists what must change on an instance (live) to match desired.
// Fields absent from desired are unmanaged and ignored; fields the instance
// doesn't report (older AdGuard Home releases) are skipped too, see Unsupported.
func Diff(sec *Section, desired, live Doc) []Change {
	var out []Change
	for _, f := range sec.Fields {
		want, managed := desired[f.Name]
		if !managed {
			continue
		}
		if _, supported := live[f.Name]; !supported {
			continue
		}
		have := asJSON(live[f.Name])
		switch f.Kind {
		case KindSet:
			w, h := toStrings(want), toStrings(have)
			for _, s := range w {
				if !containsStr(h, s) {
					out = append(out, Change{Section: sec.Key, Field: f.Name, Label: f.Label, Op: "add", Item: s})
				}
			}
			for _, s := range h {
				if !containsStr(w, s) {
					out = append(out, Change{Section: sec.Key, Field: f.Name, Label: f.Label, Op: "remove", Item: s})
				}
			}
		case KindKeyed:
			wm, hm := map[string]any{}, map[string]any{}
			for _, it := range toItems(want) {
				wm[f.keyFn(it)] = it
			}
			for _, it := range toItems(have) {
				hm[f.keyFn(it)] = it
			}
			for _, k := range sortedKeys(wm) {
				h, ok := hm[k]
				switch {
				case !ok:
					out = append(out, Change{Section: sec.Key, Field: f.Name, Label: f.Label, Op: "add", Item: k, To: wm[k]})
				case !itemEqual(sec, wm[k], h):
					out = append(out, Change{Section: sec.Key, Field: f.Name, Label: f.Label, Op: "update", Item: k, From: h, To: wm[k]})
				}
			}
			for _, k := range sortedKeys(hm) {
				if _, ok := wm[k]; !ok {
					out = append(out, Change{Section: sec.Key, Field: f.Name, Label: f.Label, Op: "remove", Item: k, From: hm[k]})
				}
			}
		default:
			if !reflect.DeepEqual(asJSON(want), have) {
				out = append(out, Change{Section: sec.Key, Field: f.Name, Label: f.Label, Op: "set", From: have, To: want})
			}
		}
	}
	return out
}

// Unsupported lists managed fields the instance doesn't report, i.e. settings
// its AdGuard Home version doesn't have.
func Unsupported(sec *Section, desired, live Doc) []string {
	var out []string
	for _, f := range sec.Fields {
		if _, managed := desired[f.Name]; !managed {
			continue
		}
		if _, ok := live[f.Name]; !ok {
			out = append(out, f.Name)
		}
	}
	return out
}

// itemEqual compares keyed items on the attributes present in the desired
// item, so a desired item doesn't need every attribute AGH reports.
func itemEqual(sec *Section, want, have any) bool {
	w, h := asMap(want), asMap(have)
	if sec.Key == "clients" {
		w, h = clientItem(w), clientItem(h)
	}
	for k, wv := range w {
		if !reflect.DeepEqual(asJSON(wv), asJSON(h[k])) {
			return false
		}
	}
	return true
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	if d, ok := v.(Doc); ok {
		return d
	}
	return map[string]any{}
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func removeStr(list []string, s string) []string {
	out := list[:0]
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// ParseDoc decodes a JSON object into a Doc.
func ParseDoc(b []byte) (Doc, error) {
	var d Doc
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	if d == nil {
		return nil, errors.New("expected a JSON object")
	}
	return d, nil
}
