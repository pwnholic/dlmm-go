package uint256

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/pwnholic/dlmm-go/num"
)

// ErrDivideByZero reports a division by a zero denominator.
var ErrDivideByZero = errors.New("uint256: division by zero")

// Div256By128 divides n by d, returning the quotient and the remainder.
//
// IMPLEMENTATION NOTE — deliberate, not accidental.
//
// The obvious hand-written approach is Knuth Algorithm D, about a hundred lines
// of carry and borrow manipulation. It is not used here on purpose: this
// operation's correctness is load-bearing for swap quoting, and a hand-rolled
// version is exactly the kind of code that looks authoritative while being
// subtly wrong in a case nobody tests. math/big is correct by construction and
// gives the same answer as the Rust reference by definition.
//
// The cost is allocation on every call. That is accepted for now and the API
// is deliberately stable so a hand-written kernel can replace the body later
// without touching any caller. The decision to do that is a benchmark
// decision, not a guess: run TestDivBenchmark, and only then consider
// Knuth D or reciprocal multiplication.
//
// The hot path in DLMM is MulShr, which needs no division at all.
func Div256By128(n U256, d num.U128) (q, r num.U128, err error) {
	if d.IsZero() {
		return num.U128{}, num.U128{}, ErrDivideByZero
	}

	nBig := n.bigInt()
	dBig := new(big.Int).SetBytes(bytesBE128(d))

	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(nBig, dBig, remainder)

	q, err = fromBigInt(quotient)
	if err != nil {
		return num.U128{}, num.U128{}, fmt.Errorf("uint256: quotient: %w", err)
	}

	r, err = fromBigInt(remainder)
	if err != nil {
		return num.U128{}, num.U128{}, fmt.Errorf("uint256: remainder: %w", err)
	}

	return q, r, nil
}

// DivCeil256By128 returns ceil(n / d).
func DivCeil256By128(n U256, d num.U128) (num.U128, error) {
	q, r, err := Div256By128(n, d)
	if err != nil {
		return num.U128{}, err
	}

	if r.IsZero() {
		return q, nil
	}

	return U256FromU128(q).Add1().ToU128()
}

// bigInt converts n to a *big.Int using its big-endian byte form.
//
// This is a test-and-division helper, not part of the arithmetic hot path.
func (n U256) bigInt() *big.Int {
	b := n.BytesBE()
	return new(big.Int).SetBytes(b[:])
}

// bytesBE128 returns the 16-byte big-endian encoding: the high half first.
//
// This uses encoding/binary rather than a hand-rolled byte loop. The first
// version used a loop and wrote Lo into the high half, which made every divisor
// 2^64 times too large and silently turned correct divisions into zero.
func bytesBE128(v num.U128) []byte {
	var b [16]byte
	binary.BigEndian.PutUint64(b[0:8], v.Hi)
	binary.BigEndian.PutUint64(b[8:16], v.Lo)

	return b[:]
}

// fromBigInt narrows a non-negative big.Int that fits in 128 bits.
//
// The byte order matters and was wrong in the first version: v.Bytes() is
// big-endian, so of the 16 right-aligned bytes the FIRST eight are the high
// half. Assigning those to Lo silently shifts every converted value by 2^64,
// which turned every successful division into a wrong answer.
func fromBigInt(v *big.Int) (num.U128, error) {
	if v.Sign() < 0 {
		return num.U128{}, fmt.Errorf("%w: negative value %s", ErrOverflow, v)
	}

	if v.BitLen() > 128 {
		return num.U128{}, fmt.Errorf("%w: %s exceeds 128 bits", ErrOverflow, v)
	}

	// Right-align the minimal big-endian representation in 16 bytes.
	var padded [16]byte

	src := v.Bytes()
	copy(padded[16-len(src):], src)

	return num.U128{
		Lo: beUint64(padded[8:16]), // low eight bytes
		Hi: beUint64(padded[0:8]),  // high eight bytes
	}, nil
}

func beUint64(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}

	return v
}
