//go:build e2e

// Package e2e is the browser suite: a real Chromium, driven by Playwright,
// against a real board served in-process.
//
// It is behind the `e2e` build tag and reached through `make e2e`. It is never
// in GitHub CI — decision 13 of plan-1 stands, Go unit tests gate CI and the
// browser suite is a local ritual — and `go build ./...`, `go vet ./...` and
// `go test ./...` do not see this directory at all, because build constraints
// exclude every file in it. That is also why playwright-go never enters the
// shipped binary: `go list -deps ./cmd/aboard` does not mention it.
//
// Everything here is a _test.go file on purpose. The repo's rule is that no
// path is joined outside pkg/aboard/layout.go, enforced by an AST walk that
// parses every .go file in the tree REGARDLESS of build tags — so a plain
// harness.go behind an e2e tag would still be read by it. Test files are
// exempt by that rule's own design, and this whole package is tests.
package e2e

import (
	"fmt"
	"os"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

// The module path is github.com/mxschmitt/playwright-go and not
// .../playwright-community/..., which is the import path most documentation
// shows. The community fork kept the original module line in the go.mod it
// publishes at these tags, so `go get playwright-community/...` fails with
// "module declares its path as". This is that same library — the driver it
// downloads is Playwright CLI 1.62.1 — reached by the name it declares.

// runOptions is the single description of what this suite needs installed:
// chromium and nothing else. Firefox and WebKit are another ~500 MB no test
// here drives, and the board is only ever viewed in a Chromium webview (VS
// Code's Simple Browser), so a second engine would be testing a host this
// product does not have.
//
// The browser build is pinned by the playwright-go module version, which go.mod
// already pins. That is why there is no `cmd/playwright` entry in .bingo beside
// gofumpt and golangci-lint: those are host tools with no other pin, whereas a
// bingo pin here would be a SECOND statement of the same version number, free to
// disagree with go.mod and with nothing to notice when it did.
func runOptions() *playwright.RunOptions {
	return &playwright.RunOptions{
		Browsers: []string{"chromium"},
		// The headless shell is a separate ~90 MB download that only serves
		// `channel: "chromium-headless-shell"`. This suite runs the full browser
		// (headless by default, headed under E2E_HEADED=1), so the shell is
		// weight with no reader.
		NoInstallShell: true,
	}
}

// installDriver fetches the Playwright driver and Chromium when they are not
// already under ~/.cache. Safe to call when everything is present: it checks and
// returns in a few seconds. The first run downloads ~330 MB.
func installDriver() error {
	if err := playwright.Install(runOptions()); err != nil {
		return fmt.Errorf("playwright install: %w", err)
	}
	return nil
}

// driverVersion is the Playwright CLI version go.mod pins, reported in the
// suite's own log so a failure says which browser saw it.
func driverVersion() string {
	d, err := playwright.NewDriver(runOptions())
	if err != nil {
		return "unknown"
	}
	return d.Version
}

// launchOptions is how the browser is started.
//
// THE SANDBOXED-FRAME FALLBACK, and why it is not on:
//
// An html tab's frame is sandbox="allow-scripts" with no allow-same-origin, so
// it has an opaque origin, and Chrome's IsolateSandboxedIframes (on by default
// since ~M132) puts such a frame in its OWN PROCESS. Reaching into an
// out-of-process frame needs a separate CDP session per frame; Puppeteer
// disabled that isolation in its own test suite rather than deal with it.
//
// Playwright's FrameLocator handles it transparently, and it was measured here
// rather than assumed: the bridge tests (a tab's widget and an html block inside
// a stack) drive buttons INSIDE that frame and read the result back off disk,
// and they have not flaked. So the flag stays off — testing the browser the
// human actually uses is the point, and disabling process isolation would make
// the frame tests pass under a configuration nobody runs.
//
// If it ever does prove flaky, the escape hatch is:
//
//	--disable-features=IsolateSandboxedIframes
//
// with one trap worth writing down: Playwright passes its OWN --disable-features
// list, and Chromium takes the LAST occurrence of a switch rather than merging
// them, so appending a bare `--disable-features=IsolateSandboxedIframes` silently
// drops everything Playwright disabled. Append to the existing value instead of
// adding a second switch, and record what was observed that justified it.
//
// One note for anyone copying from the playwright-go documentation: every
// example there writes `playwright.Bool(x)` / `playwright.String(x)` for these
// *bool and *string option fields. Those helpers are two-line pointer takers and
// nothing more, so `new(x)` — Go 1.26's generalised new — is exactly the same
// value, and it is what this repo's linter requires. Do not add an exclusion to
// .golangci.yaml to keep the helper; write new(x).
func launchOptions() playwright.BrowserTypeLaunchOptions {
	headed := os.Getenv("E2E_HEADED") == "1"
	opts := playwright.BrowserTypeLaunchOptions{
		Headless: new(!headed),
		// Channel is what makes `NoInstallShell: true` above true rather than a
		// wish. Without it, a headless launch on this driver resolves to
		// `chrome-headless-shell` — the separate ~90 MB download runOptions()
		// deliberately skips — and the run dies before the first test with
		// "Executable doesn't exist at .../chrome-headless-shell", which reads
		// like a broken machine rather than a contradiction inside this file.
		// `chromium` names the full browser for headless and headed alike, which
		// is what this suite always meant to drive: the shell is the OLD headless
		// implementation, and this repo already has iframe-painting gotchas that
		// come from exactly that difference.
		Channel: new("chromium"),
	}
	if headed {
		// Enough to watch what the driver is doing without the run taking a
		// coffee break.
		opts.SlowMo = playwright.Float(120)
	}
	if extra := strings.TrimSpace(os.Getenv("E2E_CHROMIUM_ARGS")); extra != "" {
		opts.Args = strings.Fields(extra)
	}
	return opts
}

// traceAlways keeps a trace for a test that PASSED. Off by default: a trace per
// test is a few MB and the suite is ~40 tests, so keeping them all turns a green
// run into a disk write nobody reads. Failures always keep theirs.
func traceAlways() bool { return os.Getenv("E2E_TRACE") == "always" }
