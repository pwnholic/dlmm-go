// Package math implements the fixed-point arithmetic that the DLMM program
// uses, matching the Rust reference in dlmm-sdk/commons/src/math.
//
// The reference implementation works in u128 and widens to U256 for the
// intermediate product. Go has no native 128-bit integer, so the value type
// lives in package num and the widening lives in internal/uint256.
//
// Overflow policy: the Rust code returns Option<u128>, which is None when the
// result does not fit. Here that is an error, never a silently truncated value.
// An on-chain amount that wrapped is not recoverable, so every operation that
// could exceed 128 bits reports it instead of hiding it.
package math

import "errors"

// Rounding selects how a truncated result is adjusted.
type Rounding uint8

// Rounding has an explicit invalid zero value so that a zero-valued Rounding —
// a missing field in a struct, a zero-initialised config — is caught rather
// than silently behaving as the first real mode.
const (
	// RoundingInvalid is not a usable mode. Its presence as the zero value
	// turns an uninitialised Rounding into an error instead of a silent
	// rounding decision.
	RoundingInvalid Rounding = iota
	// RoundingDown truncates toward zero, matching Rust's div_rem.
	RoundingDown
	// RoundingUp rounds away from zero, matching Rust's div_ceil.
	RoundingUp
)

// String renders the rounding mode for diagnostics.
func (r Rounding) String() string {
	switch r {
	case RoundingDown:
		return "down"
	case RoundingUp:
		return "up"
	case RoundingInvalid:
		return "invalid"
	default:
		return "unknown"
	}
}

// Sentinel errors. All are matchable with errors.Is.
var (
	// ErrDivideByZero reports a zero denominator.
	ErrDivideByZero = errors.New("math: division by zero")
	// ErrOffsetTooLarge reports a shift offset of 128 or more, which the
	// Rust reference rejects through 1u128.checked_shl returning None.
	ErrOffsetTooLarge = errors.New("math: shift offset must be less than 128")
	// ErrOverflow reports a result that does not fit in 128 bits.
	ErrOverflow = errors.New("math: result does not fit in 128 bits")
	// ErrInvalidRounding reports a Rounding outside the defined modes.
	ErrInvalidRounding = errors.New("math: invalid rounding mode")
)
