#!/usr/bin/env python3
"""Generate golden vectors for the SDK's fixed-point arithmetic.

Why this exists
---------------
The arithmetic under test (``num``, ``internal/uint256`` and ``math``) is
load-bearing for swap quoting, and its existing tests compare the Go code
against ``math/big`` from inside Go. That is a good check, but both sides live in
the same repository, use the same limb layout, and were written by the same hand:
a shared misunderstanding of "which limb is low" or of what rounding up means
would cancel itself out and the tests would still pass.

So the expected values here are produced by a different language, with different
integer semantics (arbitrary precision instead of explicit limbs) and a different
algorithm (``//``, ``divmod`` and ``-(-a // b)`` instead of wide multiplication
with carries). Python is the oracle; the Go test files are the consumers.

The vectors are committed rather than generated during ``go test`` so that a
changed expectation shows up as a reviewable diff instead of as a green test run
that silently regenerated itself.

Output
------
``testdata/vectors/numeric.json``, read by ``num/vectors_test.go`` (num and
internal/uint256 operations) and ``math/vectors_test.go`` (MulDiv, MulShr,
ShlDiv).

JSON shape, one object per case::

    {
      "op": "<operation>",                       # what the case targets
      "x", "y", "d", "n", "denominator": <int>,  # operands, per op
      "offset", "shift": <int>,                  # shift amount, where applicable
      "rounding": "down" | "up",                 # where applicable
      "want": <int> | null,                      # null exactly when want_error is set
      "want_rem": <int> | null,                  # div256_by128 only
      "want_error": "overflow" | "divide_by_zero" | "offset_too_large" | null
    }

An ``<int>`` is an object, never a bare JSON number::

    u128: {"dec": "340282366920938463463374607431768211455", "lo": "...", "hi": "..."}
    u256: {"dec": "...", "l0": "...", "l1": "...", "l2": "...", "l3": "..."}

The limbs are present so the Go test can build the value with no parsing at all,
and the decimal string is present so the test can check that the limbs mean what
everyone thinks they mean (``lo`` is the least significant 64 bits). Without that
cross-check a transposed limb order would agree on both sides and every other
assertion would still pass. Limbs are decimal strings rather than JSON numbers so
no value ever has to survive a float64 round trip in a parser.

Semantics encoded here (from the Rust reference in
``package-referense/dlmm-sdk/commons/src/math`` and the documented Go behaviour):

* rounding down truncates, rounding up is the ceiling; both directions are the
  same because the operands are unsigned.
* a zero denominator is an error (``u128x128_math.rs`` returns None).
* an offset of 128 or more is an error, mirroring ``1u128.checked_shl``.
* a result that does not fit in 128 bits is an error, never a wrapped value.
* ``uint256.Shr``/``Shl`` truncate, and a shift of 256 or more yields zero as
  documented in ``internal/uint256/uint256.go``.

Because of the offset rule, MulShr and ShlDiv with an offset of 128 or more never
reach the divide-by-zero rule; the vectors assert the precedence, since that
ordering is a decision a caller could observe.

The script re-checks its own arithmetic where an invariant exists (most notably
``q*d + r == n`` and ``r < d`` for every Div256By128 vector) and exits non-zero if
any check fails.

Usage::

    python3 tools/gen_vectors/gen_numeric_vectors.py

Standard library only. The RNG is SplitMix64 as implemented below rather than
``random.Random``: only ``random()`` has a documented stability guarantee across
Python versions, and regenerating this file must be byte-identical anywhere.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
OUT_PATH = ROOT / "testdata" / "vectors" / "numeric.json"

GENERATED_BY = "tools/gen_vectors/gen_numeric_vectors.py"
SEED = 20240929

MASK64 = (1 << 64) - 1
MAX_U128 = (1 << 128) - 1
MAX_U256 = (1 << 256) - 1

# The largest offset math.MulShr and math.ShlDiv accept: the Rust reference builds
# the divisor with 1u128.checked_shl(offset), which is None at 128 or above.
MAX_SHIFT_OFFSET = 127

# The offsets the mission requires, plus the two that must be rejected.
VALID_OFFSETS = (0, 1, 7, 31, 32, 63, 64, 65, 100, 127)
INVALID_OFFSETS = (128, 129)

# uint256.Shr/Shl shifts. Beyond the math offsets this list covers the limb
# boundaries (191, 192, 193, 255) and the documented "256 or more yields zero".
U256_SHIFTS = (0, 1, 7, 31, 32, 63, 64, 65, 100, 127, 128, 129, 191, 192, 193, 255, 256, 300)

ROUNDINGS = ("down", "up")


def fail(message: str) -> None:
    """Abort: a violated assumption must never become a vector."""
    raise SystemExit(f"gen_numeric_vectors: {message}")


def require(condition: bool, message: str) -> None:
    """fail() when condition is false. Used for every load-bearing self-check."""
    if not condition:
        fail(message)


def ceil_div(a: int, b: int) -> int:
    """ceil(a / b) for positive b, written the way Python allows."""
    return -(-a // b)


class SplitMix64:
    """A fixed, self-contained 64-bit generator.

    Explicit rather than ``random.Random`` so that a Python upgrade cannot
    silently change the pseudo-random vectors: only ``random()`` is documented as
    stable across versions, and this file is meant to regenerate identically.
    """

    def __init__(self, seed: int) -> None:
        self.state = seed & MASK64

    def next_u64(self) -> int:
        self.state = (self.state + 0x9E3779B97F4A7C15) & MASK64
        z = self.state
        z = ((z ^ (z >> 30)) * 0xBF58476D1CE4E5B9) & MASK64
        z = ((z ^ (z >> 27)) * 0x94D049BB133111EB) & MASK64
        return (z ^ (z >> 31)) & MASK64

    def bits(self, width: int) -> int:
        """An unsigned value of at most `width` bits, width in [1, 128]."""
        require(1 <= width <= 128, f"bits({width}) out of range")
        chunks = 0
        value = 0
        while chunks < width:
            value = (value << 64) | self.next_u64()
            chunks += 64
        return value >> (chunks - width)

    def pick(self, items: tuple) -> object:
        return items[self.next_u64() % len(items)]

    def below(self, bound: int) -> int:
        """A value in [0, bound) for bound up to 2^64."""
        require(0 < bound <= 1 << 64, f"below({bound}) out of range")
        return self.bits(bound.bit_length()) % bound


# --------------------------------------------------------------------------- #
# JSON encoding helpers
# --------------------------------------------------------------------------- #


def u128_json(value: int) -> dict:
    """A u128 as a decimal string plus its two limbs, low first."""
    require(0 <= value <= MAX_U128, f"u128 out of range: {value}")
    return {"dec": str(value), "lo": str(value & MASK64), "hi": str(value >> 64)}


def u256_json(value: int) -> dict:
    """A u256 as a decimal string plus its four limbs, low first."""
    require(0 <= value <= MAX_U256, f"u256 out of range: {value}")
    return {
        "dec": str(value),
        "l0": str(value & MASK64),
        "l1": str((value >> 64) & MASK64),
        "l2": str((value >> 128) & MASK64),
        "l3": str((value >> 192) & MASK64),
    }


CASES: list[dict] = []


def emit(op: str, **fields: object) -> None:
    """Append one case. `op` is written first so each JSON line reads in order."""
    case: dict = {"op": op}
    case.update(fields)
    CASES.append(case)


# --------------------------------------------------------------------------- #
# Expected values, one function per operation
#
# Each returns (value, error), with error None when the operation succeeds. The
# formulas are the definitions, not a translation of the Go code: quotient and
# remainder come from divmod, the ceiling from -(-a // b).
# --------------------------------------------------------------------------- #


def expect_mul_div(x: int, y: int, denominator: int, rounding: str) -> tuple[int | None, str | None]:
    """(x * y) / denominator, rounded."""
    if denominator == 0:
        return None, "divide_by_zero"

    product = x * y
    quotient = product // denominator if rounding == "down" else ceil_div(product, denominator)

    if rounding == "down":
        # Cross-check the truncation against the definition: the quotient must
        # bracket the product.
        require(
            quotient * denominator <= product < (quotient + 1) * denominator,
            f"mul_div self-check failed for ({x}, {y}, {denominator}, {rounding})",
        )

    if quotient > MAX_U128:
        return None, "overflow"
    return quotient, None


def expect_mul_shr(x: int, y: int, offset: int, rounding: str) -> tuple[int | None, str | None]:
    """(x * y) >> offset, rounded."""
    if offset > MAX_SHIFT_OFFSET:
        return None, "offset_too_large"

    product = x * y

    if offset == 0:
        # No bit is discarded, so rounding up has nothing to round and the result
        # is the product itself. (In Go, HasBitsBelow(0) is false.)
        shifted, discarded = product, False
    else:
        shifted, remainder = divmod(product, 1 << offset)
        # Cross-check against the shift, which is a different formulation.
        require(shifted == product >> offset, f"shift self-check failed for ({x}, {y}, {offset})")
        discarded = remainder != 0

    if rounding == "up" and discarded:
        shifted += 1

    if shifted > MAX_U128:
        return None, "overflow"
    return shifted, None


def expect_shl_div(x: int, y: int, offset: int, rounding: str) -> tuple[int | None, str | None]:
    """(x << offset) / y, rounded."""
    if offset > MAX_SHIFT_OFFSET:
        return None, "offset_too_large"
    if y == 0:
        return None, "divide_by_zero"

    # x is at most 128 bits and the offset at most 127, so the shifted numerator
    # needs at most 255 bits and cannot be lost in the 256-bit form.
    numerator = x << offset
    require(numerator <= MAX_U256, f"shl_div numerator does not fit 256 bits: {x} << {offset}")

    quotient = numerator // y if rounding == "down" else ceil_div(numerator, y)

    if quotient > MAX_U128:
        return None, "overflow"
    return quotient, None


def expect_div256_by128(n: int, d: int) -> tuple[int | None, int | None, str | None]:
    """n / d as (quotient, remainder, error), with the Euclidean identity checked."""
    require(0 <= n <= MAX_U256, f"u256 out of range: {n}")
    require(0 <= d <= MAX_U128, f"u128 out of range: {d}")

    if d == 0:
        return None, None, "divide_by_zero"

    quotient, remainder = divmod(n, d)

    # The identity holds however the division is implemented, so a disagreement
    # here means Python itself is being misused, not that the Go code is wrong.
    require(quotient * d + remainder == n, f"q*d + r != n for ({n}, {d})")
    require(remainder < d, f"remainder >= divisor for ({n}, {d})")

    if quotient > MAX_U128:
        return None, None, "overflow"
    return quotient, remainder, None


def expect_divceil256_by128(n: int, d: int) -> tuple[int | None, str | None]:
    """ceil(n / d) as (value, error).

    DivCeil256By128 is floor-then-increment, so it reports overflow both when the
    truncated quotient is already too wide and when the increment is the value
    that exceeds 128 bits. Both collapse to "the ceiling does not fit", which is
    what the identity below asserts.
    """
    quotient, remainder, err = expect_div256_by128(n, d)
    if err is not None:
        # A zero divisor, or a truncated quotient that already exceeds 128 bits:
        # the ceiling of that is not representable either.
        return None, err

    assert quotient is not None and remainder is not None
    ceiling = quotient + (1 if remainder else 0)
    if ceiling > MAX_U128:
        return None, "overflow"
    return ceiling, None


# --------------------------------------------------------------------------- #
# Constructing the "rounding down fits, rounding up overflows" cases
# --------------------------------------------------------------------------- #


def candidate_factors(denominator: int) -> list[int]:
    """The factors searched, in order, by find_round_up_overflow.

    An explicit list rather than a range: the search has to visit a few very
    different shapes of factor (tiny ones for large denominators, ones just above
    the denominator for the powers of two), and a blind range would need millions
    of steps to reach the same place.
    """
    factors: list[int] = list(range(2, 66))
    for delta in (1, 2, 3, 4, 7, 8, 15, 16, 31, 32, 63, 64, 100):
        factors.append(denominator + delta)
    factors.append(2 * denominator + 1)
    factors.append(3 * denominator + 1)
    for shift in range(1, 129):
        factors.append((1 << shift) - 1)
        factors.append((1 << shift) + 1)

    seen: set[int] = set()
    unique: list[int] = []
    for factor in factors:
        if 1 < factor <= MAX_U128 and factor not in seen:
            seen.add(factor)
            unique.append(factor)
    return unique


def find_round_up_overflow(denominator: int) -> tuple[int, int] | None:
    """Find x, y with floor(x*y / denominator) == 2^128-1 and a non-zero remainder.

    This is the only input class where rounding down is representable and
    rounding up is not: the truncated result lands exactly on the u128 ceiling, so
    the ceiling increment is the value that overflows. Every other up-overflow
    also overflows when rounding down, which the ordinary cases already cover.

    Returns None when no witness exists. A found witness is verified with divmod
    before it is returned, so a wrong candidate fails loudly rather than becoming
    a wrong vector.
    """
    target = MAX_U128 * denominator
    for x in candidate_factors(denominator):
        y = ceil_div(target, x)
        if not 0 < y <= MAX_U128:
            continue
        quotient, remainder = divmod(x * y, denominator)
        if quotient == MAX_U128 and remainder != 0:
            return x, y
    return None


def emit_round_up_overflow_mul_div(denominator: int) -> None:
    """The down-fits/up-overflows pair for MulDiv, in both roundings."""
    witness = find_round_up_overflow(denominator)
    require(witness is not None, f"no MulDiv round-up-overflow witness for denominator {denominator}")
    assert witness is not None  # require() already failed loudly; this narrows the type
    x, y = witness

    down, down_err = expect_mul_div(x, y, denominator, "down")
    up, up_err = expect_mul_div(x, y, denominator, "up")
    require(down == MAX_U128 and down_err is None, f"witness is not a round-down success: {x} * {y} / {denominator}")
    require(up is None and up_err == "overflow", f"witness does not overflow on round up: {x} * {y} / {denominator}")

    emit("mul_div", x=u128_json(x), y=u128_json(y), denominator=u128_json(denominator),
         rounding="down", want=u128_json(down), want_error=None)
    emit("mul_div", x=u128_json(x), y=u128_json(y), denominator=u128_json(denominator),
         rounding="up", want=None, want_error="overflow")


def emit_round_up_overflow_mul_shr(offset: int) -> None:
    """The down-fits/up-overflows pair for MulShr, in both roundings.

    MulShr at offset o is division by 2^o, so the same search applies.
    """
    witness = find_round_up_overflow(1 << offset)
    require(witness is not None, f"no MulShr round-up-overflow witness for offset {offset}")
    assert witness is not None
    x, y = witness

    down, down_err = expect_mul_shr(x, y, offset, "down")
    up, up_err = expect_mul_shr(x, y, offset, "up")
    require(down == MAX_U128 and down_err is None, f"witness is not a round-down success at offset {offset}")
    require(up is None and up_err == "overflow", f"witness does not overflow on round up at offset {offset}")

    emit("mul_shr", x=u128_json(x), y=u128_json(y), offset=offset,
         rounding="down", want=u128_json(down), want_error=None)
    emit("mul_shr", x=u128_json(x), y=u128_json(y), offset=offset,
         rounding="up", want=None, want_error="overflow")


# --------------------------------------------------------------------------- #
# Operand pools
# --------------------------------------------------------------------------- #

# Every value the mission requires as an operand: zero, one, the two limb
# boundaries, the required powers of two, and the max u128.
OPERAND_POOL = (
    0,
    1,
    2,
    3,
    7,
    10,
    100,
    1_000_000,
    (1 << 63) - 1,
    1 << 63,
    (1 << 64) - 1,
    1 << 64,
    (1 << 64) + 1,
    (1 << 127) - 1,
    1 << 127,
    MAX_U128,
)

# A smaller pool for the cross products that would otherwise explode in size.
CROSS_POOL = (0, 1, 2, 1 << 63, 1 << 64, 1 << 127, MAX_U128 - 1, MAX_U128)

MUL_DIV_DENOMINATORS = (
    0,
    1,
    2,
    3,
    7,
    10,
    1_000_000,
    1 << 63,
    1 << 64,
    1 << 127,
    MAX_U128,
)

# n values for the 256/128 divisions: zero, one, the powers of two that straddle
# the u128 boundary, max u128, max u256, and a dense bit pattern.
U256_DIVIDENDS = (
    0,
    1,
    2,
    3,
    MAX_U128,
    1 << 63,
    1 << 64,
    1 << 127,
    1 << 128,
    (1 << 128) + 1,
    1 << 192,
    1 << 255,
    (MAX_U128 << 128) | MAX_U128,
    (MAX_U128 << 64) | 12345,
    MAX_U256,
)

DIVISORS = (
    0,
    1,
    2,
    3,
    7,
    10,
    1_000_000,
    (1 << 63) - 1,
    1 << 63,
    (1 << 64) - 1,
    1 << 64,
    1 << 127,
    MAX_U128,
)

# n values for the uint256 shifts. It carries the same zero, one, max u128 and
# powers of two the other operations use, plus the limb boundaries of the wider
# type.
U256_SHIFT_OPERANDS = (
    0,
    1,
    MAX_U128,
    1 << 63,
    1 << 64,
    1 << 127,
    1 << 128,
    1 << 129,
    1 << 191,
    1 << 192,
    1 << 255,
    (1 << 192) - 1,
    (MAX_U128 << 128) | MAX_U128,
    (MAX_U128 << 64) | MAX_U128,
    MAX_U256,
)


# --------------------------------------------------------------------------- #
# Per-operation vector generation
# --------------------------------------------------------------------------- #


def random_u256(rng: SplitMix64, width: int) -> int:
    """A random 256-bit value of at most `width` bits."""
    require(1 <= width <= 256, f"random_u256 width out of range: {width}")
    if width <= 128:
        return rng.bits(width)
    return (rng.bits(width - 128) << 128) | rng.bits(128)


def emit_u128_limb_vectors() -> None:
    """One case per pool value: pins that lo is the low 64 bits of the value.

    The Go test rebuilds the value from the limbs and compares it with the
    decimal string using an integer type that shares no code with num. Every other
    vector would still agree on both sides if the limb order were transposed; this
    is the case that catches it.
    """
    for value in OPERAND_POOL:
        emit("u128_limbs", x=u128_json(value))


def emit_u256_limb_vectors() -> None:
    """The u256 counterpart of emit_u128_limb_vectors."""
    # dict.fromkeys dedupes while keeping the order, so a value that appears in
    # both lists is emitted once.
    for value in dict.fromkeys(U256_SHIFT_OPERANDS + (MAX_U128, (1 << 64) - 1)):
        emit("u256_limbs", n=u256_json(value))


def emit_mul128_to256_vectors(rng: SplitMix64) -> None:
    """The exact 128x128 -> 256 widening product. It cannot overflow."""
    for x in OPERAND_POOL:
        for y in OPERAND_POOL:
            product = x * y
            require(product <= MAX_U256, f"mul128_to256 exceeded 256 bits: {x} * {y}")
            emit("mul128_to256", x=u128_json(x), y=u128_json(y), want=u256_json(product))

    for _ in range(150):
        x = rng.bits(rng.pick((64, 96, 128)))
        y = rng.bits(rng.pick((64, 96, 128)))
        emit("mul128_to256", x=u128_json(x), y=u128_json(y), want=u256_json(x * y))


def emit_u256_shift_vectors(rng: SplitMix64) -> None:
    """Shr and Shl on the 256-bit intermediate.

    Both truncate, and a shift of 256 or more yields zero, which
    internal/uint256/uint256.go documents.
    """

    def one(value: int, shift: int) -> None:
        if shift >= 256:
            right, left = 0, 0
        else:
            right = value >> shift
            require(right == divmod(value, 1 << shift)[0], f"shr self-check failed for ({value}, {shift})")
            left = (value << shift) & MAX_U256
            require(left == divmod(value << shift, 1 << 256)[1], f"shl self-check failed for ({value}, {shift})")

        emit("u256_shr", n=u256_json(value), shift=shift, want=u256_json(right))
        emit("u256_shl", n=u256_json(value), shift=shift, want=u256_json(left))

    for shift in U256_SHIFTS:
        for value in U256_SHIFT_OPERANDS:
            one(value, shift)

    for _ in range(100):
        one(random_u256(rng, rng.pick((64, 128, 192, 256))), rng.below(260))


def emit_div256_by128_vectors(rng: SplitMix64) -> None:
    """The full 256/128 division: quotient and remainder.

    Every case is checked for q*d + r == n and r < d inside expect_div256_by128
    before the vector is written.
    """

    def one(n: int, d: int) -> None:
        quotient, remainder, err = expect_div256_by128(n, d)
        if err is not None:
            emit("div256_by128", n=u256_json(n), d=u128_json(d), want=None, want_rem=None, want_error=err)
            return
        assert quotient is not None and remainder is not None
        emit("div256_by128", n=u256_json(n), d=u128_json(d),
             want=u128_json(quotient), want_rem=u128_json(remainder), want_error=None)

    for n in U256_DIVIDENDS:
        for d in DIVISORS:
            one(n, d)

    # Exact divisions and divisions with a remainder, written out rather than
    # left to the grid so that both are visibly present.
    for n, d in (
        (0, 1),
        (1, 1),
        (2, 2),
        ((1 << 128) - 2, 2),
        (MAX_U128 * 2, 2),
        ((1 << 128) * 3, 3),
        (7, 2),
        (10, 3),
        (MAX_U256, MAX_U128),
        (MAX_U256, 1),
    ):
        one(n, d)

    for _ in range(200):
        n = random_u256(rng, rng.pick((64, 128, 192, 256)))
        # A zero divisor appears often enough to keep that path present.
        d = 0 if rng.next_u64() % 20 == 0 else rng.bits(rng.pick((32, 64, 128)))
        one(n, d)


def emit_divceil256_by128_vectors(rng: SplitMix64) -> None:
    """The ceiling of the same division."""

    def one(n: int, d: int) -> None:
        value, err = expect_divceil256_by128(n, d)
        if err is not None:
            emit("divceil256_by128", n=u256_json(n), d=u128_json(d), want=None, want_error=err)
            return
        assert value is not None
        emit("divceil256_by128", n=u256_json(n), d=u128_json(d), want=u128_json(value), want_error=None)

    for n in U256_DIVIDENDS:
        for d in DIVISORS:
            one(n, d)

    # Exact division must equal truncation; a non-zero remainder adds exactly one.
    for n, d in (
        (0, 1),
        (1, 1),
        (2, 2),
        (7, 2),
        (10, 3),
        (MAX_U128, 1),
        (MAX_U128 * MAX_U128, MAX_U128),
        # floor is max u128 and there is a remainder, so the ceiling is the value
        # that must not be representable.
        (MAX_U128 * 2 + 1, 2),
    ):
        one(n, d)

    for _ in range(200):
        n = random_u256(rng, rng.pick((64, 128, 192, 256)))
        d = 0 if rng.next_u64() % 20 == 0 else rng.bits(rng.pick((32, 64, 128)))
        one(n, d)


def emit_mul_div_vectors(rng: SplitMix64) -> None:
    """(x * y) / denominator, both roundings."""

    def one(x: int, y: int, d: int, rounding: str) -> None:
        value, err = expect_mul_div(x, y, d, rounding)
        if err is not None:
            emit("mul_div", x=u128_json(x), y=u128_json(y), denominator=u128_json(d),
                 rounding=rounding, want=None, want_error=err)
            return
        assert value is not None
        emit("mul_div", x=u128_json(x), y=u128_json(y), denominator=u128_json(d),
             rounding=rounding, want=u128_json(value), want_error=None)

    # Identity-like combinations, so zero, one and the max are each exercised in
    # every operand position.
    for value in OPERAND_POOL:
        for rounding in ROUNDINGS:
            one(value, 1, 1, rounding)
            one(1, value, 1, rounding)
            one(value, value, 1, rounding)
            if value != 0:
                one(value, 1, value, rounding)
                one(1, value, value, rounding)

    for denominator in MUL_DIV_DENOMINATORS:
        for rounding in ROUNDINGS:
            one(MAX_U128, MAX_U128, denominator, rounding)
            one(MAX_U128, 1, denominator, rounding)
            one(1, MAX_U128, denominator, rounding)
            one(MAX_U128, 0, denominator, rounding)
            one(0, 0, denominator, rounding)
            one(0, 1, denominator, rounding)

    # Exact division against a non-power-of-two denominator, the same product with
    # a denominator that leaves a remainder, and a scale by 2^64.
    for rounding in ROUNDINGS:
        one(42, 6, 7, rounding)
        one(6, 7, 42, rounding)
        one(42, 6, 5, rounding)
        one(1 << 100, 1 << 28, 1 << 64, rounding)
        one(MAX_U128, 1, MAX_U128, rounding)
        one(1 << 63, 2, 1 << 64, rounding)
        one(2, (1 << 127) - 1, 3, rounding)

    # floor == max u128 with a non-zero remainder: rounding down fits, rounding up
    # does not. Not constructible for denominator 1 (the division is always exact)
    # or for denominator max u128 (x*y >= 2^128 * (2^128-1) forces x = y = max,
    # which divides exactly); those two are covered above as both-roundings
    # overflow cases.
    for denominator in (2, 3, 7, 10, 1_000_000, 1 << 63, 1 << 64, 1 << 127):
        emit_round_up_overflow_mul_div(denominator)

    for _ in range(250):
        x = rng.bits(rng.pick((16, 32, 64, 128)))
        y = rng.bits(rng.pick((16, 32, 64, 128)))
        d = 0 if rng.next_u64() % 20 == 0 else rng.bits(rng.pick((8, 32, 64, 128)))
        one(x, y, d, rng.pick(ROUNDINGS))


def emit_mul_shr_vectors(rng: SplitMix64) -> None:
    """(x * y) >> offset, both roundings."""

    def one(x: int, y: int, offset: int, rounding: str) -> None:
        value, err = expect_mul_shr(x, y, offset, rounding)
        if err is not None:
            emit("mul_shr", x=u128_json(x), y=u128_json(y), offset=offset,
                 rounding=rounding, want=None, want_error=err)
            return
        assert value is not None
        emit("mul_shr", x=u128_json(x), y=u128_json(y), offset=offset,
             rounding=rounding, want=u128_json(value), want_error=None)

    for offset in VALID_OFFSETS:
        for x in CROSS_POOL:
            for y in (0, 1, 12345, 1 << 63, 1 << 64, 1 << 127, MAX_U128):
                for rounding in ROUNDINGS:
                    one(x, y, offset, rounding)

    # The rejected offsets, with a product small enough that the error can only
    # have come from the offset.
    for offset in INVALID_OFFSETS:
        for rounding in ROUNDINGS:
            one(1, 1, offset, rounding)
            one(MAX_U128, MAX_U128, offset, rounding)

    # Bit-boundary cases: a product that is an exact multiple of 2^offset must not
    # be incremented, one with a single discarded bit must be.
    for rounding in ROUNDINGS:
        one(1 << 63, 2, 64, rounding)
        one(1 << 63, 2, 63, rounding)
        one(1 << 64, 1 << 63, 127, rounding)
        one(0, MAX_U128, 127, rounding)
        one(MAX_U128, MAX_U128, 0, rounding)
        one(1, MAX_U128, 0, rounding)
        one(1, MAX_U128, 127, rounding)

    # floor == max u128 with a non-zero remainder. Not constructible at offset 0,
    # where nothing is discarded so the two roundings coincide.
    for offset in VALID_OFFSETS[1:]:
        emit_round_up_overflow_mul_shr(offset)

    for _ in range(250):
        x = rng.bits(rng.pick((16, 32, 64, 128)))
        y = rng.bits(rng.pick((16, 32, 64, 128)))
        one(x, y, rng.pick(VALID_OFFSETS), rng.pick(ROUNDINGS))


def emit_shl_div_vectors(rng: SplitMix64) -> None:
    """(x << offset) / y, both roundings.

    On coverage: "rounding down fits but rounding up overflows" is not generated
    here because no u128 input can produce it. If floor(x*2^o / y) == 2^128-1 with
    remainder r and 0 < r < y, then x*2^o == y*(2^128-1) + r, so r == y (mod 2^o),
    which forces y >= 2^o + r and hence x >= (2^o+1)*(2^128-1)/2^o > max u128. The
    boundary is still pinned by cases that round up to exactly max u128.
    """

    def one(x: int, y: int, offset: int, rounding: str) -> None:
        value, err = expect_shl_div(x, y, offset, rounding)
        if err is not None:
            emit("shl_div", x=u128_json(x), y=u128_json(y), offset=offset,
                 rounding=rounding, want=None, want_error=err)
            return
        assert value is not None
        emit("shl_div", x=u128_json(x), y=u128_json(y), offset=offset,
             rounding=rounding, want=u128_json(value), want_error=None)

    for offset in VALID_OFFSETS:
        for x in CROSS_POOL:
            for y in (1, 1 << 63, 1 << 64, MAX_U128):
                for rounding in ROUNDINGS:
                    one(x, y, offset, rounding)

    # A zero divisor is an error, but only once the offset has been accepted.
    for offset in (0, 64, 127) + INVALID_OFFSETS:
        for rounding in ROUNDINGS:
            one(1, 0, offset, rounding)
            one(MAX_U128, 0, offset, rounding)

    for offset in INVALID_OFFSETS:
        for rounding in ROUNDINGS:
            one(1, 1, offset, rounding)
            one(MAX_U128, 1, offset, rounding)
            # The offset check runs first, so this must report offset_too_large
            # rather than divide_by_zero.
            one(1, 0, offset, rounding)

    # Boundaries: the largest exact result, the rounding-up boundary that lands on
    # exactly max u128, and cases where the numerator alone is wide enough to
    # overflow whatever the rounding.
    for rounding in ROUNDINGS:
        one(MAX_U128, 1 << 127, 127, rounding)
        one(MAX_U128, 2, 1, rounding)
        one(1, 1 << 127, 127, rounding)
        one(1, 1 << 127, 0, rounding)
        one(1, 1, 127, rounding)
        one(MAX_U128, 1, 64, rounding)
        one(MAX_U128, 1, 100, rounding)
        one(MAX_U128, 1, 127, rounding)
        one(0, MAX_U128, 127, rounding)

    for _ in range(250):
        x = rng.bits(rng.pick((16, 32, 64, 128)))
        y = rng.bits(rng.pick((1, 8, 32, 64, 128)))
        one(x, y, rng.pick(VALID_OFFSETS), rng.pick(ROUNDINGS))


# --------------------------------------------------------------------------- #
# Output
# --------------------------------------------------------------------------- #


def encode(cases: list[dict]) -> str:
    """One case per line: a diff shows a changed vector, not a rewritten blob."""
    body = ",\n".join("    " + json.dumps(case) for case in cases)
    return (
        "{\n"
        f'  "generated_by": "{GENERATED_BY}",\n'
        f'  "seed": {SEED},\n'
        '  "cases": [\n'
        f"{body}\n"
        "  ]\n"
        "}\n"
    )


def main() -> int:
    rng = SplitMix64(SEED)

    emit_u128_limb_vectors()
    emit_u256_limb_vectors()
    emit_mul128_to256_vectors(rng)
    emit_u256_shift_vectors(rng)
    emit_div256_by128_vectors(rng)
    emit_divceil256_by128_vectors(rng)
    emit_mul_div_vectors(rng)
    emit_mul_shr_vectors(rng)
    emit_shl_div_vectors(rng)

    # A targeted case sometimes coincides with a grid case. Exact duplicates add
    # bytes and nothing else, so they are dropped here, and dropping them cannot
    # hide a missing operation because the coverage tests in both Go files count
    # cases per op. Order is otherwise preserved.
    unique: list[dict] = []
    seen: set[str] = set()
    for case in CASES:
        key = json.dumps(case, sort_keys=True)
        if key in seen:
            continue
        seen.add(key)
        unique.append(case)
    duplicates = len(CASES) - len(unique)

    # Re-read what is about to be written: a JSON syntax error fails here, not in
    # a Go test run.
    text = encode(unique)
    parsed = json.loads(text)
    require(len(parsed["cases"]) == len(unique), "re-encoded case count differs from the generated count")

    counts: dict[str, int] = {}
    for case in unique:
        counts[case["op"]] = counts.get(case["op"], 0) + 1

    OUT_PATH.parent.mkdir(parents=True, exist_ok=True)
    OUT_PATH.write_text(text, encoding="utf-8")
    size = OUT_PATH.stat().st_size
    require(size < 2 * 1024 * 1024, f"vector file is {size} bytes, over the 2 MB budget")

    print(f"wrote {OUT_PATH.relative_to(ROOT)} ({size} bytes, {len(unique)} cases, {duplicates} duplicates dropped)")
    for op in sorted(counts):
        print(f"  {op}: {counts[op]}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
