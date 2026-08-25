package aboard

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRoot is testdata/recipes, a tree carrying all three on-disk tiers plus
// the deliberately broken files. It is a Root by construction rather than by
// FindRoot: the point of the fixture is the recipe directories, not discovery.
func fixtureRoot(t *testing.T) Root {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "recipes"))
	if err != nil {
		t.Fatal(err)
	}
	return Root(abs)
}

func recipeByName(t *testing.T, list []Recipe, name string) Recipe {
	t.Helper()
	for _, r := range list {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no recipe named %q in %d discovered", name, len(list))
	return Recipe{}
}

// Precedence is the whole contract of the four tiers, and it is stated in three
// documents. Asserted on the file that actually won, not on the scope label,
// because a scope constant could be right while the wrong body was read.
func TestRecipePrecedence(t *testing.T) {
	found, err := DiscoverRecipes(fixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}

	everywhere := recipeByName(t, found, "everywhere")
	if everywhere.Scope != ScopeApex {
		t.Errorf("scope = %q, want %q — the _apex copy must win", everywhere.Scope, ScopeApex)
	}
	if !strings.Contains(everywhere.Body, "from _apex/aboard/recipes") {
		t.Errorf("the body came from the wrong tier:\n%s", everywhere.Body)
	}

	// A file on disk beats a built-in of the same name. That is what makes
	// overriding possible at all, and it is the direction most likely to be
	// broken by a tier-ordering typo, since the built-in tier is the one that
	// always has something.
	override := recipeByName(t, found, "apply-a-write")
	if override.Scope != ScopeAboard {
		t.Errorf("apply-a-write resolved to %q; a workspace file must beat the built-in", override.Scope)
	}
}

// Shadowing is REPORTED, not hidden. The row a reader is looking at when they
// wonder why their edit did nothing is the winner's, so that is where the losers
// are named.
func TestRecipeShadowReport(t *testing.T) {
	found, err := DiscoverRecipes(fixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}

	everywhere := recipeByName(t, found, "everywhere")
	if len(everywhere.ShadowedBy) != 2 {
		t.Fatalf("everywhere shadows %d file(s), want 2: %v", len(everywhere.ShadowedBy), everywhere.ShadowedBy)
	}
	// Most specific first: the workspace copy, then the project one.
	if !strings.Contains(everywhere.ShadowedBy[0], filepath.Join("_aboard", "recipes")) {
		t.Errorf("first shadowed file is %q, want the _aboard copy", everywhere.ShadowedBy[0])
	}
	if !strings.Contains(everywhere.ShadowedBy[1], filepath.Join(".aboard", "recipes")) {
		t.Errorf("second shadowed file is %q, want the .aboard copy", everywhere.ShadowedBy[1])
	}

	override := recipeByName(t, found, "apply-a-write")
	if len(override.ShadowedBy) != 1 || !strings.Contains(override.ShadowedBy[0], builtinDir) {
		t.Errorf("apply-a-write shadows %v, want the built-in file", override.ShadowedBy)
	}

	// And the human table says so, which is the form anybody actually reads.
	human := RecipeListHuman(found)
	if !strings.Contains(human, "shadows ") {
		t.Errorf("the human listing never mentions shadowing:\n%s", human)
	}
}

// A file that cannot be used is listed with the reason. Silently skipping it is
// the failure this whole package is written against: the author is looking at
// the file, and the tool behaves as though it is not there.
func TestInvalidRecipesAreReportedNotSkipped(t *testing.T) {
	found, err := DiscoverRecipes(fixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		wantErr string
	}{
		{"no-frontmatter", "frontmatter"},
		{"mismatched", "file stem"},
	} {
		r := recipeByName(t, found, tc.name)
		if r.Valid() {
			t.Errorf("%s: parsed cleanly, want an error", tc.name)
			continue
		}
		if !strings.Contains(r.Err, tc.wantErr) {
			t.Errorf("%s: error %q does not mention %q", tc.name, r.Err, tc.wantErr)
		}
	}

	human := RecipeListHuman(found)
	if !strings.Contains(human, "INVALID") {
		t.Errorf("the human listing does not mark the invalid rows:\n%s", human)
	}

	// `show` on a broken file must fail with the reason rather than print
	// half a document.
	broken := recipeByName(t, found, "no-frontmatter")
	if _, err := broken.TemplateJSON(); err == nil {
		t.Error("TemplateJSON on an invalid recipe returned no error")
	}
}

// The template is found by its FENCE TAG, not by position: a recipe is full of
// code blocks, and the fixture puts a decoy ```json above the real one.
func TestRecipeTemplateExtraction(t *testing.T) {
	root := fixtureRoot(t)

	r, err := FindRecipe(root, "with-template")
	if err != nil {
		t.Fatal(err)
	}
	if !r.HasTemplate {
		t.Fatal("with-template reports no template")
	}
	body, err := r.TemplateJSON()
	if err != nil {
		t.Fatal(err)
	}
	var tab map[string]any
	if err := json.Unmarshal([]byte(body), &tab); err != nil {
		t.Fatalf("the extracted template is not valid json: %v\n%s", err, body)
	}
	if tab["name"] != "Fixture tab" {
		t.Errorf("extracted the decoy block, not the template: %s", body)
	}

	// A recipe with no template must FAIL rather than print an empty document —
	// an empty document handed to `aboard apply` is an empty tab on the human's
	// screen, which is the silent-and-successful failure again.
	plain, err := FindRecipe(root, "project-only")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.TemplateJSON(); err == nil {
		t.Error("a recipe with no template produced one")
	} else if !strings.Contains(err.Error(), "project-only") {
		t.Errorf("the error does not name the recipe: %v", err)
	}

	// The staged user example carries a real template; it is in the fixture
	// precisely so extraction is proven against a file nobody wrote for the test.
	wizard, err := FindRecipe(root, "decision-wizard-with-live-summary")
	if err != nil {
		t.Fatal(err)
	}
	got, err := wizard.TemplateJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid([]byte(got)) {
		t.Errorf("the user example's template is not valid json:\n%s", got)
	}
}

// requires.min_schema marks a recipe rather than hiding it: the reader can still
// open it and see what they are missing.
func TestRecipeMinSchema(t *testing.T) {
	r, err := FindRecipe(fixtureRoot(t), "future-schema")
	if err != nil {
		t.Fatal(err)
	}
	if r.Requires.MinSchema != 99 {
		t.Fatalf("min_schema = %d, want 99", r.Requires.MinSchema)
	}
	if !r.NeedsSchema() {
		t.Error("a recipe wanting schema 99 does not report needing a newer schema")
	}
	if !r.Valid() {
		t.Errorf("needing a newer schema made the recipe invalid: %s", r.Err)
	}
	if !strings.Contains(RecipeListHuman([]Recipe{r}), "needs schema 99") {
		t.Error("the human listing does not say the recipe needs a newer schema")
	}
}

// Every built-in must parse. They are compiled in, so a broken one ships to
// every project the binary reaches and there is no file for anyone to fix
// locally.
func TestBuiltinRecipesAllParse(t *testing.T) {
	built, err := BuiltinRecipes()
	if err != nil {
		t.Fatal(err)
	}
	if len(built) < 9 {
		t.Fatalf("%d built-in recipes, want at least the nine that shipped", len(built))
	}
	for _, r := range built {
		if !r.Valid() {
			t.Errorf("%s: %s", r.Path, r.Err)
		}
		if r.Scope != ScopeBuiltin {
			t.Errorf("%s: scope %q, want %q", r.Path, r.Scope, ScopeBuiltin)
		}
	}
}

// A bare directory with no recipes at all still answers with the built-ins:
// `aboard recipes list` has to work from a copied binary in a project that has
// never held a board, which is the same property `capabilities` has.
func TestRecipesAnswerWithNoProject(t *testing.T) {
	found, err := DiscoverRecipes(Root(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("an empty project found no recipes; the built-ins are compiled in")
	}
	for _, r := range found {
		if r.Scope != ScopeBuiltin {
			t.Errorf("%s came from %q in an empty project", r.Name, r.Scope)
		}
	}
}

// An unknown name lists what IS available. A bare "not found" makes the reader
// run a second command, and the commonest cause is a near miss.
func TestFindRecipeUnknown(t *testing.T) {
	_, err := FindRecipe(fixtureRoot(t), "no-such-recipe")
	if err == nil {
		t.Fatal("an unknown recipe name returned no error")
	}
	if !strings.Contains(err.Error(), "no-such-recipe") {
		t.Errorf("the error does not name what was asked for: %v", err)
	}
	if !strings.Contains(err.Error(), "apply-a-write") {
		t.Errorf("the error does not list the available names: %v", err)
	}
}

// The index is committed into the skill and regenerated by `make caps`, so it
// must be byte-identical between two runs: anything that moves turns every
// regeneration into a diff and the file stops being read.
func TestRecipeIndexIsStable(t *testing.T) {
	built, err := BuiltinRecipes()
	if err != nil {
		t.Fatal(err)
	}
	first := RecipeIndexMarkdown(built)
	second := RecipeIndexMarkdown(built)
	if first != second {
		t.Fatal("the recipe index differs between two calls in one process")
	}

	// Regenerated from a fresh read, which is what `make caps` actually does.
	again, err := BuiltinRecipes()
	if err != nil {
		t.Fatal(err)
	}
	if RecipeIndexMarkdown(again) != first {
		t.Fatal("the recipe index differs between two reads of the same files")
	}

	if !strings.HasPrefix(first, "# Recipes\n") {
		t.Errorf("the index does not open with its heading:\n%.80s", first)
	}
	if !strings.Contains(first, "| name | description | when to use | scope |") {
		t.Error("the index has no table header in the declared column order")
	}
	if !strings.Contains(first, "`aboard recipes list`") {
		t.Error("the index does not point at the only complete answer")
	}
	if !strings.HasSuffix(first, "\n") || strings.Contains(first, " \n") {
		t.Error("the index has trailing whitespace or no final newline")
	}

	// It documents the BINARY. A project's own recipes must never reach it, or
	// the file — which is copied between projects inside a skill directory —
	// would be wrong everywhere it was copied to.
	for _, r := range built {
		if r.Scope != ScopeBuiltin {
			t.Errorf("the index would carry %s from %q", r.Name, r.Scope)
		}
	}
	if strings.Contains(first, "project-only") {
		t.Error("the index carries a recipe from the fixture project")
	}
}

// The frontmatter splitter's tolerances, stated so they cannot be narrowed by
// accident: a BOM, CRLF, and a closing delimiter at EOF with no trailing
// newline all appear in files people actually commit.
func TestFrontmatterTolerances(t *testing.T) {
	body := "---\nname: x\n---\nbody\n"
	for _, tc := range []struct {
		name string
		data string
	}{
		{"plain", body},
		{"bom", "\xEF\xBB\xBF" + body},
		{"crlf", strings.ReplaceAll(body, "\n", "\r\n")},
		{"no trailing newline", "---\nname: x\n---"},
		{"padded delimiter", "---  \nname: x\n--- \nbody\n"},
	} {
		if _, _, err := splitFrontmatter([]byte(tc.data)); err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}

	for _, tc := range []struct {
		name string
		data string
	}{
		{"no block", "# just markdown\n"},
		{"unclosed", "---\nname: x\nbody with no closing delimiter\n"},
		{"delimiter not first", "\n---\nname: x\n---\n"},
	} {
		if _, _, err := splitFrontmatter([]byte(tc.data)); err == nil {
			t.Errorf("%s: parsed, want an error", tc.name)
		}
	}
}

// Two template blocks is an ERROR, not a silent choice of which one wins. The
// author wrote two on purpose or by accident, and either way the tool guessing
// is worse than the tool asking.
func TestTwoTemplateBlocksIsAnError(t *testing.T) {
	src := "---\nname: two\ndescription: \"d\"\nwhen_to_use: \"w\"\n---\n\n" +
		"```" + TemplateFence + "\n{\"a\":1}\n```\n\n" +
		"```" + TemplateFence + "\n{\"b\":2}\n```\n"
	r := parseRecipe([]byte(src), "two.md", ScopeDotAboard)
	if r.Valid() {
		t.Fatal("two template blocks parsed cleanly")
	}
	if !strings.Contains(r.Err, TemplateFence) {
		t.Errorf("the error does not name the fence: %s", r.Err)
	}
}

// A template that is not JSON is caught HERE rather than by `aboard apply` after
// the agent has already told the human it was done.
func TestInvalidTemplateJSONIsReported(t *testing.T) {
	src := "---\nname: bad\ndescription: \"d\"\nwhen_to_use: \"w\"\n---\n\n" +
		"```" + TemplateFence + "\n{ \"trailing\": \"comma\", }\n```\n"
	r := parseRecipe([]byte(src), "bad.md", ScopeDotAboard)
	if r.Valid() {
		t.Fatal("an invalid template block parsed cleanly")
	}
	if !strings.Contains(r.Err, "JSON") {
		t.Errorf("the error does not say what is wrong: %s", r.Err)
	}
}

// Missing required fields are named one at a time, so the author fixes the field
// rather than re-reading the format.
func TestRequiredFrontmatterFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		fm   string
		want string
	}{
		{"no name", "description: \"d\"\nwhen_to_use: \"w\"", "name"},
		{"no description", "name: r\nwhen_to_use: \"w\"", "description"},
		{"no when_to_use", "name: r\ndescription: \"d\"", "when_to_use"},
	} {
		r := parseRecipe([]byte("---\n"+tc.fm+"\n---\nbody\n"), "r.md", ScopeDotAboard)
		if r.Valid() {
			t.Errorf("%s: parsed cleanly", tc.name)
			continue
		}
		if !strings.Contains(r.Err, tc.want) {
			t.Errorf("%s: error %q does not name %q", tc.name, r.Err, tc.want)
		}
	}
}

// The README `aboard init` leaves in .aboard/recipes/ is not a recipe. Reporting
// it as a broken one on every listing would be the noise that teaches people to
// ignore the INVALID marker, which is where the real failures show up.
func TestRecipesReadmeIsNotARecipe(t *testing.T) {
	found, err := DiscoverRecipes(fixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range found {
		if strings.EqualFold(r.Name, "README") {
			t.Errorf("README.md was read as a recipe (%s)", r.Path)
		}
	}
}
