# Implementation decisions

Decisions made while writing the code, as opposed to the design-time records in
`DESIGN.md` §4. Each one records the evidence that drove it and the trigger that
should reopen it.

Design records (D1–D7) live in `DESIGN.md`. Feature research lives in
`FEATURES.md`. Architecture lives in `ARCHITECTURE.md`.

---

## I1 — `bin.Uint128` cannot be the arithmetic type

**Decision.** The SDK uses its own `num.U128` for values, not
`github.com/gagliardetto/binary.Uint128`.

**Evidence.** `bin.Uint128` is `{Lo, Hi uint64; Endianness binary.ByteOrder}`:

- It has **no arithmetic methods at all** — only `BigInt`, `Bytes`, `String`, and
  marshal/unmarshal. It is a serialization carrier, not a number.
- The `Endianness` field makes `==` compare byte order as well as value.
- `ReadUint128` never assigns that field, so a decoded value has
  `Endianness == nil` while a constructed one may not — two "equal" numbers can
  compare unequal.
- The big-endian decode path carries an unresolved `// TODO: is this correct?`.

**Consequence.** `num.U128` is a plain `{Lo, Hi uint64}`: comparable, usable as a
map key, with a useful zero value. `bin.Uint128` appears only at the codec
boundary. This invalidated the original `DESIGN.md` §3.4 plan to expose
`bin.Uint128` in the public API.

**Reopen if.** Upstream adds arithmetic and removes the `Endianness` field.

---

## I2 — 256/128 division uses `math/big` rather than Knuth Algorithm D

**Decision.** `internal/uint256.Div256By128` and `DivCeil256By128` delegate to
`math/big`. The hot path, `math.MulShr`, needs no division at all: dividing by
`2^offset` is a shift plus a check of whether any bit was discarded.

**Evidence.** Knuth Algorithm D is roughly a hundred lines of carry and borrow
manipulation. It could not be executed at the time it would have been written, so
a subtle error in a rare branch would have shipped looking authoritative.
`math/big` is correct by construction.

**Consequence.** Allocation on every multiply-divide. The API is deliberately
stable so a hand-written kernel can replace the body without touching callers.
The ban on `math/big` inside `internal/uint256` was relaxed for this one file, and
`math/big` remains the test oracle everywhere else, which keeps the oracle
genuinely independent.

**Reopen if.** `make bench` shows division dominating a realistic quote, or
validation against the golden vectors in `testdata/vectors/` passes and the
allocation shows up in a profile. Both are measurement triggers, not opinions.

---

## I3 — The layering check is fail-closed, not fail-open

**Decision.** `make check-layering` enumerates every package and fails when a
package that is not on an explicit `RPC_ALLOWED` list imports
`solana-go/rpc`.

**Evidence.** The first version enumerated the *pure* packages instead. It
reported `ok` while checking nothing, because `go list` returns no output at all
when any requested package has no Go files — and several directories were still
empty. A guard that silently passes is worse than no guard.

**Consequence.** A new package that imports rpc fails the check until it is
declared. `make check-layering-selftest` proves the detector fires on a real
violation, so a future false pass is itself detectable.

---

## I4 — Tools are pinned with `tool` directives; `go 1.26` and a large go.sum are accepted

**Decision.** `golangci-lint`, `govulncheck`, `gofumpt` and `benchstat` are
declared in `go.mod`'s `tool` block and run as `go tool <name>`. The module's
`go` directive was raised to `1.26.0` as a consequence.

**Evidence.**

| | before | + 3 small tools | + golangci-lint |
|---|---|---|---|
| `go.mod` | 30 lines | 44 | **241** |
| `go.sum` | 115 lines | 137 | **998** |
| `go` directive | 1.25.0 | 1.26.0 | 1.26.0 |

- Every current `golang.org/x/*` release requires `go 1.26.0`, so the bump is
  unavoidable when pinning `@latest`.
- `go get` also forced `google.golang.org/protobuf v1.36.11 → v1.36.12`, which is
  real MVS pressure on consumers.
- Isolation was verified: **zero** tool dependencies appear in the library's
  package graph, so `go build` of the library is unaffected.
- The `tool` directive is what fixes the original failure, where a
  PATH-installed golangci-lint built with go1.26 could not type-check a go1.27
  toolchain's stdlib. Under `go tool`, the tool builds with the project's own
  toolchain.

**Consequence.** Consumers of this module need Go 1.26+. That was accepted while
the module has zero consumers, on the principle that reproducible tooling is
worth more than dependency hygiene when nobody depends on us yet.

**Reopen before the first published tag.** The alternative is a separate
`tools/` module, which keeps the library at `go 1.25` and confines golangci-lint's
tree to `tools/go.sum`. It was not chosen now because cross-module tool
invocation adds friction that could not be tested at the time.

---

## I5 — `pda` takes the program ID explicitly, with no default

**Decision.** Every derivation in `pda` takes `programID` as its first argument.
There is no package-level "current cluster".

**Evidence.** `ARCHITECTURE.md` §5 states there is no implicit default, and the
TypeScript client has three deployments — `mainnet-beta`, `devnet` and
`localhost` — where mainnet and devnet happen to share one address but localhost
does not. A process may hold state for two clusters at once.

**Consequence.** Slightly more verbose call sites. In exchange, no global and no
way to derive an address for the wrong network by omission. The three IDs live in
`program/lbclmm` as separate constants rather than one collapsed "current" value.

---

## I6 — `ILMBaseKey` is a function, not an exported variable

**Decision.** `pda.ILMBaseKey()` returns a copy; the constant itself is
unexported.

**Evidence.** `ARCHITECTURE.md` P2 forbids global mutable state. An exported
`var` of type `solana.PublicKey` can be reassigned by any caller, silently
changing address derivation for the whole process. `PublicKey` is a fixed-size
array, so returning it copies and mutation is harmless.

**Consequence.** Program IDs remain exported vars, matching the convention of
`solana-go`'s own `TokenProgramID`. The inconsistency is deliberate: program IDs
are meant to be compared against, whereas `ILMBaseKey` is a fixed seed
component.

---

## I7 — Test volume is bounded, and `-race` is not the default

**Decision.** Randomized test loops default to 200 iterations, lowered to 50
under `-short` and raised with `DLMM_TEST_SCALE=n`. `make test` does not use
`-race`; `make test-race` does.

**Evidence.** The first version ran roughly 20,000 iterations of `math/big`
comparisons, several suites in parallel. It coincided with the development
machine becoming unresponsive. A carry or limb-order bug is caught within a few
hundred inputs; a bug that only appears at input 4,000 is not worth a machine.

**Consequence.** An earlier claim that all suites passed is weaker than it
sounded: it rested on a run that may have been the cause of the instability.
`go vet ./...` was later measured at 4,949 MB → 4,720 MB RSS on a 15.6 GB
machine, so type-checking is not the expensive part.

---

## I8 — `gofumpt` lost its `-extra` flag

**Decision.** `make fmt` runs `go tool gofumpt -l -w .`.

**Evidence.** gofumpt v0.12.0's flags are `-l -w -d -e -lang -modpath -r -s`.
`-extra` is gone; the extra rules moved to per-rule settings, which is the same
change that deprecated `extra-rules` in `.golangci.yml`.

**Consequence.** The previous `make fmt` would have failed with an unknown flag.
Found by reading the pinned version's source rather than by running it.

---

## I9 — The IDL is vendored and checked mechanically

**Decision.** `idls/dlmm.json` is committed with provenance in
`idls/PROVENANCE.md`, and `tools/check_idl_constants.py` verifies both the
constants and the `pda` seed literals against it.

**Evidence.** Constants are the most dangerous kind of transcription: a typo
produces a plausible number that compiles and runs, and diverges only in a rare
branch. A mistyped seed produces a valid public key with no account behind it.

**Consequence.** 36 values are checked. `make check-idl-selftest` mutates a
constant and asserts the check fails, so a false pass is detectable. Every value
in the IDL is a string, and seed constants are stringified byte arrays, so the
parser distinguishes numeric strings from byte arrays rather than coercing
blindly.

---

## I10 — Golden PDA values are a recorded gap, not a passing test

**Decision.** `pda.TestGoldenAddresses` is skipped with an explicit reason
rather than omitted.

**Evidence.** A PDA is the output of an ed25519 curve check. Property tests prove
internal consistency — determinism, mint-order symmetry, off-curve output, no
collisions — but they cannot prove the seed set itself is right, because
comparing the implementation against itself is not an oracle.

**Consequence.** The gap is visible in test output instead of silent. Closing it
needs an external source: the TypeScript SDK's `deriveBinArray`, or an address
read from RPC for a known pool.

---

## I11 — Formatting is scoped to our packages, and reference checkouts are guarded

**Decision.** `make fmt` and `make fmt-check` operate on the directories returned
by `go list -f '{{.Dir}}' ./...`, not on `.`. A new `make check-referense-pristine`
fails when any `package-referense/*/` checkout has uncommitted changes.

**Evidence.** The first version ran `go tool gofumpt -w .`, which walks the
filesystem. It reformatted **139 files inside the vendored `solana-go`
checkout** before anyone noticed. Those are upstream repositories cloned for
reference, not our code; rewriting them corrupts the evidence the port is being
compared against, and would have silently invalidated any later comparison
against upstream.

The separate module is what hides the problem: `go list ./...` excludes
`package-referense/*` because each has its own `go.mod`, so the module graph was
already correct while the filesystem walk was not.

**Consequence.** All three checkouts were restored with `git checkout -- .` and
verified pristine. The guard was self-tested by appending a line to an upstream
`README.md` and confirming the check fails, then restoring.

**Reopen if.** The reference checkouts are removed in favour of module
requirements, at which point the guard becomes unnecessary.

---

## I12 — Seven defects, and the one that hid behind a green test

**Context.** The numeric core was written, hand-traced, and reviewed before any
test was executed. Two independent verification lanes then ran: an adversarial
read-only review of the source, and a Python-generated golden-vector oracle
written without sight of the implementation.

**What the review found (5 defects, all real and confirmed against primary sources):**

| # | Defect | Evidence |
|---|---|---|
| 1 | `bits.Mul64` returns `(hi, lo)`, upper half first; four call sites assigned the halves the other way, so `Mul128To256` was wrong for essentially every input | `math/bits/bits.go:470` — "upper half returned in hi" |
| 2 | The carry out of the cross-term sum has weight `2^192` (limb 3), not `2^128`; it was added one limb too low, breaking exactly the full-width products the function exists for | hand trace of `(2^128-1)^2` |
| 3 | `bits.Div64` returns `(quo, rem)`; `U256.String()` treated the first result as a carry, so it never terminated for any value below `10^19`, and `ToU128` formats the value into its overflow error, so every u128-overflow path hung instead of returning | `math/bits/bits.go:518` |
| 4 | `HasBitsBelow` had no partial-limb case for limb 3, so shifts in `193..255` never inspected the top limb | unreachable through the current API, live if `MaxShiftOffset` is ever raised |
| 5 | Bin IDs were bounded by `MaxBinIDPerBinStep` (351639), which measures a bin range per bin step, not the addressable bin ID space (443636) | `constants.rs:18-19` and `lb_pair.rs:216` vs the IDL doc string |

**What the review missed, and the golden vectors caught (2 further defects):**

| # | Defect | Effect |
|---|---|---|
| 6 | `bytesBE128` wrote `Lo` into the high half, so every divisor was `2^64` times too large | turned correct divisions into zero: `MulDiv(42, 6, 7)` returned `0` |
| 7 | `fromBigInt` assigned the first eight big-endian bytes to `Lo` | shifted every converted quotient by `2^64` |

**Why defect 7 was invisible to both review and inspection:** the same
`b[7-i] = byte(v.Lo >> …)` pattern had been written into the test oracles too —
`oracleU128` in `internal/uint256/uint256_test.go` and in `math/mul_test.go`. The
oracle and the code under test shared one misconception, so they agreed on the
same wrong answer. A test written by the author, from the author's understanding,
cannot catch that understanding being wrong; it only proves internal consistency.
That is precisely why the Python vectors — a different language, a different
algorithm, `fractions.Fraction` as a second method, and no sight of the Go
source — found it immediately.

**Consequence.** All seven are fixed, and the three manual byte loops are gone in
favour of `encoding/binary`, which does not offer the opportunity to swap halves.
The full suite is green: 6 packages, `go test -count=1 -p 1 ./...` exit 0.
Regression guards were added for the specific cases: `MaxU128*MaxU128` against its
hand-computable value, `1*1` landing in the low limb, `String()` terminating, the
`HasBitsBelow` top-limb window, and the bin ID bound.

**Reopen if.** Never for the arithmetic itself. The durable lesson is the
process: a self-written oracle is not independent verification, and a bug in the
shared assumption is invisible to both sides.

---

## I13 — What now counts as verified

The defects above make the accounting concrete:

| Claim | Status |
|---|---|
| The module type-checks | **Verified** — `go vet ./...` exit 0, and the exit code was shown non-zero on deliberately broken input |
| The arithmetic is correct | **Verified against an independent oracle** — 4,747 golden vectors computed in Python with exact integers, cross-checked by a second method using `fractions.Fraction`, 0 mismatches |
| The Go suite passes | **Verified** — 6 packages, exit 0 |
| The tests have teeth | **Partially verified** — 3 mutations of `num` were each caught; the numeric-suite defects surfaced as vector failures |
| A derived pool address matches the chain | **Not verified** — `pda.TestGoldenAddresses` is still skipped; property tests prove only internal consistency |
| Anything talks to a real cluster | **Not started** — no client code exists yet |

The second row is the one that changed. Before the vectors existed, "the
arithmetic is correct" rested on hand-tracing, which is what missed defects 1, 2,
6 and 7.

---

## Status of the "nothing has been executed" concern

Resolved. `go vet ./...` passes with a verified exit code, so all six packages
type-check, and the Go test suite has now been executed: six packages pass under
`go test -count=1 -p 1 ./...`. See I12 for the seven defects that only surfaced
once it ran.
