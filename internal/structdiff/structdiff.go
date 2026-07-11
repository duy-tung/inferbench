// Package structdiff provides one structural diff primitive over
// JSON-marshalable Go values: the single mechanism behind every
// single-variable-rule / no-full-matrix-sweep check in this repo (the
// `compare` CLI, the `sweep` derived-workload guard, and the `experiment`
// governance framework all call this, directly or via
// internal/manifest.Diff, so "differs" means exactly the same thing
// everywhere it is checked — experiments.md rule 10 and §5).
package structdiff

import (
	"encoding/json"
	"reflect"
	"sort"
)

// Diff returns the sorted, dotted-path list of leaf fields on which a and b
// differ, computed by JSON round-tripping both into map[string]any and
// recursively comparing every key present on either side. Fields present on
// one side and absent on the other count as differing. Arrays are compared
// as whole values (not element-by-element paths) — coarse-grained but
// correct: this repo's structs use arrays only for small declared lists
// (tags, phases), never as the thing whose single-variable status matters.
func Diff(a, b any) ([]string, error) {
	am, err := toMap(a)
	if err != nil {
		return nil, err
	}
	bm, err := toMap(b)
	if err != nil {
		return nil, err
	}
	var out []string
	diffMaps("", am, bm, &out)
	sort.Strings(out)
	return out, nil
}

func toMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func diffMaps(prefix string, a, b map[string]any, out *[]string) {
	keys := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}
	for k := range keys {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		av, aok := a[k]
		bv, bok := b[k]
		if aok != bok {
			*out = append(*out, path)
			continue
		}
		diffValue(path, av, bv, out)
	}
}

func diffValue(path string, a, b any, out *[]string) {
	am, aIsMap := a.(map[string]any)
	bm, bIsMap := b.(map[string]any)
	if aIsMap && bIsMap {
		diffMaps(path, am, bm, out)
		return
	}
	if !reflect.DeepEqual(a, b) {
		*out = append(*out, path)
	}
}
