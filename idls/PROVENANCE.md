# IDL provenance

| Field | Value |
|---|---|
| Source | https://github.com/MeteoraAg/dlmm-sdk |
| Path | `idls/dlmm.json` |
| Commit | `576919e3e4368e542c402f000b4264724f7f23ec` |
| Retrieved | 2026-09-29 |
| IDL version | `0.12.0` |

Vendored rather than referenced: the IDL is a build input for
`program/lbclmm`, and regenerating bindings must not require a second
checkout.

`program/lbclmm/constants.go` is checked against this file by
`tools/check_idl_constants.py`. When the upstream IDL changes, re-vendor it,
regenerate, and review the diff.

## `dlmm-commons-constants.rs`

| Field | Value |
|---|---|
| Source | https://github.com/MeteoraAg/dlmm-sdk |
| Path | `commons/src/constants.rs` |
| Commit | `576919e3e4368e542c402f000b4264724f7f23ec` |
| Retrieved | 2026-09-29 |

Vendored because it is the only authority for two constants that `program/lbclmm`
needs but the IDL does not carry: `MIN_BIN_ID` and `MAX_BIN_ID` (both ±443636).
The IDL's `MAX_BIN_ID_PER_BIN_STEP` (351639) is a different quantity, documented
as "used for bin id bound estimation", and using it as the addressing bound
refuses legitimate bin IDs.

`tools/check_idl_constants.py` verifies the Go values against this file. The
reference checkout itself (`package-referense/`) is gitignored and 37 MB, so a
fresh clone could not run the check without this copy.
