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
