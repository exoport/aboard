package aboard

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// capsHash fingerprints the DESCRIBED SURFACE. If it moved between two calls in
// one process, every `capabilities --check` would report drift that does not
// exist, and the warning that is meant to catch a stale skill would be noise
// within a week.
func TestCapsHashIsStable(t *testing.T) {
	first, err := buildManifest(web.FS)
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}
	second, err := buildManifest(web.FS)
	if err != nil {
		t.Fatalf("buildManifest (again): %v", err)
	}
	if first.Hash == "" {
		t.Fatal("capsHash is empty")
	}
	if first.Hash != second.Hash {
		t.Fatalf("capsHash moved between two calls: %s then %s", first.Hash, second.Hash)
	}
}

// The manifest describes the BOARD, not the process serving it, so a board
// hosted by ape must hash identically to a standalone one.
//
// This used to read `if m.App != AppName`, which is a constant compared to
// itself: buildManifest takes no host, so the assertion held whatever anyone did
// to the two host identities, and the property it named was never checked. What
// makes it real is the three-way separation — the manifest's app name is
// NEITHER host's invocation name, and the two hosts are distinct from each
// other, so a future edit that made the manifest report its host has to break
// one of these lines.
func TestManifestAppIsHostIndependent(t *testing.T) {
	m, err := buildManifest(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	if m.App != AppName {
		t.Fatalf("manifest app = %q, want %q", m.App, AppName)
	}
	if HostStandalone == HostApe {
		t.Fatal("the two host identities are the same string; nothing below distinguishes them")
	}
	// `aboard` the app and `aboard` the standalone host spell the same, which is
	// exactly the confusion this guards: the point is that ape's mount does NOT
	// change the answer.
	if m.App == HostApe {
		t.Errorf("the manifest reports the host (%q) rather than the board", m.App)
	}
	// And the mechanism, asserted rather than described: NEITHER host's
	// invocation name appears anywhere in the manifest the binary emits. Running
	// Capabilities twice and comparing the two outputs would prove nothing —
	// nothing in this package takes a host, so both runs are the same call — and
	// a self-comparison dressed up as a two-host comparison is the same defect
	// this test was fixed for.
	var buf strings.Builder
	if _, err := Capabilities(Root(t.TempDir()), web.FS, "json", "", false, &buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), HostApe) {
		t.Errorf("the manifest mentions the host %q; it describes the board, not the process serving it", HostApe)
	}
	// The board's own name is in there, and it is not either host's by accident:
	// it is AppName, which the assertions above pin against both hosts.
	if !strings.Contains(buf.String(), `"app": "`+AppName+`"`) {
		t.Errorf("the manifest does not report the app name %q", AppName)
	}
}

// The manifest must round-trip: the browser reads it at boot for the help panel
// and the skill reference is generated from it, so an unmarshalable field would
// break both at once.
func TestManifestMarshals(t *testing.T) {
	m, err := buildManifest(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"app", "schema", "capsHash", "types", "commands", "routes"} {
		if _, ok := back[key]; !ok {
			t.Errorf("manifest has no %q", key)
		}
	}
}

// Every declared type needs a spec file to have been read; a manifest with no
// types would still hash and still pass the stability check above.
func TestManifestHasTypes(t *testing.T) {
	m, err := buildManifest(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Types) < 15 {
		t.Fatalf("manifest declares %d types, want at least the 15 renderers", len(m.Types))
	}
	if len(m.Commands) != len(Commands()) {
		t.Fatalf("manifest carries %d commands, table declares %d", len(m.Commands), len(Commands()))
	}
}

// The recipe index was the one `make caps` artifact with no drift gate. Renaming
// or adding a built-in left `.claude/skills/aboard/references/recipes.md`
// describing a set that no longer existed — a table naming a recipe that
// `aboard recipes show` cannot find — and nothing failed anywhere: not the Go
// suite, not the shell suite, not `capabilities --check`.
//
// It is checkable where the two others are not universally so, because it is
// generated from the BUILT-IN recipes, which are compiled into the binary.
func TestTheGeneratedRecipeIndexIsNotStale(t *testing.T) {
	want, err := generatedRecipeIndex()
	if err != nil {
		t.Fatal(err)
	}
	root := repoRoot(t)
	got, err := os.ReadFile(root.SkillRecipes())
	if err != nil {
		t.Fatalf("reading the generated recipe index: %v", err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(want) {
		t.Errorf("%s no longer matches the built-in recipes — run `make caps`", root.SkillRecipes())
	}
}

// And `capabilities --check` sees it, which is what makes `make caps`'s last
// line a gate rather than a formality.
func TestCapabilitiesCheckCatchesRecipeIndexDrift(t *testing.T) {
	root := Root(t.TempDir())
	dir := filepath.Dir(root.SkillRecipes())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.SkillRecipes(), []byte("# recipes\n\nnothing like the real table\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	code, err := Capabilities(root, web.FS, "json", "", true, &out)
	if err != nil {
		t.Fatal(err)
	}
	if code != ExitFailed {
		t.Errorf("exit %d, want %d — a drifted recipe index was not reported", code, ExitFailed)
	}
	if !strings.Contains(out.String(), "recipes.md") {
		t.Errorf("the report does not name the stale file: %s", out.String())
	}
}

// A project that copied the binary but not the skill has nothing to be stale,
// exactly as for the reference. This is the case that made `--check` fail in a
// bare project once already.
func TestCapabilitiesCheckIsQuietWithNoSkillCopied(t *testing.T) {
	var out strings.Builder
	code, err := Capabilities(Root(t.TempDir()), web.FS, "json", "", true, &out)
	if err != nil {
		t.Fatal(err)
	}
	if code != ExitOK {
		t.Errorf("exit %d, want 0 — a missing skill is nothing to check", code)
	}
}

// repoRoot is this checkout, reached from the package directory. The generated
// skill files live in the repo and not in the embedded tree, so a test that
// checks them has to say where the repo is.
func repoRoot(t *testing.T) Root {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return Root(abs)
}

// `--check` treats a MISSING generated file as "nothing to check". A file that
// is PRESENT and unreadable is a different thing entirely, and answering
// "current" for it is the one failure mode a gate must not have — it is exactly
// the shape of the recipe-index hole this whole check was added to close.
func TestCapabilitiesCheckDoesNotCallAnUnreadableFileCurrent(t *testing.T) {
	for _, tc := range []struct {
		name string
		file func(Root) string
	}{
		{"the controls module", Root.GeneratedControls},
		{"the recipe index", Root.SkillRecipes},
		{"the skill reference", Root.SkillReference},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := Root(t.TempDir())
			// A DIRECTORY where the file goes: readable listing, unreadable file,
			// and it needs no uid games.
			if err := os.MkdirAll(tc.file(root), 0o755); err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			code, err := Capabilities(root, web.FS, "json", "", true, &out)
			if err == nil && code == ExitOK {
				t.Errorf("%s was present and unreadable, and was reported as nothing to check: %s", tc.name, out.String())
			}
		})
	}
}

// The stale messages are the ONLY instruction a reader in a project that copied
// the skill and not the Makefile ever sees. `make caps` is a target in aboard's
// own checkout; naming it alone was the false claim the review filed against
// SKILL.md, and it lived in these two strings as well.
func TestTheStaleMessagesNameARemedyThatRunsAnywhere(t *testing.T) {
	root := Root(t.TempDir())
	dir := filepath.Dir(root.SkillReference())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.SkillReference(), []byte("# not the reference\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	code, err := Capabilities(root, web.FS, "json", "", true, &out)
	if err != nil {
		t.Fatal(err)
	}
	if code != ExitFailed {
		t.Fatalf("exit %d, want %d — a drifted reference was not reported", code, ExitFailed)
	}
	if !strings.Contains(out.String(), AppName+" capabilities --format md") {
		t.Errorf("the stale message names no remedy that runs outside aboard's checkout:\n%s", out.String())
	}
}

// The committed skill reference must still match the binary.
//
// `capabilities --check` says so, `make caps` runs it, and test/smoke.sh used to
// run it too — but smoke.sh never ran in CI and `make caps` is something a
// person remembers. This is the same assertion as a Go test, which is the only
// place it runs on every push. It is the sibling of
// TestTheGeneratedRecipeIndexIsNotStale and of the cli package's
// TestTheGeneratedCLIReferenceIsNotStale; the three generated files now fail the
// same way.
func TestTheGeneratedSkillReferenceIsNotStale(t *testing.T) {
	root := repoRoot(t)
	var out strings.Builder
	code, err := Capabilities(root, web.FS, "json", "", true, &out)
	if err != nil {
		t.Fatal(err)
	}
	if code != ExitOK {
		t.Errorf("`aboard capabilities --check` exits %d in this checkout — run `make caps` and commit what it writes:\n%s",
			code, out.String())
	}
}

// One type can be asked for on its own — the cheap per-type answer a resuming
// session actually uses. And an unknown one is refused rather than answered with
// the whole manifest, which is what would happen if the filter silently fell
// through.
func TestOneTypeCanBeAskedForOnItsOwn(t *testing.T) {
	var out strings.Builder
	code, err := Capabilities(Root(t.TempDir()), web.FS, "json", "dag", false, &out)
	if err != nil {
		t.Fatal(err)
	}
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var m struct {
		Types []struct {
			Type string `json:"type"`
		} `json:"types"`
		Commands []any `json:"commands"`
	}
	if err := json.Unmarshal([]byte(out.String()), &m); err != nil {
		t.Fatalf("unreadable manifest: %v", err)
	}
	if len(m.Types) != 1 || m.Types[0].Type != "dag" {
		t.Errorf("asking for one type answered with %d: %+v", len(m.Types), m.Types)
	}
	// The command table and the routes are dropped for a single type: the point
	// of the per-type form is that it is short.
	if len(m.Commands) != 0 {
		t.Errorf("the per-type manifest still carries the whole command table")
	}

	var bad strings.Builder
	code, err = Capabilities(Root(t.TempDir()), web.FS, "json", "definitely-not-a-type", false, &bad)
	if err == nil {
		t.Error("an unknown type was answered instead of refused")
	}
	if code != ExitUsage {
		t.Errorf("an unknown type exits %d, want %d", code, ExitUsage)
	}
}

// Every prop a component's `text` declaration names is a prop that component
// actually reads.
//
// The declaration exists so export.go has no second copy of the catalog; a `text`
// entry naming a prop that was renamed or removed would put that copy back,
// silently — the node would simply print with no text, which is what an empty
// node looks like anyway.
func TestEveryDeclaredTextPropIsARealProp(t *testing.T) {
	specs, err := loadSpecs(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		for name, component := range spec.Components {
			reads := map[string]bool{}
			for _, prop := range append(append([]string{}, spec.CommonProps...), component.Props...) {
				reads[prop] = true
			}
			for _, entry := range component.Text {
				for prop := range strings.SplitSeq(strings.TrimPrefix(entry, "="), "|") {
					if !reads[prop] {
						t.Errorf("%s: %s declares text %q, but %s does not read %q — it reads: %s",
							spec.Type, name, entry, name, prop, strings.Join(component.Props, ", "))
					}
				}
			}
			// A component that draws nothing of its own cannot also have display
			// text. Both are read by the outline and they contradict each other:
			// the layout flag says "no line", the text says "here is the line".
			if component.Layout && len(component.Text) > 0 {
				t.Errorf("%s: %s is declared layout AND declares text %v", spec.Type, name, component.Text)
			}
		}
	}
}

// The schema version is declared TWICE — here in Go, and in the shell as
// SCHEMA_VERSION — and the two must agree.
//
// Not a style rule: `repaint()` compares the document's version against the
// shell's constant and, when they differ, clears the tab strip and raises a
// notice instead of drawing. That is right when a page has been open across an
// upgrade, and catastrophic when it is the CONSTANTS that disagree — the board
// comes up with no tabs at all, no console error, and a document that is
// perfectly valid. Measured on 2026-08-28: resetting SchemaVersion to 1 without
// the shell left every board blank, and six browser tests reported it as "the
// tab strip never appeared", which reads like a broken suite.
//
// caps.go says the constant is kept in Go "so the manifest can state it without
// parsing JavaScript". Parsing the JavaScript is exactly what a CHECK should do.
func TestTheShellAgreesWithTheDeclaredSchemaVersion(t *testing.T) {
	shell, err := fs.ReadFile(web.FS, "aboard.html")
	if err != nil {
		t.Fatalf("reading the shell: %v", err)
	}
	m := regexp.MustCompile(`(?m)^const SCHEMA_VERSION = (\d+);`).FindSubmatch(shell)
	if m == nil {
		t.Fatal("aboard.html no longer declares `const SCHEMA_VERSION = <n>;` at the start of a line — " +
			"if it moved, move this check with it rather than deleting it")
	}
	got, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("SCHEMA_VERSION is not a number: %v", err)
	}
	if got != SchemaVersion {
		t.Errorf("aboard.html understands schema %d and this package writes %d — "+
			"every board would come up with an empty tab strip and no error", got, SchemaVersion)
	}
}
