package aboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// asFakeBrowser turns this test binary into a stand-in for chromium, so `shot`
// can be tested end to end on every platform CI runs, none of which has a
// browser: the child is this same file, and TestMain hands it to fakeBrowser.
const (
	asFakeBrowser   = "ABOARD_TEST_AS_FAKE_BROWSER"
	fakeBrowserLog  = "ABOARD_TEST_FAKE_BROWSER_LOG"
	fakeBrowserFail = "ABOARD_TEST_FAKE_BROWSER_FAIL"
	// fakeBrowserReport is the report the fake page writes into itself, which
	// --dump-dom prints; unset, the page is one from a board too old to write one.
	fakeBrowserReport = "ABOARD_TEST_FAKE_BROWSER_REPORT"
	fakePicture       = "\x89PNG fake picture"
)

func TestMain(m *testing.M) {
	if os.Getenv(asFakeBrowser) == "1" {
		os.Exit(fakeBrowser(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeBrowser does what `chromium --screenshot=<path> <url>` does, minus the
// browser: it records how it was called and writes a picture — or, asked to
// fail, writes nothing and says why the way a browser does, on stderr.
func fakeBrowser(args []string) int {
	if log := os.Getenv(fakeBrowserLog); log != "" {
		line, err := json.Marshal(args)
		if err != nil {
			return 1
		}
		f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.Write(append(line, '\n'))
			_ = f.Close()
		}
	}
	if os.Getenv(fakeBrowserFail) == "1" {
		fmt.Fprintln(os.Stderr, "boom: the fake browser was told to fail")
		return 1
	}
	for _, arg := range args {
		if path, ok := strings.CutPrefix(arg, "--screenshot="); ok {
			if err := os.WriteFile(path, []byte(fakePicture), 0o600); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
	}
	if slices.Contains(args, "--dump-dom") {
		fmt.Print(`<html><head></head><body><div id="views">...</div>`)
		if report := os.Getenv(fakeBrowserReport); report != "" {
			fmt.Printf(`<script type="application/json" id="aboard-shot-report">%s</script>`, report)
		}
		fmt.Print(`</body></html>`)
	}
	return 0
}

// shotBoard is a running board as far as `shot` can tell: an instance record,
// a /health that answers as this project's board, and a document.
func shotBoard(t *testing.T) Root {
	t.Helper()
	root := Root(t.TempDir())
	var stub *httptest.Server
	stub = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(Instance{App: HostStandalone, Project: root.String(), URL: stub.URL, PID: 4242})
		case "/aboard.json":
			_, _ = w.Write([]byte(shotDoc))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(stub.Close)
	writeInstanceRecord(t, root, "", serverPort(t, stub.URL), 4242)
	return root
}

const shotDoc = `{"version":1,"rev":3,"nextId":20,"tabs":[
  {"id":"ab1","key":"work","name":"Build queue","type":"kanban","state":{"nodes":[{"id":"ab7","title":"a card"}]}},
  {"id":"ab2","name":"Review","type":"ui","state":{"data":{"t":{"summary":"Summary"}},"root":{"type":"col","children":[
    {"type":"tabs","panels":[
      {"label":"Overview","children":[{"type":"text","value":"hello"}]},
      {"label":{"bind":"t.summary"},"children":[{"type":"notice","id":"far","value":"down here"}]}
    ]}
  ]}}},
  {"id":"ab3","name":"Widget","type":"html","state":{"html":"<p>hi</p>"}}
]}`

// fakeShot runs Shot with this test binary as the browser, and returns what the
// "browser" was called with, one argv per picture.
func fakeShot(t *testing.T, root Root, opts ShotOptions) ([]ShotResult, [][]string, error) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv(asFakeBrowser, "1")
	t.Setenv(fakeBrowserLog, log)
	opts.Browser = exe
	results, shotErr := Shot(context.Background(), root, "", opts, DefaultInvocation)

	var calls [][]string
	if body, err := os.ReadFile(log); err == nil {
		for line := range strings.SplitSeq(strings.TrimSpace(string(body)), "\n") {
			var argv []string
			if json.Unmarshal([]byte(line), &argv) == nil {
				calls = append(calls, argv)
			}
		}
	}
	return results, calls, shotErr
}

func lastArg(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	return argv[len(argv)-1]
}

// The whole contract in one run: the tab is resolved by its KEY, the page is
// loaded with the three parameters a headless picture needs, the picture lands
// in the project, and a stale picture from an earlier run is not reported as
// this one's.
func TestAShotIsOfTheTabAskedForAndReplacesTheLastOne(t *testing.T) {
	root := shotBoard(t)
	if err := os.MkdirAll(root.ShotsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.ShotFile("ab1"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	results, calls, err := fakeShot(t, root, ShotOptions{Targets: []string{"work"}})
	if err != nil {
		t.Fatalf("shot: %v", err)
	}
	if len(results) != 1 || results[0].Tab != "ab1" || results[0].Path != root.ShotFile("ab1") {
		t.Fatalf("results = %+v", results)
	}
	body, err := os.ReadFile(root.ShotFile("ab1"))
	if err != nil || string(body) != fakePicture {
		t.Fatalf("the picture on disk is %q (%v), not this run's", body, err)
	}
	if len(calls) != 1 {
		t.Fatalf("the browser ran %d times, want 1", len(calls))
	}
	argv := calls[0]
	for _, want := range []string{"--headless", "--window-size=1400,900"} {
		if !containsString(argv, want) {
			t.Errorf("the browser was not given %s: %v", want, argv)
		}
	}
	addr := lastArg(argv)
	for _, want := range []string{"nosse=1", "shot=1", "tab=ab1"} {
		if !strings.Contains(addr, want) {
			t.Errorf("the page was loaded without %s: %s", want, addr)
		}
	}
}

// A ui tab with panels: without --node the picture is of the FIRST panel, and
// the result says so; with one — here a label that is itself a {bind} — the
// deep link asks for it and the file is named for it, so two panels are two
// files.
func TestAShotOfAPanelledTabSaysWhichPanelItShows(t *testing.T) {
	root := shotBoard(t)

	results, _, err := fakeShot(t, root, ShotOptions{Targets: []string{"ab2"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(results[0].Note, "FIRST panel") {
		t.Errorf("a panelled tab shot without --node does not say it shows the first panel: %+v", results[0])
	}

	results, calls, err := fakeShot(t, root, ShotOptions{Targets: []string{"ab2"}, Node: "Summary"})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Note != "" {
		t.Errorf("a panel that was asked for still carries the first-panel note: %q", results[0].Note)
	}
	if results[0].Path != root.ShotFile("ab2-Summary") {
		t.Errorf("the panel's picture went to %s", results[0].Path)
	}
	if addr := lastArg(calls[0]); !strings.Contains(addr, "node=Summary") {
		t.Errorf("the deep link does not name the panel: %s", addr)
	}
}

// A node that is not on the tab is refused BEFORE the browser runs. The shell
// would open the tab and show whatever comes first, which is a picture that
// looks exactly like success.
func TestAShotRefusesANodeTheTabDoesNotHave(t *testing.T) {
	root := shotBoard(t)
	_, calls, err := fakeShot(t, root, ShotOptions{Targets: []string{"ab2"}, Node: "Nowhere"})
	if err == nil || !strings.Contains(err.Error(), `"Nowhere"`) {
		t.Fatalf("an unknown node was not refused by name: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("the browser ran %d times for a refused node", len(calls))
	}

	// A node id counts on any tab type, because that is what every renderer's
	// focus() looks up; a panel LABEL only means something on a ui tab.
	if _, _, err := fakeShot(t, root, ShotOptions{Targets: []string{"ab1"}, Node: "ab7"}); err != nil {
		t.Errorf("a kanban card's id was refused: %v", err)
	}
	if _, _, err := fakeShot(t, root, ShotOptions{Targets: []string{"ab2"}, Node: "far"}); err != nil {
		t.Errorf("a ui node's id was refused: %v", err)
	}
}

// Every target resolves before anything is shot: one typo among several costs
// the message and nothing else, and the message lists what IS there.
func TestAShotOfAnUnknownTabRunsNoBrowser(t *testing.T) {
	root := shotBoard(t)
	_, calls, err := fakeShot(t, root, ShotOptions{Targets: []string{"ab1", "nosuch"}})
	if err == nil || !strings.Contains(err.Error(), `no tab "nosuch"`) || !strings.Contains(err.Error(), "ab2") {
		t.Fatalf("the refusal should name the target and list the board's tabs: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("the browser ran %d times before the typo was noticed", len(calls))
	}
}

// Two names for one tab are one picture.
func TestAShotTakesOnePicturePerTab(t *testing.T) {
	root := shotBoard(t)
	results, calls, err := fakeShot(t, root, ShotOptions{Targets: []string{"ab1", "work", "kanban"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(calls) != 1 {
		t.Errorf("three names for ab1 gave %d results and %d browser runs, want 1 and 1", len(results), len(calls))
	}
}

// An html tab is shot on its own route: headless chromium does not reliably
// paint the frame inside the shell.
func TestAShotOfAnHTMLTabIsOfTheWidget(t *testing.T) {
	root := shotBoard(t)
	results, calls, err := fakeShot(t, root, ShotOptions{Targets: []string{"ab3"}})
	if err != nil {
		t.Fatal(err)
	}
	if addr := lastArg(calls[0]); !strings.HasSuffix(addr, "/tab/ab3/html") {
		t.Errorf("an html tab was loaded at %s", addr)
	}
	if !strings.Contains(results[0].Note, "widget") {
		t.Errorf("the result does not say it shows the widget alone: %+v", results[0])
	}
}

// A browser that writes nothing is a FAILED shot with the browser's own words
// in it, never a path to a file that is not there.
func TestAShotThatWritesNothingFails(t *testing.T) {
	root := shotBoard(t)
	t.Setenv(fakeBrowserFail, "1")
	results, _, err := fakeShot(t, root, ShotOptions{Targets: []string{"ab1"}})
	if !errors.Is(err, ErrShotsFailed) {
		t.Fatalf("err = %v, want ErrShotsFailed", err)
	}
	if len(results) != 1 || results[0].Path != "" || !strings.Contains(results[0].Error, "boom") {
		t.Errorf("the failure should carry no path and the browser's reason: %+v", results)
	}
}

func TestAShotWithNoBoardSaysHowToStartOne(t *testing.T) {
	_, err := Shot(context.Background(), Root(t.TempDir()), "", ShotOptions{Targets: []string{"ab1"}}, DefaultInvocation)
	if err == nil || !strings.Contains(err.Error(), "aboard serve") {
		t.Fatalf("err = %v, want it to name `aboard serve`", err)
	}
}

func TestShotNamesAreFilenames(t *testing.T) {
	for _, tc := range []struct {
		opts ShotOptions
		want string
	}{
		{ShotOptions{}, "ab2"},
		{ShotOptions{Node: "Held back", Theme: "light"}, "ab2-Held_back-light"},
		{ShotOptions{Node: "../../etc", HelpPanel: true}, "ab2-.._.._etc-help"},
	} {
		if got := shotName("ab2", tc.opts); got != tc.want {
			t.Errorf("shotName(%+v) = %q, want %q", tc.opts, got, tc.want)
		}
	}
}

func TestANamedBrowserThatIsNotThereIsNamedInTheError(t *testing.T) {
	_, err := FindBrowser("no-such-browser-anywhere")
	if err == nil || !strings.Contains(err.Error(), "no-such-browser-anywhere") {
		t.Fatalf("err = %v", err)
	}
}

// What the headless page measured comes back with the picture: the width it was
// taken at, the boxes that did not fit, and any unknown-component marker. The
// report is read out of the same chromium run's --dump-dom, so a DOM without one
// — a board served by an older binary — says so instead of reporting a clean
// page it never measured.
func TestAShotReportsWhatDidNotFitInItsWindow(t *testing.T) {
	root := shotBoard(t)
	t.Setenv(fakeBrowserReport, `{"width":700,"unknown":["sparkline"],"clipped":[`+
		`{"where":"code (panel Wide)","kind":"scroll","x":2864},{"where":"text \u003cb\u003e","kind":"spill","x":35}]}`)
	results, calls, err := fakeShot(t, root, ShotOptions{Targets: []string{"ab2"}, Node: "Summary", Width: 700})
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(calls[0], "--dump-dom") {
		t.Errorf("the browser was not asked for the DOM its report is in: %v", calls[0])
	}
	r := results[0]
	if !r.Measured || r.Width != 700 || len(r.Clipped) != 2 || strings.Join(r.Unknown, ",") != "sparkline" {
		t.Fatalf("the page's report did not come back: %+v", r)
	}
	if r.Clipped[1].Where != "text <b>" {
		t.Errorf("an escaped label was not decoded: %q", r.Clipped[1].Where)
	}
	human := ShotsHuman(results)
	for _, want := range []string{"does not fit at 700px wide", "scroll code (panel Wide) — 2864px across", "UNKNOWN MARKER: sparkline"} {
		if !strings.Contains(human, want) {
			t.Errorf("shot does not say %q:\n%s", want, human)
		}
	}

	t.Setenv(fakeBrowserReport, "")
	results, _, err = fakeShot(t, root, ShotOptions{Targets: []string{"ab1"}})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Measured || !strings.Contains(ShotsHuman(results), "reported nothing about what fits") {
		t.Errorf("a page with no report was taken for a clean one: %+v", results[0])
	}
}
