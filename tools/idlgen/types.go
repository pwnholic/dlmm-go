package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// typeRef is a resolved IDL type. The legacy shank encoding used by this IDL is
// positional and must be read exactly: {"array": [elem, len]} has the element
// first, the primitive names are bare strings, and the pubkey type is "pubkey"
// (never "publicKey"). Every one of those is a place a naive parser silently
// emits nothing.
type typeRef struct {
	kind   string // "prim" | "defined" | "array" | "vec" | "option" | "enum"
	prim   string
	name   string // defined type / enum name
	elem   *typeRef
	length int
	// variant payload for a data-carrying enum, used only by AccountsType
	payload *typeRef
}

var primSize = map[string]int{
	"u8": 1, "i8": 1, "bool": 1,
	"u16": 2, "i16": 2,
	"u32": 4, "i32": 4, "f32": 4,
	"u64": 8, "i64": 8, "f64": 8,
	"u128": 16, "i128": 16,
	"pubkey": 32,
}

func parseTypeRef(raw json.RawMessage) (*typeRef, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if _, ok := primSize[s]; !ok {
			return nil, fmt.Errorf("unknown primitive type %q", s)
		}
		return &typeRef{kind: "prim", prim: s}, nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("unrecognised type %s: %w", raw, err)
	}

	if v, ok := obj["array"]; ok {
		// Positional: [element, length].
		var pair []json.RawMessage
		if err := json.Unmarshal(v, &pair); err != nil || len(pair) != 2 {
			return nil, fmt.Errorf("array must be [element, length], got %s", v)
		}
		elem, err := parseTypeRef(pair[0])
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(string(pair[1]))
		if err != nil {
			// The length is occasionally a bare number rather than a string.
			var anyLen int
			if json.Unmarshal(pair[1], &anyLen) != nil {
				return nil, fmt.Errorf("array length %s is not an integer", pair[1])
			}
			n = anyLen
		}
		return &typeRef{kind: "array", elem: elem, length: n}, nil
	}

	if v, ok := obj["defined"]; ok {
		var d struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(v, &d); err != nil {
			return nil, fmt.Errorf("defined: %w", err)
		}
		return &typeRef{kind: "defined", name: d.Name}, nil
	}

	if v, ok := obj["vec"]; ok {
		elem, err := parseTypeRef(v)
		if err != nil {
			return nil, err
		}
		return &typeRef{kind: "vec", elem: elem}, nil
	}

	if v, ok := obj["option"]; ok {
		elem, err := parseTypeRef(v)
		if err != nil {
			return nil, err
		}
		return &typeRef{kind: "option", elem: elem}, nil
	}

	if v, ok := obj["coption"]; ok {
		elem, err := parseTypeRef(v)
		if err != nil {
			return nil, err
		}
		return &typeRef{kind: "option", elem: elem}, nil
	}

	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return nil, fmt.Errorf("unhandled type construct %v", keys)
}

// typeGraph owns every IDL type with its resolved layout and Borsh size.
type typeGraph struct {
	idl   *idl
	types map[string]*typeInfo
	order []string
}

type typeInfo struct {
	def     idlType
	fields  []resolvedField
	isEnum  bool
	variants []idlEnumCase
	// size is the Borsh payload size; -1 when the type is variable-length.
	size int
}

type resolvedField struct {
	name string
	typ  *typeRef
}

func newTypeGraph(doc *idl) (*typeGraph, error) {
	g := &typeGraph{idl: doc, types: map[string]*typeInfo{}}

	for _, t := range doc.Types {
		info := &typeInfo{def: t, size: -1}
		if t.Type.Kind == "enum" {
			info.isEnum = true
			info.variants = t.Type.Variants
		} else if t.Type.Kind == "struct" {
			for _, f := range t.Type.Fields {
				rt, err := parseTypeRef(f.Type)
				if err != nil {
					return nil, fmt.Errorf("type %s field %s: %w", t.Name, f.Name, err)
				}
				info.fields = append(info.fields, resolvedField{name: f.Name, typ: rt})
			}
		} else if t.Type.Kind != "" {
			return nil, fmt.Errorf("type %s: unsupported kind %q", t.Name, t.Type.Kind)
		}
		g.types[t.Name] = info
		g.order = append(g.order, t.Name)
	}

	// Sizes are resolved lazily with memoisation. The hazard survey found no
	// recursion in the defined graph, so a memo is enough and no
	// forward-declaration machinery is needed; a cycle would surface as an
	// unresolved size here rather than as a silent wrong number.
	for _, name := range g.order {
		if _, err := g.sizeOf(name, 0); err != nil {
			return nil, err
		}
	}

	return g, nil
}

func (g *typeGraph) sizeOf(name string, depth int) (int, error) {
	if depth > 16 {
		return 0, fmt.Errorf("type %s: nesting too deep, probable recursion", name)
	}

	info, ok := g.types[name]
	if !ok {
		return 0, fmt.Errorf("unknown type %q", name)
	}
	if info.size >= 0 {
		return info.size, nil
	}

	if info.isEnum {
		// A unit enum is one discriminant byte. A data-carrying enum is
		// variable, which is why only AccountsType can grow.
		if info.hasPayload() {
			info.size = -1
			return -1, nil
		}
		info.size = 1
		return 1, nil
	}

	total := 0
	for _, f := range info.fields {
		fs, err := g.refSize(f.typ, depth+1)
		if err != nil {
			return 0, fmt.Errorf("type %s field %s: %w", name, f.name, err)
		}
		if fs < 0 {
			info.size = -1
			return -1, nil
		}
		total += fs
	}

	info.size = total
	return total, nil
}

func (t *typeInfo) hasPayload() bool {
	for _, v := range t.variants {
		if len(v.Fields) > 0 {
			return true
		}
	}
	return false
}

func (g *typeGraph) refSize(t *typeRef, depth int) (int, error) {
	switch t.kind {
	case "prim":
		return primSize[t.prim], nil
	case "defined":
		return g.sizeOf(t.name, depth)
	case "array":
		es, err := g.refSize(t.elem, depth+1)
		if err != nil {
			return 0, err
		}
		if es < 0 {
			return -1, nil
		}
		return es * t.length, nil
	case "option":
		es, err := g.refSize(t.elem, depth+1)
		if err != nil {
			return 0, err
		}
		if es < 0 {
			return -1, nil
		}
		// One discriminant byte plus the payload when present.
		return 1 + es, nil
	case "vec":
		// Four-byte length prefix plus elements, so a vec makes its container
		// variable-length.
		return -1, nil
	default:
		return 0, fmt.Errorf("unhandled ref kind %q", t.kind)
	}
}

// knownSizes are the sizes the real on-chain fixtures prove, payload only.
// LbPair is 904 bytes on chain and BinArray is 10136, both including the
// 8-byte account discriminator, so the payload sizes are 896 and 10128.
var knownSizes = map[string]int{
	"LbPair":  896,
	"BinArray": 10128,
	"Oracle": 24,
}

func (g *typeGraph) assertKnownSizes() error {
	for name, want := range knownSizes {
		got, err := g.sizeOf(name, 0)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("size model wrong for %s: computed %d, fixtures prove %d", name, got, want)
		}
	}
	return nil
}
