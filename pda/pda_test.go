package pda

import (
	"bytes"
	"encoding/binary"
	"testing"

	solana "github.com/gagliardetto/solana-go"
)

// programID is the devnet deployment, used only as a stable identifier for
// these tests. Derivation is program-ID agnostic, so any valid key works.
var programID = solana.MustPublicKeyFromBase58("LBUZKhRxPF3XUpBCjp4YzTKgLccjZhTSDM9YuVaPwxo")

var (
	mintA = solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
	mintB = solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	base  = solana.MustPublicKeyFromBase58("9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM")
)

func TestOrderedMintsSortsByRawBytesNotBase58(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b solana.PublicKey
	}{
		{"a before b", mintA, mintB},
		{"b before a", mintB, mintA},
		{"identical keys", mintA, mintA},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lo, hi := orderedMints(tt.a, tt.b)

			if bytes.Compare(lo[:], hi[:]) > 0 {
				t.Errorf("orderedMints returned lo=%s hi=%s, which is not ascending", lo, hi)
			}

			// The pair must be preserved regardless of input order.
			sameForward := lo == tt.a && hi == tt.b

			sameReversed := lo == tt.b && hi == tt.a
			if !sameForward && !sameReversed {
				t.Errorf("orderedMints(%s, %s) = (%s, %s), which is not the same pair", tt.a, tt.b, lo, hi)
			}
		})
	}
}

func TestMintOrderDoesNotChangeThePoolAddress(t *testing.T) {
	t.Parallel()

	// The on-chain program sorts the mints, so passing them the other way round
	// must produce the same pool. A failure here means a min/max mistake, which
	// would otherwise surface as a valid-looking address for a pool that does
	// not exist.
	tests := []struct {
		name string
		call func(a, b solana.PublicKey) (solana.PublicKey, uint8, error)
	}{
		{"LbPair", func(a, b solana.PublicKey) (solana.PublicKey, uint8, error) {
			return LbPair(programID, a, b, 25)
		}},
		{"LbPairV2", func(a, b solana.PublicKey) (solana.PublicKey, uint8, error) {
			return LbPairV2(programID, a, b, 25, 10000)
		}},
		{"LbPairWithPreset", func(a, b solana.PublicKey) (solana.PublicKey, uint8, error) {
			return LbPairWithPreset(programID, base, a, b)
		}},
		{"CustomizablePermissionlessLbPair", func(a, b solana.PublicKey) (solana.PublicKey, uint8, error) {
			return CustomizablePermissionlessLbPair(programID, a, b)
		}},
		{"PermissionLbPair", func(a, b solana.PublicKey) (solana.PublicKey, uint8, error) {
			return PermissionLbPair(programID, base, a, b, 25)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forward, bumpF, errF := tt.call(mintA, mintB)
			if errF != nil {
				t.Fatalf("forward call: %v", errF)
			}

			reversed, bumpR, errR := tt.call(mintB, mintA)
			if errR != nil {
				t.Fatalf("reversed call: %v", errR)
			}

			if forward != reversed {
				t.Errorf("mint order changed the address: %s vs %s", forward, reversed)
			}

			if bumpF != bumpR {
				t.Errorf("mint order changed the bump: %d vs %d", bumpF, bumpR)
			}
		})
	}
}

func TestDerivationsAreDeterministic(t *testing.T) {
	t.Parallel()

	type call struct {
		name string
		fn   func() (solana.PublicKey, uint8, error)
	}

	calls := []call{
		{"LbPair", func() (solana.PublicKey, uint8, error) { return LbPair(programID, mintA, mintB, 25) }},
		{"LbPairV2", func() (solana.PublicKey, uint8, error) { return LbPairV2(programID, mintA, mintB, 25, 10000) }},
		{"Position", func() (solana.PublicKey, uint8, error) {
			return Position(programID, mintA, base, -1234, 70)
		}},
		{"Oracle", func() (solana.PublicKey, uint8, error) { return Oracle(programID, mintA) }},
		{"BinArray", func() (solana.PublicKey, uint8, error) { return BinArray(programID, mintA, -517) }},
		{"BinArrayBitmapExtension", func() (solana.PublicKey, uint8, error) {
			return BinArrayBitmapExtension(programID, mintA)
		}},
		{"Reserve", func() (solana.PublicKey, uint8, error) { return Reserve(programID, mintA, mintB) }},
		{"RewardVault", func() (solana.PublicKey, uint8, error) { return RewardVault(programID, mintA, 1) }},
		{"EventAuthority", func() (solana.PublicKey, uint8, error) { return EventAuthority(programID) }},
		{"PresetParameter", func() (solana.PublicKey, uint8, error) { return PresetParameter(programID, 25) }},
		{"PresetParameter2", func() (solana.PublicKey, uint8, error) { return PresetParameter2(programID, 25, 10000) }},
		{"PresetParameter2ByIndex", func() (solana.PublicKey, uint8, error) {
			return PresetParameter2ByIndex(programID, 3)
		}},
		{"TokenBadge", func() (solana.PublicKey, uint8, error) { return TokenBadge(programID, mintA) }},
		{"ClaimProtocolFeeOperator", func() (solana.PublicKey, uint8, error) {
			return ClaimProtocolFeeOperator(programID, base)
		}},
		{"Operator", func() (solana.PublicKey, uint8, error) { return Operator(programID, base) }},
		{"ILMBaseKey", func() (solana.PublicKey, uint8, error) {
			return CustomizablePermissionlessLbPair(programID, mintA, mintB)
		}},
	}

	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			first, bump1, err := c.fn()
			if err != nil {
				t.Fatalf("first call: %v", err)
			}

			second, bump2, err := c.fn()
			if err != nil {
				t.Fatalf("second call: %v", err)
			}

			if first != second || bump1 != bump2 {
				t.Errorf("not deterministic: (%s, %d) vs (%s, %d)", first, bump1, second, bump2)
			}
		})
	}
}

func TestDerivationsAreDistinctAcrossInputs(t *testing.T) {
	t.Parallel()

	// Distinct seeds must not collide. This catches a missing seed component,
	// which would make two different entities share one address.
	lbA, _, err := LbPair(programID, mintA, mintB, 25)
	if err != nil {
		t.Fatalf("LbPair: %v", err)
	}

	lbB, _, err := LbPair(programID, mintA, mintB, 26)
	if err != nil {
		t.Fatalf("LbPair: %v", err)
	}

	if lbA == lbB {
		t.Error("different bin steps produced the same pool address")
	}

	// A PDA is by definition off the ed25519 curve: find_program_address keeps
	// decrementing the bump until the hash is not a valid curve point. Checking
	// this is meaningful, unlike asserting a uint8 is below 256.
	if lbA.IsOnCurve() {
		t.Error("derived pool address is on the ed25519 curve, so it is not a PDA")
	}

	if lbB.IsOnCurve() {
		t.Error("derived pool address is on the ed25519 curve, so it is not a PDA")
	}

	ba1, _, err := BinArray(programID, lbA, 0)
	if err != nil {
		t.Fatalf("BinArray: %v", err)
	}

	ba2, _, err := BinArray(programID, lbA, 1)
	if err != nil {
		t.Fatalf("BinArray: %v", err)
	}

	baNeg, _, err := BinArray(programID, lbA, -1)
	if err != nil {
		t.Fatalf("BinArray: %v", err)
	}

	if ba1 == ba2 {
		t.Error("bin array indexes 0 and 1 produced the same address")
	}

	if ba1 == baNeg {
		t.Error("bin array indexes 0 and -1 produced the same address")
	}

	if ba2 == baNeg {
		t.Error("bin array indexes 1 and -1 produced the same address")
	}

	// A different pool must not share bin arrays with lbA.
	baOther, _, err := BinArray(programID, lbB, 1)
	if err != nil {
		t.Fatalf("BinArray: %v", err)
	}

	if ba2 == baOther {
		t.Error("bin arrays for different pools collided")
	}
}

func TestProgramIDChangesTheAddress(t *testing.T) {
	t.Parallel()

	other := solana.MustPublicKeyFromBase58("zapvX9M3uf5pvy4wRPAbQgdQsM1xmuiFnkfHKPvwMiz")

	a, _, err := LbPair(programID, mintA, mintB, 25)
	if err != nil {
		t.Fatalf("LbPair: %v", err)
	}

	b, _, err := LbPair(other, mintA, mintB, 25)
	if err != nil {
		t.Fatalf("LbPair: %v", err)
	}

	if a == b {
		t.Error("the same seeds under different program IDs produced the same address")
	}
}

func TestScalarEncodersAreLittleEndian(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  []byte
		want []byte
	}{
		{"u16 1", u16LE(1), []byte{0x01, 0x00}},
		{"u16 258", u16LE(258), []byte{0x02, 0x01}},
		{"u16 max", u16LE(0xffff), []byte{0xff, 0xff}},
		{"u32 1", u32LE(1), []byte{0x01, 0x00, 0x00, 0x00}},
		{"i32 minus one", i32LE(-1), []byte{0xff, 0xff, 0xff, 0xff}},
		{"i32 min", i32LE(-2147483648), []byte{0x00, 0x00, 0x00, 0x80}},
		{"i32 two's complement", i32LE(-2), []byte{0xfe, 0xff, 0xff, 0xff}},
		{"u64 1", u64LE(1), []byte{1, 0, 0, 0, 0, 0, 0, 0}},
		{"i64 minus one", i64LE(-1), []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !bytes.Equal(tt.got, tt.want) {
				t.Errorf("got %x, want %x", tt.got, tt.want)
			}
		})
	}
}

func TestScalarEncodersRoundTrip(t *testing.T) {
	t.Parallel()

	// Negative bin IDs are normal, so the signed encoders must survive a
	// round trip through their unsigned width.
	for _, v := range []int32{0, 1, -1, 70, -70, -71, 2147483647, -2147483648} {
		got := int32(binary.LittleEndian.Uint32(i32LE(v)))
		if got != v {
			t.Errorf("i32LE round trip: %d -> %d", v, got)
		}
	}

	for _, v := range []int64{0, 1, -1, 517, -517, 9223372036854775807, -9223372036854775808} {
		got := int64(binary.LittleEndian.Uint64(i64LE(v)))
		if got != v {
			t.Errorf("i64LE round trip: %d -> %d", v, got)
		}
	}
}

func TestILMBaseKeyIsStableAndCopied(t *testing.T) {
	t.Parallel()

	first := ILMBaseKey()
	if first.IsZero() {
		t.Fatal("ILMBaseKey is the zero key")
	}

	// Mutating the returned value must not affect the package constant.
	first[0] ^= 0xff
	if second := ILMBaseKey(); second == first {
		t.Error("ILMBaseKey returned a reference to internal state rather than a copy")
	}
}

func TestPositionWidthAndLowerBinAreBothCommitted(t *testing.T) {
	t.Parallel()

	// lower_bin_id and width are separate seeds; omitting either would make
	// two genuinely different positions resolve to one address.
	base1, _, err := Position(programID, mintA, base, 10, 70)
	if err != nil {
		t.Fatalf("Position: %v", err)
	}

	base2, _, err := Position(programID, mintA, base, 11, 70)
	if err != nil {
		t.Fatalf("Position: %v", err)
	}

	base3, _, err := Position(programID, mintA, base, 10, 71)
	if err != nil {
		t.Fatalf("Position: %v", err)
	}

	if base1 == base2 {
		t.Error("changing lower_bin_id did not change the address")
	}

	if base1 == base3 {
		t.Error("changing width did not change the address")
	}
}

func TestEveryDerivationIsOffCurve(t *testing.T) {
	t.Parallel()

	// The defining property of a program-derived address. If any of these were
	// on the curve it would mean the seed set is wrong: find_program_address
	// only returns a value once the hash fails the curve check.
	type call struct {
		name string
		fn   func() (solana.PublicKey, uint8, error)
	}

	calls := []call{
		{"LbPair", func() (solana.PublicKey, uint8, error) { return LbPair(programID, mintA, mintB, 25) }},
		{"LbPairV2", func() (solana.PublicKey, uint8, error) { return LbPairV2(programID, mintA, mintB, 25, 10000) }},
		{"LbPairWithPreset", func() (solana.PublicKey, uint8, error) {
			return LbPairWithPreset(programID, base, mintA, mintB)
		}},
		{"CustomizablePermissionlessLbPair", func() (solana.PublicKey, uint8, error) {
			return CustomizablePermissionlessLbPair(programID, mintA, mintB)
		}},
		{"PermissionLbPair", func() (solana.PublicKey, uint8, error) {
			return PermissionLbPair(programID, base, mintA, mintB, 25)
		}},
		{"Position", func() (solana.PublicKey, uint8, error) {
			return Position(programID, mintA, base, -1234, 70)
		}},
		{"Oracle", func() (solana.PublicKey, uint8, error) { return Oracle(programID, mintA) }},
		{"BinArray", func() (solana.PublicKey, uint8, error) { return BinArray(programID, mintA, -517) }},
		{"BinArrayBitmapExtension", func() (solana.PublicKey, uint8, error) {
			return BinArrayBitmapExtension(programID, mintA)
		}},
		{"Reserve", func() (solana.PublicKey, uint8, error) { return Reserve(programID, mintA, mintB) }},
		{"RewardVault", func() (solana.PublicKey, uint8, error) { return RewardVault(programID, mintA, 1) }},
		{"EventAuthority", func() (solana.PublicKey, uint8, error) { return EventAuthority(programID) }},
		{"PresetParameter", func() (solana.PublicKey, uint8, error) { return PresetParameter(programID, 25) }},
		{"PresetParameter2", func() (solana.PublicKey, uint8, error) { return PresetParameter2(programID, 25, 10000) }},
		{"PresetParameter2ByIndex", func() (solana.PublicKey, uint8, error) {
			return PresetParameter2ByIndex(programID, 3)
		}},
		{"TokenBadge", func() (solana.PublicKey, uint8, error) { return TokenBadge(programID, mintA) }},
		{"ClaimProtocolFeeOperator", func() (solana.PublicKey, uint8, error) {
			return ClaimProtocolFeeOperator(programID, base)
		}},
		{"Operator", func() (solana.PublicKey, uint8, error) { return Operator(programID, base) }},
	}

	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			addr, _, err := c.fn()
			if err != nil {
				t.Fatalf("derive: %v", err)
			}

			if addr.IsOnCurve() {
				t.Errorf("%s is on the ed25519 curve, so it is not a valid PDA", addr)
			}
		})
	}
}

// TestGoldenAddresses is deliberately skipped.
//
// A PDA is the result of an ed25519 curve check, so it cannot be computed in
// the test itself and compared against the implementation without the oracle
// and the implementation being the same thing. Real confirmation needs an
// external source: the value from the Meteora TypeScript SDK's deriveBinArray,
// or an address fetched from RPC for a known pool.
//
// The gap is recorded here rather than hidden, because the property tests above
// prove internal consistency but cannot prove the seeds themselves are right.
func TestGoldenAddresses(t *testing.T) {
	t.Parallel()
	t.Skip("no golden PDA values yet; needs an external oracle (TS SDK or RPC)")

	// Fill in as table entries of the form:
	//   {name, got, want}
	// where want comes from the TypeScript SDK or from a live account lookup.
}
