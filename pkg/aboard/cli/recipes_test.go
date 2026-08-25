package cli

import (
	"encoding/json"
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
