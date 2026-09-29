package binarray

import (
	"errors"
	"math"
	"testing"

	"github.com/pwnholic/dlmm-go/program/lbclmm"
)

const perArray = lbclmm.MaxBinPerArray

func TestIndexForBinIsFloorDivisionNotTruncation(t *testing.T) {
	t.Parallel()

	// The whole point of this package. Truncating division would map -1 to
	// index 0, and index 0 holds bins 0..69, so bin -1 would resolve to a bin
	// array that does not contain it.
	tests := []struct {
		name  string
		binID int32
		want  int32
	}{
		{"zero is in index zero", 0, 0},
		{"first positive bin", 1, 0},
		{"last bin of index zero", perArray - 1, 0},
		{"first bin of index one", perArray, 1},
		{"minus one underflows to minus one", -1, -1},
		{"minus one is not index zero", -1, -1},
		{"last bin of index minus one", -perArray, -1},
		{"one below that", -perArray - 1, -2},
		{"minus two", -2, -1},
		{"large positive", 351_639, 5_023},
		{"large negative", -351_639, -5_024},
		{"int32 max", math.MaxInt32, 30_678_337},
		{"int32 min", math.MinInt32, -30_678_338},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IndexForBin(tt.binID); got != tt.want {
				t.Errorf("IndexForBin(%d) = %d, want %d", tt.binID, got, tt.want)
			}
		})
	}
}

func TestIndexForBinSatisfiesFloorDivisionProperty(t *testing.T) {
	t.Parallel()

	// The defining property: lower <= binID < lower + perArray, where lower is
	// the first bin of the returned index. This holds for every int32, so it is
	// checked exhaustively across the program's real range plus the boundaries.
	check := func(t *testing.T, binID int32) {
		t.Helper()

		index := IndexForBin(binID)

		lower := int64(index) * int64(perArray)
		if int64(binID) < lower {
			t.Errorf("bin %d is below its own index %d (lower bound %d)", binID, index, lower)
		}

		if int64(binID) >= lower+int64(perArray) {
			t.Errorf("bin %d is above the end of its index %d (upper bound %d)", binID, index, lower+int64(perArray)-1)
		}
	}

	// Every bin ID the program can address.
	for binID := int32(-lbclmm.MaxBinIDPerBinStep); binID <= lbclmm.MaxBinIDPerBinStep; binID++ {
		check(t, binID)
	}

	// The extrema, which the loop above does not reach.
	for _, binID := range []int32{math.MaxInt32, math.MinInt32, math.MaxInt32 - 1, math.MinInt32 + 1} {
		check(t, binID)
	}
}

func TestIndexForBinAdvancesExactlyOncePerArray(t *testing.T) {
	t.Parallel()

	// Crossing an array boundary must change the index by exactly one, in both
	// directions. An off-by-one at the boundary is the failure this catches.
	for binID := int32(-1000); binID <= 1000; binID++ {
		next := IndexForBin(binID + perArray)
		if got, want := next-IndexForBin(binID), int32(1); got != want {
			t.Fatalf("index advanced by %d between bin %d and %d, want %d", got, binID, binID+perArray, want)
		}
	}
}

func TestBinRangeForIndexIsInverseOfIndexForBin(t *testing.T) {
	t.Parallel()

	for index := int32(-20); index <= 20; index++ {
		lower, upper, err := BinRangeForIndex(index)
		if err != nil {
			t.Fatalf("BinRangeForIndex(%d): %v", index, err)
		}

		if got := upper - lower + 1; got != perArray {
			t.Errorf("index %d spans %d bins, want %d", index, got, perArray)
		}

		// Every bin in the range must map back to this index.
		if got := IndexForBin(lower); got != index {
			t.Errorf("IndexForBin(lower=%d) = %d, want %d", lower, got, index)
		}

		if got := IndexForBin(upper); got != index {
			t.Errorf("IndexForBin(upper=%d) = %d, want %d", upper, got, index)
		}

		// And the neighbours must not.
		if got := IndexForBin(lower - 1); got == index {
			t.Errorf("bin just below the range still maps to index %d", index)
		}

		if got := IndexForBin(upper + 1); got == index {
			t.Errorf("bin just above the range still maps to index %d", index)
		}
	}
}

func TestBinRangeForIndexIsContiguous(t *testing.T) {
	t.Parallel()

	// Consecutive indexes must produce adjacent, non-overlapping ranges, so
	// covering a bin range never misses a bin or double-counts one.
	var prevUpper int32

	for index := int32(-50); index <= 50; index++ {
		lower, upper, err := BinRangeForIndex(index)
		if err != nil {
			t.Fatalf("BinRangeForIndex(%d): %v", index, err)
		}

		if index > -50 && lower != prevUpper+1 {
			t.Fatalf("index %d starts at %d but the previous index ended at %d", index, lower, prevUpper)
		}

		prevUpper = upper
	}
}

func TestBinRangeForIndexRejectsUnrepresentableIndex(t *testing.T) {
	t.Parallel()

	// Multiplying an unbounded index by 70 would overflow. The range check has
	// to happen before the multiplication, not after.
	for _, index := range []int32{
		lbclmm.MaxBinArrayIndexWithExtension + 1,
		lbclmm.MinBinArrayIndexWithExtension - 1,
		math.MaxInt32,
		math.MinInt32,
	} {
		if _, _, err := BinRangeForIndex(index); !errors.Is(err, ErrIndexOutOfRange) {
			t.Errorf("BinRangeForIndex(%d) error = %v, want ErrIndexOutOfRange", index, err)
		}
	}
}

func TestDefaultBitmapBoundary(t *testing.T) {
	t.Parallel()

	// The default bitmap covers indexes in [-512, 511]; anything outside needs
	// the extension account. The boundary is where a wrong comparison operator
	// shows up.
	tests := []struct {
		index int32
		want  bool
	}{
		{lbclmm.MinBinArrayIndexInDefaultBitmap, true},
		{lbclmm.MaxBinArrayIndexInDefaultBitmap, true},
		{lbclmm.MinBinArrayIndexInDefaultBitmap - 1, false},
		{lbclmm.MaxBinArrayIndexInDefaultBitmap + 1, false},
		{0, true},
		{-1, true},
	}

	for _, tt := range tests {
		if got := IsInDefaultBitmap(tt.index); got != tt.want {
			t.Errorf("IsInDefaultBitmap(%d) = %v, want %v", tt.index, got, tt.want)
		}

		// The bin-ID form must be the exact negation of the index form: a bin is
		// in the default bitmap exactly when its array index is.
		binID := tt.index * perArray
		if got, want := IsOverflowDefaultBitmap(binID), !tt.want; got != want {
			t.Errorf("IsOverflowDefaultBitmap(%d) = %v, want %v (index %d)", binID, got, want, tt.index)
		}
	}
}

func TestIsOverflowDefaultBitmapMatchesTheIndexForm(t *testing.T) {
	t.Parallel()

	// The two forms must agree, since callers reach for whichever they have.
	for binID := int32(-36_000); binID <= 36_000; binID += 137 {
		want := !IsInDefaultBitmap(IndexForBin(binID))
		if got := IsOverflowDefaultBitmap(binID); got != want {
			t.Fatalf("IsOverflowDefaultBitmap(%d) = %v, index form says %v", binID, got, want)
		}
	}
}

func TestOffsetOfBinCoversZeroToPerArrayMinusOne(t *testing.T) {
	t.Parallel()

	for index := int32(-5); index <= 5; index++ {
		lower, upper, err := BinRangeForIndex(index)
		if err != nil {
			t.Fatalf("BinRangeForIndex(%d): %v", index, err)
		}

		first, err := OffsetOfBin(index, lower)
		if err != nil {
			t.Fatalf("OffsetOfBin(%d, %d): %v", index, lower, err)
		}

		if first != 0 {
			t.Errorf("the first bin of index %d has offset %d, want 0", index, first)
		}

		last, err := OffsetOfBin(index, upper)
		if err != nil {
			t.Fatalf("OffsetOfBin(%d, %d): %v", index, upper, err)
		}

		if last != perArray-1 {
			t.Errorf("the last bin of index %d has offset %d, want %d", index, last, perArray-1)
		}
	}
}

func TestOffsetOfBinRejectsBinFromAnotherArray(t *testing.T) {
	t.Parallel()

	lower, _, err := BinRangeForIndex(3)
	if err != nil {
		t.Fatalf("BinRangeForIndex: %v", err)
	}

	// One bin below the array, and one above it.
	if _, err := OffsetOfBin(3, lower-1); !errors.Is(err, ErrBinNotInArray) {
		t.Errorf("error = %v, want ErrBinNotInArray", err)
	}

	if _, err := OffsetOfBin(3, lower+perArray); !errors.Is(err, ErrBinNotInArray) {
		t.Errorf("error = %v, want ErrBinNotInArray", err)
	}
}

func TestContainsBinMatchesTheRange(t *testing.T) {
	t.Parallel()

	lower, upper, err := BinRangeForIndex(0)
	if err != nil {
		t.Fatalf("BinRangeForIndex: %v", err)
	}

	for binID := lower - 2; binID <= upper+2; binID++ {
		want := binID >= lower && binID <= upper
		if got := ContainsBin(0, binID); got != want {
			t.Errorf("ContainsBin(0, %d) = %v, want %v", binID, got, want)
		}
	}
}

func TestIndexesForBinRangeCoversEveryTouchedArray(t *testing.T) {
	t.Parallel()

	indexes, err := IndexesForBinRange(-1, perArray)
	if err != nil {
		t.Fatalf("IndexesForBinRange: %v", err)
	}

	// Bins -1..70 span indexes -1, 0 and 1.
	want := []int32{-1, 0, 1}
	if len(indexes) != len(want) {
		t.Fatalf("got %v, want %v", indexes, want)
	}

	for i, w := range want {
		if indexes[i] != w {
			t.Errorf("indexes[%d] = %d, want %d", i, indexes[i], w)
		}
	}

	// Every index returned must actually contain part of the range.
	for _, idx := range indexes {
		lower, upper, err := BinRangeForIndex(idx)
		if err != nil {
			t.Fatalf("BinRangeForIndex(%d): %v", idx, err)
		}

		if upper < -1 || lower > perArray {
			t.Errorf("index %d (bins %d..%d) does not intersect the requested range", idx, lower, upper)
		}
	}
}

func TestIndexesForBinRangeIsAscending(t *testing.T) {
	t.Parallel()

	indexes, err := IndexesForBinRange(-500, 500)
	if err != nil {
		t.Fatalf("IndexesForBinRange: %v", err)
	}

	for i := 1; i < len(indexes); i++ {
		if indexes[i] != indexes[i-1]+1 {
			t.Fatalf("indexes are not consecutive at %d: %v", i, indexes)
		}
	}
}

func TestIndexesForBinRangeRejectsInvertedRange(t *testing.T) {
	t.Parallel()

	if _, err := IndexesForBinRange(100, 99); !errors.Is(err, ErrInvertedRange) {
		t.Errorf("error = %v, want ErrInvertedRange", err)
	}
}

func TestIndexesForBinRangeRejectsOutOfBoundsBinIDs(t *testing.T) {
	t.Parallel()

	over := int32(lbclmm.MaxBinID + 1)

	if _, err := IndexesForBinRange(0, over); !errors.Is(err, ErrBinOutOfRange) {
		t.Errorf("error = %v, want ErrBinOutOfRange", err)
	}

	if _, err := IndexesForBinRange(-over, 0); !errors.Is(err, ErrBinOutOfRange) {
		t.Errorf("error = %v, want ErrBinOutOfRange", err)
	}
}

func TestBinIDBoundIsTheAddressableRangeNotMaxBinIDPerBinStep(t *testing.T) {
	t.Parallel()

	// Regression guard. The first version bounded bin IDs by
	// MaxBinIDPerBinStep (351639), which measures a bin range per bin step, not
	// the addressable bin ID space (443636). Bin IDs between the two were
	// wrongly refused even though their bin array index is representable.
	for _, binID := range []int32{351_640, 400_000, 443_636, -351_640, -443_636} {
		index := IndexForBin(binID)
		if !IndexIsRepresentable(index) {
			t.Fatalf("bin %d maps to index %d, which is not representable; the bound is inconsistent", binID, index)
		}

		if _, err := IndexesForBinRange(binID, binID); err != nil {
			t.Errorf("IndexesForBinRange(%d, %d) = %v, want success", binID, binID, err)
		}
	}

	// And one step outside must still be refused.
	if _, err := IndexesForBinRange(MaxBinID+1, MaxBinID+1); !errors.Is(err, ErrBinOutOfRange) {
		t.Errorf("just above the bound: error = %v, want ErrBinOutOfRange", err)
	}

	if _, err := IndexesForBinRange(MinBinID-1, MinBinID-1); !errors.Is(err, ErrBinOutOfRange) {
		t.Errorf("just below the bound: error = %v, want ErrBinOutOfRange", err)
	}
}

func TestIndexesForBinRangeIsBounded(t *testing.T) {
	t.Parallel()

	// The widest range the program allows must still fit the cap, otherwise
	// legitimate full-range requests would be rejected.
	indexes, err := IndexesForBinRange(MinBinID, MaxBinID)
	if err != nil {
		t.Fatalf("IndexesForBinRange over the full range: %v", err)
	}

	if len(indexes) > MaxIndexesPerRange {
		t.Errorf("returned %d indexes, cap is %d", len(indexes), MaxIndexesPerRange)
	}

	if len(indexes) == 0 {
		t.Error("full-range request returned no indexes")
	}
}

func TestMaxIndexesPerRangeMatchesTheProgramLimits(t *testing.T) {
	t.Parallel()

	// The bin ID limit must be reachable through the extension bitmap.
	needed := IndexForBin(MaxBinID)
	if !IndexIsRepresentable(needed) {
		t.Errorf("the largest bin ID needs index %d, which the extension does not cover", needed)
	}

	neededNeg := IndexForBin(MinBinID)
	if !IndexIsRepresentable(neededNeg) {
		t.Errorf("the smallest bin ID needs index %d, which the extension does not cover", neededNeg)
	}
}

func TestIndexIsRepresentableBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		index int32
		want  bool
	}{
		{lbclmm.MinBinArrayIndexWithExtension, true},
		{lbclmm.MaxBinArrayIndexWithExtension, true},
		{lbclmm.MinBinArrayIndexWithExtension - 1, false},
		{lbclmm.MaxBinArrayIndexWithExtension + 1, false},
		{math.MaxInt32, false},
		{math.MinInt32, false},
	}

	for _, tt := range tests {
		if got := IndexIsRepresentable(tt.index); got != tt.want {
			t.Errorf("IndexIsRepresentable(%d) = %v, want %v", tt.index, got, tt.want)
		}
	}
}
