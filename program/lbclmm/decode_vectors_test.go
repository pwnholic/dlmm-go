package lbclmm_test

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	solana "github.com/gagliardetto/solana-go"

	"github.com/pwnholic/dlmm-go/num"
	"github.com/pwnholic/dlmm-go/pda"
	"github.com/pwnholic/dlmm-go/program/lbclmm"
)

// The vectors are produced by tools/gen_golden/gen_account_vectors.py, an
// independent Python Borsh decoder written from the IDL alone. It shares no code
// with the Go decoder under test, so agreement between the two is evidence about
// the wire format rather than a restatement of the same assumption.
//
// Each vector records the byte offset and width of every field, which makes the
// comparison a check on the layout itself and not only on the values.

type vectorField struct {
	Offset int `json:"offset"`
	Bytes  int `json:"bytes"`
	Value  any `json:"value"`
	// Lo and Hi carry the limbs of a u128 field, so the comparison checks the
	// limb split as well as the value.
	Lo *uint64 `json:"lo"`
	Hi *uint64 `json:"hi"`
}

type vectorCase struct {
	Source        string                     `json:"source"`
	Account       string                     `json:"account"`
	ByteLength    int                        `json:"byte_length"`
	Discriminator []int                      `json:"discriminator"`
	Fields        map[string]json.RawMessage `json:"fields"`
}

type vectorFile struct {
	Account     string       `json:"account"`
	VectorCount int          `json:"vector_count"`
	Vectors     []vectorCase `json:"vectors"`
}

func vectorsDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join("..", "..", "testdata", "decoded")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("golden vectors absent at %s; regenerate with tools/gen_golden", dir)
	}

	return dir
}

func loadVectors(t *testing.T, account string) vectorFile {
	t.Helper()

	path := filepath.Join(vectorsDir(t), account+".json")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no vectors for %s: %v", account, err)
	}

	var vf vectorFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	if len(vf.Vectors) == 0 {
		t.Fatalf("%s contains no vectors, so it would prove nothing", path)
	}

	return vf
}

// loadedCase pairs a vector with its fixture bytes, read up front so a subtest
// never has to skip and so a missing fixture skips the whole test rather than
// part of it.
type loadedCase struct {
	vec vectorCase
	raw []byte
}

// loadCases reads every fixture a vector file references, or skips the test.
//
// Doing this in the parent is what lets the subtests run in parallel while still
// proving they covered everything: either all inputs exist, or the test does not
// run at all.
func loadCases(t *testing.T, vf vectorFile) []loadedCase {
	t.Helper()

	out := make([]loadedCase, 0, len(vf.Vectors))
	for _, c := range vf.Vectors {
		// source is recorded relative to the repository root, so only the root
		// needs joining. Prefixing the fixtures directory here as well produced
		// a doubled path and skipped every case while reporting PASS.
		path := filepath.Join("..", "..", c.Source)

		raw, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("fixture unavailable at %s: %v", path, err)
		}

		if len(raw) != c.ByteLength {
			t.Fatalf("%s is %d bytes, vector records %d", path, len(raw), c.ByteLength)
		}

		out = append(out, loadedCase{vec: c, raw: raw})
	}

	if len(out) != len(vf.Vectors) {
		t.Fatalf("loaded %d of %d fixtures", len(out), len(vf.Vectors))
	}

	return out
}

func fieldOf(t *testing.T, c vectorCase, name string) vectorField {
	t.Helper()

	raw, ok := c.Fields[name]
	if !ok {
		t.Fatalf("vector has no field %q", name)
	}

	var f vectorField
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("field %q: %v", name, err)
	}

	return f
}

// intOf renders a vector field as an integer. The generator emits small scalars
// as JSON numbers and wide or byte-array fields as strings, so both are accepted.
func intOf(t *testing.T, f vectorField) *big.Int {
	t.Helper()

	switch v := f.Value.(type) {
	case float64:
		return big.NewInt(int64(v))
	case string:
		if n, ok := new(big.Int).SetString(v, 10); ok {
			return n
		}

		t.Fatalf("field value %q is not a decimal integer", v)

		return nil
	default:
		t.Fatalf("unsupported field value type %T", f.Value)
		return nil
	}
}

func assertUint(t *testing.T, c vectorCase, name string, got uint64, width int) {
	t.Helper()

	f := fieldOf(t, c, name)
	if f.Bytes != width {
		t.Errorf("%s: vector width %d, expected %d", name, f.Bytes, width)
	}

	want := intOf(t, f)
	if want.Uint64() != got {
		t.Errorf("%s @%d: Go read %d, Python read %s", name, f.Offset, got, want)
	}
}

func assertInt(t *testing.T, c vectorCase, name string, got int64, width int) {
	t.Helper()

	f := fieldOf(t, c, name)
	if f.Bytes != width {
		t.Errorf("%s: vector width %d, expected %d", name, f.Bytes, width)
	}

	want := intOf(t, f)
	if want.Int64() != got {
		t.Errorf("%s @%d: Go read %d, Python read %s", name, f.Offset, got, want)
	}
}

func assertPubkey(t *testing.T, c vectorCase, name string, got solana.PublicKey) {
	t.Helper()

	f := fieldOf(t, c, name)

	want, ok := f.Value.(string)
	if !ok {
		t.Fatalf("%s: vector value is %T, expected a base58 string", name, f.Value)
	}

	if got.String() != want {
		t.Errorf("%s @%d: Go read %s, Python read %s", name, f.Offset, got, want)
	}
}

// assertU128 compares a num.U128 against the vector, checking the limbs the
// Python decoder recorded rather than only the combined value. A limb swap
// changes the value too, but checking both makes the failure message name the
// limb that is wrong.
func assertU128(t *testing.T, c vectorCase, name string, got num.U128) {
	t.Helper()

	f := fieldOf(t, c, name)
	if f.Bytes != 16 {
		t.Errorf("%s: vector width %d, expected 16", name, f.Bytes)
	}

	if f.Lo == nil || f.Hi == nil {
		t.Fatalf("%s: vector has no lo/hi limbs", name)
	}

	if got.Lo != *f.Lo || got.Hi != *f.Hi {
		t.Errorf("%s @%d: Go read {Lo:%d Hi:%d}, Python read {Lo:%d Hi:%d}",
			name, f.Offset, got.Lo, got.Hi, *f.Lo, *f.Hi)
	}
}

// assertBytes compares a fixed byte array against the vector.
//
// The generator emits a [u8; N] field as a hex string of its raw bytes, not as a
// decimal, because the field is an array rather than a scalar. Guessing at that
// in the integer helper would be ambiguous with a decimal that happens to look
// like hex, so byte arrays get their own comparison.
func assertBytes(t *testing.T, c vectorCase, name string, got []byte) {
	t.Helper()

	f := fieldOf(t, c, name)
	if f.Bytes != len(got) {
		t.Errorf("%s: vector width %d, Go type width %d", name, f.Bytes, len(got))
	}

	hexStr, ok := f.Value.(string)
	if !ok {
		t.Fatalf("%s: vector value is %T, expected a hex string", name, f.Value)
	}

	want, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("%s: vector value %q is not hex: %v", name, hexStr, err)
	}

	if len(want) != len(got) {
		t.Fatalf("%s: vector has %d bytes, Go has %d", name, len(want), len(got))
	}

	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s @%d byte %d: Go read 0x%02x, Python read 0x%02x",
				name, f.Offset, i, got[i], want[i])
		}
	}
}

func assertAllZero(t *testing.T, name string, data []byte) {
	t.Helper()

	for i, b := range data {
		if b != 0 {
			t.Errorf("%s[%d] = %d, want 0: a non-zero padding byte means the decode is "+
				"misaligned or the field order is wrong", name, i, b)

			return
		}
	}
}

// poolAddress reproduces a pool's address from its own decoded fields.
//
// This is the strongest check available because the address is not stored in the
// account data: a decoder that misreads a seed field cannot also produce a
// matching address. pair_type selects the layout, so every permissionless form
// is tried and one must match.
func poolAddress(t *testing.T, p *lbclmm.LbPair, want string) solana.PublicKey {
	t.Helper()

	program := lbclmm.ProgramIDMainnet

	candidates := []struct {
		name string
		fn   func() (solana.PublicKey, uint8, error)
	}{
		{"LbPair", func() (solana.PublicKey, uint8, error) {
			return pda.LbPair(program, p.TokenXMint, p.TokenYMint, p.BinStep)
		}},
		{"LbPairV2", func() (solana.PublicKey, uint8, error) {
			return pda.LbPairV2(program, p.TokenXMint, p.TokenYMint, p.BinStep, p.Parameters.BaseFactor)
		}},
		{"LbPairWithPreset", func() (solana.PublicKey, uint8, error) {
			return pda.LbPairWithPreset(program, p.BaseKey, p.TokenXMint, p.TokenYMint)
		}},
		{"CustomizablePermissionlessLbPair", func() (solana.PublicKey, uint8, error) {
			return pda.CustomizablePermissionlessLbPair(program, p.TokenXMint, p.TokenYMint)
		}},
	}

	// Every derivation succeeds in the sense that find_program_address always
	// returns some address, so the match is what identifies the layout. Returning
	// the first success would always yield the LbPair form and would report a
	// mismatch for every pool created any other way.
	tried := make([]string, 0, len(candidates))
	for _, c := range candidates {
		addr, _, err := c.fn()
		if err != nil {
			continue
		}

		if addr.String() == want {
			return addr
		}

		tried = append(tried, c.name+" -> "+addr.String())
	}

	t.Fatalf("no derivation reproduces %s from the decoded fields:\n    %s",
		want, strings.Join(tried, "\n    "))

	return solana.PublicKey{}
}

// TestDecodeLbPairAgainstIndependentVectors compares every field the Python
// decoder recorded, plus the account's own address, against the Go decoder.
func TestDecodeLbPairAgainstIndependentVectors(t *testing.T) {
	t.Parallel()

	vf := loadVectors(t, "LbPair")

	for _, lc := range loadCases(t, vf) {
		c, raw := lc.vec, lc.raw
		name := filepath.Base(filepath.Dir(c.Source))
		t.Run(name[:8], func(t *testing.T) {
			t.Parallel()

			got, err := lbclmm.DecodeLbPair(raw)
			if err != nil {
				t.Fatalf("DecodeLbPair: %v", err)
			}

			// Fields chosen because they pin the layout: they are spread across
			// the struct and cover both nested parameter blocks.
			assertUint(t, c, "parameters.base_factor", uint64(got.Parameters.BaseFactor), 2)
			assertUint(t, c, "parameters.filter_period", uint64(got.Parameters.FilterPeriod), 2)
			assertUint(t, c, "parameters.protocol_share", uint64(got.Parameters.ProtocolShare), 2)
			assertInt(t, c, "parameters.min_bin_id", int64(got.Parameters.MinBinId), 4)
			assertInt(t, c, "parameters.max_bin_id", int64(got.Parameters.MaxBinId), 4)
			assertUint(t, c, "v_parameters.volatility_accumulator", uint64(got.VParameters.VolatilityAccumulator), 4)
			assertInt(t, c, "v_parameters.index_reference", int64(got.VParameters.IndexReference), 4)
			assertInt(t, c, "v_parameters.last_update_timestamp", got.VParameters.LastUpdateTimestamp, 8)

			assertBytes(t, c, "bump_seed", got.BumpSeed[:])
			assertBytes(t, c, "bin_step_seed", got.BinStepSeed[:])
			assertBytes(t, c, "base_factor_seed", got.BaseFactorSeed[:])
			assertUint(t, c, "pair_type", uint64(got.PairType), 1)
			assertInt(t, c, "active_id", int64(got.ActiveId), 4)
			assertUint(t, c, "bin_step", uint64(got.BinStep), 2)
			assertUint(t, c, "status", uint64(got.Status), 1)
			assertUint(t, c, "require_base_factor_seed", uint64(got.RequireBaseFactorSeed), 1)
			assertUint(t, c, "activation_point", got.ActivationPoint, 8)
			assertUint(t, c, "version", uint64(got.Version), 1)

			assertPubkey(t, c, "token_x_mint", got.TokenXMint)
			assertPubkey(t, c, "token_y_mint", got.TokenYMint)
			assertPubkey(t, c, "reserve_x", got.ReserveX)
			assertPubkey(t, c, "reserve_y", got.ReserveY)
			assertPubkey(t, c, "oracle", got.Oracle)
			assertPubkey(t, c, "base_key", got.BaseKey)
			assertPubkey(t, c, "creator", got.Creator)

			// Padding is the cheapest misalignment detector: one byte of shift
			// upstream turns one of these non-zero.
			assertAllZero(t, "parameters._padding", got.Parameters.Padding[:])
			assertAllZero(t, "v_parameters._padding", got.VParameters.Padding[:])
			assertAllZero(t, "v_parameters._padding_1", got.VParameters.Padding1[:])
			assertAllZero(t, "_padding_1", got.Padding1[:])
			assertAllZero(t, "_padding_2", got.Padding2[:])
			assertAllZero(t, "_padding_3", got.Padding3[:])
			assertAllZero(t, "_padding_4 (u64)", []byte{
				byte(got.Padding4), byte(got.Padding4 >> 8), byte(got.Padding4 >> 16), byte(got.Padding4 >> 24),
				byte(got.Padding4 >> 32), byte(got.Padding4 >> 40), byte(got.Padding4 >> 48), byte(got.Padding4 >> 56),
			})
			assertAllZero(t, "_reserved", got.Reserved[:])

			// The account's own address must fall out of the decoded fields.
			// The address is not stored in the account, so a decoder that
			// misreads a seed field cannot also produce a matching address.
			poolAddress(t, got, name)
		})
	}
}

// TestDecodeBinArrayAgainstIndependentVectors checks the largest account, whose
// 70-bin array is where a stride error would be most visible.
func TestDecodeBinArrayAgainstIndependentVectors(t *testing.T) {
	t.Parallel()

	vf := loadVectors(t, "BinArray")

	for _, lc := range loadCases(t, vf) {
		c, raw := lc.vec, lc.raw
		name := filepath.Base(filepath.Dir(c.Source)) + "/" + filepath.Base(c.Source)
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := lbclmm.DecodeBinArray(raw)
			if err != nil {
				t.Fatalf("DecodeBinArray: %v", err)
			}

			assertInt(t, c, "index", got.Index, 8)
			assertUint(t, c, "version", uint64(got.Version), 1)
			assertPubkey(t, c, "lb_pair", got.LbPair)
			assertAllZero(t, "_padding_1", got.Padding1[:])

			if len(got.Bins) != lbclmm.MaxBinPerArray {
				t.Fatalf("decoded %d bins, want %d", len(got.Bins), lbclmm.MaxBinPerArray)
			}

			// The bin stride is what a wrong element size would break, so read
			// the first and last bin and compare their price offset.
			assertBinFields(t, c, 0, got.Bins[0])
			assertBinFields(t, c, lbclmm.MaxBinPerArray-1, got.Bins[lbclmm.MaxBinPerArray-1])
		})
	}
}

func assertBinFields(t *testing.T, c vectorCase, i int, b lbclmm.Bin) {
	t.Helper()

	assertUint(t, c, "bins["+itoa(i)+"].amount_x", b.AmountX, 8)
	assertUint(t, c, "bins["+itoa(i)+"].amount_y", b.AmountY, 8)
	assertU128(t, c, "bins["+itoa(i)+"].price", b.Price)
	assertU128(t, c, "bins["+itoa(i)+"].liquidity_supply", b.LiquiditySupply)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}

	var buf [8]byte

	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}

	return string(buf[pos:])
}

// TestDecodeOracleAgainstIndependentVectors checks the account whose declared
// struct is far shorter than its on-chain form.
func TestDecodeOracleAgainstIndependentVectors(t *testing.T) {
	t.Parallel()

	vf := loadVectors(t, "Oracle")

	for _, lc := range loadCases(t, vf) {
		c, raw := lc.vec, lc.raw
		t.Run(filepath.Base(filepath.Dir(c.Source))[:8], func(t *testing.T) {
			t.Parallel()

			// The fixture is 3232 bytes while the declared struct is 32: the
			// rest is the observation buffer that grows on chain. The decoder
			// must read the header and tolerate the remainder.
			got, err := lbclmm.DecodeOracle(raw)
			if err != nil {
				t.Fatalf("DecodeOracle: %v", err)
			}

			assertUint(t, c, "idx", got.Idx, 8)
			assertUint(t, c, "active_size", got.ActiveSize, 8)
			assertUint(t, c, "length", got.Length, 8)

			// Internally stated relations from the IDL documentation.
			if got.ActiveSize > got.Length {
				t.Errorf("active_size %d > length %d", got.ActiveSize, got.Length)
			}

			if got.Length > 0 && got.Idx >= got.Length {
				t.Errorf("idx %d >= length %d", got.Idx, got.Length)
			}

			// The base struct plus the observation buffer must account for the
			// whole account, which is the only check available for the trailing
			// region since its layout is not in this IDL.
			const observationStride = 32

			trailing := len(raw) - (8 + 24)
			if trailing%observationStride != 0 {
				t.Errorf("%d trailing bytes are not a whole number of %d-byte observations",
					trailing, observationStride)
			}
		})
	}
}
