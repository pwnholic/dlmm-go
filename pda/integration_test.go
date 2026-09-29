//go:build integration

// Integration tests that compare derived addresses against real on-chain state.
//
// They live in the external test package pda_test so that importing
// solana-go/rpc here does not add an RPC dependency to package pda itself. The
// layering rule in ARCHITECTURE.md P3 constrains the production dependency
// graph; a test that talks to a cluster is a consumer of it, not part of it.
//
// Run with:
//
//	set -a; . ./.envrc; set +a
//	go test -tags=integration -count=1 -v ./pda/
//
// The test skips rather than fails when credentials are absent, so the default
// offline suite stays offline.
package pda_test

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	solana "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/pwnholic/dlmm-go/pda"
	"github.com/pwnholic/dlmm-go/program/lbclmm"
)

const (
	dataAPIBase = "https://dlmm.datapi.meteora.ag"
	// A Helius free-tier key is rate limited. The test makes one RPC call per
	// pool, so the sample is kept modest and every call is retried on 429 with
	// backoff. Raising this without raising the delay just trades coverage for
	// flakes; a real regression guard wants a small, reliable sample plus a
	// separate, deliberately paced integration run.
	poolSample  = 12
	rpcMinGap   = 120 * time.Millisecond
	rpcMaxTries = 5
)

// apiPools is the subset of the pools endpoint this test needs. Every field is
// one the derivation depends on; nothing is inferred.
type apiPools struct {
	Data []struct {
		Address string `json:"address"`
		TokenX  struct {
			Address string `json:"address"`
		} `json:"token_x"`
		TokenY struct {
			Address string `json:"address"`
		} `json:"token_y"`
		PoolConfig struct {
			BinStep uint16 `json:"bin_step"`
		} `json:"pool_config"`
		ReserveX string `json:"reserve_x"`
		ReserveY string `json:"reserve_y"`
	} `json:"data"`
}

func fetchPools(t *testing.T) apiPools {
	t.Helper()

	url := fmt.Sprintf("%s/pools?page_size=%d", dataAPIBase, poolSample)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("accept", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		t.Skipf("cannot reach the Meteora data API (%v); skipping", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Skipf("data API returned %s; skipping", resp.Status)
	}

	var out apiPools
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding pools: %v", err)
	}
	if len(out.Data) == 0 {
		t.Fatal("data API returned no pools; the test would prove nothing")
	}

	return out
}

func mustKey(t *testing.T, s string) solana.PublicKey {
	t.Helper()

	k, err := solana.PublicKeyFromBase58(s)
	if err != nil {
		t.Fatalf("bad key %q: %v", s, err)
	}
	return k
}

// TestDerivedAddressesMatchRealPools checks three derivations against the chain.
//
// What each part can and cannot prove:
//
//   - Reserve: seeds are [lb_pair, token_mint], so the data API's reserve_x and
//     reserve_y are a direct, unambiguous oracle for the seed order.
//   - The LbPair discriminator: proves the address holds a pool account, which
//     validates the address before any field is read from it.
//   - The pool address itself: several creation paths exist, each with its own
//     seed set, so every candidate is tried and the tally is reported. A
//     candidate that never matches anywhere is a fact about the seed set, not
//     noise, and the tally is what makes it visible.
func TestDerivedAddressesMatchRealPools(t *testing.T) {
	pools := fetchPools(t)

	apiKey := os.Getenv("HELIUS_API_KEY")
	if apiKey == "" {
		t.Skip("HELIUS_API_KEY is not set; run with: set -a; . ./.envrc; set +a")
	}

	client := rpc.New("https://mainnet.helius-rpc.com/?api-key=" + apiKey)

	// Pace the RPC calls. A rate-limited provider returning 429 is a provider
	// limit, not a defect in this SDK, so a sample that cannot be completed is
	// skipped rather than failed.
	var lastRPC time.Time

	var (
		reservesChecked int
		discChecked     int
		matched         = map[string]int{}
		unmatched       []string
		skipped         int
	)

	for _, p := range pools.Data {
		pool := mustKey(t, p.Address)
		mintX := mustKey(t, p.TokenX.Address)
		mintY := mustKey(t, p.TokenY.Address)

		t.Run(p.Address[:8], func(t *testing.T) {
			// 1. Reserves.
			for _, side := range []struct {
				name string
				mint solana.PublicKey
				want string
			}{
				{"reserve_x", mintX, p.ReserveX},
				{"reserve_y", mintY, p.ReserveY},
			} {
				got, _, err := pda.Reserve(lbclmm.ProgramIDMainnet, side.mint, pool)
				if err != nil {
					t.Fatalf("%s: %v", side.name, err)
				}
				if got.String() != side.want {
					t.Errorf("%s: derived %s, chain has %s", side.name, got, side.want)
				}
				reservesChecked++
			}

			// 2. Confirm the account is an LbPair before trusting its bytes.
			// Space out RPC calls and back off on 429, then give up gracefully:
			// a rate limit says nothing about the correctness of the decode.
			var (
				info *rpc.GetAccountInfoResult
				err  error
			)
			for attempt := range rpcMaxTries {
				if gap := rpcMinGap - time.Since(lastRPC); gap > 0 {
					time.Sleep(gap)
				}

				info, err = client.GetAccountInfoWithOpts(t.Context(), pool, &rpc.GetAccountInfoOpts{
					Commitment: rpc.CommitmentConfirmed,
				})
				lastRPC = time.Now()

				if err == nil {
					break
				}
				if !strings.Contains(err.Error(), "429") || attempt == rpcMaxTries-1 {
					t.Skipf("RPC unavailable for %s (%v); skipping rather than failing on a provider limit", pool, err)
				}
				time.Sleep(time.Duration(attempt+1) * rpcMinGap * 4)
			}
			if err != nil {
				t.Skipf("RPC unavailable for %s; skipping", pool)
			}
			if info == nil || info.Value == nil {
				// The pool is listed but the account is not readable (e.g. a
				// node that has not indexed it, or a pruned slot). That is a
				// provider condition, not a decode regression.
				skipped++
				t.Skipf("pool account %s not readable; skipping", pool)
			}

			data := info.Value.Data.GetBinary()

			kind, ok := lbclmm.KindOf(data)
			if !ok {
				t.Fatalf("account %s carries no known discriminator", pool)
			}
			if kind != lbclmm.AccountLbPair {
				t.Fatalf("account %s is a %s, not an LbPair", pool, kind)
			}
			discChecked++

			// The pool's creation seeds live in their own fields, separate from
			// the live bin_step and parameters.base_factor, because the live
			// values are operator-mutable (update_base_fee_parameters) while the
			// seeds are fixed at creation.
			//
			// Offsets come from walking the IDL field tree with Borsh
			// accumulation (StaticParameters and VariableParameters are each 32
			// bytes, which is what an earlier hand-computation got wrong).
			const (
				pairTypeOffset       = 75
				binStepSeedOffset    = 73
				baseFactorSeedOffset = 84
				baseKeyOffset        = 784
			)
			if len(data) < baseKeyOffset+32 {
				t.Fatal("pool account too short to hold the creation fields")
			}

			binStepSeed := binary.LittleEndian.Uint16(data[binStepSeedOffset:])
			baseFactorSeed := binary.LittleEndian.Uint16(data[baseFactorSeedOffset:])
			liveBaseFactor := binary.LittleEndian.Uint16(data[lbclmm.AccountDiscriminatorLen:])
			pairType := data[pairTypeOffset]
			baseKey := solana.PublicKeyFromBytes(data[baseKeyOffset : baseKeyOffset+32])

			t.Logf("pair_type=%d base_key=%s", pairType, baseKey)
			t.Logf("bin_step_seed=%d base_factor_seed=%d | live bin_step=%d base_factor=%d require_seed=%d",
				binStepSeed, baseFactorSeed, p.PoolConfig.BinStep, liveBaseFactor, data[baseFactorSeedOffset-1])

			// 3. Try every seed set a permissionless pool can use.
			candidates := []struct {
				name string
				fn   func() (solana.PublicKey, uint8, error)
			}{
				{"LbPair", func() (solana.PublicKey, uint8, error) {
					return pda.LbPair(lbclmm.ProgramIDMainnet, mintX, mintY, binStepSeed)
				}},
				{"LbPairV2(live base_factor)", func() (solana.PublicKey, uint8, error) {
					return pda.LbPairV2(lbclmm.ProgramIDMainnet, mintX, mintY, binStepSeed, liveBaseFactor)
				}},
				{"LbPairV2(creation seeds)", func() (solana.PublicKey, uint8, error) {
					return pda.LbPairV2(lbclmm.ProgramIDMainnet, mintX, mintY, binStepSeed, baseFactorSeed)
				}},
				// These two commit to a base key rather than to a bin step,
				// which is why trying only the bin-step forms left real pools
				// unexplained. pair_type selects the branch.
				{"LbPairWithPreset(base_key)", func() (solana.PublicKey, uint8, error) {
					return pda.LbPairWithPreset(lbclmm.ProgramIDMainnet, baseKey, mintX, mintY)
				}},
				{"PermissionLbPair(base_key)", func() (solana.PublicKey, uint8, error) {
					return pda.PermissionLbPair(lbclmm.ProgramIDMainnet, baseKey, mintX, mintY, binStepSeed)
				}},
				{"CustomizablePermissionlessLbPair", func() (solana.PublicKey, uint8, error) {
					return pda.CustomizablePermissionlessLbPair(lbclmm.ProgramIDMainnet, mintX, mintY)
				}},
			}

			derived := make([]string, 0, len(candidates))
			for _, c := range candidates {
				addr, _, err := c.fn()
				if err != nil {
					t.Fatalf("deriving %s: %v", c.name, err)
				}
				if addr == pool {
					matched[c.name]++
					t.Logf("reproduced by %s", c.name)
					return
				}
				derived = append(derived, c.name+" -> "+addr.String())
			}

			unmatched = append(unmatched, pool.String())
			t.Logf("not reproduced %s:\n    %s", pool, strings.Join(derived, "\n    "))
		})
	}

	for _, name := range []string{
		"LbPair",
		"LbPairV2(live base_factor)",
		"LbPairV2(creation seeds)",
		"LbPairWithPreset(base_key)",
		"PermissionLbPair(base_key)",
		"CustomizablePermissionlessLbPair",
	} {
		t.Logf("reproduced by %-32s %d", name, matched[name])
	}
	t.Logf("pools %d | reserves checked %d | discriminators confirmed %d | "+
		"unmatched %d | skipped %d",
		len(pools.Data), reservesChecked, discChecked, len(unmatched), skipped)

	// The reserves and discriminators are fully verified: assert on them.
	if reservesChecked == 0 || discChecked == 0 {
		t.Fatal("no address was actually verified")
	}

	// Pool-address reproduction is asserted, not merely reported.
	//
	// pair_type selects the seed layout: 0 is the plain or v2 form that commits
	// to a bin step, while 3 is the customizable-permissionless form that
	// commits to a preset parameter held in base_key. Trying only the bin-step
	// forms left five real pools unexplained until base_key was read at its
	// offset. Every sampled pool must now reproduce under exactly one candidate,
	// so a regression in any derivation fails here rather than being reported
	// and tolerated.
	if len(unmatched) > 0 {
		t.Errorf("%d of %d pool addresses were not reproduced by any candidate "+
			"derivation; a derivation or a field offset has regressed",
			len(unmatched), len(pools.Data))
	}
}
