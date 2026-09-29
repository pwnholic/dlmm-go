// Package pda derives the program-derived addresses used by the Meteora DLMM
// program.
//
// Seeds are taken from the Rust reference in
// dlmm-sdk/commons/src/{seeds.rs,pda.rs} rather than inferred from the
// TypeScript client, because the seeds are part of the on-chain contract: a
// wrong seed produces a valid-looking address that simply does not exist,
// which surfaces much later as an empty account.
//
// This package performs no I/O and holds no state. Every function takes the
// program ID explicitly instead of defaulting to a cluster, so a process can
// derive addresses for mainnet and devnet at the same time without a global.
package pda

import (
	"bytes"
	"encoding/binary"
	"fmt"

	solana "github.com/gagliardetto/solana-go"
)

// Seed literals, byte-for-byte as declared in seed.rs.
const (
	seedBinArray                 = "bin_array"
	seedOracle                   = "oracle"
	seedBinArrayBitmap           = "bitmap"
	seedPresetParameter          = "preset_parameter"
	seedPresetParameter2         = "preset_parameter2"
	seedPosition                 = "position"
	seedTokenBadge               = "token_badge"
	seedClaimProtocolFeeOperator = "cf_operator"
	seedOperator                 = "operator"
	seedEventAuthority           = "__event_authority"
)

// ilmBaseKey is the constant base key used for customizable permissionless
// pools. It is a literal program constant, not a derived address.
//
// It is unexported and reached through ILMBaseKey() so that no caller can
// reassign it and silently change address derivation for the whole process.
var ilmBaseKey = solana.MustPublicKeyFromBase58("MFGQxwAmB91SwuYX36okv2Qmdc9aMuHTwWGUrp4AtB1")

// ILMBaseKey returns the base key used to derive customizable permissionless
// pool addresses.
//
// A PublicKey is a fixed-size array, so the returned value is a copy: mutating
// it leaves the package constant untouched.
func ILMBaseKey() solana.PublicKey { return ilmBaseKey }

// derive runs find_program_address and normalises its error.
//
// find_program_address fails only when no valid bump exists in 0..=255, which
// means the seeds themselves cannot produce a PDA. Returning the error keeps
// that observable; discarding it would turn an impossible-looking situation
// into a silently wrong address.
func derive(programID solana.PublicKey, parts ...[]byte) (solana.PublicKey, uint8, error) {
	addr, bump, err := solana.FindProgramAddress(parts, programID)
	if err != nil {
		return solana.PublicKey{}, 0, fmt.Errorf("pda: deriving address: %w", err)
	}

	return addr, bump, nil
}

// u16LE, u32LE and u64LE encode a scalar the way Rust's to_le_bytes does.
func u16LE(v uint16) []byte {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, v)

	return b
}

func u32LE(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)

	return b
}

func u64LE(v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)

	return b
}

// i32LE encodes a signed 32-bit value in two's complement, little-endian.
//
// The signed-to-unsigned conversion is the definition of two's complement
// encoding, not an accident of narrowing.
func i32LE(v int32) []byte {
	//nolint:gosec // G115: two's complement encoding is the intent
	return u32LE(uint32(v))
}

// i64LE encodes a signed 64-bit value in two's complement, little-endian.
func i64LE(v int64) []byte {
	//nolint:gosec // G115: two's complement encoding is the intent
	return u64LE(uint64(v))
}

// orderedMints returns the two mints in ascending byte order.
//
// The on-chain programs order the pair by lexicographic comparison of the raw
// 32 bytes, not by base58 string. Go arrays are not ordered with <, so this
// compares the underlying bytes explicitly. Getting this backwards yields a
// different, valid-looking pool address for the same pair, so it is worth the
// explicit helper rather than an inlined comparison.
func orderedMints(a, b solana.PublicKey) (lo, hi solana.PublicKey) {
	if bytes.Compare(a[:], b[:]) <= 0 {
		return a, b
	}

	return b, a
}

// LbPair derives the pool address for the original layout:
// seeds = [min_mint, max_mint, bin_step].
//
// Deprecated on-chain in favour of LbPairV2, which also commits to the base
// factor. It is retained because pools created with the original layout still
// exist and must remain addressable.
func LbPair(programID, tokenX, tokenY solana.PublicKey, binStep uint16) (solana.PublicKey, uint8, error) {
	lo, hi := orderedMints(tokenX, tokenY)
	return derive(programID, lo[:], hi[:], u16LE(binStep))
}

// LbPairV2 derives the pool address for the current layout:
// seeds = [min_mint, max_mint, bin_step, base_factor].
func LbPairV2(programID, tokenX, tokenY solana.PublicKey, binStep, baseFactor uint16) (solana.PublicKey, uint8, error) {
	lo, hi := orderedMints(tokenX, tokenY)
	return derive(programID, lo[:], hi[:], u16LE(binStep), u16LE(baseFactor))
}

// LbPairWithPreset derives a pool address anchored to a preset parameter
// account: seeds = [preset_parameter, min_mint, max_mint].
func LbPairWithPreset(programID, presetParameter, tokenX, tokenY solana.PublicKey) (solana.PublicKey, uint8, error) {
	lo, hi := orderedMints(tokenX, tokenY)
	return derive(programID, presetParameter[:], lo[:], hi[:])
}

// CustomizablePermissionlessLbPair derives an ILM pool address:
// seeds = [ILM_BASE_KEY, min_mint, max_mint].
func CustomizablePermissionlessLbPair(programID, tokenX, tokenY solana.PublicKey) (solana.PublicKey, uint8, error) {
	lo, hi := orderedMints(tokenX, tokenY)
	return derive(programID, ilmBaseKey[:], lo[:], hi[:])
}

// PermissionLbPair derives a permissioned pool address:
// seeds = [base, min_mint, max_mint, bin_step].
func PermissionLbPair(programID, base, tokenX, tokenY solana.PublicKey, binStep uint16) (solana.PublicKey, uint8, error) {
	lo, hi := orderedMints(tokenX, tokenY)
	return derive(programID, base[:], lo[:], hi[:], u16LE(binStep))
}

// Position derives a position address:
// seeds = [POSITION, lb_pair, base, lower_bin_id, width].
//
// The two integers are written as signed 32-bit little-endian, matching
// lower_bin_id.to_le_bytes() on an i32. A negative lower bin ID is normal.
func Position(programID, lbPair, base solana.PublicKey, lowerBinID, width int32) (solana.PublicKey, uint8, error) {
	return derive(programID, []byte(seedPosition), lbPair[:], base[:], i32LE(lowerBinID), i32LE(width))
}

// Oracle derives the oracle for a pool: seeds = [ORACLE, lb_pair].
func Oracle(programID, lbPair solana.PublicKey) (solana.PublicKey, uint8, error) {
	return derive(programID, []byte(seedOracle), lbPair[:])
}

// BinArray derives a bin array: seeds = [BIN_ARRAY, lb_pair, bin_array_index].
//
// The index is written as a signed 64-bit little-endian value, matching
// bin_array_index.to_le_bytes() on an i64. The on-chain seed is i64 even though
// the index is derived from an i32 bin ID, so widening here is required rather
// than incidental.
func BinArray(programID, lbPair solana.PublicKey, binArrayIndex int64) (solana.PublicKey, uint8, error) {
	return derive(programID, []byte(seedBinArray), lbPair[:], i64LE(binArrayIndex))
}

// BinArrayBitmapExtension derives the bitmap extension account:
// seeds = [bitmap, lb_pair].
func BinArrayBitmapExtension(programID, lbPair solana.PublicKey) (solana.PublicKey, uint8, error) {
	return derive(programID, []byte(seedBinArrayBitmap), lbPair[:])
}

// Reserve derives a reserve: seeds = [lb_pair, token_mint].
func Reserve(programID, tokenMint, lbPair solana.PublicKey) (solana.PublicKey, uint8, error) {
	return derive(programID, lbPair[:], tokenMint[:])
}

// RewardVault derives a reward vault: seeds = [lb_pair, reward_index].
func RewardVault(programID, lbPair solana.PublicKey, rewardIndex uint64) (solana.PublicKey, uint8, error) {
	return derive(programID, lbPair[:], u64LE(rewardIndex))
}

// EventAuthority derives the program's event authority. The seed is not
// namespaced by anything, so the result is a single well-known address per
// program ID.
func EventAuthority(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return derive(programID, []byte(seedEventAuthority))
}

// PresetParameter derives a preset parameter account:
// seeds = [preset_parameter, bin_step].
//
// Deprecated on-chain in favour of PresetParameter2.
func PresetParameter(programID solana.PublicKey, binStep uint16) (solana.PublicKey, uint8, error) {
	return derive(programID, []byte(seedPresetParameter), u16LE(binStep))
}

// PresetParameter2 derives a preset parameter account:
// seeds = [preset_parameter, bin_step, base_factor].
func PresetParameter2(programID solana.PublicKey, binStep, baseFactor uint16) (solana.PublicKey, uint8, error) {
	return derive(programID, []byte(seedPresetParameter), u16LE(binStep), u16LE(baseFactor))
}

// PresetParameter2ByIndex derives a v2 preset parameter account by its index:
// seeds = [preset_parameter2, index].
func PresetParameter2ByIndex(programID solana.PublicKey, index uint16) (solana.PublicKey, uint8, error) {
	return derive(programID, []byte(seedPresetParameter2), u16LE(index))
}

// TokenBadge derives a token badge: seeds = [token_badge, mint].
func TokenBadge(programID, mint solana.PublicKey) (solana.PublicKey, uint8, error) {
	return derive(programID, []byte(seedTokenBadge), mint[:])
}

// ClaimProtocolFeeOperator derives a protocol-fee operator account:
// seeds = [cf_operator, operator].
func ClaimProtocolFeeOperator(programID, operator solana.PublicKey) (solana.PublicKey, uint8, error) {
	return derive(programID, []byte(seedClaimProtocolFeeOperator), operator[:])
}

// Operator derives a whitelisted operator account:
// seeds = [operator, whitelisted_signer].
func Operator(programID, whitelistedSigner solana.PublicKey) (solana.PublicKey, uint8, error) {
	return derive(programID, []byte(seedOperator), whitelistedSigner[:])
}
