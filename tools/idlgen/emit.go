package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// goTypeFor renders the Go type for a resolved IDL type.
//
// The u128 mapping is deliberate: a 128-bit field becomes num.U128, the value
// type this SDK already uses, rather than a byte blob or a big.Int. It keeps
// account structs comparable and keeps one numeric type across the SDK.
func (g *typeGraph) goTypeFor(t *typeRef) (string, error) {
	switch t.kind {
	case "prim":
		switch t.prim {
		case "pubkey":
			return "solana.PublicKey", nil
		case "u128", "i128":
			return "num.U128", nil
		case "bool":
			return "bool", nil
		default:
			// The IDL spells scalars the way Rust does (u64, i32). Go spells them
			// uint64, int32. There is no u64 or i32 in Go, so the mapping is
			// explicit rather than a pass-through: a pass-through emits code that
			// cannot compile, and the mistake is invisible until build time.
			if goPrim, ok := goPrimName[t.prim]; ok {
				return goPrim, nil
			}
			return "", fmt.Errorf("no Go mapping for IDL primitive %q", t.prim)
		}
	case "defined":
		info, ok := g.types[t.name]
		if !ok {
			return "", fmt.Errorf("unknown type %q", t.name)
		}
		if info.isEnum {
			return goTypeName(t.name), nil
		}
		return goTypeName(t.name), nil
	case "array":
		elem, err := g.goTypeFor(t.elem)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("[%d]%s", t.length, elem), nil
	case "option":
		elem, err := g.goTypeFor(t.elem)
		if err != nil {
			return "", err
		}
		return "Option[" + elem + "]", nil
	case "vec":
		elem, err := g.goTypeFor(t.elem)
		if err != nil {
			return "", err
		}
		return "[]" + elem, nil
	default:
		return "", fmt.Errorf("unhandled ref kind %q", t.kind)
	}
}

// generate writes the Go files for the given output directory and returns the
// paths written.
func (g *typeGraph) generate(outDir, idlPath string) ([]string, error) {
	var written []string

	var buf bytes.Buffer
	g.writeHeader(&buf, idlPath, impBinary, impFmt, impBin, impSolana, impNum)
	g.writeTypeDecls(&buf)
	for i := range g.order {
		name := g.order[i]
		info := g.types[name]
		if info.isEnum {
			continue
		}
		g.emitDecode(&buf, name, info)
	}
	path := filepath.Join(outDir, "types_gen.go")
	if err := writeGoFile(path, buf.Bytes()); err != nil {
		return nil, err
	}
	written = append(written, path)

	buf.Reset()
	g.writeHeader(&buf, idlPath, impFmt, impBin)
	g.writeEnums(&buf)
	path = filepath.Join(outDir, "enums_gen.go")
	if err := writeGoFile(path, buf.Bytes()); err != nil {
		return nil, err
	}
	written = append(written, path)

	// Option is its own file because both the type and enum files reference it,
	// and Go has no cross-file private types.
	buf.Reset()
	g.writeHeader(&buf, idlPath)
	g.writeOption(&buf)
	path = filepath.Join(outDir, "option_gen.go")
	if err := writeGoFile(path, buf.Bytes()); err != nil {
		return nil, err
	}
	written = append(written, path)

	// Decoders for the twelve accounts. Only accounts get a decoder: every one of
	// them is fixed-size and has no vec or option in its payload, whereas the
	// instruction and event types are variable-length and are handled with the
	// instructions rather than here.
	buf.Reset()
	g.writeHeader(&buf, idlPath, impFmt, impSlices, impBin)
	if err := g.writeAccountDecoders(&buf); err != nil {
		return nil, err
	}
	path = filepath.Join(outDir, "decode_gen.go")
	if err := writeGoFile(path, buf.Bytes()); err != nil {
		return nil, err
	}
	written = append(written, path)

	return written, nil
}

// writeAccountDecoders emits DecodeX helpers for the twelve IDL accounts.
//
// DecodeX takes the full account data, checks the discriminator, then reads the
// payload. Requiring the discriminator is the point: without it a decoder would
// happily interpret any account's bytes as pool state.
func (g *typeGraph) writeAccountDecoders(buf *bytes.Buffer) error {
	for _, acc := range g.idl.Accounts {
		info, ok := g.types[acc.Name]
		if !ok {
			return fmt.Errorf("account %s has no matching type", acc.Name)
		}
		if info.isEnum {
			return fmt.Errorf("account %s is an enum, which cannot be an account", acc.Name)
		}
		if info.size <= 0 {
			return fmt.Errorf("account %s is not fixed-size; an account decoder must be static", acc.Name)
		}

		goName := goTypeName(acc.Name)

		// DummyZcAccount is zero-copy: it carries no Anchor discriminator, so a
		// length check is its only prefix validation.
		zeroCopy := acc.Name == "DummyZcAccount"

		fmt.Fprintf(buf, "// Decode%s reads a %s account from raw account data.\n", goName, acc.Name)
		if zeroCopy {
			fmt.Fprintf(buf, "// %s is a zero-copy account and carries no discriminator, so only its\n", acc.Name)
			fmt.Fprintf(buf, "// length is validated.\n")
		} else {
			fmt.Fprintf(buf, "// The data must begin with the %s discriminator.\n", acc.Name)
		}
		fmt.Fprintf(buf, "func Decode%s(data []byte) (*%s, error) {\n", goName, goName)

		prefix := 0
		if !zeroCopy {
			prefix = 8
			fmt.Fprintf(buf, "\tif len(data) < AccountDiscriminatorLen {\n")
			fmt.Fprintf(buf, "\t\treturn nil, fmt.Errorf(\"lbclmm: %%s needs at least %%d bytes, got %%d\", %q, AccountDiscriminatorLen, len(data))\n", acc.Name)
			fmt.Fprintf(buf, "\t}\n")
			fmt.Fprintf(buf, "\tif !slices.Equal(data[:AccountDiscriminatorLen], Discriminator%s[:]) {\n", goName)
			fmt.Fprintf(buf, "\t\treturn nil, fmt.Errorf(\"lbclmm: %%s discriminator mismatch\", %q)\n", acc.Name)
			fmt.Fprintf(buf, "\t}\n")
		}

		fmt.Fprintf(buf, "\twant := %d\n", prefix+info.size)
		fmt.Fprintf(buf, "\tif len(data) < want {\n")
		fmt.Fprintf(buf, "\t\treturn nil, fmt.Errorf(\"lbclmm: %%s needs %%d bytes, got %%d\", %q, want, len(data))\n", acc.Name)
		fmt.Fprintf(buf, "\t}\n")

		fmt.Fprintf(buf, "\tvar out %s\n", goName)
		fmt.Fprintf(buf, "\tdec := bin.NewBorshDecoder(data[%d:])\n", prefix)
		fmt.Fprintf(buf, "\tif err := out.UnmarshalWithDecoder(dec); err != nil {\n")
		fmt.Fprintf(buf, "\t\treturn nil, fmt.Errorf(\"lbclmm: decoding %%s: %%w\", %q, err)\n", acc.Name)
		fmt.Fprintf(buf, "\t}\n")
		fmt.Fprintf(buf, "\treturn &out, nil\n")
		fmt.Fprintf(buf, "}\n\n")
	}

	return nil
}

// importSpec is one generated-file import. Each file declares exactly the
// imports it uses: a shared superset would leave unused imports in three of the
// four files, and Go treats an unused import as an error.
type importSpec struct {
	path  string
	alias string
}

var (
	impBinary = importSpec{path: "encoding/binary"}
	impErrors = importSpec{path: "errors"}
	impFmt    = importSpec{path: "fmt"}
	impSlices = importSpec{path: "slices"}
	impBin    = importSpec{path: "github.com/gagliardetto/binary", alias: "bin"}
	impSolana = importSpec{path: "github.com/gagliardetto/solana-go", alias: "solana"}
	impNum    = importSpec{path: "github.com/pwnholic/dlmm-go/num"}
)

// writeHeader emits the generated-file banner, the package clause, and exactly
// the imports the file needs.
func (g *typeGraph) writeHeader(buf *bytes.Buffer, idlPath string, imports ...importSpec) {
	fmt.Fprintf(buf, "// Code generated by tools/idlgen from %s. DO NOT EDIT.\n\n", idlPath)
	fmt.Fprintf(buf, "package lbclmm\n\n")

	if len(imports) == 0 {
		return
	}

	// Group stdlib before external, as gofmt would.
	std, ext := make([]importSpec, 0, len(imports)), make([]importSpec, 0, len(imports))
	for _, im := range imports {
		if strings.Contains(im.path, ".") && !strings.HasPrefix(im.path, "golang.org/") {
			ext = append(ext, im)
			continue
		}
		std = append(std, im)
	}

	fmt.Fprintf(buf, "import (\n")
	writeImports(buf, std)
	if len(std) > 0 && len(ext) > 0 {
		fmt.Fprintf(buf, "\n")
	}
	writeImports(buf, ext)
	fmt.Fprintf(buf, ")\n\n")
}

func writeImports(buf *bytes.Buffer, specs []importSpec) {
	for _, im := range specs {
		if im.alias != "" {
			fmt.Fprintf(buf, "\t%s %q\n", im.alias, im.path)
			continue
		}
		fmt.Fprintf(buf, "\t%q\n", im.path)
	}
}

func (g *typeGraph) writeTypeDecls(buf *bytes.Buffer) {
	names := append([]string{}, g.order...)
	sort.Strings(names)

	for _, name := range names {
		info := g.types[name]
		if info.isEnum {
			continue
		}
		emitDoc(buf, fmt.Sprintf("%s mirrors the IDL type %s.", goTypeName(name), name), "")

		// An account struct carries its discriminator so a decoder can refuse
		// data that belongs to a different program.
		if acc := g.accountFor(name); acc != nil {
			writeDiscriminatorConst(buf, acc)
		}

		fmt.Fprintf(buf, "type %s struct {\n", goTypeName(name))
		for _, f := range info.fields {
			gt, err := g.goTypeFor(f.typ)
			if err != nil {
				// Cannot happen: sizes resolved, so the mapping resolves.
				continue
			}
			// No struct tag is emitted. A Borsh struct is positional, so a tag
			// carries no information, and an empty tag literal is actively
			// harmful: Go reads the text before the first backquote as the field
			// name, which makes the declaration unparseable.
			fmt.Fprintf(buf, "\t%s %s\n", goIdent(f.name), gt)
		}
		fmt.Fprintf(buf, "}\n\n")
	}
}

// enumPrefix is the constant-name prefix for an enum's variants, so the
// constants are qualified by their type at every call site.
// goPrimName maps IDL/Rust scalar spellings onto Go's. Go has uint8 and int32;
// it has no u8 and no i32, so every scalar needs a real mapping.
var goPrimName = map[string]string{
	"u8":   "uint8",
	"u16":  "uint16",
	"u32":  "uint32",
	"u64":  "uint64",
	"i8":   "int8",
	"i16":  "int16",
	"i32":  "int32",
	"i64":  "int64",
	"f32":  "float32",
	"f64":  "float64",
	"bool": "bool",
}

func enumPrefix(enumName string) string { return goTypeName(enumName) }

func (g *typeGraph) writeEnums(buf *bytes.Buffer) {
	names := append([]string{}, g.order...)
	sort.Strings(names)

	for _, name := range names {
		info := g.types[name]
		if !info.isEnum {
			continue
		}
		emitDoc(buf, fmt.Sprintf("%s mirrors the IDL enum %s. The zero value is an explicit unknown sentinel so a zero-valued field cannot masquerade as the first real variant.", goTypeName(name), name), "")

		fmt.Fprintf(buf, "type %s uint8\n\n", goTypeName(name))
		fmt.Fprintf(buf, "const (\n")
		fmt.Fprintf(buf, "\t%sInvalid %s = iota\n", enumPrefix(name), goTypeName(name))
		for i, v := range info.variants {
			// The constant is prefixed with the enum type so a variant name is
			// never ambiguous at a call site: StrategyTypeSpotOneSide, not
			// SpotOneSide. Values are the IDL declaration index, not an
			// alphabetical or bitmask value.
			constName := enumPrefix(name) + v.Name
			fmt.Fprintf(buf, "\t%s %s = %d", constName, goTypeName(name), i)
			if len(v.Fields) > 0 {
				// A data-carrying variant is wider on the wire: one extra byte
				// after the discriminant.
				fmt.Fprintf(buf, " // carries a %s payload; encoded width differs per variant",
					"one-byte")
			}
			fmt.Fprintf(buf, "\n")
		}
		fmt.Fprintf(buf, ")\n\n")

		// A decoder so an enum used as a struct field reads like any other type.
		// The discriminant is one byte whose value is the IDL declaration index.
		// An out-of-range value is rejected rather than accepted as zero: an
		// unrecognised variant means the decoder does not match the program, and
		// silently decoding it as the first variant would misreport pool state.
		fmt.Fprintf(buf, "// UnmarshalWithDecoder reads the one-byte discriminant.\n")
		fmt.Fprintf(buf, "func (e *%s) UnmarshalWithDecoder(dec *bin.Decoder) error {\n", goTypeName(name))
		fmt.Fprintf(buf, "\tif e == nil {\n")
		fmt.Fprintf(buf, "\t\treturn fmt.Errorf(\"lbclmm: cannot decode %s into a nil receiver\")\n", goTypeName(name))
		fmt.Fprintf(buf, "\t}\n")
		fmt.Fprintf(buf, "\tv, err := dec.ReadUint8()\n")
		fmt.Fprintf(buf, "\tif err != nil {\n")
		fmt.Fprintf(buf, "\t\treturn fmt.Errorf(\"reading %s: %%w\", err)\n", name)
		fmt.Fprintf(buf, "\t}\n")
		fmt.Fprintf(buf, "\tif v >= %d {\n", len(info.variants))
		fmt.Fprintf(buf, "\t\treturn fmt.Errorf(\"lbclmm: %s has %%d variants, got %%d\", %d, v)\n", name, len(info.variants))
		fmt.Fprintf(buf, "\t}\n")
		fmt.Fprintf(buf, "\t*e = %s(v)\n", goTypeName(name))
		fmt.Fprintf(buf, "\treturn nil\n")
		fmt.Fprintf(buf, "}\n\n")
	}
}

func (g *typeGraph) accountFor(typeName string) *idlAccount {
	for i := range g.idl.Accounts {
		if g.idl.Accounts[i].Name == typeName {
			return &g.idl.Accounts[i]
		}
	}
	return nil
}

func writeDiscriminatorConst(buf *bytes.Buffer, acc *idlAccount) {
	fmt.Fprintf(buf, "// Discriminator%s is the 8-byte account discriminator for %s.\n",
		goIdent(acc.Name), acc.Name)
	fmt.Fprintf(buf, "var Discriminator%s = [8]byte{", goIdent(acc.Name))
	for i, b := range acc.Discriminator {
		if i > 0 {
			fmt.Fprintf(buf, ", ")
		}
		fmt.Fprintf(buf, "%d", b)
	}
	fmt.Fprintf(buf, "}\n\n")
}

func (g *typeGraph) writeOption(buf *bytes.Buffer) {
	emitDoc(buf, "Option is a Borsh optional: a one-byte discriminant followed by the payload when present. It is generic so it can wrap a defined struct as well as a primitive.", "")
	fmt.Fprintf(buf, "type Option[T any] struct {\n\tSome bool\n\tValue T\n}\n\n")

	fmt.Fprintf(buf, "// OptionSize reports the encoded size of an Option with a payload of the given width.\n")
	fmt.Fprintf(buf, "func OptionSize(payload int) int { return 1 + payload }\n\n")

	fmt.Fprintf(buf, "func SomeOption[T any](v T) Option[T] { return Option[T]{Some: true, Value: v} }\n\n")
	fmt.Fprintf(buf, "func NoneOption[T any]() Option[T] { var z T; return Option[T]{Some: false, Value: z} }\n\n")
}

func emitDoc(buf *bytes.Buffer, text, tag string) {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		fmt.Fprintf(buf, "// %s\n", strings.TrimSpace(line))
	}
	if tag != "" {
		fmt.Fprintf(buf, "// `%s`\n", tag)
	}
	fmt.Fprintf(buf, "\n")
}

func writeGoFile(path string, src []byte) error {
	formatted, err := format.Source(src)
	if err != nil {
		// Write the unformatted source so the syntax error can be read, then fail.
		_ = os.WriteFile(path+".broken", src, 0o644)
		return fmt.Errorf("formatting %s: %w (unformatted copy at %s.broken)", path, err, path)
	}
	return os.WriteFile(path, formatted, 0o644)
}
