package num_test

// Golden-vector tests for package num's limb layout and for internal/uint256.
//
// The expected values come from tools/gen_vectors/gen_numeric_vectors.py, which
// computes them in Python with arbitrary-precision integers: a different
// language, a different algorithm, and no shared code with the implementation
// under test. An oracle written in Go, even one built on math/big, would have
// been free to repeat whatever misunderstanding of the limb layout or of
// rounding the implementation already has.
//
// The vectors are committed under testdata/ rather than regenerated during the
// test run, so a changed expectation shows up as a reviewable diff instead of a
// quietly greener test run. Regenerate with:
//
//	python3 tools/gen_vectors/gen_numeric_vectors.py
//
// This file is in package num_test rather than package num because
// internal/uint256 imports num: an internal test file that imported a package
// depending on the package under test would be an import cycle.
//
// Each case names the operation it targets in its "op" field; the tests below
// dispatch on it. math/vectors_test.go covers the MulDiv/MulShr/ShlDiv entries
// of the same file, and this file still walks them to check the limb encoding.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/pwnholic/dlmm-go/internal/uint256"
	"github.com/pwnholic/dlmm-go/num"
)

// vectorFile is relative to this package's directory, which is the working
// directory of the test binary.
const vectorFile = "../testdata/vectors/numeric.json"

// Op names this file dispatches on. They are the generator's "op" values.
const (
	opU128Limbs       = "u128_limbs"
	opU256Limbs       = "u256_limbs"
	opMul128To256     = "mul128_to256"
	opU256Shr         = "u256_shr"
	opU256Shl         = "u256_shl"
	opDiv256By128     = "div256_by128"
	opDivCeil256By128 = "divceil256_by128"
)

// jsonInt is a u128 or a u256 exactly as the generator emits it: a decimal
// string plus trailing limbs, each limb also a decimal string. Limbs are strings
// so that no value ever has to survive a float64 round trip in a JSON parser.
//
// The u128 fields (lo/hi) and the u256 fields (l0..l3) share one type because a
// case fills in only the limbs its op calls for; the op decides which are there.
type jsonInt struct {
	Dec string  `json:"dec"`
	Lo  *string `json:"lo"`
	Hi  *string `json:"hi"`
	L0  *string `json:"l0"`
	L1  *string `json:"l1"`
	L2  *string `json:"l2"`
	L3  *string `json:"l3"`
}

func (j *jsonInt) isU256() bool { return j.L3 != nil }

// vectorCase mirrors one case object. Every field is a pointer because the
// generator omits whatever the op does not use, and an omitted field must be
// distinguishable from a zero value.
type vectorCase struct {
	Op          string   `json:"op"`
	X           *jsonInt `json:"x"`
	Y           *jsonInt `json:"y"`
	Denominator *jsonInt `json:"denominator"`
	D           *jsonInt `json:"d"`
	N           *jsonInt `json:"n"`
	Rounding    string   `json:"rounding"`
	Offset      *int     `json:"offset"`
	Shift       *int     `json:"shift"`
	Want        *jsonInt `json:"want"`
	WantRem     *jsonInt `json:"want_rem"`
	WantError   *string  `json:"want_error"`
}

type vectorFileJSON struct {
	GeneratedBy string       `json:"generated_by"`
	Seed        int64        `json:"seed"`
	Cases       []vectorCase `json:"cases"`
}

var (
	vectorsOnce sync.Once
	vectorsData vectorFileJSON
	errVectors  error
)

// loadVectors reads the generated file once per test binary.
func loadVectors(t *testing.T) vectorFileJSON {
	t.Helper()

	vectorsOnce.Do(func() {
		raw, err := os.ReadFile(vectorFile)
		if err != nil {
			errVectors = fmt.Errorf("reading: %w", err)
			return
		}

		if err := json.Unmarshal(raw, &vectorsData); err != nil {
			errVectors = fmt.Errorf("parsing: %w", err)
			return
		}

		if len(vectorsData.Cases) == 0 {
			errVectors = errors.New("no cases")
		}
	})

	if errVectors != nil {
		t.Fatalf("%s: %v (regenerate with python3 tools/gen_vectors/gen_numeric_vectors.py)", vectorFile, errVectors)
	}

	return vectorsData
}

// eachVector runs fn for every case targeting op and returns how many it ran.
// The file index is passed through so a failure points at the exact JSON line.
func eachVector(t *testing.T, op string, fn func(index int, c vectorCase)) int {
	t.Helper()

	count := 0

	for index, c := range loadVectors(t).Cases {
		if c.Op != op {
			continue
		}

		fn(index, c)

		count++
	}

	return count
}

// vectorU64 parses a limb emitted as a decimal string.
func vectorU64(t *testing.T, limb *string, ctx string) uint64 {
	t.Helper()

	if limb == nil {
		t.Fatalf("%s: missing limb", ctx)
	}

	value, err := strconv.ParseUint(*limb, 10, 64)
	if err != nil {
		t.Fatalf("%s: limb %q is not a uint64: %v", ctx, *limb, err)
	}

	return value
}

// vectorDec parses the decimal form with math/big, which shares no code with num
// or uint256.
func vectorDec(t *testing.T, j *jsonInt, ctx string) *big.Int {
	t.Helper()

	value, ok := new(big.Int).SetString(j.Dec, 10)
	if !ok {
		t.Fatalf("%s: %q is not a decimal integer", ctx, j.Dec)
	}

	return value
}

// vectorU128 builds the value from the limbs only.
func vectorU128(t *testing.T, j *jsonInt, ctx string) num.U128 {
	t.Helper()

	if j == nil {
		t.Fatalf("%s: missing value", ctx)
	}

	return num.U128{
		Lo: vectorU64(t, j.Lo, ctx+".lo"),
		Hi: vectorU64(t, j.Hi, ctx+".hi"),
	}
}

// vectorU256 builds the value from the limbs only.
func vectorU256(t *testing.T, j *jsonInt, ctx string) uint256.U256 {
	t.Helper()

	if j == nil {
		t.Fatalf("%s: missing value", ctx)
	}

	return uint256.U256{
		L0: vectorU64(t, j.L0, ctx+".l0"),
		L1: vectorU64(t, j.L1, ctx+".l1"),
		L2: vectorU64(t, j.L2, ctx+".l2"),
		L3: vectorU64(t, j.L3, ctx+".l3"),
	}
}

// u128Big combines a u128's limbs the way its documentation says the fields go:
// Lo is the low 64 bits. It does not call into num, so it can catch a transposed
// layout instead of agreeing with it.
func u128Big(v num.U128) *big.Int {
	return new(big.Int).Or(
		new(big.Int).Lsh(new(big.Int).SetUint64(v.Hi), 64),
		new(big.Int).SetUint64(v.Lo),
	)
}

// u256Big combines a u256's limbs, most significant first, without going through
// uint256.String.
func u256Big(v uint256.U256) *big.Int {
	out := new(big.Int)
	for _, limb := range [...]uint64{v.L3, v.L2, v.L1, v.L0} {
		out.Lsh(out, 64)
		out.Or(out, new(big.Int).SetUint64(limb))
	}

	return out
}

// vectorInts returns every integer field present in a case, labelled. A case
// carries only the fields its op uses, so the limb checks below do not have to
// repeat the field list per op.
func vectorInts(c vectorCase) []struct {
	Name  string
	Value *jsonInt
} {
	fields := []struct {
		Name  string
		Value *jsonInt
	}{
		{"x", c.X},
		{"y", c.Y},
		{"denominator", c.Denominator},
		{"d", c.D},
		{"n", c.N},
		{"want", c.Want},
		{"want_rem", c.WantRem},
	}

	present := make([]struct {
		Name  string
		Value *jsonInt
	}, 0, len(fields))
	for _, field := range fields {
		if field.Value != nil {
			present = append(present, field)
		}
	}

	return present
}

// TestVectorLimbEncodingIsUnambiguous checks that the decimal form and the limb
// form of every integer in the file describe the same number.
//
// This is the check that makes the rest of the vectors meaningful. Every
// assertion below builds its operands from limbs, so if the generator and the Go
// code agreed on a transposed limb order — lo holding the high bits — they would
// agree on all of them and the whole oracle would be worthless.
func TestVectorLimbEncodingIsUnambiguous(t *testing.T) {
	t.Parallel()

	checked := 0

	for index, c := range loadVectors(t).Cases {
		for _, field := range vectorInts(c) {
			ctx := fmt.Sprintf("case %d (%s) field %s", index, c.Op, field.Name)

			var fromLimbs *big.Int
			if field.Value.isU256() {
				fromLimbs = u256Big(vectorU256(t, field.Value, ctx))
			} else {
				fromLimbs = u128Big(vectorU128(t, field.Value, ctx))
			}

			if want := vectorDec(t, field.Value, ctx); fromLimbs.Cmp(want) != 0 {
				t.Errorf("%s: limbs say %s, decimal says %s", ctx, fromLimbs, want)
			}

			checked++
		}
	}

	if checked == 0 {
		t.Fatal("no integer fields found in the vector file")
	}
}

// TestU128VectorsRoundTripThroughBigInt exercises package num's conversions
// against the vectors: the file supplies both the decimal value and the limbs, so
// U128FromBigInt and BigInt are each checked against Python rather than against
// each other.
func TestU128VectorsRoundTripThroughBigInt(t *testing.T) {
	t.Parallel()

	for index, c := range loadVectors(t).Cases {
		for _, field := range vectorInts(c) {
			if field.Value.isU256() {
				// The 256-bit limb layout belongs to internal/uint256.
				continue
			}

			ctx := fmt.Sprintf("case %d (%s) field %s", index, c.Op, field.Name)
			value := vectorU128(t, field.Value, ctx)
			want := vectorDec(t, field.Value, ctx)

			if got := value.BigInt(); got.Cmp(want) != 0 {
				t.Errorf("%s: U128{%d,%d}.BigInt() = %s, want %s", ctx, value.Lo, value.Hi, got, want)
			}

			if got := value.String(); got != field.Value.Dec {
				t.Errorf("%s: U128.String() = %s, want %s", ctx, got, field.Value.Dec)
			}

			decoded, err := num.U128FromBigInt(want)
			if err != nil {
				t.Fatalf("%s: U128FromBigInt(%s) error = %v", ctx, want, err)
			}

			if decoded != value {
				t.Errorf("%s: U128FromBigInt(%s) = %+v, want %+v", ctx, want, decoded, value)
			}
		}
	}
}

// TestU256LimbsVectors checks the 256-bit limb order against the decimal form,
// using u256Big rather than uint256's own conversions.
func TestU256LimbsVectors(t *testing.T) {
	t.Parallel()

	count := eachVector(t, opU256Limbs, func(index int, c vectorCase) {
		ctx := fmt.Sprintf("case %d (%s)", index, c.Op)
		value := vectorU256(t, c.N, ctx)

		if got, want := u256Big(value), vectorDec(t, c.N, ctx); got.Cmp(want) != 0 {
			t.Errorf("%s: limbs say %s, decimal says %s", ctx, got, want)
		}

		if got, want := value.String(), c.N.Dec; got != want {
			t.Errorf("%s: U256.String() = %s, want %s", ctx, got, want)
		}
	})

	if count == 0 {
		t.Fatal("no u256_limbs vectors")
	}
}

// TestMul128To256Vectors checks the exact 128x128 -> 256 widening product.
func TestMul128To256Vectors(t *testing.T) {
	t.Parallel()

	count := eachVector(t, opMul128To256, func(index int, c vectorCase) {
		ctx := fmt.Sprintf("case %d (%s)", index, c.Op)

		x := vectorU128(t, c.X, ctx+".x")
		y := vectorU128(t, c.Y, ctx+".y")
		got := uint256.Mul128To256(x, y)

		if want := vectorU256(t, c.Want, ctx+".want"); got != want {
			t.Errorf("%s: Mul128To256(%s, %s) = %+v, want %+v", ctx, x, y, got, want)
			return
		}

		if want := vectorDec(t, c.Want, ctx+".want"); u256Big(got).Cmp(want) != 0 {
			t.Errorf("%s: Mul128To256(%s, %s) = %s, want %s", ctx, x, y, u256Big(got), want)
		}
	})

	if count == 0 {
		t.Fatal("no mul128_to256 vectors")
	}
}

// TestU256ShrVectors checks the truncating right shift, including the documented
// rule that a shift of 256 or more yields zero.
func TestU256ShrVectors(t *testing.T) {
	t.Parallel()

	count := eachVector(t, opU256Shr, func(index int, c vectorCase) {
		ctx := fmt.Sprintf("case %d (%s)", index, c.Op)
		if c.Shift == nil {
			t.Fatalf("%s: missing shift", ctx)
		}

		in := vectorU256(t, c.N, ctx+".n")
		got := in.Shr(uint(*c.Shift))

		if want := vectorU256(t, c.Want, ctx+".want"); got != want {
			t.Errorf("%s: %s >> %d = %+v, want %+v", ctx, in, *c.Shift, got, want)
			return
		}

		if want := vectorDec(t, c.Want, ctx+".want"); u256Big(got).Cmp(want) != 0 {
			t.Errorf("%s: %s >> %d = %s, want %s", ctx, in, *c.Shift, u256Big(got), want)
		}
	})

	if count == 0 {
		t.Fatal("no u256_shr vectors")
	}
}

// TestU256ShlVectors checks the truncating left shift.
func TestU256ShlVectors(t *testing.T) {
	t.Parallel()

	count := eachVector(t, opU256Shl, func(index int, c vectorCase) {
		ctx := fmt.Sprintf("case %d (%s)", index, c.Op)
		if c.Shift == nil {
			t.Fatalf("%s: missing shift", ctx)
		}

		in := vectorU256(t, c.N, ctx+".n")
		got := in.Shl(uint(*c.Shift))

		if want := vectorU256(t, c.Want, ctx+".want"); got != want {
			t.Errorf("%s: %s << %d = %+v, want %+v", ctx, in, *c.Shift, got, want)
			return
		}

		if want := vectorDec(t, c.Want, ctx+".want"); u256Big(got).Cmp(want) != 0 {
			t.Errorf("%s: %s << %d = %s, want %s", ctx, in, *c.Shift, u256Big(got), want)
		}
	})

	if count == 0 {
		t.Fatal("no u256_shl vectors")
	}
}

// TestDiv256By128Vectors checks quotient, remainder and the overflow rule.
func TestDiv256By128Vectors(t *testing.T) {
	t.Parallel()

	count := eachVector(t, opDiv256By128, func(index int, c vectorCase) {
		ctx := fmt.Sprintf("case %d (%s)", index, c.Op)

		n := vectorU256(t, c.N, ctx+".n")
		d := vectorU128(t, c.D, ctx+".d")

		gotQ, gotR, err := uint256.Div256By128(n, d)

		if c.WantError != nil {
			if c.Want != nil || c.WantRem != nil {
				t.Fatalf("%s: case reports %q but also carries a value", ctx, *c.WantError)
			}

			assertUint256Error(t, ctx, err, *c.WantError)

			return
		}

		if err != nil {
			t.Fatalf("%s: Div256By128(%s, %s) error = %v, want a value", ctx, n, d, err)
		}

		wantQ := vectorU128(t, c.Want, ctx+".want")

		wantR := vectorU128(t, c.WantRem, ctx+".want_rem")
		if gotQ != wantQ || gotR != wantR {
			t.Errorf("%s: Div256By128(%s, %s) = (%+v, %+v), want (%+v, %+v)", ctx, n, d, gotQ, gotR, wantQ, wantR)
			return
		}

		// Re-derive q*d + r == n and r < d from the vector's own operands, in
		// big.Int, so the result is checked against the Euclidean definition and
		// not only against the expected quotient.
		nBig := vectorDec(t, c.N, ctx+".n")
		dBig := vectorDec(t, c.D, ctx+".d")

		rebuilt := new(big.Int).Add(new(big.Int).Mul(u128Big(gotQ), dBig), u128Big(gotR))
		if rebuilt.Cmp(nBig) != 0 {
			t.Errorf("%s: q*d + r = %s, want n = %s", ctx, rebuilt, nBig)
		}

		if u128Big(gotR).Cmp(dBig) >= 0 {
			t.Errorf("%s: remainder %s is not less than divisor %s", ctx, u128Big(gotR), dBig)
		}
	})

	if count == 0 {
		t.Fatal("no div256_by128 vectors")
	}
}

// TestDivCeil256By128Vectors checks the ceiling, including that the increment
// itself can be the value that does not fit.
func TestDivCeil256By128Vectors(t *testing.T) {
	t.Parallel()

	count := eachVector(t, opDivCeil256By128, func(index int, c vectorCase) {
		ctx := fmt.Sprintf("case %d (%s)", index, c.Op)

		n := vectorU256(t, c.N, ctx+".n")
		d := vectorU128(t, c.D, ctx+".d")

		got, err := uint256.DivCeil256By128(n, d)

		if c.WantError != nil {
			if c.Want != nil {
				t.Fatalf("%s: case reports %q but also carries a value", ctx, *c.WantError)
			}

			assertUint256Error(t, ctx, err, *c.WantError)

			return
		}

		if err != nil {
			t.Fatalf("%s: DivCeil256By128(%s, %s) error = %v, want a value", ctx, n, d, err)
		}

		want := vectorU128(t, c.Want, ctx+".want")
		if got != want {
			t.Errorf("%s: DivCeil256By128(%s, %s) = %+v, want %+v", ctx, n, d, got, want)
			return
		}

		if wantBig := vectorDec(t, c.Want, ctx+".want"); u128Big(got).Cmp(wantBig) != 0 {
			t.Errorf("%s: DivCeil256By128(%s, %s) = %s, want %s", ctx, n, d, u128Big(got), wantBig)
		}
	})

	if count == 0 {
		t.Fatal("no divceil256_by128 vectors")
	}
}

// assertUint256Error maps the generator's error names onto this package's
// sentinels. An unknown name is a failure: it would otherwise pass silently.
func assertUint256Error(t *testing.T, ctx string, err error, want string) {
	t.Helper()

	switch want {
	case "overflow":
		if !errors.Is(err, uint256.ErrOverflow) {
			t.Errorf("%s: error = %v, want ErrOverflow", ctx, err)
		}
	case "divide_by_zero":
		if !errors.Is(err, uint256.ErrDivideByZero) {
			t.Errorf("%s: error = %v, want ErrDivideByZero", ctx, err)
		}
	default:
		t.Fatalf("%s: unknown want_error %q", ctx, want)
	}
}

// TestNumVectorCoverage guards against the file quietly losing an operation or an
// error class: an empty sub-list would turn every case test above into a no-op
// that passes.
func TestNumVectorCoverage(t *testing.T) {
	t.Parallel()

	required := []string{
		opU128Limbs,
		opU256Limbs,
		opMul128To256,
		opU256Shr,
		opU256Shl,
		opDiv256By128,
		opDivCeil256By128,
	}

	counts := map[string]int{}
	errorsSeen := map[string]int{}

	for _, c := range loadVectors(t).Cases {
		counts[c.Op]++
		if c.WantError != nil {
			errorsSeen[*c.WantError]++
		}
	}

	for _, op := range required {
		if counts[op] == 0 {
			t.Errorf("no %s vectors", op)
		}
	}

	for _, class := range []string{"overflow", "divide_by_zero"} {
		if errorsSeen[class] == 0 {
			t.Errorf("no %s vectors", class)
		}
	}
}
