package patch

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/vsariola/sointu"
	sointuvm "github.com/vsariola/sointu/vm"
)

// SointuVoicesPerVM is the voice capacity of one pinned Sointu VM (its
// polyphony bitmask and per-voice state arrays are 32 wide).
const SointuVoicesPerVM = 32

// ShardSeedBase is the noise RNG seed of shard 0, equal to the VM default so a
// single-shard patch renders exactly as an unsharded one.
const ShardSeedBase uint32 = 1

// ShardSeed returns the deterministic noise seed of shard n.
func ShardSeed(n int) uint32 { return ShardSeedBase + uint32(n)*0x9E3779B1 }

// ShardInstrument is one logical instrument, or one slice of it, inside a
// shard patch.
type ShardInstrument struct {
	ID                  InstrumentID `json:"id"`
	Index               int          `json:"index"` // instrument index inside the shard patch
	Slice               int          `json:"slice"`
	Slices              int          `json:"slices"`
	FirstLogicalVoice   int          `json:"first_logical_voice"`
	FirstLocalVoice     int          `json:"first_local_voice"`
	NumVoices           int          `json:"voices"`
	HardReleaseOperands []int        `json:"-"`
}

// CompiledShard is an ordinary Sointu patch of at most VoicesPerVM voices that
// runs in its own VM. Binding and hard-release operands are shard-local.
type CompiledShard struct {
	Index       int               `json:"index"`
	Patch       sointu.Patch      `json:"-"`
	Instruments []ShardInstrument `json:"instruments"`
	Bindings    []ControlBinding  `json:"control_bindings,omitempty"`
	TotalVoices int               `json:"voices"`
	Fingerprint string            `json:"fingerprint"`
	Seed        uint32            `json:"seed"`
}

// VoiceAddress locates a logical voice in a shard VM.
type VoiceAddress struct {
	Shard int `json:"shard"`
	Local int `json:"local"`
}

// shardEntry is a placement of (a slice of) one logical instrument.
type shardEntry struct {
	instrument int // logical instrument index
	slice      int
	slices     int
	voices     int
	firstVoice int // first logical voice of this slice
}

// placementItem is the indivisible unit of placement: a whole coupling group
// or one slice of a sliceable instrument.
type placementItem struct {
	entries []shardEntry
	voices  int
}

// couplingGroups returns connected components of instruments that must share
// one VM, ordered by first member, and the instruments that cannot be split
// into slices: voice-addressed sends address absolute voices, and an `in`
// unit may read signal written by another voice of the same instrument.
// Disabled units are ignored because Sointu skips them.
func couplingGroups(spec PatchSpec) ([][]int, []bool) {
	n := len(spec.Instruments)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if ra < rb {
			parent[rb] = ra
		} else {
			parent[ra] = rb
		}
	}
	index := make(map[InstrumentID]int, n)
	for i, in := range spec.Instruments {
		index[in.ID] = i
	}
	unsliceable := make([]bool, n)
	const channels = 8
	writers := [channels][]int{}
	readers := [channels][]int{}
	mark := func(list *[channels][]int, instrument, channel int, stereo bool) {
		for c := channel; c <= channel+boolInt(stereo) && c < channels; c++ {
			if c >= 0 {
				list[c] = append(list[c], instrument)
			}
		}
	}
	for i, in := range spec.Instruments {
		for _, u := range in.Units {
			if u.Disabled {
				continue
			}
			channel := 0
			if v, ok := u.Parameters["channel"]; ok && v.Kind == ParameterInteger {
				channel = v.Integer
			}
			switch u.Type {
			case "out":
				mark(&writers, i, 0, u.Stereo)
			case "outaux":
				mark(&writers, i, 0, u.Stereo)
				mark(&writers, i, 2, u.Stereo)
			case "aux":
				mark(&writers, i, channel, u.Stereo)
			case "in":
				mark(&readers, i, channel, u.Stereo)
				unsliceable[i] = true
			case "send":
				target, ok := u.Parameters["target"]
				if !ok || target.Kind != ParameterReference || target.Reference == nil {
					continue
				}
				targetID := target.Reference.Instrument
				if targetID == "" {
					targetID = in.ID
				}
				if j, exists := index[targetID]; exists && j != i {
					union(i, j)
				}
				if v, ok := u.Parameters["voice"]; ok && v.Kind == ParameterInteger && v.Integer != 0 {
					unsliceable[i] = true
				}
			}
		}
	}
	for c := 0; c < channels; c++ {
		for _, r := range readers[c] {
			for _, w := range writers[c] {
				union(r, w)
			}
		}
	}
	byRoot := map[int][]int{}
	roots := []int{}
	for i := 0; i < n; i++ {
		root := find(i)
		if _, seen := byRoot[root]; !seen {
			roots = append(roots, root)
		}
		byRoot[root] = append(byRoot[root], i)
	}
	groups := make([][]int, 0, len(roots))
	for _, root := range roots {
		groups = append(groups, byRoot[root])
	}
	return groups, unsliceable
}

func instrumentList(spec PatchSpec, members []int) string {
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = ":" + string(spec.Instruments[m].ID)
	}
	return strings.Join(names, ", ")
}

// planShards assigns logical instruments and slices to shards. Coupled groups
// are never split; only an uncoupled, sliceable instrument may exceed
// VoicesPerVM, as identical slices in separate VMs. Placement is
// first-fit in logical order, and entries inside a shard keep logical order,
// so a patch that fits one VM yields exactly the original instrument order.
func (c *Compiler) planShards(spec PatchSpec) ([][]shardEntry, []Diagnostic) {
	perVM := c.VoicesPerVM
	if perVM <= 0 {
		perVM = SointuVoicesPerVM
	}
	firstVoice := make([]int, len(spec.Instruments))
	total := 0
	for i, in := range spec.Instruments {
		firstVoice[i] = total
		total += in.Voices
	}
	groups, unsliceable := couplingGroups(spec)
	diagnostics := []Diagnostic{}
	items := []placementItem{}
	for _, members := range groups {
		voices := 0
		for _, m := range members {
			voices += spec.Instruments[m].Voices
		}
		if len(members) > 1 {
			if voices > perVM {
				d := diag("coupled-group-voice-limit-exceeded", spec.Instruments[members[0]].ID, 0, "", "", fmt.Sprintf("Synths %s are coupled through aux/in channels or cross-synth send and must share one Sointu VM; together they use %d voices, limit %d", instrumentList(spec, members), voices, perVM))
				d.Details = map[string]any{"instruments": instrumentList(spec, members), "voices": voices, "voices_per_vm": perVM}
				diagnostics = append(diagnostics, d)
				continue
			}
			item := placementItem{voices: voices}
			for _, m := range members {
				item.entries = append(item.entries, shardEntry{instrument: m, slices: 1, voices: spec.Instruments[m].Voices, firstVoice: firstVoice[m]})
			}
			items = append(items, item)
			continue
		}
		m := members[0]
		in := spec.Instruments[m]
		if in.Voices > perVM && unsliceable[m] {
			diagnostics = append(diagnostics, diag("coupled-group-voice-limit-exceeded", in.ID, 0, "", "", fmt.Sprintf("Synth :%s uses an `in` unit or voice-addressed send, so its voices are coupled and cannot be split across Sointu VMs; %d voices exceeds %d", in.ID, in.Voices, perVM)))
			continue
		}
		slices := (in.Voices + perVM - 1) / perVM
		for s := 0; s < slices; s++ {
			voices := min(perVM, in.Voices-s*perVM)
			items = append(items, placementItem{voices: voices, entries: []shardEntry{{instrument: m, slice: s, slices: slices, voices: voices, firstVoice: firstVoice[m] + s*perVM}}})
		}
	}
	if hasErrors(diagnostics) {
		return nil, diagnostics
	}
	var shards [][]shardEntry
	used := []int{}
	for _, item := range items {
		target := -1
		for s := range shards {
			if used[s]+item.voices <= perVM {
				target = s
				break
			}
		}
		if target < 0 {
			shards = append(shards, nil)
			used = append(used, 0)
			target = len(shards) - 1
		}
		shards[target] = append(shards[target], item.entries...)
		used[target] += item.voices
	}
	if c.MaxShards > 0 && len(shards) > c.MaxShards {
		return nil, []Diagnostic{diag("shard-limit-exceeded", "", 0, "", "", fmt.Sprintf("patch needs %d Sointu VMs; maximum is %d", len(shards), c.MaxShards))}
	}
	for _, entries := range shards {
		sort.SliceStable(entries, func(a, b int) bool {
			if entries[a].instrument != entries[b].instrument {
				return entries[a].instrument < entries[b].instrument
			}
			return entries[a].slice < entries[b].slice
		})
	}
	return shards, nil
}

// buildShards turns a placement into per-VM Sointu patches, validates each
// one's bytecode, and computes shard-local control and hard-release operands.
// logical holds one fully resolved Sointu instrument per logical instrument.
func (c *Compiler) buildShards(spec PatchSpec, logical sointu.Patch, plan [][]shardEntry, bpm int) ([]CompiledShard, []VoiceAddress, error) {
	total := 0
	for _, in := range spec.Instruments {
		total += in.Voices
	}
	voiceMap := make([]VoiceAddress, total)
	shards := make([]CompiledShard, len(plan))
	for s, entries := range plan {
		shard := CompiledShard{Index: s, Seed: ShardSeed(s)}
		local := 0
		for j, entry := range entries {
			source := logical[entry.instrument]
			copied := copyInstrument(source)
			copied.NumVoices = entry.voices
			shard.Patch = append(shard.Patch, copied)
			shard.Instruments = append(shard.Instruments, ShardInstrument{ID: spec.Instruments[entry.instrument].ID, Index: j, Slice: entry.slice, Slices: entry.slices, FirstLogicalVoice: entry.firstVoice, FirstLocalVoice: local, NumVoices: entry.voices})
			for v := 0; v < entry.voices; v++ {
				voiceMap[entry.firstVoice+v] = VoiceAddress{Shard: s, Local: local + v}
			}
			local += entry.voices
		}
		shard.TotalVoices = local
		bytecode, err := sointuvm.NewBytecode(shard.Patch, sointuvm.AllFeatures{}, bpm)
		if err != nil {
			return nil, nil, shardError(s, len(plan), err)
		}
		for j, entry := range entries {
			instrument := spec.Instruments[entry.instrument]
			if !hasEnabledEnvelope(instrument) {
				for unitIndex, unit := range instrument.Units {
					if unit.Disabled {
						continue
					}
					for _, parameter := range outputGainParameters(unit.Type) {
						if operand, ok := bytecode.ParameterOperands[sointuvm.ParameterAddress{Instrument: j, Unit: unitIndex, Parameter: parameter}]; ok {
							shard.Instruments[j].HardReleaseOperands = append(shard.Instruments[j].HardReleaseOperands, operand)
						}
					}
				}
			}
			for unitIndex, unit := range instrument.Units {
				names := make([]string, 0, len(unit.ControlBindings))
				for name := range unit.ControlBindings {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					reference := unit.ControlBindings[name]
					descriptor := instrument.Parameters[reference.Parameter]
					operand, ok := bytecode.ParameterOperands[sointuvm.ParameterAddress{Instrument: j, Unit: unitIndex, Parameter: name}]
					if !ok {
						return nil, nil, &CompileError{[]Diagnostic{diag("control-binding-missing", instrument.ID, unitIndex, unit.ID, name, "Sointu control operand was not generated")}}
					}
					shard.Bindings = append(shard.Bindings, ControlBinding{Index: len(shard.Bindings), InstrumentID: instrument.ID, ParameterID: reference.Parameter, UnitID: unit.ID, UnitIndex: unitIndex, UnitParameter: name, Operand: operand, Scope: descriptor.Scope, Minimum: descriptor.Minimum, Maximum: descriptor.Maximum, Default: descriptor.Default, Smoothing: descriptor.Smoothing, Transform: reference.Transform})
				}
			}
		}
		// Force VM construction before the candidate reaches audio.
		synth, err := (sointuvm.GoSynther{}).Synth(shard.Patch, bpm)
		if err != nil {
			return nil, nil, shardError(s, len(plan), err)
		}
		synth.Close()
		fingerprint, err := shardFingerprint(shard)
		if err != nil {
			return nil, nil, err
		}
		shard.Fingerprint = fingerprint
		shards[s] = shard
	}
	return shards, voiceMap, nil
}

func shardError(shard, count int, err error) error {
	where := ""
	if count > 1 {
		where = fmt.Sprintf(" (VM %d of %d)", shard+1, count)
	}
	return &CompileError{[]Diagnostic{diag("patch-compile-failed", "", 0, "", "", fmt.Sprintf("Sointu rejected patch%s: %v", where, err))}}
}

func shardFingerprint(shard CompiledShard) (string, error) {
	b, err := json.Marshal(struct {
		Patch       sointu.Patch      `json:"patch"`
		Instruments []ShardInstrument `json:"instruments"`
		Seed        uint32            `json:"seed"`
	}{shard.Patch, shard.Instruments, shard.Seed})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b)), nil
}

func copyInstrument(in sointu.Instrument) sointu.Instrument {
	out := in
	out.Units = make([]sointu.Unit, len(in.Units))
	for i, u := range in.Units {
		out.Units[i] = u.Copy()
	}
	return out
}

func cloneShards(in []CompiledShard) []CompiledShard {
	if in == nil {
		return nil
	}
	out := make([]CompiledShard, len(in))
	for i, shard := range in {
		shard.Patch = cloneSointuPatch(shard.Patch)
		shard.Instruments = append([]ShardInstrument(nil), shard.Instruments...)
		for j := range shard.Instruments {
			shard.Instruments[j].HardReleaseOperands = append([]int(nil), shard.Instruments[j].HardReleaseOperands...)
		}
		shard.Bindings = append([]ControlBinding(nil), shard.Bindings...)
		out[i] = shard
	}
	return out
}
