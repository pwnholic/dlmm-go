package math

import (
	"fmt"
	"math/bits"

	"github.com/pwnholic/dlmm-go/internal/uint256"
	"github.com/pwnholic/dlmm-go/num"
)

// Add returns a + b.
//
// It reports ErrOverflow when the sum exceeds 128 bits. The Rust reference uses
// checked_add for the same operation, so wrapping silently here would diverge
// from the program exactly where the program refuses.
func Add(a, b num.U128) (num.U128, error) {
	lo, carry := bits.Add64(a.Lo, b.Lo, 0)

	hi, carryOut := bits.Add64(a.Hi, b.Hi, carry)
	if carryOut != 0 {
		return num.U128{}, fmt.Errorf("%w: %s + %s", ErrOverflow, a, b)
	}

	return num.U128{Lo: lo, Hi: hi}, nil
}

// Sub returns a - b.
//
// It reports ErrOverflow when b exceeds a, which is the Rust checked_sub case.
func Sub(a, b num.U128) (num.U128, error) {
	lo, borrow := bits.Sub64(a.Lo, b.Lo, 0)

	hi, borrowOut := bits.Sub64(a.Hi, b.Hi, borrow)
	if borrowOut != 0 {
		return num.U128{}, fmt.Errorf("%w: %s - %s is negative", ErrOverflow, a, b)
	}

	return num.U128{Lo: lo, Hi: hi}, nil
}

// Mul returns a * b.
//
// It reports ErrOverflow when the product exceeds 128 bits, matching the
// checked_mul calls in the Rust reference.
func Mul(a, b num.U128) (num.U128, error) {
	product := uint256.Mul128To256(a, b)
	if !product.IsU128() {
		return num.U128{}, fmt.Errorf("%w: %s * %s", ErrOverflow, a, b)
	}

	out, err := product.ToU128()
	if err != nil {
		return num.U128{}, fmt.Errorf("%w: %s * %s", ErrOverflow, a, b)
	}

	return out, nil
}

// AddU64 returns a + b for small values, as a convenience for the fee math.
func AddU64(a, b uint64) (num.U128, error) {
	return Add(num.U128FromU64(a), num.U128FromU64(b))
}

// Div returns a / b, truncated toward zero.
//
// It reports ErrDivideByZero when b is zero and ErrOverflow when the quotient
// exceeds 128 bits, which cannot happen for a 128-bit numerator but is checked
// so the function has one contract regardless of its inputs.
func Div(a, b num.U128) (num.U128, error) {
	if b.IsZero() {
		return num.U128{}, fmt.Errorf("%w: division by zero", ErrDivideByZero)
	}

	q, _, err := uint256.Div256By128(uint256.U256FromU128(a), b)
	if err != nil {
		return num.U128{}, fmt.Errorf("%w: %s / %s", ErrOverflow, a, b)
	}

	return q, nil
}

// Shr returns a >> shift.
//
// A shift of 128 or more is zero, matching the Rust shifts which are applied to
// values known to fit; a shift of exactly 64 is the SCALE_OFFSET conversion used
// throughout the price math.
func Shr(a num.U128, shift uint8) (num.U128, error) {
	if shift > MaxShiftOffset {
		return num.U128{}, fmt.Errorf("%w: shift %d exceeds %d", ErrOverflow, shift, MaxShiftOffset)
	}

	if shift == 0 {
		return a, nil
	}

	switch {
	case shift < 64:
		lo := a.Lo>>shift | a.Hi<<(64-shift)
		hi := a.Hi >> shift

		return num.U128{Lo: lo, Hi: hi}, nil
	case shift == 64:
		return num.U128{Lo: a.Hi}, nil
	default:
		return num.U128{Lo: a.Hi >> (shift - 64)}, nil
	}
}
