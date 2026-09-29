package uint256

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

// oracleU256 builds a *big.Int for a U256 without going through the package's
// own limb logic, so a bug in the limb layout cannot cancel out against itself.
func oracleU256(n U256) *big.Int {
	b := n.BytesBE()
	return new(big.Int).SetBytes(b[:])
}

// oracleU128 builds a *big.Int for a num.U128 by an independent route.
//
// The first version hand-rolled the byte loop and wrote Lo into the high half:
// the same mistake as the production code it was meant to check, so the two
// agreed on a wrong answer and the defect stayed hidden. encoding/binary
// removes the opportunity.
func oracleU128(v num.U128) *big.Int {
	var b [16]byte
	binary.BigEndian.PutUint64(b[0:8], v.Hi)
	binary.BigEndian.PutUint64(b[8:16], v.Lo)

	return new(big.Int).SetBytes(b[:])
}

// randomIters returns how many randomized iterations a test should run.
//
// The default is intentionally small. These tests exist to pin behaviour, not
// to burn CPU: a wrong limb order or a missed carry is caught within a few
// hundred inputs, and a bug that only appears at input 4,000 is vanishingly
// unlikely to matter next to the golden on-chain fixtures used elsewhere.
//
// Raise the count deliberately when hunting a specific suspect:
//
//	DLMM_TEST_SCALE=20000 go test ./internal/uint256/
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

// shiftSamples covers the structurally interesting offsets instead of sweeping
// all 300. Every 0..255 case is still covered by the fuzz target, and the
// values here are the ones where an off-by-one in the word/bit split lives.
var shiftSamples = []uint{0, 1, 31, 32, 63, 64, 65, 100, 127, 128, 129, 191, 192, 255, 256, 257, 300}

func TestMul128To256MatchesBigInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b num.U128
	}{
		{"zero times zero", num.U128{}, num.U128{}},
		{"one times one", num.U128FromU64(1), num.U128FromU64(1)},
		{"max uint64 squared", num.U128FromU64(^uint64(0)), num.U128FromU64(^uint64(0))},
		{"max u128 squared", num.MaxU128, num.MaxU128},
		{"max u128 times one", num.MaxU128, num.U128FromU64(1)},
		{"cross term only", num.U128{Hi: 1}, num.U128{Lo: 1}},
		{"cross term only reversed", num.U128{Lo: 1}, num.U128{Hi: 1}},
		{"high limbs set", num.MaxU128, num.MaxU128},
		{"two to the 127th", num.U128{Hi: 1 << 63}, num.U128{Hi: 1 << 63}},
		{"asymmetric", num.U128{Lo: 0x0123456789abcdef, Hi: 0xfedcba9876543210}, num.U128{Lo: 0xdeadbeefcafebabe, Hi: 0x0badc0defeedface}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Mul128To256(tt.a, tt.b)
			want := new(big.Int).Mul(oracleU128(tt.a), oracleU128(tt.b))

			if oracleU256(got).Cmp(want) != 0 {
				t.Errorf("Mul128To256(%v, %v) = %s, want %s", tt.a, tt.b, got, want)
			}
		})
	}
}

func TestMul128To256IsCommutative(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(1, 2))
	for range randomIters(t) {
		a := num.U128{Lo: rng.Uint64(), Hi: rng.Uint64()}
		b := num.U128{Lo: rng.Uint64(), Hi: rng.Uint64()}

		if ab, ba := Mul128To256(a, b), Mul128To256(b, a); ab != ba {
			t.Fatalf("not commutative for %v %v: %s != %s", a, b, ab, ba)
		}
	}
}

func TestShrMatchesBigInt(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(3, 4))

	values := []U256{
		{},
		U256FromU64(1),
		U256FromU64(^uint64(0)),
		NewU256(^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)),
		NewU256(0, 0, 0, ^uint64(0)),
		NewU256(0, 0, 1, 0),
		NewU256(1, 0, 0, 0),
	}
	for range 4 {
		values = append(values, NewU256(rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Uint64()))
	}

	for _, v := range values {
		for _, shift := range shiftSamples {
			got := v.Shr(shift)
			want := new(big.Int).Rsh(oracleU256(v), shift)

			if oracleU256(got).Cmp(want) != 0 {
				t.Fatalf("Shr(%s, %d) = %s, want %s", v, shift, got, want)
			}
		}
	}
}

func TestShrByZeroIsIdentity(t *testing.T) {
	t.Parallel()

	v := NewU256(0xdeadbeef, 0xcafebabe, 0x8badf00d, 0xfeedface)
	if got := v.Shr(0); got != v {
		t.Errorf("Shr by 0 changed the value: %+v -> %+v", v, got)
	}
}

func TestToU128RejectsOverflowInsteadOfTruncating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      U256
		wantErr bool
	}{
		{"zero", U256{}, false},
		{"fits in low limb", U256FromU64(42), false},
		{"fits in two limbs", U256{L1: 7}, false},
		{"exactly max u128", U256FromU128(num.MaxU128), false},
		{"one above max u128", U256{L2: 1}, true},
		{"top limb set", U256{L3: 1}, true},
		{"all ones", NewU256(^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.in.ToU128()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ToU128(%s) = %v, want error", tt.in, got)
				}

				if !errors.Is(err, ErrOverflow) {
					t.Errorf("error = %v, want it to wrap ErrOverflow", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("ToU128(%s) error = %v", tt.in, err)
			}

			if got.Lo != tt.in.L0 || got.Hi != tt.in.L1 {
				t.Errorf("ToU128(%s) = %+v, want L0=%d L1=%d", tt.in, got, tt.in.L0, tt.in.L1)
			}
		})
	}
}

func TestStringMatchesBigInt(t *testing.T) {
	t.Parallel()

	tests := []U256{
		{},
		U256FromU64(1),
		U256FromU64(10),
		U256FromU64(^uint64(0)),
		U256FromU128(num.MaxU128),
		NewU256(^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)),
		{L0: 10_000_000_000_000_000_000},
		{L1: 1},
	}

	for _, v := range tests {
		if got, want := v.String(), oracleU256(v).String(); got != want {
			t.Errorf("String() = %s, want %s", got, want)
		}
	}
}

func TestStringHasNoLeadingZeros(t *testing.T) {
	t.Parallel()

	// A value needing more than one 19-digit chunk exercises the padding path.
	if got, want := (U256{L0: 12345}).String(), "12345"; got != want {
		t.Errorf("String() = %s, want %s", got, want)
	}
}

func TestCmpMatchesBigInt(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(5, 6))
	for range randomIters(t) {
		a := NewU256(rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Uint64())
		b := NewU256(rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Uint64())

		got, want := a.Cmp(b), oracleU256(a).Cmp(oracleU256(b))
		if got != want {
			t.Fatalf("Cmp(%s, %s) = %d, want %d", a, b, got, want)
		}

		if b.Cmp(a) != -want {
			t.Fatalf("Cmp is not antisymmetric for %s %s", a, b)
		}
	}
}

func TestMulThenShrMatchesBigInt(t *testing.T) {
	t.Parallel()

	// The DLMM hot path: (x * y) >> offset.
	rng := rand.New(rand.NewPCG(7, 8))
	for range randomIters(t) {
		x := num.U128{Lo: rng.Uint64() >> 4, Hi: rng.Uint64() >> 4}
		y := num.U128{Lo: rng.Uint64() >> 4, Hi: rng.Uint64() >> 4}
		offset := rng.UintN(256)

		got := Mul128To256(x, y).Shr(offset)

		want := new(big.Int).Mul(oracleU128(x), oracleU128(y))
		want.Rsh(want, offset)

		if oracleU256(got).Cmp(want) != 0 {
			t.Fatalf("(%s * %s) >> %d = %s, want %s", x, y, offset, got, want)
		}
	}
}

func TestMul128To256KnownFullWidthProduct(t *testing.T) {
	t.Parallel()

	// The hand-checkable case that both original defects broke.
	// (2^128-1)^2 = 2^256 - 2^129 + 1.
	want := U256{
		L0: 1,
		L1: 0,
		L2: 0xFFFFFFFFFFFFFFFE,
		L3: 0xFFFFFFFFFFFFFFFF,
	}

	if got := Mul128To256(num.MaxU128, num.MaxU128); got != want {
		t.Errorf("MaxU128*MaxU128 = %+v, want %+v", got, want)
	}
}

func TestMul128To256OneTimesOne(t *testing.T) {
	t.Parallel()

	// The smallest case that exposes a swapped (hi, lo): the product must land
	// in the low limb, not in limb 1.
	if got, want := Mul128To256(num.U128FromU64(1), num.U128FromU64(1)), (U256{L0: 1}); got != want {
		t.Errorf("1*1 = %+v, want %+v", got, want)
	}
}

func TestStringTerminatesForSmallValues(t *testing.T) {
	t.Parallel()

	// Regression: String() originally treated bits.Div64's quotient as a
	// remainder, so every value whose limbs were below 10^19 reached a fixed
	// point and the loop never terminated. Because ToU128 formats the value into
	// its overflow error, that made every u128-overflow path hang.
	tests := []struct {
		in   U256
		want string
	}{
		{U256FromU64(1), "1"},
		{U256FromU64(42), "42"},
		{U256FromU64(10_000_000_000_000_000_000), "10000000000000000000"},
	}

	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("String() = %s, want %s", got, tt.want)
		}
	}
}

func FuzzMulThenShr(f *testing.F) {
	f.Add(uint64(0), uint64(0), uint8(0))
	f.Add(uint64(1), uint64(1), uint8(0))
	f.Add(uint64(0xffffffffffffffff), uint64(0xffffffffffffffff), uint8(64))
	f.Add(uint64(0xffffffffffffffff), uint64(1), uint8(127))

	f.Fuzz(func(t *testing.T, loA, loB uint64, offset uint8) {
		a := num.U128{Lo: loA, Hi: loA ^ 0x5555555555555555}
		b := num.U128{Lo: loB, Hi: loB ^ 0xaaaaaaaaaaaaaaaa}

		got := Mul128To256(a, b).Shr(uint(offset))

		want := new(big.Int).Mul(oracleU128(a), oracleU128(b))
		want.Rsh(want, uint(offset))

		if oracleU256(got).Cmp(want) != 0 {
			t.Fatalf("(%s * %s) >> %d = %s, want %s", a, b, offset, got, want)
		}
	})
}
