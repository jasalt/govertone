package patch

import (
	"reflect"
	"testing"

	"github.com/example/letgo-sointu/internal/instruments"
)

func TestRegistryOrderRedefinitionAndGeneration(t *testing.T) {
	r, err := NewRegistry(NewCompiler(), minimal(t, "a", "sine", 1), minimal(t, "b", "sine", 1))
	if err != nil {
		t.Fatal(err)
	}
	start := r.Snapshot()
	update, err := r.PrepareUpsert(minimal(t, "a", "saw", 2))
	if err != nil {
		t.Fatal(err)
	}
	if update.Compiled.Generation != 2 || !update.Changed {
		t.Fatalf("bad update %#v", update)
	}
	if err = r.Commit(update); err != nil {
		t.Fatal(err)
	}
	snapshot := r.Snapshot()
	if snapshot.Generation != 2 || snapshot.Layout.OrderedIDs[0] != "a" || snapshot.Layout.Instruments["b"].FirstVoice != 2 {
		t.Fatalf("bad snapshot %#v", snapshot)
	}
	if start.Fingerprint == snapshot.Fingerprint {
		t.Fatal("fingerprint did not change")
	}
}
func TestRegistryIdenticalElisionAndRollback(t *testing.T) {
	spec := minimal(t, "a", "sine", 1)
	r, err := NewRegistry(NewCompiler(), spec)
	if err != nil {
		t.Fatal(err)
	}
	update, err := r.PrepareUpsert(spec)
	if err != nil {
		t.Fatal(err)
	}
	if update.Changed {
		t.Fatal("identical update changed")
	}
	if err = r.Commit(update); err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	bad := spec
	bad.Units = []UnitSpec{mustUnit(t, "mulp", nil)}
	if _, err = r.PrepareUpsert(bad); err == nil {
		t.Fatal("invalid candidate accepted")
	}
	after := r.Snapshot()
	if before.Generation != after.Generation || before.Fingerprint != after.Fingerprint {
		t.Fatal("failed update mutated registry")
	}
}
func TestRegistryRemoveAndReaddAppends(t *testing.T) {
	r, _ := NewRegistry(NewCompiler(), minimal(t, "a", "sine", 1), minimal(t, "b", "sine", 1))
	remove, err := r.PrepareRemove("a")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Commit(remove); err != nil {
		t.Fatal(err)
	}
	add, err := r.PrepareUpsert(minimal(t, "a", "sine", 1))
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Commit(add); err != nil {
		t.Fatal(err)
	}
	ids := r.Snapshot().Layout.OrderedIDs
	if !reflect.DeepEqual(ids, []InstrumentID{"b", "a"}) {
		t.Fatalf("order %v", ids)
	}
}
func TestBuiltinsUseTypedPipeline(t *testing.T) {
	r, err := NewBuiltinRegistry()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := r.Snapshot()
	if snapshot.Generation != 1 || snapshot.Layout.TotalVoices != 24 {
		t.Fatalf("bad builtins %#v", snapshot)
	}
	original := instruments.BuiltinProvider{}.Patch()
	compiled := r.Patch()
	if len(original) != len(compiled) {
		t.Fatal("instrument count")
	}
	for i := range original {
		for j := range original[i].Units {
			for name, want := range original[i].Units[j].Parameters {
				if got := compiled[i].Units[j].Parameters[name]; got != want {
					t.Fatalf("unit %d/%d parameter %s = %d, want %d", i, j, name, got, want)
				}
			}
		}
	}
}
