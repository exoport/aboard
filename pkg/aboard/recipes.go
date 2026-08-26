// recipes.go — the method, kept out of the skill.
//
//	aboard recipes list            what is available here, and where each came from
//	aboard recipes show <name>     the body, which is what an agent actually follows
//	aboard recipes show <name> --template   just the JSON tab skeleton, so it pipes
//
// A recipe is a markdown file with YAML frontmatter. The frontmatter is the part
// a machine reads (name, description, when_to_use) and the body is the part an
// agent reads. Keeping the bodies OUT of the skill is the whole design: the skill
// stays small, it can never disagree with the recipe it is describing, and a
// project can add or override one without regenerating anything.
//
// Four tiers, first wins by name:
//
//	_apex/aboard/recipes/   the wider workspace's house style
//	_aboard/recipes/        committed, shared with the team
//	.aboard/recipes/        this checkout only, gitignored with the rest
//	built-in                compiled into the binary, ships everywhere it goes
//
// The three directory names are literal strings and not configurable, exactly as
// ape hard-codes `_apex/pipelines`. A discovery order that can be reconfigured is
// one that has to be explained in every error message, and "first wins, in this
// order" stops being a fact anybody can state.
//
// Nothing here fails silently and successfully — the failure this whole codebase
// keeps having. A recipe that does not parse is not skipped: it appears in `list`
// marked invalid with the reason, and `show` on it fails with that reason. A
// recipe shadowed by a higher tier is reported on the winner and still listed.
// The alternative is a file the author is looking at, that the tool behaves as
// though does not exist.

package aboard

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

//go:embed recipes/builtin/*.md
var builtinRecipes embed.FS

const builtinDir = "recipes/builtin"

// TemplateFence is the info string of the one fenced block a recipe may carry.
// A tagged fence rather than a convention about position: a recipe is full of
// code blocks, and "the first ```json" would break the day somebody documented
// a payload above the skeleton.
const TemplateFence = "aboard-template"

// Recipe scopes, most specific first. The constant is what a machine reads; the
// directory is what a person reads, and ScopeLabel maps between them.
const (
	ScopeApex      = "apex"
	ScopeAboard    = "aboard"
	ScopeDotAboard = "dot-aboard"
	ScopeBuiltin   = "builtin"
)

// ScopeLabel renders a scope the way a reader can act on it: the directory the
// file is in, or "built-in" for the compiled ones. `apex` alone does not tell
// anybody where to go and edit it.
func ScopeLabel(scope string) string {
	switch scope {
	case ScopeApex:
		return "_apex/aboard/" + recipeDirName
	case ScopeAboard:
		return "_aboard/" + recipeDirName
	case ScopeDotAboard:
		return DirName + "/" + recipeDirName
	case ScopeBuiltin:
		return "built-in"
	}
	return scope
}

// RecipeRequires is the frontmatter `requires` block.
type RecipeRequires struct {
	// MinSchema is the board schema version this recipe is written against. A
	// recipe that needs a newer one is marked rather than hidden: the reader can
	// still open it and see what they are missing.
	MinSchema int `json:"minSchema,omitempty" yaml:"min_schema"`
}

// Recipe is one file, parsed. Err is set instead of the file being dropped.
type Recipe struct {
	Name        string         `json:"name"              yaml:"name"`
	Description string         `json:"description"       yaml:"description"`
	WhenToUse   string         `json:"whenToUse"         yaml:"when_to_use"`
	Tags        []string       `json:"tags,omitempty"    yaml:"tags,omitempty"`
	Requires    RecipeRequires `json:"requires,omitzero" yaml:"requires,omitempty"`

	// Scope is which tier it came from; Path is where it lives (the embedded
	// path for a built-in, so an error message can still name the file).
	Scope string `json:"scope" yaml:"-"`
	Path  string `json:"path"  yaml:"-"`

	// Body is the markdown below the frontmatter, dropped from list output: a
	// listing carrying every body would be the skill's problem all over again.
	Body string `json:"-" yaml:"-"`
	// Template is the raw contents of the one aboard-template fence, if any.
	Template string `json:"-" yaml:"-"`
	// HasTemplate says whether --template will produce anything, without list
	// output having to carry the skeleton itself.
	HasTemplate bool `json:"hasTemplate" yaml:"-"`

	// ShadowedBy names the lower-tier files this recipe won over, most specific
	// first. Reported on the WINNER, because that is the row a reader is looking
	// at when they wonder why their edit did nothing.
	ShadowedBy []string `json:"shadowedBy,omitempty" yaml:"-"`

	// Err is why this file could not be used, empty when it parsed. A recipe
	// with an Err still appears in `list`; `show` on it fails with this text.
	Err string `json:"error,omitempty" yaml:"-"`
}

// RecipeOut is a recipe as `recipes list --output-format` prints it, and the
// reason it is a SECOND struct rather than more tags on the first.
//
// Recipe is the frontmatter PARSE target: its yaml tags name the keys an author
// writes in a file (`when_to_use`, `min_schema`), and the fields that are not
// frontmatter at all — the tier it was found in, the file it came from, what it
// shadowed, why it did not parse — are `yaml:"-"` because they must never be
// read out of a document. Marshalling that struct for output honoured every one
// of those decisions in the wrong direction: `--output-format yaml` printed a
// different, lossier document than `--output-format json`, missing `scope`,
// `path`, `shadowedBy` and `error` — the four things the command exists to
// report — and renaming `whenToUse` on the way out.
//
// One struct cannot serve both: the input keys are the author's and the output
// keys are the API's, and they genuinely differ. So the parse struct keeps its
// frontmatter tags and this one carries json and yaml tags that are identical to
// each other.
type RecipeOut struct {
	Name        string            `json:"name"                 yaml:"name"`
	Description string            `json:"description"          yaml:"description"`
	WhenToUse   string            `json:"whenToUse"            yaml:"whenToUse"`
	Tags        []string          `json:"tags,omitempty"       yaml:"tags,omitempty"`
	Requires    RecipeRequiresOut `json:"requires,omitzero"    yaml:"requires,omitempty"`
	Scope       string            `json:"scope"                yaml:"scope"`
	Path        string            `json:"path"                 yaml:"path"`
	HasTemplate bool              `json:"hasTemplate"          yaml:"hasTemplate"`
	ShadowedBy  []string          `json:"shadowedBy,omitempty" yaml:"shadowedBy,omitempty"`
	Err         string            `json:"error,omitempty"      yaml:"error,omitempty"`
}

// RecipeRequiresOut is `requires` on the way out: the same value, under the key
// the JSON form uses rather than the frontmatter one.
type RecipeRequiresOut struct {
	MinSchema int `json:"minSchema,omitempty" yaml:"minSchema,omitempty"`
}

// RecipeOutputs converts a discovered list into the output view. Called by the
// command rather than by DiscoverRecipes, so everything inside this package goes
// on working with the parsed shape.
func RecipeOutputs(found []Recipe) []RecipeOut {
	out := make([]RecipeOut, 0, len(found))
	for i := range found {
		r := &found[i]
		out = append(out, RecipeOut{
			Name:        r.Name,
			Description: r.Description,
			WhenToUse:   r.WhenToUse,
			Tags:        r.Tags,
			Requires:    RecipeRequiresOut{MinSchema: r.Requires.MinSchema},
			Scope:       r.Scope,
			Path:        r.Path,
			HasTemplate: r.HasTemplate,
			ShadowedBy:  r.ShadowedBy,
			Err:         r.Err,
		})
	}
	return out
}

// NeedsSchema reports a recipe written against a newer board than this binary.
func (r Recipe) NeedsSchema() bool { return r.Requires.MinSchema > SchemaVersion }

// Valid reports whether the file parsed.
func (r Recipe) Valid() bool { return r.Err == "" }

// TemplateJSON returns the recipe's tab skeleton, or an error naming the recipe.
//
// It does NOT return an empty document when there is no template: an empty
// document handed to `aboard apply` is an empty tab on somebody's screen, which
// is the silent-and-successful failure again.
func (r Recipe) TemplateJSON() (string, error) {
	if r.Err != "" {
		return "", errors.New(r.Err)
	}
	if !r.HasTemplate {
		return "", fmt.Errorf("recipe %q has no %s block — run `aboard recipes show %s` and follow the body",
			r.Name, TemplateFence, r.Name)
	}
	return r.Template, nil
}

// recipeTier is one place recipes are looked for.
type recipeTier struct {
	scope string
	dir   string // empty for the built-in tier
}

// recipeTiers is the discovery order, most specific first.
func recipeTiers(root Root) []recipeTier {
	return []recipeTier{
		{ScopeApex, root.ApexRecipesDir()},
		{ScopeAboard, root.WorkspaceRecipesDir()},
		{ScopeDotAboard, root.RecipesDir()},
		{ScopeBuiltin, ""},
	}
}

// DiscoverRecipes returns every recipe available under root, one entry per NAME,
// sorted by name. A name found in more than one tier resolves to the most
// specific, with the others named in ShadowedBy.
//
// A missing directory is not an error — most projects have none of the three.
// An unreadable one is: a directory that exists and cannot be read is a fact the
// caller needs, not a silently empty tier.
func DiscoverRecipes(root Root) ([]Recipe, error) {
	byName := map[string]Recipe{}
	order := []string{}

	for _, tier := range recipeTiers(root) {
		found, err := readRecipeTier(tier)
		if err != nil {
			return nil, err
		}
		for i := range found {
			r := found[i]
			key := r.Name
			if key == "" {
				// A file whose frontmatter has no name is keyed by its stem, so
				// two broken files do not collapse into one row.
				key = stemOf(r.Path)
			}
			if won, seen := byName[key]; seen {
				won.ShadowedBy = append(won.ShadowedBy, r.Path)
				byName[key] = won
				continue
			}
			byName[key] = r
			order = append(order, key)
		}
	}

	sort.Strings(order)
	out := make([]Recipe, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out, nil
}

// FindRecipe resolves one name through the same precedence. An unknown name is
// an error listing what IS available, because the commonest cause is a near
// miss and a bare "not found" makes the reader run a second command to find out.
func FindRecipe(root Root, name string) (Recipe, error) {
	all, err := DiscoverRecipes(root)
	if err != nil {
		return Recipe{}, err
	}
	names := make([]string, 0, len(all))
	for i := range all {
		if all[i].Name == name {
			return all[i], nil
		}
		if all[i].Name != "" {
			names = append(names, all[i].Name)
		}
	}
	if len(names) == 0 {
		return Recipe{}, fmt.Errorf("no recipe named %q, and none are available here", name)
	}
	return Recipe{}, fmt.Errorf("no recipe named %q — available: %s", name, strings.Join(names, ", "))
}

// BuiltinRecipes is the compiled-in set, in name order. Used by `recipes index`,
// which documents the BINARY and must therefore not see a project's files.
func BuiltinRecipes() ([]Recipe, error) {
	return readRecipeTier(recipeTier{scope: ScopeBuiltin})
}

func readRecipeTier(tier recipeTier) ([]Recipe, error) {
	if tier.scope == ScopeBuiltin {
		return readRecipeFS(builtinRecipes, builtinDir, ScopeBuiltin,
			func(dir, name string) string { return path.Join(dir, name) })
	}
	if info, err := os.Stat(tier.dir); err != nil || !info.IsDir() {
		return nil, nil //nolint:nilerr // a tier that does not exist is the normal case
	}
	return readRecipeFS(os.DirFS(tier.dir), ".", tier.scope,
		func(_, name string) string { return RecipeFile(tier.dir, name) })
}

// readRecipeFS reads one directory of *.md. join builds the path REPORTED for a
// file, which differs from the path read: a built-in reports its embedded path,
// a disk recipe reports where a person would go to edit it.
func readRecipeFS(fsys fs.FS, dir, scope string, join func(string, string) string) ([]Recipe, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("reading recipes in %s: %w", scope, err)
	}
	out := make([]Recipe, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			// A recipe directory is FLAT, and until now a subdirectory of them
			// was dropped without a word — while the how-to told the reader that
			// a recipe not in the listing must have failed frontmatter
			// validation, which sent them to debug the wrong file.
			//
			// Reported rather than recursed: the precedence order is four fixed
			// tiers, and nesting would add a fifth axis with no rule for what
			// shadows what. Reported only when it actually HOLDS recipes, so an
			// unrelated directory sitting there stays quiet.
			if held := recipesInside(fsys, path.Join(dir, e.Name())); held > 0 {
				out = append(out, Recipe{
					Name:  e.Name(),
					Scope: scope,
					Path:  join(dir, e.Name()),
					Err: fmt.Sprintf("is a directory holding %d .md file(s) — recipe directories are flat, "+
						"so move them up one level; nothing inside it is loaded", held),
				})
			}
			continue
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		// README.md is the note `aboard init` leaves saying what the directory is
		// for. It is not a recipe, and reporting it as a broken one every time
		// would be the noise that teaches people to ignore the invalid marker.
		if strings.EqualFold(e.Name(), "README.md") {
			continue
		}
		reportPath := join(dir, e.Name())
		body, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			// One file nobody can read is not a reason to report that the project
			// has no recipes. It aborted the WHOLE discovery — every tier, the
			// built-ins included — so a dangling symlink or a chmod 000 in
			// .aboard/recipes/ took `aboard recipes list` down to an error and
			// left an agent with no recipes at all, when the nine built-ins were
			// compiled into the binary it was running.
			//
			// The same rule the parser already follows: the file becomes a row
			// carrying its reason. "Your recipe cannot be read" is actionable;
			// "recipes are unavailable" is not.
			out = append(out, Recipe{
				Name:  stemOf(reportPath),
				Scope: scope,
				Path:  reportPath,
				Err:   fmt.Sprintf("cannot be read: %v", err),
			})
			continue
		}
		out = append(out, parseRecipe(body, reportPath, scope))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// recipesInside counts the .md files one level down, so a subdirectory is only
// reported when it is plausibly somebody's attempt to organise their recipes.
func recipesInside(fsys fs.FS, dir string) int {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") && !strings.EqualFold(e.Name(), "README.md") {
			n++
		}
	}
	return n
}

// parseRecipe never returns an error: a file that cannot be used becomes a
// Recipe carrying the reason. That is the difference between a tool that says
// "your recipe has no frontmatter" and one that behaves as though the file the
// author is looking at does not exist.
func parseRecipe(data []byte, reportPath, scope string) Recipe {
	r := Recipe{Scope: scope, Path: reportPath}
	stem := stemOf(reportPath)

	fm, body, err := splitFrontmatter(data)
	if err != nil {
		r.Name = stem
		r.Err = "no YAML frontmatter block (a recipe opens with a `---` line and closes with another)"
		return r
	}
	if err := yaml.Unmarshal(fm, &r); err != nil {
		r.Name = stem
		r.Err = fmt.Sprintf("frontmatter is not valid YAML: %v", err)
		return r
	}

	switch {
	case r.Name == "":
		r.Name = stem
		r.Err = "frontmatter has no `name`"
		return r
	case r.Name != stem:
		// Reported under the STEM, not under the name it claims. The reader is
		// looking at a directory listing, and a row named after a frontmatter
		// field they cannot see would send them hunting for a file that does not
		// exist. The error names both, which is the fix they have to make.
		r.Err = fmt.Sprintf("frontmatter name %q does not match the file stem %q — "+
			"`aboard recipes show` takes the name, so the two must agree", r.Name, stem)
		r.Name = stem
		return r
	case strings.TrimSpace(r.Description) == "":
		r.Err = "frontmatter has no `description`"
		return r
	case strings.TrimSpace(r.WhenToUse) == "":
		r.Err = "frontmatter has no `when_to_use`"
		return r
	}

	tmpl, blocks := extractTemplate(string(body))
	if blocks > 1 {
		r.Err = fmt.Sprintf("%d `%s` blocks — a recipe carries at most one", blocks, TemplateFence)
		return r
	}
	if blocks == 1 {
		// The template's only validation, and on purpose the weakest one that
		// catches the real mistake: a skeleton with a trailing comma or an
		// unquoted key, which `aboard apply` would otherwise reject after the
		// agent had already told the human it was done. It does NOT check the
		// shape against the tab schema — a template is a SKELETON, edited before
		// it is applied, and half of them are deliberately partial.
		if !json.Valid([]byte(tmpl)) {
			r.Err = fmt.Sprintf("the `%s` block is not valid JSON", TemplateFence)
			return r
		}
		r.Template, r.HasTemplate = tmpl, true
	}

	r.Body = strings.TrimLeft(string(body), "\n")
	return r
}

func stemOf(p string) string {
	base := path.Base(filepath.ToSlash(p))
	return strings.TrimSuffix(base, ".md")
}

/* ---------- frontmatter ---------- */

var (
	fmBOM       = []byte{0xEF, 0xBB, 0xBF}
	fmDelimiter = []byte("---")
)

// errNoFrontmatter reports a document with no `---`-delimited leading block.
var errNoFrontmatter = errors.New("no YAML frontmatter block")

// splitFrontmatter returns the frontmatter YAML (without its delimiters) and the
// body below it. Both are subslices of data — no copying.
//
// Shaped after ape's internal/frontmatter deliberately: a developer with both
// tools in the same tree should not have to hold two parsers in their head, and
// its tolerances are already the right ones. A UTF-8 BOM, CRLF line endings and
// a closing delimiter at EOF with no trailing newline all work; everything else
// is a parse failure, reported rather than guessed at.
//
// Line-oriented rather than searching for "\n---\n", which is what makes CRLF
// and a final line without a newline behave like the common case.
func splitFrontmatter(data []byte) (fm, body []byte, err error) {
	rest := bytes.TrimPrefix(data, fmBOM)

	lineEnd := bytes.IndexByte(rest, '\n')
	if lineEnd < 0 || !isFMDelimiter(rest[:lineEnd]) {
		return nil, nil, errNoFrontmatter
	}
	rest = rest[lineEnd+1:]

	for offset := 0; offset < len(rest); {
		var line []byte
		next := len(rest)
		if end := bytes.IndexByte(rest[offset:], '\n'); end >= 0 {
			line = rest[offset : offset+end]
			next = offset + end + 1
		} else {
			line = rest[offset:]
		}
		if isFMDelimiter(line) {
			return rest[:offset], rest[next:], nil
		}
		offset = next
	}
	return nil, nil, errNoFrontmatter
}

// isFMDelimiter reports whether a line is exactly `---`, ignoring a trailing CR
// and trailing spaces.
func isFMDelimiter(line []byte) bool {
	return bytes.Equal(bytes.TrimRight(line, " \t\r"), fmDelimiter)
}

/* ---------- the template block ---------- */

// extractTemplate returns the contents of the aboard-template fence and how many
// such fences the body has. The COUNT is returned rather than the first match
// alone so two blocks are an error and not a silent choice of which one wins.
func extractTemplate(body string) (template string, blocks int) {
	var (
		in  bool
		buf strings.Builder
	)
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		fence := strings.TrimSpace(trimmed)
		if !in {
			if isTemplateFence(fence) {
				in, blocks = true, blocks+1
				buf.Reset()
			}
			continue
		}
		if strings.HasPrefix(fence, "```") && strings.TrimRight(fence, "`") == "" {
			in = false
			if blocks == 1 {
				template = buf.String()
			}
			continue
		}
		buf.WriteString(trimmed)
		buf.WriteString("\n")
	}
	if in && blocks == 1 {
		// An unclosed fence at EOF: take what there is. The JSON check in
		// parseRecipe rejects it if the block was truncated mid-object, which is
		// the honest outcome.
		template = buf.String()
	}
	return strings.TrimSpace(template), blocks
}

func isTemplateFence(line string) bool {
	if !strings.HasPrefix(line, "```") {
		return false
	}
	return strings.TrimSpace(strings.TrimLeft(line, "`")) == TemplateFence
}

/* ---------- rendering ---------- */

// descWidth is where a description is cut in the human listing, and the width of
// the rule above it. The one value that IS truncated, and only here: the full
// text is one `recipes show` away and `--output-format json` carries it whole.
const descWidth = 64

// RecipeListHuman is the table `aboard recipes list` prints.
//
// Three columns — name, scope, description — because those are the three a
// reader scans to pick one. Everything else that matters about a row goes on an
// indented line UNDER it rather than in a fourth column: where the file is, what
// it shadowed, and why it will not parse. A row that is fine occupies one line;
// a row with something wrong occupies two and says what.
func RecipeListHuman(recipes []Recipe) string {
	if len(recipes) == 0 {
		return "no recipes found — this binary ships none, which should be impossible; run `aboard version`\n"
	}

	nameWidth, scopeWidth := len("name"), len("scope")
	for i := range recipes {
		nameWidth = max(nameWidth, len(recipes[i].Name))
		scopeWidth = max(scopeWidth, len(ScopeLabel(recipes[i].Scope)))
	}
	pad := strings.Repeat(" ", nameWidth)

	var b strings.Builder
	fmt.Fprintf(&b, "%-*s  %-*s  %s\n", nameWidth, "name", scopeWidth, "scope", "description")
	fmt.Fprintf(&b, "%s  %s  %s\n",
		strings.Repeat("-", nameWidth), strings.Repeat("-", scopeWidth), strings.Repeat("-", descWidth))

	invalid, shadowed := 0, 0
	for i := range recipes {
		r := recipes[i]
		fmt.Fprintf(&b, "%-*s  %-*s  %s\n",
			nameWidth, r.Name, scopeWidth, ScopeLabel(r.Scope), ellipsis(oneLine(r.Description), descWidth))
		if !r.Valid() {
			invalid++
			fmt.Fprintf(&b, "%s  INVALID: %s\n", pad, r.Err)
		}
		if r.NeedsSchema() {
			fmt.Fprintf(&b, "%s  needs schema %d; this board is v%d\n", pad, r.Requires.MinSchema, SchemaVersion)
		}
		for _, other := range r.ShadowedBy {
			shadowed++
			fmt.Fprintf(&b, "%s  shadows %s\n", pad, other)
		}
		// The path, but only where it is somewhere a reader can go and edit. A
		// built-in's path is inside the binary, so printing it on all nine rows
		// would be nine lines of noise around the two that matter.
		if r.Scope != ScopeBuiltin {
			fmt.Fprintf(&b, "%s  %s\n", pad, r.Path)
		}
	}

	fmt.Fprintf(&b, "\n%d recipe(s)", len(recipes))
	if invalid > 0 {
		fmt.Fprintf(&b, ", %d invalid", invalid)
	}
	if shadowed > 0 {
		fmt.Fprintf(&b, ", %d shadowed file(s)", shadowed)
	}
	b.WriteString(". `aboard recipes show <name>` prints one.\n")
	return b.String()
}

// RecipeShowText is the body an agent follows, with a title line naming the
// recipe and saying what it is for.
//
// The frontmatter is stripped: it is metadata for the list, and leaving it on
// would put YAML at the top of something meant to be read as prose. The title
// line replaces it with the same two facts in a form a reader wants.
func RecipeShowText(r Recipe) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s\n\n", r.Name, oneLine(r.Description))
	if r.WhenToUse != "" {
		fmt.Fprintf(&b, "**When to use:** %s\n\n", oneLine(r.WhenToUse))
	}
	if r.NeedsSchema() {
		fmt.Fprintf(&b, "> This recipe wants board schema v%d; this binary writes v%d. Parts of it may not render.\n\n",
			r.Requires.MinSchema, SchemaVersion)
	}
	if body := strings.TrimRight(r.Body, "\n"); body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	if r.HasTemplate {
		fmt.Fprintf(&b, "\n(`aboard recipes show %s --template` prints just the tab skeleton.)\n", r.Name)
	}
	return b.String()
}

// RecipeIndexMarkdown is the index the skill's references/recipes.md is
// generated from: the BUILT-IN recipes only.
//
// Built-in only, deliberately. This file documents the BINARY — it ships in a
// skill directory that is copied between projects, and a table listing one
// project's own recipes would be wrong in every other project it was copied to.
// The paragraph underneath is what makes that limitation harmless: it leads with
// `aboard recipes list`, which is the only complete answer.
//
// Deterministic: sorted by name, no timestamps, no counts that move. `make caps`
// regenerates it and the diff must be empty on a clean tree.
func RecipeIndexMarkdown(recipes []Recipe) string {
	sorted := append([]Recipe(nil), recipes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	b.WriteString("# Recipes\n\n")
	b.WriteString("<!-- Generated from the built-in recipes compiled into the binary. Do not edit.\n")
	b.WriteString("     Regenerate with `" + AppName + " recipes index > <this file>`, or `make caps`\n")
	b.WriteString("     in aboard's own checkout, which also rebuilds the other two. -->\n\n")
	b.WriteString("| name | description | when to use | scope |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for i := range sorted {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n",
			sorted[i].Name, mdCell(sorted[i].Description), mdCell(sorted[i].WhenToUse), ScopeLabel(ScopeBuiltin))
	}
	b.WriteString("\n")
	b.WriteString(recipeIndexParagraph)
	return b.String()
}

// recipeIndexParagraph is FIXED TEXT, not assembled from data, and that is the
// point: it is the half of the index that stays true when the built-in set
// changes. The table can only ever list what is compiled in, and this says so —
// an agent that reads the table and acts is incomplete, an agent that reads the
// table and believes it is the whole set is wrong.
const recipeIndexParagraph = "**The table above is only the recipes shipped in the binary. This project may\n" +
	"have more.** Run `aboard recipes list` to see every recipe actually available\n" +
	"here — one line per recipe (`name`, `scope`, `description`), and, indented\n" +
	"under it, the file's path and anything it shadows. A clean built-in is one\n" +
	"line; a recipe with something worth knowing about it is two, and says what.\n" +
	"`aboard recipes list --output-format json` carries the whole record, including\n" +
	"`whenToUse`, `tags`, `requires`, `hasTemplate` and `shadowedBy`. It is the only\n" +
	"complete answer, because a project's own recipes are files on disk that no\n" +
	"generated document can know about. `aboard recipes show <name>` prints the\n" +
	"recipe body to stdout; read it and follow it. If the recipe carries a tab\n" +
	"skeleton, `aboard recipes show <name> --template` prints just that JSON, ready to\n" +
	"edit and hand to `aboard apply` — and a recipe with no skeleton exits non-zero\n" +
	"naming itself, rather than printing an empty document you would apply as an\n" +
	"empty tab. When two recipes share a name the first of `_apex/aboard/recipes/` →\n" +
	"`_aboard/recipes/` → `.aboard/recipes/` → built-in wins, and the winning row\n" +
	"names the file it shadowed rather than hiding it — a project that overrides a\n" +
	"built-in recipe is doing something deliberate and you should be able to see\n" +
	"what it replaced.\n"

// mdCell makes a value safe for a markdown table cell: one line, pipes escaped.
// Never truncated — a description too long for a table is a description to
// rewrite in the recipe, not one to cut here where the author cannot see it.
func mdCell(s string) string {
	return strings.ReplaceAll(oneLine(s), "|", "\\|")
}

// oneLine collapses newlines and runs of whitespace. `when_to_use` is commonly
// written across two lines in the file and has to be one line in a table.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// ellipsis cuts to n, on a rune boundary and then on a word boundary if there is
// one nearby, so a truncation never produces a broken character or half a word.
func ellipsis(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n-1]
	for !utf8.ValidString(cut) && cut != "" {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndexByte(cut, ' '); i > n/2 {
		cut = cut[:i]
	}
	return cut + "…"
}
