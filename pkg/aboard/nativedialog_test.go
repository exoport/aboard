package aboard

import (
	"strconv"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// The board never calls a native dialog.
//
// `window.alert`, `window.confirm` and `window.prompt` are not merely styled
// differently inside a VS Code webview, or inside any <iframe> sandboxed without
// `allow-modals`: they are SUPPRESSED. `confirm()` returns false, `prompt()`
// returns null, nothing is drawn, nothing is logged and nothing throws. Every
// gesture guarded by one is therefore dead in the panel, and dead in the way
// that is hardest to diagnose — the human clicks and nothing at all happens.
//
// That is exactly how it was found, on 2026-08-26: "the remove tab button — I
// clicked it but nothing happens". Three calls were in the tree at the time, the
// pending-removal Remove, the tab-strip rename and the form's reset, and all
// three were dead in the panel while working perfectly in a plain browser —
// which is why nothing in the browser suite had caught them.
//
// The replacement is `views/dialog.js`, which draws the question in the page
// with the same `<dialog class="sheet-dialog">` the new-tab sheet and the dag's
// delete-confirm already use. A `<dialog>` element is unaffected by
// `allow-modals`; only the three window functions are.
//
// This check is in Go, not only in the browser suite, for the reason
// controls_test.go gives for its own: the browser suite is local and CI never
// runs it, so a static rule that only holds on the machine of whoever remembers
// to run chromium is the rule that was already broken when it shipped.

// nativeDialogCalls are the three names, with the parenthesis, so a variable
// called `confirmLabel` or a function called `askConfirm` cannot match.
var nativeDialogCalls = []string{"confirm(", "prompt(", "alert("}

// callsANativeDialog reports the first native dialog call in src, or "".
//
// A match is rejected when the character before it continues an identifier, so
// `askConfirm(` and `showConfirm(` are not hits — they differ in case anyway,
// but the guard is what makes a future `reconfirm(` safe. A leading `.` is NOT
// excluded: `window.confirm(` is the exact spelling this test exists to catch.
func callsANativeDialog(src string) string {
	for _, call := range nativeDialogCalls {
		from := 0
		for {
			i := strings.Index(src[from:], call)
			if i < 0 {
				break
			}
			at := from + i
			from = at + len(call)
			if at > 0 {
				prev := src[at-1]
				if prev == '_' || prev == '$' ||
					(prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') ||
					(prev >= '0' && prev <= '9') {
					continue
				}
			}
			line := 1 + strings.Count(src[:at], "\n")
			return call + " at line " + strconv.Itoa(line)
		}
	}
	return ""
}

// stripHTMLComments blanks <!-- … --> spans, keeping the newlines so a reported
// line number still means something. Deliberately crude, like stripJSComments
// beside it: the shell's comments are written one per line and none of them
// opens a comment mid-statement.
func stripHTMLComments(src string) string {
	var out strings.Builder
	for {
		i := strings.Index(src, "<!--")
		if i < 0 {
			out.WriteString(src)
			return out.String()
		}
		out.WriteString(src[:i])
		rest := src[i:]
		j := strings.Index(rest, "-->")
		if j < 0 {
			out.WriteString(strings.Repeat("\n", strings.Count(rest, "\n")))
			return out.String()
		}
		out.WriteString(strings.Repeat("\n", strings.Count(rest[:j+3], "\n")))
		src = rest[j+3:]
	}
}

func TestNoNativeDialogInTheWebTree(t *testing.T) {
	sources := viewSources(t)

	shell, err := web.FS.ReadFile("aboard.html")
	if err != nil {
		t.Fatal(err)
	}
	sources["aboard.html"] = stripJSComments(stripHTMLComments(string(shell)))

	for name, src := range sources {
		if where := callsANativeDialog(src); where != "" {
			t.Errorf("%s calls a native dialog (%s) — a VS Code webview suppresses it silently; "+
				"use askConfirm/askPrompt from views/dialog.js", name, where)
		}
	}
}

// The other direction. Deleting the guard entirely would pass the check above
// while making a destructive gesture unaskable, and the whole point of the
// change was that the question survives, not that the call disappears.
func TestTheThreeAskingGesturesGoThroughTheInPageDialog(t *testing.T) {
	want := map[string][]string{
		"aboard.html": {"askConfirm(", "askPrompt("}, // removeTab, renameTab
		"form.js":     {"askConfirm("},               // the reset button
	}
	sources := viewSources(t)
	shell, err := web.FS.ReadFile("aboard.html")
	if err != nil {
		t.Fatal(err)
	}
	sources["aboard.html"] = stripJSComments(stripHTMLComments(string(shell)))

	for name, calls := range want {
		src, ok := sources[name]
		if !ok {
			t.Fatalf("%s is not in the embedded web tree", name)
		}
		for _, call := range calls {
			if !strings.Contains(src, call) {
				t.Errorf("%s no longer calls %s — the question a human answers before a "+
					"destructive edit must not vanish with the native call it replaced", name, call)
			}
		}
	}

	// And the helper itself has to exist and export both.
	helper, err := web.FS.ReadFile("views/dialog.js")
	if err != nil {
		t.Fatalf("views/dialog.js is missing from the embedded tree: %v", err)
	}
	for _, exported := range []string{"export function askConfirm(", "export function askPrompt("} {
		if !strings.Contains(string(helper), exported) {
			t.Errorf("views/dialog.js does not declare `%s`", exported)
		}
	}
}
