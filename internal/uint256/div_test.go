package uint256

import (
	"errors"
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/pwnholic/dlmm-go/num"
)

func TestDiv256By128KnownValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		n            U256
		d            num.U128
		wantQ, wantR num.U128
		wantErr      error
	}{
		{"zero over one", U256{}, num.U128FromU64(1), num.U128{}, num.U128{}, nil},
		{"one over one", U256FromU64(1), num.U128FromU64(1), num.U128FromU64(1), num.U128{}, nil},
		{"seven over two", U256FromU64(7), num.U128FromU64(2), num.U128FromU64(3), num.U128FromU64(1), nil},
		{"exact power of two", U256FromU64(8), num.U128FromU64(4), num.U128FromU64(2), num.U128{}, nil},
		{
			"dividend below divisor",
			U256FromU64(3), num.U128FromU64(10),
			num.U128{},
			num.U128FromU64(3), nil,
		},
		{
			// (2^256-1) / (2^128-1) == 2^128 + 1, which does not fit in 128
			// bits, so this must error rather than wrap.
			"quotient exceeds 128 bits",
			NewU256(^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)),
			num.MaxU128,
			num.U128{},
			num.U128{},
			ErrOverflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q, r, err := Div256By128(tt.n, tt.d)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Div256By128(%s, %s) error = %v, want %v", tt.n, tt.d, err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("Div256By128(%s, %s) error = %v", tt.n, tt.d, err)
			}

			if q != tt.wantQ || r != tt.wantR {
				t.Errorf("Div256By128(%s, %s) = %v r %v, want %v r %v", tt.n, tt.d, q, r, tt.wantQ, tt.wantR)
			}
		})
	}
}

func TestDiv256By128RejectsZeroDivisor(t *testing.T) {
	t.Parallel()

	if _, _, err := Div256By128(U256FromU64(1), num.U128{}); !errors.Is(err, ErrDivideByZero) {
		t.Fatalf("error = %v, want ErrDivideByZero", err)
	}

	if _, err := DivCeil256By128(U256FromU64(1), num.U128{}); !errors.Is(err, ErrDivideByZero) {
		t.Fatalf("DivCeil error = %v, want ErrDivideByZero", err)
	}
}

func TestDiv256By128MatchesBigInt(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(11, 12))
	for range randomIters(t) {
		n := NewU256(rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Uint64())

		d := num.U128{Lo: rng.Uint64(), Hi: rng.Uint64()}
		if d.IsZero() {
			continue
		}

		q, r, err := Div256By128(n, d)
		if err != nil {
			// The quotient does not fit in 128 bits. Confirm that with big.Int
			// rather than assuming the error is legitimate.
			wantQ := new(big.Int).Quo(oracleU256(n), oracleU128(d))
			if wantQ.BitLen() <= 128 {
				t.Fatalf("Div256By128 reported overflow for %s / %s but quotient is %s", n, d, wantQ)
			}

			if !errors.Is(err, ErrOverflow) {
				t.Fatalf("error = %v, want ErrOverflow", err)
			}

			continue
		}

		wantQ := new(big.Int).Quo(oracleU256(n), oracleU128(d))
		wantR := new(big.Int).Rem(oracleU256(n), oracleU128(d))

		if got := oracleU128(q); got.Cmp(wantQ) != 0 {
			t.Fatalf("quotient for %s / %s = %s, want %s", n, d, got, wantQ)
		}

		if got := oracleU128(r); got.Cmp(wantR) != 0 {
			t.Fatalf("remainder for %s / %s = %s, want %s", n, d, got, wantR)
		}
	}
}

func TestDiv256By128SatisfiesEuclideanIdentity(t *testing.T) {
	t.Parallel()

	// n == q*d + r and r < d. This needs no oracle at all, so it stays valid
	// even if the big.Int comparison and the implementation share a
	// misunderstanding about limb order.
	rng := rand.New(rand.NewPCG(13, 14))
	for range randomIters(t) {
		n := NewU256(rng.Uint64()>>16, rng.Uint64(), rng.Uint64(), rng.Uint64())

		d := num.U128{Lo: rng.Uint64(), Hi: rng.Uint64() >> 16}
		if d.IsZero() {
			continue
		}

		q, r, err := Div256By128(n, d)
		if err != nil {
			continue // genuine overflow; covered above
		}

		// Rebuild q*d + r in big.Int without reusing the code under test.
		lhs := new(big.Int).Add(
			new(big.Int).Mul(oracleU128(q), oracleU128(d)),
			oracleU128(r),
		)
		if lhs.Cmp(oracleU256(n)) != 0 {
			t.Fatalf("q*d + r = %s, want %s (n=%s d=%s q=%s r=%s)", lhs, oracleU256(n), n, d, q, r)
		}

		if oracleU128(r).Cmp(oracleU128(d)) >= 0 {
			t.Fatalf("remainder %s is not less than divisor %s", r, d)
		}
	}
}

func TestDivCeil256By128IsCeilingOfQuotient(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(15, 16))
	for range randomIters(t) {
		n := NewU256(0, 0, rng.Uint64(), rng.Uint64())

		d := num.U128{Lo: rng.Uint64(), Hi: rng.Uint64() >> 32}
		if d.IsZero() {
			continue
		}

		got, err := DivCeil256By128(n, d)
		if err != nil {
			continue
		}

		// ceil(a/b) with big.Int: (a + b - 1) / b
		nBig := oracleU256(n)
		dBig := oracleU128(d)
		want := new(big.Int).Quo(new(big.Int).Add(nBig, new(big.Int).Sub(dBig, big.NewInt(1))), dBig)

		if oracleU128(got).Cmp(want) != 0 {
			t.Fatalf("DivCeil(%s, %s) = %s, want %s", n, d, got, want)
		}
	}
}

func TestDivCeilMatchesDivWhenExact(t *testing.T) {
	t.Parallel()

	// When the division is exact, ceiling must equal truncation.
	tests := []struct {
		n U256
		d num.U128
	}{
		{U256FromU64(100), num.U128FromU64(10)},
		{U256FromU64(0), num.U128FromU64(7)},
		{U256FromU128(num.MaxU128), num.U128FromU64(1)},
	}

	for _, tt := range tests {
		q, r, err := Div256By128(tt.n, tt.d)
		if err != nil {
			t.Fatalf("Div error: %v", err)
		}

		got, err := DivCeil256By128(tt.n, tt.d)
		if err != nil {
			t.Fatalf("DivCeil error: %v", err)
		}

		if !r.IsZero() {
			t.Fatalf("test expects exact division but remainder is %s", r)
		}

		if got != q {
			t.Errorf("DivCeil(%s, %s) = %s, want %s (exact division)", tt.n, tt.d, got, q)
		}
	}
}

func TestAdd1CarriesAcrossAllLimbs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   U256
		want U256
	}{
		{"zero", U256{}, U256{L0: 1}},
		{"low limb overflow carries", U256{L0: ^uint64(0)}, U256{L1: 1}},
		{"carries to third limb", U256{L0: ^uint64(0), L1: ^uint64(0)}, U256{L2: 1}},
		{"carries to top limb", U256{L0: ^uint64(0), L1: ^uint64(0), L2: ^uint64(0)}, U256{L3: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.in.Add1(); got != tt.want {
				t.Errorf("Add1(%+v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestHasBitsBelow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		in    U256
		shift uint
		want  bool
	}{
		{"zero shift always false", U256{L0: ^uint64(0)}, 0, false},
		{"zero value", U256{}, 64, false},
		{"low bit inside window", U256{L0: 1}, 8, true},
		{"bit above window", U256{L0: 1 << 8}, 8, false},
		{"bit exactly at window edge", U256{L0: 1 << 7}, 8, true},
		{"whole low limb counted", U256{L0: 1, L1: 0}, 64, true},
		{"second limb inside window", U256{L1: 1}, 128, true},
		{"bit in masked part of second limb", U256{L1: 1}, 65, true},
		{"bit above masked part of second limb", U256{L1: 1 << 1}, 65, false},
		{"shift covering everything", U256{L3: 1}, 256, true},
		{"shift beyond width with zero value", U256{}, 300, false},
		// Regression: the masked-limb switch originally stopped at limb 2, so a
		// shift in 193..255 never inspected L3 at all.
		{"bit in low part of top limb is below the window", U256{L3: 1}, 194, true},
		{"bit at the edge of the top-limb window", U256{L3: 1}, 193, true},
		{"bit above the top-limb window", U256{L3: 1}, 192, false},
		{"top limb low bit below a wide shift", U256{L3: 1}, 255, true},
		{"only the top bit of the top limb is above the window", U256{L3: 1 << 63}, 193, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.in.HasBitsBelow(tt.shift); got != tt.want {
				t.Errorf("HasBitsBelow(%+v, %d) = %v, want %v", tt.in, tt.shift, got, tt.want)
			}
		})
	}
}

func TestShlIsInverseOfShrOnNonOverlappingInput(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(17, 18))
	for range randomIters(t) {
		// Keep the value small enough that no bits are pushed off the top.
		v := NewU256(0, 0, rng.Uint64()>>40, rng.Uint64())
		shift := rng.UintN(40) + 1

		roundTrip := v.Shl(shift).Shr(shift)
		if roundTrip != v {
			t.Fatalf("Shr(Shl(%s, %d), %d) = %s, want %s", v, shift, shift, roundTrip, v)
		}
	}
}
