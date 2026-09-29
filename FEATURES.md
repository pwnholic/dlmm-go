# Fitur Lanjutan untuk Go DLMM SDK — Brainstorming

Status: **option set + provisional shortlist** — menunggu frame & prioritas dari user
Tanggal: 2026-09-29
Metode: `brainstorming` (divergent lanes → coverage map → challenge → synthesis)

---

## 0. Ringkas eksekusi

Riset menemukan **lubang nyata** yang tidak terisi SDK TS mana pun, di 6 area. Target pengguna sekarang confirmed: **bot + AI agent** (frame kedua), dan **MCP bukan delivery mechanism** — SDK/library + daemon CLI.

Rekomendasi: **Lane G (agent runtime) sebagai sumbu**, dengan Lane A (analytics) sebagai fondasi data. Alasannya di §5.

Pertanyaan yang akan mengubah urutan: **Q-FRAME** (§8).

---

## 1. Decision frame

Pertanyaan yang dijawab: *"Fitur apa yang harus ada di Go SDK tapi tidak ada di SDK TS, supaya SDK ini bernilai lebih dari sekadar port?"*

Ini questions the wrong level kalau dibiarkan begitu. Pertanyaan yang benar-benar menentukan jawaban:

> **Siapa yang memakai SDK ini, dan pekerjaan apa yang today mereka tidak bisa lakukan?**

Karena jawabannya mem radically mengurutkan seluruh ruang fitur. Contoh konkret — fitur yang sama имеют nilai berbedatotal:

| Kalau targetnya… | Yang paling bernilai | Yang jadi tidakfol |
|---|---|---|
| **Retail LP** yang mau "diam tapi optimal" | simulator + parameter optimizer; analyzer PnL/IL | operator/admin instructions; multi-pair router |
| **Operator bot MM** | autonomous re-centering engine; tx/CU optimizer; RPC pool | PnL report (dia sudah punya) |
| **Protokol integrator** (mis. app Solana) | event stream; safety verifier; operator surface | UI-facing report |

Jadi di bawah ini saya dont shy dari memberi fitur untuk semua frame, tapi **menandai mana yang load-bearing untuk frame mana**.

---

## 2. Bukti: apa yang memang tidak ada (FACT)

Verified against `meteora-dlmm-sdk` @`576919e` + `meteora-zap-sdk` @`9198753`:

| # | Gap | Bukti |
|---|---|---|
| E1 | **Zero** simulasi/backtest/APR/IL analytics | grep `montecarlo\|sharpe\|apr\|impermanent\|annualiz\|backtest\|pnl` di seluruh `ts-client/src/dlmm/` → **0 hit** |
| E2 | 29 event di IDL, tapi TS hanya punya `parseLogs(EventParser, logs)` — caller harus membawa `EventParser` Anchor sendiri | `helpers/index.ts:193` |
| E3 | 27 dari 76 instruction IDL tidak punya wrapper TS | cross-reference IDL ↔ `index.ts` |
| E4 | State machine re-centering LP (`shift_left`/`shift_right`/`check_shift_price_range`) ada di Rust `market_making/` (1.777 LOC) — **tidak ada** di SDK TS manapun | `market_making/src/core.rs:761-923` |
| E5 | Modul ILM di CLI (1.404 LOC): `remove_liquidity_by_price_range`, `seed_liquidity_from_operator`, `seed_liquidity_single_bin_by_operator` — TS cuma punya 2 method tipis (`seedLiquidity`, `seedLiquiditySingleBin`) | `cli/src/instructions/ilm/` |
| E6 | Init by **price range** (`initialize_bin_array_with_price_range`, `initialize_position_with_price_range`) ada di CLI, tidak di TS | `cli/src/instructions/` |
| E7 | Model dynamic fee (`compute_variable_fee(volatility_accumulator)`, `update_volatility_accumulator`) ada di Rust — TS cuma `getDynamicFee` yang baca state, tidak memodel | `commons/src/extensions/lb_pair.rs:22-128` |
| E8 | Oracle: TS punya `wrapOracle` + TWAP interface, tidak ada proyeksi variance / staleness / price-at-T | `helpers/oracle/wrapper.ts` |

**Yang TIDAK boleh diklaim sebagai gap** (sudah ada, jangan diduplikasi): weight/curve/spot/bid-ask strategy (`helpers/rebalance/liquidity_strategy/`), `rebalancePosition` + `simulateRebalancePosition*`, `swapQuote`/`swapQuoteExactOut`, token-2022 partial support, CU estimation + buffer.

### 2b. Lanskap eksternal — sudah ada, jangan dibangun ulang (riset 2026-09-29)

| # | Sudah ada | Sumber / tanggal | Implikasi ke desain |
|---|---|---|---|
| X1 | **Meteora Documentation MCP** (`https://docs.meteora.ag/mcp`) — search + baca docs, termasuk **program references** (account/instruction/event/error) | `docs.meteora.ag/agents/mcp`, diakses 2026-09-29 | **Tidak perlu MCP kita.** Tapi: agent-readable program reference sudah tersedia gratis — jangan bangun ulang |
| X2 | **Meteora Data API publik, keyless** — `dlmm.datapi.meteora.ag`, OHLCV candles, list+detail pool, swap quote | diumumkan 2026-09-28 | **Gunakan, jangan bangun ulang** untuk market screening. Tidak ada: posisi wallet, eksekusi, event history, bin-level data |
| X3 | **Position max length = 1400 bin**, bukan 70. 70 adalah batas *transaksi*. Range lebar dipecah multi-tx; endpoint `quote-liquidity` melaporkan `positionCount` + `transactionCount` | Hummingbot v2.17.0 release notes, 2026-09 | **Bukti pasar bahwa "berapa ini akan costing?" adalah kebutuhan nyata** → G10 (cost pre-flight) naik prioritas |
| X4 | **Helius Parsed Streams keluar dari beta** — decode swap Meteora DLMM native (`meteora_dlmm` di katalog) | 2026-09 | Event decoding jadi commodity di level indexer. SDK tetap butuh decoder sendiri untuk verifikasi lokal/offline |
| X5 | **5+ bot/agent DLMM sudah ada**, semua rebuild primitif yang sama di TypeScript: `ridhofrd/DLMM-auto`, `mgalihpp/my-dlmm-bot`, `romankurnovskii/etemaro`, `brinkgenesis/dlmm`, `pokka-dlmm-bot` | diakses 2026-09-29 | Permintaan **tervalidasi** (bukan spekulatif), tapi pasar penuh. Diferensiasi di kualitas/keamanan, bukan "punya bot" |
| X6 | **Hummingbot v2.17.0anzen "Solana DEX LP Expert"** — CLMM LP autonom di Meteora/Orca/Raydium | 2026-09 | Ada pemain institusional. Gap kita harus spesifik, bukan "LP agent" generik |
| X7 | **Meteora "agent skill files"** dipublikasikan lewat docs MCP | `docs.meteora.ag/agents/mcp` | Kanal distribusi agent resmi sudah ada — tidak perlu duplikat |
| X8 | **Safety layer = kategori produk tersendiri**: MetaMask Agent Wallet (spend limit + allowlist + Guard/Beast), AgentScope (7 lapis on-chain, ASP-1), AgentSafe (Solana, via Solana Actions), OnlyFence | diakses 2026-09-29 | Pola industri sudah terbentuk. Kita harus **patuh**, bukan reinvent |
| X9 | **4 failure mode agent terdokumentasi**: hallucination (alamat/jumlah fiktif), unbounded spending, silent failure, no oversight | `lambdaclass/eth-agent` docs/safety.md | Struktur Lane G (§3) |

**Yang saya TIDAK klaim:** bahwa fitur di atas novel. X5 justru menunjukkan sebaliknya — sudah ada yang membangun sebagian. Diferensiasi kita harus pada **kekurangan yang mereka punya**, bukan pada keberadaan fitur.

---

## 3. Option space — 6 lane independen

Lane dipartisi oleh **mekanisme**, bukan nama fitur. Masing-masing harus berdiri sendiri sebelum di-pollinate.

### Lane A — Intelligence (offline, deterministik, tanpa network)

| ID | Fitur | Mekanisme | Gap yang ditutup |
|---|---|---|---|
| **A1** | **Off-chain AMM simulator** — replikasi bin math DLMM, feed arbitrary synthetic price path, hitung PnL net fee + IL + rounding | reuse `bin`/`math` package yang sudah harus dibangun (P2) → jadi simulator hampir gratis | E1 |
| **A2** | **Strategy/parameter optimizer** — search `(binStep, range width, baseFactor, functionType, collectFeeMode, spot-curve shape)` terhadap profil risiko; Monte-Carlo aturan pull | A1 sebagai oracle → Pareto frontier | E1 |
| **A3** | **Position lifecycle analyzer** — rekonstruksi satu position dari event on-chain (`PositionCreate`→`AddLiquidity`→`Swap2Evt`→`RemoveLiquidity`→`PositionClose` + reward events) → realized fee, IL vs HODL, time-in-range, fee/IL attribution, APR | butuh event decoder (P1) | E1 + E2 |
| **A4** | **Dynamic-fee projector** — Given `volatility_accumulator` dinamika, proyeksi fee horizon | port `compute_variable_fee` (E7) | E7 |

### Lane B — Autonomous execution (state-changing, risiko tinggi)

| ID | Fitur | Mekanisme | Gap |
|---|---|---|---|
| **B1** | **Re-centering engine** — port `shift_left`/`shift_right` dari `market_making/core.rs`, tapi jadi *policy engine*: `Planner` (kebLOC/what-if) + `Executor` (dengan dry-run, idempotency key, rate limit) | E4 | E4 |
| **B2** | **IL-aware continuous rebalancer** — `rebalancePosition` TS itu one-shot. Lanjut: putuskan *kapan* rebalance dengan membandingkan marginal fee vs marginal IL+gas | A1/A2 sebagai model decisively | — |
| **B3** | **Multi-pair portfolio allocator** — alokasi modal lintas pair by expected yield/risk, rebalance atomik 1 tx | B1 + router | — |
| **B4** | **Sandwich-aware executor** — quote → simulate → bundle (Jito) → verifikasi post-state | zap-sdk punya contoh `sendJitoBundle`; tidak ada di DLMM SDK | — |

### Lane C — Safety & trust layer (offline, skeptis)

| ID | Fitur | Mekanisme | Gap |
|---|---|---|---|
| **C1** | **Instruction policy verifier** — decode tx, cek terhadap policy deklaratif: "hanya sentuh mint ini; max slippage X%; tidak ada program asing; tidak ada ATA.close tanpa sisa 0" | decode instruksi (P1) → rule engine | — |
| **C2** | **Conservation checker** — dari hasil `simulate`, verifikasi Σ token delta == yang diharapkan; deteksi drain | log simulate | — |
| **C3** | **State-diff assertion** — sebelum/m sesudah: `active_id`, `liquidity`, reserve tidak boleh bergerak di luar yang Instruksi izinkan | log simulate | — |

### Lane D — Latency & infrastructure (SDS洁 eksploitasi Go)

| ID | Fitur | Mekanisme | Gap |
|---|---|---|---|
| **D1** | **Tx builder** — ALT compression, CU estimation ladder, priority fee strategy | `solana-go` + CU logic | partly ada |
| **D2** | **RPC pool + failover + rate limit** | `solana-go` supports custom transport; Go goroutine | — |
| **D3** | **Websocket subscription manager** — live position/oracle/bin update | `solana-go` ws | — |

### Lane E — Operator & protocol surface

| ID | Fitur | Gap |
|---|---|---|
| **E1** | **27 instruction wrapper yang belum ada** (token badge, reward, preset parameter, pre-activation, fee withdrawal) | E3 |

### Lane F — Oracle & time

| ID | Fitur | Gap |
|---|---|---|
| **F1** | **Oracle projector** — extrapolate observation, variance, price-at-T, staleness alert | E8 |
| **F2** | **Pre-activation pool scheduler** — `set_pre_activation_duration`/`_swap_address`/`set_activation_point` (E3) → auto-deploy saat activation point tercapai | E3 |

### Coverage map — apakah ada blind spot?

Setelah pass pertama, **3 region belum tersentuh**:
1. **Privacy/zero-knowledge** — ada `zap-box` di upstream `solana-go`; DLMM tidak ada hook privacy → kemungkinan besar bukan area DLMM, tapi perlu konfirmasi.
2. **Cross-protocol composability** — DLMM ↔ DAMM v2 ↔ Jupiter router. Zap program adalahPaidVersion dari ini; E-lane Sentuh Tapi router-nya belum.
3. **Governance/permissionless operation** — `set_permissionless_operation_bits`, `enablePositionPermissionlessClaimFee`: siapa boleh apa. Ini surface multi-party, belum digarap.

Region 2 & 3 aku gabung ke E dan tidak jadi candidate sendiri — alasannya di §5.

### Lane G — Agent & bot runtime (SUMBU UTAMA; tanpa MCP)

Struktur lane ini **bukan** dari brainstorming bebas — diturunkan dari 4 failure mode yang terdokumentasi (X9), dipetakan ke primitif Go yang menutupnya. Ini yang membuat desainnya bisa diuji, bukan sekadar ide.

| Failure mode (X9) | Primitif Go yang menutupnya |
|---|---|
| **Hallucination** — LLM mengarang alamat/jumlah | **G1 Address derivation is SDK-only.** SDK turunkan address dari PDA/seed, tidak pernah menerima raw address dari LLM tanpa validasi. Alamat position/binArray **harus** deterministik-derive-able |
| | **G2 Pre-trade validation.** Tiap action divalidasi terhadap policy sebelum sign: mint allowlist, amount dalam limit, decimals cocok, address bukan zero, bukan program ID |
| **Unbounded spending** | **G3 Policy engine (in-process, non-bypassable).** `Policy{ MaxPerTx, MaxPerDay, MaxSlippageBps, AllowedMints, AllowedPrograms, MaxPositionValue }` — dievaluasi di dalam `Apply()`, bukan advice ke pemanggil. Mengikuti pola X8 |
| | **G4 Hard ceiling di layer signing.** Key tidak pernah diekspos ke pemanggil; SDK yang pegang signer. Policy default = deny |
| **Silent failure** | **G5 Exhaustive typed outcome.** Tidak ada parsing string. `type SwapOutcome struct { Kind SwapOutcomeKind; ... }` dengan enum exhausted: `InsufficientLiquidity`, `SlippageWouldFail`, `PriceImpactTooHigh`, `PositionOutOfRange`, `StaleOracle`, `RentTooLow`, … Caller bercabang via type switch |
| | **G6 Idempotency + crash recovery.** Action punya idempotency key; state on-disk (agent bisa mati di tengah rebalance). `Resume(journal)` tahu harus lanjut dari mana |
| **No oversight** | **G7 Plan/Apply split.** `Plan()` → immutable, JSON-serializable `Plan` (dengan hash) → `Apply(plan)`. Pola sudah divalidasi pasar: `meteora-plugin` pakai `--confirm` + `preview: true` |
| | **G8 Decision provenance.** Tiap `Step` membawa `Reason` terstruktur — kenapa rebalance, berapa biaya, alternative apa yang ditolak |

Bot-specific (bukan agent, tapi frame yang sama):

| ID | Fitur | Mekanisme | Gap |
|---|---|---|---|
| **G9** | **Daemon CLI, kontrak output stabil** — JSON di stdout, log ke stderr, exit code bermakna, mode `--dry-run` default | binary tunggal; consumer = script/agent, bukan manusia | X5: semua bot TS butuh Node runtime |
| **G10** | **Cost pre-flight** — `Plan` selalu includi `EstimatedCU`, `EstimatedFeeSOL`, `TransactionCount`, `PositionCount` | X3 = bukti pasar | evidence-driven |
| **G11** | **Atomic multi-step** — zap-out → rebalance → zap-in dalam 1 tx, atau plan yang rollback-able | zap-sdk + D4 | |
| **G12** | **Strategy sebagai policy runtime** — ganti strategi tanpa redeploy (config-driven) | config + interface | |

---

## 4. Kandidat.shortlist (yang layak discussion serius)

Format: `mekanisme · gap · asumsi kritis · upside · downside · test informatif termurah`

### 🥇 A1 — Off-chain AMM simulator
- **Mekanisme:** package `sim` yang memanggil `bin`/`math` yang sudah ada dengan price path sintetis
- **Gap:** E1 — nol iterasi di mana pun
- **Asumsi kritis:** bin math port kita **harus benar** dulu (A2/risiko A3 di DESIGN.md). Kalau simulator pakai math yang salah, hasilnya meyakinkan dan salah — lebih berbahaya dari tidak ada.
- **Upside:** enabler A2, B2, dan plan B1; 100% offline-testable; jadi harness untuk A4
- **Downside:** effort besar; model tidak capture MEV/latency
- **Test termurah:** rekonstruksi 1 swap historis dari event on-chain → simulator harus reproduce `amount_out` **persis**. Kalau cocok untuk N=1000 sample, keyakinan naik drastis.

### 🥈 C1 — Instruction policy verifier
- **Mekanisme:** decode `[]solana.Instruction` → rule engine deklaratif
- **Gap:** keamanan wallet — kelas serangan "tx drains" yang paling nyata untuk user SDK
- **Asumsi kritis:** kita harus bisa decode semua instruction DLMM + SPL + ATA + System +_compute budget dengan benar
- **Upside:** differensiator besar; permitir早期 dry-run; offline; murah dibanding A1
- **Downside:** dipatahin legit policy; harus escape hatch
- **Test termurah:** corpus instruksi dari test TS + `zap-sdk/tests/fixtures` → policy harus detect planted violation. Existential test: 100% recall di fixture yang violated by construction.

### 🥉 B1 — Re-centering engine (dengan Planner/Executor split)
- **Mekanisme:** port `market_making/core.rs`, pisahkan `Plan(ctx, policy) → []Step` dari `Execute`
- **Gap:** E4 — sudah ada bukti kelayakan (1.777 LOC Rust)
- **Asumsi kritis:** `withdraw → swap → deposit` harus atomic atau punya recovery; kalau gagal tengah, position kosong. Butuh A1 untuk menguji "kalau price balik lagi, apakah ini损失的?"
- **Upside:** fitur "diam tapi LP mungkin Optimal" —ornamen besar untuk retail/operator
- **Downside:** **riskaverse tinggi** — menyentuh uang. Wajib dry-run, max-loss cap, kill switch
- **Test termurah:** **replay** — jalankanPlanner terhadap fixture `.bin` offline dengan price path historis, ukur berapa kali rebalance + simulasi PnL vs no-op. Kalau Planner rebalance 500× dan burn gas, latency-nya belum benar.

### A3 — Position lifecycle analyzer
- **Mekanisme:** event stream → full position state machine
- **Gap:** E1 + E2
- **Asumsi kritis:** event coverage cukup (29 event; `Swap2Evt` vs `Swap` ada dua varian → versioning{})
- **Upside:** highest *user-visible* wow (ganti L dengan angka riil); analytics untuk SEMUA frame
- **Downside:** parser event = pekerjaanhal;回头修复 ketika program upgrade
- **Test terminah:** rekonstruksi 1 position yang chuyên dari RPC mainnet → cocok dengan UI Meteora (kalau & accessible). Kalau cocok, keyakinan tinggi.

### B2 — IL-aware continuous rebalancer
- **Asumsi kritis:** B1 (tidak bisa decide rebalance tanpa eksekusi yang handal) + A1 (tanpa model, keputusan berbasis asumsi)
- **Status:** **fase 2**, bukan fase 1. Tergantung A1 + B1.

### Yang saya relegasi (dan kenapa)

- **E1 (27 instruction wrapper)** — nyata sebagai gap, tapi nilainya **hanya** untuk pool operator / protocol integrator. Kalau frame-nya bukan itu, ini 2–3 hari kerja tanpa downside-driven value. → "lengkapi, jangan membuang; jangan jadikan prioritas"
- **Lane D** — infrastruktur; bagus tapi **bukan differentiator** (library lain juga bisa). Tapi **G9/G10 butuh potongan D1** (CU/fee ladder), jadi pull sebagian, bukan seluruh lane
- **Lane F** — F2 (oracle projector) tetap sempit. (F1 dipromosi jadi **G10** oleh bukti X3)
- **Cross-protocol router** — real, tapi itu **project sendiri**, bukan fitur SDK. Payback-nya beda.
- **MCP** — **dikeluarkan atas permintaan user.** X1 sudah memenuhi kebutuhan docs/program-reference
- **Pool scanner sendiri** — **jangan bangun.** Pakai X2 (`dlmm.datapi.meteora.ag`)

---

## 5. Tradeoff & yang mendominasi

| Dimensi | **G1–G8 agent safety** | A1 Simulator | C1 Verifier | B1 Re-centering | A3 Analyzer |
|---|---|---|---|---|---|
| Effort | ~2–3 minggu (G3+G5+G7 duluan) | ~3–4 minggu | ~1 minggu | ~3 minggu | ~2 minggu |
| **Offline-verifiable** | ✅ penuh | ✅ penuh | ✅ penuh | ⚠️ butuh devnet | ✅ penuh |
| Reversibilitas | tinggi | tinggi | tinggi | **rendah** (sentuh dana) | tinggi |
| Unblocks | G9–G12, semua write path | A2, A4, B2, B1-testing | semua write path | B2, B3 | laporan, feedback loop |
| Risk ke user | rendah | rendah | rendah | **tinggi** | rendah |
| Diferensiasi vs X5 (5 bot TS) | **tinggi** — tidak terlihat ada yang punya ini | sedang | tinggi | **rendah** — sudah banyak | tinggi |

**Perubahan yang dipaksakan oleh riset X1–X9:**

1. **B1 turun prioritas.** Lima bot sudah mengimplementasikan primitif yang sama. Kalau kita ship re-centering engine tanpa safety primitives, itu bot ke-6 yang sama.
2. **G naik ke sumbu.** 4 failure mode X9 adalah kebutuhan terstruktur dan bisa ditutup di Go dengan biaya rendah. Dari 5 bot TS di X5, tidak satu pun yang terlihat menutupnya.
3. **G7 bukan pilihan gaya** — sudah divalidasi pasar (X2: `preview: true`, `--confirm`). Tanpa itu, agent akan melakukan hal irreversible.
4. **G10 naik** karena X3.

**Rekomendasi (portfolio, bukan satu):**
```
Foundation   P0-P4 (DESIGN.md) — types, codec, math, pda, client   [tetap]
Minggu 1     G3+G5+G7  policy engine, typed outcome, Plan/Apply    → melindungi semua write path
Minggu 2     A1 simulator                                          → data untuk keputusan
Minggu 3     G1+G2+G8+G9  validation + provenance + daemon CLI     → agent bisa pakai dengan aman
Minggu 4-5   A3 analyzer                                          → user-visible, bagi baris dengan event decoder
Minggu 6-8   B1 re-centering                                      → HANYA setelah G + A1 + dry-run proven
Ditunda      B2, B3, E1, D (selain D1), router, pool scanner       → bukan ditolak, ditunda
```

**Kenapa urut ini:** G3/G5/G7 tidak butuh satu pun fitur lain untuk deliver value, dan melindungi setiap baris yang menyusul. A1 butuh `math` yang sudah jadi (P2). B1 di akhir karena mengombinasikan G + A1 dan butuh dry-run terbukti.

---

## 6. UNKNOWN & disconfirming evidence

| # | Unknown / Risiko | Mengapa penting | Discriminator |
|---|---|---|---|
| **U1** | **Tidak ada SVM Go yang mature** (probe: `gagliardetto/svm`, `portto`, `_Chapter-Entropy` → semua tidak ada). Riset saya berbasis tebakan nama, belum exhaustif | menentukan apakah verifikasi bisa offline atau wajib devnet | `go list -m -versions` sweep lewat `pkg.go.dev/search`, atau `github search`. **Ini blockerLevel untuk kalender P5** |
| **U2** | Python client `python-client/dlmm/` — mungkin punya fitur yang tidak di TS dan tidak saya lihat | bisa jadi sudah ada solver/optimizer | baca `python-client/dlmm/dlmm/*.py` (2.1k LOC) |
| **U3** | Apakah user Community sudah membangun MM bot di atas market_making | blueprint + realistis | search GitHub `MeteoraAg/dlmm-sdk market_making` |
| **U4** | Kessler: program bisa upgrade → codec & event bisa stale |famously relevant |-version IDL; `version` field in LbPair struct |
| **U5** | A1 bisa jadi **menyesatkan** kalau math port salah | simulator yang salah = confident wrong | cross-check vs `math/big` + event rekonstruksi (test di A1) |
| **U6** | Regulatory/maintenance: apakah user ini FAYA_MM/bot-runner? | menentukan seluruh frame | Q-FRAME |

**Tidak saya klaim:** bahwa fitur di atas "novel" atau "belum pernah ada di mana pun". Yang bisa saya buktikan hanya **tidak ada di repo Meteora yang dipindai** (E1–E8). Kemungkinan ada implementasi pihak ketiga.

---

## 7. Next experiment (sebelum eksekusi)

Sebelum P0, saya sarankan 3 validasi murah yang mengubah **kapan/kita prioritise**:

| # | Experiment | Cheap? | Mengubah apa |
|---|---|---|---|
| **X1** | Sweep Go SVM (U1) | ~30 menit, read-only | menentukan apakah verifikasi write-path bisa offline |
| **X2** | Baca `python-client/dlmm/` (U2) | ~1 jam, read-only | mungkin menemukan fitur yang saya laporkan "tidak ada" |
| **X3** | Rekonstruksi 1 swap historis dari event on-chain vs `bin` math | ~2 jam, butuh RPC read | **gate A1**: kalau tidak cocok, A1 bukan fondasi, dan strateginya berubah total |

---

## 8. Handoff

Brainstorming selesai sebagai **provisional shortlist + portfolio + unresolved frame**.
Belum ada yang divalidasi — G-lane dan A1 adalah **hypothesis**, bukan fakta.

Yang saya butuhkan untuk lanjut:
1. **Q-FRAME** — bot kedaulahan penuh, atau agent dengan human-in-the-loop? (menentukan G3 default dan G4)
2. **X1–X3** setuju dijalankan sebelum P0, atau langsung P0?
3. Mana yang **bukan** masuk
4. Kalau G: **output contract** — JSON schema-nya siapa yang konsumsi? (agent lain, script/bash, atau UI kamu sendiri?)

Kalau frame sudah jelas, handoff ke `software-engineer` untuk design implementation-ready (dengan `verification` playbook untuk—oracle A3/X3).
