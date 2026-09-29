package num

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"testing"

	bin "github.com/gagliardetto/binary"
)

// oracle converts a U128 to *big.Int. Tests use it as an independent
// implementation so that a bug in BigInt() cannot hide a bug elsewhere.
func oracle(u U128) *big.Int {
	return new(big.Int).Add(
		new(big.Int).Lsh(new(big.Int).SetUint64(u.Hi), 64),
		new(big.Int).SetUint64(u.Lo),
	)
}

func TestU128BitsAreTheExpectedWidth(t *testing.T) {
	t.Parallel()
	// The whole SDK assumes 16 bytes on the wire. Pin that assumption.
	var u U128
	if got := len(u.BytesLE()); got != 16 {
		t.Fatalf("encoded width = %d, want 16", got)
	}
}

func TestCmp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b U128
		want int
	}{
		{"equal zero", U128{}, U128{}, 0},
		{"equal nonzero", U128{Lo: 7, Hi: 3}, U128{Lo: 7, Hi: 3}, 0},
		{"lo decides", U128{Lo: 1, Hi: 5}, U128{Lo: 2, Hi: 5}, -1},
		{"hi decides", U128{Lo: 9, Hi: 5}, U128{Lo: 0, Hi: 6}, -1},
		{"hi dominates lo", U128{Lo: math.MaxUint64, Hi: 1}, U128{Lo: 0, Hi: 2}, -1},
		{"max vs zero", MaxU128, U128{}, +1},
		{"zero vs max", U128{}, MaxU128, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.a.Cmp(tt.b); got != tt.want {
				t.Errorf("Cmp(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.want)
			}

			if got := tt.b.Cmp(tt.a); got != -tt.want {
				t.Errorf("Cmp is not antisymmetric: got %d, want %d", got, -tt.want)
			}
		})
	}
}

func TestIsZeroAndIsU64AreConsistentWithBigInt(t *testing.T) {
	t.Parallel()

	values := []U128{
		{},
		{Lo: 1},
		{Hi: 1},
		MaxU128,
		{Lo: math.MaxUint64},
		{Hi: math.MaxUint64},
	}
	for _, u := range values {
		if got, want := u.IsZero(), oracle(u).Sign() == 0; got != want {
			t.Errorf("IsZero(%v) = %v, want %v", u, got, want)
		}

		if got, want := u.IsU64(), oracle(u).BitLen() <= 64; got != want {
			t.Errorf("IsU64(%v) = %v, want %v", u, got, want)
		}
	}
}

func TestU128IsComparableAndUsableAsMapKey(t *testing.T) {
	t.Parallel()
	// The design relies on U128 being a pure value. If a non-comparable field
	// is ever added, this fails to compile rather than silently changing
	// equality semantics.
	m := map[U128]string{
		{Lo: 1}:        "one",
		{Lo: 1, Hi: 1}: "2^64+1",
		{Lo: 1}:        "duplicate key must overwrite",
	}
	if len(m) != 2 {
		t.Fatalf("map has %d entries, want 2", len(m))
	}

	if got := m[U128{Lo: 1}]; got != "duplicate key must overwrite" {
		t.Errorf("map lookup = %q, want the last write", got)
	}
}

func TestBigIntRoundTripAgainstOracle(t *testing.T) {
	t.Parallel()

	values := []U128{
		{},
		{Lo: 1},
		{Lo: math.MaxUint64},
		{Hi: 1},
		{Lo: 1, Hi: 1},
		MaxU128,
		{Lo: 0x0123456789abcdef, Hi: 0xfedcba9876543210},
	}
	for _, u := range values {
		want := oracle(u)
		if got := u.BigInt(); got.Cmp(want) != 0 {
			t.Errorf("BigInt(%v) = %s, want %s", u, got, want)
		}

		back, err := U128FromBigInt(want)
		if err != nil {
			t.Fatalf("U128FromBigInt(%s) error = %v", want, err)
		}

		if back != u {
			t.Errorf("round trip: got %v, want %v", back, u)
		}
	}
}

func TestU128FromBigIntRejectsOutOfRange(t *testing.T) {
	t.Parallel()

	over := new(big.Int).Lsh(big.NewInt(1), 128)

	tests := []struct {
		name string
		in   *big.Int
	}{
		{"negative", big.NewInt(-1)},
		{"exactly 2^128", over},
		{"far above 2^128", new(big.Int).Lsh(big.NewInt(1), 4096)},
		{"nil", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := U128FromBigInt(tt.in); err == nil {
				t.Fatalf("U128FromBigInt(%v) succeeded, want error", tt.in)
			}
		})
	}
}

func TestBytesLEMatchesBigIntEncoding(t *testing.T) {
	t.Parallel()

	values := []U128{
		{},
		{Lo: 1},
		{Hi: 1},
		MaxU128,
		{Lo: 0x0123456789abcdef, Hi: 0xfedcba9876543210},
	}
	for _, u := range values {
		got := u.BytesLE()

		// Independent expectation: big.Int is big-endian, so reverse it.
		b := u.BigInt().Bytes()

		var want [16]byte
		for i := range b {
			want[i] = b[len(b)-1-i]
		}

		if got != want {
			t.Errorf("BytesLE(%v) = %x, want %x", u, got, want)
		}

		if back := U128FromBytesLE(got); back != u {
			t.Errorf("BytesLE round trip: got %v, want %v", back, u)
		}
	}
}

func TestBorshRoundTripThroughDecoder(t *testing.T) {
	t.Parallel()

	values := []U128{
		{},
		{Lo: 1},
		{Hi: 1},
		MaxU128,
		{Lo: 0xdeadbeefcafebabe, Hi: 0x0123456789abcdef},
	}
	for _, u := range values {
		var buf bytes.Buffer

		enc := bin.NewBorshEncoder(&buf)
		if err := u.MarshalWithEncoder(enc); err != nil {
			t.Fatalf("marshal %v: %v", u, err)
		}

		if got := buf.Len(); got != 16 {
			t.Fatalf("marshalled %d bytes, want 16", got)
		}

		var back U128

		dec := bin.NewBorshDecoder(buf.Bytes())
		if err := back.UnmarshalWithDecoder(dec); err != nil {
			t.Fatalf("unmarshal %v: %v", u, err)
		}

		if back != u {
			t.Errorf("round trip: got %v, want %v", back, u)
		}
	}
}

func TestUnmarshalRejectsShortInput(t *testing.T) {
	t.Parallel()

	short := make([]byte, 15)

	var u U128

	dec := bin.NewBorshDecoder(short)
	if err := u.UnmarshalWithDecoder(dec); err == nil {
		t.Fatal("decoding 15 bytes succeeded, want error")
	}
}

func TestUnmarshalRejectsNilReceiver(t *testing.T) {
	t.Parallel()

	var u *U128

	dec := bin.NewBorshDecoder(make([]byte, 16))
	if err := u.UnmarshalWithDecoder(dec); err == nil {
		t.Fatal("nil receiver succeeded, want error")
	}
}

func TestWireFormatIsExactly16BytesLittleEndian(t *testing.T) {
	t.Parallel()
	// Pins the on-chain wire format byte-for-byte. A field-layout change that
	// the codec's reflection fallback would happily follow must fail here.
	tests := []struct {
		name string
		in   U128
		want string
	}{
		{"zero", U128{}, "00000000000000000000000000000000"},
		{"one", U128FromU64(1), "01000000000000000000000000000000"},
		{"low limb only", U128FromU64(^uint64(0)), "ffffffffffffffff0000000000000000"},
		{"hi limb only", U128{Hi: 1}, "0000000000000000 0100000000000000"[0:16] + "0100000000000000"},
		{"max", MaxU128, "ffffffffffffffffffffffffffffffff"},
		{
			"mixed",
			U128{Lo: 0x0123456789abcdef, Hi: 0xfedcba9876543210},
			"efcdab89674523011032547698badcfe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := fmt.Sprintf("%x", tt.in.BytesLE()); got != tt.want {
				t.Errorf("BytesLE = %s, want %s", got, tt.want)
			}

			// The borsh encoder must agree with BytesLE exactly, so the codec is
			// pinned to the same 16-byte little-endian contract.
			var buf bytes.Buffer
			if err := tt.in.MarshalWithEncoder(bin.NewBorshEncoder(&buf)); err != nil {
				t.Fatalf("marshal: %v", err)
			}

			if got := hex.EncodeToString(buf.Bytes()); got != tt.want {
				t.Errorf("borsh bytes = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNewU128ArgumentOrderIsHighThenLow(t *testing.T) {
	t.Parallel()
	// Pins the documented argument order so a future refactor cannot silently
	// swap the limbs.
	u := NewU128(2, 1)
	if u.Hi != 2 || u.Lo != 1 {
		t.Fatalf("NewU128(2,1) = %+v, want Hi=2 Lo=1", u)
	}

	want := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(2), 64), big.NewInt(1))
	if got := u.BigInt(); got.Cmp(want) != 0 {
		t.Errorf("NewU128(2,1) = %s, want %s", got, want)
	}
}

func FuzzU128BorshRoundTrip(f *testing.F) {
	f.Add(uint64(0), uint64(0))
	f.Add(uint64(1), uint64(0))
	f.Add(uint64(1<<63), uint64(1<<63))
	f.Add(^uint64(0), ^uint64(0))

	f.Fuzz(func(t *testing.T, lo, hi uint64) {
		u := U128{Lo: lo, Hi: hi}

		var buf bytes.Buffer

		enc := bin.NewBorshEncoder(&buf)
		if err := u.MarshalWithEncoder(enc); err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var back U128

		dec := bin.NewBorshDecoder(buf.Bytes())
		if err := back.UnmarshalWithDecoder(dec); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if back != u {
			t.Fatalf("round trip: got %+v, want %+v", back, u)
		}

		if u.BigInt().Cmp(oracle(u)) != 0 {
			t.Fatalf("BigInt disagrees with oracle for %+v", u)
		}
	})
}
