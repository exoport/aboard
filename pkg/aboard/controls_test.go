package aboard

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// The one way a renderer makes a button is views/controls.js. That rule had a
// grep behind it — test/smoke.sh looks for createElement('button') — and
// views/diagram.js walked straight through it by writing four literal button
// tags into a template string instead. Four capabilities that `aboard
// capabilities` had never heard of, absent from the help panel, and unreachable
// by every drift check the control series added.
//
// These checks are in Go, and not only in the shell suite, because the shell
// suite is local: it needs a headless chromium and a running server, so CI never
// runs it. A static check that only runs on the machine of whoever remembers to
// run it is the check that was already there when this shipped.

// viewSources returns every views/*.js as it is EMBEDDED, which is what actually
// ships, and with comment lines removed — several of the comments below discuss
// the very patterns being banned, and a check that matched its own explanation
// would be one nobody could write about.
func viewSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := fs.ReadDir(web.FS, "views")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		body, err := fs.ReadFile(web.FS, path.Join("views", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = stripJSComments(string(body))
	}
	if len(out) < 15 {
		t.Fatalf("only %d view modules found; the embedded tree looks wrong", len(out))
	}
	return out
}

// stripJSComments blanks whole-line comments. Deliberately crude: it is not a
// parser, and it does not need to be — the patterns being looked for are written
// at the start of a statement, and a false NEGATIVE here would need somebody to
// hide a button behind a line that also opens a comment.
func stripJSComments(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*") {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// No renderer writes a literal button tag. This is the check that did not exist:
// the old one looked for createElement, and markup in a template literal is not
// createElement.
//
// controls.js is the only exemption, and it is exempt because it IS the helper.
// The five files the spike lists as legitimately using the plain `button()`
// helper — menu, inline, ui, dag, markup — are NOT exempt here: they use the
// helper, so they have no literal tags to excuse, and exempting them would blind
// the check in five of the places it most needs to work.
func TestNoRendererWritesALiteralButtonTag(t *testing.T) {
	for name, src := range viewSources(t) {
		if name == "controls.js" {
			continue
		}
		if strings.Contains(src, "<button") {
			t.Errorf("views/%s writes a literal <button> tag — route it through controlsFor(type) "+
				"and declare it in views/<type>.spec.json, or use button() if it is not a capability", name)
		}
	}
}

// The original rule, mirrored in Go so CI sees it too.
func TestEveryButtonGoesThroughTheHelper(t *testing.T) {
	for name, src := range viewSources(t) {
		if name == "controls.js" {
			continue
		}
		if strings.Contains(src, "createElement('button')") || strings.Contains(src, `createElement("button")`) {
			t.Errorf("views/%s builds a button by hand — use button() or controlsFor(type) from views/controls.js", name)
		}
	}
}

// The other direction, and the one nothing else covers: a declared control whose
// button was deleted leaves its declaration behind, and a spec describing a
// button that no longer exists is exactly the drift the declaration was meant to
// remove.
func TestEveryDeclaredControlIsUsedByItsRenderer(t *testing.T) {
	m, err := buildManifest(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	sources := viewSources(t)

	for _, spec := range m.Types {
		if len(spec.Controls) == 0 {
			continue
		}
		src, ok := sources[spec.Type+".js"]
		if !ok {
			// A type with controls and no module of its own: `gate`, `log` and
			// friends all have one, so this is worth saying rather than skipping.
			t.Errorf("%s declares %d controls but there is no views/%s.js", spec.Type, len(spec.Controls), spec.Type)
			continue
		}
		for _, c := range spec.Controls {
			if !strings.Contains(src, "'"+c.ID+"'") && !strings.Contains(src, `"`+c.ID+`"`) {
				t.Errorf("%s declares control %q and views/%s.js never asks for it", spec.Type, c.ID, spec.Type)
			}
		}
	}
}

// A declared control must have somewhere for a reader to end up: a label to
// press and a sentence saying what pressing it does. An empty doc is a row in
// the generated reference that tells an agent nothing.
func TestDeclaredControlsCarryTheirDocumentation(t *testing.T) {
	m, err := buildManifest(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, spec := range m.Types {
		for _, c := range spec.Controls {
			seen++
			if c.ID == "" {
				t.Errorf("%s declares a control with no id", spec.Type)
			}
			if strings.TrimSpace(c.Doc) == "" {
				t.Errorf("%s control %q has no doc", spec.Type, c.ID)
			}
		}
	}
	if seen < 50 {
		t.Errorf("only %d controls declared across all renderers; the specs look truncated", seen)
	}
}

// The generated module the renderers import must match the specs. `make caps`
// writes it and the shell suite checks it, but the shell suite is local — so a
// spec edit committed without regenerating would reach CI green and ship buttons
// whose labels disagree with their own declarations.
func TestGeneratedControlsModuleMatchesTheSpecs(t *testing.T) {
	m, err := buildManifest(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(web.FS, "views/controls.generated.js")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(controlsModule(m)) {
		t.Fatalf("views/controls.generated.js is stale (capsHash %s) — run `make caps`, "+
			"or `aboard capabilities --format js > pkg/aboard/web/views/controls.generated.js` and build twice", m.Hash)
	}
}

// The spec for the html renderer used to claim that an html BLOCK inside a stack
// does not render. It does, and `test/e2e` proves it every run
// (TestAWidgetInsideAStackBlockWritesThroughTheBridge clicks the block's widget
// and reads blocks[].state.data back off the server). The false sentence survived
// a whole commit series because nothing reads a `notes` entry except a person.
//
// The claim is gone; this stops it coming back, and stops the opposite sentence
// disappearing silently.
func TestHTMLSpecDoesNotDenyBlocksInStacks(t *testing.T) {
	m, err := buildManifest(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	var notes []string
	for _, spec := range m.Types {
		if spec.Type == "html" {
			notes = spec.Notes
		}
	}
	if len(notes) == 0 {
		t.Fatal("the html spec carries no notes")
	}
	joined := strings.Join(notes, "\n")
	if strings.Contains(joined, "does not render") {
		t.Errorf("html.spec.json still says a block does not render:\n%s", joined)
	}
	if !strings.Contains(joined, "blocks[].state.data") {
		t.Errorf("html.spec.json does not say where a block's state lives:\n%s", joined)
	}
}

// ADVISORY, never a failure: which renderers still make plain, undeclared
// buttons.
//
// Whether a button is a CAPABILITY an agent should know about or merely an
// affordance is a judgement no rule can make — a dialog's Cancel is not worth
// declaring, a delete-row button is. So this reports and a person decides, which
// is the honest version of the check that was originally planned as a DOM sweep.
//
// It was a `note` line in test/smoke.sh; here it is a t.Log, which means it is
// printed by `go test -v` and by CI rather than only on the machine of whoever
// remembered to run the shell suite.
func TestWhichRenderersStillUsePlainButtons(t *testing.T) {
	var users []string
	for name, src := range viewSources(t) {
		if name == "controls.js" {
			continue
		}
		// `button(` not preceded by a dot or a word character: `controlsFor` and
		// `iconButton` must not match, and neither must `.button(`.
		for line := range strings.SplitSeq(src, "\n") {
			i := strings.Index(line, "button(")
			if i < 0 {
				continue
			}
			if i > 0 {
				prev := line[i-1]
				if prev == '.' || prev == '_' || (prev >= 'a' && prev <= 'z') ||
					(prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') {
					continue
				}
			}
			users = append(users, name)
			break
		}
	}
	sort.Strings(users)
	t.Logf("plain (undeclared) buttons remain in: %s", strings.Join(users, " "))
}
