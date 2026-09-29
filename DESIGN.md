# Meteora DLMM — Go SDK: System Design

Status: **proposal** — menunggu keputusan pada `OPEN_QUESTIONS`
Tanggal: 2026-09-29
Referensi ter-index (codebase-memory-mcp): `meteora-dlmm-sdk` @`576919e`, `meteora-zap-sdk` @`9198753`, `solana-go` @`5cf5b46`

---

## 1. DESIGN_BRIEF

### Tujuan & outcome yang terukur

Outcome, bukan aktivitas:

| # | Outcome | Bukti pembuktian |
|---|---------|-----------------|
| O1 | Decode 12 tipe akun DLMM benar terhadap data on-chain nyata | 22 fixture `.bin` dari `commons/tests/fixtures/` (3 LbPair + bin_array/oracle/reserve/mint) → golden test, identik dengan output Rust/TS |
| O2 | Matematika swap/quote identik dengan program on-chain | Cross-check `math` vs `math/big` pada 10⁶ kasus + expected value dari `commons/src/quote.rs` test (`test_swap_quote_exact_in/out`) |
| O3 | Instruction yang dihasilkan diterima program (bukan ditolaknya signature/discriminator) | SimulateTransaction di devnet; byte-level compare dengan instruksi TS untuk skenario yang sama |
| O4 | Alur quote → swap selesai tanpa RPC hardcoded di logic | `dlmm.Pool` sebagai value object; package `math`/`bin`/`strategy` bisa diuji tanpa RPC sama sekali |

### Scope

**In scope (P0–P4):** port penuh DLMM: types, codecs, math, PDA, client read, liquidity/swap write, strategies, rebalance, limit order, oracle.
**Out of scope:** program on-chain (Rust), CLI production, DAMM v2, Zap (P5, terpisah), bonding curve, merkle/fee router (`zap-box`), Go rewrite of `solana-go` internals.

### Non-goals

- Bukan reimplementasi `solana-go`. Número satu dependensi runtime.
- Bukan general-purpose AMM framework. Binds ke DLMM saja.
- Tidak mengorbankan Go idiom demi parity API 1:1 dengan TS.

### Batasan & reality check

| Fakta | Nilai | Sumber |
|---|---|---|
| Instruction | 76 | `idls/dlmm.json` |
| Account | 12 (semua punya discriminator 8-byte) | IDL |
| Type | 86 (78 struct + 8 enum) | IDL |
| Error on-chain | 111 (kode 6000+) | IDL |
| Event | 29 | IDL |
| Metode client TS | 97, di `index.ts` baris 240–9636 (~9.400 baris) | graph |
| Otoritas math | `commons/src/math` + `quote.rs` (~1.380 LOC Rust) | graph |
| Golden fixture | 22 file `.bin` | `commons/tests/fixtures/` |
| Toolchain | go1.27.1, GOPROXY hidup | observasi lokal |

---

## 2. ARCHITECTURE_VIEWS

### 2.1 Context

```
                 ┌─────────────────────────────────────────┐
   pemanggil ───▶│            dlmm (public)                │
   (bot/trader)  │   Client · Pool · Option · Err          │
                 └───────────────┬─────────────────────────┘
                                 │
        ┌────────────────────────┼─────────────────────────┐
        ▼                        ▼                         ▼
 ┌──────────────┐      ┌─────────────────┐       ┌──────────────────┐
 │ math / bin / │      │ program/lbclmm  │       │ zap (P5)         │
 │ pda /strategy│      │ codec, pure     │       │ client + tx      │
 │ ZERO RPC     │      │ ZERO RPC        │       └──────────────────┘
 └──────────────┘      └────────┬────────┘
                                │ encode/decode Borsh
                                ▼
                     ┌────────────────────┐
                     │ gagliardetto/      │
                     │ solana-go v1.24.0  │  rpc · types · tx · keys
                     └────────┬───────────┘
                              ▼
                     ┌────────────────────┐
                     │ Solana RPC         │
                     └────────────────────┘

   ┌──────────────────────────────────────────────┐
   │ tools/idlgen  →  meng-generate program/lbclmm│  build-time only
   └──────────────────────────────────────────────┘
```

Batas kepercayaan: `math`/`bin`/`pda`/`program` tidak pernah menyentuh jaringan. Hanya `dlmm.Client` yang boleh. Ini yang bikin O4 bisa dibuktikan.

### 2.2 Functional — package map

| Package | Tanggung jawab |RNPC? | Sumber kebenaran |
|---|---|---|---|
| `program/lbclmm` | 12 struct akun + codec, 76 builder instruksi, 111 error, 29 event. **Generated.** | ❌ | `idls/dlmm.json` |
| `math` | `MulDiv`, `MulShr`, `ShlDiv`, `GetPriceFromQIndex`, `GetQIndexFromPrice`, fee | ❌ | `commons/src/math/*` |
| `bin` | `SwapExactInQuoteAtBin`, `SwapExactOutQuoteAtBin`, `GetAmountIn/Out`, `GetBinMaxAmountOut` | ❌ | `commons/src/quote.rs` |
| `binarray` | `BinIDToBinArrayIndex`, `DeriveBinArrayBitmapExtension`, chunking 70-bin | ❌ | `commons/src/extensions/bin_array.rs` |
| `pda` | Semua seed & `FindProgramAddress` | ❌ | `commons/src/pda.rs` |
| `strategy` | weight / curve / spot / bid-ask, `ToAmountIntoBins`, bit-flag | ❌ | `ts-client/src/dlmm/helpers/rebalance/` |
| `oracle` | Ekstrak observation, varians, `IncreaseOracleLength` | ❌ | `commons/src/extensions/` + `helpers/oracle/` |
| `dlmm` | `Client` (I/O) + `Pool` (state) + option constructor | ✅ | client |
| `zap` | Zap in/out (P5) | ✅ | `zap-sdk/src/zap.ts` |
| `tools/idlgen` | Generator IDL→Go. Modul Go terpisah. | — | IDL |
| `internal/uint256` | `bits.Mul64`/`bits.Div64`, 4×uint64. 256-bit intermediate untuk `math`. | ❌ | — |

Generator berada di modul terpisah (`tools/idlgen/go.mod`) supaya dependensinya tidak mengotori `go.mod` SDK.

### 2.3 Runtime — alur kritis

**Baca (quote swap):**
```
client.SwapQuote(ctx, pool, inMint, amount, opts...)
  → rp := bin.GetBinArrayForSwap(...)        // derived, no I/O
  → decode binArray (I/O: 1 getMultipleAccounts)
  → for each bin: bin.SwapExactInQuoteAtBin(...)   // pure, hot loop
  → accumulate → SwapQuote
```

**Tulis (eksekusi swap):**
```
user := client.Swap(ctx, SwapParams{...})
  → [pure] hitung minOut via slippage, susun AccountMeta
  → [pure] ix := lbclmm.NewSwap(accounts, params)   // encode Borsh
  → [pure] ix ATA idempotent, compute budget dari quote
  → [I/O ] Simulate → SetComputeUnitLimit(buffer)
  → return []solana.Instruction   ← TIDAK kirim
```

**Batas penting:** `Swap` mengembalikan `[]solana.Instruction`, **tidak** menandatangani/mengirim. Why: komposisi, batch multi-pair, simulasi caller, dan testability. TS SDK mengirim di dalam — itu yang membuat测试-nya butuh validas. Go: pisahkan. `Client.SendTransaction(ctx, ...)` jadi lapisan eksplisit terpisah.

### 2.4 Data/state

Sumber kebenaran mutable: **on-chain**. Tidak ada cache lokal yang otoritatif. `Pool` adalah *snapshot*, bukan cache — harus di-refetch kalau mau segar.

```
Pool (immutable snapshot, struct)
├── Pubkey solana.PublicKey
├── ProgramID solana.PublicKey
├── LbPair          program/lbclmm.LbPair
├── BinArrayBitmapExtension *BinArrayBitmapExtension
├── TokenX, TokenY  TokenReserve
├── Rewards         []TokenReserve
└── Clock           Clock
```

Aturan: `Pool` tidak punya method yang melakukan I/O.零 mutable state. Dibuat hanya oleh `Client.RefetchPool`.

### 2.5 Evolution

- Codec di-commit ke repo, bukan di-generate saat build (`//go:generate` + `make generate`). Code review bisa melihat diff binding.
- IDL version di-pin di `program/lbclmm/idl_version.go` sebagai konstanta. Program ID per cluster jadi map, bukan `if cluster ==`.
- Zeppy version bump → `make generate && git diff` = review artefak upgrade yang eksplisit.

---

## 3. INTERFACE & DATA CONTRACTS

### 3.1 Constructor (functional options)

```go
// ProgramType: Functional options. Validasi di construction, bukan saat call.
type Option func(*config) error

func WithProgramID(id solana.PublicKey) Option
func WithCommitment(c solana.Commitment) Option
func WithRequestTimeout(d time.Duration) Option
func WithComputeUnitBuffer(f float64) Option   // default 0.1, clamp 0..1
func WithSkipSOLWrapping() Option
func WithLogger(l *slog.Logger) Option

func New(rpc solana.RPC, opts ...Option) (*Client, error)  // accept interface, return struct
```

`New` mengembalikan `error` karena option bisa gagal validasi — menangkap config buruk di construction, bukan saat runtime (design-patterns #2).

### 3.2 I/O boundary

Semua operasi I/O punya bentuk sama:

```go
func (c *Client) RefetchPool(ctx context.Context, pubkey solana.PublicKey) (*Pool, error)
func (c *Client) GetBins(ctx context.Context, p *Pool, minBin, maxBin int) ([]Bin, error)
func (c *Client) SwapQuote(ctx context.Context, p *Pool, q SwapQuoteParams) (*SwapQuote, error)
func (c *Client) Swap(ctx context.Context, q SwapParams) ([]solana.Instruction, error)
```

- `ctx` selalu parameter pertama (golang-context).
- Tiap call eksternal dibungkus timeout default (`WithRequestTimeout`), overridable per-call via `context.WithTimeout` pemanggil.
- Read tidak pernah menulis state `Client`. `Client` immutable setelah `New` → aman dipakai konkuren.

### 3.3 Error contract

Dua kelas, dibedakan oleh `errors.As`:

```go
// Transport/validation — Go idiom
var ErrPoolNotFound = errors.New("dlmm: pool not found")
var ErrSlippageTooHigh = errors.New("dlmm: slippage exceeds 100%")

// On-chain program error — 111 kode dari IDL
type ProgramError struct {
    Code uint32       // 6000+
    Name string       // "InvalidBinId"
    Msg  string       // "Invalid bin id"
}
func (e *ProgramError) Error() string
func (e *ProgramError) Is(target error) bool   // dukung errors.Is(err, ErrX) via mapping
```

Pemisahan ini penting: user SDK.py/TSvant都无法 bedakan "RPC timeout" dari "program menolak karena bin invalid". Go memaksa itu terlihat sejak compile time.

Pencarian error dari RPC log Anchor: `program/lbclmm` meng-generate tabel `code → ProgramError`; `dlmm`解析 log ke tipe konkret. Kegagalan parse → error transport, bukan `ProgramError` tebakan.

### 3.4 Numerics — keputusan berisiko tertinggi

Rust `mul_div` (u128) naik ke **U256** untuk intermediate. Go punya pilihan, dan salah pilih = bug halus di hot loop swap.

| Opsi | Benar? | Performa | Alokasi |
|---|---|---|---|
| `math/big.Int` | ✅ | lambat | tinggi per-op |
| `Uint128` + 256-bit internal (`bits`) | ✅ | dekat native | nol di hot path |
| Semua `uint64` | ❌ overflow | — | — |

**Keputusan: opsi 2.** API publik memakai `bin.Uint128` (`[2]uint64`) supaya signature terbaca seperti on-chain; `math`_package` hanya memakai 256-bit intermediate melalui `internal/uint256`.

`math/big` **dipakai di test saja** — sebagai oracle independen untuk cross-check. Itu优点: kita punya dua implementasi yang tidak berbagi bug.

Fungsi inti (semua punya `Rounding`, semua return error saat overflow — cerminan `Option<u128>` di Rust):

```go
func MulDiv(x, y, denominator bin.Uint128, r Rounding) (bin.Uint128, error)
func MulShr(x, y bin.Uint128, offset uint8, r Rounding) (bin.Uint128, error)
func ShlDiv(x, y bin.Uint128, offset uint8, r Rounding) (bin.Uint128, error)
func GetPriceFromQIndex(qi, base, exponent bin.Uint128, r Rounding) (float64, error)
func GetQIndexFromPrice(price, base, exponent float64, r Rounding) (bin.Uint128, error)
```

`Rounding` = enum mulai dari 1 (`RoundingDown`, `RoundingUp`), zero value = invalid — sesuai design-patterns #4.

### 3.5 Type design

- Semua struct state punya `noCopy` bila membawa lock; receiver konsisten pointer.
- Enum mulai dari 0 sebagai sentinel `Unknown` bila 0 bukan nilai valid on-chain; kalau tidak, mulai dari 1.
- `var _ SomeIface = (*T)(nil)` untuk setiap tipe yang memenuhi interface.
- Field tag di semua struct yang di-serialize.

---

## 4. DECISION_RECORDS

### D1 — Base: `gagliardetto/solana-go` v1.24.0, bukan fork lokal
**Alasan:** satu-satunya dependensi runtime. Menyediakan `rpc`, `types`, `Transaction`, `PublicKey`, `bin.Decoder`. Fork `solana-foundation/solana-go` yang ada di `package-referense/` module path-nya **tetap** `gagliardetto/solana-go` — memakai fork hanya menghasilkan `replace` yang rapuh tanpa Benefit.
**Alternatif:** `replace` ke fork lokal. **Ditolak** — tidak ada fitur yang kita pakai yang hilang di upstream; `replace` lokal mengunci kita ke fork.
**Pemicu invalidasi:** upstream hilangkan API yang kita pakai →评估 upgrade vs fork saat itu.

### D2 — Binding di-generate sendiri dari IDL, bukan `anchor-go`
**Alasan:** `anchor-go` v1.0.0 terakhir 2023, IDL DLMM adalah format Anchor 0.30+ (`metadata.version: 0.12.0`, `address` top-level, `discriminator` per akun). Risiko parse salah tinggi.
**Alternatif:** tulis tangan. **Ditolak** — 76 instruction + 86 type tangan = ~4.000 baris mechanical yang justru paling rentan salah ketik.
**Konsekuensi:** generator (~800 baris) jadi artefak yang harus di-maintain. Dibatasi: hanya emit yang ada di IDL, tanpa inferensi.

### D3 — Lapis: pure math terpisah dari client I/O
**Alasan:** DLMM punya loop aritmetika panas (swap lintas bin) yang tidak boleh berinteraksi dengan jaringan. Pemisahan ini yang membuat O2 dan O4 bisa dibuktikan tanpa RPC, dan membuat swap math bisa di-fuzz.
**Alternatif:** satu struct besar ala TS. **Ditolak** — mengunci testability dan membuat `math` menarik dependensi RPC.
**Konsekuensi:** `Pool` harus di-thread eksplisit. 这是 ergonomic tax yang disepakati.

### D4 — Builder mengembalikan `Instruction`, tidak mengirim
**Alasan:** memungkinkan komposisi (multi-pair dalam 1 tx), simulasi caller, dan test tanpa signing. TS tidak bisa ini.
**Konsekuensi:** `Client.SendTransaction` jadi lapisan kedua yang eksplisit. API surface nambah satu konsep.

### D5 — DI: constructor manual + functional options, tanpa container
**Alasan:** ini **library**, bukan service. Container DI menambah indirection tanpa consumer kedua. Aturan "jangan buat interface sebelum ada implementasi kedua" berlaku di sini: `Client` menerima `solana.RPC` sebagai interface consumer-side yang memang sudah ada di `solana-go`.
**Ditolak:** `wire`/`dig`/`fx`/`do` — semua menambah dependensi dan build step untuk masalah yang tidak ada.

### D6 — Layout: library, `pkg/` publik di root, `internal/` privat
Mengikuti `golang-project-layout` "Libraries": API publik di direktori root-level, tidak ada `cmd/` selain optional `cmd/dlmmctl`.

### D7 — Golden fixture dari upstream sebagai oracle
**Alasan:** 22 file `.bin` + expected value di test Rust/TS = bukti yang sudah ada dan independen dari implementasi kita. Tidak perlu RPC untuk memvalidasi decode & math.
**Konsekuensi:** fixture di-copy (atau di-submodule) ke `testdata/`. PerluUzum pin commit upstream.

---

## 5. ASSUMPTION_RISK_LEDGER

| # | Asumsi | Risiko | Discriminator | reopen bila |
|---|---|---|---|---|
| A1 | `solana-go` v1.24.0 API cukup (bin codec, tx, `FindProgramAddress`) | Sedang | compile P0 | compile gagal |
| A2 | IDL di `idls/dlmm.json` = IDL program on-chain yang deploy | **Tinggi** | simulate instruksi hasil decode di devnet; bandingkan account discriminator vs bytes on-chain | mismatch discriminator |
| A3 | `commons/src/math` identik dengan math program on-chain | Tinggi | cross-check `math/big` + test `quote.rs` | test gagal |
| A4 | Anchor 0.30 IDL → struct Go straightforward | Sedang | decode 3 fixture LbPair, bandingkan ke `sdk.test.ts` | field meleset |
| A5 | 22 fixture cukup sebagai coverage decode | Sedang | foto: mutation test — ubah 1 byte fixture, decoder harus gagal | mutant lolos |
| A6 | `U256` intermediate cukup untuk semua operasi | Sedang | fuzz vs `math/big` | overflow tak tertangani |
| A7 | TypeScript SDK v1.9.14 sesuai dengan program yang dipakai user | Sedang | bandingkan program ID default | user di cluster lain |
| A8 | Go 1.27 toolchain kompatibel dengan `solana-go` | Rendah | `go build` | error versi |

**A2 adalah yang paling berbahaya** — kalau IDL lokal sudah stale relatif program on-chain, seluruh codec jadi salah dan test fixture tidak akan menangkapnya. Discriminator wajib di P1, bukan ditunda.

---

## 6. DELIVERY_EVIDENCE_PLAN

| Slice | Isi | Verifikasi | Bisa offline? |
|---|---|---|---|
| **P0** | `go.mod`, layout, `program/lbclmm` generator kerangka, 12 struct akun + codec | decode 22 fixture; field match `sdk.test.ts` | ✅ |
| **P1** | 76 instruction builder, 111 error, 29 event | round-trip encode→decode; discriminator match; **A2 check via simulate** | ✅ (kecuali A2) |
| **P2** | `math` + `internal/uint256` | fuzz vs `math/big` 10⁶ kasus; expected dari `quote.rs` test | ✅ |
| **P3** | `bin`, `binarray`, `pda` | golden values; PDA match `deriveBinArray` TS | ✅ |
| **P4** | `dlmm.Client` + `Pool` + read ops | `httptest` mock RPC: request shape + decode response | ✅ |
| **P5** | write ops (swap, add/remove liquidity, position) | byte-compare instruksi vs TS; simulate devnet | ⚠️ butuh devnet |
| **P6** | `strategy` (weight/curve/spot/bid-ask) + rebalance | golden dari `rebalance.test.ts`, `calculate_distribution.test.ts` | ✅ |
| **P7** | `oracle` + limit order | golden dari `oracle.test.ts`, `ilm.test.ts` | ✅ |
| **P8** | Zap (terpisah) | `zap-sdk/tests/fixtures` | ✅ |

**Sequencing rationale:** P0–P4 seluruhnya offline-verifiable. Ini sengaja: majority of the risk (A3, A4, A5, A6) terkurung di sana sebelum menyentuh jaringan.

---

## 7. OPEN_QUESTIONS

| # | Pertanyaan | Rekomendasi |
|---|---|---|
| Q1 | Module path / nama? Repo ini belum punya commit & remote. | `github.com/<user>/meteora-dlmm-go`, atau `meteora-sdk` kalau mau 1 repo untuk DLMM + Zap nanti |
| Q2 | Cluster default & program ID? | map per cluster (`mainnet-beta` / `devnet` / `localhost`), default devnet saat ini |
| Q3 | Fixture: copy ke `testdata/` atau submodule? | copy + pin commit di header file |
| Q4 | Repository publik atau privat? | affects `go.mod` path & apakah perlu LICENSE header |
| Q5 | Apakah perlu `cmd/dlmmctl` di scope awal? | tidak — P9, SDK dulu |

---

## Lampiran: sumber & bukti

**Index codebase-memory-mcp** (generation 2026-09-29T06:59:03Z / :08Z, mode `full`, coverage check bersih):

| Project | Nodes | Edges | Commit |
|---|---|---|---|
| `meteora-dlmm-sdk` | 2.807 | 9.256 | `576919e` |
| `meteora-zap-sdk` | 543 | 1.390 | `9198753` |
| `solana-go` | 7.122 | 40.337 | `5cf5b46` |

**Pola codec yang diikuti:** `programs/token-2022/accounts.go` — struct + `UnmarshalWithDecoder(dec *bin.Decoder)`, `bin:"optional"` untuk COption.
