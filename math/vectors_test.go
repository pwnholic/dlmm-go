package math

// Golden-vector tests for MulDiv, MulShr and ShlDiv.
//
// The expected values come from tools/gen_vectors/gen_numeric_vectors.py, which
// computes them in Python with arbitrary-precision integers: a different
// language, a different algorithm, and no shared code with the implementation
// under test. That matters here more than anywhere else in the SDK, because the
// Rust reference this code mirrors and its Go port were written from the same
// reading of the same spec, and a shared misunderstanding of "round up" or of
// the offset bound would survive every test that compares the two.
//
// The vectors are committed under testdata/ rather than regenerated during the
// test run, so a changed expectation shows up as a reviewable diff instead of a
// quietly greener test run. Regenerate with:
//
//	python3 tools/gen_vectors/gen_numeric_vectors.py
//
// The same file also carries num and internal/uint256 cases, which
// num/vectors_test.go consumes; this file reads only the three ops below but
// still checks the limb encoding of the u128 operands it does read.
//
// The jsonInt/vectorCase types are declared here rather than shared with
// num/vectors_test.go because the two files are in different packages and no
// shared test-helper package exists for them.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/pwnholic/dlmm-go/num"
)

// vectorFile is relative to this package's directory, which is the working
// directory of the test binary.
const vectorFile = "../testdata/vectors/numeric.json"

// Op names this file dispatches on. They are the generator's "op" values.
const (
	opMulDivVectors = "mul_div"
	opMulShrVectors = "mul_shr"
	opShlDivVectors = "shl_div"
)

// jsonInt is a u128 exactly as the generator emits it: a decimal string plus
// limbs, each limb also a decimal string. Limbs are strings so that no value ever
// has to survive a float64 round trip in a JSON parser.
type jsonInt struct {
	Dec string  `json:"dec"`
	Lo  *string `json:"lo"`
	Hi  *string `json:"hi"`
	L0  *string `json:"l0"`
	L1  *string `json:"l1"`
	L2  *string `json:"l2"`
	L3  *string `json:"l3"`
}

// vectorCase mirrors one case object. Every field is a pointer because the
// generator omits whatever the op does not use, and an omitted field must stay
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

// eachVector runs fn for every case targeting op and returns how many it ran. The
// file index is passed through so a failure points at the exact JSON line.
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

// vectorDec parses the decimal form with math/big, which shares no code with num.
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

// u128Big combines a u128's limbs the way its documentation says the fields go:
// Lo is the low 64 bits. It does not call into num, so it can catch a transposed
// layout instead of agreeing with it.
func u128Big(v num.U128) *big.Int {
	return new(big.Int).Or(
		new(big.Int).Lsh(new(big.Int).SetUint64(v.Hi), 64),
		new(big.Int).SetUint64(v.Lo),
	)
}

// vectorRounding maps the file's rounding name onto the mode. An unknown name is a
// failure rather than a default, since RoundingInvalid would make the op under
// test return an error and the case would then be reported as a wrong error.
func vectorRounding(t *testing.T, ctx, name string) Rounding {
	t.Helper()

	switch name {
	case "down":
		return RoundingDown
	case "up":
		return RoundingUp
	default:
		t.Fatalf("%s: unknown rounding %q", ctx, name)
		return RoundingInvalid
	}
}

// vectorOffset converts the file's offset, which is a plain integer, into the
// uint8 the API takes. Out-of-range values are rejected here so that a generator
// change cannot silently wrap into a different offset.
func vectorOffset(t *testing.T, ctx string, c vectorCase) uint8 {
	t.Helper()

	if c.Offset == nil {
		t.Fatalf("%s: missing offset", ctx)
	}

	if *c.Offset < 0 || *c.Offset > 0xff {
		t.Fatalf("%s: offset %d does not fit a uint8", ctx, *c.Offset)
	}

	return uint8(*c.Offset)
}

// assertMathVectorError maps the generator's error names onto this package's
// sentinels. An unknown name is a failure: it would otherwise pass silently.
func assertMathVectorError(t *testing.T, ctx string, err error, want string) {
	t.Helper()

	switch want {
	case "overflow":
		if !errors.Is(err, ErrOverflow) {
			t.Errorf("%s: error = %v, want ErrOverflow", ctx, err)
		}
	case "divide_by_zero":
		if !errors.Is(err, ErrDivideByZero) {
			t.Errorf("%s: error = %v, want ErrDivideByZero", ctx, err)
		}
	case "offset_too_large":
		if !errors.Is(err, ErrOffsetTooLarge) {
			t.Errorf("%s: error = %v, want ErrOffsetTooLarge", ctx, err)
		}
	default:
		t.Fatalf("%s: unknown want_error %q", ctx, want)
	}
}

// checkCaseShape rejects a case that contradicts itself: want and want_error are
// mutually exclusive by construction in the generator, so a file with both (or
// neither) is stale or hand-edited and must not be read as a pass.
func checkCaseShape(t *testing.T, ctx string, c vectorCase) bool {
	t.Helper()

	if (c.Want == nil) == (c.WantError == nil) {
		t.Fatalf("%s: want and want_error must be exactly one of set/null, got want=%v want_error=%v",
			ctx, c.Want != nil, c.WantError != nil)
	}

	return c.Want != nil
}

// TestMathVectorLimbEncoding checks that the decimal form and the limb form of
// every u128 in the file describe the same number.
//
// Every assertion below builds its operands from limbs, so if the generator and
// the Go code agreed on a transposed limb order they would agree on all of them
// and this whole oracle would be worthless.
func TestMathVectorLimbEncoding(t *testing.T) {
	t.Parallel()

	checked := 0

	for index, c := range loadVectors(t).Cases {
		for _, field := range []struct {
			Name  string
			Value *jsonInt
		}{
			{"x", c.X},
			{"y", c.Y},
			{"denominator", c.Denominator},
			{"want", c.Want},
		} {
			if field.Value == nil || field.Value.L3 != nil {
				// Absent, or a u256 that belongs to num/vectors_test.go.
				continue
			}

			ctx := fmt.Sprintf("case %d (%s) field %s", index, c.Op, field.Name)

			fromLimbs := u128Big(vectorU128(t, field.Value, ctx))
			if want := vectorDec(t, field.Value, ctx); fromLimbs.Cmp(want) != 0 {
				t.Errorf("%s: limbs say %s, decimal says %s", ctx, fromLimbs, want)
			}

			checked++
		}
	}

	if checked == 0 {
		t.Fatal("no u128 fields found in the vector file")
	}
}

// TestMulDivVectors checks (x * y) / denominator in both roundings: the value, the
// overflow rule, and the divide-by-zero rule.
func TestMulDivVectors(t *testing.T) {
	t.Parallel()

	count := eachVector(t, opMulDivVectors, func(index int, c vectorCase) {
		ctx := fmt.Sprintf("case %d (%s)", index, c.Op)

		x := vectorU128(t, c.X, ctx+".x")
		y := vectorU128(t, c.Y, ctx+".y")
		d := vectorU128(t, c.Denominator, ctx+".denominator")
		mode := vectorRounding(t, ctx, c.Rounding)

		got, err := MulDiv(x, y, d, mode)
		hasValue := checkCaseShape(t, ctx, c)

		if !hasValue {
			assertMathVectorError(t, ctx, err, *c.WantError)
			return
		}

		if err != nil {
			t.Fatalf("%s: MulDiv(%s, %s, %s, %s) error = %v, want a value", ctx, x, y, d, mode, err)
		}

		want := vectorU128(t, c.Want, ctx+".want")
		if got != want {
			t.Errorf("%s: MulDiv(%s, %s, %s, %s) = %+v, want %+v", ctx, x, y, d, mode, got, want)
			return
		}

		if wantBig := vectorDec(t, c.Want, ctx+".want"); u128Big(got).Cmp(wantBig) != 0 {
			t.Errorf("%s: MulDiv(%s, %s, %s, %s) = %s, want %s", ctx, x, y, d, mode, u128Big(got), wantBig)
		}
	})

	if count == 0 {
		t.Fatal("no mul_div vectors")
	}
}

// TestMulShrVectors checks (x * y) >> offset in both roundings, including the
// rejected offsets and the two ways the result can fail to fit.
func TestMulShrVectors(t *testing.T) {
	t.Parallel()

	count := eachVector(t, opMulShrVectors, func(index int, c vectorCase) {
		ctx := fmt.Sprintf("case %d (%s)", index, c.Op)

		x := vectorU128(t, c.X, ctx+".x")
		y := vectorU128(t, c.Y, ctx+".y")
		offset := vectorOffset(t, ctx, c)
		mode := vectorRounding(t, ctx, c.Rounding)

		got, err := MulShr(x, y, offset, mode)
		hasValue := checkCaseShape(t, ctx, c)

		if !hasValue {
			assertMathVectorError(t, ctx, err, *c.WantError)
			return
		}

		if err != nil {
			t.Fatalf("%s: MulShr(%s, %s, %d, %s) error = %v, want a value", ctx, x, y, offset, mode, err)
		}

		want := vectorU128(t, c.Want, ctx+".want")
		if got != want {
			t.Errorf("%s: MulShr(%s, %s, %d, %s) = %+v, want %+v", ctx, x, y, offset, mode, got, want)
			return
		}

		if wantBig := vectorDec(t, c.Want, ctx+".want"); u128Big(got).Cmp(wantBig) != 0 {
			t.Errorf("%s: MulShr(%s, %s, %d, %s) = %s, want %s", ctx, x, y, offset, mode, u128Big(got), wantBig)
		}
	})

	if count == 0 {
		t.Fatal("no mul_shr vectors")
	}
}

// TestShlDivVectors checks (x << offset) / y in both roundings.
func TestShlDivVectors(t *testing.T) {
	t.Parallel()

	count := eachVector(t, opShlDivVectors, func(index int, c vectorCase) {
		ctx := fmt.Sprintf("case %d (%s)", index, c.Op)

		x := vectorU128(t, c.X, ctx+".x")
		y := vectorU128(t, c.Y, ctx+".y")
		offset := vectorOffset(t, ctx, c)
		mode := vectorRounding(t, ctx, c.Rounding)

		got, err := ShlDiv(x, y, offset, mode)
		hasValue := checkCaseShape(t, ctx, c)

		if !hasValue {
			assertMathVectorError(t, ctx, err, *c.WantError)
			return
		}

		if err != nil {
			t.Fatalf("%s: ShlDiv(%s, %s, %d, %s) error = %v, want a value", ctx, x, y, offset, mode, err)
		}

		want := vectorU128(t, c.Want, ctx+".want")
		if got != want {
			t.Errorf("%s: ShlDiv(%s, %s, %d, %s) = %+v, want %+v", ctx, x, y, offset, mode, got, want)
			return
		}

		if wantBig := vectorDec(t, c.Want, ctx+".want"); u128Big(got).Cmp(wantBig) != 0 {
			t.Errorf("%s: ShlDiv(%s, %s, %d, %s) = %s, want %s", ctx, x, y, offset, mode, u128Big(got), wantBig)
		}
	})

	if count == 0 {
		t.Fatal("no shl_div vectors")
	}
}

// TestMathVectorCoverage guards against the file quietly losing an operation, an
// error class, or the input class that distinguishes the two roundings: an empty
// sub-list would turn the case tests above into no-ops that pass.
func TestMathVectorCoverage(t *testing.T) {
	t.Parallel()

	counts := map[string]int{}
	errorsSeen := map[string]int{}

	for _, c := range loadVectors(t).Cases {
		counts[c.Op]++
		if c.WantError != nil {
			errorsSeen[*c.WantError]++
		}
	}

	for _, op := range []string{opMulDivVectors, opMulShrVectors, opShlDivVectors} {
		if counts[op] == 0 {
			t.Errorf("no %s vectors", op)
		}
	}

	for _, class := range []string{"overflow", "divide_by_zero", "offset_too_large"} {
		if errorsSeen[class] == 0 {
			t.Errorf("no %s vectors", class)
		}
	}
}

// TestMathVectorCoversTheRoundingDisagreement checks the one input class where
// the two roundings disagree about representability: the quotient lands exactly on
// the u128 ceiling and there is a remainder, so rounding down fits and rounding up
// does not. Both entry points that round must have such a pair, or the branch that
// turns the ceiling increment into an error is unexercised.
//
// ShlDiv is absent on purpose: no u128 input can produce that class there. If
// floor(x<<o / y) were max u128 with remainder r and 0 < r < y, then
// x<<o == y*(2^128-1) + r, so r == y (mod 2^o), which forces y >= 2^o + r and
// therefore x >= (2^o+1)*(2^128-1)/2^o > max u128.
func TestMathVectorCoversTheRoundingDisagreement(t *testing.T) {
	t.Parallel()

	type key struct {
		op      string
		x, y    string
		divisor string
		offset  string
	}

	type pair struct {
		downFits bool
		downWant *jsonInt
		upFits   bool
		upErr    *string
	}

	// The file is ordered down-then-up per input, but grouping by input rather
	// than relying on that order keeps this check honest if the generator ever
	// changes its emission order.
	groups := map[key]*pair{}

	for index, c := range loadVectors(t).Cases {
		if c.Op != opMulDivVectors && c.Op != opMulShrVectors {
			continue
		}

		var k key
		if c.Op == opMulDivVectors {
			k = key{op: c.Op, x: c.X.Dec, y: c.Y.Dec, divisor: c.Denominator.Dec}
		} else {
			k = key{op: c.Op, x: c.X.Dec, y: c.Y.Dec, offset: strconv.Itoa(*c.Offset)}
		}

		group, ok := groups[k]
		if !ok {
			group = &pair{}
			groups[k] = group
		}

		switch c.Rounding {
		case "down":
			group.downFits = c.Want != nil
			group.downWant = c.Want
		case "up":
			group.upFits = c.Want != nil
			group.upErr = c.WantError
		default:
			t.Fatalf("case %d (%s): unknown rounding %q", index, c.Op, c.Rounding)
		}
	}

	found := map[string]int{}

	for k, group := range groups {
		if !group.downFits || group.upFits {
			continue
		}

		if group.upErr == nil || *group.upErr != "overflow" {
			continue
		}

		// Rounding down fits and rounding up overflows, so the truncated result can
		// only be the ceiling itself.
		if got := vectorU128(t, group.downWant, "rounding-disagreement pair"); got != num.MaxU128 {
			t.Errorf("%s: rounding down gives %+v, want MaxU128 in a pair where rounding up overflows", k.op, got)
		}

		found[k.op]++
	}

	for _, op := range []string{opMulDivVectors, opMulShrVectors} {
		if found[op] == 0 {
			t.Errorf("no %s vector where rounding down fits and rounding up overflows", op)
		}
	}
}
