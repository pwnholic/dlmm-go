// Package lbclmm holds the generated Go bindings for the Meteora DLMM program
// (the on-chain program is named lb_clmm).
//
// Everything in this package is derived from idls/dlmm.json. Nothing here is
// hand-maintained logic: constants, account layouts, instruction encoders and
// error codes are all emitted from the IDL so that a program upgrade becomes a
// reviewable diff rather than a silent divergence.
package lbclmm

import (
	"fmt"

	solana "github.com/gagliardetto/solana-go"
)

// IDLVersion is the version recorded in the IDL metadata. The bindings are only
// valid for this revision of the program interface, so a mismatch is a signal to
// regenerate rather than to patch a value by hand.
const IDLVersion = "0.12.0"

// Program IDs, transcribed from the IDL address field and from
// LBCLMM_PROGRAM_IDS in the TypeScript client.
//
// mainnet-beta and devnet share one address; localhost is a distinct deployment,
// so the three are listed separately rather than collapsed into one "current"
// constant. There is deliberately no default: a process may hold mainnet and
// devnet state at once, and a silently chosen default is how a transaction ends
// up on the wrong network.
//
// These follow the convention of solana-go's own program IDs (exported vars
// rather than accessors, because a PublicKey is not a Go constant). They are
// effectively immutable and must not be reassigned.
var (
	// ProgramIDMainnet is the mainnet-beta deployment of lb_clmm.
	ProgramIDMainnet = mustKey("LBUZKhRxPF3XUpBCjp4YzTKgLccjZhTSDM9YuVaPwxo")

	// ProgramIDDevnet is the devnet deployment. It is the same address as
	// mainnet-beta, verified against LBCLMM_PROGRAM_IDS rather than assumed.
	ProgramIDDevnet = ProgramIDMainnet

	// ProgramIDLocalhost is the local-validator deployment used by the program's
	// own integration tests. It differs from the public clusters.
	ProgramIDLocalhost = mustKey("LbVRzDTvBDEcrthxfZ4RL6yiq3uZw8bS6MwtdY6UhFQ")
)

// mustKey decodes a base58 public key, panicking on a malformed literal.
//
// It is used only for hard-coded program addresses. Panicking is correct here:
// a malformed literal is a build-time mistake in this package, not a runtime
// condition a caller could handle, and the failure must not be deferred to the
// first transaction.
func mustKey(s string) solana.PublicKey {
	k, err := solana.PublicKeyFromBase58(s)
	if err != nil {
		panic(fmt.Sprintf("lbclmm: invalid program id literal %q: %v", s, err))
	}

	return k
}
