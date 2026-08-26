// init.go — the one command that creates a root instead of finding one.
//
// Every other command walks UP from --cwd looking for `.aboard/`, because a
// board belongs to a project and not to whichever subdirectory you happened to
// be in. `init` cannot do that: there is nothing to find yet, and a walk would
// mean `aboard init` in a subdirectory of a project that already has a board
// quietly doing nothing while reporting success.
//
// So init creates a root WHERE YOU STAND and refuses when that would produce a
// second one — naming the root it found, because "it already exists" without a
// path sends the reader off to run `find`.

package aboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The example board, compiled in.
//
// It lives under pkg/aboard/ and not at testdata/example-board/ for one
// mechanical reason: //go:embed cannot reach upward out of its own package
// directory. A fixture at the repo root is reachable by tests and by nothing
// else, and `aboard init --example` has to work from a copied binary in a
// project that has no checkout at all.
//
//go:embed example/aboard.json
var exampleFS embed.FS

const exampleFile = "example/aboard.json"

// A FILE-MODE NOTE, for the three //nolint:gosec below.
//
// Everything the board writes is 0o644, and that is a repo-wide policy, not an
// oversight here: the state document, the uploads and the journal are read by
// the developer's editor, by a VS Code extension running as them, and by every
// other agent session in the project. gosec's G306 wants 0o600 for any file
// written by a program, which is the right default for a service handling other
// people's secrets and the wrong one for a project directory whose whole purpose
// is to be shared between the tools the developer already runs.
//
// It is annotated at each call site rather than excluded in .golangci.yaml so
// that a NEW write has to make the same decision consciously.

// initActor is the author recorded on the document init writes. Not an agent
// name and not "human": nobody edited anything, a command created a file, and
// the journal should not claim otherwise.
const initActor = "init"

// GitignoreLine is what a project adds to stop tracking its board. One line,
// covering the document, the uploads, the recipes and the whole run directory —
// which is the point of nesting them all under one directory.
const GitignoreLine = DirName + "/"

// InitConfig is what `aboard init` was asked to do.
type InitConfig struct {
	// Dir is where a root is created when none exists. Absolute or relative to
	// the process; resolved here.
	Dir string
	// Name is the board name, "" for the project's default board.
	Name string
	// Example seeds the example board instead of an empty one.
	Example bool
	// Gitignore appends GitignoreLine to the project's .gitignore if absent.
	Gitignore bool
}

// InitResult is what it did, reported rather than printed so the human form and
// --output-format render the same values.
type InitResult struct {
	Root      string `json:"root"           yaml:"root"`
	Name      string `json:"name,omitempty" yaml:"name,omitempty"`
	StateFile string `json:"stateFile"      yaml:"stateFile"`
	// Created lists every directory and file this run brought into existence, in
	// the order it made them. A run that found everything already there reports
	// an empty list rather than claiming work it did not do.
	Created []string `json:"created" yaml:"created"`
	// Tabs is how many tabs the seeded document holds: 0 for an empty board.
	Tabs int `json:"tabs" yaml:"tabs"`
	// Seeded is true when --example was used.
	Seeded bool `json:"seeded" yaml:"seeded"`
	// GitignoreLine is the line a project needs, always reported.
	GitignoreLine string `json:"gitignoreLine" yaml:"gitignoreLine"`
	// GitignoreState is "added", "present" or "not-asked".
	GitignoreState string `json:"gitignoreState" yaml:"gitignoreState"`
	// GitignoreFile is the file that was (or would be) appended to.
	GitignoreFile string `json:"gitignoreFile,omitempty" yaml:"gitignoreFile,omitempty"`
}

// Gitignore states, reported by InitResult.
const (
	GitignoreAdded    = "added"
	GitignorePresent  = "present"
	GitignoreNotAsked = "not-asked"
)

// Init creates a board root, or completes one that is missing its document.
//
// The rules, in the order they are applied, because the interesting cases are
// all near-misses:
//
//  1. The requested document already exists → refuse. Overwriting is the one
//     mistake here that destroys somebody's work, and there is no flag for it:
//     delete the file yourself if you mean it.
//  2. A root exists ABOVE this directory and no --name was given → refuse and
//     name it. Creating a nested root would give the project two boards, one of
//     which no command run from the root can ever see.
//  3. A root exists (here or above) and --name names a board it does not have →
//     create that board inside the EXISTING root. Two boards in one project is
//     the supported shape; two roots is not.
//  4. A root exists exactly HERE but its document is missing → complete it.
//     `serve` tells the reader to run `aboard init` when the document is gone,
//     and a blanket refusal would make that instruction impossible to follow.
//  5. Otherwise → create `.aboard/` where you stand.
func Init(cfg InitConfig) (InitResult, error) {
	dir, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return InitResult{}, fmt.Errorf("resolve %s: %w", cfg.Dir, err)
	}

	root := Root(dir)
	if found, ferr := FindRoot(dir); ferr == nil {
		if found.String() != dir && cfg.Name == "" {
			return InitResult{}, fmt.Errorf(
				"a board root already exists at %s (%s) — init does not walk up, and a second root here would be invisible from there. "+
					"Run `aboard init` from that directory, or add a second board to it with `aboard init --name <name>`",
				found, found.Dir(),
			)
		}
		root = found
	}

	res := InitResult{
		Root:           root.String(),
		Name:           cfg.Name,
		StateFile:      root.StateFile(cfg.Name),
		Seeded:         cfg.Example,
		GitignoreLine:  GitignoreLine,
		GitignoreState: GitignoreNotAsked,
		Created:        []string{},
	}

	if _, err := os.Stat(res.StateFile); err == nil {
		hint := "delete it first if you mean to start over"
		if cfg.Name == "" {
			hint += ", or open a second board beside it with `aboard init --name <name>`"
		}
		return InitResult{}, fmt.Errorf("a board already exists at %s — %s", res.StateFile, hint)
	}

	for _, d := range []string{root.Dir(), root.RunDir(), root.UploadsDir(), root.RecipesDir()} {
		made, err := mkdirIfMissing(d)
		if err != nil {
			return InitResult{}, err
		}
		if made {
			res.Created = append(res.Created, d)
		}
	}

	readme := filepath.Join(root.RecipesDir(), "README.md")
	if _, err := os.Stat(readme); os.IsNotExist(err) {
		if err := os.WriteFile(readme, []byte(recipesReadme), 0o644); err != nil { //nolint:gosec // see the file-mode note above
			return InitResult{}, fmt.Errorf("writing %s: %w", readme, err)
		}
		res.Created = append(res.Created, readme)
	}

	doc, tabs, err := initialDocument(cfg.Example)
	if err != nil {
		return InitResult{}, err
	}
	res.Tabs = tabs
	if err := os.WriteFile(res.StateFile, doc, 0o644); err != nil { //nolint:gosec // see the file-mode note above
		return InitResult{}, fmt.Errorf("writing %s: %w", res.StateFile, err)
	}
	res.Created = append(res.Created, res.StateFile)

	if cfg.Gitignore {
		res.GitignoreFile = root.GitignoreFile()
		state, err := ensureGitignore(res.GitignoreFile)
		if err != nil {
			return InitResult{}, err
		}
		res.GitignoreState = state
	}
	return res, nil
}

// initialDocument is the document init writes: the minimal one the shell
// accepts, or the embedded example.
//
// The minimal shape is not invented here — aboard.html refuses to render a
// document whose `version` it does not know (it draws a "reload" notice
// instead), reads `tabs` as an array and `nextId` as the board-wide id counter.
// Those three are load-bearing; `rev`, `updatedAt` and `lastEditedBy` are
// stamped so the first write has a compare-and-set base and the journal has an
// author. `rev` is the base — a fresh board starts at 0 and the first accepted
// write makes it 1 — and it is written here so that no board ever has to take
// the timestamp-base compatibility path in commitState.
func initialDocument(example bool) (document []byte, tabs int, err error) {
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	if !example {
		doc := map[string]any{
			"version":      SchemaVersion,
			"nextId":       1,
			"tabs":         []any{},
			"rev":          0,
			"updatedAt":    stamp,
			"lastEditedBy": initActor,
		}
		body, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, 0, err
		}
		return append(body, '\n'), 0, nil
	}

	raw, err := exampleFS.ReadFile(exampleFile)
	if err != nil {
		return nil, 0, fmt.Errorf("reading the embedded example board: %w", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, 0, fmt.Errorf("the embedded example board is not valid json: %w", err)
	}

	seeded, _ := doc["tabs"].([]any)
	doc["version"] = SchemaVersion
	doc["rev"] = 0
	doc["updatedAt"] = stamp
	doc["lastEditedBy"] = initActor
	// Recomputed rather than copied. The counter is the ONLY correct id
	// allocator, and a fixture whose counter had fallen behind its own contents
	// would hand out an id that already names something — silently re-pointing
	// every reference to the older object.
	doc["nextId"] = nextIDFor(doc)

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, 0, err
	}
	return append(body, '\n'), len(seeded), nil
}

// nextIDFor walks the whole document for `bb<n>` ids and returns one past the
// highest, never below the counter the document already carried.
func nextIDFor(doc map[string]any) int {
	highest := 0
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if id, ok := t["id"].(string); ok {
				if n, err := strconv.Atoi(strings.TrimLeft(id, "abn")); err == nil && n > highest {
					highest = n
				}
			}
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(doc)

	next := highest + 1
	if declared, ok := doc["nextId"].(float64); ok && int(declared) > next {
		next = int(declared)
	}
	return next
}

func mkdirIfMissing(dir string) (bool, error) {
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("%s exists and is not a directory", dir)
		}
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("creating %s: %w", dir, err)
	}
	return true, nil
}

// ensureGitignore appends GitignoreLine unless the file already ignores the
// board directory.
//
// Idempotent by reading the file rather than by remembering: a project may have
// added the line by hand, spelled `.aboard` or `/.aboard/`, and appending a
// second one would be noise in somebody else's file.
func ensureGitignore(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	for line := range strings.SplitSeq(string(body), "\n") {
		if ignoresBoard(strings.TrimSpace(line)) {
			return GitignorePresent, nil
		}
	}

	out := string(body)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += GitignoreLine + "\n"
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil { //nolint:gosec // see the file-mode note above
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return GitignoreAdded, nil
}

// ignoresBoard recognises the spellings a person actually writes for the same
// rule. Not a gitignore parser: it answers "would a second line be redundant",
// and the cost of a false negative is one duplicate line, not a broken repo.
func ignoresBoard(line string) bool {
	return strings.TrimSuffix(strings.TrimPrefix(line, "/"), "/") == DirName
}

// recipesReadme is dropped into an empty `.aboard/recipes/` so the directory
// says what it is for. A recipe is a document a person writes; an empty
// directory with no explanation is one nobody ever puts a file in.
const recipesReadme = `# Project recipes

Markdown files here are recipes this project's agents can read with
` + "`aboard recipes show <name>`" + `. One recipe per file; the file stem IS the name.

They are the lowest-precedence tier: a recipe of the same name in
` + "`_apex/aboard/recipes/`" + ` or ` + "`_aboard/recipes/`" + ` wins, and a recipe here wins
over a built-in one of the same name. ` + "`aboard recipes list`" + ` shows which won and
names what it shadowed.

Each file opens with YAML frontmatter, then a markdown body:

    ---
    name: my-recipe            # required, must equal the file stem
    description: "One line."   # required
    when_to_use: "When ..."    # required
    tags: [optional, list]
    requires: { min_schema: 3 }
    ---

    # The body is markdown, and it is what an agent reads.

A recipe may also carry ONE fenced block tagged ` + "`aboard-template`" + ` holding a JSON
tab skeleton. ` + "`aboard recipes show <name> --template`" + ` prints just that block, so it
pipes straight into an edit and then into ` + "`aboard apply`" + `.
`
