#!/usr/bin/env python3
"""Generate golden vectors for the Meteora DLMM per-bin swap arithmetic.

Why this exists
---------------
The per-bin swap arithmetic decides how much a user gets for a given input, so a
mistake there is a mistake in money. The Go port under test (``bin/fee.go``) and
this file must therefore not share an author-supplied mental model. Two
implementations that were derived from the same wrong reading of the reference
agree with each other and still hand out the wrong answer.

So the expected values here are produced by a third implementation: plain Python
``int`` (arbitrary precision, no limbs, no wrapping, no checked arithmetic),
transliterated from the Rust reference in
``package-referense/dlmm-sdk/commons/src`` only. That Rust file tree was not
consulted for anything about the Go side, and ``bin/fee.go`` was deliberately
never opened.

Independence notes worth keeping honest about:

* The account *layout* is not what is being tested, so the field values come from
  ``testdata/decoded/{LbPair,BinArray}.json``. Those were produced by an
  independent Python decoder (``tools/gen_golden/gen_account_vectors.py``) which
  is itself cross-checked against the raw ``.bin`` fixtures. No Go decoder was
  read or run.
* ``get_price_from_id`` is the one place where a naive transliteration would be
  easy to fudge, so the port is validated against ground truth that cannot come
  from this file: all 140 bin prices stored in the two fixture ``BinArray``
  accounts are reproduced bit-for-bit (self-check 6).

Semantics encoded here, from the Rust reference
-----------------------------------------------
``math/u128x128_math.rs`` + ``math/utils.rs``:

* ``mul_div(x, y, d, r)`` = ``x*y/d`` rounded down (``r=down``) or up (``r=up``),
  computed in 256 bits. It is *None* when ``d == 0`` or when the result does not
  fit in 128 bits. It is never a wrapped value.
* ``mul_shr(x, y, off, r)`` = ``mul_div(x, y, 1 << off, r)``; ``off >= 128`` is an
  error instead, mirroring ``1u128.checked_shl``.
* ``shl_div(x, y, off, r)`` = ``mul_div(x, 1 << off, y, r)``.
* ``safe_*_cast`` narrows the 128-bit result to ``u64`` and errors when it does
  not fit. Roughly every per-bin amount is narrowed this way, so "fits in 64
  bits" is a real, observable failure mode and every vector records it as
  ``rejected: "overflow"`` rather than clamping.

``math/u64x64_math.rs``:

* ``get_price_from_id(bin_id, bin_step)`` builds ``base = 2^64 + (bin_step << 64)
  / 10000`` (floor) and raises it to ``bin_id`` with a fixed 19-bit
  square-and-multiply loop that (a) inverts the base when ``base >= 2^64`` so the
  128-bit products cannot overflow, and (b) truncates every product by ``>> 64``.
  ``|exp| >= 0x80000`` is an error, as is a zero result and any product that
  would exceed 128 bits. The loop is reproduced statement by statement rather
  than re-derived from ``(1 + step/1e4) ** n``, because the truncation at each
  step is observable.

``extensions/lb_pair.rs``: the fee model (base, variable, total, compute_fee,
compute_fee_from_amount, compute_protocol_fee).

``extensions/bin.rs``: ``get_amount_out``, ``get_amount_in``,
``get_max_amount_in``, ``calculate_out_amount``, the limit-order layer accessors
and the fill planner.

``quote.rs``: ``calculate_exact_in_fill_amount``,
``get_exact_in_fill_amount_result``, ``split_fee``, ``swap_exact_in_quote_at_bin``,
and the multi-bin ``quote_exact_in`` traversal (needed for the two integration
scenarios; its per-bin steps are emitted as ``bin_quote`` records so the
composition stays checkable).

Output shape
------------
``testdata/bin_vectors/<kind>.json``::

    {"generated_by": "...", "kind": "...", "source": "...", "count": N,
     "vectors": [{"kind": "...", "inputs": {...}, "want": {...}|null,
                  "rejected": null|"overflow"|"zero_liquidity", ...}, ...]}

* Scalars are decimal **strings** in every field except JSON booleans and the
  ``rejected`` token, so no value ever round-trips through a float64 parser.
* A ``u128`` is ``{"dec": "...", "lo": "...", "hi": "..."}``; the decimal string
  and the limbs are both present so a transposed limb order cannot pass unseen.
* ``want`` is ``null`` exactly when ``rejected`` is non-null.
* Extra keys (``trace``, ``note``) are additive: a consumer may ignore them.

Self-checks (all fatal, exit code 1)
------------------------------------
1. ``q*d + r == n`` and ``0 <= r < d`` for every division this file performs.
2. ``ceil - floor`` is 0 or 1, and ``ceil >= floor``, for every rounding pair.
3. ``get_amount_out`` and ``get_amount_in`` invert each other to within one unit
   for the same price and direction; mismatches are reported, never smoothed over.
4. Every emitted value fits its declared width; oversize values become
   ``rejected`` records instead of silent clamps.
5. ``get_price_from_id(0, bin_step) == 1 << 64`` for every bin step emitted.
6. All 140 prices stored in the two fixture bin arrays equal
   ``get_price_from_id(bin_id, bin_step)``.
7. The fixture ``LbPair`` fee fields are self-consistent: ``get_total_fee`` equals
   ``min(base + variable, MAX_FEE_RATE)``, and ``compute_fee`` inverts
   ``compute_fee_from_amount`` to within a rounding unit.

Usage::

    python3 tools/gen_bin_oracle/gen_bin_vectors.py           # write testdata/bin_vectors
    python3 tools/gen_bin_oracle/gen_bin_vectors.py --check   # verify, write nothing

Standard library only. No network, no Go toolchain, no pip.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
OUT_DIR = ROOT / "testdata" / "bin_vectors"
DECODED = ROOT / "testdata" / "decoded"

GENERATED_BY = "tools/gen_bin_oracle/gen_bin_vectors.py"

POOL = "9t3EyC9FweyL7PBWvKz3mrXg8B9fwFc9SK3QxM4ENqhd"

# ---------------------------------------------------------------- constants ---
# commons/src/math/u64x64_math.rs
SCALE_OFFSET = 64
ONE = 1 << SCALE_OFFSET
MAX_EXPONENTIAL = 0x80000  # 524288; the Rust doc comment says 1048576, the code says 0x80000
# commons/src/math/u128x128_math.rs
MAX_SHIFT_OFFSET = 127  # 1u128.checked_shl(128) is None
# commons/src/constants.rs
BASIS_POINT_MAX = 10000
LIMIT_ORDER_FEE_SHARE = 5000
MAX_FEE_RATE = 100_000_000
FEE_PRECISION = 1_000_000_000
MAX_BIN_PER_ARRAY = 70

MAX_U64 = (1 << 64) - 1
MAX_U128 = (1 << 128) - 1

DOWN = "down"
UP = "up"

FIXTURE_BIN_STEP = 10


def fail(message: str) -> None:
    """Abort: a violated assumption must never become a vector."""
    raise SystemExit(f"gen_bin_vectors: {message}")


def require(condition: bool, message: str) -> None:
    if not condition:
        fail(message)


class Overflow(Exception):
    """The Rust returns None / Err for this; here it is an exception."""


class OutOfLiquidity(Exception):
    pass


# ------------------------------------------------------------- fixed point ---
def div_floor(n: int, d: int) -> int:
    """Floor division with the Euclidean identity asserted on every call.

    Self-check 1 lives here: every division in this file goes through it, so
    ``q*d + r == n`` and ``r < d`` cannot be skipped by accident.
    """
    if d == 0:
        raise Overflow("division by zero")
    q, r = divmod(n, d)
    require(q * d + r == n, f"euclid identity broken: {n}/{d} -> q={q} r={r}")
    require(0 <= r < d, f"remainder out of range: {n}/{d} -> r={r}")
    return q


def ceil_floor(n: int, d: int) -> tuple[int, int]:
    """(ceil, floor) of n/d, asserting they differ by at most one (self-check 2)."""
    if d == 0:
        raise Overflow("division by zero")
    q, r = divmod(n, d)
    require(q * d + r == n, f"euclid identity broken: {n}/{d}")
    require(0 <= r < d, f"remainder out of range: {n}/{d}")
    lo, hi = q, q + (1 if r else 0)
    require(hi >= lo, "ceil < floor")
    require(hi - lo <= 1, "ceil and floor differ by more than one")
    return hi, lo


def _u128(v: int) -> int:
    if v < 0 or v > MAX_U128:
        raise Overflow(f"value does not fit u128: {v}")
    return v


def _u64(v: int) -> int:
    if v < 0 or v > MAX_U64:
        raise Overflow(f"value does not fit u64: {v}")
    return v


def mul_div(x: int, y: int, denominator: int, rounding: str) -> int:
    """u128x128_math.rs::mul_div -- None on zero denominator or u128 overflow."""
    if denominator == 0:
        raise Overflow("mul_div: zero denominator")
    _u128(x)
    _u128(y)
    _u128(denominator)
    hi, lo = ceil_floor(x * y, denominator)
    return _u128(hi if rounding == UP else lo)


def mul_shr(x: int, y: int, offset: int, rounding: str) -> int:
    """u128x128_math.rs::mul_shr -- (x*y) >> offset."""
    if offset >= 128 or offset < 0:
        raise Overflow(f"mul_shr: offset {offset} rejected by 1u128.checked_shl")
    return mul_div(x, y, 1 << offset, rounding)


def shl_div(x: int, y: int, offset: int, rounding: str) -> int:
    """u128x128_math.rs::shl_div -- (x << offset) / y."""
    if offset >= 128 or offset < 0:
        raise Overflow(f"shl_div: offset {offset} rejected by 1u128.checked_shl")
    return mul_div(x, 1 << offset, y, rounding)


def safe_mul_shr_cast(x: int, y: int, offset: int, rounding: str) -> int:
    return _u64(mul_shr(x, y, offset, rounding))


def safe_shl_div_cast(x: int, y: int, offset: int, rounding: str) -> int:
    return _u64(shl_div(x, y, offset, rounding))


def safe_mul_div_cast(x: int, y: int, denominator: int, rounding: str) -> int:
    return _u64(mul_div(x, y, denominator, rounding))


# ------------------------------------------------------------ price (Q64.64) ---
def pow_q64(base: int, exp: int) -> int:
    """u64x64_math.rs::pow, statement for statement.

    ``exp`` is an i32 bin id and may be negative. The 19 unrolled bit tests in the
    Rust are written here as a loop over the same 19 bits with the same ordering:
    test bit, then square (18 squarings, none after the last test).
    """
    _u128(base)
    invert = exp < 0
    if exp == 0:
        return ONE
    e = -exp if invert else exp
    if e >= MAX_EXPONENTIAL:
        raise Overflow(f"pow: exponent {exp} >= MAX_EXPONENTIAL")

    squared_base = base
    result = ONE
    # `if squared_base >= result` -- base is always >= ONE so this fires unless the
    # caller passed a base below 2^64, which get_price_from_id never does.
    if squared_base >= result:
        squared_base = div_floor(MAX_U128, squared_base)
        invert = not invert

    for bit in range(19):
        if e & (1 << bit):
            product = result * squared_base
            if product > MAX_U128:
                raise Overflow("pow: checked_mul overflow")
            result = product >> SCALE_OFFSET
        if bit < 18:
            product = squared_base * squared_base
            if product > MAX_U128:
                raise Overflow("pow: checked_mul overflow")
            squared_base = product >> SCALE_OFFSET

    if result == 0:
        raise Overflow("pow: zero result")
    if invert:
        result = div_floor(MAX_U128, result)
    return _u128(result)


def get_price_from_id(bin_id: int, bin_step: int) -> int:
    """math/price_math.rs::get_price_from_id."""
    bps = div_floor(bin_step << SCALE_OFFSET, BASIS_POINT_MAX)
    base = ONE + bps
    return pow_q64(base, bin_id)


# ------------------------------------------------------------- bin amounts ---
def get_amount_out(amount_in: int, price: int, swap_for_y: bool, rounding: str) -> int:
    """extensions/bin.rs::Bin::get_amount_out."""
    if swap_for_y:
        return safe_mul_shr_cast(price, amount_in, SCALE_OFFSET, rounding)
    return safe_shl_div_cast(amount_in, price, SCALE_OFFSET, rounding)


def get_amount_in(amount_out: int, price: int, swap_for_y: bool, rounding: str) -> int:
    """extensions/bin.rs::Bin::get_amount_in."""
    if swap_for_y:
        return safe_shl_div_cast(amount_out, price, SCALE_OFFSET, rounding)
    return safe_mul_shr_cast(amount_out, price, SCALE_OFFSET, rounding)


def calculate_out_amount(amount_x: int, amount_y: int, liquidity_supply: int, share: int):
    """extensions/bin.rs::Bin::calculate_out_amount -- (out_x, out_y)."""
    out_x = safe_mul_div_cast(share, amount_x, liquidity_supply, DOWN)
    out_y = safe_mul_div_cast(share, amount_y, liquidity_supply, DOWN)
    return out_x, out_y


# ---------------------------------------------------------------- fee model ---
class Pair:
    """The LbPair fields the fee model and the quote read."""

    def __init__(
        self,
        pool: str,
        bin_step: int,
        base_factor: int,
        base_fee_power_factor: int,
        variable_fee_control: int,
        protocol_share: int,
        max_volatility_accumulator: int,
        volatility_accumulator: int,
        function_type: int,
        collect_fee_mode: int,
        active_id: int,
        synthetic: bool = False,
    ) -> None:
        self.pool = pool
        self.bin_step = bin_step
        self.base_factor = base_factor
        self.base_fee_power_factor = base_fee_power_factor
        self.variable_fee_control = variable_fee_control
        self.protocol_share = protocol_share
        self.max_volatility_accumulator = max_volatility_accumulator
        self.volatility_accumulator = volatility_accumulator
        self.function_type = function_type
        self.collect_fee_mode = collect_fee_mode
        self.active_id = active_id
        self.synthetic = synthetic

    # -- extensions/lb_pair.rs
    def get_base_fee(self) -> int:
        return _u128(
            self.base_factor * self.bin_step * 10 * (10 ** self.base_fee_power_factor)
        )

    def compute_variable_fee(self, volatility_accumulator: int) -> int:
        if self.variable_fee_control == 0:
            return 0
        square_vfa_bin = _u128(volatility_accumulator * self.bin_step) ** 2
        require(square_vfa_bin <= MAX_U128, "compute_variable_fee: checked_pow overflow")
        v_fee = self.variable_fee_control * square_vfa_bin
        require(v_fee <= MAX_U128, "compute_variable_fee: checked_mul overflow")
        return div_floor(v_fee + 99_999_999_999, 100_000_000_000)

    def get_variable_fee(self) -> int:
        return self.compute_variable_fee(self.volatility_accumulator)

    def get_total_fee_for_vol(self, volatility_accumulator: int) -> int:
        total = self.get_base_fee() + self.compute_variable_fee(volatility_accumulator)
        require(total <= MAX_U128, "get_total_fee: checked_add overflow")
        return min(total, MAX_FEE_RATE)

    def get_total_fee(self) -> int:
        return self.get_total_fee_for_vol(self.volatility_accumulator)

    def compute_fee(self, amount: int, total_fee_rate: int) -> int:
        """extensions/lb_pair.rs::compute_fee -- ceil(amount * rate / (1e9 - rate))."""
        denominator = FEE_PRECISION - total_fee_rate
        if denominator <= 0:
            raise Overflow("compute_fee: FEE_PRECISION - total_fee_rate underflow")
        hi, _lo = ceil_floor(amount * total_fee_rate, denominator)
        return _u64(hi)

    def compute_fee_from_amount(self, amount: int, total_fee_rate: int) -> int:
        """extensions/lb_pair.rs::compute_fee_from_amount -- ceil(amount * rate / 1e9)."""
        hi, _lo = ceil_floor(amount * total_fee_rate, FEE_PRECISION)
        return _u64(hi)

    def compute_protocol_fee(self, fee_amount: int) -> int:
        """extensions/lb_pair.rs::compute_protocol_fee -- floor(fee * share / 1e4)."""
        return _u64(div_floor(fee_amount * self.protocol_share, BASIS_POINT_MAX))

    # -- extensions/lb_pair.rs kind predicates
    def is_support_limit_order(self) -> bool:
        if self.function_type == 2:  # FunctionType::LimitOrder
            return True
        if self.function_type == 1:  # FunctionType::LiquidityMining
            return False
        return True  # Undetermined + no reward mints (fixture has none)

    def fee_on_input(self, swap_for_y: bool) -> bool:
        if self.collect_fee_mode == 0:  # CollectFeeMode::InputOnly
            return True
        if self.collect_fee_mode == 1:  # CollectFeeMode::OnlyY
            return not swap_for_y
        return True  # invalid value falls back to true


# --------------------------------------------------------------- bin struct ---
class Bin:
    def __init__(
        self,
        bin_id: int,
        array_index: int,
        amount_x: int,
        amount_y: int,
        price: int,
        liquidity_supply: int,
        open_order_amount: int,
        processed_order_remaining_amount: int,
        limit_order_ask_side: int,
    ) -> None:
        self.bin_id = bin_id
        self.array_index = array_index
        self.amount_x = amount_x
        self.amount_y = amount_y
        self.price = price
        self.liquidity_supply = liquidity_supply
        self.open_order_amount = open_order_amount
        self.processed_order_remaining_amount = processed_order_remaining_amount
        self.limit_order_ask_side = limit_order_ask_side

    def is_empty(self) -> bool:
        return not (
            self.amount_x
            or self.amount_y
            or self.open_order_amount
            or self.processed_order_remaining_amount
        )

    # -- extensions/bin.rs
    def get_max_amount_out(self, swap_for_y: bool) -> int:
        return self.amount_y if swap_for_y else self.amount_x

    def get_max_amount_in(self, price: int, swap_for_y: bool) -> int:
        if swap_for_y:
            return safe_shl_div_cast(self.amount_y, price, SCALE_OFFSET, UP)
        return safe_mul_shr_cast(self.amount_x, price, SCALE_OFFSET, UP)

    def get_limit_order_amounts_by_direction(self, swap_for_y: bool) -> tuple[int, int]:
        """(open_order_amount, processed_order_remaining_amount) or (0, 0)."""
        is_ask_side = self.limit_order_ask_side != 0
        if (swap_for_y and not is_ask_side) or (not swap_for_y and is_ask_side):
            return self.open_order_amount, self.processed_order_remaining_amount
        return 0, 0

    def get_max_amount_out_with_limit_orders(
        self, swap_for_y: bool, support_limit_order: bool
    ) -> int:
        mm_amount = self.get_max_amount_out(swap_for_y)
        if not support_limit_order:
            return mm_amount
        open_order, processed_remaining = self.get_limit_order_amounts_by_direction(swap_for_y)
        total = mm_amount + open_order + processed_remaining
        return min(total, MAX_U64)  # saturating_add


# ------------------------------------------------------------- per-bin quote ---
class BinQuote:
    def __init__(self, amount_in: int, amount_out: int, fee: int, protocol_fee: int) -> None:
        self.amount_in = amount_in
        self.amount_out = amount_out
        self.fee = fee
        self.protocol_fee = protocol_fee


class FillResult:
    def __init__(self, amount_in: int, amount_left: int, out_amount: int) -> None:
        self.amount_in = amount_in
        self.amount_left = amount_left
        self.out_amount = out_amount


class ExactInFillResult:
    def __init__(self, amount_in: int, amount_left: int, out_amount: int, mm_amount_in: int) -> None:
        self.amount_in = amount_in
        self.amount_left = amount_left
        self.out_amount = out_amount
        self.mm_amount_in = mm_amount_in


def calculate_exact_in_fill_amount(
    bin: Bin, price: int, amount: int, max_amount_out: int, swap_for_y: bool
) -> FillResult:
    """quote.rs::calculate_exact_in_fill_amount."""
    if max_amount_out == 0:
        return FillResult(0, amount, 0)
    max_amount_in = get_amount_in(max_amount_out, price, swap_for_y, UP)
    if amount >= max_amount_in:
        return FillResult(max_amount_in, amount - max_amount_in, max_amount_out)
    out_amount = get_amount_out(amount, price, swap_for_y, DOWN)
    return FillResult(amount, 0, out_amount)


def get_exact_in_fill_amount_result(
    bin: Bin, price: int, amount_in: int, swap_for_y: bool, support_limit_order: bool
) -> ExactInFillResult:
    """quote.rs::get_exact_in_fill_amount_result."""
    mm_amount = bin.amount_y if swap_for_y else bin.amount_x
    mm_fill = calculate_exact_in_fill_amount(bin, price, amount_in, mm_amount, swap_for_y)

    if not support_limit_order:
        return ExactInFillResult(
            mm_fill.amount_in, mm_fill.amount_left, mm_fill.out_amount, mm_fill.amount_in
        )

    total_amount_in = mm_fill.amount_in
    total_amount_out = mm_fill.out_amount
    amount_left_after_mm = mm_fill.amount_left

    if amount_left_after_mm > 0:
        open_order_amount, processed_order_remaining = bin.get_limit_order_amounts_by_direction(
            swap_for_y
        )
        # processed orders first
        processed_fill = calculate_exact_in_fill_amount(
            bin, price, amount_left_after_mm, processed_order_remaining, swap_for_y
        )
        total_amount_in += processed_fill.amount_in
        total_amount_out += processed_fill.out_amount
        # then open orders
        if processed_fill.amount_left > 0:
            open_fill = calculate_exact_in_fill_amount(
                bin, price, processed_fill.amount_left, open_order_amount, swap_for_y
            )
            total_amount_in += open_fill.amount_in
            total_amount_out += open_fill.out_amount

    if total_amount_in > amount_in:
        raise Overflow("get_exact_in_fill_amount_result: amount_left underflow")
    return ExactInFillResult(
        total_amount_in,
        amount_in - total_amount_in,
        total_amount_out,
        mm_fill.amount_in,
    )


def split_fee(
    trading_fee: int, protocol_share: int, mm_amount_in: int, total_amount_in: int
) -> tuple[int, int]:
    """quote.rs::split_fee -- returns (total_user_fee, total_protocol_fee).

    Note the two different roundings the reference uses: ``mm_fee`` is a *ceiling*
    division, every other division in the function is a floor. ``LIMIT_ORDER_FEE_SHARE``
    is 5000 and ``BASIS_POINT_MAX`` is 10000.
    """
    if total_amount_in == 0 or trading_fee == 0:
        return 0, 0

    mm_fee = _u64(div_floor(trading_fee * mm_amount_in + (total_amount_in - 1), total_amount_in))
    if mm_fee > trading_fee:
        raise Overflow("split_fee: total_lo_fee underflow (mm_amount_in > total_amount_in)")
    total_lo_fee = trading_fee - mm_fee

    lo_fee = _u64(div_floor(total_lo_fee * LIMIT_ORDER_FEE_SHARE, BASIS_POINT_MAX))
    lo_protocol_fee = total_lo_fee - lo_fee
    mm_protocol_fee = _u64(div_floor(mm_fee * protocol_share, BASIS_POINT_MAX))
    total_protocol_fee = _u64(lo_protocol_fee + mm_protocol_fee)
    total_user_fee = trading_fee - total_protocol_fee
    return total_user_fee, total_protocol_fee


def swap_exact_in_quote_at_bin(
    bin: Bin,
    price: int,
    pair: Pair,
    total_fee_rate: int,
    in_amount: int,
    swap_for_y: bool,
    support_limit_order: bool,
    fee_on_input: bool,
) -> tuple[BinQuote, ExactInFillResult]:
    """quote.rs::swap_exact_in_quote_at_bin."""
    trading_fee = 0
    excluded_fee_amount_in = in_amount

    if fee_on_input:
        fee = pair.compute_fee_from_amount(in_amount, total_fee_rate)
        trading_fee = fee
        if fee > in_amount:
            raise Overflow("swap_exact_in_quote_at_bin: excluded fee amount in underflow")
        excluded_fee_amount_in = in_amount - fee

    fill_result = get_exact_in_fill_amount_result(
        bin, price, excluded_fee_amount_in, swap_for_y, support_limit_order
    )

    amount_left = fill_result.amount_left
    out_amount = fill_result.out_amount
    included_fee_amount_in = in_amount

    if amount_left > 0:
        excluded_fee_amount_in = excluded_fee_amount_in - amount_left
        if fee_on_input:
            fee = pair.compute_fee(excluded_fee_amount_in, total_fee_rate)
            trading_fee = fee
            included_fee_amount_in = excluded_fee_amount_in + fee
        else:
            included_fee_amount_in = excluded_fee_amount_in

    excluded_fee_amount_out = out_amount
    if not fee_on_input:
        fee = pair.compute_fee_from_amount(out_amount, total_fee_rate)
        trading_fee = fee
        if fee > out_amount:
            raise Overflow("swap_exact_in_quote_at_bin: excluded fee amount out underflow")
        excluded_fee_amount_out = out_amount - fee

    _user_fee, protocol_fee = split_fee(
        trading_fee, pair.protocol_share, fill_result.mm_amount_in, fill_result.amount_in
    )

    return (
        BinQuote(included_fee_amount_in, excluded_fee_amount_out, trading_fee, protocol_fee),
        fill_result,
    )


# ------------------------------------------------------ multi-bin traversal ---
def update_volatility_accumulator(
    index_reference: int, active_id: int, volatility_reference: int, max_volatility_accumulator: int
) -> int:
    """extensions/lb_pair.rs::update_volatility_accumulator."""
    delta_id = abs(index_reference - active_id)
    volatility_accumulator = volatility_reference + delta_id * BASIS_POINT_MAX
    require(volatility_accumulator < (1 << 64), "volatility accumulator does not fit u64")
    return min(volatility_accumulator, max_volatility_accumulator)


def bin_array_index_of(bin_id: int) -> int:
    """BinArray::bin_id_to_bin_array_index -- floor division, so -1 lives in -1."""
    return bin_id // MAX_BIN_PER_ARRAY


def quote_exact_in_traversal(
    pair: Pair,
    arrays: dict[int, list[Bin]],
    amount_in: int,
    swap_for_y: bool,
    support_limit_order: bool,
    fee_on_input: bool,
    index_reference: int,
    volatility_reference: int,
):
    """The multi-bin loop of quote.rs::quote_exact_in, restricted to the fixture's
    two contiguous bin arrays (index -1 and 0).

    ``index_reference``/``volatility_reference`` are the values
    ``update_references`` leaves behind. For the fixture pool every real
    ``current_timestamp`` is >= ``decay_period`` past ``last_update_timestamp == 0``,
    so ``index_reference = active_id`` and ``volatility_reference = 0``; both are
    passed in explicitly so the vector records them.
    """
    active_id = pair.active_id
    amount_left = amount_in
    total_amount_out = 0
    total_fee = 0
    total_protocol_fee = 0
    steps = []

    guard = 0
    while amount_left > 0:
        guard += 1
        require(guard <= 10_000, "quote_exact_in: traversal did not terminate")

        array_index = bin_array_index_of(active_id)
        if array_index not in arrays:
            raise OutOfLiquidity("Pool out of liquidity")

        bins = arrays[array_index]
        lower = array_index * MAX_BIN_PER_ARRAY
        while lower <= active_id <= lower + MAX_BIN_PER_ARRAY - 1 and amount_left > 0:
            bin_ = bins[active_id - lower]
            price = bin_.price
            max_out = bin_.get_max_amount_out_with_limit_orders(swap_for_y, support_limit_order)

            if max_out > 0:
                vol_acc = update_volatility_accumulator(
                    index_reference,
                    active_id,
                    volatility_reference,
                    pair.max_volatility_accumulator,
                )
                total_fee_rate = pair.get_total_fee_for_vol(vol_acc)
                quote, fill = swap_exact_in_quote_at_bin(
                    bin_,
                    price,
                    pair,
                    total_fee_rate,
                    amount_left,
                    swap_for_y,
                    support_limit_order,
                    fee_on_input,
                )
                if quote.amount_in > 0:
                    amount_left -= quote.amount_in
                    total_amount_out += quote.amount_out
                    total_fee += quote.fee
                    total_protocol_fee += quote.protocol_fee
                    steps.append(
                        {
                            "bin_id": active_id,
                            "in_amount": amount_left + quote.amount_in,
                            "volatility_accumulator": vol_acc,
                            "total_fee_rate": total_fee_rate,
                            "quote": quote,
                            "fill": fill,
                            "max_amount_out": max_out,
                        }
                    )

            if amount_left > 0:
                active_id += -1 if swap_for_y else 1

    return {
        "amount_out": total_amount_out,
        "fee": total_fee,
        "protocol_fee": total_protocol_fee,
        "amount_left": amount_left,
        "steps": steps,
        "active_id_end": active_id,
    }


# ------------------------------------------------------------------ fixtures ---
def load_pair(pool_field: str = POOL) -> Pair:
    doc = json.loads((DECODED / "LbPair.json").read_text())
    for vector in doc["vectors"]:
        if pool_field not in vector["source"]:
            continue
        f = vector["fields"]
        return Pair(
            pool=pool_field,
            bin_step=f["bin_step"]["value"],
            base_factor=f["parameters.base_factor"]["value"],
            base_fee_power_factor=f["parameters.base_fee_power_factor"]["value"],
            variable_fee_control=f["parameters.variable_fee_control"]["value"],
            protocol_share=f["parameters.protocol_share"]["value"],
            max_volatility_accumulator=f["parameters.max_volatility_accumulator"]["value"],
            volatility_accumulator=f["v_parameters.volatility_accumulator"]["value"],
            function_type=f["parameters.function_type"]["value"],
            collect_fee_mode=f["parameters.collect_fee_mode"]["value"],
            active_id=f["active_id"]["value"],
        )
    fail(f"LbPair.json has no vector for pool {pool_field}")


def load_bin_arrays(pool_field: str = POOL) -> dict[int, list[Bin]]:
    doc = json.loads((DECODED / "BinArray.json").read_text())
    arrays: dict[int, list[Bin]] = {}
    for vector in doc["vectors"]:
        if pool_field not in vector["source"]:
            continue
        f = vector["fields"]
        array_index = f["index"]["value"]
        bins = []
        for i in range(MAX_BIN_PER_ARRAY):
            p = f"bins[{i}]."
            bins.append(
                Bin(
                    bin_id=array_index * MAX_BIN_PER_ARRAY + i,
                    array_index=array_index,
                    amount_x=f[p + "amount_x"]["value"],
                    amount_y=f[p + "amount_y"]["value"],
                    price=int(f[p + "price"]["value"]),
                    liquidity_supply=int(f[p + "liquidity_supply"]["value"]),
                    open_order_amount=f[p + "open_order_amount"]["value"],
                    processed_order_remaining_amount=f[p + "processed_order_remaining_amount"][
                        "value"
                    ],
                    limit_order_ask_side=f[p + "limit_order_ask_side"]["value"],
                )
            )
        arrays[array_index] = bins
    require(arrays, f"BinArray.json has no vector for pool {pool_field}")
    return arrays


# ------------------------------------------------------------ json encoding ---
def u128j(v: int) -> dict:
    _u128(v)
    return {"dec": str(v), "lo": str(v & MAX_U64), "hi": str(v >> 64)}


def s(v: int) -> str:
    return str(v)


def dump_file(header: dict, records: list[dict]) -> str:
    """One record per line, like testdata/vectors/numeric.json."""
    lines = ["{"]
    for key, value in header.items():
        lines.append(f"  {json.dumps(key)}: {json.dumps(value, separators=(', ', ': '))},")
    lines.append(f'  "count": {len(records)},')
    lines.append('  "vectors": [')
    for i, record in enumerate(records):
        comma = "," if i + 1 < len(records) else ""
        lines.append("    " + json.dumps(record, separators=(", ", ": ")) + comma)
    lines.append("  ]")
    lines.append("}")
    return "\n".join(lines) + "\n"


def rejected_value(reason: str):
    return reason


# ------------------------------------------------------------- self-check log ---
class Checks:
    def __init__(self) -> None:
        self.notes: list[str] = []
        self.inverse_mismatches: list[str] = []
        self.rejections: dict[str, int] = {}

    def note(self, message: str) -> None:
        self.notes.append(message)

    def rejection(self, reason: str) -> None:
        self.rejections[reason] = self.rejections.get(reason, 0) + 1


CHECKS = Checks()


# -------------------------------------------------------------- vector sets ---
FEE_AMOUNTS = (0, 1, 2, 3, 7, 100, 999, 1_000_000, 1_000_000_007, 2**32, 2**63, 2**64 - 1)
FEE_VOL_ACC = (0, 10_000, 150_000)

PRICE_BIN_STEPS = (1, 2, 5, 10, 25, 100, 250, 400)
PRICE_BIN_IDS = (
    -524_288,
    -443_636,
    -443_635,
    -1000,
    -70,
    -1,
    0,
    1,
    70,
    1000,
    443_635,
    443_636,
    524_287,
    524_288,
)

AMOUNT_INPUTS = (0, 1, 100, 1_000_000, 1_000_000_000, 40_000_000_000, 45_000_000_000_000, 2**32, 2**63)
ROUNDINGS = (DOWN, UP)

BIN_QUOTE_AMOUNTS_Y = (0, 1, 1_000, 1_000_000, 1_000_000_000, 40_000_000_000)
BIN_QUOTE_AMOUNTS_X = (0, 1, 1_000, 1_000_000, 1_000_000_000, 45_000_000_000_000)
BIN_QUOTE_VOL_ACC = (0, 10_000, 150_000)

SPLIT_FEE_SHARES = (0, 500, 2000, 2500, 10_000)
SPLIT_FEE_FEES = (0, 1, 2, 3, 7, 100, 1000, 1_000_000_000, 2**63, 2**64 - 1)
SPLIT_FEE_RATIOS = (
    (0, 0),
    (0, 1),
    (1, 1),
    (1, 2),
    (1, 3),
    (2, 3),
    (5, 10),
    (7, 10),
    (9, 10),
    (10, 10),
    (999, 1000),
    (1_000_000_000, 1_000_000_000),
)

LIQUIDITY_SHARES = (0, 1, 2, 3, 10, 999, 2**32, 2**63, 2**64, 2**127)


def build_price_vectors() -> list[dict]:
    records = []
    seen = set()

    def add(bin_step: int, bin_id: int, note: str | None = None) -> None:
        key = (bin_step, bin_id)
        if key in seen:
            return
        seen.add(key)
        inputs = {"bin_step": s(bin_step), "bin_id": s(bin_id)}
        if note:
            inputs["note"] = note
        record = {"kind": "price", "inputs": inputs}
        try:
            price = get_price_from_id(bin_id, bin_step)
        except Overflow as exc:
            CHECKS.rejection("overflow")
            record["want"] = None
            record["rejected"] = rejected_value("overflow")
            record["rejected_detail"] = str(exc)
            records.append(record)
            return
        record["want"] = {"price": u128j(price)}
        record["rejected"] = None
        records.append(record)

    # Fixture bins: every bin in both arrays, at the pool's own bin step.
    arrays = load_bin_arrays()
    for array_index in sorted(arrays):
        for bin_ in arrays[array_index]:
            add(FIXTURE_BIN_STEP, bin_.bin_id, f"fixture bin array {array_index}")

    # Sweep across bin steps and the extremes of the bin id range.
    for bin_step in PRICE_BIN_STEPS:
        for bin_id in PRICE_BIN_IDS:
            add(bin_step, bin_id)
    return records


def build_fee_vectors() -> list[dict]:
    records = []
    pair_docs = json.loads((DECODED / "LbPair.json").read_text())
    pairs = []
    for vector in pair_docs["vectors"]:
        pool = Path(vector["source"]).parent.name
        f = vector["fields"]
        pairs.append(
            Pair(
                pool=pool,
                bin_step=f["bin_step"]["value"],
                base_factor=f["parameters.base_factor"]["value"],
                base_fee_power_factor=f["parameters.base_fee_power_factor"]["value"],
                variable_fee_control=f["parameters.variable_fee_control"]["value"],
                protocol_share=f["parameters.protocol_share"]["value"],
                max_volatility_accumulator=f["parameters.max_volatility_accumulator"]["value"],
                volatility_accumulator=f["v_parameters.volatility_accumulator"]["value"],
                function_type=f["parameters.function_type"]["value"],
                collect_fee_mode=f["parameters.collect_fee_mode"]["value"],
                active_id=f["active_id"]["value"],
            )
        )
    # One synthetic pair exercising the MAX_FEE_RATE cap: base = 10000*400*10*10^2 = 4e9.
    pairs.append(
        Pair(
            pool="synthetic/max-fee-rate-cap",
            bin_step=400,
            base_factor=10_000,
            base_fee_power_factor=2,
            variable_fee_control=50_000,
            protocol_share=2500,
            max_volatility_accumulator=150_000,
            volatility_accumulator=0,
            function_type=0,
            collect_fee_mode=0,
            active_id=0,
            synthetic=True,
        )
    )

    for pair in pairs:
        base = {
            "pool": pair.pool,
            "synthetic": pair.synthetic,
            "bin_step": s(pair.bin_step),
            "base_factor": s(pair.base_factor),
            "base_fee_power_factor": s(pair.base_fee_power_factor),
            "variable_fee_control": s(pair.variable_fee_control),
            "protocol_share": s(pair.protocol_share),
        }
        record = {
            "kind": "fee",
            "inputs": dict(base, op="get_base_fee"),
            "want": {"base_fee": u128j(pair.get_base_fee())},
            "rejected": None,
        }
        records.append(record)

        vol_accs = sorted({0, pair.volatility_accumulator, *FEE_VOL_ACC})
        for vol_acc in vol_accs:
            inputs = dict(base, op="compute_variable_fee", volatility_accumulator=s(vol_acc))
            records.append(
                {
                    "kind": "fee",
                    "inputs": inputs,
                    "want": {"variable_fee": u128j(pair.compute_variable_fee(vol_acc))},
                    "rejected": None,
                }
            )
            records.append(
                {
                    "kind": "fee",
                    "inputs": dict(base, op="get_total_fee", volatility_accumulator=s(vol_acc)),
                    "want": {"total_fee": u128j(pair.get_total_fee_for_vol(vol_acc))},
                    "rejected": None,
                }
            )
            total_fee_rate = pair.get_total_fee_for_vol(vol_acc)
            for amount in FEE_AMOUNTS:
                for op in ("compute_fee", "compute_fee_from_amount"):
                    inputs = dict(
                        base,
                        op=op,
                        volatility_accumulator=s(vol_acc),
                        total_fee_rate=s(total_fee_rate),
                        amount=s(amount),
                    )
                    record = {"kind": "fee", "inputs": inputs}
                    try:
                        if op == "compute_fee":
                            value = pair.compute_fee(amount, total_fee_rate)
                        else:
                            value = pair.compute_fee_from_amount(amount, total_fee_rate)
                    except Overflow as exc:
                        CHECKS.rejection("overflow")
                        record["want"] = None
                        record["rejected"] = "overflow"
                        record["rejected_detail"] = str(exc)
                    else:
                        record["want"] = {"fee": s(value)}
                        record["rejected"] = None
                    records.append(record)
            for amount in FEE_AMOUNTS:
                inputs = dict(
                    base,
                    op="compute_protocol_fee",
                    volatility_accumulator=s(vol_acc),
                    fee_amount=s(amount),
                )
                records.append(
                    {
                        "kind": "fee",
                        "inputs": inputs,
                        "want": {"protocol_fee": s(pair.compute_protocol_fee(amount))},
                        "rejected": None,
                    }
                )
        # The pair's own state, for the headline numbers.
        record = {
            "kind": "fee",
            "inputs": dict(base, op="get_total_fee", volatility_accumulator=s(pair.volatility_accumulator)),
            "want": {
                "total_fee": u128j(pair.get_total_fee()),
                "base_fee": u128j(pair.get_base_fee()),
                "variable_fee": u128j(pair.get_variable_fee()),
            },
            "rejected": None,
        }
        records.append(record)

    # Self-check 7: the fee model agrees with itself on the fixture pool.
    fixture = load_pair()
    base_fee = fixture.get_base_fee()
    variable_fee = fixture.compute_variable_fee(fixture.volatility_accumulator)
    require(base_fee == 10_000_000, f"fixture base fee: expected 10000000, got {base_fee}")
    require(variable_fee == 0, f"fixture variable fee: expected 0, got {variable_fee}")
    require(
        fixture.get_total_fee() == min(base_fee + variable_fee, MAX_FEE_RATE),
        "fixture total fee is not min(base + variable, MAX_FEE_RATE)",
    )
    capped = Pair(
        "synthetic/max-fee-rate-cap",
        400,
        10_000,
        2,
        50_000,
        2500,
        150_000,
        0,
        0,
        0,
        0,
        synthetic=True,
    )
    require(capped.get_total_fee() == MAX_FEE_RATE, "MAX_FEE_RATE cap branch not reached")
    CHECKS.note("fixture total fee rate = 10_000_000 (1.00%), variable fee 0 at vol_acc 0")
    return records


def build_split_fee_vectors() -> list[dict]:
    records = []
    for share in SPLIT_FEE_SHARES:
        for fee in SPLIT_FEE_FEES:
            for mm, total in SPLIT_FEE_RATIOS:
                inputs = {
                    "trading_fee": s(fee),
                    "protocol_share": s(share),
                    "mm_amount_in": s(mm),
                    "total_amount_in": s(total),
                }
                record = {"kind": "split_fee", "inputs": inputs}
                try:
                    user_fee, protocol_fee = split_fee(fee, share, mm, total)
                except Overflow as exc:
                    CHECKS.rejection("overflow")
                    record["want"] = None
                    record["rejected"] = "overflow"
                    record["rejected_detail"] = str(exc)
                else:
                    record["want"] = {"user_fee": s(user_fee), "protocol_fee": s(protocol_fee)}
                    record["rejected"] = None
                records.append(record)
    return records


def build_amount_vectors() -> tuple[list[dict], list[dict]]:
    """(amount_out records, amount_in records)."""
    arrays = load_bin_arrays()
    out_records: list[dict] = []
    in_records: list[dict] = []

    def price_inputs(bin_: Bin) -> dict:
        return {
            "bin_id": s(bin_.bin_id),
            "bin_array_index": s(bin_.array_index),
            "price": u128j(bin_.price),
        }

    for array_index in sorted(arrays):
        for bin_ in arrays[array_index]:
            # get_max_amount_out is a field selector, but it is the denominator of the
            # fill planner's "bin is drained" test, so it gets a record per bin.
            out_records.append(
                {
                    "kind": "amount_out",
                    "inputs": dict(price_inputs(bin_), op="get_max_amount_out", swap_for_y=True),
                    "want": {"max_amount_out": s(bin_.get_max_amount_out(True))},
                    "rejected": None,
                }
            )
            out_records.append(
                {
                    "kind": "amount_out",
                    "inputs": dict(price_inputs(bin_), op="get_max_amount_out", swap_for_y=False),
                    "want": {"max_amount_out": s(bin_.get_max_amount_out(False))},
                    "rejected": None,
                }
            )
            for swap_for_y in (True, False):
                if bin_.is_empty():
                    continue
                # get_amount_out / get_amount_in, both roundings, same amount set.
                for amount in AMOUNT_INPUTS:
                    pair_results = {}
                    for rounding in ROUNDINGS:
                        inputs = dict(
                            price_inputs(bin_),
                            op="get_amount_out",
                            amount_in=s(amount),
                            swap_for_y=swap_for_y,
                            rounding=rounding,
                        )
                        record = {"kind": "amount_out", "inputs": inputs}
                        try:
                            value = get_amount_out(amount, bin_.price, swap_for_y, rounding)
                        except Overflow as exc:
                            CHECKS.rejection("overflow")
                            record["want"] = None
                            record["rejected"] = "overflow"
                            record["rejected_detail"] = str(exc)
                            value = None
                        else:
                            record["want"] = {"amount_out": s(value)}
                            record["rejected"] = None
                        pair_results[rounding] = value
                        out_records.append(record)

                        inputs = dict(
                            price_inputs(bin_),
                            op="get_amount_in",
                            amount_out=s(amount),
                            swap_for_y=swap_for_y,
                            rounding=rounding,
                        )
                        record = {"kind": "amount_in", "inputs": inputs}
                        try:
                            value_in = get_amount_in(amount, bin_.price, swap_for_y, rounding)
                        except Overflow as exc:
                            CHECKS.rejection("overflow")
                            record["want"] = None
                            record["rejected"] = "overflow"
                            record["rejected_detail"] = str(exc)
                        else:
                            record["want"] = {"amount_in": s(value_in)}
                            record["rejected"] = None
                        in_records.append(record)

                    # Self-check 2 on the pair we just emitted.
                    down, up = pair_results[DOWN], pair_results[UP]
                    if down is not None and up is not None:
                        require(up >= down, f"ceil < floor for bin {bin_.bin_id}: {up} < {down}")
                        require(
                            up - down <= 1,
                            f"ceil/floor differ by >1 for bin {bin_.bin_id}: {up} vs {down}",
                        )

                # get_max_amount_in (Rounding::Up sister of get_amount_in).
                inputs = dict(price_inputs(bin_), op="get_max_amount_in", swap_for_y=swap_for_y)
                record = {"kind": "amount_in", "inputs": inputs}
                try:
                    value = bin_.get_max_amount_in(bin_.price, swap_for_y)
                except Overflow as exc:
                    CHECKS.rejection("overflow")
                    record["want"] = None
                    record["rejected"] = "overflow"
                    record["rejected_detail"] = str(exc)
                else:
                    record["want"] = {"max_amount_in": s(value)}
                    record["rejected"] = None
                in_records.append(record)

                # calculate_out_amount for a sample of shares.
                for share in LIQUIDITY_SHARES:
                    inputs = dict(
                        price_inputs(bin_),
                        op="calculate_out_amount",
                        liquidity_share=u128j(share),
                        amount_x=s(bin_.amount_x),
                        amount_y=s(bin_.amount_y),
                        liquidity_supply=u128j(bin_.liquidity_supply),
                    )
                    record = {"kind": "amount_out", "inputs": inputs}
                    try:
                        out_x, out_y = calculate_out_amount(
                            bin_.amount_x, bin_.amount_y, bin_.liquidity_supply, share
                        )
                    except Overflow as exc:
                        reason = "zero_liquidity" if bin_.liquidity_supply == 0 else "overflow"
                        CHECKS.rejection(reason)
                        record["want"] = None
                        record["rejected"] = reason
                        record["rejected_detail"] = str(exc)
                    else:
                        record["want"] = {"out_amount_x": s(out_x), "out_amount_y": s(out_y)}
                        record["rejected"] = None
                    out_records.append(record)

    # An empty bin has liquidity_supply == 0; that is the zero_liquidity case.
    empty_bin = next(b for b in arrays[max(arrays)] if b.is_empty())
    for share in (0, 1, 2**64):
        inputs = {
            "bin_id": s(empty_bin.bin_id),
            "bin_array_index": s(empty_bin.array_index),
            "price": u128j(empty_bin.price),
            "op": "calculate_out_amount",
            "liquidity_share": u128j(share),
            "amount_x": s(empty_bin.amount_x),
            "amount_y": s(empty_bin.amount_y),
            "liquidity_supply": u128j(empty_bin.liquidity_supply),
        }
        record = {"kind": "amount_out", "inputs": inputs}
        try:
            calculate_out_amount(
                empty_bin.amount_x, empty_bin.amount_y, empty_bin.liquidity_supply, share
            )
        except Overflow as exc:
            CHECKS.rejection("zero_liquidity")
            record["want"] = None
            record["rejected"] = "zero_liquidity"
            record["rejected_detail"] = str(exc)
        else:
            fail("calculate_out_amount on a zero-liquidity bin did not raise")
        out_records.append(record)

    return out_records, in_records


def build_bin_quote_vectors() -> list[dict]:
    arrays = load_bin_arrays()
    pair = load_pair()
    records: list[dict] = []

    fee_inputs = {
        "pool": pair.pool,
        "bin_step": s(pair.bin_step),
        "base_factor": s(pair.base_factor),
        "base_fee_power_factor": s(pair.base_fee_power_factor),
        "variable_fee_control": s(pair.variable_fee_control),
        "protocol_share": s(pair.protocol_share),
    }

    def bin_inputs(bin_: Bin) -> dict:
        return {
            "bin_id": s(bin_.bin_id),
            "bin_array_index": s(bin_.array_index),
            "price": u128j(bin_.price),
            "amount_x": s(bin_.amount_x),
            "amount_y": s(bin_.amount_y),
            "liquidity_supply": u128j(bin_.liquidity_supply),
            "open_order_amount": s(bin_.open_order_amount),
            "processed_order_remaining_amount": s(bin_.processed_order_remaining_amount),
            "limit_order_ask_side": s(bin_.limit_order_ask_side),
        }

    def emit(
        bin_: Bin,
        in_amount: int,
        swap_for_y: bool,
        support_limit_order: bool,
        fee_on_input: bool,
        vol_acc: int,
        note: str | None = None,
    ) -> None:
        total_fee_rate = pair.get_total_fee_for_vol(vol_acc)
        inputs = dict(
            bin_inputs(bin_),
            **fee_inputs,
            volatility_accumulator=s(vol_acc),
            total_fee_rate=s(total_fee_rate),
            in_amount=s(in_amount),
            swap_for_y=swap_for_y,
            support_limit_order=support_limit_order,
            fee_on_input=fee_on_input,
        )
        if note:
            inputs["note"] = note
        record = {"kind": "bin_quote", "inputs": inputs}
        try:
            quote, fill = swap_exact_in_quote_at_bin(
                bin_,
                bin_.price,
                pair,
                total_fee_rate,
                in_amount,
                swap_for_y,
                support_limit_order,
                fee_on_input,
            )
        except Overflow as exc:
            CHECKS.rejection("overflow")
            record["want"] = None
            record["rejected"] = "overflow"
            record["rejected_detail"] = str(exc)
            records.append(record)
            return
        record["want"] = {
            "amount_in": s(quote.amount_in),
            "amount_out": s(quote.amount_out),
            "fee": s(quote.fee),
            "protocol_fee": s(quote.protocol_fee),
        }
        record["trace"] = {
            "max_amount_out": s(
                bin_.get_max_amount_out_with_limit_orders(swap_for_y, support_limit_order)
            ),
            "fill_amount_in": s(fill.amount_in),
            "fill_amount_left": s(fill.amount_left),
            "fill_out_amount": s(fill.out_amount),
            "mm_amount_in": s(fill.mm_amount_in),
        }
        record["rejected"] = None
        records.append(record)

    # Dense sweep: every non-empty bin, both directions, both fee_on_input values,
    # support_limit_order both ways, three volatility accumulators.
    for array_index in sorted(arrays):
        for bin_ in arrays[array_index]:
            if bin_.is_empty():
                continue
            for swap_for_y in (True, False):
                amounts = BIN_QUOTE_AMOUNTS_Y if swap_for_y else BIN_QUOTE_AMOUNTS_X
                for support_limit_order in (True, False):
                    for fee_on_input in (False, True):
                        for vol_acc in BIN_QUOTE_VOL_ACC:
                            for in_amount in amounts:
                                emit(
                                    bin_,
                                    in_amount,
                                    swap_for_y,
                                    support_limit_order,
                                    fee_on_input,
                                    vol_acc,
                                )

    # The two integration scenarios, replayed bin by bin. Each step is emitted as an
    # ordinary bin_quote record whose inputs carry the traversal's in_amount and
    # volatility_accumulator, so the multi-bin composition stays checkable without a
    # new vector kind.
    scenarios = (
        ("exact_in_x_to_y", 40_000_000_000, True),
        ("exact_in_y_to_x", 45_000_000_000_000, False),
    )
    scenario_totals = {}
    for name, amount_in, swap_for_y in scenarios:
        for support_limit_order in (True, False):
            for fee_on_input in (False, True):
                # index_reference == active_id and volatility_reference == 0 is what
                # quote.rs::update_references leaves behind for this fixture: the pool's
                # last_update_timestamp is 0 and elapsed >= decay_period (120s).
                result = quote_exact_in_traversal(
                    pair,
                    arrays,
                    amount_in,
                    swap_for_y,
                    support_limit_order,
                    fee_on_input,
                    index_reference=pair.active_id,
                    volatility_reference=0,
                )
                for step in result["steps"]:
                    step_array_index = bin_array_index_of(step["bin_id"])
                    bin_ = arrays[step_array_index][
                        step["bin_id"] - step_array_index * MAX_BIN_PER_ARRAY
                    ]
                    require(
                        step["quote"].amount_out > 0 or step["quote"].amount_in == 0,
                        "traversal step with amount_in == 0 should have been skipped",
                    )
                    emit(
                        bin_,
                        step["in_amount"],
                        swap_for_y,
                        support_limit_order,
                        fee_on_input,
                        step["volatility_accumulator"],
                        note=(
                            f"{name}: traversal step from amount_in={amount_in}, "
                            f"active_id={bin_.bin_id}"
                        ),
                    )
                key = (name, support_limit_order, fee_on_input)
                scenario_totals[key] = result

    # The scenario the integration test actually runs: function_type == LimitOrder and
    # collect_fee_mode == OnlyY, so support_limit_order = true and fee_on_input = !swap_for_y.
    real = {}
    for name, amount_in, swap_for_y in scenarios:
        support_limit_order = pair.is_support_limit_order()
        fee_on_input = pair.fee_on_input(swap_for_y)
        key = (name, support_limit_order, fee_on_input)
        real[name] = scenario_totals[key]
        require(support_limit_order is True, "fixture pool must support limit orders")
        bins_consumed = len(scenario_totals[key]["steps"])
        require(
            bins_consumed >= 5,
            f"{name}: integration test requires the active bin to move >= 5 bins, got {bins_consumed}",
        )
    require(
        real["exact_in_x_to_y"]["amount_out"] > 0,
        "exact_in_x_to_y produced no output",
    )
    require(
        real["exact_in_y_to_x"]["amount_out"] > 0,
        "exact_in_y_to_x produced no output",
    )
    CHECKS.scenario_totals = real
    return records


def run_inverse_self_check() -> None:
    """Self-check 3: get_amount_out and get_amount_in invert to within one unit."""
    arrays = load_bin_arrays()
    for array_index in sorted(arrays):
        for bin_ in arrays[array_index]:
            if bin_.is_empty():
                continue
            for swap_for_y in (True, False):
                for amount in (1, 2, 100, 1_000_000, 1_000_000_000, 2**32):
                    try:
                        out_d = get_amount_out(amount, bin_.price, swap_for_y, DOWN)
                        out_u = get_amount_out(amount, bin_.price, swap_for_y, UP)
                        back_from_out_d = get_amount_in(out_d, bin_.price, swap_for_y, UP)
                        back_from_out_u = get_amount_in(out_u, bin_.price, swap_for_y, DOWN)
                    except Overflow:
                        continue
                    for label, back in (
                        ("out(down)->in(up)", back_from_out_d),
                        ("out(up)->in(down)", back_from_out_u),
                    ):
                        if abs(back - amount) > 1:
                            CHECKS.inverse_mismatches.append(
                                f"bin {bin_.bin_id} swap_for_y={swap_for_y} amount={amount} "
                                f"{label} -> {back}"
                            )
                    try:
                        in_d = get_amount_in(amount, bin_.price, swap_for_y, DOWN)
                        in_u = get_amount_in(amount, bin_.price, swap_for_y, UP)
                        fwd_d = get_amount_out(in_d, bin_.price, swap_for_y, UP)
                        fwd_u = get_amount_out(in_u, bin_.price, swap_for_y, DOWN)
                    except Overflow:
                        continue
                    for label, fwd in (("in(down)->out(up)", fwd_d), ("in(up)->out(down)", fwd_u)):
                        if abs(fwd - amount) > 1:
                            CHECKS.inverse_mismatches.append(
                                f"bin {bin_.bin_id} swap_for_y={swap_for_y} amount={amount} "
                                f"{label} -> {fwd}"
                            )


def run_price_self_check() -> None:
    """Self-checks 5 and 6."""
    for bin_step in PRICE_BIN_STEPS:
        price = get_price_from_id(0, bin_step)
        require(
            price == ONE,
            f"get_price_from_id(0, {bin_step}) == {price}, expected {ONE}",
        )
    arrays = load_bin_arrays()
    checked = 0
    for array_index in sorted(arrays):
        for bin_ in arrays[array_index]:
            computed = get_price_from_id(bin_.bin_id, FIXTURE_BIN_STEP)
            require(
                computed == bin_.price,
                f"stored price mismatch for bin {bin_.bin_id}: "
                f"stored {bin_.price}, computed {computed}",
            )
            checked += 1
    CHECKS.note(f"reproduced {checked} stored BinArray prices from get_price_from_id")


def run_fee_inverse_self_check() -> None:
    """Self-check 7, second half: compute_fee and compute_fee_from_amount are inverse."""
    pair = load_pair()
    rate = pair.get_total_fee()
    for amount in (1, 7, 999, 1_000_000, 1_000_000_000, 2**32, 2**63):
        fee = pair.compute_fee(amount, rate)
        # fee is the fee charged on an input that is then reduced to `amount`.
        back = pair.compute_fee_from_amount(amount + fee, rate)
        if abs(back - fee) > 1:
            CHECKS.note(
                f"fee inversion drift > 1 at amount={amount}: compute_fee={fee}, "
                f"compute_fee_from_amount(amount+fee)={back}"
            )


# ---------------------------------------------------------------------- main ---
def generate() -> dict[str, str]:
    run_price_self_check()
    run_inverse_self_check()
    run_fee_inverse_self_check()

    files: dict[str, str] = {}

    price_records = build_price_vectors()
    files["price.json"] = dump_file(
        {
            "generated_by": GENERATED_BY,
            "kind": "price",
            "source": "testdata/decoded/{LbPair,BinArray}.json + the Rust reference",
            "note": "get_price_from_id, Q64.64, exact integer pow with per-step truncation",
        },
        price_records,
    )

    fee_records = build_fee_vectors()
    files["fee.json"] = dump_file(
        {
            "generated_by": GENERATED_BY,
            "kind": "fee",
            "source": "testdata/decoded/LbPair.json + one synthetic MAX_FEE_RATE pair",
            "note": "inputs.op selects the function: get_base_fee, compute_variable_fee, "
            "get_total_fee, compute_fee, compute_fee_from_amount, compute_protocol_fee",
        },
        fee_records,
    )

    split_records = build_split_fee_vectors()
    files["split_fee.json"] = dump_file(
        {
            "generated_by": GENERATED_BY,
            "kind": "split_fee",
            "source": "quote.rs::split_fee",
            "note": "mm_fee is a ceiling division; lo_fee and both protocol shares are floors",
        },
        split_records,
    )

    out_records, in_records = build_amount_vectors()
    files["amount_out.json"] = dump_file(
        {
            "generated_by": GENERATED_BY,
            "kind": "amount_out",
            "source": "testdata/decoded/BinArray.json + the Rust reference",
            "note": "inputs.op selects the function: get_amount_out, get_max_amount_out, "
            "calculate_out_amount",
        },
        out_records,
    )
    files["amount_in.json"] = dump_file(
        {
            "generated_by": GENERATED_BY,
            "kind": "amount_in",
            "source": "testdata/decoded/BinArray.json + the Rust reference",
            "note": "inputs.op selects the function: get_amount_in, get_max_amount_in",
        },
        in_records,
    )

    bin_records = build_bin_quote_vectors()
    files["bin_quote.json"] = dump_file(
        {
            "generated_by": GENERATED_BY,
            "kind": "bin_quote",
            "source": "testdata/decoded/{LbPair,BinArray}.json + quote.rs",
            "note": "quote.rs::swap_exact_in_quote_at_bin. Records with a `note` in inputs are "
            "traversal steps of the multi-bin quote_exact_in for the two integration "
            "scenarios; the rest is a dense sweep.",
        },
        bin_records,
    )
    return files


def report(counts: dict[str, int]) -> None:
    print("gen_bin_vectors: self-checks")
    for note in CHECKS.notes:
        print(f"  ok: {note}")
    if CHECKS.inverse_mismatches:
        print(f"  REPORT: {len(CHECKS.inverse_mismatches)} amount_out/amount_in inverse "
              f"mismatch(es) beyond one unit")
        for line in CHECKS.inverse_mismatches[:20]:
            print(f"    {line}")
        if len(CHECKS.inverse_mismatches) > 20:
            print(f"    ... and {len(CHECKS.inverse_mismatches) - 20} more")
    else:
        print("  ok: get_amount_out and get_amount_in invert within one unit for every "
              "sampled bin/direction/amount")
    if CHECKS.rejections:
        summary = ", ".join(f"{k}={v}" for k, v in sorted(CHECKS.rejections.items()))
        print(f"  ok: recorded rejections: {summary}")
    if hasattr(CHECKS, "scenario_totals"):
        for name, result in CHECKS.scenario_totals.items():
            print(
                f"  scenario {name}: amount_out={result['amount_out']} "
                f"fee={result['fee']} protocol_fee={result['protocol_fee']} "
                f"bins={len(result['steps'])} active_id_end={result['active_id_end']}"
            )
    print("gen_bin_vectors: vectors")
    for name, count in counts.items():
        print(f"  {name}: {count}")


def main(argv: list[str]) -> int:
    check_only = "--check" in argv[1:]
    unknown = [a for a in argv[1:] if a != "--check"]
    if unknown:
        print(f"usage: {argv[0]} [--check]", file=sys.stderr)
        return 2

    files = generate()

    counts = {name: json.loads(text)["count"] for name, text in files.items()}

    report(counts)

    drift = []
    for name, text in files.items():
        path = OUT_DIR / name
        if not path.exists() or path.read_text() != text:
            drift.append(name)
            if not check_only:
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(text)
    if check_only:
        if drift:
            print(f"gen_bin_vectors: DRIFT in {', '.join(sorted(drift))}", file=sys.stderr)
            return 1
        print("gen_bin_vectors: --check clean, no file written")
        return 0
    print(f"gen_bin_vectors: wrote {len(files)} file(s) under {OUT_DIR.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
