.DEFAULT_GOAL := help

BIN          := aboard
INSTALL_DIR  ?= /usr/local/bin
COVER_FILE   := coverage.out

# Tooling pinned via bingo. See .bingo/Variables.mk for $(GOLANGCI_LINT),
# $(GOFUMPT), $(GORELEASER), $(GOVULNCHECK), $(BINGO) — each variable expands
# to a version-stamped binary path under $(GOBIN), and the included rules
# (re)build the tool when its .mod file changes. Update versions with
# `bingo get <module>@<version>`.
include .bingo/Variables.mk

# Version stamping. The same three variables goreleaser sets (see the ldflags
# block in .goreleaser.yaml) so a `make build` binary and a released one report
# identity by identical rules; pkg/aboard falls back to debug.ReadBuildInfo when
# they are unset, which is what `go install` and `go run` get.
#
# DATE is the COMMIT's date, not `date -u` now: a wall-clock stamp changes every
# second, so every build would relink and Go's build cache would never hit —
# and `make caps`, which builds twice on purpose, would pay for it twice.
VERSION_PKG := github.com/exoport/aboard/pkg/aboard
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT  ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_DATE  ?= $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)
GO_LDFLAGS  := -X $(VERSION_PKG).Version=$(VERSION) \
               -X $(VERSION_PKG).BuildDate=$(BUILD_DATE) \
               -X $(VERSION_PKG).GitCommit=$(GIT_COMMIT)

# What `make caps` regenerates. Named here so the recipe can write through a
# temp file: a bare `> file` truncates before the command runs, so a failed
# generator leaves an EMPTY controls module in the tree and the next build
# embeds it.
CAPS_JS      := pkg/aboard/web/views/controls.generated.js
CAPS_MD      := .claude/skills/aboard/references/reference.generated.md
CAPS_RECIPES := .claude/skills/aboard/references/recipes.md

.PHONY: help
# The character class carries 0-9 deliberately: without it `e2e` — the browser
# suite, the target this project most wants a session to run — matched nothing
# and was invisible in `make help`, while CLAUDE.md said help listed the
# available targets. A help rule that silently omits a target is worse than no
# help rule, because it reads as proof the target does not exist.
# TestEveryDocumentedMakeTargetIsListedByHelp keeps this honest.
help:              ## Show this help.
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | sort \
	  | awk 'BEGIN {FS = ":[^#]*## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build:             ## Build the aboard binary into ./aboard.
	go build -ldflags '$(GO_LDFLAGS)' -o $(BIN) ./cmd/aboard

.PHONY: install
install:           ## Build and install aboard to INSTALL_DIR (default: /usr/local/bin).
	@go build -ldflags '$(GO_LDFLAGS)' -o $(BIN) ./cmd/aboard
	@install -m 755 $(BIN) $(INSTALL_DIR)/$(BIN)
	@rm -f $(BIN)
	@echo "installed $(BIN) to $(INSTALL_DIR)/"

.PHONY: test
test:              ## Run all Go tests with the race detector.
	go test -race ./...

.PHONY: test-cover
test-cover:        ## Run tests and produce a coverage profile.
	go test -race -coverprofile=$(COVER_FILE) ./...
	@echo "view coverage: go tool cover -html=$(COVER_FILE)"

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci-lint (pinned via bingo).
	$(GOLANGCI_LINT) run ./...

.PHONY: fmt
fmt: $(GOFUMPT)    ## Format Go source with gofumpt (pinned via bingo).
	$(GOFUMPT) -l -w .

# The formatting GATE, as opposed to `fmt` which fixes it. It exists so that
# nothing has to call a tool from $PATH to check formatting: a $PATH gofumpt and
# the bingo pin are two different binaries, they were two different versions, and
# they disagreed about this tree — which is how a hook and a ladder can both be
# green while the CI that runs the pin is not. Every gate now goes through make,
# and make goes through .bingo.
#
# The tool's EXIT STATUS is checked as well as its output, and that is not
# belt-and-braces: `-l` prints nothing when gofumpt cannot parse a file, so a
# recipe that only tests `-n "$$out"` prints "fmt-check ok" and exits 0 over a
# tree that was never formatted-checked at all. Measured — a file with a syntax
# error passed this gate. It is the same trap COMMON.md records for
# `$$(cmd; echo $$?)`, and a green gate that checked nothing is the exact failure
# this whole item exists to remove. stderr is folded into the capture so the
# parse error is what the reader sees.
.PHONY: fmt-check
fmt-check: $(GOFUMPT) ## Fail with the file list if anything needs gofumpt (the pinned one).
	@out=$$($(GOFUMPT) -l . 2>&1); status=$$?; \
		if [ $$status -ne 0 ]; then echo "gofumpt failed (exit $$status):"; echo "$$out"; exit $$status; fi; \
		if [ -n "$$out" ]; then echo "gofumpt needed:"; echo "$$out"; exit 1; fi; \
		echo "fmt-check ok"

# The zero-dependency gate, carried over from the split's Makefile. `lint` is
# the real one, but it needs bingo to have fetched golangci-lint; this one runs
# in a bare checkout with nothing but the Go toolchain, which is what a first
# `git clone && make check` has.
.PHONY: check
check:             ## vet + gofmt check — the gate that needs no tools fetched.
	go vet ./...
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	@echo "check ok"

.PHONY: pre-commit
pre-commit:        ## Run pre-commit hooks across all files.
	pre-commit run --all-files

.PHONY: snapshot
snapshot: $(GORELEASER) ## Build release snapshot artifacts via goreleaser (no upload, no sign).
	# --skip=sign avoids the cosign OIDC device flow in local runs.
	# Real releases sign via release.yml, which runs on a GitHub Actions
	# runner whose ambient OIDC token is automatically exchanged with
	# Fulcio. Locally we just want to verify the archive layout.
	$(GORELEASER) release --snapshot --clean --skip=publish --skip=sign

.PHONY: govulncheck
govulncheck: $(GOVULNCHECK) ## Scan for known vulnerabilities (pinned via bingo); allow-lists documented unfixable advisories.
	python3 scripts/govulncheck-gate.py $(GOVULNCHECK) ./...

.PHONY: tools
tools: $(GOLANGCI_LINT) $(GOFUMPT) $(GORELEASER) $(GOVULNCHECK) ## Pre-install all bingo-pinned tools.
	@echo "tools installed under $(GOBIN)"

.PHONY: tidy
tidy:              ## Update go.mod and go.sum.
	go mod tidy

# Screenshots are deliberately NOT removed here: test/shot.sh writes them to
# .aboard/run/shots/ (Root.ShotsDir), and everything under .aboard/ is the
# board's own — state, uploads, journal, instance record. A `make clean` that
# reached in there would delete a human's work to save a few kilobytes.
.PHONY: clean
clean:             ## Remove build artifacts. Never touches .aboard/ — that is board STATE.
	rm -f $(BIN) $(COVER_FILE)
	rm -rf dist/

.PHONY: xcompile-windows
xcompile-windows:  ## Cross-compile + cross-vet for Windows; catches portability compile errors.
	@echo "==> GOOS=windows go vet ./..."
	@GOOS=windows GOARCH=amd64 go vet ./...
	@echo "==> GOOS=windows go build ./..."
	@GOOS=windows GOARCH=amd64 go build ./...
	@echo "==> GOOS=windows go test -c (per package, output discarded)"
	@for pkg in $$(go list ./...); do \
		GOOS=windows GOARCH=amd64 go test -c -o /dev/null $$pkg \
		  || { echo "FAIL: $$pkg"; exit 1; }; \
	done

.PHONY: docs-cli
docs-cli:          ## Regenerate docs/reference/cli.md from the cobra command tree.
	go run ./cmd/aboard gen-docs --out docs/reference/cli.md

.PHONY: docs-check
docs-check:        ## Verify docs/ links resolve and every doc is reachable from docs/README.md.
	python3 scripts/check-docs-links.py docs

# Regenerate what the binary emits from its own manifest: the controls module
# the renderers import, the skill's generated reference, and the skill's recipe
# index. The authored files carry judgment; these carry facts, and facts drift.
#
# Built TWICE on purpose. pkg/aboard/web is embedded, so the first binary emits
# the module from the current specs and the second one embeds the module it just
# wrote — otherwise the server keeps serving the previous controls while your
# spec edit appears to do nothing. That is the spike's oldest gotcha; it costs
# one link to avoid.
#
# `capabilities --check` is last, and it is the assertion: it fails when the
# committed reference was generated for a different capsHash. Run this after
# ANY edit to a views/*.spec.json or a builtin recipe, and commit what it writes.
.PHONY: caps
caps: build        ## Regenerate the generated controls module, skill reference and recipe index.
	@./$(BIN) capabilities --format js > $(CAPS_JS).tmp && mv $(CAPS_JS).tmp $(CAPS_JS)
	@./$(BIN) capabilities --format md > $(CAPS_MD).tmp && mv $(CAPS_MD).tmp $(CAPS_MD)
	@./$(BIN) recipes index > $(CAPS_RECIPES).tmp && mv $(CAPS_RECIPES).tmp $(CAPS_RECIPES)
	go build -ldflags '$(GO_LDFLAGS)' -o $(BIN) ./cmd/aboard
	@./$(BIN) capabilities --check
	@echo "caps regenerated — check 'git diff' and commit the generated files"

# The browser suite. Local only, never in CI: it drives a real Chromium against a
# real board, and decision 13 of plan-1 stands — Go unit tests gate CI, the
# browser suite is a local ritual.
#
# It needs NO running server and NO PROJECT: the harness starts the engine
# in-process on a temp root it seeds itself with `init --example` plus the
# interaction fixture under test/e2e/testdata/. That is the whole reason it
# replaced test/smoke.sh, which had to be aimed at somebody's board and WROTE to
# it. Nothing here can touch a board you care about.
#
# First run downloads ~330 MB (the Playwright driver and Chromium) into
# ~/.cache/ms-playwright*; after that it is a few seconds of startup. The whole
# suite is ~1 min, so do not run it twice in one shell call.
#
#   make e2e                          headless
#   E2E_HEADED=1 make e2e             a visible browser, slowed down to watch
#   E2E_TRACE=always make e2e         keep a trace for the tests that PASSED too
#   E2E_KEEP=1 make e2e               keep the temp board after the run
#   E2E_RUN='TestKanban.*' make e2e   one test (the gesture-coverage gate is skipped)
#
# `e2e: build` is not a dependency of the tests — `go test` builds its own
# binary and the web tree is embedded in it. It is there so that a human staring
# at a failure has a current ./aboard to drive the same board by hand.
E2E_RUN ?= .

.PHONY: e2e
e2e: build         ## LOCAL ONLY: the real browser suite (playwright-go, //go:build e2e). No server or PROJECT needed.
	go test -tags e2e -count=1 -timeout 10m -run '$(E2E_RUN)' -v ./test/e2e

# SHOT_TABS is passed straight through: `make shot SHOT_TABS="bb133 bb22#help"`.
# With none, shot.sh shoots its default set. LOOK at the pictures — every visual
# regression this project has shipped passed the DOM assertions first.
# PROJECT picks the board, and here it is optional and defaults to this
# checkout: shot.sh only READS the board and writes pictures into its
# .aboard/run/shots/. It is the one shell script left in test/ — `make e2e`
# takes its own screenshots, but only of a temp board that is deleted, and
# looking at a picture of the board you are actually working on is a different
# job.
.PHONY: shot
shot:              ## Screenshot tabs into <project>/.aboard/run/shots/ (PROJECT=<dir> SHOT_TABS="bb1 bb22#help"); a running server is required.
	PROJECT="$(PROJECT)" ./test/shot.sh $(SHOT_TABS)

.PHONY: dev
dev:               ## Serve the UI from disk, so edits to pkg/aboard/web need no rebuild.
	go run ./cmd/aboard serve --dev

.PHONY: run
run: build         ## Build and serve with the embedded UI.
	./$(BIN) serve

.PHONY: status
status: build      ## What is running for this project, on which port, and is the skill stale.
	./$(BIN) status

.PHONY: ci-local
ci-local: test fmt-check lint govulncheck docs-check xcompile-windows snapshot ## Run every gate CI + release would run (Linux + Windows cross-compile + snapshot).
	@echo
	@echo "Local CI gates green. Safe to push + tag."
	@echo "Catches: Linux test failures, lint, vuln, doc links, Windows compile-time portability bugs, release-config regressions."
	@echo "Does NOT catch: Windows runtime behaviour (use a push-to-branch + GitHub Actions Windows runner for that)."
	@echo "Does NOT catch: anything only a browser sees. Run 'make caps' and check git diff is clean, then"
	@echo "                'make e2e' (the browser suite; needs no server) and 'make shot' against a"
	@echo "                running server — and look at the pictures."
