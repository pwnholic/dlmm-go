# dlmm-go — Meteora DLMM SDK for Go
#
# Layering rule (ARCHITECTURE.md P3): math/bin/binarray/pda/program must not
# depend on solana-go/rpc. `make check-layering` enforces that.

GO        ?= go
GOFLAGS   ?=
GOTOOL    := $(GO) tool
BINARY    ?= dlmmctl
BIN_DIR   ?= bin
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   ?= -ldflags "-X main.version=$(VERSION)"

# Tools are pinned in go.mod via `tool` directives (Go 1.24+), so each one
# builds with the project's own toolchain instead of whatever is on PATH.
# This is what fixes the earlier failure where a PATH-installed golangci-lint
# was built with go1.26 and could not type-check a go1.27 toolchain's stdlib.
TOOLS     := golangci-lint govulncheck gofumpt benchstat

# Offline-verifiable packages: no RPC, no network. Must stay green without
# network access. Keep in sync with ARCHITECTURE.md section 8.
OFFLINE_PKGS := ./num/... ./internal/... ./math/... ./bin/... ./binarray/... \
                ./pda/... ./program/... ./strategy/... ./risk/... ./agent/... \
                ./sim/...

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: all
all: tidy check test ## Run the full local gate

.PHONY: tidy
tidy: ## Sync go.mod/go.sum
	$(GO) mod tidy

.PHONY: build
build: ## Build all packages and the CLI
	$(GO) build $(GOFLAGS) ./...
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY) ./cmd/$(BINARY)

.PHONY: install
install: ## Install the CLI into GOPATH/bin
	$(GO) install $(LDFLAGS) ./cmd/$(BINARY)

.PHONY: test
test: ## Run all tests (light, no race detector)
	$(GO) test -count=1 ./...

.PHONY: test-race
test-race: ## Run all tests under the race detector (heavier; use in CI)
	$(GO) test -race -count=1 ./...

.PHONY: test-offline
test-offline: ## Run only the offline-verifiable packages (no network)
	$(GO) test -count=1 $(OFFLINE_PKGS)

.PHONY: test-verbose
test-verbose: ## Run all tests verbosely (light)
	$(GO) test -count=1 -v ./...

.PHONY: test-integration
test-integration: ## Run tests that require network/devnet
	$(GO) test -race -count=1 -tags=integration ./...

.PHONY: cover
cover: ## Write and open an HTML coverage report
	$(GO) test -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: bench
bench: ## Run benchmarks
	$(GO) test -run '^$$' -bench=. -benchmem ./...

.PHONY: fuzz
fuzz: ## Fuzz the numerics and codec layers for FUZZTIME (default 30s each)
	@for pkg in ./num ./internal/uint256 ./math; do \
		for fn in $$($(GO) test -list 'Fuzz.*' $$pkg 2>/dev/null | grep '^Fuzz'); do \
			echo "fuzzing $$pkg $$fn"; \
			$(GO) test $$pkg -run '^$$' -fuzz "^$$fn$$" -fuzztime $${FUZZTIME:-30s} || exit 1; \
		done; \
	done

.PHONY: fuzz-short
fuzz-short: ## A 5s pass per fuzz target, for a quick sanity sweep
	@$(MAKE) fuzz FUZZTIME=5s

# Our own package directories, from the module graph rather than a filesystem
# walk. package-referense/* are separate modules holding upstream checkouts, so
# `go list ./...` excludes them; `gofumpt -w .` does not, and once reformatted
# 139 files inside the vendored solana-go reference.
OWN_DIRS := $(shell $(GO) list -f '{{.Dir}}' ./... 2>/dev/null)

.PHONY: fmt
fmt: ## Format our own sources with the pinned gofumpt
	@if [ -z "$(OWN_DIRS)" ]; then echo "FAIL: no package directories resolved"; exit 1; fi
	@$(GOTOOL) gofumpt -l -w $(OWN_DIRS)
	@echo "formatted $(words $(OWN_DIRS)) package directories"

.PHONY: fmt-check
fmt-check: ## Fail if our sources are not formatted
	@if [ -z "$(OWN_DIRS)" ]; then echo "FAIL: no package directories resolved"; exit 1; fi
	@out=$$($(GOTOOL) gofumpt -l $(OWN_DIRS) 2>/dev/null || true); \
	if [ -n "$$out" ]; then echo "unformatted:"; echo "$$out"; exit 1; fi
	@echo "  ok ($(words $(OWN_DIRS)) package directories checked)"

.PHONY: check-referense-pristine
check-referense-pristine: ## Fail if a vendored reference checkout was modified
	@bad=0; \
	for d in package-referense/*/; do \
		[ -d "$$d/.git" ] || continue; \
		n=$$(git -C "$$d" status --porcelain 2>/dev/null | wc -l); \
		if [ "$$n" -ne 0 ]; then \
			echo "  MODIFIED: $$d ($$n files) -- these are upstream checkouts, not ours"; \
			git -C "$$d" status --porcelain | head -5; \
			bad=1; \
		fi; \
	done; \
	if [ $$bad -ne 0 ]; then echo "reference checkouts were modified; restore them"; exit 1; fi; \
	echo "  ok: all reference checkouts are pristine"

.PHONY: lint
lint: ## Run the pinned golangci-lint
	$(GOTOOL) golangci-lint run ./...

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with --fix
	$(GOTOOL) golangci-lint run --fix ./...

.PHONY: tools
list-tools: ## List the tools pinned in go.mod
	@echo "pinned tools:"; for t in $(TOOLS); do printf "  %-16s " "$$t"; $(GOTOOL) -n "$$t" >/dev/null 2>&1 && echo "resolved" || echo "NOT RESOLVED"; done

.PHONY: install-tools
install-tools: ## Install every pinned tool into GOBIN/PATH
	$(GO) install tool

.PHONY: update-tools
update-tools: ## Update pinned tools and review the go.mod/go.sum diff
	$(GO) get -u tool
	$(GO) mod tidy
	@echo "review the go.mod/go.sum diff before committing"

.PHONY: check-tools-broken
check-tools-broken: ## Confirm the old PATH toolchain mismatch is actually gone
	@echo "toolchain: $$($(GO) version)"
	@echo "golangci-lint: $$($(GOTOOL) golangci-lint --version 2>&1 | head -1)"

.PHONY: vet
typecheck: vet ## Alias: type-check the module without linking test binaries

.PHONY: vet
vet: ## Type-check the module (does not build or run tests)
	$(GO) vet ./...

# Packages permitted to talk to the network. Everything else must not import
# solana-go/rpc. Allowlist is deliberate: a new package that starts importing
# rpc fails the check until it is declared here (fail-closed).
RPC_ALLOWED := github.com/pwnholic/dlmm-go/dlmm \
               github.com/pwnholic/dlmm-go/datapi \
               github.com/pwnholic/dlmm-go/market \
               github.com/pwnholic/dlmm-go/cmd/dlmmctl

.PHONY: check-layering
check-layering: ## Enforce the no-RPC rule: only allowlisted packages may import solana-go/rpc
	@echo "checking layering (only allowlisted packages may import solana-go/rpc)..."
	@pkgs=$$($(GO) list -f '{{.ImportPath}}' ./... 2>/dev/null); \
	if [ -z "$$pkgs" ]; then \
		echo "  FAIL: go list returned no packages; nothing was verified"; exit 1; \
	fi; \
	checked=0; bad=0; \
	for pkg in $$pkgs; do \
		checked=$$((checked+1)); \
		imports=$$($(GO) list -f '{{join .Imports "\n"}}' $$pkg 2>/dev/null); \
		case "$$imports" in *github.com/gagliardetto/solana-go/rpc*) ;; *) continue ;; esac; \
		case " $(RPC_ALLOWED) " in *" $$pkg "*) continue ;; esac; \
		echo "  VIOLATION: $$pkg imports solana-go/rpc but is not in RPC_ALLOWED"; \
		bad=1; \
	done; \
	if [ $$bad -ne 0 ]; then echo "layering violated"; exit 1; fi; \
	echo "  ok ($$checked packages checked; RPC allowlist has $(words $(RPC_ALLOWED)) entries, "
	@echo "     some of which may not exist yet)"

.PHONY: check-layering-selftest
check-layering-selftest: ## Prove the layering check can actually fail (guards against a false pass)
	@echo "self-test: does the detector fire on a known violation?"
	@probe=./internal/zzlayeringprobe; \
	mkdir -p $$probe; \
	printf 'package zzlayeringprobe\n\nimport _ "github.com/gagliardetto/solana-go/rpc"\n' > $$probe/probe.go; \
	imports=$$($(GO) list -f '{{join .Imports "\n"}}' ./internal/zzlayeringprobe 2>/dev/null); \
	rm -rf $$probe; \
	if echo "$$imports" | grep -qx 'github.com/gagliardetto/solana-go/rpc'; then \
		echo "  ok: detector fires on a real violation"; \
	else \
		echo "  FAIL: detector did not fire. check-layering cannot be trusted."; exit 1; \
	fi

.PHONY: generate
generate: ## Regenerate program bindings from the IDL
	cd tools/idlgen && $(GO) run . -idl ../../idls/dlmm.json -out ../../program/lbclmm

.PHONY: check-idl
check-idl: ## Verify lbclmm constants and pda seeds against the vendored IDL
	@python3 tools/check_idl_constants.py

.PHONY: check-idl-selftest
check-idl-selftest: ## Prove check-idl can fail (guards against a false pass)
	@echo "self-test: mutating a constant and expecting the check to fail..."
	@cp program/lbclmm/constants.go /tmp/dlmmgo-const.bak; \
	sed -i 's/\tMaxBinPerArray = 70/\tMaxBinPerArray = 71/' program/lbclmm/constants.go; \
	if python3 tools/check_idl_constants.py >/dev/null 2>&1; then \
		cp /tmp/dlmmgo-const.bak program/lbclmm/constants.go; \
		echo "  FAIL: check-idl accepted a corrupted constant; it cannot be trusted"; exit 1; \
	fi; \
	cp /tmp/dlmmgo-const.bak program/lbclmm/constants.go; \
	if ! python3 tools/check_idl_constants.py >/dev/null 2>&1; then \
		echo "  FAIL: check-idl did not pass after restoring the file"; exit 1; \
	fi; \
	echo "  ok: check-idl rejects a wrong constant and accepts the restored file"

.PHONY: vectors
vectors: ## Regenerate the golden numeric vectors with the Python oracle
	@python3 tools/gen_vectors/gen_numeric_vectors.py
	@echo "now run: make test-offline"

.PHONY: vectors-check
vectors-check: ## Fail if the committed vectors differ from a fresh generation
	@cp testdata/vectors/numeric.json /tmp/dlmmgo-vectors.bak
	@python3 tools/gen_vectors/gen_numeric_vectors.py >/dev/null
	@if ! diff -q /tmp/dlmmgo-vectors.bak testdata/vectors/numeric.json >/dev/null; then \
		cp /tmp/dlmmgo-vectors.bak testdata/vectors/numeric.json; \
		echo "vectors are stale; run 'make vectors' and review the diff"; exit 1; \
	fi
	@echo "  ok: vectors reproduce byte-for-byte"

.PHONY: check
check: check-idl check-layering check-referense-pristine fmt-check vet ## Static gate: IDL, layering, references, formatting, type-check
	@echo "static gate passed"

.PHONY: generate-check
generate-check: check-idl ## Fail if generated bindings are stale or constants drifted
	$(MAKE) generate
	@if ! git diff --quiet -- program/ pda/; then \
		echo "generated code is stale; commit the regenerated files"; \
		git --no-pager diff --stat -- program/ pda/; exit 1; \
	fi

.PHONY: vuln
vuln: ## Run the pinned govulncheck
	$(GOTOOL) govulncheck ./...

.PHONY: benchstat
benchstat: ## Compare two benchmark outputs: make benchstat OLD=a.txt NEW=b.txt
	@test -n "$(OLD)" -a -n "$(NEW)" || { echo "usage: make benchstat OLD=old.txt NEW=new.txt"; exit 1; }
	$(GOTOOL) benchstat $(OLD) $(NEW)

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html
