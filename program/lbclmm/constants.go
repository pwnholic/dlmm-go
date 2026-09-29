package lbclmm

import "github.com/pwnholic/dlmm-go/num"

// Program constants, transcribed from the `constants` section of
// idls/dlmm.json (IDL version 0.12.0).
//
// Every value here is checked against the IDL by tools/check_idl_constants.py,
// which runs under `make generate-check`. Nothing is inferred, rounded, or
// derived: if the IDL says a value, this file says the same value, and the
// checker fails the build when they diverge.
const (
	// MaxBinPerArray is the number of bins stored in one bin array account.
	// It is also the divisor used to convert a bin ID into a bin array index.
	MaxBinPerArray = 70

	// DefaultBinPerPosition is the number of bins a position covers by default.
	// A position may be widened beyond this; the hard cap is PositionMaxLength.
	DefaultBinPerPosition = 70

	// PositionMaxLength is the largest number of bins one position may cover.
	//
	// The 70-bin figure often quoted is a transaction-size limit, not a position
	// limit: a wider position is created and funded across several transactions.
	PositionMaxLength = 1400

	// BinArrayBitmapSize is the number of bin array indexes tracked by one word
	// of the pool's default bitmap. It is not a bin count.
	BinArrayBitmapSize = 512

	// ExtensionBinArrayBitmapSize is the number of extra bitmap words available
	// on each side through the extension account.
	ExtensionBinArrayBitmapSize = 12

	// MaxBinIDPerBinStep bounds how far the active bin may sit from zero.
	MaxBinIDPerBinStep = 351_639

	// MaxBinStep is the largest bin step a pool can be created with, in basis
	// points.
	MaxBinStep = 400

	// BasisPointMax is the denominator for basis-point values (100%).
	BasisPointMax = 10_000

	// FeeDenominator is the denominator for fee-rate values.
	FeeDenominator = 1_000_000_000

	// NumRewards is the number of reward slots per pool.
	NumRewards = 2

	// MaxBinPerLimitOrder is the number of bins a single limit order may span.
	MaxBinPerLimitOrder = 50

	// MaxRewardBinSplit is the maximum number of bins a reward distribution may
	// be split across.
	MaxRewardBinSplit = 15

	// MaxResizeLength is the largest single position-length change allowed by
	// one increase or decrease instruction.
	MaxResizeLength = 91

	// MinRewardDuration is the shortest permitted reward emission period, in
	// seconds.
	MinRewardDuration = 1

	// MaxRewardDuration is the longest permitted reward emission period, in
	// seconds (365 days).
	MaxRewardDuration = 31_536_000

	// ProtocolShareDefault is the default share of swap fees taken by the
	// protocol, in basis points.
	ProtocolShareDefault = 500

	// MaxProtocolShare is the largest protocol share a pool may be configured
	// with, in basis points.
	MaxProtocolShare = 2_500

	// HostFeeBps is the share of the protocol fee routed to the host, in basis
	// points.
	HostFeeBps = 2_000

	// ILMProtocolShare is the protocol share applied to customizable
	// permissionless (ILM) pools, in basis points.
	ILMProtocolShare = 2_000

	// LimitOrderFeeShare is the share of the fee credited to a limit order
	// position, in basis points.
	LimitOrderFeeShare = 5_000
)

// MaxBaseFee and MinBaseFee bound the base fee rate. They are u128 in the IDL,
// so they are represented as num.U128 rather than narrowed to a Go integer.
var (
	// MaxBaseFee is the largest base fee rate a pool may use.
	MaxBaseFee = num.U128FromU64(100_000_000)

	// MinBaseFee is the smallest base fee rate a pool may use.
	MinBaseFee = num.U128FromU64(100_000)

	// MaxFeeRate is the largest fee rate the fee logic accepts.
	MaxFeeRate = num.U128FromU64(100_000_000)

	// MinimumLiquidity is the smallest liquidity amount the program will accept
	// into a bin, guarding against dust positions.
	MinimumLiquidity = num.U128FromU64(1_000_000)
)

// Bin ID bounds.
//
// These come from commons/src/constants.rs, NOT from the IDL. The IDL's
// MaxBinIDPerBinStep (351639) is a different quantity: it is documented as
// "Maximum bin ID per bin step. Computed based on 1 bps. Used for bin id bound
// estimation", whereas the program enforces this pair directly against
// next_active_bin_id in extensions/lb_pair.rs.
//
// Using MaxBinIDPerBinStep as the addressing bound was a real defect: it rejects
// legitimate bin IDs in (351639, 443636].
//
// tools/check_idl_constants.py verifies these against the Rust constants file.
const (
	// MinBinID is the lowest bin ID the program supports, computed at 1 bps.
	MinBinID = -443_636

	// MaxBinID is the highest bin ID the program supports, computed at 1 bps.
	MaxBinID = 443_636
)

// Bin array bitmap index bounds.
//
// The pool account carries a fixed-size bitmap covering BinArrayBitmapSize bin
// array indexes on each side of zero; anything outside that needs the extension
// account. These are derived here rather than inline so that the boundary is
// stated once, with its provenance.
const (
	// MinBinArrayIndexInDefaultBitmap is the lowest bin array index the pool's
	// own bitmap can represent.
	MinBinArrayIndexInDefaultBitmap = -BinArrayBitmapSize

	// MaxBinArrayIndexInDefaultBitmap is the highest bin array index the pool's
	// own bitmap can represent.
	MaxBinArrayIndexInDefaultBitmap = BinArrayBitmapSize - 1

	// MinBinArrayIndexWithExtension is the lowest bin array index representable
	// once the extension account is present.
	MinBinArrayIndexWithExtension = -BinArrayBitmapSize * (ExtensionBinArrayBitmapSize + 1)

	// MaxBinArrayIndexWithExtension is the highest bin array index representable
	// once the extension account is present.
	MaxBinArrayIndexWithExtension = BinArrayBitmapSize*(ExtensionBinArrayBitmapSize+1) - 1
)
