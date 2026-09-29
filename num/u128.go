// Package num provides fixed-width unsigned integer value types used across the SDK.
//
// Types in this package are pure values: comparable, safe to use as map keys, and
// with a useful zero value. Arithmetic lives in package math, not here.
//
// These types exist because github.com/gagliardetto/binary.Uint128 is a serialization
// carrier, not an arithmetic type: it has no arithmetic methods, and it carries an
// Endianness field that makes == compare byte order rather than value.
//
// See DESIGN.md and the note in ARCHITECTURE.md section 3.1 for the full rationale.
package num

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	bin "github.com/gagliardetto/binary"
)

// U128 is an unsigned 128-bit integer stored as two 64-bit limbs, low first.
// The zero value is a valid zero.
type U128 struct {
	Lo uint64
	Hi uint64
}

// maxU64 is 2^64-1, the saturated value of a single limb.
const maxU64 = ^uint64(0)

// NewU128 builds a U128 from high and low limbs.
func NewU128(hi, lo uint64) U128 { return U128{Lo: lo, Hi: hi} }

// U128FromU64 widens a uint64 into a U128.
func U128FromU64(v uint64) U128 { return U128{Lo: v} }

// MaxU128 is the largest representable U128 (2^128-1).
var MaxU128 = U128{Lo: maxU64, Hi: maxU64}

// IsZero reports whether u is zero.
func (u U128) IsZero() bool { return u.Lo == 0 && u.Hi == 0 }

// IsU64 reports whether u fits in a uint64 without loss.
func (u U128) IsU64() bool { return u.Hi == 0 }

// Cmp compares u and o, returning -1, 0, or +1.
func (u U128) Cmp(o U128) int {
	switch {
	case u.Hi != o.Hi:
		if u.Hi < o.Hi {
			return -1
		}

		return +1
	case u.Lo != o.Lo:
		if u.Lo < o.Lo {
			return -1
		}

		return +1
	default:
		return 0
	}
}

// Uint64 returns the low 64 bits. It truncates silently; guard with IsU64 when
// the value may not fit.
func (u U128) Uint64() uint64 { return u.Lo }

// BigInt returns u as a *big.Int. It allocates, so it is not suitable for hot
// paths; use it at boundaries and in tests.
func (u U128) BigInt() *big.Int {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint64(b[0:8], u.Lo)
	binary.LittleEndian.PutUint64(b[8:16], u.Hi)
	// Reverse to big-endian for big.Int.SetBytes.
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}

	return new(big.Int).SetBytes(b)
}

// U128FromBigInt converts v into a U128.
//
// It returns an error when v is negative or does not fit in 128 bits, rather
// than truncating: silent wraparound in on-chain amounts is not recoverable.
func U128FromBigInt(v *big.Int) (U128, error) {
	if v == nil {
		return U128{}, errors.New("num: nil big.int")
	}

	if v.Sign() < 0 {
		return U128{}, fmt.Errorf("num: negative value %s does not fit in u128", v)
	}

	if v.BitLen() > 128 {
		return U128{}, fmt.Errorf("num: value %s exceeds 128 bits", v)
	}

	b := v.Bytes() // big-endian, minimal length

	var full [16]byte
	copy(full[16-len(b):], b)

	return U128{
		Hi: binary.BigEndian.Uint64(full[0:8]),
		Lo: binary.BigEndian.Uint64(full[8:16]),
	}, nil
}

// BytesLE returns the 16-byte little-endian encoding used by Borsh.
func (u U128) BytesLE() [16]byte {
	var b [16]byte
	binary.LittleEndian.PutUint64(b[0:8], u.Lo)
	binary.LittleEndian.PutUint64(b[8:16], u.Hi)

	return b
}

// U128FromBytesLE decodes a 16-byte little-endian value.
func U128FromBytesLE(b [16]byte) U128 {
	return U128{
		Lo: binary.LittleEndian.Uint64(b[0:8]),
		Hi: binary.LittleEndian.Uint64(b[8:16]),
	}
}

// String renders u in decimal.
func (u U128) String() string { return u.BigInt().String() }

// UnmarshalWithDecoder reads a little-endian u128.
//
// The encoding is fixed by Borsh and by the on-chain program, so the byte order
// is not configurable.
func (u *U128) UnmarshalWithDecoder(dec *bin.Decoder) error {
	if u == nil {
		return errors.New("num: cannot unmarshal into nil u128")
	}

	le, err := dec.ReadUint128(binary.LittleEndian)
	if err != nil {
		return fmt.Errorf("num: reading u128: %w", err)
	}

	*u = U128{Lo: le.Lo, Hi: le.Hi}

	return nil
}

// MarshalWithEncoder writes a 16-byte little-endian u128.
//
// The encoding is written explicitly rather than left to the codec's
// reflection fallback: the fallback is byte-identical for today's two-uint64
// layout, but it would silently follow any future field change. The wire
// format is pinned by TestWireFormatIsExactly16BytesLittleEndian.
//
// The byte order is passed explicitly rather than read from
// bin.Uint128.Endianness, because ReadUint128 leaves that field nil.
func (u U128) MarshalWithEncoder(enc *bin.Encoder) error {
	src := bin.Uint128{Lo: u.Lo, Hi: u.Hi}
	if err := enc.WriteUint128(src, binary.LittleEndian); err != nil {
		return fmt.Errorf("num: writing u128: %w", err)
	}

	return nil
}
