package main

import (
	"bytes"
	"fmt"
)

// emitDecode writes an UnmarshalWithDecoder method for a struct.
//
// The decode is emitted field by field, in declaration order, rather than driven
// by reflection. Borsh is positional, so field order is the format; making the
// order explicit in the generated source means it can be read and compared
// against the IDL by eye, and a mismatch shows up as a diff rather than as a
// reflection surprise.
func (g *typeGraph) emitDecode(buf *bytes.Buffer, name string, info *typeInfo) {
	recv := goTypeName(name)

	fmt.Fprintf(buf, "// UnmarshalWithDecoder reads %s in Borsh form.\n", recv)
	fmt.Fprintf(buf, "// It reads the account payload, without the 8-byte discriminator: callers\n")
	fmt.Fprintf(buf, "// check the discriminator before calling it, because a decoder that silently\n")
	fmt.Fprintf(buf, "// accepted another program's account would read unrelated bytes as state.\n")
	fmt.Fprintf(buf, "func (m *%s) UnmarshalWithDecoder(dec *bin.Decoder) error {\n", recv)
	fmt.Fprintf(buf, "\tif m == nil {\n")
	fmt.Fprintf(buf, "\t\treturn fmt.Errorf(\"lbclmm: cannot decode %s into a nil receiver\")\n", recv)
	fmt.Fprintf(buf, "\t}\n")

	for _, f := range info.fields {
		if err := g.emitFieldRead(buf, "m."+goIdent(f.name), f.typ, 1); err != nil {
			// A field the emitter cannot express is a generator bug, not a
			// runtime one; surface it as a comment so it cannot be missed.
			fmt.Fprintf(buf, "\t// TODO(idlgen): field %s of type kind %s is not handled\n", f.name, f.typ.kind)
		}
	}

	fmt.Fprintf(buf, "\treturn nil\n")
	fmt.Fprintf(buf, "}\n\n")

	// A size constant makes the wire size checkable at the call site.
	if info.size > 0 {
		fmt.Fprintf(buf, "// PayloadSize%s is the Borsh payload size of %s, excluding the\n", recv, recv)
		fmt.Fprintf(buf, "// %d-byte account discriminator.\n", 8)
		fmt.Fprintf(buf, "const PayloadSize%s = %d\n\n", recv, info.size)
	}
}

// emitFieldRead writes the statements that read one field into target.
func (g *typeGraph) emitFieldRead(buf *bytes.Buffer, target string, t *typeRef, depth int) error {
	ind := bytes.Repeat([]byte("\t"), depth)
	pad := string(ind)

	switch t.kind {
	case "prim":
		switch t.prim {
		case "pubkey":
			// Wrapped in a block so two consecutive pubkey fields do not both
			// declare b and err in the same scope.
			fmt.Fprintf(buf, "%s{\n", pad)
			fmt.Fprintf(buf, "%s\tb, err := dec.ReadNBytes(32)\n", pad)
			fmt.Fprintf(buf, "%s\tif err != nil {\n%s\t\treturn fmt.Errorf(\"reading pubkey: %%w\", err)\n%s\t}\n", pad, pad, pad)
			fmt.Fprintf(buf, "%s\t%s = solana.PublicKeyFromBytes(b)\n", pad, target)
			fmt.Fprintf(buf, "%s}\n", pad)
			return nil
		case "u128":
			fmt.Fprintf(buf, "%s{\n", pad)
			fmt.Fprintf(buf, "%s\tv, err := dec.ReadUint128(binary.LittleEndian)\n", pad)
			fmt.Fprintf(buf, "%s\tif err != nil {\n%s\t\treturn fmt.Errorf(\"reading u128: %%w\", err)\n%s\t}\n", pad, pad, pad)
			fmt.Fprintf(buf, "%s\t%s = num.U128{Lo: v.Lo, Hi: v.Hi}\n", pad, target)
			fmt.Fprintf(buf, "%s}\n", pad)
			return nil
		case "i128":
			// The IDL has no i128, so this is unreachable today; fail loudly
			// rather than writing a sign-wrong decode.
			return fmt.Errorf("i128 is not supported")
		case "bool":
			fmt.Fprintf(buf, "%s{\n", pad)
			fmt.Fprintf(buf, "%s\tv, err := dec.ReadBool()\n", pad)
			fmt.Fprintf(buf, "%s\tif err != nil {\n%s\t\treturn fmt.Errorf(\"reading bool: %%w\", err)\n%s\t}\n", pad, pad, pad)
			fmt.Fprintf(buf, "%s\t%s = v\n", pad, target)
			fmt.Fprintf(buf, "%s}\n", pad)
			return nil
		default:
			goT, ok := goPrimName[t.prim]
			if !ok {
				return fmt.Errorf("no reader for primitive %q", t.prim)
			}
			// bin.Decoder differs by width: the multi-byte readers take a byte
			// order, the single-byte readers do not. Passing one to ReadUint8 is a
			// compile error, so the emitter has to distinguish them.
			call := fmt.Sprintf("dec.Read%s(binary.LittleEndian)", bitsOf(goT))
			if isSingleByte(goT) {
				call = fmt.Sprintf("dec.Read%s()", bitsOf(goT))
			}
			fmt.Fprintf(buf, "%s{\n", pad)
			fmt.Fprintf(buf, "%s\tv, err := %s\n", pad, call)
			fmt.Fprintf(buf, "%s\tif err != nil {\n%s\t\treturn fmt.Errorf(\"reading %s: %%w\", err)\n%s\t}\n", pad, pad, t.prim, pad)
			fmt.Fprintf(buf, "%s\t%s = %s(v)\n", pad, target, goT)
			fmt.Fprintf(buf, "%s}\n", pad)
			return nil
		}

	case "defined":
		// A user-defined type decodes into its own method. Because it is declared
		// at package scope with its own receiver, the `v` local of the previous
		// field cannot collide here.
		fmt.Fprintf(buf, "%sif err := %s.UnmarshalWithDecoder(dec); err != nil {\n", pad, target)
		fmt.Fprintf(buf, "%s\treturn err\n%s}\n", pad, pad)
		return nil

	case "array":
		fmt.Fprintf(buf, "%sfor i := range %s {\n", pad, target)
		if err := g.emitFieldRead(buf, target+"[i]", t.elem, depth+1); err != nil {
			return err
		}
		fmt.Fprintf(buf, "%s}\n", pad)
		return nil

	case "option":
		// Borsh encodes an option as one discriminant byte. A definition-site
		// option in an account is unusual but present, so it is emitted rather
		// than rejected.
		fmt.Fprintf(buf, "%s{\n", pad)
		fmt.Fprintf(buf, "%s\tpresent, err := dec.ReadOption()\n", pad)
		fmt.Fprintf(buf, "%s\tif err != nil {\n%s\t\treturn fmt.Errorf(\"reading option tag: %%w\", err)\n%s\t}\n", pad, pad, pad)
		fmt.Fprintf(buf, "%s\t%s.Some = present\n", pad, target)
		fmt.Fprintf(buf, "%s}\n", pad)
		return nil

	case "vec":
		// Accounts use fixed arrays throughout, so a vec in an account means the
		// IDL changed shape and this decoder is no longer static.
		return fmt.Errorf("vec in an account layout")

	default:
		return fmt.Errorf("unhandled kind %q", t.kind)
	}
}

// isSingleByte reports whether a Go scalar needs the no-argument reader form.
func isSingleByte(goType string) bool {
	return goType == "uint8" || goType == "int8"
}

// bitsOf maps a Go scalar type to the ReadXxx method suffix on bin.Decoder.
func bitsOf(goType string) string {
	switch goType {
	case "uint8":
		return "Uint8"
	case "uint16":
		return "Uint16"
	case "uint32":
		return "Uint32"
	case "uint64":
		return "Uint64"
	case "int8":
		return "Int8"
	case "int16":
		return "Int16"
	case "int32":
		return "Int32"
	case "int64":
		return "Int64"
	default:
		return ""
	}
}
