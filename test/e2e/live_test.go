//go:build e2e

package e2e

import (
	"context"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/exoport/aboard/pkg/aboard/web"
)

// The live half: the SSE stream, and the two things it carries besides a state
// change. The old suite switched SSE OFF (`?nosse=1`) so a one-shot headless
// chromium could reach network-idle, and then checked the stream with curl —
// which exercised the server and none of the page. Everything here is the page.

// A second actor writes; the open page shows it. No reload, no polling, no
// gesture — that is what makes the board a channel rather than a file two
// programs happen to share.
func TestASecondActorsWriteAppearsWithoutAReload(t *testing.T) {
	s := open(t, "tab=ab202")

	s.markPage()

	written := "written by another session at " + time.Now().Format(time.RFC3339Nano)
	d := readDoc(t)
	d.state(t, "ab202")["text"] = written
	apply(t, d)

	if err := expect.Locator(s.view("ab202").Locator(".notes-area")).ToHaveValue(written); err != nil {
		t.Fatalf("the other session's write never reached the open page: %v", err)
	}
	// The change is announced, not silent: an agent editing the tab you are
	// reading is something you are told about.
	if err := expect.Locator(s.view("ab202").Locator(".banner")).ToContainText("agent-e2e changed this tab"); err != nil {
		t.Errorf("nothing said who changed the tab: %v", err)
	}
	if s.pageReloaded() {
		t.Error("the page reloaded instead of updating in place")
	}
}

// The board's own page must NOT reload on its own write echoing back — the frame
// carries the origin that caused it, and a page that reloaded on its own edits
// would fight the human's typing.
func TestThePageIgnoresItsOwnWriteComingBack(t *testing.T) {
	s := open(t, "tab=ab202")
	s.markPage()

	area := s.view("ab202").Locator(".notes-area")
	if err := area.Fill("typed here, echoed back"); err != nil {
		t.Fatalf("typing: %v", err)
	}
	eventually(t, "the edit to reach the server", func() bool {
		text, _ := readDoc(t).state(t, "ab202")["text"].(string)
		return text == "typed here, echoed back"
	})
	time.Sleep(settle)

	if s.pageReloaded() {
		t.Error("the page reloaded on its own write coming back down the stream")
	}
	if err := expect.Locator(area).ToHaveValue("typed here, echoed back"); err != nil {
		t.Errorf("the page lost what was typed into it: %v", err)
	}
}

// --dev: the UI signature, and the two different things it costs.
//
// A stylesheet change RE-LINKS app.css and keeps your scroll position, your
// selection and your half-typed sentence. Anything else reloads, and even that
// waits for blur if the focus is in an editable — losing a sentence to a
// stylesheet edit would be worse than the staleness.
//
// This needs its own server, because the signature is only recomputed under
// --dev: with the embedded tree the files live inside the process and cannot
// change, so the watcher does not run at all. The web tree is copied out of the
// EMBEDDED FS rather than pointed at the repo's own pkg/aboard/web, so the test
// edits a throwaway copy — a suite that writes to the tree it is testing is one
// interrupted run away from a dirty checkout.
func TestADevStylesheetChangeRelinksWithoutReloading(t *testing.T) {
	dev := startDevBoard(t)

	s := openAt(t, dev.url, "tab=ab202")
	s.markPage()
	link := s.page.Locator(`link[rel="stylesheet"]`).Last()
	before, err := link.GetAttribute("href")
	if err != nil {
		t.Fatal(err)
	}

	appendToFile(t, aboard.DevWebFile(dev.webDir, "app.css"), "\n/* touched by the browser suite */\n")

	// The re-link appends ?v=<css hash>, so the new sheet is a different URL and
	// the browser actually fetches it.
	if err := expect.Locator(s.page.Locator(`link[rel="stylesheet"][href*="?v="]`)).
		ToBeAttached(); err != nil {
		t.Fatalf("the stylesheet was never re-linked: %v (href was %q)", err, before)
	}
	if s.pageReloaded() {
		t.Error("a CSS-only change reloaded the page — scroll, selection and any half-typed sentence are gone")
	}
}

// A change to the html or the JS is not survivable in place, so the page reloads
// itself. That is what makes a `--force` restart self-healing and why nobody has
// to be told to run "Developer: Reload Webviews" any more.
func TestADevCodeChangeReloadsThePage(t *testing.T) {
	dev := startDevBoard(t)

	s := openAt(t, dev.url, "tab=ab202")
	s.markPage()

	appendToFile(t, aboard.DevWebFile(dev.webDir, "aboard.html"), "\n<!-- touched by the browser suite -->\n")

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if s.pageReloaded() {
			return // the marker did not survive, so the page reloaded itself
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("a change to aboard.html did not reload the open page — it is still running the old code")
}

/* ---------- a second, --dev board ---------- */

type devBoard struct {
	url    string
	webDir string
}

// startDevBoard seeds a fresh project, copies the embedded web tree beside it,
// and serves the two together with --dev. Its own root, because `aboard serve`
// refuses a second board for the same project — which is the rule, not an
// obstacle to route around.
func startDevBoard(t *testing.T) devBoard {
	t.Helper()

	dir := t.TempDir()
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir, Example: true}); err != nil {
		t.Fatalf("seeding the dev board: %v", err)
	}
	if err := applyFixture(aboard.Root(dir).StateFile("")); err != nil {
		t.Fatalf("applying the fixture: %v", err)
	}

	webDir := t.TempDir()
	if err := os.CopyFS(webDir, web.FS); err != nil {
		t.Fatalf("copying the web tree out of the binary: %v", err)
	}
	// os.CopyFS preserves the read-only modes an embedded FS reports, and the
	// whole point here is to edit two of these files.
	if err := makeWritable(webDir); err != nil {
		t.Fatalf("making the copied tree writable: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	url := "http://127.0.0.1:" + strconv.Itoa(port)

	served := make(chan error, 1)
	go func() {
		served <- aboard.Serve(ctx, aboard.Options{
			Host: aboard.HostStandalone, Argv0: "aboard", Logger: log.New(io.Discard, "", 0),
		}, aboard.ServeConfig{Root: aboard.Root(dir), Port: port, Dev: true, DevDir: webDir})
	}()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-served:
			t.Fatalf("the dev board stopped: %v", err)
		default:
		}
		if aboard.ProbeBoard(ctx, port, "") != nil {
			return devBoard{url: url, webDir: webDir}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the dev board never answered /health")
	return devBoard{}
}

// makeWritable clears the read-only modes os.CopyFS carries over from the
// embedded FS — embed reports every file as 0444, and this test exists to edit
// two of them.
func makeWritable(dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if d.IsDir() {
			mode = 0o755
		}
		return os.Chmod(path, mode)
	})
}

func appendToFile(t *testing.T, path, text string) {
	t.Helper()
	// A throwaway copy of the web tree under t.TempDir().
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(text); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
