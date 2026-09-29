package math

import (
	"errors"
	"fmt"

	"github.com/pwnholic/dlmm-go/internal/uint256"
	"github.com/pwnholic/dlmm-go/num"
)

// MaxShiftOffset is the largest shift the Rust reference accepts: 1u128 must
// still be representable, so the offset is bounded at 127.
const MaxShiftOffset = 127

// checkRounding rejects a Rounding that is not one of the defined modes.
func checkRounding(r Rounding) error {
	if r != RoundingDown && r != RoundingUp {
		return fmt.Errorf("%w: %d", ErrInvalidRounding, r)
	}

	return nil
}

// wrapOverflow maps an internal/uint256 failure onto this package's sentinels.
//
// Without this, callers would have to match on uint256's errors, and a caller
// writing errors.Is(err, math.ErrOverflow) would silently get false for two of
// the three entry points. Errors are therefore normalised at this boundary:
// callers only ever match against math's sentinels.
func wrapOverflow(op string, err error) error {
	switch {
	case errors.Is(err, uint256.ErrOverflow):
		return fmt.Errorf("math: %s: %w", op, ErrOverflow)
	case errors.Is(err, uint256.ErrDivideByZero):
		return fmt.Errorf("math: %s: %w", op, ErrDivideByZero)
	default:
		return fmt.Errorf("math: %s: %w", op, err)
	}
}

// MulDiv returns (x * y) / denominator, rounded per r.
//
// This mirrors mul_div in dlmm-sdk/commons/src/math/u128x128_math.rs. A
// denominator of zero and a result wider than 128 bits are both errors, the
// two conditions that make the Rust version return None.
func MulDiv(x, y, denominator num.U128, r Rounding) (num.U128, error) {
	if err := checkRounding(r); err != nil {
		return num.U128{}, err
	}

	if denominator.IsZero() {
		return num.U128{}, ErrDivideByZero
	}

	// Two 128-bit factors always fit in 256 bits, so the product cannot
	// overflow the wide representation.
	product := uint256.Mul128To256(x, y)

	if r == RoundingUp {
		q, err := uint256.DivCeil256By128(product, denominator)
		if err != nil {
			return num.U128{}, wrapOverflow("mul_div", err)
		}

		return q, nil
	}

	q, _, err := uint256.Div256By128(product, denominator)
	if err != nil {
		return num.U128{}, wrapOverflow("mul_div", err)
	}

	return q, nil
}

// MulShr returns (x * y) >> offset, rounded per r.
//
// This is the hot path: unlike MulDiv it needs no division. Dividing by 2^offset
// is a shift plus a check of whether any bit was discarded, and rounding up is
// just an increment when one was.
func MulShr(x, y num.U128, offset uint8, r Rounding) (num.U128, error) {
	if err := checkRounding(r); err != nil {
		return num.U128{}, err
	}

	if offset > MaxShiftOffset {
		return num.U128{}, fmt.Errorf("%w: got %d", ErrOffsetTooLarge, offset)
	}

	product := uint256.Mul128To256(x, y)
	shifted := product.Shr(uint(offset))

	// Rounding up means discarding bits were non-zero, so add one back.
	if r == RoundingUp && product.HasBitsBelow(uint(offset)) {
		shifted = shifted.Add1()
	}

	out, err := shifted.ToU128()
	if err != nil {
		return num.U128{}, wrapOverflow("mul_shr", err)
	}

	return out, nil
}

// ShlDiv returns (x << offset) / y, rounded per r.
//
// This mirrors shl_div in the Rust reference, which is mul_div with the scale
// as the second factor.
func ShlDiv(x, y num.U128, offset uint8, r Rounding) (num.U128, error) {
	if err := checkRounding(r); err != nil {
		return num.U128{}, err
	}

	if offset > MaxShiftOffset {
		return num.U128{}, fmt.Errorf("%w: got %d", ErrOffsetTooLarge, offset)
	}

	if y.IsZero() {
		return num.U128{}, ErrDivideByZero
	}

	// x is at most 128 bits and the offset is at most 127, so the shifted
	// value needs at most 255 bits and cannot be lost by the 256-bit form.
	scaled := uint256.U256FromU128(x).Shl(uint(offset))

	if r == RoundingUp {
		q, err := uint256.DivCeil256By128(scaled, y)
		if err != nil {
			return num.U128{}, wrapOverflow("shl_div", err)
		}

		return q, nil
	}

	q, _, err := uint256.Div256By128(scaled, y)
	if err != nil {
		return num.U128{}, wrapOverflow("shl_div", err)
	}

	return q, nil
}
