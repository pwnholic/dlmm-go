#!/usr/bin/env python3
"""Generate golden account vectors for the Meteora DLMM Go decoder.

This decoder is written independently from ``idls/dlmm.json`` alone. It does not
import, read, or mirror any Go source. Byte offsets are derived here by walking
the IDL type graph with plain Borsh rules:

  * scalars are little-endian, fixed width, no padding between fields;
  * fields appear in declaration order, nested structs inline;
  * a fixed array ``[T; N]`` is N consecutive inline encodings;
  * ``u8`` arrays are byte blobs, not integers;
  * every account begins with an 8-byte anchor discriminator.

Python standard library only. No network, no Go toolchain.

Usage:
    python3 tools/gen_golden/gen_account_vectors.py [--check]

``--check`` regenerates into memory and compares against the committed files
under ``testdata/decoded/`` instead of writing them.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import sys

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
IDL_PATH = os.path.join(REPO_ROOT, "idls", "dlmm.json")
FIXTURE_ROOT = os.path.join(
    REPO_ROOT, "package-referense", "dlmm-sdk", "commons", "tests", "fixtures"
)
OUT_DIR = os.path.join(REPO_ROOT, "testdata", "decoded")

BASE58_ALPHABET = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

# Fixture file name -> the DLMM account it holds, or None for foreign programs.
FIXTURE_FILES = (
    "lb_pair.bin",
    "bin_array_1.bin",
    "bin_array_2.bin",
    "oracle.bin",
)

# Fixtures belonging to the SPL Token program, used only as a negative control.
SPL_FIXTURE_FILES = (
    "reserve_x.bin",
    "reserve_y.bin",
    "token_x_mint.bin",
    "token_y_mint.bin",
)

# Size anchors that the vendored IDL must reproduce against the real dumps.
# "account" is struct + 8 discriminator bytes. "fixture" is the observed dump
# size; Oracle is larger than its base struct because increase_oracle_length
# appends an observation buffer past the declared fields.
SIZE_ANCHORS = {
    "LbPair": {"struct": 896, "account": 904, "fixture": 904},
    "BinArray": {"struct": 10128, "account": 10136, "fixture": 10136},
    "Oracle": {"struct": 24, "account": 32, "fixture": 3232},
}

SCALAR_WIDTHS = {
    "bool": 1,
    "u8": 1,
    "i8": 1,
    "u16": 2,
    "i16": 2,
    "u32": 4,
    "i32": 4,
    "u64": 8,
    "i64": 8,
    "u128": 16,
    "i128": 16,
    "pubkey": 32,
}


class DecodeError(Exception):
    """Raised when the IDL layout cannot be applied to the given bytes."""


# --------------------------------------------------------------------------
# base58 (Bitcoin alphabet, the alphabet Solana uses for base58 pubkeys)
# --------------------------------------------------------------------------


def b58encode(raw: bytes) -> str:
    n = int.from_bytes(raw, "big")
    out = ""
    while n:
        n, rem = divmod(n, 58)
        out = BASE58_ALPHABET[rem] + out
    pad = 0
    for byte in raw:
        if byte == 0:
            pad += 1
        else:
            break
    return "1" * pad + out


def b58decode(text: str) -> bytes:
    n = 0
    for ch in text:
        idx = BASE58_ALPHABET.find(ch)
        if idx < 0:
            raise DecodeError("invalid base58 character %r" % ch)
        n = n * 58 + idx
    body = n.to_bytes((n.bit_length() + 7) // 8, "big") if n else b""
    pad = 0
    for ch in text:
        if ch == "1":
            pad += 1
        else:
            break
    return b"\x00" * pad + body


# --------------------------------------------------------------------------
# IDL model
# --------------------------------------------------------------------------


class Idl:
    def __init__(self, path: str) -> None:
        with open(path, "r", encoding="utf-8") as handle:
            doc = json.load(handle)
        self.types = {t["name"]: t for t in doc["types"]}
        self.accounts = list(doc["accounts"])
        self.disc_to_account = {}
        for account in self.accounts:
            key = bytes(account["discriminator"])
            if len(key) != 8:
                raise DecodeError("account %s has a non 8-byte discriminator" % account["name"])
            self.disc_to_account[key] = account["name"]

    def defined(self, name: str) -> dict:
        try:
            entry = self.types[name]
        except KeyError:
            raise DecodeError("IDL has no type named %r" % name)
        if entry["type"]["kind"] != "struct":
            raise DecodeError("%s is not a struct type" % name)
        return entry["type"]

    def enum(self, name: str) -> list:
        try:
            entry = self.types[name]
        except KeyError:
            raise DecodeError("IDL has no type named %r" % name)
        if entry["type"]["kind"] != "enum":
            raise DecodeError("%s is not an enum type" % name)
        return entry["type"]["variants"]


def type_kind(ty) -> str:
    """Classify an IDL type node: primitive, defined, array, vec, option, or unknown."""
    if isinstance(ty, str):
        if ty in SCALAR_WIDTHS:
            return "primitive"
        return "defined"
    if not isinstance(ty, dict) or len(ty) != 1:
        raise DecodeError("unrecognised IDL type node: %r" % (ty,))
    (head, tail), = ty.items()
    if head in ("defined", "option", "coption", "vec", "array", "optionArray"):
        return head
    raise DecodeError("unrecognised IDL type key %r" % head)


def static_size(idl: Idl, ty) -> int:
    """Size in bytes of a fixed-width type. Raises for anything dynamic."""
    kind = type_kind(ty)
    if kind == "primitive":
        return SCALAR_WIDTHS[ty]
    if kind == "defined":
        return struct_size(idl, ty["defined"]["name"])
    if kind == "array":
        inner, count = ty["array"]
        if not isinstance(count, int):
            raise DecodeError("non-literal array length %r" % (count,))
        return count * static_size(idl, inner)
    if kind == "option":
        raise DecodeError("Option<> has no static size")
    if kind == "vec":
        raise DecodeError("Vec<> has no static size")
    raise DecodeError("unrecognised type node for sizing: %r" % (ty,))


def struct_size(idl: Idl, name: str) -> int:
    total = 0
    for field in idl.defined(name)["fields"]:
        total += static_size(idl, field["type"])
    return total


def enum_index(idl: Idl, name: str, value: int):
    variants = idl.enum(name)
    if 0 <= value < len(variants):
        return {"index": value, "name": variants[value]["name"]}
    return {"index": value, "name": None}


# --------------------------------------------------------------------------
# decoding
# --------------------------------------------------------------------------


class Decoder:
    def __init__(self, idl: Idl, data: bytes) -> None:
        self.idl = idl
        self.data = data

    def _slice(self, offset: int, width: int) -> bytes:
        if offset < 0 or width < 0 or offset + width > len(self.data):
            raise DecodeError(
                "field would read bytes [%d,%d) but the account is only %d bytes"
                % (offset, offset + width, len(self.data))
            )
        return self.data[offset:offset + width]

    def scalar(self, name: str, offset: int):
        width = SCALAR_WIDTHS[name]
        raw = self._slice(offset, width)
        if name == "bool":
            if raw[0] > 1:
                raise DecodeError("bool at offset %d has non 0/1 value %d" % (offset, raw[0]))
            return bool(raw[0])
        if name == "pubkey":
            return b58encode(raw)
        signed = name[0] == "i"
        value = int.from_bytes(raw, "little", signed=signed)
        if name == "u128":
            lo = int.from_bytes(raw[0:8], "little")
            hi = int.from_bytes(raw[8:16], "little")
            return {"__u128__": True, "dec": str(value), "lo": lo, "hi": hi}
        return value

    def emit(self, path: str, offset: int, width: int, value) -> dict:
        entry = {"offset": offset, "bytes": width, "value": value}
        if isinstance(value, dict) and value.get("__u128__"):
            entry["value"] = value["dec"]
            entry["lo"] = value["lo"]
            entry["hi"] = value["hi"]
        elif width == 2 and isinstance(value, str) and _is_hex(value):
            # 2-byte [u8; 2] fields (bin_step_seed, base_factor_seed) are byte
            # blobs in the IDL. Their little-endian u16 reading is the field's
            # real meaning, so surface it as an additive key.
            entry["u16le"] = int.from_bytes(bytes.fromhex(value), "little")
        return {path: entry}

    def fields(self, name: str, offset: int, prefix: str = "") -> dict:
        """Decode every declared field of struct ``name`` starting at ``offset``.

        Field paths are dotted and rooted at ``prefix``, so a nested struct such
        as ``StaticParameters`` inside ``LbPair`` yields ``parameters.base_factor``.
        """
        out = {}
        cursor = offset
        for field in self.idl.defined(name)["fields"]:
            out.update(
                self.field(prefix + field["name"], field["type"], cursor, name)
            )
            cursor += static_size(self.idl, field["type"])
        return out

    def field(self, path: str, ty, offset: int, owner: str) -> dict:
        kind = type_kind(ty)

        if kind == "primitive":
            width = SCALAR_WIDTHS[ty]
            return self.emit(path, offset, width, self.scalar(ty, offset))

        if kind == "defined":
            name = ty["defined"]["name"]
            entry = self.idl.types.get(name)
            if entry is None:
                raise DecodeError("unknown defined type %r" % name)
            if entry["type"]["kind"] == "enum":
                width = static_size(self.idl, ty)
                raw = self._slice(offset, width)
                return self.emit(
                    path, offset, width, enum_index(self.idl, name, raw[0])
                )
            if entry["type"]["kind"] != "struct":
                raise DecodeError("defined type %r is neither struct nor enum" % name)
            return self.fields(name, offset, prefix=path + ".")

        if kind == "array":
            inner, count = ty["array"]
            if not isinstance(count, int):
                raise DecodeError("non-literal array length %r" % (count,))
            out = {}
            cursor = offset
            for i in range(count):
                out.update(self.field("%s[%d]" % (path, i), inner, cursor, owner))
                cursor += static_size(self.idl, inner)
            return out

        raise DecodeError(
            "%s.%s uses unsupported IDL type shape %r; account accounts must be "
            "fixed width" % (owner, path, ty)
        )


def _is_hex(text: str) -> bool:
    try:
        bytes.fromhex(text)
    except ValueError:
        return False
    return len(text) % 2 == 0


def bytes_to_hex(raw: bytes) -> str:
    return raw.hex()


class RawDecoder(Decoder):
    """Same walk, but [u8; N] becomes a hex string instead of N entries."""

    def field(self, path: str, ty, offset: int, owner: str) -> dict:
        if isinstance(ty, dict) and "array" in ty:
            inner, count = ty["array"]
            if inner == "u8" and isinstance(count, int):
                raw = self._slice(offset, count)
                return self.emit(path, offset, count, bytes_to_hex(raw))
        return Decoder.field(self, path, ty, offset, owner)


# --------------------------------------------------------------------------
# vector construction
# --------------------------------------------------------------------------


def decode_account(idl: Idl, source: str, data: bytes, account: str) -> dict:
    expected = bytes(
        next(a["discriminator"] for a in idl.accounts if a["name"] == account)
    )
    if data[:8] != expected:
        raise DecodeError(
            "%s: discriminator prefix %s does not match IDL %s %s"
            % (source, list(data[:8]), account, list(expected))
        )

    struct_start = 8
    struct_len = struct_size(idl, account)
    consumed = struct_start + struct_len
    if consumed > len(data):
        raise DecodeError(
            "%s: declared struct for %s needs %d bytes but the file is only %d"
            % (source, account, consumed, len(data))
        )

    fields = RawDecoder(idl, data).fields(account, struct_start)

    # Every emitted field must sit inside the declared struct body.
    body_end = struct_start + struct_len
    for path, entry in fields.items():
        if entry["offset"] < struct_start or entry["offset"] + entry["bytes"] > body_end:
            raise DecodeError(
                "%s: field %s [%d,%d) escapes the %s struct body [%d,%d)"
                % (source, path, entry["offset"], entry["offset"] + entry["bytes"],
                   account, struct_start, body_end)
            )

    trailing = data[consumed:]
    return {
        "source": source,
        "account": account,
        "discriminator": list(data[:8]),
        "byte_length": len(data),
        "struct_size": struct_len,
        "fields": fields,
        "trailing": {
            "offset": consumed,
            "length": len(trailing),
            "sha256": hashlib.sha256(trailing).hexdigest(),
        },
    }


def rel_source(pool_dir: str, name: str) -> str:
    return "package-referense/dlmm-sdk/commons/tests/fixtures/%s/%s" % (pool_dir, name)


def collect(idl: Idl):
    vectors = {}
    pools = sorted(
        d for d in os.listdir(FIXTURE_ROOT) if os.path.isdir(os.path.join(FIXTURE_ROOT, d))
    )
    for pool_dir in pools:
        for name in FIXTURE_FILES:
            path = os.path.join(FIXTURE_ROOT, pool_dir, name)
            if not os.path.exists(path):
                raise DecodeError("expected fixture missing: %s" % path)
            with open(path, "rb") as handle:
                data = handle.read()
            account = idl.disc_to_account.get(data[:8])
            if account is None:
                raise DecodeError(
                    "%s: discriminator %s belongs to no DLMM account"
                    % (path, list(data[:8]))
                )
            vectors.setdefault(account, []).append(
                decode_account(idl, rel_source(pool_dir, name), data, account)
            )
    return vectors, pools


def collect_spl(idl: Idl, pools):
    """Negative control: the SPL Token dumps must NOT carry a DLMM discriminator."""
    results = []
    for pool_dir in pools:
        for name in SPL_FIXTURE_FILES:
            path = os.path.join(FIXTURE_ROOT, pool_dir, name)
            if not os.path.exists(path):
                continue
            with open(path, "rb") as handle:
                data = handle.read()
            prefix = data[:8]
            matched = idl.disc_to_account.get(prefix)
            if matched is not None:
                raise DecodeError(
                    "%s: expected a non-DLMM account but prefix %s is %s"
                    % (path, list(prefix), matched)
                )
            results.append(
                {
                    "source": rel_source(pool_dir, name),
                    "program": "spl-token",
                    "discriminator": list(prefix),
                    "byte_length": len(data),
                    "dlmm_discriminator_match": False,
                    "sha256": hashlib.sha256(data).hexdigest(),
                }
            )
    return results


# --------------------------------------------------------------------------
# self checks
# --------------------------------------------------------------------------


def self_check(idl: Idl, vectors, pools) -> list:
    lines = []

    for account, expected in sorted(SIZE_ANCHORS.items()):
        if account not in vectors:
            raise DecodeError("size anchor %s has no fixture to check against" % account)
        got = vectors[account][0]
        struct = got["struct_size"]
        if struct != expected["struct"]:
            raise DecodeError(
                "size anchor: IDL struct for %s computes %d, anchor says %d"
                % (account, struct, expected["struct"])
            )
        if got["trailing"]["offset"] != expected["account"]:
            raise DecodeError(
                "size anchor: %s declared fields end at %d, anchor says %d"
                % (account, got["trailing"]["offset"], expected["account"])
            )
        if got["byte_length"] != expected["fixture"]:
            raise DecodeError(
                "size anchor: fixture for %s is %d bytes, anchor says %d"
                % (account, got["byte_length"], expected["fixture"])
            )
        lines.append(
            "  anchor %-9s struct=%-6d declared-end=%-6d dump=%-6d OK"
            % (account, struct, got["trailing"]["offset"], got["byte_length"])
        )

    # Oracle is the one account with an appended observation buffer.
    oracle = vectors["Oracle"][0]
    if oracle["trailing"]["offset"] != 32 or oracle["trailing"]["length"] == 0:
        raise DecodeError("Oracle trailing section not modelled as expected")
    lines.append(
        "  oracle    declared fields end at 32, dump=3232, trailing=%d bytes OK"
        % oracle["trailing"]["length"]
    )

    for account, entries in sorted(vectors.items()):
        declared = bytes(
            next(a["discriminator"] for a in idl.accounts if a["name"] == account)
        )
        for entry in entries:
            if bytes(entry["discriminator"]) != entry_bytes(entry):
                raise DecodeError("internal: discriminator rewritten for %s" % account)
            if entry["byte_length"] != os.path.getsize(
                os.path.join(REPO_ROOT, entry["source"])
            ):
                raise DecodeError("byte_length drifted for %s" % entry["source"])
            end = max(e["offset"] + e["bytes"] for e in entry["fields"].values())
            if end > entry["byte_length"]:
                raise DecodeError("field overrun in %s" % entry["source"])
            if end > entry["trailing"]["offset"]:
                raise DecodeError("field reached into the trailing section of %s" % entry["source"])
            # Strongest layout invariant: the emitted spans must tile the declared
            # struct body exactly once, with no gap, no overlap and no aliasing.
            spans = sorted(
                (e["offset"], e["offset"] + e["bytes"]) for e in entry["fields"].values()
            )
            cursor = entry["trailing"]["offset"] - entry["struct_size"]
            for span_start, span_end in spans:
                if span_start != cursor:
                    raise DecodeError(
                        "%s: field span [%d,%d) does not continue at %d"
                        % (entry["source"], span_start, span_end, cursor)
                    )
                cursor = span_end
            if cursor != entry["trailing"]["offset"]:
                raise DecodeError(
                    "%s: field spans end at %d, struct ends at %d"
                    % (entry["source"], cursor, entry["trailing"]["offset"])
                )
            if entry["discriminator"] != list(declared):
                raise DecodeError("%s: discriminator mismatch" % entry["source"])
    lines.append(
        "  per-field containment, exact tiling, discriminator prefix: %d vectors OK"
        % sum(len(v) for v in vectors.values())
    )

    # Cross-check: BinArray.lb_pair must equal the base58 fixture directory name,
    # which the upstream fixture layout defines as the pool address.
    for entry in vectors["BinArray"]:
        pool = entry["source"].split("/")[-2]
        want = b58decode(pool)
        got = entry["fields"]["lb_pair"]["value"]
        if b58decode(got) != want:
            raise DecodeError(
                "%s: bin_array.lb_pair %s does not decode to fixture pool %s"
                % (entry["source"], got, pool)
            )
    lines.append("  BinArray.lb_pair == base58(fixture dir) for %d vectors OK"
                 % len(vectors["BinArray"]))

    # Cross-check: LbPair.token_x_mint must equal the SPL reserve_x mint field.
    for entry in vectors["LbPair"]:
        pool = entry["source"].split("/")[-2]
        path = os.path.join(FIXTURE_ROOT, pool, "reserve_x.bin")
        with open(path, "rb") as handle:
            reserve = handle.read()
        if entry["fields"]["token_x_mint"]["value"] != b58encode(reserve[0:32]):
            raise DecodeError(
                "%s: token_x_mint disagrees with reserve_x.mint" % entry["source"]
            )
    lines.append("  LbPair.token_x_mint == reserve_x.mint for %d vectors OK"
                 % len(vectors["LbPair"]))

    # Negative control.
    spl = collect_spl(idl, pools)
    expected_spl = sum(
        1
        for pool_dir in pools
        for name in SPL_FIXTURE_FILES
        if os.path.exists(os.path.join(FIXTURE_ROOT, pool_dir, name))
    )
    if len(spl) != expected_spl:
        raise DecodeError(
            "negative control read %d SPL fixtures, expected %d" % (len(spl), expected_spl)
        )
    lines.append("  negative control: %d SPL fixtures, none match a DLMM discriminator OK"
                 % len(spl))

    return lines, spl


def entry_bytes(entry: dict) -> bytes:
    return bytes(entry["discriminator"])


# --------------------------------------------------------------------------
# output
# --------------------------------------------------------------------------


def build_layout_index(idl: Idl, vectors) -> dict:
    accounts = []
    for account in idl.accounts:
        name = account["name"]
        struct = struct_size(idl, name)
        accounts.append(
            {
                "account": name,
                "discriminator": list(account["discriminator"]),
                "struct_size": struct,
                "account_size": struct + 8,
                "has_fixture": name in vectors,
                "fixture_count": len(vectors.get(name, [])),
            }
        )
    accounts.sort(key=lambda a: a["account"])
    return {
        "idl": "idls/dlmm.json",
        "idl_address": idl_accounts_address(),
        "encoding": "borsh packed, little-endian, fields in declaration order, "
        "8-byte anchor discriminator prefix",
        "accounts": accounts,
    }


def idl_accounts_address() -> str:
    with open(IDL_PATH, "r", encoding="utf-8") as handle:
        return json.load(handle)["address"]


def render(account: str, entries, layout_index) -> dict:
    return {
        "account": account,
        "discriminator": entries[0]["discriminator"],
        "vector_count": len(entries),
        "layout": next(a for a in layout_index["accounts"] if a["account"] == account),
        "vectors": entries,
    }


def write_or_check(name: str, payload: dict, check_only: bool) -> bool:
    path = os.path.join(OUT_DIR, name)
    text = json.dumps(payload, indent=2, sort_keys=False, ensure_ascii=False) + "\n"
    if check_only:
        if not os.path.exists(path):
            print("MISSING %s" % name)
            return False
        with open(path, "r", encoding="utf-8") as handle:
            if handle.read() != text:
                print("DRIFT   %s" % name)
                return False
        print("OK      %s" % name)
        return True
    os.makedirs(OUT_DIR, exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(text)
    print("wrote   %s (%d bytes)" % (name, len(text)))
    return True


def main(argv) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true",
                        help="verify committed vectors instead of writing them")
    args = parser.parse_args(argv)

    idl = Idl(IDL_PATH)
    vectors, pools = collect(idl)
    lines, spl = self_check(idl, vectors, pools)

    print("size anchors:")
    for line in lines:
        print(line)

    layout_index = build_layout_index(idl, vectors)
    ok = True
    for account in sorted(vectors):
        ok &= write_or_check("%s.json" % account,
                             render(account, vectors[account], layout_index),
                             args.check)
    ok &= write_or_check("_negative_control_spl.json",
                         {"program": "spl-token",
                          "note": "these fixtures are NOT DLMM accounts; a DLMM "
                                  "decoder must reject them",
                          "fixtures": spl},
                         args.check)
    ok &= write_or_check("_layout_index.json", layout_index, args.check)

    print()
    for account in sorted(vectors):
        entries = vectors[account]
        print("%-9s vectors=%-2d fields/vector=%-5d file=%d bytes"
              % (account, len(entries), len(entries[0]["fields"]),
                 os.path.getsize(os.path.join(OUT_DIR, "%s.json" % account))
                 if os.path.exists(os.path.join(OUT_DIR, "%s.json" % account)) else 0))
    if not ok:
        print("\nFAILED: vectors are out of date or missing", file=sys.stderr)
        return 1
    print("\nall self checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
