// Package uint256 provides the 256-bit intermediate arithmetic that the
// DLMM swap math needs.
//
// The Rust reference computes (x * y) / d and (x * y) >> offset on u128 inputs
// by widening to U256 for the product. Go has no native 256-bit integer, so
// this package supplies exactly the operations that widening requires and
// nothing more.
//
// It deliberately avoids math/big: these calls sit in the swap loop, and big.Int
// allocates on every operation. math/big is used only in this package's tests,
// as an independent oracle.
//
// Values are little-endian limbs: L0 is the least significant 64 bits.
package uint256

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"slices"
	"strconv"
	"strings"

	"github.com/pwnholic/dlmm-go/num"
)

// U256 is an unsigned 256-bit integer as four little-endian 64-bit limbs.
// The zero value is a valid zero.
type U256 struct {
	L0, L1, L2, L3 uint64
}

// ErrOverflow reports a result that does not fit in the destination width.
var ErrOverflow = errors.New("uint256: value does not fit")

// NewU256 builds a U256 from limbs given most-significant first.
func NewU256(l3, l2, l1, l0 uint64) U256 {
	return U256{L0: l0, L1: l1, L2: l2, L3: l3}
}

// U256FromU128 widens a num.U128.
func U256FromU128(v num.U128) U256 {
	return U256{L0: v.Lo, L1: v.Hi}
}

// U256FromU64 widens a uint64.
func U256FromU64(v uint64) U256 { return U256{L0: v} }

// IsZero reports whether n is zero.
func (n U256) IsZero() bool { return n == U256{} }

// IsU128 reports whether n fits in 128 bits without loss.
func (n U256) IsU128() bool { return n.L2 == 0 && n.L3 == 0 }

// ToU128 narrows n to num.U128.
//
// It reports ErrOverflow rather than truncating: a silently wrapped on-chain
// amount is not recoverable.
func (n U256) ToU128() (num.U128, error) {
	if !n.IsU128() {
		return num.U128{}, fmt.Errorf("%w: %s exceeds 128 bits", ErrOverflow, n)
	}

	return num.U128{Lo: n.L0, Hi: n.L1}, nil
}

// Mul128To256 returns a*b as a full 256-bit product.
//
// The product of two 128-bit values always fits in 256 bits, so this cannot
// overflow and returns no error.
//
// It computes a*b = aLo*bLo + (aLo*bHi + aHi*bLo)·2^64 + aHi*bHi·2^128 using
// four 64x64->128 partial products, which is the cheapest exact form: the two
// cross terms differ only by operand order and share the same accumulation slot.
//
// Two details here are easy to get wrong and were both wrong in the first
// version of this function:
//
//  1. bits.Mul64 returns (hi, lo), upper half first. Writing the low word first
//     silently swaps every half before accumulation even begins.
//  2. The carry out of the cross-term sum has weight 2^192, not 2^128. The cross
//     sum occupies bits 64..191, so a carry out of it lands in limb 3.
func Mul128To256(a, b num.U128) U256 {
	// bits.Mul64 returns the upper half first.
	p0Hi, p0Lo := bits.Mul64(a.Lo, b.Lo)

	// The two cross terms both occupy bits 64..191.
	c0Hi, c0Lo := bits.Mul64(a.Lo, b.Hi)
	c1Hi, c1Lo := bits.Mul64(a.Hi, b.Lo)

	crossLo, carry := bits.Add64(c0Lo, c1Lo, 0)
	crossHi, crossCarry := bits.Add64(c0Hi, c1Hi, carry)

	// hh occupies bits 128..255.
	hhHi, hhLo := bits.Mul64(a.Hi, b.Hi)

	// Accumulate low to high, carrying between limbs. crossCarry is added at
	// limb 3 because it is the bit-192 overflow of the cross-term sum, which is
	// then scaled by 2^64 on its way into the product.
	l1, carryLow := bits.Add64(p0Hi, crossLo, 0)
	l2, carryMid := bits.Add64(crossHi, hhLo, carryLow)
	l3, _ := bits.Add64(hhHi, crossCarry, carryMid)

	return U256{L0: p0Lo, L1: l1, L2: l2, L3: l3}
}

// Shr returns n >> shift.
//
// A shift of 256 or more yields zero, matching the Rust reference's behaviour
// of rejecting an out-of-range offset at the call site rather than wrapping.
func (n U256) Shr(shift uint) U256 {
	if shift >= 256 {
		return U256{}
	}

	word := shift / 64
	bit := shift % 64

	limbs := [4]uint64{n.L0, n.L1, n.L2, n.L3}

	var out [4]uint64

	for i := range 4 {
		src := i + int(word)
		if src >= 4 {
			break
		}

		// i is always < 4 because it ranges over the array, and src < 4 is
		// guaranteed by the break above. gosec cannot follow either fact.
		//nolint:gosec // G602: src < 4 by the break, i < 4 by the range
		out[i] = limbs[src] >> bit
		if bit != 0 && src+1 < 4 {
			//nolint:gosec // G602: src+1 < 4 is the condition on this branch
			out[i] |= limbs[src+1] << (64 - bit)
		}
	}

	return U256{L0: out[0], L1: out[1], L2: out[2], L3: out[3]}
}

// Add1 returns n+1.
//
// A 256-bit value plus one cannot overflow, so this never fails.
func (n U256) Add1() U256 {
	l0, carry := bits.Add64(n.L0, 1, 0)
	l1, carry := bits.Add64(n.L1, 0, carry)
	l2, carry := bits.Add64(n.L2, 0, carry)
	l3, _ := bits.Add64(n.L3, 0, carry)

	return U256{L0: l0, L1: l1, L2: l2, L3: l3}
}

// HasBitsBelow reports whether any of the low `shift` bits of n is set.
//
// It is what distinguishes RoundingUp from RoundingDown for a right shift:
// rounding up means "round the truncated result toward the discarded bits".
//
// It indexes the limbs as an array rather than switching per limb. The switch
// version was both longer and buggy: it had no case for limb 3, so a shift in
// 193..255 never inspected the top limb at all.
func (n U256) HasBitsBelow(shift uint) bool {
	if shift == 0 {
		return false
	}

	if shift >= 256 {
		return !n.IsZero()
	}

	word := shift / 64
	bit := shift % 64
	limbs := [4]uint64{n.L0, n.L1, n.L2, n.L3}

	// Limbs entirely below the window count in full.
	for i := range word {
		if limbs[i] != 0 {
			return true
		}
	}

	// The limb the window cuts through: only its low `bit` bits are below.
	if bit == 0 {
		return false
	}

	return limbs[word]&(uint64(1)<<bit-1) != 0
}

// Shl returns n << shift.
//
// A shift of 256 or more yields zero. Truncation is intentional and matches
// the left-shift semantics of the Rust reference; callers that must not lose
// the high bits check IsU128 on the result.
func (n U256) Shl(shift uint) U256 {
	if shift >= 256 {
		return U256{}
	}

	word := shift / 64
	bit := shift % 64

	limbs := [4]uint64{n.L0, n.L1, n.L2, n.L3}

	var out [4]uint64

	for i := 3; i >= 0; i-- {
		src := i - int(word)
		if src < 0 {
			break
		}

		// i indexes the array it ranges over, and src >= 0 is guaranteed by the
		// break above. gosec cannot follow either fact.
		out[i] = limbs[src] << bit
		if bit != 0 && src-1 >= 0 {
			//nolint:gosec // G602: src-1 >= 0 is the condition on this branch
			out[i] |= limbs[src-1] >> (64 - bit)
		}
	}

	return U256{L0: out[0], L1: out[1], L2: out[2], L3: out[3]}
}

// Cmp compares n and m, returning -1, 0, or +1.
func (n U256) Cmp(m U256) int {
	switch {
	case n.L3 != m.L3:
		return cmpU64(n.L3, m.L3)
	case n.L2 != m.L2:
		return cmpU64(n.L2, m.L2)
	case n.L1 != m.L1:
		return cmpU64(n.L1, m.L1)
	case n.L0 != m.L0:
		return cmpU64(n.L0, m.L0)
	default:
		return 0
	}
}

func cmpU64(a, b uint64) int {
	if a < b {
		return -1
	}

	return +1
}

// BytesBE returns the 32-byte big-endian encoding.
//
// It exists for diagnostics and for comparison against math/big in tests; the
// wire format is little-endian and is handled by the codec in program/lbclmm.
//
// It uses encoding/binary rather than shifting bytes out by hand. The manual
// version was seven separate uint64-to-byte truncations, each of which a
// security linter must flag because it cannot see that the truncation is
// intended.
func (n U256) BytesBE() [32]byte {
	var b [32]byte
	binary.BigEndian.PutUint64(b[0:8], n.L3)
	binary.BigEndian.PutUint64(b[8:16], n.L2)
	binary.BigEndian.PutUint64(b[16:24], n.L1)
	binary.BigEndian.PutUint64(b[24:32], n.L0)

	return b
}

// String renders n in decimal.
//
// It is implemented with repeated division by 10^19 rather than math/big, so
// that this package contains no big.Int at all and the test oracle in
// uint256_test.go stays genuinely independent of the code under test.
func (n U256) String() string {
	if n.IsZero() {
		return "0"
	}

	// 10^19 is the largest power of ten that fits in a uint64.
	const chunk = uint64(10_000_000_000_000_000_000)

	limbs := [4]uint64{n.L0, n.L1, n.L2, n.L3}
	parts := make([]string, 0, 5)

	for {
		var rem uint64

		// Divide the 4-limb value by 10^19 in place. bits.Div64 returns
		// (quo, rem), so the quotient becomes the new limb and the remainder
		// carries into the next iteration. bits.Div64 requires hi < divisor,
		// which holds because rem is always < chunk.
		for i := 3; i >= 0; i-- {
			q, r := bits.Div64(rem, limbs[i], chunk)
			limbs[i] = q
			rem = r
		}

		if limbs == [4]uint64{} {
			parts = append(parts, strconv.FormatUint(rem, 10))
			break
		}
		// Every chunk except the most significant one is zero-padded.
		parts = append(parts, pad19(rem))
	}

	var sb strings.Builder
	for _, part := range slices.Backward(parts) {
		sb.WriteString(part)
	}

	return sb.String()
}

// pad19 formats v as exactly 19 decimal digits.
func pad19(v uint64) string {
	s := strconv.FormatUint(v, 10)
	if len(s) >= 19 {
		return s
	}

	return strings.Repeat("0", 19-len(s)) + s
}
