package lbclmm

import "bytes"

// AccountKind identifies one of the program's account types.
//
// The zero value is deliberately invalid: an unrecognised discriminator must
// not be reported as a valid account kind.
type AccountKind uint8

// Account kinds, in the order the IDL lists them.
const (
	AccountUnknown AccountKind = iota
	AccountBinArray
	AccountBinArrayBitmapExtension
	AccountClaimFeeOperator
	AccountDummyZcAccount
	AccountLbPair
	AccountLimitOrder
	AccountOperator
	AccountOracle
	AccountPositionV2
	AccountPresetParameter
	AccountPresetParameter2
	AccountTokenBadge
)

// accountKindNames maps each kind to the account name used in the IDL.
var accountKindNames = map[AccountKind]string{
	AccountBinArray:                "BinArray",
	AccountBinArrayBitmapExtension: "BinArrayBitmapExtension",
	AccountClaimFeeOperator:        "ClaimFeeOperator",
	AccountDummyZcAccount:          "DummyZcAccount",
	AccountLbPair:                  "LbPair",
	AccountLimitOrder:              "LimitOrder",
	AccountOperator:                "Operator",
	AccountOracle:                  "Oracle",
	AccountPositionV2:              "PositionV2",
	AccountPresetParameter:         "PresetParameter",
	AccountPresetParameter2:        "PresetParameter2",
	AccountTokenBadge:              "TokenBadge",
}

// accountDiscriminators maps each kind to the 8-byte account discriminator.
//
// These are emitted from idls/dlmm.json and checked against it by
// tools/check_idl_constants.py. They are what makes an account identifiable at
// all: the on-chain program writes them as the first eight bytes of every
// account, so a decoder must compare them before trusting any field.
var accountDiscriminators = map[AccountKind][8]byte{
	AccountBinArray:                {92, 142, 92, 220, 5, 148, 70, 181},
	AccountBinArrayBitmapExtension: {80, 111, 124, 113, 55, 237, 18, 5},
	AccountClaimFeeOperator:        {166, 48, 134, 86, 34, 200, 188, 150},
	AccountDummyZcAccount:          {94, 107, 238, 80, 208, 48, 180, 8},
	AccountLbPair:                  {33, 11, 49, 98, 181, 101, 177, 13},
	AccountLimitOrder:              {137, 183, 212, 91, 115, 29, 141, 227},
	AccountOperator:                {219, 31, 188, 145, 69, 139, 204, 117},
	AccountOracle:                  {139, 194, 131, 179, 140, 179, 229, 244},
	AccountPositionV2:              {117, 176, 212, 199, 245, 180, 133, 182},
	AccountPresetParameter:         {242, 62, 244, 34, 181, 112, 58, 170},
	AccountPresetParameter2:        {171, 236, 148, 115, 162, 113, 222, 174},
	AccountTokenBadge:              {116, 219, 204, 229, 249, 116, 255, 150},
}

// AccountDiscriminatorLen is the number of leading bytes every account carries.
const AccountDiscriminatorLen = 8

// String returns the IDL account name, or "unknown".
func (k AccountKind) String() string {
	if name, ok := accountKindNames[k]; ok {
		return name
	}

	return "unknown"
}

// Discriminator returns the 8-byte discriminator for the kind.
//
// The second result is false for AccountUnknown, so a caller cannot
// accidentally compare against eight zero bytes.
func (k AccountKind) Discriminator() ([8]byte, bool) {
	disc, ok := accountDiscriminators[k]
	return disc, ok
}

// KindOf identifies the account kind from raw account data.
//
// The second result is false when the data is too short to hold a
// discriminator or matches none of them, which is the normal outcome for an
// account belonging to some other program. Callers must check it before reading
// any field: treating an unknown account as an LbPair would interpret unrelated
// bytes as pool state.
func KindOf(data []byte) (AccountKind, bool) {
	if len(data) < AccountDiscriminatorLen {
		return AccountUnknown, false
	}

	for kind, disc := range accountDiscriminators {
		if bytes.Equal(data[:AccountDiscriminatorLen], disc[:]) {
			return kind, true
		}
	}

	return AccountUnknown, false
}
