# gen_golden — DLMM golden account vectors

`gen_account_vectors.py` is an **independent** Python decoder for Meteora DLMM
on-chain accounts. It was written from `idls/dlmm.json` alone. It does not
import, read, or mirror any Go code — that is deliberate, because the Go decoder
under test must be an independent implementation too. If both sides are derived
from the same mistake, the vectors would agree with the bug instead of catching
it.

Python standard library only. No network, no Go toolchain, no `pip`.

## Regenerate

```sh
python3 tools/gen_golden/gen_account_vectors.py           # write testdata/decoded/*.json
python3 tools/gen_golden/gen_account_vectors.py --check   # verify committed files, write nothing
```

`--check` exits non-zero if any committed vector drifts. Wire it into CI next to
the Go decoder test: the Go side should load these files and assert its own
decoded offsets/values match.

Inputs (read-only, never modified):

| Path | Role |
| --- | --- |
| `idls/dlmm.json` | authoritative layout: 12 accounts, 86 types |
| `package-referense/dlmm-sdk/commons/tests/fixtures/<POOL>/*.bin` | 22 real account dumps, 3 pools |

## Outputs

| File | Contents |
| --- | --- |
| `testdata/decoded/LbPair.json` | 3 vectors, one per pool |
| `testdata/decoded/BinArray.json` | 6 vectors (`bin_array_1`, `bin_array_2` x 3 pools) |
| `testdata/decoded/Oracle.json` | 3 vectors |
| `testdata/decoded/_layout_index.json` | discriminator + computed struct size for **all 12** IDL accounts |
| `testdata/decoded/_negative_control_spl.json` | the 10 SPL Token dumps, which must **not** decode as DLMM |

## Encoding rules used

Every offset is measured from the start of the whole account data, so the first
declared field begins at **8**, right after the 8-byte Anchor discriminator.

* Borsh, little-endian, **packed**: no implicit alignment padding anywhere. This
  is the single most important fact about the DLMM account layout — a decoder
  that inserts `repr(C)` alignment before `active_id` will land 2 bytes off and
  then never recover.
* Fields in declaration order, nested structs inline.
* A fixed array `[T; N]` is N consecutive inline encodings. `[u8; N]` is a byte
  blob.
* `vec<T>` and `Option<T>` do not occur in any of the 12 account structs. The
  decoder raises on them rather than guessing, because a dynamic field would
  make every later offset conditional.

## Vector schema

```json
{
  "source": "package-referense/dlmm-sdk/commons/tests/fixtures/9t3Ey.../lb_pair.bin",
  "account": "LbPair",
  "discriminator": [33, 11, 49, 98, 181, 101, 177, 13],
  "byte_length": 904,
  "struct_size": 896,
  "fields": {
    "parameters.base_factor": {"offset": 8, "bytes": 2, "value": 10000},
    "active_id":               {"offset": 76, "bytes": 4, "value": 0},
    "token_x_mint":            {"offset": 88, "bytes": 32, "value": "<base58>"}
  },
  "trailing": {"offset": 904, "length": 0, "sha256": "<hex of the empty buffer>"}
}
```

Field paths are dotted, with `[i]` for array elements:
`parameters.base_factor`, `reward_infos[0].reward_rate`, `bins[42].amount_x`.

Value encoding by IDL type:

| IDL type | `value` JSON type |
| --- | --- |
| `u8`/`u16`/`u32`/`u64`/`i8`/`i16`/`i32`/`i64` | number (decimal) |
| `u128`/`i128` | string (decimal, so no float64 precision loss) |
| `pubkey` | string (base58, Bitcoin alphabet) |
| `bool` | true/false |
| `[u8; N]` | string (lowercase hex) |
| enum (`defined` non-struct) | object `{"index": <u8>, "name": "<Variant>"}` |

Two **additive** keys exist; a consumer may ignore them and still pass:

* `lo` / `hi` on every `u128` field — the low and high `uint64` limbs, which is
  exactly how `num.U128` (`Lo`, `Hi`, low first) stores the value.
* `u16le` on any 2-byte `[u8; 2]` field (`bin_step_seed`, `base_factor_seed`) —
  the little-endian `u16` reading, because those two fields are byte blobs in the
  IDL but their real meaning is a number.

## Self checks (all fatal, exit code 1)

1. Every field span lies inside the declared struct body, and the declared
   struct never exceeds the file length.
2. **Exact tiling**: sorted field spans tile `[8, 8+struct_size)` with no gap,
   no overlap, and no aliasing. This is the strongest layout assertion in the
   set — an off-by-one anywhere shows up as a tiling failure, not as a plausible
   wrong number.
3. The reported `discriminator` equals the 8-byte file prefix, and equals the IDL
   value for the account being decoded.
4. Size anchors: LbPair struct 896 / dump 904, BinArray 10128 / 10136, Oracle 24
   / 32 declared, 3232 on disk.
5. Cross-checks against ground truth outside the DLMM account itself:
   `BinArray.lb_pair` base58-decodes to the fixture directory name (which is the
   pool address), and `LbPair.token_x_mint` equals the SPL `reserve_x.mint`
   field.
6. Negative control: none of the 10 SPL fixtures carry a DLMM discriminator.

## Field notes

### LbPair (904 bytes)

| Offset | Width | Field |
| --- | --- | --- |
| 8 | 32 | `parameters` (`StaticParameters`) |
| 40 | 32 | `v_parameters` (`VariableParameters`) |
| 72 | 1 | `bump_seed` `[u8;1]` |
| 73 | 2 | `bin_step_seed` `[u8;2]` — LE `u16`, the bin step used at create |
| 75 | 1 | `pair_type` — `PairStatus`-shaped enum: 0 Permissionless, 1 Permission, 2 CustomizablePermissionless, 3 PermissionlessV2 |
| 76 | 4 | `active_id` (i32) |
| 80 | 2 | `bin_step` (u16) |
| 82 | 1 | `status` — 0 Enabled, 1 Disabled |
| 83 | 1 | `require_base_factor_seed` |
| 84 | 2 | `base_factor_seed` `[u8;2]` — LE `u16` |
| 86 | 1 | `activation_type` — 0 Slot, 1 Timestamp |
| 87 | 1 | `creator_pool_on_off_control` |
| 88 | 32 | `token_x_mint` |
| 120 | 32 | `token_y_mint` |
| 152 | 32 | `reserve_x` (LB vault) |
| 184 | 32 | `reserve_y` |
| 216 | 16 | `protocol_fee` (`amount_x`, `amount_y`) |
| 232 | 32 | `_padding_1` |
| 264 | 288 | `reward_infos[2]`, each `RewardInfo` is 144 bytes |
| 552 | 32 | `oracle` |
| 584 | 128 | `bin_array_bitmap[16]` |
| 712 | 8 | `last_updated_at` |
| 720 | 32 | `_padding_2` |
| 752 | 32 | `pre_activation_swap_address` |
| 784 | 32 | `base_key` |
| 816 | 8 | `activation_point` |
| 824 | 8 | `pre_activation_duration` |
| 832 | 8 | `_padding_3` |
| 840 | 8 | `_padding_4` |
| 848 | 32 | `creator` |
| 880 | 1 | `token_mint_x_program_flag` |
| 881 | 1 | `token_mint_y_program_flag` |
| 882 | 1 | `version` |
| 883 | 21 | `_reserved` |

`RewardInfo` is **144** bytes, not 152: 3 pubkeys (96) + `reward_duration` (8) +
`reward_duration_end` (8) + `reward_rate` u128 (16) + `last_update_time` (8) +
`cumulative_seconds_with_empty_liquidity_reward` (8). Getting this wrong puts
`oracle` 16 bytes late and blows the 904-byte total.

### BinArray (10136 bytes)

`index` i64 @8, `version` u8 @16, `_padding_1` `[u8;7]` @17, `lb_pair` pubkey
@24, `bins[70]` @56. `Bin` is 144 bytes, so `70 * 144 = 10080` and
`56 + 10080 = 10136`.

Bin array `index` is a bin-id bucket: bin array `i` owns bins
`[i*70, i*70+69]` (floor division, so `-1` owns `-70..-1`).

### Oracle (3232 bytes on disk)

Only 24 bytes are declared: `idx` u64 @8, `active_size` u64 @16, `length` u64
@24. Everything from offset 32 on is the appended observation buffer that
`increase_oracle_length` grows; the decoder captures it as `trailing` with a
length and a sha256 rather than pretending the IDL describes it. On all three
fixtures the trailing region is exactly 3200 bytes.

## Accounts with no fixture

`BinArrayBitmapExtension`, `ClaimFeeOperator`, `DummyZcAccount`, `LimitOrder`,
`Operator`, `PositionV2`, `PresetParameter`, `PresetParameter2`, `TokenBadge` have
no dump in the fixture set, so no byte-level vector can be produced for them.
Their discriminators and **computed struct sizes** are still in
`_layout_index.json`; the Go decoder can assert its own struct size against those
numbers without a fixture.
