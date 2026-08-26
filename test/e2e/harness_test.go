//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/exoport/aboard/pkg/aboard"
)

// What the whole suite shares: one browser, one board, one server. Per-TEST
// isolation is a fresh BrowserContext (its own storage, its own page), not a
// fresh browser — launching Chromium costs about a second and there are forty
// tests.
//
// The BOARD is shared too, and that is the one genuinely awkward choice here.
// Each test could have had its own server and temp root, and it would be
// cleaner; it would also be forty `aboard init --example` seedings and forty
// ports. So the tests share a board and are written not to fight over it: each
// one touches its own tab where it can, and the few that must write to a shared
// one (the conflict tests, which need `bb202`'s note) put back what they found.
var (
	pw       *playwright.Playwright
	browser  playwright.Browser
	board    aboard.Root
	repo     aboard.Root
	boardURL string
	suiteLog *log.Logger
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// A linear startup script: install, seed, serve, launch, run, report.
func run(m *testing.M) int {
	suiteLog = log.New(os.Stderr, "e2e: ", 0)

	if err := installDriver(); err != nil {
		suiteLog.Printf("%v", err)
		return 1
	}
	suiteLog.Printf("playwright driver %s", driverVersion())

	// The repo root, for the stable artefact path. Found by walking up from the
	// test's own directory, exactly as every command does — this file lives at
	// <repo>/test/e2e, and .aboard/ in the repo is gitignored.
	var err error
	repo, err = aboard.FindRoot(".")
	if err != nil {
		// Not fatal. A checkout with no `.aboard/` is a perfectly good place to
		// run the suite; it only means failure artefacts stay in the temp root,
		// so say where that is instead of pretending.
		suiteLog.Printf("no board root above the test directory (%v) — failure artefacts stay in the temp root", err)
	}

	dir, err := os.MkdirTemp("", "aboard-e2e-")
	if err != nil {
		suiteLog.Printf("temp root: %v", err)
		return 1
	}
	defer func() {
		if os.Getenv("E2E_KEEP") == "1" {
			suiteLog.Printf("kept the board at %s (E2E_KEEP=1)", dir)
			return
		}
		_ = os.RemoveAll(dir)
	}()

	if err := seedBoard(dir); err != nil {
		suiteLog.Printf("seed: %v", err)
		return 1
	}
	board = aboard.Root(dir)
	if err := recordSeededTabs(board.StateFile("")); err != nil {
		suiteLog.Printf("reading the seeded board: %v", err)
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port, err := freePort()
	if err != nil {
		suiteLog.Printf("free port: %v", err)
		return 1
	}
	boardURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	served := make(chan error, 1)
	go func() {
		served <- aboard.Serve(ctx, aboard.Options{
			Host:   aboard.HostStandalone,
			Argv0:  "aboard",
			Logger: log.New(io.Discard, "", 0),
		}, aboard.ServeConfig{Root: board, Port: port})
	}()
	if err := waitForHealth(ctx, served); err != nil {
		suiteLog.Printf("server: %v", err)
		return 1
	}
	suiteLog.Printf("board at %s (root %s)", boardURL, dir)

	pw, err = playwright.Run()
	if err != nil {
		suiteLog.Printf("playwright run: %v", err)
		return 1
	}
	defer func() { _ = pw.Stop() }()

	opts := launchOptions()
	browser, err = pw.Chromium.Launch(opts)
	if err != nil {
		suiteLog.Printf("chromium launch: %v", err)
		return 1
	}
	defer func() { _ = browser.Close() }()
	suiteLog.Printf("chromium %s (headless=%v)", browser.Version(), *opts.Headless)

	code := m.Run()

	// The coverage gate runs AFTER the tests, because it is an assertion about
	// what they registered. It is skipped under -run, where a partial set is the
	// point rather than a gap — see reportGestureCoverage.
	if !reportGestureCoverage(suiteLog) && code == 0 {
		code = 1
	}
	return code
}

// seedBoard writes the board the suite drives: the embedded example, exactly as
// `aboard init --example` would, plus the interaction fixture laid over it.
func seedBoard(dir string) error {
	res, err := aboard.Init(aboard.InitConfig{Dir: dir, Example: true})
	if err != nil {
		return err
	}
	if err := applyFixture(res.StateFile); err != nil {
		return err
	}
	return seedLog(aboard.Root(dir))
}

// seedLog gives the `log` tab something to follow and filter. Written straight
// to the sidecar file rather than posted through /log, because the server is not
// up yet — and the sidecar is deliberately NOT in the state document, so there
// is nothing to keep in step.
func seedLog(root aboard.Root) error {
	if err := os.MkdirAll(root.LogsDir(), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	for i := 1; i <= 40; i++ {
		level := "info"
		switch {
		case i%9 == 0:
			level = "error"
		case i%5 == 0:
			level = "warn"
		}
		fmt.Fprintf(&b, "2026-08-26T09:%02d:00Z %s line %d of the seeded log\n", i, level, i)
	}
	// 0o644, like everything else the board writes: see the file-mode note in
	// init.go.
	return os.WriteFile(root.LogFile("bb126"), []byte(b.String()), 0o644)
}

// seededTabs is the set of tab ids the board STARTED with — the example plus the
// fixture, before any test ran.
//
// Tests make tabs of their own (the new-tab dialog, the scratch tabs the removal
// tests answer requests on), and a brand-new tab is legitimately empty: a markup
// tab with no images renders no <svg>, and asserting otherwise makes
// TestEveryTabActivatesAndRendersItsOwnOutput fail or pass on whether it happened
// to run before the dialog test. It did, in declaration order, and did not under
// -shuffle. So "it mounted" is asserted for every tab on the board, and "it
// produced its characteristic output" only for the tabs that were seeded with
// something to show.
var seededTabs = map[string]bool{}

func recordSeededTabs(stateFile string) error {
	raw, err := os.ReadFile(stateFile)
	if err != nil {
		return err
	}
	var d struct {
		Tabs []struct {
			ID string `json:"id"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return err
	}
	for _, tab := range d.Tabs {
		seededTabs[tab.ID] = true
	}
	if len(seededTabs) < 15 {
		return fmt.Errorf("the seeded board has only %d tabs; the example seeds one per renderer", len(seededTabs))
	}
	return nil
}

// freePort asks the kernel for one and gives it straight back. There is a window
// between the close and Serve's bind, and it is accepted: the alternative is
// port 0 and no way to tell the browser where to go, since Serve reports its
// chosen port through the instance file rather than a return value.
func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return 0, errors.New("the listener is not TCP")
	}
	if err := ln.Close(); err != nil {
		return 0, err
	}
	return addr.Port, nil
}

// waitForHealth polls until the board answers, or until Serve reports why it
// never will. Watching the served channel matters: a bind failure otherwise
// shows up as a ten-second timeout with no reason in it.
func waitForHealth(ctx context.Context, served <-chan error) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-served:
			if err == nil {
				err = errors.New("the server returned before it was ready")
			}
			return err
		default:
		}
		if aboard.ProbeBoard(ctx, portOf(boardURL), "") != nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("the board never answered /health")
}

func portOf(rawURL string) int {
	var port int
	if _, err := fmt.Sscanf(rawURL, "http://127.0.0.1:%d", &port); err != nil {
		return 0
	}
	return port
}

/* ---------- the board, over HTTP ---------- */

// httpClient is a package-level client with a real timeout: the default one has
// none, so a hung read would hang the whole run rather than fail one test.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// doc is the board document as the server has it. A map rather than a struct
// because the tests reach into tab state the engine deliberately does not type.
type doc map[string]any

// readDoc fetches /aboard.json. Every assertion about what landed goes through
// here rather than through the file on disk: the disk copy is written
// atomically, so a read racing a rename sees the old bytes with no error.
func readDoc(t *testing.T) doc {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, boardURL+"/aboard.json", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var d doc
	if err := json.NewDecoder(res.Body).Decode(&d); err != nil {
		t.Fatalf("decoding /aboard.json: %v", err)
	}
	return d
}

// getJSON reads a JSON endpoint into out. The board answers loopback only, so
// every URL here is built from boardURL rather than from a hostname.
func getJSON(t *testing.T, path string, out any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, boardURL+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
}

// tabOf finds a tab by id, failing rather than returning nil: every caller then
// dereferences it, and a nil map read gives "invalid memory address" instead of
// the id that was missing.
func (d doc) tab(t *testing.T, id string) map[string]any {
	t.Helper()
	list, _ := d["tabs"].([]any)
	for _, raw := range list {
		tab, ok := raw.(map[string]any)
		if ok && tab["id"] == id {
			return tab
		}
	}
	t.Fatalf("no tab %q on the board", id)
	return nil
}

// state is a tab's state, following stateFrom the way every renderer does.
func (d doc) state(t *testing.T, id string) map[string]any {
	t.Helper()
	tab := d.tab(t, id)
	if from, ok := tab["stateFrom"].(string); ok && from != "" {
		return d.state(t, from)
	}
	st, _ := tab["state"].(map[string]any)
	if st == nil {
		st = map[string]any{}
	}
	return st
}

// dig walks a path of object keys, returning nil at the first one that is not
// there. Reading `state.data.ticks` in one expression is the whole point: the
// alternative is four type assertions per assertion.
func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

// agentActor is who the suite writes as when it plays the OTHER session. Never
// "human": that word is not a label, it is the key every guarantee in tabs.go
// turns on, and a test that claimed it would be testing the human's powers while
// pretending to be an agent.
const agentActor = "agent-e2e"

// apply writes the whole document back as a second actor — the same route
// `aboard apply` uses, which is what makes "an agent wrote while you were
// typing" the real thing rather than a simulation of one.
//
// It sends no Origin header, which the server's cross-site rule allows and
// documents: the guard exists to stop a PAGE on another origin, and a program
// that wanted to lie could send any value at all.
func apply(t *testing.T, d doc) {
	t.Helper()

	payload := doc{}
	maps.Copy(payload, d)
	// The compare-and-set token is the revision the document carries, exactly as
	// pushDoc and `aboard apply` send it. A document with no rev has no base and
	// is refused, which is the behaviour, not a bug to work around here.
	//
	// Formatted the way client.go's applyBase formats it, and NOT with %v: `rev`
	// arrives from encoding/json as a float64, and %v renders a float64 through
	// %g — so the first revision past a million would be sent as "1e+06" and
	// every write in this suite would start failing compare-and-set with a
	// message about a stale base. It reads as a formatting nicety and it is a
	// correctness difference from the client this helper claims to imitate.
	payload["__base"] = revisionToken(t, d["rev"])
	payload["__by"] = agentActor
	payload["__origin"] = "e2e-" + agentActor

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		boardURL+"/aboard.json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		t.Fatalf("apply as %q: status %d: %s", agentActor, res.StatusCode, strings.TrimSpace(string(msg)))
	}
}

// revisionToken renders a decoded `rev` as the compare-and-set base, matching
// client.go's applyBase: a JSON number is an integer counter, never a float to
// be printed as one.
func revisionToken(t *testing.T, rev any) string {
	t.Helper()
	switch v := rev.(type) {
	case float64:
		return strconv.Itoa(int(v))
	case string:
		return strings.TrimSpace(v)
	default:
		t.Fatalf("the document carries no usable rev (%T %v)", rev, rev)
		return ""
	}
}

/* ---------- waiting ---------- */

// sprint renders a handful of decoded JSON values as one comparable string. Used
// where a test needs "did this change" rather than "is this exactly that" —
// pinning a resized box's arithmetic would pin the viewport size with it.
func sprint(values ...any) string { return fmt.Sprint(values...) }

// eventually polls until want returns true. Playwright's own assertions
// auto-retry inside the browser; this is for the other side — "has the write
// reached the server yet" — where there is nothing to attach a locator to.
//
// It reports the LAST value seen, because "eventually failed" without one sends
// the reader to a debugger for a fact the loop already had.
func eventually(t *testing.T, what string, want func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if want() {
			return
		}
		time.Sleep(60 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
