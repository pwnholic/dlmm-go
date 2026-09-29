#!/usr/bin/env python3
"""Verify that program/lbclmm/constants.go matches idls/dlmm.json.

Why this exists
---------------
The constants in the Go bindings are the most dangerous kind of transcription:
they are plain numbers, so a typo produces a plausible value that compiles and
runs, and only diverges from the program in a rare branch. A wrong
MAX_BIN_PER_ARRAY, for example, produces bin array indexes that resolve to
accounts that do not exist.

So the values are not trusted on review. They are compared, mechanically,
against the vendored IDL. This runs under `make generate-check`.

Exit code is 0 when every pair matches, 1 otherwise.
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
IDL_PATH = ROOT / "idls" / "dlmm.json"
GO_PATH = ROOT / "program" / "lbclmm" / "constants.go"
PDA_PATH = ROOT / "pda" / "pda.go"
RUST_CONSTANTS_PATH = ROOT / "idls" / "dlmm-commons-constants.rs"

# Go identifier -> IDL constant name.
#
# An explicit table rather than a name transform: MAX_BIN_PER_ARRAY mapping to
# MaxBinPerArray by camel-casing rules is a guess, and a guess that silently
# matches the wrong pair is worse than no check at all.
SCALARS = {
    "MaxBinPerArray": "MAX_BIN_PER_ARRAY",
    "DefaultBinPerPosition": "DEFAULT_BIN_PER_POSITION",
    "PositionMaxLength": "POSITION_MAX_LENGTH",
    "BinArrayBitmapSize": "BIN_ARRAY_BITMAP_SIZE",
    "ExtensionBinArrayBitmapSize": "EXTENSION_BINARRAY_BITMAP_SIZE",
    "MaxBinIDPerBinStep": "MAX_BIN_ID_PER_BIN_STEP",
    "MaxBinStep": "MAX_BIN_STEP",
    "BasisPointMax": "BASIS_POINT_MAX",
    "FeeDenominator": "FEE_DENOMINATOR",
    "NumRewards": "NUM_REWARDS",
    "MaxBinPerLimitOrder": "MAX_BIN_PER_LIMIT_ORDER",
    "MaxRewardBinSplit": "MAX_REWARD_BIN_SPLIT",
    "MaxResizeLength": "MAX_RESIZE_LENGTH",
    "MinRewardDuration": "MIN_REWARD_DURATION",
    "MaxRewardDuration": "MAX_REWARD_DURATION",
    "ProtocolShareDefault": "PROTOCOL_SHARE",
    "MaxProtocolShare": "MAX_PROTOCOL_SHARE",
    "HostFeeBps": "HOST_FEE_BPS",
    "ILMProtocolShare": "ILM_PROTOCOL_SHARE",
    "LimitOrderFeeShare": "LIMIT_ORDER_FEE_SHARE",
}

# The four u128 constants, written as num.U128FromU64(<literal>).
U128S = {
    "MaxBaseFee": "MAX_BASE_FEE",
    "MinBaseFee": "MIN_BASE_FEE",
    "MaxFeeRate": "MAX_FEE_RATE",
    "MinimumLiquidity": "MINIMUM_LIQUIDITY",
}

# Derived bounds: Go name -> (expression as a function of the IDL primitives).
DERIVED = {
    "MinBinArrayIndexInDefaultBitmap": lambda c: -c["BIN_ARRAY_BITMAP_SIZE"],
    "MaxBinArrayIndexInDefaultBitmap": lambda c: c["BIN_ARRAY_BITMAP_SIZE"] - 1,
    "MinBinArrayIndexWithExtension": lambda c: (
        -c["BIN_ARRAY_BITMAP_SIZE"] * (c["EXTENSION_BINARRAY_BITMAP_SIZE"] + 1)
    ),
    "MaxBinArrayIndexWithExtension": lambda c: (
        c["BIN_ARRAY_BITMAP_SIZE"] * (c["EXTENSION_BINARRAY_BITMAP_SIZE"] + 1) - 1
    ),
}


# PDA seed literals: IDL constant name -> Go constant in pda/pda.go.
#
# A mistyped seed produces an address that is a perfectly valid public key and
# simply has no account behind it, so the failure is silent and shows up much
# later as an empty account. Checking the bytes against the IDL is cheap.
SEEDS = {
    "seedBinArray": "BIN_ARRAY",
    "seedBinArrayBitmap": "BIN_ARRAY_BITMAP_SEED",
    "seedClaimProtocolFeeOperator": "CLAIM_PROTOCOL_FEE_OPERATOR",
    "seedOperator": "OPERATOR_PREFIX",
    "seedOracle": "ORACLE",
    "seedPosition": "POSITION",
    "seedPresetParameter": "PRESET_PARAMETER",
    "seedPresetParameter2": "PRESET_PARAMETER2",
}


# Constants that exist in the Rust reference but NOT in the IDL.
#
# MAX_BIN_ID_PER_BIN_STEP (351639, in the IDL) and MAX_BIN_ID (443636, Rust only)
# are different quantities. Bounding bin IDs by the wrong one silently refuses
# legitimate bin IDs, so the Rust source is checked as a second authority.
RUST_SCALARS = {
    "MinBinID": "MIN_BIN_ID",
    "MaxBinID": "MAX_BIN_ID",
}


def parse_rust_constants(src: str) -> dict[str, int]:
    """Extract `pub const NAME: <type> = <literal>;` numeric constants."""
    out: dict[str, int] = {}

    for m in re.finditer(
        r"^\s*pub const ([A-Z0-9_]+)\s*:\s*[a-z0-9]+\s*=\s*(-?[0-9_]+)\s*;", src, re.M
    ):
        out[m.group(1)] = go_int(m.group(2))

    return out


def idl_int(raw: str) -> int | None:
    """Coerce an IDL constant value to an int, or None when it is not numeric.

    Every value in the IDL is a string. Numeric constants are decimal strings;
    seed constants are stringified byte arrays and must not be silently coerced.
    """
    s = raw.strip()
    if re.fullmatch(r"-?\d+", s):
        return int(s)
    return None


def idl_bytes(raw: str) -> bytes | None:
    """Decode a stringified byte array such as '[98, 105, 110]'."""
    s = raw.strip()
    if not (s.startswith("[") and s.endswith("]")):
        return None

    body = s[1:-1].strip()
    if not body:
        return b""

    try:
        return bytes(int(part) for part in body.split(","))
    except ValueError:
        return None


def parse_go_byte_strings(src: str) -> dict[str, bytes]:
    """Extract `name = "literal"` pairs declared as Go byte-slice strings."""
    out: dict[str, bytes] = {}

    for m in re.finditer(r'^\s*([a-zA-Z]\w*)\s*=\s*"((?:[^"\\]|\\.)*)"\s*$', src, re.M):
        out[m.group(1)] = m.group(2).encode()

    return out


def go_int(text: str) -> int:
    """Parse a Go integer literal, allowing digit-group underscores."""
    return int(text.replace("_", ""), 10)


def parse_go_constants(src: str) -> dict[str, int]:
    """Extract `Name = <int>` pairs, including inside const blocks."""
    out: dict[str, int] = {}

    for m in re.finditer(r"^\s*([A-Z]\w*)\s*=\s*([0-9_]+)\s*$", src, re.M):
        out[m.group(1)] = go_int(m.group(2))

    # Derived bounds are negative and written as `-BinArrayBitmapSize`.
    for m in re.finditer(r"^\s*([A-Z]\w*)\s*=\s*-\s*([A-Z]\w*)\s*$", src, re.M):
        name, base = m.group(1), m.group(2)
        if base in out:
            out[name] = -out[base]

    # Negative literals: `A = -123`.
    for m in re.finditer(r"^\s*([A-Z]\w*)\s*=\s*-\s*([0-9_]+)\s*$", src, re.M):
        out[m.group(1)] = -go_int(m.group(2))

    # Derived bounds subtracting a literal: `A = B - 1`.
    for m in re.finditer(
        r"^\s*([A-Z]\w*)\s*=\s*([A-Z]\w*)\s*-\s*([0-9_]+)\s*$", src, re.M
    ):
        name, base = m.group(1), m.group(2)
        if base in out:
            out[name] = out[base] - go_int(m.group(3))

    # Derived bounds built from products: A*(B+1)-1 and -A*(B+1).
    for m in re.finditer(
        r"^\s*([A-Z]\w*)\s*=\s*([A-Z]\w*)\s*\*\s*\(\s*([A-Z]\w*)\s*\+\s*1\s*\)\s*-\s*1\s*$",
        src,
        re.M,
    ):
        a, b = m.group(2), m.group(3)
        if a in out and b in out:
            out[m.group(1)] = out[a] * (out[b] + 1) - 1

    for m in re.finditer(
        r"^\s*([A-Z]\w*)\s*=\s*-\s*([A-Z]\w*)\s*\*\s*\(\s*([A-Z]\w*)\s*\+\s*1\s*\)\s*$",
        src,
        re.M,
    ):
        a, b = m.group(2), m.group(3)
        if a in out and b in out:
            out[m.group(1)] = -out[a] * (out[b] + 1)

    return out


def parse_go_u128(src: str) -> dict[str, int]:
    """Extract `Name = num.U128FromU64(<literal>)` pairs."""
    out: dict[str, int] = {}

    for m in re.finditer(
        r"^\s*([A-Z]\w*)\s*=\s*num\.U128FromU64\(\s*([0-9_]+)\s*\)\s*$", src, re.M
    ):
        out[m.group(1)] = go_int(m.group(2))

    return out


def main() -> int:
    if not IDL_PATH.exists():
        print(f"FAIL: IDL not found at {IDL_PATH}", file=sys.stderr)
        return 1
    if not GO_PATH.exists():
        print(f"FAIL: constants file not found at {GO_PATH}", file=sys.stderr)
        return 1

    idl = json.loads(IDL_PATH.read_text())
    idl_raw = {c["name"]: c["value"] for c in idl.get("constants", [])}
    idl_const = {k: v for k, v in ((k, idl_int(r)) for k, r in idl_raw.items()) if v is not None}

    src = GO_PATH.read_text()
    goscalar = parse_go_constants(src)
    gou128 = parse_go_u128(src)

    failures: list[str] = []
    checked = 0

    # 1) Scalar constants.
    for go_name, idl_name in SCALARS.items():
        if idl_name not in idl_const:
            failures.append(f"{idl_name}: present in Go bindings but absent from the IDL")
            continue

        want = idl_const[idl_name]
        if not isinstance(want, int):
            continue

        if go_name not in goscalar:
            failures.append(f"{go_name}: not found in constants.go")
            continue

        checked += 1
        if goscalar[go_name] != want:
            failures.append(f"{go_name}: Go has {goscalar[go_name]}, IDL has {want}")

    # 2) u128 constants.
    for go_name, idl_name in U128S.items():
        if idl_name not in idl_const:
            failures.append(f"{idl_name}: present in Go bindings but absent from the IDL")
            continue

        want = idl_const[idl_name]
        if go_name not in gou128:
            failures.append(f"{go_name}: not found as num.U128FromU64(...) in constants.go")
            continue

        checked += 1
        if gou128[go_name] != want:
            failures.append(f"{go_name}: Go has {gou128[go_name]}, IDL has {want}")

    # 3) Derived bitmap bounds.
    for go_name, expr in DERIVED.items():
        if go_name not in goscalar:
            # Distinguish "absent" from "present but the parser could not read
            # it": both fail the check, but only one of them means someone
            # needs to extend the parser.
            if re.search(rf"^\s*{re.escape(go_name)}\s*=", src, re.M):
                failures.append(
                    f"{go_name}: value could not be parsed; the derivation patterns "
                    f"in this script must be extended"
                )
            else:
                failures.append(f"{go_name}: not found in constants.go")
            continue

        checked += 1
        want = expr(idl_const)
        if goscalar[go_name] != want:
            failures.append(f"{go_name}: Go has {goscalar[go_name]}, derivation gives {want}")

    # 4) PDA seed literals against the IDL byte arrays.
    if PDA_PATH.exists():
        seeds = parse_go_byte_strings(PDA_PATH.read_text())

        for go_name, idl_name in SEEDS.items():
            if idl_name not in idl_raw:
                failures.append(f"{idl_name}: seed absent from the IDL")
                continue

            want = idl_bytes(idl_raw[idl_name])
            if want is None:
                failures.append(f"{idl_name}: IDL value is not a byte array")
                continue

            if go_name not in seeds:
                failures.append(f"{go_name}: not found in pda/pda.go")
                continue

            checked += 1
            if seeds[go_name] != want:
                failures.append(
                    f"{go_name}: Go has {seeds[go_name]!r}, IDL has {want!r}"
                )

    # 6) Constants that live in the Rust reference rather than the IDL.
    #
    # The Rust file is vendored under idls/ rather than read from
    # package-referense/, which is gitignored: a fresh clone must be able to run
    # this check. A missing file is a failure, not a skip, so the check cannot
    # quietly stop covering these two values.
    if not RUST_CONSTANTS_PATH.exists():
        failures.append(
            f"{RUST_CONSTANTS_PATH.name} is missing; MinBinID and MaxBinID cannot be verified"
        )
    else:
        rust = parse_rust_constants(RUST_CONSTANTS_PATH.read_text())

        for go_name, rust_name in RUST_SCALARS.items():
            if rust_name not in rust:
                failures.append(f"{rust_name}: expected in constants.rs but not found")
                continue

            if go_name not in goscalar:
                failures.append(f"{go_name}: not found in constants.go")
                continue

            checked += 1
            if goscalar[go_name] != rust[rust_name]:
                failures.append(
                    f"{go_name}: Go has {goscalar[go_name]}, constants.rs has {rust[rust_name]}"
                )

        # The two bin ID quantities must not be confused with each other.
        if rust.get("MAX_BIN_ID") == idl_const.get("MAX_BIN_ID_PER_BIN_STEP"):
            failures.append(
                "MAX_BIN_ID and MAX_BIN_ID_PER_BIN_STEP are equal; the distinction "
                "this check exists to protect may no longer hold"
            )

    # 7) Independent sanity: the extension must strictly widen the default range.
    if (
        goscalar.get("MinBinArrayIndexWithExtension", 0)
        >= goscalar.get("MinBinArrayIndexInDefaultBitmap", 0)
    ):
        failures.append("extension range does not widen the bitmap downwards")
    if (
        goscalar.get("MaxBinArrayIndexWithExtension", 0)
        <= goscalar.get("MaxBinArrayIndexInDefaultBitmap", 0)
    ):
        failures.append("extension range does not widen the bitmap upwards")

    # 5) The largest bin array index the program allows must be reachable.
    max_reachable = goscalar.get("MaxBinArrayIndexWithExtension", 0)
    max_bin_id = goscalar.get("MaxBinID", 0)
    max_bin_per_array = goscalar.get("MaxBinPerArray", 1)
    needed_index = max_bin_id // max_bin_per_array
    if needed_index > max_reachable:
        failures.append(
            f"MaxBinID ({max_bin_id}) needs bin array index {needed_index}, "
            f"but the extension only reaches {max_reachable}"
        )

    min_bin_id = goscalar.get("MinBinID", 0)
    min_needed = -((-min_bin_id + max_bin_per_array - 1) // max_bin_per_array)
    if min_needed < goscalar.get("MinBinArrayIndexWithExtension", 0):
        failures.append(
            f"MinBinID ({min_bin_id}) needs bin array index {min_needed}, "
            f"but the extension only reaches {goscalar.get('MinBinArrayIndexWithExtension')}"
        )

    if failures:
        print("IDL constant check FAILED:", file=sys.stderr)
        for f in failures:
            print(f"  - {f}", file=sys.stderr)
        return 1

    print(f"IDL constant check passed: {checked} values match idls/dlmm.json")
    return 0


if __name__ == "__main__":
    sys.exit(main())
