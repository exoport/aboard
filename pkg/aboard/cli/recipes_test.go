package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard"
)

func fixtureDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "recipes"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// The listing is the only complete answer — the skill's generated index can only
// carry what is compiled in — so it has to show a project's own files, name what
// they shadowed, and mark what will not parse.
func TestRecipesListShowsEveryTier(t *testing.T) {
	out, _, err := run(t, "--cwd", fixtureDir(t), "recipes", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"everywhere", "project-only", "apply-a-write", // one per tier
		"_apex/aboard/recipes", "built-in", // the scope labels a reader can act on
		"shadows", // the loser is named, not hidden
		"INVALID", // and so is the file that will not parse
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing does not mention %q:\n%s", want, out)
		}
	}
}

// --output-format json carries everything the human form does except the body,
// so a script can pick a recipe without parsing a table.
func TestRecipesListJSON(t *testing.T) {
	out, _, err := run(t, "--cwd", fixtureDir(t), "recipes", "list", "--output-format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got []aboard.Recipe
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the json listing does not parse: %v\n%s", err, out)
	}
	if len(got) == 0 {
		t.Fatal("the json listing is empty")
	}
	for _, r := range got {
		if r.Body != "" {
			t.Errorf("%s carries its body in the listing; that is what `show` is for", r.Name)
		}
	}
}

// `show` prints the body with the frontmatter stripped and a title line that
// says what the recipe is for.
func TestRecipesShowPrintsTheBody(t *testing.T) {
	out, _, err := run(t, "--cwd", fixtureDir(t), "recipes", "show", "apply-a-write")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "# apply-a-write — ") {
		t.Errorf("no title line:\n%.120s", out)
	}
	if strings.Contains(out, "when_to_use:") {
		t.Errorf("the frontmatter was not stripped:\n%s", out)
	}
	// The fixture overrides this built-in from _aboard/recipes, so this is also
	// the precedence assertion at the CLI boundary.
	if !strings.Contains(out, "overridden by the workspace") {
		t.Errorf("`show` read the built-in instead of the override:\n%s", out)
	}
}

// --template pipes. It prints the skeleton and nothing else, so `| jq` works.
func TestRecipesShowTemplate(t *testing.T) {
	out, _, err := run(t, "--cwd", fixtureDir(t), "recipes", "show", "with-template", "--template")
	if err != nil {
		t.Fatal(err)
	}
	var tab map[string]any
	if err := json.Unmarshal([]byte(out), &tab); err != nil {
		t.Fatalf("--template printed something that is not json: %v\n%s", err, out)
	}
	if tab["name"] != "Fixture tab" {
		t.Errorf("--template printed the wrong block: %s", out)
	}
}

// A recipe with no template exits non-zero rather than printing an empty
// document — an empty document handed to `aboard apply` is an empty tab.
func TestRecipesShowTemplateWithoutOneFails(t *testing.T) {
	_, _, err := run(t, "--cwd", fixtureDir(t), "recipes", "show", "project-only", "--template")
	if err == nil {
		t.Fatal("--template on a recipe with no template succeeded")
	}
	if code, _ := ExitCode(err); code != aboard.ExitFailed {
		t.Errorf("exit code %d, want %d", code, aboard.ExitFailed)
	}
}

// An unknown name lists what IS available, because the commonest cause is a near
// miss and a bare "not found" makes the reader run a second command.
func TestRecipesShowUnknownNameLists(t *testing.T) {
	_, _, err := run(t, "--cwd", fixtureDir(t), "recipes", "show", "aply-a-write")
	if err == nil {
		t.Fatal("an unknown recipe succeeded")
	}
	if code, _ := ExitCode(err); code != aboard.ExitFailed {
		t.Errorf("exit code %d, want %d", code, aboard.ExitFailed)
	}
	if !strings.Contains(err.Error(), "apply-a-write") {
		t.Errorf("the error does not list the available names: %v", err)
	}
}

// `show` on a file that does not parse fails with the reason AND the path,
// because the author is looking at the file.
func TestRecipesShowInvalidNamesTheFile(t *testing.T) {
	_, _, err := run(t, "--cwd", fixtureDir(t), "recipes", "show", "no-frontmatter")
	if err == nil {
		t.Fatal("an invalid recipe printed successfully")
	}
	if !strings.Contains(err.Error(), "no-frontmatter.md") {
		t.Errorf("the error does not name the file: %v", err)
	}
}

// The hidden index. `make caps` pipes it into the skill, so it must be
// deterministic and must never carry a project's own recipes — the file it
// writes is copied between projects inside a skill directory.
func TestRecipesIndexIsDeterministicAndBuiltinOnly(t *testing.T) {
	first, _, err := run(t, "--cwd", fixtureDir(t), "recipes", "index")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := run(t, "--cwd", fixtureDir(t), "recipes", "index")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("`recipes index` is not deterministic")
	}
	if strings.Contains(first, "project-only") || strings.Contains(first, "everywhere") {
		t.Errorf("the index carries the fixture project's recipes:\n%s", first)
	}
	if !strings.Contains(first, "| `apply-a-write` |") {
		t.Errorf("the index does not carry the built-ins:\n%s", first)
	}
}

// Recipes answer with no project at all, like `capabilities`: an agent should be
// able to ask what a copied binary knows before deciding to use it.
func TestRecipesWorkWithNoProject(t *testing.T) {
	out, _, err := run(t, "--cwd", t.TempDir(), "recipes", "list")
	if err != nil {
		t.Fatalf("recipes list in a bare directory failed: %v", err)
	}
	if !strings.Contains(out, "built-in") {
		t.Errorf("no built-in recipes reported:\n%s", out)
	}
}

// The library in the repository's top-level `recipes/` is used by COPYING a file
// into one of the project's own recipe directories. That is the whole mechanism,
// so it is asserted end to end rather than described: copy, list, show
// --template, apply --check — the four commands `recipes/README.md` tells a
// reader to run, in that order.
//
// It exists because the two files moved OUT of the binary in this change. While
// they were built-ins the compiler guaranteed they were present and the suite
// guaranteed they parsed; a file that is only ever `cp`d has neither, and
// "documented" is not a substitute for "checked".
func TestALibraryRecipeIsDiscoveredWhenCopiedIn(t *testing.T) {
	library, err := filepath.Abs(filepath.Join("..", "..", "..", "recipes"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(library, "human-checklist.md"))
	if err != nil {
		t.Fatalf("the library recipe is not where recipes/README.md says it is: %v", err)
	}

	// A scratch project, seeded the way `aboard init` seeds one. Never a board
	// anybody is using: this writes a file into .aboard/recipes/.
	dir := t.TempDir()
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	recipes := aboard.Root(dir).RecipesDir()
	if err := os.MkdirAll(recipes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recipes, "human-checklist.md"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	// Discovered, and reported as the PROJECT's — the directory a reader can go
	// and edit, not "built-in".
	list, _, err := run(t, "--cwd", dir, "recipes", "list", "--output-format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var found []aboard.Recipe
	if err := json.Unmarshal([]byte(list), &found); err != nil {
		t.Fatalf("the json listing does not parse: %v\n%s", err, list)
	}
	var copied *aboard.Recipe
	for i := range found {
		if found[i].Name == "human-checklist" {
			copied = &found[i]
		}
	}
	if copied == nil {
		t.Fatalf("the copied recipe was not discovered:\n%s", list)
	}
	if copied.Scope != aboard.ScopeDotAboard {
		t.Errorf("scope = %q, want %q — it is this project's file now", copied.Scope, aboard.ScopeDotAboard)
	}
	if !copied.Valid() {
		t.Fatalf("the copied recipe does not parse: %s", copied.Err)
	}
	if len(copied.ShadowedBy) != 0 {
		t.Errorf("it shadows %v; the library file is not a built-in, so there is nothing under it", copied.ShadowedBy)
	}

	// And its template applies. --check runs the write path's own warnings and
	// posts nothing, so it needs no board and cannot touch one.
	tmpl, _, err := run(t, "--cwd", dir, "recipes", "show", "human-checklist", "--template")
	if err != nil {
		t.Fatal(err)
	}
	var tab map[string]any
	if err := json.Unmarshal([]byte(tmpl), &tab); err != nil {
		t.Fatalf("--template printed something that is not json: %v\n%s", err, tmpl)
	}
	doc, err := json.Marshal(map[string]any{"tabs": []any{tab}})
	if err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	root := NewRootCmd(Options{
		Host: aboard.HostStandalone, Stdout: &out, Stderr: &errOut,
		Stdin: bytes.NewReader(doc),
	})
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"--cwd", dir, "apply", "--check", "--by", "agent-1"})
	if code, _ := ExitCode(root.Execute()); code != 0 {
		t.Fatalf("`recipes show --template | apply --check` exited %d: %s", code, errOut.String()+out.String())
	}
	if strings.Contains(errOut.String(), "warning") {
		t.Errorf("the library recipe's template warns on the write path:\n%s", errOut.String())
	}
}
