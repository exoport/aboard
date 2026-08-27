package aboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
	for i := range list {
		if list[i].Name == name {
			return list[i]
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

	// And against a file nobody wrote for the test: the decision wizard, read out
	// of the repository's recipe LIBRARY. It was staged in this fixture, then
	// shipped as a built-in, and is neither now — it is one of the two files in
	// `recipes/`, which no binary embeds and every project reaches by copying.
	// Read from there rather than from a second copy, because two copies of one
	// document are two documents that can disagree.
	wizard := libraryRecipe(t, "decision-wizard-with-live-summary")
	got, err := wizard.TemplateJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid([]byte(got)) {
		t.Errorf("the library recipe's template is not valid json:\n%s", got)
	}
}

// libraryDir is the repository's top-level `recipes/` folder — the library. It
// is NOT a discovery tier and NOT embedded: a project gets one of these files by
// copying it into `.aboard/recipes/` (or one of the other two directories), and
// that copy is what TestALibraryRecipeIsDiscoveredWhenCopiedIn exercises at the
// CLI boundary.
func libraryDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "recipes"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// LibraryRecipes parses every file in it, through the same reader the on-disk
// tiers use, so README.md is skipped and a broken file arrives carrying its
// reason rather than being dropped.
func libraryRecipes(t *testing.T) []Recipe {
	t.Helper()
	dir := libraryDir(t)
	found, err := readRecipeFS(os.DirFS(dir), ".", "library",
		func(_, name string) string { return RecipeFile(dir, name) })
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func libraryRecipe(t *testing.T, name string) Recipe {
	t.Helper()
	return recipeByName(t, libraryRecipes(t), name)
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

// One file nobody can read is not a reason to report that the project has no
// recipes. `readRecipeFS` returned an error for it, which aborted the WHOLE
// discovery — every tier, the built-ins included — so a dangling symlink
// or a chmod 000 in `.aboard/recipes/` took `aboard recipes list` down to a bare
// error message while the recipes the agent needed sat compiled into the binary
// it was running.
func TestDiscoverySurvivesAnUnreadableRecipe(t *testing.T) {
	root := Root(t.TempDir())
	dir := root.RecipesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "usable.md")
	if err := os.WriteFile(good, []byte("---\nname: usable\ndescription: a recipe that reads\nwhen_to_use: whenever\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A DANGLING SYMLINK: fs.ReadDir lists it, fs.ReadFile fails on it. The
	// unreadable case that needs no root and no special filesystem.
	if err := os.Symlink(filepath.Join(dir, "nothing-here.md"), filepath.Join(dir, "dangling.md")); err != nil {
		t.Fatal(err)
	}

	found, err := DiscoverRecipes(root)
	if err != nil {
		t.Fatalf("one unreadable file aborted discovery: %v", err)
	}

	broken := recipeByName(t, found, "dangling")
	if broken.Valid() {
		t.Error("the dangling recipe was reported as valid")
	}
	if !strings.Contains(broken.Err, "cannot be read") {
		t.Errorf("the reason does not say what is wrong: %q", broken.Err)
	}
	if r := recipeByName(t, found, "usable"); !r.Valid() {
		t.Errorf("the readable recipe beside it was lost: %q", r.Err)
	}
	// And the built-ins, which have nothing to do with this project's directory,
	// are still there. That is the half the abort was really costing.
	if r := recipeByName(t, found, "apply-a-write"); !r.Valid() {
		t.Error("a built-in recipe was lost to an unreadable project file")
	}
	if !strings.Contains(RecipeListHuman(found), "INVALID") {
		t.Error("the human listing does not mark the unreadable row")
	}
}

// A recipe directory is FLAT, and a subdirectory of one used to be dropped
// without a word — while the how-to told the reader that a recipe missing from
// the listing must have failed frontmatter validation. That sentence sent them
// to debug the wrong file, which is worse than saying nothing.
//
// Reported rather than recursed: the precedence order is four fixed tiers, and
// nesting would add a fifth axis with no rule for what shadows what.
func TestANestedRecipeDirectoryIsReportedNotDropped(t *testing.T) {
	root := Root(t.TempDir())
	dir := root.RecipesDir()
	if err := os.MkdirAll(filepath.Join(dir, "team"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("---\nname: nested\ndescription: a recipe someone filed away\nwhen_to_use: never, it is not loaded\n---\n\nBody.\n")
	if err := os.WriteFile(filepath.Join(dir, "team", "nested.md"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	found, err := DiscoverRecipes(root)
	if err != nil {
		t.Fatal(err)
	}
	// The recipe itself is not loaded — that is the behaviour, not the bug.
	for i := range found {
		if found[i].Name == "nested" && found[i].Valid() {
			t.Error("a recipe in a subdirectory was loaded; discovery is meant to be flat")
		}
	}
	// But the directory is on the listing, saying why.
	row := recipeByName(t, found, "team")
	if row.Valid() {
		t.Fatal("the subdirectory was reported as a usable recipe")
	}
	for _, want := range []string{"flat", "directory"} {
		if !strings.Contains(row.Err, want) {
			t.Errorf("the reason does not mention %q: %q", want, row.Err)
		}
	}
}

// An unrelated directory sitting in a recipes tier stays quiet: reporting every
// stray directory is the noise that teaches people to ignore the INVALID marker.
func TestAnEmptyDirectoryInARecipesTierIsNotReported(t *testing.T) {
	root := Root(t.TempDir())
	dir := root.RecipesDir()
	if err := os.MkdirAll(filepath.Join(dir, "notes", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes", "scratch.txt"), []byte("not a recipe"), 0o644); err != nil {
		t.Fatal(err)
	}

	found, err := DiscoverRecipes(root)
	if err != nil {
		t.Fatal(err)
	}
	for i := range found {
		if found[i].Name == "notes" {
			t.Errorf("a directory holding no recipes was reported: %q", found[i].Err)
		}
	}
}

// A recipe's template is a SKELETON an agent hands to `aboard apply`
// after filling it in, and both halves of that are asserted here.
//
// It must be a tab skeleton — no `id`, no `rev`, no `updatedAt`: those belong to
// the document and to the server, and a template that carried one would teach
// every agent that reads it to write a field the server owns. That is exactly
// how the spike's stale `"version": 2` example got copied into a real write and
// blanked a board.
//
// And it must APPLY CLEAN. `ui` is the type CLAUDE.md tells agents to prefer and
// the one that fails silently and successfully: an unknown prop renders nothing
// at all, `apply` still prints `applied`, and the only instrument left is the
// human looking at the screen. A skeleton shipped in the binary would spread that
// mistake to every project the binary reaches, so it is checked by the same
// function the write path runs.
//
// BOTH SOURCES, on the same assertions. The built-ins travel inside the binary;
// the library in `recipes/` travels by `cp`. Nothing about the second makes a
// wrong `ui` prop cheaper — it is copied into somebody's project and drawn on
// somebody's screen either way, and it is checked by NOTHING at runtime, since
// no compile step ever reads it. Missing this walk was the real cost of moving
// the two files out: they were covered when they were built-ins, and a move that
// silently dropped their only check is the failure this repo keeps having.
func TestBuiltinTemplatesAreCleanTabSkeletons(t *testing.T) {
	built, err := BuiltinRecipes()
	if err != nil {
		t.Fatal(err)
	}
	library := libraryRecipes(t)

	// A floor on the library too, because the assertions below are all "for each
	// file found": an empty `recipes/` would pass every one of them silently,
	// which is precisely how a deleted file gets away.
	if len(library) < 2 {
		t.Fatalf("%d recipes in the library, want at least the two that live there", len(library))
	}

	withTemplate := map[string]bool{}
	for _, src := range []struct {
		where   string
		recipes []Recipe
	}{
		{"built-in", built},
		{"library", library},
	} {
		for _, r := range src.recipes {
			// Frontmatter complete and the file parsed at all. For a built-in
			// TestBuiltinRecipesAllParse already says so; for a library file this
			// is the only place that does.
			if !r.Valid() {
				t.Errorf("%s %s: %s", src.where, r.Path, r.Err)
				continue
			}
			if !r.HasTemplate {
				continue
			}
			withTemplate[r.Name] = true

			tmpl, err := r.TemplateJSON()
			if err != nil {
				t.Errorf("%s %s: %v", src.where, r.Name, err)
				continue
			}
			var tab map[string]any
			if err := json.Unmarshal([]byte(tmpl), &tab); err != nil {
				t.Errorf("%s %s: the template is not a JSON object: %v", src.where, r.Name, err)
				continue
			}
			for _, managed := range []string{"id", "rev", "updatedAt", "version", "lastEditedBy", "touched"} {
				if _, present := tab[managed]; present {
					t.Errorf("%s %s: the template sets %q, which the document or the server owns",
						src.where, r.Name, managed)
				}
			}
			if _, ok := tab["type"].(string); !ok {
				t.Errorf("%s %s: the template has no `type`, so nothing can render it", src.where, r.Name)
			}

			// Wrapped in a document exactly as `apply` receives one, and checked by
			// the function the write path itself calls.
			tab["id"] = "bb1"
			doc, err := json.Marshal(map[string]any{"tabs": []any{tab}})
			if err != nil {
				t.Fatal(err)
			}
			if warnings := writeWarnings(WebFS(), doc); len(warnings) > 0 {
				t.Errorf("%s %s: the template warns on the write path:\n%s",
					src.where, r.Name, strings.Join(warnings, "\n"))
			}
		}
	}

	// A floor, not the exact set: a later recipe may add a template and must not
	// have to edit this list. These three are the ones whose whole point is a
	// tab you can apply, and two of them are `ui` trees, where the write-time
	// check above is the only thing standing between a wrong prop and a blank
	// panel on somebody's screen. The two `ui` ones are library files now, which
	// is why the loop above covers both sources rather than the binary alone.
	for _, name := range []string{
		"ask-for-a-decision",
		"decision-wizard-with-live-summary",
		"human-checklist",
	} {
		if !withTemplate[name] {
			t.Errorf("%s ships no `%s` block", name, TemplateFence)
		}
	}
}

// The library is not a discovery tier. Nothing embeds `recipes/`, nothing walks
// it at runtime, and a project that has not copied a file out of it must not see
// its recipes — otherwise the split between "compiled in" and "copy it yourself"
// would exist only in the documentation.
func TestTheLibraryIsNotADiscoveryTier(t *testing.T) {
	// A project that looks EXACTLY like aboard's own checkout: a top-level
	// `recipes/` holding the library, beside a root with no recipe tier of its
	// own. An empty temp directory would not do — it has no `recipes/` for a
	// fourth tier to find, so it passes whether or not one exists, and the
	// assertion below would be about the built-ins rather than about the
	// library. This is the arrangement the split has to survive, and it is the
	// one aboard's own repository is in.
	dir := t.TempDir()
	beside := filepath.Join(dir, "recipes")
	if err := os.MkdirAll(beside, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, r := range libraryRecipes(t) {
		body, err := os.ReadFile(r.Path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(RecipeFile(beside, r.Name+".md"), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	found, err := DiscoverRecipes(Root(dir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range found {
		if found[i].Scope != ScopeBuiltin {
			t.Errorf("%s came from %q in a project whose only recipes are the library beside it",
				found[i].Name, found[i].Scope)
		}
	}
	for _, name := range []string{"decision-wizard-with-live-summary", "human-checklist"} {
		for i := range found {
			if found[i].Name == name {
				t.Errorf("%s is discoverable without being copied in; it is a library file, not a built-in", name)
			}
		}
	}

	// And the generated index cannot carry them either: it is written into a
	// skill directory that is copied between projects, where a path into
	// aboard's own checkout names nothing.
	index := RecipeIndexMarkdown(func() []Recipe { r, _ := BuiltinRecipes(); return r }())
	for _, name := range []string{"decision-wizard-with-live-summary", "human-checklist"} {
		if strings.Contains(index, "| `"+name+"` |") {
			t.Errorf("the generated index has a table row for the library recipe %s", name)
		}
	}
	// It does say the library exists, though. A library nothing agent-facing
	// mentions is a library nobody copies from.
	if !strings.Contains(index, "`recipes/`") {
		t.Error("the generated index never mentions the repository's recipe library")
	}
}

// `recipes/README.md` is the library's INDEX, and it is the only one the library
// can have: the generated table in the skill is built from the recipes compiled
// into the binary, and no binary embeds this folder. So the index is written by
// hand — and a hand-maintained copy of what ships is a copy that drifts. This
// repository has already paid that bill once: the skill's table went on naming a
// built-in that had been renamed, and nothing anywhere failed, which is why
// `capabilities --check` gates that file now. A gate that exists for the
// generated index and not for the hand-written one is a gate on the safer half.
//
// Three claims, and the first is the one that actually goes wrong — somebody
// adds a third recipe and the table still lists two:
//
//   - the table's rows and the folder's recipes are the SAME SET, named in both
//     directions so the failure says which way round it is;
//   - every row links to a file that is there, because a row is useless as an
//     index entry if its link 404s (`make docs-check` only walks `docs/`, so
//     nothing else checks these);
//   - every row's "when to use" is still the recipe's own `when_to_use`.
//
// Backticks are normalised away and nothing else is. The README is prose and may
// mark `gate` up as code where YAML frontmatter cannot; it may not paraphrase,
// because then two documents describe one recipe and a reader picking between
// them has no way to tell which is current.
func TestTheLibraryReadmeIsAnIndexOfTheLibrary(t *testing.T) {
	readme := RecipeFile(libraryDir(t), "README.md")
	body, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("the library has no index: %v", err)
	}

	// A row is `| [`name`](link) | when to use |`. Matched strictly: a loose
	// pattern that also matched the prose above the table would make an empty
	// table look full.
	row := regexp.MustCompile("(?m)^\\|\\s*\\[`([^`]+)`\\]\\(([^)]+)\\)\\s*\\|\\s*(.*?)\\s*\\|\\s*$")
	listed := map[string]string{} // name -> when to use
	for _, m := range row.FindAllStringSubmatch(string(body), -1) {
		name, link, when := m[1], m[2], m[3]
		if _, dup := listed[name]; dup {
			t.Errorf("%s is listed twice in the library index", name)
		}
		listed[name] = when
		if _, err := os.Stat(RecipeFile(libraryDir(t), name+".md")); err != nil {
			t.Errorf("the index row for %s does not name a file in the library: %v", name, err)
		}
		if want := name + ".md"; link != want {
			t.Errorf("the index row for %s links to %q, want %q", name, link, want)
		}
	}

	for _, r := range libraryRecipes(t) {
		when, ok := listed[r.Name]
		if !ok {
			t.Errorf("%s is in the library and not in its index — add a row to recipes/README.md", r.Name)
			continue
		}
		delete(listed, r.Name)
		if normaliseIndexProse(when) != normaliseIndexProse(r.WhenToUse) {
			t.Errorf("the index row for %s no longer says what its frontmatter says:\n index: %s\n  file: %s",
				r.Name, when, r.WhenToUse)
		}
	}
	for name := range listed {
		t.Errorf("the library index lists %s, which is not in recipes/ — remove the row or restore the file", name)
	}
}

// normaliseIndexProse drops the backticks a markdown table may add and collapses
// whitespace a table cell may have re-wrapped. Nothing else: the comparison is
// meant to catch a paraphrase, so it must not be lenient enough to allow one.
func normaliseIndexProse(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "`", "")), " ")
}
