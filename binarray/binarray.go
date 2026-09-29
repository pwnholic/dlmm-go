// Package binarray maps between bin IDs and the bin array accounts that hold
// them.
//
// A bin array stores MaxBinPerArray (70) consecutive bins, so a bin ID has to be
// divided by 70 to find its array. The division is a FLOOR division, not the
// truncating division that Go's / operator performs for negative operands. Go
// and Rust agree on truncation, so the correction is mirrored from
// bin_id_to_bin_array_index in the Rust reference rather than assumed:
//
//	bin ID   -71  -70   -1    0    1    69    70
//	index     -2   -1   -1    0    0     0     1
//
// Getting this wrong is silent: the derived address is a valid public key, it
// simply has no account behind it, and the failure surfaces much later as an
// empty account.
//
// This package performs no I/O.
package binarray

import (
	"errors"
	"fmt"
	"math"

	"github.com/pwnholic/dlmm-go/program/lbclmm"
)

// MaxIndexesPerRange bounds how many bin array indexes one range request may
// return.
//
// It is the total number of indexes the program can represent: the default
// bitmap plus the full extension on both sides. The bound exists because the
// request is driven by caller-supplied bin IDs, and without a cap a single
// nonsensical range would ask for a slice far larger than any real position.
const MaxIndexesPerRange = lbclmm.MaxBinArrayIndexWithExtension -
	lbclmm.MinBinArrayIndexWithExtension + 1

// MaxBinID and MinBinID re-export the program's addressable bin ID bounds so
// callers of this package do not need a second import.
const (
	// MinBinID is the lowest addressable bin ID.
	MinBinID = lbclmm.MinBinID

	// MaxBinID is the highest addressable bin ID.
	MaxBinID = lbclmm.MaxBinID
)

// Sentinel errors, matchable with errors.Is.
var (
	// ErrIndexOutOfRange reports a bin array index outside the range the
	// program can represent, with or without the extension bitmap.
	ErrIndexOutOfRange = errors.New("binarray: bin array index out of range")
	// ErrBinOutOfRange reports a bin ID outside the range the program allows.
	ErrBinOutOfRange = errors.New("binarray: bin ID out of range")
	// ErrRangeTooLarge reports a bin array index range wider than the program
	// can represent.
	ErrRangeTooLarge = errors.New("binarray: requested range is too wide")
	// ErrInvertedRange reports an upper bound below its lower bound.
	ErrInvertedRange = errors.New("binarray: upper bound is below lower bound")
	// ErrBinNotInArray reports a bin ID that the given bin array does not hold.
	ErrBinNotInArray = errors.New("binarray: bin ID is not within the given bin array")
)

// IndexForBin returns the bin array index holding binID.
//
// The result is a floor division by MaxBinPerArray, so a bin ID of -1 maps to
// index -1 rather than to index 0.
//
// Any int32 input has a representable index, so this cannot fail.
func IndexForBin(binID int32) int32 {
	idx := int64(binID) / int64(lbclmm.MaxBinPerArray)
	rem := int64(binID) % int64(lbclmm.MaxBinPerArray)

	// Truncating division rounds toward zero; floor division must round down,
	// which differs exactly when the dividend is negative and not a multiple.
	if binID < 0 && rem != 0 {
		idx--
	}

	// |idx| <= |binID|/70, so the result always fits int32; the conversion
	// cannot truncate for any int32 input.
	//nolint:gosec // G115: idx is bounded by |binID|/MaxBinPerArray, provably in range
	return int32(idx)
}

// BinRangeForIndex returns the inclusive bin ID range held by a bin array.
//
// The index must be one the program can address; anything outside the extension
// range is rejected rather than allowed to wrap when multiplied by 70.
func BinRangeForIndex(index int32) (lower, upper int32, err error) {
	if !IndexIsRepresentable(index) {
		return 0, 0, fmt.Errorf("%w: index %d is outside [%d, %d]",
			ErrIndexOutOfRange, index, lbclmm.MinBinArrayIndexWithExtension, lbclmm.MaxBinArrayIndexWithExtension)
	}

	// Compute in int64: index*70 is representable but index*70+69 overflows
	// int32 for a large index, so the arithmetic must not happen in int32.
	base := int64(index) * int64(lbclmm.MaxBinPerArray)
	lo := base
	hi := base + int64(lbclmm.MaxBinPerArray) - 1

	if lo < math.MinInt32 || hi > math.MaxInt32 {
		return 0, 0, fmt.Errorf("%w: index %d spans bins outside the int32 range",
			ErrIndexOutOfRange, index)
	}

	// The guard above is what makes these conversions exact rather than
	// truncating; the linter cannot see through it.
	//nolint:gosec // G115: the preceding bounds check proves both conversions exact
	return int32(lo), int32(hi), nil
}

// IndexIsRepresentable reports whether index is one the program can address,
// taking the bitmap extension into account.
func IndexIsRepresentable(index int32) bool {
	return index >= lbclmm.MinBinArrayIndexWithExtension &&
		index <= lbclmm.MaxBinArrayIndexWithExtension
}

// IsInDefaultBitmap reports whether index is covered by the pool's own bitmap.
//
// When this is false the pool's bitmap cannot describe the index, and the
// bin array bitmap extension account must exist and be consulted instead.
func IsInDefaultBitmap(index int32) bool {
	return index >= lbclmm.MinBinArrayIndexInDefaultBitmap &&
		index <= lbclmm.MaxBinArrayIndexInDefaultBitmap
}

// ContainsBin reports whether the bin array at index holds binID.
//
// It is false for an unrepresentable index rather than an error, because the
// common use is a membership test against a range.
func ContainsBin(index, binID int32) bool {
	lower, upper, err := BinRangeForIndex(index)
	if err != nil {
		return false
	}

	return binID >= lower && binID <= upper
}

// OffsetOfBin returns binID's position within the bin array at index, in
// [0, MaxBinPerArray).
//
// This is the index into the account's bins array, not a bin ID.
func OffsetOfBin(index, binID int32) (int, error) {
	lower, upper, err := BinRangeForIndex(index)
	if err != nil {
		return 0, err
	}

	if binID < lower || binID > upper {
		return 0, fmt.Errorf("%w: bin %d is not in index %d (range [%d, %d])",
			ErrBinNotInArray, binID, index, lower, upper)
	}

	return int(binID - lower), nil
}

// IndexesForBinRange returns every bin array index that the inclusive bin ID
// range touches, in ascending order.
//
// The result is bounded by MaxIndexesPerRange so that a mistyped or hostile
// range cannot ask for an unbounded slice.
func IndexesForBinRange(lowerBinID, upperBinID int32) ([]int32, error) {
	if err := checkBinBounds(lowerBinID); err != nil {
		return nil, err
	}

	if err := checkBinBounds(upperBinID); err != nil {
		return nil, err
	}

	if upperBinID < lowerBinID {
		return nil, fmt.Errorf("%w: [%d, %d]", ErrInvertedRange, lowerBinID, upperBinID)
	}

	lowerIndex := IndexForBin(lowerBinID)
	upperIndex := IndexForBin(upperBinID)

	count := int64(upperIndex) - int64(lowerIndex) + 1
	if count <= 0 || count > MaxIndexesPerRange {
		return nil, fmt.Errorf("%w: %d indexes requested, limit is %d",
			ErrRangeTooLarge, count, MaxIndexesPerRange)
	}

	indexes := make([]int32, 0, count)
	for i := lowerIndex; i <= upperIndex; i++ {
		indexes = append(indexes, i)
	}

	return indexes, nil
}

// IsOverflowDefaultBitmap reports whether a bin ID needs the bitmap extension.
//
// It is the convenience form of IsInDefaultBitmap(IndexForBin(binID)) for
// callers that hold bin IDs rather than indexes.
func IsOverflowDefaultBitmap(binID int32) bool {
	return !IsInDefaultBitmap(IndexForBin(binID))
}

// checkBinBounds rejects a bin ID the program cannot address.
//
// The bound is MinBinID/MaxBinID, not MaxBinIDPerBinStep: the latter measures a
// bin range per bin step, not the addressable bin ID space.
func checkBinBounds(binID int32) error {
	if binID < lbclmm.MinBinID || binID > lbclmm.MaxBinID {
		return fmt.Errorf("%w: %d is outside [%d, %d]",
			ErrBinOutOfRange, binID, lbclmm.MinBinID, lbclmm.MaxBinID)
	}

	return nil
}
