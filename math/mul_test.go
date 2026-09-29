package math

import (
	"encoding/binary"
	"errors"
	"math/big"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"

	"github.com/pwnholic/dlmm-go/num"
)

// oracleU128 converts a num.U128 to *big.Int without using package num's own
// conversion, so a limb-order mistake cannot cancel itself out.
//
// The first version hand-rolled the byte loop and wrote Lo into the high half --
// the same mistake as the production code it was meant to check, so the two
// agreed on a wrong answer and the defect stayed hidden behind a green test.
func oracleU128(v num.U128) *big.Int {
	var b [16]byte
	binary.BigEndian.PutUint64(b[0:8], v.Hi)
	binary.BigEndian.PutUint64(b[8:16], v.Lo)

	return new(big.Int).SetBytes(b[:])
}

// randomIters returns how many randomized iterations a test should run.
//
// The default is deliberately small: these tests pin behaviour, and a carry or
// limb-order bug shows up within a few hundred inputs. Raise it explicitly when
// chasing a specific suspect:
//
//	DLMM_TEST_SCALE=20000 go test ./math/
func randomIters(tb testing.TB) int {
	tb.Helper()

	if testing.Short() {
		return 50
	}

	if raw := os.Getenv("DLMM_TEST_SCALE"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}

	return 200
}

// interestingU128s returns the boundary values worth testing explicitly.
func interestingU128s() []num.U128 {
	return []num.U128{
		{},
		num.U128FromU64(1),
		num.U128FromU64(2),
		num.U128FromU64(10),
		num.U128FromU64(1_000_000),
		{Hi: 1},
		{Lo: 1, Hi: 1},
		{Hi: 1 << 63},
		num.MaxU128,
	}
}

func TestMulDivMatchesBigInt(t *testing.T) {
	t.Parallel()

	values := interestingU128s()

	for _, x := range values {
		for _, y := range values {
			for _, d := range []num.U128{num.U128FromU64(1), num.U128FromU64(3), num.U128FromU64(10), num.MaxU128} {
				for _, r := range []Rounding{RoundingDown, RoundingUp} {
					assertMulDivMatchesBigInt(t, x, y, d, r)
				}
			}
		}
	}
}

func assertMulDivMatchesBigInt(t *testing.T, x, y, d num.U128, r Rounding) {
	t.Helper()

	got, err := MulDiv(x, y, d, r)

	prod := new(big.Int).Mul(oracleU128(x), oracleU128(y))
	divisor := oracleU128(d)

	var want *big.Int

	if r == RoundingUp {
		one := big.NewInt(1)
		num1 := new(big.Int).Sub(divisor, one)
		want = new(big.Int).Quo(new(big.Int).Add(prod, num1), divisor)
	} else {
		want = new(big.Int).Quo(prod, divisor)
	}

	if want.BitLen() > 128 {
		if err == nil {
			t.Fatalf("MulDiv(%v,%v,%v,%v) = %v, want overflow error (want %s)", x, y, d, r, got, want)
		}

		if !errors.Is(err, ErrOverflow) {
			t.Fatalf("error = %v, want ErrOverflow", err)
		}

		return
	}

	if err != nil {
		t.Fatalf("MulDiv(%v,%v,%v,%v) error = %v, want %s", x, y, d, r, err, want)
	}

	if oracleU128(got).Cmp(want) != 0 {
		t.Fatalf("MulDiv(%v,%v,%v,%v) = %v, want %s", x, y, d, r, got, want)
	}
}

func TestMulShrMatchesBigInt(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(21, 22))

	for _, x := range interestingU128s() {
		for _, offset := range []uint8{0, 1, 7, 32, 63, 64, 100, 127} {
			assertMulShrMatchesBigInt(t, x, num.U128FromU64(12345), offset, RoundingDown)
			assertMulShrMatchesBigInt(t, x, num.U128FromU64(12345), offset, RoundingUp)
		}
	}

	for range randomIters(t) {
		x := num.U128{Lo: rng.Uint64() >> 8, Hi: rng.Uint64() >> 8}
		y := num.U128{Lo: rng.Uint64() >> 8, Hi: rng.Uint64() >> 8}
		offset := uint8(rng.UintN(128))

		assertMulShrMatchesBigInt(t, x, y, offset, RoundingDown)
		assertMulShrMatchesBigInt(t, x, y, offset, RoundingUp)
	}
}

func assertMulShrMatchesBigInt(t *testing.T, x, y num.U128, offset uint8, r Rounding) {
	t.Helper()

	got, err := MulShr(x, y, offset, r)

	prod := new(big.Int).Mul(oracleU128(x), oracleU128(y))
	want := new(big.Int).Rsh(prod, uint(offset))

	if r == RoundingUp {
		// Add one when any discarded bit was set.
		mask := new(big.Int).Lsh(big.NewInt(1), uint(offset))

		rem := new(big.Int).Rem(prod, mask)
		if rem.Sign() != 0 {
			want = new(big.Int).Add(want, big.NewInt(1))
		}
	}

	if want.BitLen() > 128 {
		if err == nil {
			t.Fatalf("MulShr(%v,%v,%d,%v) = %v, want overflow error", x, y, offset, r, got)
		}

		return
	}

	if err != nil {
		t.Fatalf("MulShr(%v,%v,%d,%v) error = %v, want %s", x, y, offset, r, err, want)
	}

	if oracleU128(got).Cmp(want) != 0 {
		t.Fatalf("MulShr(%v,%v,%d,%v) = %v, want %s", x, y, offset, r, got, want)
	}
}

func TestShlDivMatchesBigInt(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(23, 24))

	for _, y := range []num.U128{num.U128FromU64(1), num.U128FromU64(3), num.U128FromU64(10), num.MaxU128} {
		for _, offset := range []uint8{0, 1, 32, 64, 100, 127} {
			assertShlDivMatchesBigInt(t, num.U128FromU64(1_000_000), y, offset, RoundingDown)
			assertShlDivMatchesBigInt(t, num.U128FromU64(1_000_000), y, offset, RoundingUp)
		}
	}

	for range randomIters(t) {
		x := num.U128{Lo: rng.Uint64() >> 16, Hi: rng.Uint64() >> 16}

		y := num.U128{Lo: rng.Uint64(), Hi: rng.Uint64() >> 4}
		if y.IsZero() {
			continue
		}

		offset := uint8(rng.UintN(128))

		assertShlDivMatchesBigInt(t, x, y, offset, RoundingDown)
		assertShlDivMatchesBigInt(t, x, y, offset, RoundingUp)
	}
}

func assertShlDivMatchesBigInt(t *testing.T, x, y num.U128, offset uint8, r Rounding) {
	t.Helper()

	got, err := ShlDiv(x, y, offset, r)

	numerator := new(big.Int).Lsh(oracleU128(x), uint(offset))
	divisor := oracleU128(y)

	var want *big.Int

	if r == RoundingUp {
		one := big.NewInt(1)
		want = new(big.Int).Quo(new(big.Int).Add(numerator, new(big.Int).Sub(divisor, one)), divisor)
	} else {
		want = new(big.Int).Quo(numerator, divisor)
	}

	if want.BitLen() > 128 {
		if err == nil {
			t.Fatalf("ShlDiv(%v,%v,%d,%v) = %v, want overflow error", x, y, offset, r, got)
		}

		return
	}

	if err != nil {
		t.Fatalf("ShlDiv(%v,%v,%d,%v) error = %v, want %s", x, y, offset, r, err, want)
	}

	if oracleU128(got).Cmp(want) != 0 {
		t.Fatalf("ShlDiv(%v,%v,%d,%v) = %v, want %s", x, y, offset, r, got, want)
	}
}

func TestMulShrAgreesWithMulDivOnPowerOfTwo(t *testing.T) {
	t.Parallel()

	// MulShr takes the shift path; MulDiv takes the general division path.
	// They must agree, and they share no code below the 256-bit product, so
	// agreement is real evidence rather than a tautology.
	rng := rand.New(rand.NewPCG(25, 26))

	for range randomIters(t) {
		x := num.U128{Lo: rng.Uint64() >> 8, Hi: rng.Uint64() >> 8}
		y := num.U128{Lo: rng.Uint64() >> 8, Hi: rng.Uint64() >> 8}
		offset := uint8(rng.UintN(128))

		for _, r := range []Rounding{RoundingDown, RoundingUp} {
			viaShift, errShift := MulShr(x, y, offset, r)

			denominator := pow2U128(offset)
			viaDiv, errDiv := MulDiv(x, y, denominator, r)

			if (errShift == nil) != (errDiv == nil) {
				t.Fatalf("error disagreement at x=%v y=%v offset=%d r=%v: shift=%v div=%v",
					x, y, offset, r, errShift, errDiv)
			}

			if errShift != nil {
				continue
			}

			if viaShift != viaDiv {
				t.Fatalf("MulShr=%v but MulDiv(2^%d)=%v for x=%v y=%v r=%v",
					viaShift, offset, viaDiv, x, y, r)
			}
		}
	}
}

// pow2U128 returns 2^offset as a num.U128 for offset in [0, 127].
//
// Taking offset%64 directly into the low limb is wrong at offset == 64: that
// computes 2^0 in Lo instead of 2^64 in Hi.
func pow2U128(offset uint8) num.U128 {
	if offset < 64 {
		return num.U128{Lo: uint64(1) << uint(offset)}
	}

	return num.U128{Hi: uint64(1) << uint(offset-64)}
}

func TestPow2U128CoversTheLimbBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		offset uint8
		want   num.U128
	}{
		{0, num.U128{Lo: 1}},
		{1, num.U128{Lo: 2}},
		{63, num.U128{Lo: uint64(1) << 63}},
		{64, num.U128{Hi: 1}},
		{65, num.U128{Hi: 2}},
		{127, num.U128{Hi: uint64(1) << 63}},
	}

	for _, tt := range tests {
		if got := pow2U128(tt.offset); got != tt.want {
			t.Errorf("pow2U128(%d) = %+v, want %+v", tt.offset, got, tt.want)
		}
	}
}

func TestMulDivRejectsZeroDenominator(t *testing.T) {
	t.Parallel()

	if _, err := MulDiv(num.U128FromU64(1), num.U128FromU64(1), num.U128{}, RoundingDown); !errors.Is(err, ErrDivideByZero) {
		t.Fatalf("error = %v, want ErrDivideByZero", err)
	}
}

func TestShlDivRejectsZeroDivisor(t *testing.T) {
	t.Parallel()

	if _, err := ShlDiv(num.U128FromU64(1), num.U128{}, 10, RoundingDown); !errors.Is(err, ErrDivideByZero) {
		t.Fatalf("error = %v, want ErrDivideByZero", err)
	}
}

func TestOffsetBounds(t *testing.T) {
	t.Parallel()

	// The Rust reference multiplies by 1u128.checked_shl(offset), which is None
	// at 128 or above. Both shift-based entry points must reject that.
	for _, offset := range []uint8{128, 129, 200, 255} {
		if _, err := MulShr(num.U128FromU64(1), num.U128FromU64(1), offset, RoundingDown); !errors.Is(err, ErrOffsetTooLarge) {
			t.Errorf("MulShr offset %d: error = %v, want ErrOffsetTooLarge", offset, err)
		}

		if _, err := ShlDiv(num.U128FromU64(1), num.U128FromU64(1), offset, RoundingDown); !errors.Is(err, ErrOffsetTooLarge) {
			t.Errorf("ShlDiv offset %d: error = %v, want ErrOffsetTooLarge", offset, err)
		}
	}

	// 127 is the largest accepted offset.
	if _, err := MulShr(num.U128FromU64(1), num.U128FromU64(1), 127, RoundingDown); err != nil {
		t.Errorf("MulShr offset 127 returned error: %v", err)
	}
}

func TestInvalidRoundingIsRejected(t *testing.T) {
	t.Parallel()

	// The zero value of Rounding is RoundingInvalid, so an uninitialised field
	// must fail rather than silently rounding down.
	var zero Rounding

	if _, err := MulDiv(num.U128FromU64(1), num.U128FromU64(1), num.U128FromU64(1), zero); !errors.Is(err, ErrInvalidRounding) {
		t.Errorf("MulDiv: error = %v, want ErrInvalidRounding", err)
	}

	if _, err := MulShr(num.U128FromU64(1), num.U128FromU64(1), 0, zero); !errors.Is(err, ErrInvalidRounding) {
		t.Errorf("MulShr: error = %v, want ErrInvalidRounding", err)
	}

	if _, err := ShlDiv(num.U128FromU64(1), num.U128FromU64(1), 0, zero); !errors.Is(err, ErrInvalidRounding) {
		t.Errorf("ShlDiv: error = %v, want ErrInvalidRounding", err)
	}
}

func TestRoundingUpIsNeverLessThanRoundingDown(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(27, 28))
	for range randomIters(t) {
		x := num.U128{Lo: rng.Uint64() >> 8, Hi: rng.Uint64() >> 8}
		y := num.U128{Lo: rng.Uint64() >> 8, Hi: rng.Uint64() >> 8}
		offset := uint8(rng.UintN(64))

		down, errDown := MulShr(x, y, offset, RoundingDown)
		if errDown != nil {
			continue
		}

		up, errUp := MulShr(x, y, offset, RoundingUp)
		if errUp != nil {
			// Rounding up may overflow where rounding down does not.
			continue
		}

		diff := up.Cmp(down)
		if diff < 0 {
			t.Fatalf("round up %v < round down %v", up, down)
		}

		if diff > 1 {
			t.Fatalf("rounding changed result by more than one: %v vs %v", up, down)
		}
	}
}

func TestRoundingModeString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   Rounding
		want string
	}{
		{RoundingInvalid, "invalid"},
		{RoundingDown, "down"},
		{RoundingUp, "up"},
		{Rounding(200), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("Rounding(%d).String() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func FuzzMulShr(f *testing.F) {
	f.Add(uint64(0), uint64(0), uint64(0), uint8(0), uint8(1))
	f.Add(uint64(1000), uint64(7), uint64(3), uint8(64), uint8(1))
	f.Add(^uint64(0), ^uint64(0), ^uint64(0), uint8(127), uint8(2))

	f.Fuzz(func(t *testing.T, loX, hiX, loY uint64, offset, mode uint8) {
		if offset > MaxShiftOffset {
			return
		}

		r := Rounding(mode%2 + 1) // 1 and 2 are RoundingDown and RoundingUp

		x := num.U128{Lo: loX, Hi: hiX}
		y := num.U128{Lo: loY, Hi: loY ^ 0x9e3779b97f4a7c15}

		got, err := MulShr(x, y, offset, r)

		prod := new(big.Int).Mul(oracleU128(x), oracleU128(y))

		want := new(big.Int).Rsh(prod, uint(offset))
		if r == RoundingUp {
			mask := new(big.Int).Lsh(big.NewInt(1), uint(offset))
			if new(big.Int).Rem(prod, mask).Sign() != 0 {
				want = new(big.Int).Add(want, big.NewInt(1))
			}
		}

		if want.BitLen() > 128 {
			if err == nil {
				t.Fatalf("MulShr(%v,%v,%d,%v) = %v, want overflow error", x, y, offset, r, got)
			}

			return
		}

		if err != nil {
			t.Fatalf("MulShr(%v,%v,%d,%v) error = %v, want %s", x, y, offset, r, err, want)
		}

		if oracleU128(got).Cmp(want) != 0 {
			t.Fatalf("MulShr(%v,%v,%d,%v) = %v, want %s", x, y, offset, r, got, want)
		}
	})
}
