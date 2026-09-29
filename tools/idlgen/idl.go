package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// idl is the subset of the Anchor IDL the generator needs. Field types in the
// legacy shank encoding are bare JSON strings for primitives and single-key
// objects for everything else, which is why Type is json.RawMessage rather than
// a struct.
type idl struct {
	Version  string          `json:"version"`
	Address  string          `json:"address"`
	Metadata struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"metadata"`
	Accounts []idlAccount `json:"accounts"`
	Types    []idlType    `json:"types"`
	Errors   []idlError   `json:"errors"`
}

type idlAccount struct {
	Name          string `json:"name"`
	Discriminator []int  `json:"discriminator"`
}

type idlError struct {
	Code uint32 `json:"code"`
	Name string `json:"name"`
	Msg  string `json:"msg"`
}

type idlType struct {
	Name string `json:"name"`
	Type struct {
		Kind     string          `json:"kind"`
		Fields   []idlField      `json:"fields"`
		Variants []idlEnumCase   `json:"variants"`
		Raw      json.RawMessage `json:"-"`
	} `json:"type"`
}

type idlField struct {
	Name string          `json:"name"`
	Type json.RawMessage `json:"type"`
}

// idlEnumCase is an enum variant. Fields is non-empty for data-carrying
// variants; per the hazard survey only AccountsType.TransferHookMultiReward has
// one, which makes that enum variable-width on the wire.
type idlEnumCase struct {
	Name   string            `json:"name"`
	Fields []json.RawMessage `json:"fields"`
}

func loadIDL(path string) (*idl, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var doc idl
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing IDL: %w", err)
	}

	if len(doc.Types) == 0 {
		return nil, fmt.Errorf("IDL has no types")
	}
	if len(doc.Accounts) == 0 {
		return nil, fmt.Errorf("IDL has no accounts")
	}

	return &doc, nil
}

// goIdent converts a snake_case IDL name to MixedCaps.
//
// It strips the leading underscore that padding and dummy fields use, so
// `_padding_1` becomes Padding1 rather than a Go identifier starting with _.
// The dummy enum fields on DummyIx depend on this: `_pair_type` must not
// collide with the PairType type.
func goIdent(name string) string {
	name = strings.TrimLeft(name, "_")
	if name == "" {
		return "Blank"
	}

	parts := strings.Split(name, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}

	out := strings.Join(parts, "")
	if out == "" {
		return "Blank"
	}
	if isGoKeyword(out) {
		return out + "Field"
	}
	return out
}

// goTypeName renders a type name for the Go type namespace. IDL names are
// already PascalCase for types; this only guards against a name that collides
// with a Go keyword or that starts with a digit.
func goTypeName(name string) string {
	name = strings.TrimLeft(name, "_")
	if name == "" {
		return "Blank"
	}
	if isGoKeyword(name) {
		return name + "Type"
	}
	return name
}

var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

func isGoKeyword(s string) bool { return goKeywords[s] }
