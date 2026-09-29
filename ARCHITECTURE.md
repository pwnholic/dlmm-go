# Arsitektur Lengkap — Meteora DLMM Go SDK

Status: **implementation-ready design** (superset dari `DESIGN.md` + `FEATURES.md`)
Tanggal: 2026-09-29
Prinsip utama: **fleksibel & customisable tanpa menjadi interface soup**

---

## 0. Prinsip yang mengikat seluruh desain

Tiga aturan yang dipegang, dan tempat setiap keputusan bisa diperiksa terhadapnya:

| # | Aturan | Konsekuensi praktis |
|---|---|---|
| **P1** | **Interface hanya di seam yang terbukti berubah** | 9 seam (��1). Bukan 9 interface karena-navigasi skematis — tiap seam punya ≥2 implementasi nyata atau variasi yang terbukti. Sisanya **konkrit** |
| **P2** | **Nol global mutable state** | Dua `Config` berbeda bisa hidup di satu proses. Ini yang membedakan *library* dari *aplikasi* |
| **P3** | **Arah dependensi satu-way, tanpa siklus** | `math` tidak tahu apa itu Solana. `program` tidak tahu RPC. `client` tidak tahu policy |

### P1 diuji, bukan diasumsikan

Bukti bahwa seam transport & signer harus kita definisikan sendiri (bukan mewarisi):
- `solana-go` **tidak punya** interface `RPC` — `rpc.Client` adalah struct konkret (`rpc/client.go`)
- `solana-go` **tidak mengekspor** interface `Signer` — `Transaction.Sign(privateKeyGetter)`, `privateKeyGetter` unexported (`transaction.go:707,737`)

Jadi "fleksibel" di dua tempat itu berarti kerja kita, bukan warisan.

---

## 1. Peta package & arah dependensi

```
                        ┌──────────────────────┐
  config-driven  ──────▶│  cmd/dlmmctl         │  daemon CLI (G9)
                        └──────────┬───────────┘
                                   │
  plugin-driven  ──────▶ ┌─────────▼───────────┐
                        │  dlmm  (composition) │  registrasi seam + builder
                        └──┬───┬───┬───┬───┬───┘
                           │   │   │   │   │
        ┌──────────────────┼───┼───┼───┼───┼──────────────┐
        ▼                  ▼   ▼   ▼   ▼   ▼              ▼
 ┌─────────────┐  ┌──────────────┐  ┌──────────┐  ┌──────────────┐
 │  strategy   │  │  risk        │  │  agent   │  │   observ     │
 │  (G12)      │  │  (G2,G3)     │  │  (G1,G5, │  │   (G8)       │
 └──────┬──────┘  └──────┬───────┘  │   G7)    │  └──────┬───────┘
        │                │          └────┬─────┘         │
        └────────┬───────┘               │               │
                 ▼                       ▼               ▼
        ┌────────────────────────────────────────────────────┐
        │              dlmm  (Client + Pool)                  │  ← satu-satunya yang boleh I/O
        │  RefetchPool · GetBins · SwapQuote · Swap · …       │
        └───────────────┬────────────────────┬───────────────┘
                        │                    │
        ┌───────────────▼──────┐   ┌─────────▼────────────┐
        │   program / lbclmm   │   │  sim   (A1)          │
        │   codec + 76 builder │   │  simulator off-chain │
        └───────────────┬──────┘   └─────────┬────────────┘
                        │                    │
        ┌───────────────▼────────────────────▼───────────────┐
        │   math · bin · binarray · pda · oracle            │  ←nol I/O, nol Solana
        └───────────────────────┬────────────────────────────┘
                                │
                    ┌───────────▼────────────┐
                    │  internal/uint256      │
                    └────────────────────────┘

  tools/idlgen  ──generate──▶  program/lbclmm   (build-time, modul terpisah)
```

**Aturan yang bisa diuji:** `go list -deps ./math/... ./bin/... ./program/...` tidak boleh memuat `solana-go/rpc`. Kalau iya, P3 dilanggar.

---

## 2. Model ekstensi — inti "flexible & customisable"

### 2.1 Sepuluh seam yang terbukti

| # | Seam | Kenapa **terbukti berubah** (bukti, bukan MTV) | Bentuk |
|---|---|---|---|
| S1 | `Transport` | `rpc.Client` konkret. Variasi nyata: devnet/mainnet/local-validator, Helius/Triton/QuickNode, failover, rate-limit, mock test | `interface` |
| S2 | `Signer` | `privateKeyGetter` unexported. Variasi nyata: keypair lokal, KMS remote, multisig, custodian, **dan** signer yang tidak pernah memberikan private key (G4) | `interface` |
| S3 | `Strategy` | 4 implementasi sudah ada di TS (`SpotStrategyParameterBuilder`, `CurveStrategyParameterBuilder`, `BidAskStrategyParameterBuilder`, weight). Bentuk interface-nya sudah terbukti | `interface` |
| S4 | `PolicyRule` | Produk itu sendiri. Tiap user punya aturan risiko sendiri; mustahil untuk总监 exhaustif | `interface` + registry |
| S5 | `PriceSource` | ≥3 sumber nyata & berbeda sifat: oracle on-chain, Meteora Data API REST (X2), websocket/indexer. Simulator butuh virtual price | `interface` |
| S6 | `EventSink` | JSONL audit, `slog`, metrics, DB. Bentuk output berbeda total | `interface` |
| S7 | `MarketSource` | Meteora Data API (X2) vs on-chain `getProgramAccounts` vs indexer. Kualitas & biaya berbeda | `interface` |
| S8 | `TxDecorator` | Variasi lingkungan: ATA idempotent, priority fee, compute budget, ALT, Jito tip. Urutan & kondisi berbeda per deployment | `interface` |
| S9 | `Clock` | Wajib virtual untuk A1 (simulator) & test. `time.Now()` langsung di seluruh kode = simulasi mustahil | `interface` (kecil) |
| S10 | `Approver` | **Dari frame hybrid**: kanal persetujuan manusia benar-benar bervariasi — CLI interaktif, file yang dijatuhkan, webhook, tanda tangan remote, in-process callback. Bentuk, latensi, dan model kepercayaannya berbeda total | `interface` |

> S10 ditambahkan karena keputusan Q-FRAME = **hybrid**. Frame itu sendiri yang membuktikannya: begitu ada manusia di dalam loop, ada variasi nyata pada cara manusia itu menyetujui. Kalau frame-nya bot kedaulahan penuh, S10 **harus dicabut** — `VerdictNeedsApproval` jadi tidak mungkin.

### 2.2 Yang sengaja TIDAK jadi seam

| Kandidat | Alasan tidak jadi interface |
|---|---|
| 12 tipe akun (`LbPair`, `BinArray`, …) | Bentuknya tetap on-chain. Bukan variasi, bukan pluggable |
| `math` / `bin` / `pda` | Fungsi murni, deterministic, tidak ada variasi runtime yang bermakna. Interface hanya menambah hop |
| `ProgramError` | Tipe data, bukan collaborator |
| Strategy *parameters* | Struct data (P2: zero value berguna), bukan interface |

Perbedaannya: seam = **collaborator yang bisa diganti**, non-seam = **benda mati yang bentuknya sudah selesai**.

### 2.3 Dua sumbu kustomisasi

```
                    KUSTOMISASI
                         │
        ┌────────────────┴────────────────┐
        ▼                                 ▼
   AXIS 1: Config                    AXIS 2: Plugin
   (tanpa compile)                    (kode Go)
        │                                 │
   YAML / TOML / env                  register(Seam, Impl)
        │                                 │
   strategy params, policy            strategy custom,
   limits, thresholds,                policy rule custom,
   transport pool,                    price source custom,
   price source selection             tx decorator custom
        │                                 │
        └────────────┬────────────────────┘
                     ▼
              Config yang sama
        (P2: beberapa Config = beberapa proses/vault,
         tanpa global state)
```

**Kenapa dua sumbu:** user akhir butuh mengubah angka tanpa compile (Axis 1). Developer SDK butuh menambah kapabilitas (Axis 2). Kalau hanya Axis 1, SDK buntu. Kalau hanya Axis 2, user akhir tersedak.

**P2 sebagai pengikat:** tidak ada `var Global`. Semua runtime state ada di `*Runtime` yang dibuat eksplisit.

---

## 3. Spesifikasi package (ujung ke ujung)

### 3.1 `math` — nol I/O, nol Solana

```go
package math

type Rounding uint8
const (
    RoundingInvalid Rounding = iota // 0 = sentinel, invalid
    RoundingDown
    RoundingUp
)

func MulDiv(x, y, denominator bin.Uint128, r Rounding) (bin.Uint128, error)
func MulShr(x, y bin.Uint128, offset uint8, r Rounding) (bin.Uint128, error)
func ShlDiv(x, y bin.Uint128, offset uint8, r Rounding) (bin.Uint128, error)
func GetPriceFromQIndex(qi, base, exponent bin.Uint128, r Rounding) (float64, error)
func GetQIndexFromPrice(price, base, exponent float64, r Rounding) (bin.Uint128, error)
```

Semuanya return `error` pada overflow — mencerminkan `Option<u128>` di Rust. Tidak ada panic (design-patterns #6).

### 3.2 `internal/uint256` — 4×uint64, nol alokasi

```go
package uint256

type U256 [4]uint64          // little-endian limbs
func Mul128To256(a, b bin.Uint128) (U256, error)
func Div256By128(n U256, d bin.Uint128) (bin.Uint128, error)
func DivCeil256By128(n U256, d bin.Uint128) (bin.Uint128, error)
```

`math/big` **dilarang** di sini — hanya boleh muncul di `_test.go` sebagai oracle kedua.

### 3.3 `program/lbclmm` — generated, deterministik

```go
package lbclmm

var ProgramIDDevnet  = solana.MustPublicKeyFromBase58("LBUZKhRxPF3XUpBCjp4YzTKgLccjZhTSDM9YuVaPwxo")
const IDLVersion = "0.12.0"

// 12 account
type LbPair struct { /* …36 field… */ Discriminator [8]byte }
func DecodeLbPair(data []byte) (*LbPair, error)

// 76 instruction builder — signature seragam
func NewSwap(params SwapParams, accounts SwapAccounts) (Ix, error)
type Ix struct {
    ProgramID solana.PublicKey
    Accounts   []solana.AccountMeta
    Data       []byte
}

// 111 error
type ProgramError struct{ Code uint32; Name, Msg string }
func ErrorByCode(code uint32) (ProgramError, bool)

// 29 event
func DecodeEvent(discriminator [8]byte, data []byte) (Event, error)
```

Pola codec mengikuti `solana-go/programs/token-2022/accounts.go` (verified): struct + `UnmarshalWithDecoder(dec *bin.Decoder) error`, `bin:"optional"` untuk COption.

### 3.4 `strategy` — seam S3

Bentuk interface **dipertahankan dari TS** (`LiquidityStrategyParameterBuilder`) supaya port mechanical & terbukti:

```go
package strategy

type LiquidityStrategy interface {
    FindXParameters(amountX, minDeltaID, maxDeltaID, binStep, activeID bin.Uint128) (BidAskParameters, error)
    FindYParameters(amountY, minDeltaID, maxDeltaID, activeID bin.Uint128) (BidAskParameters, error)
    SuggestBalancedXParametersFromY(…) (BidAskParameters, bin.Uint128, error)
    SuggestBalancedYParametersFromX(…) (BidAskParameters, bin.Uint128, error)
}

type BidAskParameters struct{ Base, Delta bin.Uint128 }

func Register(name string, s LiquidityStrategy)
func ByName(name string) (LiquidityStrategy, error)   // Axis 2
```

Implementasi bawaan: `spot`, `curve`, `bidask`, `weight`. Registry = titik ekstensi Axis 2; `StrategyType` di config = Axis 1.

### 3.5 `risk` — seam S4, inti G2/G3

```go
package risk

type Action interface{                 // union-ish, JSON-serializable
    Kind() ActionKind
    Mints() []solana.PublicKey
    MaxValueSOL() float64
}

type Policy struct {
    MaxPerTxSOL      float64
    MaxPerDaySOL     float64
    MaxSlippageBps   uint32
    MaxPositionValueSOL float64
    AllowedMints     []solana.PublicKey   // kosong = tolak semua (default deny)
    AllowedPrograms  []solana.PublicKey
    Rules            []Rule               // Axis 2: custom
}

type Rule interface {
    Name() string
    Evaluate(ctx context.Context, a Action, s State) (Verdict, error)
}
type Verdict uint8
const (
    VerdictDeny Verdict = iota
    VerdictAllow
    VerdictNeedsApproval
)

func (p *Policy) Evaluate(ctx context.Context, a Action, s State) (Verdict, error)
```

**`AllowedMints` kosong = deny-all**, bukan allow-all. Default-deny adalah inti G3. Perhatikan `Evaluate` juga menerima `State` (saldo kumulatif hari ini, drawdown) → daily limit punya state, dan state itu milik `*Runtime`, bukan global.

### 3.6 `agent` — G1, G5, G7

```go
package agent

type Plan struct {
    Version    int
    CreatedAt  time.Time
    Steps      []Step
    Cost       Cost                 // G10
    PlanHash   [32]byte             // isi sebelum Apply
}

type Step struct {
    Kind        StepKind
    Accounts    []AccountRef         // G1: BARU derive dari PDA, bukan string dari LLM
    Instruction solana.Instruction  // sudah ter-build
    Reason      Reason               // G8
}

type Reason struct {
    Trigger       string
    Metrics       map[string]float64
    Alternatives  []RejectedAlternative   // G8: apa yang ditolak & kenapa
}

type Cost struct {
    EstimatedCU        uint64
    EstimatedFeeSOL    float64
    TransactionCount   int
    PositionCount      int   // bukti X3
}

type OutcomeKind uint8
const (
    OutcomeUnknown OutcomeKind = iota
    OutcomeInsufficientLiquidity
    OutcomeSlippageWouldFail
    OutcomePriceImpactTooHigh
    OutcomePositionOutOfRange
    OutcomeStaleOracle
    OutcomeRentTooLow
    OutcomePolicyDenied
    OutcomeInsufficientBalance
    // … exhaustive; menambah = breaking change, dan itu memang Fiesta yang bagus
)
type Outcome struct {
    Kind    OutcomeKind
    Plan    *Plan
    Sig     solana.Signature
    Err     error
}
```

**`Plan` JSON-serializable** — ini yang membuatnya bisa dikirim ke agent lain, di-review manusia, atau disimpan ke disk lalu di-`Apply()` belakangan (G6).

### 3.7 `observ` — seam S6

```go
package observ

type Sink interface {
    Emit(ctx context.Context, rec Record) error
    Close() error
}
type Record struct {
    Ts      time.Time
    Kind    RecordKind   // Decision | PlanCreated | PlanApplied | Outcome | PolicyDenied
    Pool    solana.PublicKey
    PlanHash [32]byte
    Data    map[string]any
}
```

JSONL built-in (default), `slog` adapter, dan stub no-op untuk test. G8 provenance lewat sini.

### 3.8 `dlmm` — Client + Pool, satu-satunya yang boleh I/O

```go
package dlmm

type Pool struct {                       // snapshot immutable, nol method I/O
    Pubkey    solana.PublicKey
    ProgramID solana.PublicKey
    LbPair    lbclmm.LbPair
    Bitmap    *lbclmm.BinArrayBitmapExtension
    TokenX    TokenReserve
    TokenY    TokenReserve
    Rewards   []TokenReserve
    Clock     Clock
}

type Transport interface {               // S1 — definisi kita (solana-go tidak punya)
    GetMultipleAccounts(ctx context.Context, keys []solana.PublicKey, opts AccountOpts) ([]Account, error)
    GetAccount(ctx context.Context, key solana.PublicKey) (Account, error)
    Simulate(ctx context.Context, tx *solana.Transaction) (SimulateResult, error)
    GetLatestBlockhash(ctx context.Context) (solana.Hash, error)
    SendTransaction(ctx context.Context, tx *solana.Transaction, opts SendOpts) (solana.Signature, error)
}

type Signer interface {                  // S2 — definisi kita
    PublicKey() solana.PublicKey
    Sign(message []byte) ([]byte, error)
}
```

`Transport` sengaja **tidak** alias `*rpc.Client` — supaya test bisa pakai stub tanpa jaringan, dan supaya multi-endpoint bisa diimplementasikan di luar SDK.

### 3.9 `market` — seam S7 (screening)

```go
package market

type Source interface {
    List(ctx context.Context, q Query) ([]PoolBrief, error)
    OHLCV(ctx context.Context, pool solana.PublicKey, iv Interval) ([]Candle, error)
}
```

Built-in: `datapi` (Meteora Data API REST, X2) dan `onchain` (`getProgramAccounts` + decode). **Tidak ada scanner sendiri** — itu keputusan yang sudah diambil di `FEATURES.md` §relegasi.

---

## 4. Alur ujung ke ujung (G3+G5+G7 dalam operasi)

Inilah "dari awal sampai akhir" yang konkret:

```
 [1] Trigger            price bergerak keluar range / agent request / timer
        │
 [2] Observe           Client.RefetchPool(ctx)          → Pool  (immutable snapshot)
        │
 [3] Simulate mentally  strategy + risk.State            → addr strategy params   [PURE]
        │                sim.Preview(ctx, pool, steps)   → PnL estimate            [PURE]
        │
 [4] Build Plan        steps, reason (G8), cost (G10)     [PURE]
        │                plan.Hash()
        │
 [5] ⛔ GATE            policy.Evaluate(ctx, action, state) → Verdict
        │                  Allow     → lanjut
        │                  Approval  → serialisasi Plan, tunggu approve
        │                  Deny      → observ.Emit(PolicyDenied) + Outcome, STOP
        │
 [6] ⛔ GATE            validateG1: semua address di Step harus
        │                derive-able dari PDA;nol opaque address
        │
 [7] Compile           TxDecorator chain (S8):
        │                ATA idempotent → compute budget → priority fee → [ALT] → [Jito tip]
        │
 [8] ⛔ GATE            Transport.Simulate(ctx, tx)
        │                  err / log tak terduga → STOP (G5 typed outcome)
        │                  ConservationChecker: Σ token delta == yang diharapkan
        │
 [9] Sign              S2 Signer.  ← key tidak pernah keluar dari SDK (G4)
        │                  idempotency key dicatat ke journal
        │
 [10] Send             Transport.SendTransaction
        │
 [11] Verify           Re-read pool; cek post-state cocok dengan yang di-pointer Step
        │
 [12] Audit            observ.Emit(PlanApplied, hash, cost, actual vs estimate)
        │
 [13] Journal          hapus idempotency key; simpan ringkasan
```

**Setiap gate punya kegagalan yang typed (G5)** — tidak ada jalur yang bisa "gagal diam-diam".

---

## 5. Kontrak config (Axis 1)

```yaml
# dlmm.yaml — contoh, illustrative
cluster: mainnet-beta

transport:
  endpoints:
    - { url: "https://mainnet.helius-rpc.com", weight: 3 }
    - { url: "https://api.mainnet-beta.solana.com", weight: 1 }
  commitment: confirmed
  timeout: 15s
  failover: true

signer:
  # G4: hanya reference ke provider. private key tak pernah di config ini.
  provider: file            # file | kms | multisig | custody
  path: ~/.config/dlmm/key.json

risk:
  max_per_tx_sol: 25
  max_per_day_sol: 200
  max_slippage_bps: 50
  max_position_value_sol: 500
  allowed_mints: []         # KOSONG = deny all. Wajib diisi eksplisit.
  allowed_programs:
    - LBUZKhRxPF3XUpBCjp4YzTKgLccjZhTSDM9YuVaPwxo
    - zapvX9M3uf5pvy4wRPAbQgdQsM1xmuiFnkfHKPvwMiz
  rules: []                 # Axis 2: nama rule terdaftar

strategy:
  liquidity: spot           # spot | curve | bidask | weight  (atau plugin)
  rebalance:
    mode: both              # left | right | both
    trigger_bps: 120        # seberapa jauh harga harus keluar sebelum shift

market:
  source: datapi            # datapi | onchain
  base_url: https://dlmm.datapi.meteora.ag

observ:
  sink: jsonl               # jsonl | slog | noop
  path: ./audit.jsonl

sim:
  enabled: true
  max_scenarios: 10000
```

**Invariants yang harus divalidasi saat load** (fail fast, bukan saat runtime):
- `allowed_mints` kosong → **error**, dengan pesan yang jelas. Ini intentional: config yang salah harus meledak di load.
- `max_slippage_bps > 10000` → error
- `strategy.liquidity` tidak dikenal → error
- `max_per_tx_sol > max_per_day_sol` → error (kontradiktif)

---

## 6. Resep ekstensi (bukti bahwa P1 bekerja)

### 6.1 Tambah strategi custom (Axis 2)

```go
package mystategy

type MyStrategy struct{ Shape float64 }

func (s *MyStrategy) FindXParameters(amountX, minDeltaID, maxDeltaID, binStep, activeID bin.Uint128) (strategy.BidAskParameters, error) { … }
func (s *MyStrategy) FindYParameters(…) (strategy.BidAskParameters, error) { … }
func (s *MyStrategy) SuggestBalancedXParametersFromY(…) (strategy.BidAskParameters, bin.Uint128, error) { … }
func (s *MyStrategy) SuggestBalancedYParametersFromX(…) (strategy.BidAskParameters, bin.Uint128, error) { … }

func init() { strategy.Register("my-strategy", &MyStrategy{Shape: 0.7}) }
```

Pemakaian: `strategy: { liquidity: my-strategy }` di config. **Nol perubahan di SDK.**

### 6.2 Tambah aturan risiko custom (Axis 2)

```go
func init() {
    risk.Register("no-memecoin-mint", risk.RuleFunc(func(ctx context.Context, a risk.Action, s risk.State) (risk.Verdict, error) {
        if containsSuspiciousMint(a.Mints(), s.Registry) {
            return risk.VerdictDeny, fmt.Errorf("memecoin mint ditolak: %v", a.Mints())
        }
        return risk.VerdictAllow, nil
    }))
}
```

Config: `risk: { rules: [no-memecoin-mint] }`. Kalau rule mengembalikan `VerdictNeedsApproval`, alur [5] menjemput. **Aturan bisa ditolak oleh owner** — 这是 extensibility yang tidak bisa dimiliki hard-coded policy.

### 6.3 Ganti transport ke multi-endpoint (Axis 2)

```go
rt := dlmm.New(pooling.New(endpoints, failover.On), opts…)   // implementasi di luar SDK
```

Karena `Transport` interface milik consumer (`dlmm`), implementasinya boleh tinggal di repo user. SDK tidak perlu tahu strategi retry-nya.

### 6.4 Ganti price source (Axis 2)

```go
rt := dlmm.New(…, dlmm.WithPriceSource(mysource.New(jitterDampener)))
```

Menambah anti-MEV smoothing tanpa menyentuh SDK.

---

## 7. Strategi verifikasi (per layer)

| Layer | Umpan balik | Bukti | Kapan bisa |
|---|---|---|---|
| `math` / `uint256` | **Invariant** + cross-check vs `math/big` | 10⁶ kasus random; `MulDiv(x,y,d) ≤ x*y`; non-negatif; idempoten | offline |
| `program/lbclmm` | **Golden fixture** 22 file `.bin` | decode → bandingkan `sdk.test.ts` | offline |
| — | **A2 discriminator** | instruction hasil encode → simulate devnet, bandingkan discriminator | devnet |
| `pda` | **Cross-ref** | `deriveBinArray` Go == TS untuk 10⁵ index | offline |
| `strategy` | **Property** | 4 strategy × grid parameter; tidak pernah menghasilkan delta negatif | offline |
| `sim` | **Oracle historis** | rekonstruksi 1 swap dari event on-chain → `amount_out` harus **persis** | RPC read |
| `risk` | **Existential** | 100 fixture ==================================================================== | offline |
| `client` | **`httptest`** | request shape + decode response fixture | offline |
| `agent` | **Golden plan** | snapshot `Plan` JSON → regresi lock | offline |
| `end-to-end` | **Dry-run** | plan → simulate → tidak broadcast | devnet |

**Catatan penting untuk `risk`:** aturan uji harus bernilai **recall**, bukan precision. Kalau `DenyByDefault` tidak menolak 100% dari aksi berisiko, itu bug di gate — bukan false positive yang acceptable.

**A2 tetap jadi risiko tertinggi** (§risiko di `DESIGN.md`): kalau IDL lokal stale vs program on-chain, seluruh codec salah dan fixture ikut stale. Discriminator check **wajib di awal, bukan ditunda.**

---

## 8. Slices implementasi

| Slice | Isi | Output verifiable | Dependensi |
|---|---|---|---|
| **S0** | `go.mod`, layout, `Makefile`, `.golangci.yml` | `go build ./...` | — |
| **S1** | `tools/idlgen` + `program/lbclmm` kerangka + 12 struct akun | decode 22 fixture | S0 |
| **S2** | `math` + `internal/uint256` | 10⁶ fuzz vs `big` | S0 |
| **S3** | `pda` + `bin` + `binarray` | cross-ref vs TS | S2 |
| **S4** | `Transport` + `SignClient` + `Pool` + read ops | `httptest` | S1, S3 |
| **S5** | 76 instruction builder + 111 error + 29 event | round-trip + **A2 check** | S1 |
| **S6** | `strategy` (4 impl + registry) | property test | S3 |
| **S7** | `sim` (A1) | rekonstruksi swap historis | S2, S3 |
| **S8** | `risk` (G2, G3) + `observ` | existential test | S0 |
| **S9** | `agent` Plan/Apply (G5, G7, G10) + journal (G6) | golden plan JSON | S4, S5, S8 |
| **S10** | write ops (swap, liquidity, position) | dry-run + simulate devnet | S5, S9 |
| **S11** | `market` sources (S7) | contract test vs API live | S4 |
| **S12** | `cmd/dlmmctl` (G9, G12) | CLI smoke test | S9–S11 |
| **S13** | analyzer (A3) | rekonstruksi 1 position | S5 |
| **S14** | re-centering engine (B1) | replay offline, dry-run | S7, S9, S10 |
| **S15** | Zap | fixture zap-sdk | S10 |

**S1–S4 dan S6–S9 seluruhnya offline-verifiable.** Itu disengaja: mayoritas risiko (A3/A4/A5/A6) terkurung sebelum ada yang menyentuh jaringan.

---

## 9. Risk ledger

| # | Risiko | Dampak | Mitigasi | Sinyal awal |
|---|---|---|---|---|
| R1 | **A2: IDL stale** vs program on-chain | seluruh codec salah, fixture ikut stale | discriminator check di **S5** | simulate mismatch |
| R2 | Interface soup — 9 seam terlalu banyak | kompleksitas tanpaConsumer | tiap seam harus punya consumer ke-2 dalam 12 bulan; kalau tidak, **tarik** | seam tanpa consumer ke-2 |
| R3 | Registry global di `strategy`/`risk` mutable | melanggar P2, sulit di-test paralel | registry read-only setelah init; `Register` dipanggil di `init()`/explicit bootstrap | race di `go test -race` |
| R4 | `Policy` di-`Apply()` tapi ada jalur `Send` langsung | policy bisa di-bypass | **semua** jalur tulis wajib lewat `Apply()`. Audit: `grep -rn "SendTransaction" --include="*.go"` di `internal/` harus kosong | review finding |
| R5 | Simulator meyakinkan-dan-salah | keputusan buruk | cross-check vs `math/big` + rekonstruksi historis (S7) | mismatch fixture historis |
| R6 | Pemakaian tanpa Config → default terlalu longgar | kerugian | default-deny di `AllowedMints`, `New()` tanpa policy → error, bukan warning | config load test |
| R7 | Versi program berubah (LbPair punya field `version`) | decode salah diam-diam | pinning IDL; `make generate && git diff` sebagai review gate | field version tak dikenal saat decode |
| R8 | Rate limit / RPC provider down saat bot jalan | operasi gagal di tengah | `Transport` seam + failover built-in; S8 retry yang ctx-aware | simulasi kegagalan endpoint |

**R4 deserves artefak teste sendiri:** test yang membuktikan tidak ada jalur yang bisa signing tanpa melewati `Policy.Evaluate`. Kalau tak ada, tulis.

---

## 10. Data API Client (seam S7, berspesifikasi penuh)

Sumber: `docs.meteora.ag/developer-guides/dlmm/api-reference/overview.md` + OpenAPI `0.1.0`, diakses 2026-09-29.

### 10.1 Fakta kontrak

| Aspek | Nilai |
|---|---|
| Production | `https://dlmm.datapi.meteora.ag` |
| Development | `https://dlmm.dev.metdev.io` |
| Swagger | `/swagger-ui/` di kedua environment |
| Auth | **Tidak ada** — `security: []` di OpenAPI. Keyless |
| Rate limit | **30 RPS** (dokumentasi eksplisit) |
| Method | Semua `GET` |
| Error | `{"message": string}` |
| List params | `page` (1-based), `page_size` (pools ≤1000, groups ≤100), `query`, `sort_by`, `filter_by` |
| `sort_by` | `<field>:<direction>` atau `<metric>_<window>:<direction>` |
| `filter_by` | `<field><op><value> && <field><op><value> …` |
| Time window | `5m 30m 1h 2h 4h 12h 24h` |

### 10.2 19 endpoint

```
Pools      GET /pools
           GET /pools/groups
           GET /pools/groups/{lexical_order_mints}
           GET /pools/{address}
           GET /pools/{address}/ohlcv                      ← historical, untuk simulator
           GET /pools/{address}/volume/history
Portfolio  GET /portfolio                                 ← closed positions
           GET /portfolio/open
           GET /portfolio/total
Positions  GET /positions/{address}/historical            ← add/remove/claim events
           GET /positions/{pool_address}/pnl              ← PnL on-the-fly
Stats      GET /stats/protocol_metrics
Limit ord. GET /wallets/{wallet}/limit_orders/open/pools
           GET /wallets/{wallet}/limit_orders/open/pools/{pool_address}
           GET /wallets/{wallet}/limit_orders/closed/pools
           GET /wallets/{wallet}/limit_orders/closed/pools/{pool_address}
           GET /wallets/{wallet}/limit_orders/summary
           GET /wallets/{wallet}/limit_orders/pools/{pool_address}/bonus_claimed
Wallets    GET /wallets/{wallet}/pools/{pool_address}/total_claims
```

### 10.3 Bentuk Go

```go
package datapi

type Client struct {
    baseURL string
    http    *http.Client
    bucket  *RateBucket      // 30 RPS, WAJIB — bukan opsional
}

type Option func(*config) error
func WithBaseURL(u string) Option
func WithEnvironment(env Env) Option        // Prod | Dev
func WithRateLimit(rps float64) Option       // default 30
func WithHTTPClient(h *http.Client) Option

func New(opts ...Option) (*Client, error)

// 19 method, satu-satu. Nol generik — nama endpoint = nama method.
func (c *Client) Pools(ctx context.Context, q PoolsQuery) (PoolsPage, error)
func (c *Client) Pool(ctx context.Context, addr solana.PublicKey) (PoolBrief, error)
func (c *Client) OHLCV(ctx context.Context, addr solana.PublicKey, q OHLCVQuery) (OHLCVSeries, error)
func (c *Client) HistoricalVolume(ctx context.Context, addr solana.PublicKey, q VolumeQuery) (VolumeSeries, error)
func (c *Client) Portfolio(ctx context.Context, wallet solana.PublicKey, q PortfolioQuery) (PortfolioPage, error)
func (c *Client) PortfolioOpen(ctx context.Context, wallet solana.PublicKey, q PortfolioQuery) (PortfolioPage, error)
func (c *Client) PortfolioTotal(ctx context.Context, wallet solana.PublicKey) (PortfolioTotal, error)
func (c *Client) PositionHistorical(ctx context.Context, position solana.PublicKey, q PageQuery) (PositionEvents, error)
func (c *Client) PositionPnL(ctx context.Context, pool, wallet solana.PublicKey, q PnLQuery) (PositionPnLPage, error)
func (c *Client) ProtocolMetrics(ctx context.Context) (ProtocolMetrics, error)
// … 10 limit-order & wallet method
```

**Rate limit bukan fitur, itu kontrak.** `RateBucket` (token bucket) ada di constructor. 429 dari server → retry dengan `Retry-After` yang ctx-aware (design-patterns #11).

### 10.4agnetism yang harus dihormati

- Semua response **dideserialisa ke struct bertag JSON** — bukan `map[string]any`. Field yang tidak kita pakai boleh diabaikan, tapi yang kita pakai wajib bertipe.
- `timestamp` (int64) **dan** `timestamp_str` ada berdua. Pakai `timestamp` untuk perhitungan, `timestamp_str` hanya untuk display.
- Halaman: `page_size` 1000 untuk pools adalah reddit untuk Einzelfehler — default konservatif (100), naikkan eksplisit.
- Endpoint **tidak punya versi di path** (`v1`, `v2`).，跟着 `version: 0.1.0` di OpenAPI. Pin di `const APISpecVersion = "0.1.0"` dan gagal cepat kalau schema berubah bentuk.

---

## 11. Replikasi A3 — dan penemuan yang mengubahnya

Data API sudah menyediakan:
- `GET /positions/{address}/historical` → add/remove/claim-fee/claim-reward events
- `GET /positions/{pool}/pnl` → PnL open+closed, on-the-fly calculation
- `GET /portfolio/*` → PnL agregat lintas pool

**Artinya A3 ("Position lifecycle analyzer") sebagian besar sudah ada sebagai API.** Ini menurunkan nilai A3 sebagai fitur — tapi **menaikkan nilainya sebagai alat verifikasi**, dan itu lebih penting.

### A3 direframing

| Plans lama | Rencana baru |
|---|---|
| Bangun PnL calculator dari nol | **Bungkus** API-nya. Hitung ulang lokal **hanya** untuk fallback saat API mati |
| Event decoder = kebutuhan fitur | Event decoder = **verifikasi**: bandingkan hasil decode lokal vs `PositionHistorical`. Dua implementasi independen |
| “Menarik” | **Tepat**: tanpa oracle ini, S5 dan S13 tidak punya pembuktian eksternal |

**Konsekuensi untuk S13:** turun dari "2 minggu, fitur baru" jadi "1 minggu, client + cross-check + fallback". Effort turun, nilai bukti naik.

**Dan ini menjawab sebagian U2 di `FEATURES.md`:** Data API adalah implementasi independen dari PnL yang tidak ada di repositori mana pun yang saya pindai.),”

## 12. Rekonsiliasi dengan konvensi Go resmi Meteora

Temuan: **`github.com/MeteoraAg/dbc-go` sudah ada** (dokumentasi: `developer-guides/dbc/go-integration/reference.md`).

### 12.1 Apa yang mereka lakukan

| Aspek | `dbc-go` (resmi) |
|---|---|
| Layout | **Datar**: `instructions/`, `math/`, `common/`, `helpers/`, `examples/` |
| Cara pakai | `git clone` + `go mod tidy` — **bukan** `go get`. Diperlakukan sebagai repo, bukan modul terbit |
| go.mod | **`github.com/gagliardetto/solana-go`** ✅ |
| Penamaan | `helpers.Deserialize*`, `helpers.Derive*PDA`, `instructions.Get*` |
| Cakupan | backend, indexer, script — bukan library_semua |

### 12.2 Dua hal yang harus kita adopsi

**1. D1 terkonfirmasi independen.** `dbc-go` memakai upstream `gagliardetto/solana-go`. Keputusan base library kita bukan debatable lagi.

**2. Selaras pada penamaan & tata letak yang tidak perlu berbeda.** Kalau dua SDK Go dari Meteora memakai kosakata berbeda, setiap orang yang迁 dari satu ke的其他 akan kehilangan waktu. Kita adopsi:

| Konvensi `dbc-go` | Yang kita lakukan |
|---|---|
| `helpers.Deserialize*` | ✅ `lbclmm.Decode*` (setara; `Decode` lebih idiomatik Go) |
| `helpers.Derive*PDA` | ✅ `pda.Derive*` |
| `instructions.Get*` | ✅ `Client.Get*` |
| `math/` terpisah, tanpa I/O | ✅ identik dengan desain kita |
| `common/` | ⚠️ kita bagi: `program/lbclmm` (account + konstanta) & `math` |
| `examples/` | ✅ `cmd/` + `examples/` |

### 12.3 Yang **tidak** kita adopsi, dengan alasan

| `dbc-go` | Kita | Alasan |
|---|---|---|
| Layout datar, 5 folder | Layout berlapis + 9 seam | Scope berbeda. `dbc-go` = reader + builder untuk backend. Kita = SDK untuk bot & agent otonom dengan safety rail, plan/apply, dan multi-konfigurasi. Layering itu yang membuat G3/G5 bisa diuji |
| `git clone`, bukan modul | `go get`-able, module语义 jelas | Bot & agent = konsumen library.Sysemnya butuh versi |
| Tanpa rate limiter | `RateBucket` wajib di constructor | 30 RPS itu kontrak keras; default diam-diam akan gagal saat load |
| Tanpa policy/persistensi | `risk` + `agent` + journal | Requirements utama frame kita |

**Yang harus jujur kita akui:** kalau targetnya “reader untuk backend service”, desain kita **lebih(negligible) dari yang perlu**. Flat seperti `dbc-go` akan lebih cepat sampai. Kita谈论 complexity karena ada requirement (agent safety, plan/apply, extensibility) yang tidak dimiliki use-case `dbc-go` — **bukan** karena layering itu lebih baik secara abstrak.

---

## 13. Yang TIDAK termasuk (batas tegas)

- **Bukan** executor MEV/atomic-bundle produksi — `TxDecorator` menyediakan titik pasang, tapi logika MEV di luar
- **Bukan** reimplementasi SVM — tidak ada Go SVM yang mature (U1 di `FEATURES.md`); verifikasi tulis lewat devnet simulate
- **Bukan** reimplementasi PnL calculator yang sudah ada di Data API — bungkus + fallback lokal + cross-check (§11)
- **Bukan** router cross-protocol — `TxDecorator` + `market` seam adalah titik pasang, implementasinya bukan
- **Bukan** frontend/dashboard
- **Bukan** custodian/kms — `Signer` seam-nya ada, implementasi di luar

Batas-batas ini disengaja: semuanya adalah **project terpisah** dengan kalender, risiko, dan orang yang berbeda. Menempelkannya jadi fitur SDK mengunci satu untuk yang lain.

---

## 14. Keputusan yang sudah dikunci

| # | Keputusan | Nilai | Sumber |
|---|---|---|---|
| Q1 | Module path | **`github.com/pwnholic/dlmm-go`** | user, 2026-09-29 |
| Q2 | Base library | `github.com/gagliardetto/solana-go` v1.24.0 | D1 — dikonfirmasi `dbc-go` |
| Q3 | Program ID default | map per cluster, tidak ada default implisit | §5 config |
| — | **Q-FRAME** | **HYBRID** — bot otonom **di dalam amplop**, di luar amplop wajib persetujuan manusia | user, 2026-09-29 |

---

## 15. Model otonomi hybrid (inti Q-FRAME)

### 15.1 Yanghybrid xenak

Bukan "bot atau manusia". Bot jalan sendiri **selamaaksinya di dalam amplop**; di luar itu, manusia masuk. Jadi yang harus dirancang bukan *dua mode*, tapi **satu state machine dengan ambang yang jelas**.

```
                    ┌──────────┐
                    │  DRAFT   │  Plan dibangun, belum diuji
                    └────┬─────┘
                         │ Policy.Evaluate
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
    ┌──────────┐   ┌───────────┐   ┌──────────┐
    │  ALLOW   │   │  PENDING  │   │   DENY   │
    │ (dalam   │   │ Approval  │   │ (di luar │
    │ amplop)  │   │  needed   │   │ amplop + │
    └────┬─────┘   └─────┬─────┘   │ ，也许规则 │
         │               │         │  再禁）    │
         │               │ ◄───────┘
         │               │ Approver.Approve(ctx, plan)
         │               │   ├─ GrantOnce(hash)      ← sekali
         │               │   ├─ GrantScope(hash, ttl) ← standing, dibatasi
         │               │   └─ Reject(reason)
         │               │
         │       ┌───────┴────────┐
         │       ▼                ▼
         │  ┌──────────┐    ┌──────────┐
         │  │ EXPIRED  │    │ REJECTED │
         │  └────┬─────┘    └──────────┘
         │       │ TTL habis / revalidasi gagal
         │       └──► DRAFT (re-plan)
         ▼
    ⛔ revalidasi staleness
         │
         ├─ masih fresh  → SIMULATE → sign → APPLIED
         └─ sudah basi   → EXPIRED (jangan eksekusi plan basi)
```

**Kunci desain: `PENDING` itu persisten, bukan in-memory.** Persetujuan manusia memakan waktu — menit, kadang jam. `Plan` adalah JSON, jadi ia **harus bisa********` ======` ditulis ke disk, di-review, diApprove setelahISM , lalu dieksekusiula hours kemudian. Di sinilah TTL jadi wajib, bukan opsional.

### 15.2 Bentuk Go

```go
package agent

type PlanState uint8
const (
    PlanDraft PlanState = iota
    PlanAllow              // dalam amplop → boleh langsung
    PlanPendingApproval    // butuh manusia
    PlanApproved           // disetujui, belum dieksekusi
    PlanRejected
    PlanExpired            // TTL habis atau revalidasi gagal
    PlanApplied
    PlanFailed
)

type Plan struct {
    Version   int
    PlanHash  [32]byte      // isi SETELAH hitung hash; hash = identitas persetujuan
    State     PlanState
    CreatedAt time.Time
    TTL       time.Duration // default 5m; 0 = no expiry ( discouraged)
    Steps     []Step
    Cost      Cost
    Reason    Reason
    // audit
    Approval  *Approval
}

type Approval struct {
    Kind      ApprovalKind  // GrantOnce | GrantScope
    By        string        // identitas approver
    At        time.Time
    RejectRsn string
    ScopeTTL  time.Duration // hanya untuk GrantScope
}

// S10 — interface defined where consumed
type Approver interface {
    Approve(ctx context.Context, p *Plan) (Approval, error)
}

// Built-in
type FileApprover    struct{ Dir string }  // plan.jsonl + plan.approved
type WebhookApprover struct{ URL, Secret string }
type CallbackApprover  func(*Plan) (Approval, error)
```

### 15.3 Amplop otonom (G3 dalam mode hybrid)

`Policy` dipecah jadi dua lapis, karena “di dalam amplop” harus **jelas secara operasional**:

```go
type Policy struct {
    // ── Amplop: boleh jalan tanpa manusia ──
    Envelope Envelope
    // ── Di luar amplop: PENDING, bukan DENY ──
    OnOutsideEnvelope Verdict   // NeedsApproval (default) | Deny
    Rules []Rule               // custom rules (Axis 2)
}

type Envelope struct {
    MaxPerTxSOL        float64
    MaxPerDaySOL       float64
    MaxSlippageBps     uint32
    MaxPositionValueSOL float64
    AllowedMints       []solana.PublicKey   // kosong = semua tertahan, bukan semua diizinkan
    AllowedPrograms    []solana.PublicKey
    MaxPriceImpactBps  uint32
    RequireSimulation  bool                  // default true, turn-off harus eksplisit
}
```

Perubahan penting dari desain sebelumnya: **default `AllowedMints` kosong = semua action jadi `PENDING` (bukan `DENY`)**. Itu perilaku hybrid yang benar — humans exceso bisa menyetujui, dan jejaknya tercatat. Di frame bot kedaulahan penuh, kosong harus `DENY`.

### 15.4 Re-staleness — jebakan yang paling mudah terlewat

Plan disetujui pada 14:00. Dieksekusi 14:07. Harga bergerak 3%. Plan itu **sah secara prosedural tapi salah secara ekonomi**.

`Apply()` wajib:
1. Cek `time.Since(CreatedAt) > TTL` → `PlanExpired`
2. **Re-simulate** walau sudah disetujui (biaya rendah,+-------- mencegah kerugian besar)
3. Bandingkan `Cost` aktual vs `Cost` di plan; selisih melewati `MaxSlippageBps` → `PlanExpired`, perlu approval baru
4. Catat di audit: harga saat plan vs saat apply

Kalau langkah 2–3 dihilangkan, approval manusia jadi **security theater** — manusia menyetujui angka yang sudah tidak berlaku.

### 15.5 Eskalasi

Perilaku saat berulang gagal/ditolak — tanpa ini bot bisa membanjiri manusia dengan approval request:

```go
type Escalation struct {
    ConsecutiveRejects int      // ≥ N → auto-tighten envelope, pause
    ConsecutiveSlips   int      // slippage gagal ≥ N → pause
    DrawdownBps        float64  // PnL turun<X% → pause semua
    Cooldown           time.Duration
}
```

Saat `Pause` aktif: semua plan → `PENDING` dengan `Reason{Trigger: "escalation"}`, sampai reset eksplisit. **Pause lebih baik daripada retry** saat adaallos首席UX soal human-in-the-loop.

### 15.6 Dampak ke slice

| Slice | Yang berubah |
|---|---|
| S8 | `risk` jadi dua-lapis: `Envelope` + `Verdict` tri-state |
| S9 | `Plan` dapat `State`, `TTL`, `Approval`; `Approver` interface + 3 built-in |
| S12 | `dlmmctl` dapat approval flow: `plan` → `approve` → `apply` (3 subcommand) |
| S14 | Re-centering wajib punya batas: berapa kali/hr auto-rebalance sebelum wajib approval |

**Risiko baru yang harus masuk ledger:**

| # | Risiko | Mitigasi |
|---|---|---|
| R9 | Approval kedaluwarsa tapi tetap dieksekusi | TTL + re-simulate + cost delta check (§15.4) |
| R10 | Approver salah_konfigurasi (mis. webhook salah alamat) | `Approver` default **error**, bukan default-allow. Test: konfigurasi approve-tak-sah harus ditolak |
| R11 | Banjir approval request (L1) | `Escalation` + `Cooldown` |
| R12 | `PENDING` plan basi menumpuk di disk | GC plan `PENDING` yang melewati TTL × 2 saat startup |

---

## 16. Ringkasan perubahan setelah riset Data API

| # | Yang berubah | Arah | Verifikasi |
|---|---|---|---|
| 1 | **D1 base library** — `gagliardetto/solana-go` | **Dikonfirmasi** oleh `dbc-go` | Tidak perluputed再看 |
| 2 | **A3 analyzer** | Turun effort, naik nilai bukti | Effort 2 minggu → ~1 minggu; jadi oracle independen untuk S5/S13 |
| 3 | **Rate limit 30 RPS** | Jadi kontrak constructor, bukan opsi | Wajib ada `RateBucket` di `datapi.New` |
| 4 | **Tidak ada endpoint quote maupun eksekusi** | Konfirmasi quote tetap inti SDK | Tidak ada yang bisa十六条我们的 posisi |
| 5 | **Konvensi Go Meteora** | Selaras nama & layout (§12) | Kalau kita menyimpang, itu keputusan sadar — dicatat di sini |
| 6 | **`security: []`** | Data API bukan做为 pembanding; SIGNAL BIASA | Jangan信心kan data API untuk angka yang harus benar sendiri |
