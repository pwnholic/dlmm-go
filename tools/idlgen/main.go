// Command idlgen generates Go bindings for the Meteora DLMM program from
// idls/dlmm.json.
//
// The generator is deliberately small and total: it walks the IDL type graph,
// computes the Borsh size of every type, and emits Go structs plus a decoder for
// each of the twelve accounts. The acceptance test for the size model is that
// LbPair comes out at 896 bytes and BinArray at 10128, which match the real
// on-chain fixtures (904 and 10136 including the 8-byte discriminator).
//
// It lives in its own module so its dependencies never touch the SDK's go.mod.
//
// Usage:
//
//	go run ./tools/idlgen -idl ../../idls/dlmm.json -out ../../program/lbclmm
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func main() {
	idlPath := flag.String("idl", "idls/dlmm.json", "path to the Anchor IDL")
	outDir := flag.String("out", "program/lbclmm", "directory to write generated Go into")
	flag.Parse()

	idl, err := loadIDL(*idlPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "idlgen: loading %s: %v\n", *idlPath, err)
		os.Exit(1)
	}

	types, err := newTypeGraph(idl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "idlgen: %v\n", err)
		os.Exit(1)
	}

	// The size model is the generator's own claim about the wire format. Check
	// it against the sizes the real fixtures prove, so a mistake here fails the
	// generator rather than shipping as silently wrong decoders.
	if err := types.assertKnownSizes(); err != nil {
		fmt.Fprintf(os.Stderr, "idlgen: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "idlgen: creating %s: %v\n", *outDir, err)
		os.Exit(1)
	}

	files, err := types.generate(*outDir, *idlPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "idlgen: %v\n", err)
		os.Exit(1)
	}

	sort.Strings(files)
	fmt.Printf("idlgen: wrote %d files to %s\n", len(files), *outDir)
	for _, f := range files {
		fmt.Printf("  %s\n", filepath.Base(f))
	}
}
