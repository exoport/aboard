//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/exoport/aboard/pkg/aboard"
)

// expect is Playwright's auto-retrying assertion factory. Five seconds, which is
// generous for a local server and short enough that a genuinely stuck test fails
// inside the go test timeout rather than at it.
var expect = playwright.NewPlaywrightAssertions(5000)

// session is one test's browser: its own context (own storage, own page), its
// own trace, and its own console log.
type session struct {
	t    *testing.T
	name string
	ctx  playwright.BrowserContext
	page playwright.Page

	// loads counts `load` events. A reload is otherwise invisible: "the page
	// updated in place" and "the page threw everything away and re-read it" end
	// with the same DOM, and the difference is the whole behaviour under test in
	// the SSE and --dev cases.
	loads    atomic.Int64
	markedAt int64

	mu      sync.Mutex
	console []string
}

// open loads the board in a fresh context.
//
// SSE is ON — no `?nosse=1`. That flag exists so a one-shot headless screenshot
// can reach network-idle, and switching the live stream off is exactly how the
// old suite managed to test everything about the board except the half that
// makes it live. Playwright does not wait for network-idle by default, so
// nothing here needs the flag.
func open(t *testing.T, query string) *session {
	t.Helper()
	return openAt(t, boardURL, query)
}

// openAt is open against a board other than the suite's own — the --dev server
// the live-reload tests start for themselves.
func openAt(t *testing.T, base, query string) *session {
	t.Helper()
	return openReady(t, base, query, "#tabs .tab")
}

// openReady is openAt with the readiness check named, because "the board is on
// screen" is not the same sentence on every page: under `?chrome=notabs` the tab
// strip is deliberately hidden, so waiting for it to become VISIBLE waits for
// something that will never happen and the failure reads as a broken board
// rather than as the chrome doing its job.
func openReady(t *testing.T, base, query, ready string) *session {
	t.Helper()

	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{Width: 1400, Height: 1000},
	})
	if err != nil {
		t.Fatalf("new browser context: %v", err)
	}
	if err := ctx.Tracing().Start(playwright.TracingStartOptions{
		Screenshots: new(true),
		Snapshots:   new(true),
		Sources:     new(true),
		Title:       new(t.Name()),
	}); err != nil {
		t.Fatalf("start tracing: %v", err)
	}

	// Eight seconds, not Playwright's thirty. Every server in this suite is
	// local and in-process, so nothing legitimately takes longer — and the
	// default turns one wrong selector into half a minute of a run that has
	// forty tests in it.
	ctx.SetDefaultTimeout(8000)

	page, err := ctx.NewPage()
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	s := &session{t: t, name: safeName(t.Name()), ctx: ctx, page: page}

	page.OnLoad(func(playwright.Page) { s.loads.Add(1) })
	page.OnConsole(func(m playwright.ConsoleMessage) { s.record("console." + m.Type() + ": " + m.Text()) })
	page.OnPageError(func(err error) { s.record("pageerror: " + err.Error()) })
	page.OnRequestFailed(func(r playwright.Request) {
		// The board is entirely local, so a failed request is a real defect and
		// not a flaky CDN. Recorded rather than failed on: an aborted SSE stream
		// at teardown is normal and would otherwise fail every test.
		s.record("requestfailed: " + r.URL())
	})

	// A dialog with no handler is auto-dismissed by Playwright, which turns a
	// `confirm()` into a silent "Cancel" and a `prompt()` into null — i.e. the
	// gesture appears to do nothing and the test fails somewhere else entirely.
	// So the default here is LOUD: dismiss it, and say in the console log that
	// nothing had claimed it. Tests that mean to answer one call s.onDialog.
	s.setDialog(func(d playwright.Dialog) {
		s.record(fmt.Sprintf("unclaimed dialog (%s): %q — dismissed", d.Type(), d.Message()))
		_ = d.Dismiss()
	})

	url := base + "/"
	if query != "" {
		url += "?" + query
	}
	if _, err := page.Goto(url); err != nil {
		t.Fatalf("goto %s: %v", url, err)
	}
	// The shell builds its tab strip after the first fetch, so "the page loaded"
	// is not "the board is on screen".
	if err := page.Locator(ready).First().WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		t.Fatalf("%s never appeared: %v", ready, err)
	}

	t.Cleanup(s.finish)
	return s
}

func (s *session) record(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.console = append(s.console, line)
}

// setDialog REPLACES the handler rather than adding one. Playwright calls every
// registered dialog listener, and the first to answer wins — so a test that only
// added its own would be racing the loud default below, and the loser's Accept
// silently fails on an already-answered dialog.
func (s *session) setDialog(fn func(playwright.Dialog)) {
	s.page.RemoveListeners("dialog")
	s.page.OnDialog(fn)
}

// onDialog answers the next native dialog and records what it said, so a test
// can assert on the MESSAGE — "Remove the tab …? Its content is deleted." is a
// sentence the human reads before an irreversible action, and it is worth
// pinning.
func (s *session) onDialog(accept bool, promptText string) *dialogRecord {
	rec := &dialogRecord{}
	s.setDialog(func(d playwright.Dialog) {
		rec.mu.Lock()
		rec.seen = append(rec.seen, dialogSeen{Kind: d.Type(), Message: d.Message()})
		rec.mu.Unlock()
		s.record(fmt.Sprintf("dialog (%s): %q -> accept=%v", d.Type(), d.Message(), accept))
		if accept {
			if promptText != "" {
				_ = d.Accept(promptText)
			} else {
				_ = d.Accept()
			}
			return
		}
		_ = d.Dismiss()
	})
	return rec
}

type dialogSeen struct {
	Kind    string
	Message string
}

type dialogRecord struct {
	mu   sync.Mutex
	seen []dialogSeen
}

func (r *dialogRecord) all() []dialogSeen {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]dialogSeen(nil), r.seen...)
}

// only returns the single dialog that was expected, failing when none or several
// arrived — "the confirm never appeared" is a different defect from "two did".
func (r *dialogRecord) only(t *testing.T) dialogSeen {
	t.Helper()
	seen := r.all()
	if len(seen) != 1 {
		t.Fatalf("expected exactly one native dialog, saw %d: %+v", len(seen), seen)
	}
	return seen[0]
}

/* ---------- locating things ---------- */

// tab activates a tab by id and waits until its view is the active one. Ids are
// what everything else in this project addresses a tab by, so the helper takes
// one; the NAME is what the human reads and it is free to change.
func (s *session) tab(id string) playwright.Locator {
	s.t.Helper()
	if err := s.page.Locator(`#tabs .tab[data-id="` + id + `"]`).Click(); err != nil {
		s.t.Fatalf("clicking tab %s: %v", id, err)
	}
	view := s.view(id)
	if err := view.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		s.t.Fatalf("tab %s never became the active view: %v", id, err)
	}
	return view
}

// view is a tab's mounted renderer root, whether or not it is active. Every view
// root carries data-tab and data-view; the shell sets data-active on the one on
// screen.
func (s *session) view(id string) playwright.Locator {
	return s.page.Locator(`[data-tab="` + id + `"][data-active="yes"]`)
}

// control finds a DECLARED control inside a tab's view, by the id its
// views/<type>.spec.json gives it. This is the whole reason `data-gesture`
// exists: a label can be reworded and a title rewritten for a different reader,
// and neither breaks a test.
func (s *session) control(tabID, control string) playwright.Locator {
	return s.view(tabID).Locator(`[data-gesture="` + control + `"]`)
}

// widget reaches into an html tab's sandboxed frame.
//
// The frame is sandbox="allow-scripts" with no allow-same-origin, so it has an
// opaque origin and Chrome runs it OUT OF PROCESS (IsolateSandboxedIframes,
// default since ~M132). FrameLocator crosses that boundary transparently — which
// is the single largest reason this suite is Playwright and not a hand-rolled
// CDP client, where the same reach needs a separate session per frame.
func (s *session) widget(tabID string) playwright.FrameLocator {
	return s.view(tabID).FrameLocator("iframe")
}

/* ---------- gestures ---------- */

// dragPointer composes a pointer-capture drag: down on the source, several
// moves, up on the target.
//
// The moves are not decoration. dag, markup and the sketch canvas all use
// setPointerCapture and act on pointermove, and a single jump from down to up
// produces no intermediate event at all — the drag registers as a click. Six
// steps is enough for markup's pen (which thins points below a minimum distance)
// and cheap enough not to slow the suite.
//
// Kanban does NOT use this: it is HTML5 drag-and-drop, which synthesises its own
// event sequence and needs Locator.DragTo. Two drag models in one app, and using
// the wrong helper fails in a way that looks like the feature is broken.
func (s *session) dragPointer(from, to point) {
	s.t.Helper()
	mouse := s.page.Mouse()
	if err := mouse.Move(from.X, from.Y); err != nil {
		s.t.Fatalf("pointer move to source: %v", err)
	}
	if err := mouse.Down(); err != nil {
		s.t.Fatalf("pointer down: %v", err)
	}
	const steps = 6
	for i := 1; i <= steps; i++ {
		f := float64(i) / steps
		if err := mouse.Move(from.X+(to.X-from.X)*f, from.Y+(to.Y-from.Y)*f); err != nil {
			s.t.Fatalf("pointer move %d: %v", i, err)
		}
	}
	if err := mouse.Up(); err != nil {
		s.t.Fatalf("pointer up: %v", err)
	}
}

type point struct{ X, Y float64 }

// centre of a locator, in viewport pixels — what the mouse helpers take.
func (s *session) centre(l playwright.Locator) point {
	s.t.Helper()
	box, err := l.BoundingBox()
	if err != nil || box == nil {
		s.t.Fatalf("no bounding box: %v", err)
	}
	return point{X: box.X + box.Width/2, Y: box.Y + box.Height/2}
}

// at is a point inside a locator given as fractions of its own box, so a test
// can say "a fifth in from the left" without knowing the pixel size.
func (s *session) at(l playwright.Locator, fx, fy float64) point {
	s.t.Helper()
	box, err := l.BoundingBox()
	if err != nil || box == nil {
		s.t.Fatalf("no bounding box: %v", err)
	}
	return point{X: box.X + box.Width*fx, Y: box.Y + box.Height*fy}
}

// wheel scrolls over a locator — dag's zoom listens for it on the svg.
func (s *session) wheel(l playwright.Locator, dy float64) {
	s.t.Helper()
	c := s.centre(l)
	if err := s.page.Mouse().Move(c.X, c.Y); err != nil {
		s.t.Fatalf("moving to wheel target: %v", err)
	}
	if err := s.page.Mouse().Wheel(0, dy); err != nil {
		s.t.Fatalf("wheel: %v", err)
	}
}

/* ---------- reading the page ---------- */

// evalJSON evaluates an expression in the page and decodes the result into out.
// Used for the probe seam, where the interesting values are the shell's own
// objects rather than anything in the DOM.
func (s *session) evalJSON(out any, expression string, args ...any) {
	s.t.Helper()
	// Arguments go through JSON on the way in as well as on the way out.
	// playwright-go's serialiser walks a value by reflection and panics on
	// anything but map[string]any — a map[string]string is a panic, not an
	// error — so normalising here is what keeps a typo in a test from taking
	// the whole run down with a stack trace.
	v, err := s.page.Evaluate(expression, jsonable(s.t, args)...)
	if err != nil {
		s.t.Fatalf("evaluating %s: %v", expression, err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		s.t.Fatalf("re-encoding the result of %s: %v", expression, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		s.t.Fatalf("decoding the result of %s: %v", expression, err)
	}
}

// jsonable re-encodes each argument through JSON, so a Go map, slice or struct
// arrives in the page as the plain object it marshals to.
func jsonable(t *testing.T, args []any) []any {
	t.Helper()
	out := make([]any, 0, len(args))
	for _, arg := range args {
		raw, err := json.Marshal(arg)
		if err != nil {
			t.Fatalf("encoding an evaluate argument: %v", err)
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("decoding an evaluate argument: %v", err)
		}
		out = append(out, v)
	}
	return out
}

func (s *session) evalBool(expression string, args ...any) bool {
	s.t.Helper()
	var b bool
	s.evalJSON(&b, expression, args...)
	return b
}

// markPage records how many times this page has loaded, so pageReloaded can say
// whether it has loaded again since.
//
// Counted rather than probed. The first version stamped a value on `window` and
// polled for its absence, and it flaked: the reload it was waiting for landed
// mid-Evaluate and Playwright answered "Execution context was destroyed, most
// likely because of a navigation" — which is the event, reported as an error, to
// a call that had no way to tell it apart from a real failure. A load event
// cannot be raced.
func (s *session) markPage() {
	s.t.Helper()
	s.markedAt = s.loads.Load()
}

// pageReloaded reports whether the page has loaded again since markPage.
func (s *session) pageReloaded() bool {
	return s.loads.Load() > s.markedAt
}

/* ---------- failure artefacts ---------- */

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeName(name string) string { return unsafeName.ReplaceAllString(name, "_") }

// finish stops the trace and, on failure, writes everything a human needs to see
// what happened: the trace, a screenshot, the console log and the board document
// as it stood.
//
// Written TWICE — under the temporary board, where the run happened, and under
// the repo, because the temporary root is deleted when the process ends and an
// artefact nobody can find is not an artefact.
func (s *session) finish() {
	keep := s.t.Failed() || traceAlways()

	var dirs []string
	if keep {
		for _, root := range []aboard.Root{board, repo} {
			if root == "" {
				continue
			}
			dir := root.E2ECase(s.name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				s.t.Logf("could not make %s: %v", dir, err)
				continue
			}
			dirs = append(dirs, dir)
		}
	}

	tracePath := ""
	if len(dirs) > 0 {
		tracePath = filepath.Join(dirs[0], "trace.zip")
	}
	if err := s.ctx.Tracing().Stop(tracePath); err != nil {
		s.t.Logf("stopping the trace: %v", err)
	}

	if keep && len(dirs) > 0 {
		s.writeArtefacts(dirs, tracePath)
	}

	if err := s.ctx.Close(); err != nil {
		s.t.Logf("closing the context: %v", err)
	}
}

// writeArtefacts drops the same set into every directory: the trace (already
// written into the first one by Tracing.Stop), a full-page screenshot, the board
// document, and this page's console.
func (s *session) writeArtefacts(dirs []string, tracePath string) {
	shot, err := s.page.Screenshot(playwright.PageScreenshotOptions{FullPage: new(true)})
	if err != nil {
		s.t.Logf("screenshot: %v", err)
	}
	snapshot, _ := os.ReadFile(board.StateFile(""))

	s.mu.Lock()
	console := strings.Join(s.console, "\n")
	s.mu.Unlock()

	for i, dir := range dirs {
		if i > 0 && tracePath != "" {
			if raw, err := os.ReadFile(tracePath); err == nil {
				writeArtefact(s.t, dir, "trace.zip", raw)
			}
		}
		if shot != nil {
			writeArtefact(s.t, dir, "screen.png", shot)
		}
		if snapshot != nil {
			writeArtefact(s.t, dir, "aboard.json", snapshot)
		}
		writeArtefact(s.t, dir, "console.log", []byte(console+"\n"))
	}
	s.t.Logf("artefacts: %s", strings.Join(dirs, " and "))
	s.t.Logf("open the trace with: npx playwright show-trace %s", tracePath)
}

func writeArtefact(t *testing.T, dir, name string, body []byte) {
	t.Helper()
	// 0o644: the board's own file mode, see the note in init.go.
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		t.Logf("writing %s: %v", name, err)
	}
}

// settle gives a debounced save time to fire and land. 250 ms is the shell's
// debounce and html.js's bridge debounce; 400 ms is notes and the html source
// editor. Doubling the longest of those and rounding up is what this is.
const settle = 900 * time.Millisecond
