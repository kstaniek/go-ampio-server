################################################################################
# Project metadata
################################################################################
MODULE       := github.com/kstaniek/go-ampio-server
CMD_PKG      := ./cmd/can-server
BINARY_NAME  := can-server

# Raw version from git (may have leading v)
RAW_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# Strip leading 'v' if present to enforce x.y.z format in builds
VERSION := $(shell echo $(RAW_VERSION) | sed -E 's/^v//')
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

TAGS      ?=
LDFLAGS    = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
GOFLAGS   ?=
CGO       ?= 0

BIN_DIR  := bin
DIST_DIR := dist

.DEFAULT_GOAL := help

################################################################################
# Help
################################################################################
.PHONY: help
help: ## Show this help
	@echo "\nTargets:";
	@grep -E '^[a-zA-Z0-9_.-]+:.*## ' $(MAKEFILE_LIST) | sort | sed -E 's/^([a-zA-Z0-9_.-]+):.*## /\1\t/' | awk 'BEGIN{FS="\t"} {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo "\nExamples:\n  make build\n  make build-linux\n  make ci\n  make dist VERSION=v1.2.3\n"

################################################################################
# Formatting & lint
################################################################################
.PHONY: fmt
fmt: ## Format code & tidy modules
	@gofmt -s -w .
	@go mod tidy

.PHONY: fmt-check
fmt-check: ## Fail if formatting or mod tidy not clean
	@echo "Checking formatting"; \
	files=$$(gofmt -s -l .); if [ -n "$$files" ]; then echo "Unformatted:"; echo "$$files"; exit 1; fi; echo OK
	@echo "Checking go mod tidy"; \
	tmp=$$(mktemp -d); cp go.mod go.sum $$tmp/; go mod tidy >/dev/null 2>&1; \
	if ! cmp -s go.mod $$tmp/go.mod || ! cmp -s go.sum $$tmp/go.sum; then echo 'go.mod/go.sum not tidy'; rm -rf $$tmp; exit 1; fi; rm -rf $$tmp; echo OK

.PHONY: vet
vet: ## Run go vet
	@go vet ./...

.PHONY: lint
lint: ## Run staticcheck and govulncheck if installed (install: make tools)
	@set -e; \
	if command -v staticcheck >/dev/null 2>&1; then \
	  echo '>> staticcheck'; staticcheck ./... || exit 1; \
	else \
	  echo '[info] staticcheck not installed (run: make tools)'; \
	fi; \
	if command -v govulncheck >/dev/null 2>&1; then \
	  echo '>> govulncheck'; govulncheck ./... || exit 1; \
	else \
	  echo '[info] govulncheck not installed (run: make tools)'; \
	fi

## (tools target removed; previously installed staticcheck/govulncheck/SBOM/cosign)

.PHONY: toolsma
tools: ## Install development tools (staticcheck, govulncheck, goreleaser)
	@set -e; \
	echo 'Installing staticcheck@latest'; go install honnef.co/go/tools/cmd/staticcheck@latest; \
	echo 'Installing govulncheck@latest'; go install golang.org/x/vuln/cmd/govulncheck@latest; \
	echo 'Installing goreleaser@latest'; go install github.com/goreleaser/goreleaser/v2@v2.12.3; \
	echo 'Done.'

################################################################################
# Tests & fuzz
################################################################################
.PHONY: test
test: ## Run unit tests
	@go test $(GOFLAGS) ./...

.PHONY: test-race
test-race: ## Run tests with race detector
	@go test $(GOFLAGS) -race ./...

.PHONY: cover
cover: ## Coverage (HTML at coverage.html)
	@go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -n 1
	@go tool cover -html=coverage.out -o coverage.html

.PHONY: bench
bench: ## Run benchmarks for codec
	@go test -run=^$$ -bench=. -benchmem ./internal/cnl

FUZZTIME ?= 5s
.PHONY: fuzz-smoke
fuzz-smoke: ## Short fuzz run of codec
	@go test -run=^$$ -fuzz=FuzzCodecRoundTrip -fuzztime=$(FUZZTIME) ./internal/cnl
	@go test -run=^$$ -fuzz=FuzzCodecDecodeInvalid -fuzztime=$(FUZZTIME) ./internal/cnl

.PHONY: fuzz-server
fuzz-server: ## Fuzz server codec decode (internal/server FuzzCodecDecode)
	@go test -run=FuzzCodecDecode -fuzz=FuzzCodecDecode -fuzztime=$(FUZZTIME) ./internal/server

.PHONY: stress
stress: ## Run stress broadcast test
	@go test -run TestStressBroadcast -count=1 ./internal/server

.PHONY: prepare-release
prepare-release: ## Tag NEXT version (usage: make prepare-release NEXT=1.2.3)
	@[ -n "$(NEXT)" ] || { echo 'NEXT=<x.y.z> required'; exit 1; }
	@if ! echo "$(NEXT)" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$$'; then echo 'NEXT not semver x.y.z'; exit 1; fi
	@if git rev-parse "$(NEXT)" >/dev/null 2>&1; then echo "Tag $(NEXT) already exists"; exit 1; fi
	@if ! git diff --quiet || ! git diff --cached --quiet; then echo 'Working tree not clean'; exit 1; fi
	@git tag -a $(NEXT) -m "Release $(NEXT)"
	@echo "Created tag $(NEXT). Push with: git push origin main $(NEXT)"

################################################################################
# Builds (standardized)
################################################################################
# Usage:
#   make build                -> host binary (bin/can-server)
#   make build-cross          -> linux/{amd64,arm64} binaries (bin/linux-ARCH/can-server)

ARCHES ?= amd64 arm64
# To include 32-bit ARM builds add 'arm' to ARCHES (e.g. ARCHES="amd64 arm64 arm")
GOARM  ?= 7
OS     ?= linux

.PHONY: build
build: ## Build host binary (use TAGS=prometheus to enable metrics endpoint)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO) go build $(GOFLAGS) -tags='$(TAGS)' -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PKG)

.PHONY: build-cross
build-cross: ## Cross-compile $(OS)/{amd64,arm64[,arm]} (override ARCHES="...")
	@for arch in $(ARCHES); do \
		outdir=$(BIN_DIR)/$(OS)-$$arch; \
		mkdir -p $$outdir; \
		echo "Building $(OS)/$$arch (tags: $(TAGS))"; \
		if [ "$$arch" = "arm" ]; then \
			CGO_ENABLED=0 GOOS=$(OS) GOARCH=arm GOARM=$(GOARM) go build $(GOFLAGS) -tags='$(TAGS)' -ldflags '$(LDFLAGS)' -o $$outdir/$(BINARY_NAME) $(CMD_PKG) || exit 1; \
		else \
			CGO_ENABLED=0 GOOS=$(OS) GOARCH=$$arch go build $(GOFLAGS) -tags='$(TAGS)' -ldflags '$(LDFLAGS)' -o $$outdir/$(BINARY_NAME) $(CMD_PKG) || exit 1; \
		fi; \
	done


################################################################################
# Distribution
################################################################################

# NOTE: Manual dist / checksums targets are retained for local/offline use.
# The canonical release pipeline is now defined in .goreleaser.yaml (archives,
# deb package). Prefer the goreleaser-* targets below for parity with CI.

.PHONY: dist
dist: build-cross ## Create tar.gz artifacts (requires build-cross)
	@mkdir -p $(DIST_DIR)
	tar -C $(BIN_DIR)/linux-amd64 -czf $(DIST_DIR)/$(BINARY_NAME)_$(VERSION)_linux_amd64.tar.gz $(BINARY_NAME)
	tar -C $(BIN_DIR)/linux-arm64 -czf $(DIST_DIR)/$(BINARY_NAME)_$(VERSION)_linux_arm64.tar.gz $(BINARY_NAME)

.PHONY: dist-prom
# (Use: make dist TAGS=prometheus for Prometheus build)

.PHONY: checksums
checksums: dist ## Generate SHA256 checksums for dist tarballs
	@echo "Generating sha256 sums"; \
	if command -v sha256sum >/dev/null 2>&1; then sha256sum dist/*.tar.gz > dist/sha256sums.txt; \
	else shasum -a 256 dist/*.tar.gz > dist/sha256sums.txt; fi; \
	echo "Wrote dist/sha256sums.txt"




################################################################################
# Goreleaser integration
################################################################################
.PHONY: goreleaser-snapshot
goreleaser-snapshot: ## Build snapshot (no publish) with goreleaser
	@command -v goreleaser >/dev/null 2>&1 || { echo 'goreleaser not installed (go install github.com/goreleaser/goreleaser/v2/cmd/goreleaser@latest)'; exit 1; }
	@goreleaser release --snapshot --clean --timeout=30m

## (Removed docker image variant)

.PHONY: goreleaser-prep
goreleaser-prep: ## Ensure clean tree & tag format before release
	@if ! git diff --quiet || ! git diff --cached --quiet; then echo 'Uncommitted changes present'; exit 1; fi
	@if ! git describe --tags --exact-match >/dev/null 2>&1; then echo 'HEAD not at a tag'; exit 1; fi
	@if ! git describe --tags --exact-match | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then echo 'Tag not x.y.z'; exit 1; fi

.PHONY: goreleaser-release
goreleaser-release: version-check ## Perform full release (tag required) via goreleaser
	@command -v goreleaser >/dev/null 2>&1 || { echo 'goreleaser not installed'; exit 1; }
	@if ! git diff --quiet; then echo 'Working tree dirty; commit or stash before releasing'; exit 1; fi
	@if ! git describe --tags --exact-match >/dev/null 2>&1; then echo 'No exact tag on HEAD; create tag (e.g. git tag 1.2.3 && git push --tags)'; exit 1; fi
	@goreleaser release --clean --timeout=30m

.PHONY: goreleaser-verify
goreleaser-verify: ## Verify cosign signatures for local snapshot outputs (images require registry push)
	@if ! command -v cosign >/dev/null 2>&1; then echo 'cosign not installed'; exit 1; fi
	@if [ ! -f dist/sha256sums.txt ]; then echo 'dist/sha256sums.txt missing (run goreleaser-snapshot)'; exit 1; fi
	@if [ -f dist/sha256sums.txt.sig ] && [ -f dist/sha256sums.txt.pem ]; then \
	  cosign verify-blob --certificate dist/sha256sums.txt.pem --signature dist/sha256sums.txt.sig dist/sha256sums.txt || exit 1; \
	  echo 'Verified checksum file (cosign)'; \
	else echo 'Cosign checksum signature not present (snapshot with signing disabled?)'; fi

.PHONY: goreleaser-clean
goreleaser-clean: ## Remove dist/ (goreleaser outputs)
	rm -rf dist

################################################################################
# Meta
################################################################################
.PHONY: version
version: ## Show version metadata
	@echo VERSION=$(VERSION) COMMIT=$(COMMIT) DATE=$(DATE)

.PHONY: version-check
version-check: ## Fail if VERSION not x.y.z (ignores -dirty or commits)
	@if ! echo "$(VERSION)" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		echo "VERSION $(VERSION) is not strict x.y.z"; exit 1; fi; echo OK

.PHONY: retag
retag: ## Delete existing tag TAG=x.y.z locally & remotely, recreate at HEAD (use CAREFUL=1 to skip prompt)
	@[ -n "$(TAG)" ] || { echo 'TAG=<x.y.z> required'; exit 1; }
	@if ! echo "$(TAG)" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$$'; then echo "Tag $(TAG) invalid (expect x.y.z)"; exit 1; fi
	@if ! git diff --quiet || ! git diff --cached --quiet; then echo 'Working tree not clean'; exit 1; fi
	@if git tag | grep -Fx "$(TAG)" >/dev/null; then echo "[info] Local tag $(TAG) exists (will remove)"; fi
	@if [ "$(CAREFUL)" != "1" ]; then \
		printf 'Recreate tag %s at HEAD (%s)? [y/N] ' "$(TAG)" "$(COMMIT)"; read ans; \
		[ "$$ans" = "y" ] || { echo 'Aborted'; exit 1; }; \
	fi
	@git tag -d $(TAG) >/dev/null 2>&1 || true
	@echo "[retag] Deleted local tag (if existed)"
	@git push origin :refs/tags/$(TAG) >/dev/null 2>&1 || true
	@echo "[retag] Requested remote deletion"
	@git tag -a $(TAG) -m "Release $(TAG)"
	@echo "[retag] Created new annotated tag $(TAG) at $$(git rev-parse --short HEAD)"
	@git push origin $(TAG)
	@echo "[retag] Pushed new tag $(TAG)"

.PHONY: install-hooks
install-hooks: ## Install git hooks (pre-commit formatting, pre-push tag checks)
	@[ -d .git/hooks ] || { echo '.git directory not present; run inside a git clone'; exit 1; }
	@for h in scripts/git-hooks/*; do \
	  name=$$(basename $$h); install -m755 $$h .git/hooks/$$name; echo "Installed .git/hooks/$$name"; \
	done

.PHONY: ci
ci: fmt-check vet lint test-race build ## CI aggregate (host build + tests)

################################################################################
# Smoke tests
################################################################################
.PHONY: smoke-test
smoke-test: ## Run lightweight integration smoke tests
	@go test -run TestSmokeServer ./internal/server

.PHONY: clean
clean: ## Remove build & coverage artifacts
	@rm -rf $(BIN_DIR) $(DIST_DIR) coverage.out coverage.html

################################################################################
# Toolchain helpers
################################################################################
.PHONY: toolchain
toolchain: ## Show local Go toolchain version and go.mod directive
	@echo "Local toolchain: $(shell go version)";
	@echo -n "go.mod directive: "; grep -E '^go ' go.mod || echo '(none)'

.PHONY: update-go
update-go: ## Update 'go' directive in go.mod (GO=1.xx[.y])
	@[ -n "$(GO)" ] || { echo 'GO=<major.minor>[.patch] required (e.g. make update-go GO=1.24.3)'; exit 1; }
	@MAJMIN=$$(echo "$(GO)" | awk -F. 'NF<2{print;next}{print $$1"."$$2}'); \
	 if ! echo $$MAJMIN | grep -Eq '^1\.[0-9]+$$'; then echo "Invalid GO version format: $(GO)"; exit 1; fi; \
	 BEFORE=$$(grep -E '^go ' go.mod || true); \
	 awk -v ver="$$MAJMIN" 'BEGIN{updated=0} /^go [0-9]/{print "go "ver; updated=1; next} {print} END{ if(!updated){print "go "ver} }' go.mod > go.mod.tmp && mv go.mod.tmp go.mod; \
	 AFTER=$$(grep -E '^go ' go.mod || true); \
	 echo "go.mod before: $$BEFORE"; echo "go.mod after : $$AFTER"; \
	 go mod tidy >/dev/null 2>&1 || { echo 'go mod tidy failed'; exit 1; }; \
	 echo "Updated go directive to $$MAJMIN"

## (Docker helpers removed)

################################################################################
# Debian/RPM via Goreleaser (snapshot wrapper)
################################################################################
.PHONY: deb
deb: ## Build snapshot deb via goreleaser (requires goreleaser installed)
	@command -v goreleaser >/dev/null 2>&1 || { echo 'goreleaser not installed'; exit 1; }
	@echo '>> goreleaser snapshot (deb)'
	goreleaser release --snapshot --skip=publish --clean --timeout=30m >/dev/null || { echo 'goreleaser snapshot failed'; exit 1; }
	@deb_count=$$(ls -1 dist/*.deb 2>/dev/null | wc -l || true); if [ "$$deb_count" -eq 0 ]; then echo 'No .deb produced'; exit 1; fi; ls -1 dist/*.deb
