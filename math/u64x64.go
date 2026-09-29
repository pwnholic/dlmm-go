package math

import (
	"errors"
	"fmt"

	"github.com/pwnholic/dlmm-go/num"
)

// One is 1.0 in Q64.64.
var One = num.U128{Hi: 1}

// MaxExponential bounds the exponent of Pow.
//
// The bound is not arbitrary. With the smallest bin step of 1 basis point the
// largest usable bin id is 443636, because (1 + 0.0001)^n must stay below 2^64.
// Expressing 443636 in binary takes 19 bits, and the 20th bit is 0x80000, whose
// exponential already exceeds what Q64.64 can represent. So exponents at or above
// 0x80000 are refused rather than computed and allowed to overflow.
const MaxExponential = 0x80000

// Pow returns base^exp as a Q64.64 value.
//
// This is a faithful port of the Rust pow in u64x64_math.rs, and the faithfulness
// matters more than the elegance. Three details are deliberate and each would
// change results if "cleaned up":
//
//   - The inversion is u128::MAX / x, not (1<<128) / x. Those differ by a few
//     units, which the test over 140 real bin prices detects.
//   - The base is inverted when it is at least one, and that flips which side of
//     the result gets inverted at the end. It is not a reciprocal applied on top
//     of an otherwise normal path. The two inversions therefore cancel, which is
//     why the direction is combined with an exclusive or rather than applied in
//     sequence.
//   - A zero result is an error, and an exponent at or beyond MaxExponential is
//     refused rather than computed.
//
// One consequence is worth stating because it is surprising: Pow(One, n) is not
// exactly One for nonzero n. The u128::MAX numerator leaves a residue of a few
// units. That is the reference's behaviour, not a defect here.
func Pow(base num.U128, exp int32) (num.U128, error) {
	if exp == 0 {
		return One, nil
	}

	inverted, magnitude, err := normalizeExponent(exp)
	if err != nil {
		return num.U128{}, err
	}

	result, baseInverted, err := expBySquaring(base, magnitude)
	if err != nil {
		return num.U128{}, fmt.Errorf("pow: %w", err)
	}

	// The reference flips its invert flag when it inverts the base, so the base
	// inversion cancels the negative exponent instead of compounding with it.
	if inverted != baseInverted {
		result, err = Div(maxU128(), result)
		if err != nil {
			return num.U128{}, fmt.Errorf("pow: inverting the result: %w", err)
		}
	}

	return result, nil
}

// normalizeExponent reports whether the exponent asks for an inversion, and its
// magnitude.
//
// The magnitude is taken in int64 because negating int32's most negative value
// would overflow int32. The conversion to uint32 happens only after the bound is
// checked, so it cannot truncate.
func normalizeExponent(exp int32) (inverted bool, magnitude uint32, err error) {
	if exp >= 0 {
		if exp >= MaxExponential {
			return false, 0, fmt.Errorf("%w: exponent %d reaches the %d limit",
				ErrOverflow, exp, MaxExponential)
		}

		return false, uint32(exp), nil
	}

	abs := -int64(exp)
	if abs >= MaxExponential {
		return false, 0, fmt.Errorf("%w: exponent %d reaches the %d limit",
			ErrOverflow, abs, MaxExponential)
	}

	return true, uint32(abs), nil
}

// expBySquaring computes base^exp by the square-and-multiply method, reporting
// whether it inverted the base.
//
// The multiply comes before the square in each step, so bit k multiplies by
// base^(2^k). The reference writes this as nineteen unrolled if-statements
// because it is on a hot path; the loop produces the same sequence of operations.
func expBySquaring(base num.U128, exp uint32) (result num.U128, baseInverted bool, err error) {
	squaredBase := base
	result = One

	// Multiplying two Q64.64 values that are both at least one would overflow 128
	// bits, so the computation is done on the reciprocal instead.
	if squaredBase.Cmp(result) >= 0 {
		squaredBase, err = Div(maxU128(), squaredBase)
		if err != nil {
			return num.U128{}, false, fmt.Errorf("inverting the base: %w", err)
		}

		baseInverted = true
	}

	for bit := uint32(1); bit < MaxExponential; bit <<= 1 {
		if exp&bit != 0 {
			result, err = mulShrExact(result, squaredBase)
			if err != nil {
				return num.U128{}, false, err
			}
		}

		// The reference squares between multiply steps and does not square after
		// the final bit. An extra square at the end would not change the result,
		// because nothing reads squaredBase again, but it could reject an input
		// the reference accepts, since the square is the operation that overflows.
		if bit<<1 < MaxExponential {
			squaredBase, err = mulShrExact(squaredBase, squaredBase)
			if err != nil {
				return num.U128{}, false, fmt.Errorf("squaring the base: %w", err)
			}
		}
	}

	// Checked once, after the loop, where the reference checks it. The result only
	// grows, so an earlier check would report the same condition.
	if result.IsZero() {
		return num.U128{}, false, errors.New("result underflowed to zero")
	}

	return result, baseInverted, nil
}

// maxU128 returns 2^128 - 1, which is the u128::MAX the Rust reference divides
// by. Using 2^128 instead would be off by one and would change the low bits of
// every price.
func maxU128() num.U128 {
	return num.U128{Lo: ^uint64(0), Hi: ^uint64(0)}
}

// mulShrExact returns (a * b) >> 64.
//
// It differs from MulShr in that the product must fit 128 bits first. The Rust
// reference calls checked_mul and then shifts, so a product that overflows is
// refused even though the shifted result would be representable. Reproducing the
// refusal keeps this port in step with the reference on exactly the inputs where
// the reference gives up.
func mulShrExact(a, b num.U128) (num.U128, error) {
	product, err := Mul(a, b)
	if err != nil {
		return num.U128{}, err
	}

	return Shr(product, 64)
}
