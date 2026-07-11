package structdiff

import (
	"reflect"
	"testing"
)

type inner struct {
	A int    `json:"a"`
	B string `json:"b"`
}

type outer struct {
	Name  string         `json:"name"`
	Inner inner          `json:"inner"`
	Flags map[string]any `json:"flags"`
}

func TestDiffNoDifference(t *testing.T) {
	a := outer{Name: "x", Inner: inner{A: 1, B: "y"}, Flags: map[string]any{"k": "v"}}
	b := outer{Name: "x", Inner: inner{A: 1, B: "y"}, Flags: map[string]any{"k": "v"}}
	diffs, err := Diff(&a, &b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 0 {
		t.Fatalf("want no diffs, got %v", diffs)
	}
}

func TestDiffFindsNestedLeaf(t *testing.T) {
	a := outer{Name: "x", Inner: inner{A: 1, B: "y"}}
	b := outer{Name: "x", Inner: inner{A: 2, B: "y"}}
	diffs, err := Diff(&a, &b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(diffs, []string{"inner.a"}) {
		t.Fatalf("want [inner.a], got %v", diffs)
	}
}

func TestDiffFindsMultipleFields(t *testing.T) {
	a := outer{Name: "x", Inner: inner{A: 1, B: "y"}}
	b := outer{Name: "z", Inner: inner{A: 2, B: "y"}}
	diffs, err := Diff(&a, &b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(diffs, []string{"inner.a", "name"}) {
		t.Fatalf("want [inner.a name], got %v", diffs)
	}
}

func TestDiffMapField(t *testing.T) {
	a := outer{Flags: map[string]any{"max_num_seqs": float64(8)}}
	b := outer{Flags: map[string]any{"max_num_seqs": float64(16)}}
	diffs, err := Diff(&a, &b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(diffs, []string{"flags.max_num_seqs"}) {
		t.Fatalf("want [flags.max_num_seqs], got %v", diffs)
	}
}

func TestDiffPresenceMismatch(t *testing.T) {
	a := outer{Flags: map[string]any{"a": 1}}
	b := outer{Flags: map[string]any{"a": 1, "b": 2}}
	diffs, err := Diff(&a, &b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(diffs, []string{"flags.b"}) {
		t.Fatalf("want [flags.b], got %v", diffs)
	}
}
