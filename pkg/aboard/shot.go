// shot.go — a picture of a tab, taken by a real browser.
//
//	aboard shot ab24
//	aboard shot ab24 --node Summary      a ui tab's panel, not its first one
//	aboard shot kanban dag --theme light
//
// The skill tells every agent to render what it wrote and LOOK before saying a
// tab is ready, because `apply` exits 0 for a tree that draws an empty box. For
// most of this project's life the only way to do that was test/shot.sh, which
// exists in aboard's own checkout and nowhere else. An agent in any other project
// was told to run `make shot`, found no such target, and either skipped the
// look or rebuilt the tool: a session on 2026-09-22 wrote a local proxy that
// 404'd /events, injected localStorage and clicked a panel by its label, all to
// take one picture of a `ui` tab.
//
// What this knows, so the caller does not have to, each one learned by getting
// it wrong:
//
//   - `?nosse=1`. The live stream never closes, so a headless browser never
//     reaches network-idle and writes no file at all, with no message.
//   - `?tab=<id>`, resolved here from an id, key or type, so the picture is of
//     the tab asked for and not of whichever one a fresh profile lands on.
//   - `node=` for a `ui` panel, CHECKED against the tab first. Without it the
//     picture is of the first panel, which looks like success.
//   - An `html` tab is shot on its own route, /tab/<id>/html: headless chromium
//     does not reliably paint the frame inside the shell, and a picture of an
//     empty frame proves nothing.
//   - A snap-confined browser cannot write outside $HOME, nor inside a hidden
//     directory at its top, and says "No such file or directory" when it cannot.
//     It renders into the snap's own directory and the file is moved into place.
//   - The previous picture is removed first. A stale file from an earlier run is
//     indistinguishable from a fresh one.
//   - `?shot=1`, so the page posts no mount receipt. `aboard rendered` and
//     `wait --for "rendered <id>"` mean that a browser someone was LOOKING at
//     drew the tab; a picture an agent took must not satisfy either.
//
// It never writes to the board. The shell only writes the document in answer to
// a gesture, and a headless page makes none.

package aboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Defaults the declared table repeats as strings. A 1400×900 window is a laptop
// screen, which is what the human is looking at; the suite's own shots are
// full-page, which answers a different question.
const (
	ShotDefaultWidth   = 1400
	ShotDefaultHeight  = 900
	ShotDefaultTimeout = 90 * time.Second

	// shotVirtualTime is how far chromium runs the page's clock before it takes
	// the picture. Virtual, so it costs well under a second of real time; long,
	// so a mermaid render and a first layout have long finished. The value
	// test/shot.sh used for as long as it existed.
	shotVirtualTime = 10000

	// uiComponentTabs is the `ui` component that holds panels. The same word as
	// the document's own `tabs` key (keyTabs) and a different thing.
	uiComponentTabs = "tabs"
)

// ErrShotsFailed is a run where at least one picture was not written. The
// results that were written are still returned: four good pictures and one
// failure is worth reporting as exactly that.
var ErrShotsFailed = errors.New("not every screenshot was written")

// ShotOptions is one `aboard shot` run.
type ShotOptions struct {
	// Targets are tab ids, keys or types, resolved in that order.
	Targets []string
	// Node is a deep link inside the tab: a node's id, or a ui panel's label.
	Node string
	// HelpPanel opens the shell's help panel over the tab (#help).
	HelpPanel bool
	// Theme is "dark", "light", or "" for the board's default.
	Theme  string
	Width  int
	Height int
	// Browser is a path or a command name; "" finds one.
	Browser string
	// Timeout bounds each picture.
	Timeout time.Duration
}

// ShotResult is one picture, or the reason there is none.
type ShotResult struct {
	Target string `json:"target"          yaml:"target"`
	Tab    string `json:"tab"             yaml:"tab"`
	Name   string `json:"name,omitempty"  yaml:"name,omitempty"`
	Type   string `json:"type"            yaml:"type"`
	URL    string `json:"url"             yaml:"url"`
	Path   string `json:"path,omitempty"  yaml:"path,omitempty"`
	Error  string `json:"error,omitempty" yaml:"error,omitempty"`
	// Note is what the picture does NOT show that a reader would assume it
	// does: the first panel of a panelled tab, a widget without the board
	// around it.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
	// Measured says the page reported what it drew. False means a board
	// served by a binary older than this command, whose page has no report.
	Measured bool `json:"measured" yaml:"measured"`
	// Width, Clipped and Unknown are what the headless page reported, in the
	// shape of a mount receipt: the window width, the boxes whose content did
	// not fit at it, and any unknown-component marker in the picture.
	Width   int      `json:"width,omitempty"   yaml:"width,omitempty"`
	Clipped []Clip   `json:"clipped,omitempty" yaml:"clipped,omitempty"`
	Unknown []string `json:"unknown,omitempty" yaml:"unknown,omitempty"`
}

// shotTab is what a shot needs to know about a tab.
type shotTab struct {
	ID    string          `json:"id"`
	Key   string          `json:"key"`
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	State json.RawMessage `json:"state"`
}

// Shot takes one picture per target of the running board, into
// root.ShotsDir(). The board must be running: the picture is of the page the
// human sees, served by the binary serving it.
func Shot(ctx context.Context, root Root, name string, opts ShotOptions, inv Invocation) ([]ShotResult, error) {
	inst, err := RunningInstance(root, name, inv)
	if err != nil {
		return nil, err
	}
	live := ProbeBoard(ctx, inst.Port, inst.Base)
	if live == nil {
		return nil, fmt.Errorf("the board recorded at %s is not answering; start it with `%s`", inst.URL, inv.Cmd("serve"))
	}
	base := strings.TrimSuffix(live.URL, "/")

	body, err := fetchDocument(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("reading the board: %w", err)
	}
	var doc struct {
		Tabs []shotTab `json:"tabs"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("the board's document is not readable: %w", err)
	}

	// Every target resolves before anything is shot. A typo among five should
	// cost nothing but the message, not four pictures and a failure.
	// Two names for one tab (its id and its key) are one picture: the second
	// would overwrite the first with the same image and report it twice.
	tabs := make([]shotTab, 0, len(opts.Targets))
	targets := make([]string, 0, len(opts.Targets))
	seen := map[string]bool{}
	for _, target := range opts.Targets {
		tab, ok := resolveShotTarget(doc.Tabs, target)
		if !ok {
			return nil, unknownShotTarget(target, doc.Tabs)
		}
		if seen[tab.ID] {
			continue
		}
		seen[tab.ID] = true
		tabs = append(tabs, tab)
		targets = append(targets, target)
	}
	if opts.Node != "" {
		for _, tab := range tabs {
			if !nodeInTab(tab, opts.Node) {
				return nil, fmt.Errorf("%s (%s) has no node or panel called %q — a deep link to it would open the tab and show "+
					"whatever comes first, which looks exactly like success", tab.ID, tab.Type, opts.Node)
			}
		}
	}

	browser, err := FindBrowser(opts.Browser)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root.ShotsDir(), 0o755); err != nil {
		return nil, err
	}

	results := make([]ShotResult, 0, len(tabs))
	failed := false
	for i, tab := range tabs {
		res := ShotResult{Target: targets[i], Tab: tab.ID, Name: tab.Name, Type: tab.Type}
		res.URL, res.Note = shotURL(base, tab, opts)
		path := root.ShotFile(shotName(tab.ID, opts))
		dom, err := browser.capture(ctx, res.URL, path, opts)
		if err != nil {
			res.Error = err.Error()
			failed = true
		} else {
			res.Path = path
			if report, ok := shotReport(dom); ok {
				res.Measured = true
				res.Width, res.Clipped, res.Unknown = clampPx(report.Width), capClips(report.Clipped), capIDs(report.Unknown)
			}
		}
		results = append(results, res)
	}
	if failed {
		return results, ErrShotsFailed
	}
	return results, nil
}

// resolveShotTarget finds a tab the way the shell's own deep link does — id,
// then key, then type — but settles on ONE id before the browser is involved,
// so a key that happens to equal a type cannot pick a different tab there.
func resolveShotTarget(tabs []shotTab, target string) (shotTab, bool) {
	for _, match := range []func(shotTab) bool{
		func(t shotTab) bool { return t.ID == target },
		func(t shotTab) bool { return t.Key != "" && t.Key == target },
		func(t shotTab) bool { return t.Type == target },
	} {
		for _, t := range tabs {
			if match(t) {
				return t, true
			}
		}
	}
	return shotTab{}, false
}

func unknownShotTarget(target string, tabs []shotTab) error {
	var b strings.Builder
	fmt.Fprintf(&b, "no tab %q — this board has:", target)
	for _, t := range tabs {
		fmt.Fprintf(&b, "\n  %-8s %-10s %s", t.ID, t.Type, t.Name)
		if t.Key != "" {
			fmt.Fprintf(&b, "  (key %s)", t.Key)
		}
	}
	return errors.New(b.String())
}

// nodeInTab reports whether a deep link's node= names something on this tab:
// any object in its state whose `id` is target — which is what every
// renderer's focus() looks up — or, on a `ui` tab, a `tabs` panel whose label
// reads as target.
func nodeInTab(tab shotTab, target string) bool {
	var state any
	if len(tab.State) == 0 || json.Unmarshal(tab.State, &state) != nil {
		return false
	}
	var data map[string]any
	if m, ok := state.(map[string]any); ok {
		data, _ = m["data"].(map[string]any)
	}
	isUI := tab.Type == tabTypeUI
	var walk func(v any) bool
	walk = func(v any) bool {
		switch node := v.(type) {
		case map[string]any:
			if id, ok := node["id"].(string); ok && id == target {
				return true
			}
			if isUI && node[keyType] == uiComponentTabs {
				for _, panel := range mapsOf(node["panels"]) {
					if uiText(resolveUI(panel["label"], data)) == target {
						return true
					}
				}
			}
			for _, kid := range node {
				if walk(kid) {
					return true
				}
			}
		case []any:
			return slices.ContainsFunc(node, walk)
		}
		return false
	}
	return walk(state)
}

// hasPanels reports a `ui` tab with a `tabs` component in it — the case where a
// picture without --node is a picture of the first panel only.
func hasPanels(tab shotTab) bool {
	var state any
	if tab.Type != tabTypeUI || json.Unmarshal(tab.State, &state) != nil {
		return false
	}
	var walk func(v any) bool
	walk = func(v any) bool {
		switch node := v.(type) {
		case map[string]any:
			if node[keyType] == uiComponentTabs {
				return true
			}
			for _, kid := range node {
				if walk(kid) {
					return true
				}
			}
		case []any:
			return slices.ContainsFunc(node, walk)
		}
		return false
	}
	return walk(state)
}

// shotURL is the address the browser loads, and a note on what the picture
// will not show.
func shotURL(base string, tab shotTab, opts ShotOptions) (addr, note string) {
	if tab.Type == "html" && !opts.HelpPanel && opts.Node == "" {
		return base + "/tab/" + url.PathEscape(tab.ID) + "/html",
			"the widget on its own route: a headless browser does not reliably paint the frame inside the board"
	}
	q := url.Values{}
	q.Set("nosse", "1")
	q.Set("shot", "1")
	q.Set("tab", tab.ID)
	if opts.Node != "" {
		q.Set("node", opts.Node)
	}
	if opts.Theme != "" {
		q.Set("theme", opts.Theme)
	}
	addr = base + "/?" + q.Encode()
	if opts.HelpPanel {
		addr += "#help"
	}
	if opts.Node == "" && hasPanels(tab) {
		note = "the FIRST panel of its tabs component; pass --node <panel label> for another"
	}
	return addr, note
}

var shotNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// shotName is the file a picture goes to: the tab id, and whatever else was
// asked for, so shooting two panels of one tab gives two files rather than one
// overwriting the other.
func shotName(tabID string, opts ShotOptions) string {
	parts := []string{tabID}
	if opts.Node != "" {
		parts = append(parts, opts.Node)
	}
	if opts.HelpPanel {
		parts = append(parts, "help")
	}
	if opts.Theme != "" {
		parts = append(parts, opts.Theme)
	}
	return strings.Trim(shotNameUnsafe.ReplaceAllString(strings.Join(parts, "-"), "_"), "_")
}

/* ---------- the browser ---------- */

// Browser is a chromium-family browser `aboard shot` can drive headless.
type Browser struct {
	Path string
	// Snap is the snap's name when the browser is snap-confined, "" otherwise.
	Snap string
}

// browserNames are looked for on $PATH, in order. Every one of them takes the
// same --headless --screenshot flags.
var browserNames = []string{
	"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome",
	"microsoft-edge", "microsoft-edge-stable", "msedge",
}

// FindBrowser resolves the browser to drive: the one named, or else the first
// chromium-family browser on $PATH, or else one in its usual install location.
func FindBrowser(named string) (Browser, error) {
	if named != "" {
		path, err := exec.LookPath(named)
		if err != nil {
			return Browser{}, fmt.Errorf("the browser %q was not found: %w", named, err)
		}
		return browserAt(path), nil
	}
	for _, name := range browserNames {
		if path, err := exec.LookPath(name); err == nil {
			return browserAt(path), nil
		}
	}
	for _, path := range BrowserInstallPaths(runtime.GOOS, os.Getenv) {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return browserAt(path), nil
		}
	}
	return Browser{}, fmt.Errorf("no chromium-family browser found (looked for %s on $PATH) — install one, "+
		"or name it with --browser or ABOARD_BROWSER", strings.Join(browserNames, ", "))
}

// browserAt recognises a snap by what the command resolves to. /snap/bin/<name>
// is a symlink to /usr/bin/snap, which dispatches on the name it was called by.
func browserAt(path string) Browser {
	b := Browser{Path: path}
	if runtime.GOOS != "linux" {
		return b
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && filepath.Base(resolved) == "snap" {
		b.Snap = filepath.Base(path)
	}
	return b
}

// shotReportRe finds the report a shot page writes into itself: the shell's
// receipt sweep, or an html widget's own measurement. Non-greedy to the first
// closing tag, which the writer guarantees is its own by escaping every `<`.
var shotReportRe = regexp.MustCompile(`(?s)<script[^>]*id="aboard-shot-report"[^>]*>(.*?)</script>`)

// shotReport reads that report out of chromium's --dump-dom.
func shotReport(dom []byte) (Receipt, bool) {
	var report Receipt
	m := shotReportRe.FindSubmatch(dom)
	if len(m) < 2 || json.Unmarshal(m[1], &report) != nil {
		return Receipt{}, false
	}
	return report, true
}

// maxShotDOM bounds what --dump-dom may hand back. The page is the shell and one
// mounted tab; a DOM past this is not one the report is worth reading out of.
const maxShotDOM = 64 << 20

// capture runs the browser once and leaves a non-empty picture at path, or
// says why not. It returns the page's DOM as the browser left it, which is
// where a shot page writes its report.
func (b Browser) capture(ctx context.Context, addr, path string, opts ShotOptions) ([]byte, error) {
	// Removed first, and checked: a picture left from an earlier run would
	// otherwise be reported as this run's.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("cannot clear the previous %s: %w", path, err)
	}

	target := path
	if b.Snap != "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("finding $HOME for the snap browser: %w", err)
		}
		target = SnapShotStage(home, b.Snap, filepath.Base(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		_ = os.Remove(target)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = ShotDefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	width, height := opts.Width, opts.Height
	if width <= 0 {
		width = ShotDefaultWidth
	}
	if height <= 0 {
		height = ShotDefaultHeight
	}
	args := []string{
		// Headless starts from a fresh profile on every run (measured with the
		// snap chromium: a tab stored by one run is not the tab the next one
		// opens on), so nothing a person did in their own browser — a folded
		// strip, a remembered panel — leaks into the picture.
		"--headless",
		"--disable-gpu",
		"--hide-scrollbars",
		"--window-size=" + strconv.Itoa(width) + "," + strconv.Itoa(height),
		"--virtual-time-budget=" + strconv.Itoa(shotVirtualTime),
		"--screenshot=" + target,
		// The same run prints the page's DOM once the picture is taken, which is
		// how the page's own report of what did not fit gets back out. Measured:
		// one chromium invocation does both.
		"--dump-dom",
	}
	// Chromium refuses to start sandboxed as root, which is every container.
	// Anywhere else the sandbox stays on: the page holds an agent's html widget.
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		args = append(args, "--no-sandbox")
	}
	args = append(args, addr)

	var dom, diag cappedBuffer
	dom.limit, diag.limit = maxShotDOM, 64<<10
	//nolint:gosec // G204: the browser is the one the caller named or the first chromium-family one found, and every argument is built above
	cmd := exec.CommandContext(runCtx, b.Path, args...)
	cmd.Stdout, cmd.Stderr = &dom, &diag
	err := cmd.Run()
	if runCtx.Err() != nil {
		return nil, fmt.Errorf("%s did not finish within %s", b.Path, timeout)
	}
	info, statErr := os.Stat(target)
	if statErr != nil || info.Size() == 0 {
		reason := lastLines(diag.String(), 3)
		if err != nil {
			reason = err.Error() + ": " + reason
		}
		return nil, fmt.Errorf("%s wrote no picture (%s)", b.Path, strings.TrimSpace(reason))
	}
	if target != path {
		if err := moveFile(target, path); err != nil {
			return nil, err
		}
	}
	return dom.Bytes(), nil
}

// cappedBuffer keeps the first limit bytes written to it and drops the rest,
// so a runaway browser cannot fill memory; it never refuses a write, so the
// browser is not killed by a closed pipe.
type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - c.Len(); room > 0 {
		c.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// moveFile renames, and copies when the two paths are on different
// filesystems — which a snap's directory and a project often are.
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	body, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.WriteFile(to, body, 0o644); err != nil { //nolint:gosec // 0o644 is the board's repo-wide file-mode policy; see the note in init.go
		return err
	}
	return os.Remove(from)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	joined := strings.Join(lines, " / ")
	if joined == "" {
		return "no output"
	}
	return joined
}

// ShotsHuman renders results for the terminal: one line per picture, with the
// note under it when there is one. Failures are included, so the reader sees
// the whole run in one place.
func ShotsHuman(results []ShotResult) string {
	var b strings.Builder
	for i := range results {
		r := &results[i]
		if r.Error != "" {
			fmt.Fprintf(&b, "%s  FAILED: %s\n", r.Tab, r.Error)
			continue
		}
		fmt.Fprintf(&b, "%s  %s\n", r.Tab, r.Path)
		if r.Note != "" {
			fmt.Fprintf(&b, "        shows %s\n", r.Note)
		}
		if len(r.Unknown) > 0 {
			fmt.Fprintf(&b, "        UNKNOWN MARKER: %s (drawn instead of the thing)\n", strings.Join(r.Unknown, ", "))
		}
		if len(r.Clipped) > 0 {
			b.WriteString(ClipsHuman(r.Width, r.Clipped, "        "))
		}
		if !r.Measured {
			b.WriteString("        the page reported nothing about what fits: a board served by an older binary?\n")
		}
	}
	return b.String()
}
