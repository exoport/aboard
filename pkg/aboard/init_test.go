package aboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("%s is not valid json: %v", path, err)
	}
	return doc
}

// A fresh directory gets everything a board needs, and the document it writes is
// one the shell will actually render. The three fields that decide that are
// asserted by name: aboard.html refuses a document whose `version` it does not
// know, reads `tabs` as an array, and allocates ids from `nextId`.
func TestInitCreatesAWorkingBoard(t *testing.T) {
	dir := t.TempDir()
	res, err := Init(InitConfig{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}

	root := Root(mustAbs(t, dir))
	for _, p := range []string{root.Dir(), root.RunDir(), root.UploadsDir(), root.RecipesDir()} {
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			t.Errorf("%s was not created as a directory (%v)", p, err)
		}
	}
	if res.StateFile != root.StateFile("") {
		t.Errorf("state file = %q, want %q", res.StateFile, root.StateFile(""))
	}

	doc := readDoc(t, res.StateFile)
	if v, _ := doc["version"].(float64); int(v) != SchemaVersion {
		t.Errorf("version = %v, want %d — the shell blanks a document whose version it does not know", doc["version"], SchemaVersion)
	}
	if n, _ := doc["nextId"].(float64); int(n) != 1 {
		t.Errorf("nextId = %v, want 1", doc["nextId"])
	}
	if tabs, ok := doc["tabs"].([]any); !ok || len(tabs) != 0 {
		t.Errorf("tabs = %v, want an empty array", doc["tabs"])
	}
	if doc["updatedAt"] == "" || doc["updatedAt"] == nil {
		t.Error("no updatedAt — the first write would have no compare-and-set base")
	}
	if doc["lastEditedBy"] != initActor {
		t.Errorf("lastEditedBy = %v, want %q", doc["lastEditedBy"], initActor)
	}

	// The recipes directory says what it is for. An empty directory with no
	// explanation is one nobody ever puts a file in.
	if _, err := os.Stat(filepath.Join(root.RecipesDir(), "README.md")); err != nil {
		t.Errorf("no README in the recipes directory: %v", err)
	}

	// And the run directory exists BEFORE serve: the journal and the instance
	// file are written into it, and both failures are silent.
	if res.Tabs != 0 || res.Seeded {
		t.Errorf("an empty init reported %d tabs, seeded=%v", res.Tabs, res.Seeded)
	}
}

// Never overwrite. This is the one mistake here that destroys work somebody
// cannot reconstruct, and there is deliberately no --force.
func TestInitRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(InitConfig{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(Root(mustAbs(t, dir)).StateFile(""))
	if err != nil {
		t.Fatal(err)
	}

	_, err = Init(InitConfig{Dir: dir})
	if err == nil {
		t.Fatal("a second init overwrote the board")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("the refusal does not say why: %v", err)
	}

	after, err := os.ReadFile(Root(mustAbs(t, dir)).StateFile(""))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("the refused init changed the document anyway")
	}
}

// init does not walk up. A second root inside a project would be invisible from
// the project root, so it refuses and NAMES the root it found — "it already
// exists" without a path sends the reader off to run `find`.
func TestInitRefusesInsideAnExistingRoot(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(InitConfig{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(dir, "pkg", "thing")
	mustMkdir(t, deep)

	_, err := Init(InitConfig{Dir: deep})
	if err == nil {
		t.Fatal("init created a nested root")
	}
	if !strings.Contains(err.Error(), mustAbs(t, dir)) {
		t.Errorf("the refusal does not name the root it found: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(deep, DirName)); statErr == nil {
		t.Error("a nested .aboard/ was created despite the refusal")
	}
}

// A named board is the supported way to have two boards in one project: same
// root, its own document, its own port. So --name inside an existing root is the
// one case that proceeds.
func TestInitNamedBoardJoinsTheExistingRoot(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(InitConfig{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(dir, "sub")
	mustMkdir(t, deep)

	res, err := Init(InitConfig{Dir: deep, Name: "review"})
	if err != nil {
		t.Fatalf("a named board inside an existing root was refused: %v", err)
	}
	root := Root(mustAbs(t, dir))
	if res.Root != root.String() {
		t.Errorf("the named board landed in %q, want the existing root %q", res.Root, root)
	}
	if res.StateFile != root.StateFile("review") {
		t.Errorf("state file = %q, want %q", res.StateFile, root.StateFile("review"))
	}
	if _, err := os.Stat(res.StateFile); err != nil {
		t.Errorf("the named document was not written: %v", err)
	}
	// Two boards, two ports. A shared one would mean the second serve refusing
	// to start against the first.
	if DerivePort(root, "") == DerivePort(root, "review") {
		t.Error("the named board derives the same port as the default one")
	}
}

// `serve` tells the reader to run `aboard init` when the document is missing.
// A blanket "a root already exists" refusal would make that instruction
// impossible to follow, so a root whose document is gone is COMPLETED.
func TestInitCompletesARootMissingItsDocument(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, DirName))

	res, err := Init(InitConfig{Dir: dir})
	if err != nil {
		t.Fatalf("init refused to complete a root with no document: %v", err)
	}
	if _, err := os.Stat(res.StateFile); err != nil {
		t.Errorf("no document written: %v", err)
	}
}

// --gitignore is idempotent by READING the file, not by remembering: a project
// may have added the line by hand, and a second one would be noise in somebody
// else's file.
func TestInitGitignoreIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	res, err := Init(InitConfig{Dir: dir, Gitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.GitignoreState != GitignoreAdded {
		t.Errorf("first run reported %q, want %q", res.GitignoreState, GitignoreAdded)
	}

	body, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), GitignoreLine) {
		t.Fatalf(".gitignore does not carry the line:\n%s", body)
	}

	// A second board in the same project asks again; the line must not double.
	res2, err := Init(InitConfig{Dir: dir, Name: "second", Gitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	if res2.GitignoreState != GitignorePresent {
		t.Errorf("second run reported %q, want %q", res2.GitignoreState, GitignorePresent)
	}
	body2, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body2), GitignoreLine) != 1 {
		t.Errorf("the line appears %d times:\n%s", strings.Count(string(body2), GitignoreLine), body2)
	}
}

// The spellings a person actually writes for the same rule. A false negative
// costs one duplicate line; a false positive would leave a board tracked.
func TestGitignoreRecognisesExistingSpellings(t *testing.T) {
	for _, line := range []string{".aboard/", ".aboard", "/.aboard/", "/.aboard"} {
		dir := t.TempDir()
		path := filepath.Join(dir, ".gitignore")
		if err := os.WriteFile(path, []byte("node_modules/\n"+line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		state, err := ensureGitignore(path)
		if err != nil {
			t.Fatal(err)
		}
		if state != GitignorePresent {
			t.Errorf("%q was not recognised as already ignoring the board", line)
		}
	}
}

// --example seeds the board compiled into the binary. Every property asserted
// here is one a broken fixture would carry into every project that ran it.
func TestInitExampleSeedsTheBoard(t *testing.T) {
	dir := t.TempDir()
	res, err := Init(InitConfig{Dir: dir, Example: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Seeded {
		t.Error("the result does not report that it seeded")
	}
	if res.Tabs != 15 {
		t.Errorf("seeded %d tabs, want 15 — one per renderer", res.Tabs)
	}

	doc := readDoc(t, res.StateFile)
	if v, _ := doc["version"].(float64); int(v) != SchemaVersion {
		t.Errorf("seeded version = %v, want %d", doc["version"], SchemaVersion)
	}
	if doc["lastEditedBy"] != initActor {
		t.Errorf("lastEditedBy = %v, want %q", doc["lastEditedBy"], initActor)
	}
	stamp, _ := doc["updatedAt"].(string)
	if !strings.HasSuffix(stamp, "Z") || stamp == "2026-08-25T00:00:00.000Z" {
		t.Errorf("updatedAt = %q — the fixture's own stamp was carried through instead of being rewritten", stamp)
	}

	tabs, _ := doc["tabs"].([]any)
	seen := map[string]bool{}
	highest := 0
	for _, raw := range tabs {
		tab, _ := raw.(map[string]any)
		id, _ := tab["id"].(string)
		if id == "" {
			t.Errorf("a tab has no id: %v", tab["name"])
			continue
		}
		if seen[id] {
			t.Errorf("tab id %s appears twice — an id is a handle written in sentences", id)
		}
		seen[id] = true
		if n := idNumber(id); n > highest {
			highest = n
		}
		for _, field := range []string{"name", "type"} {
			if v, _ := tab[field].(string); v == "" {
				t.Errorf("tab %s has no %s", id, field)
			}
		}
	}

	next, _ := doc["nextId"].(float64)
	if int(next) <= highest {
		t.Errorf("nextId = %d but the highest id in the document is %d — the allocator would hand out an id that already names something",
			int(next), highest)
	}

	// The Decisions tab ships ONE decided row with a reason, so the example
	// demonstrates a recorded verdict — and so `aboard export` on it carries a
	// "Why:" line, which the browser suite asserts.
	var gate map[string]any
	for _, raw := range tabs {
		if tab, _ := raw.(map[string]any); tab != nil && tab["type"] == "gate" {
			gate = tab
		}
	}
	if gate == nil {
		t.Fatal("the example board has no gate tab")
	}
	state, _ := gate["state"].(map[string]any)
	decided, _ := state["decided"].([]any)
	if len(decided) != 1 {
		t.Fatalf("the gate ships %d decided rows, want exactly one worked example", len(decided))
	}
	row, _ := decided[0].(map[string]any)
	if reason, _ := row["reason"].(string); strings.TrimSpace(reason) == "" {
		t.Error("the decided row has no reason — the reason is what the agent learns from")
	}
	if verdict, _ := row["verdict"].(string); verdict != "allow" && verdict != "deny" && verdict != "edit" {
		t.Errorf("the decided row's verdict is %q", row["verdict"])
	}
}

// The example board must APPLY, not merely parse: it is the first board most
// people see, and a write-time warning on it is a warning nobody can act on.
//
// Exactly one warning is expected, and it is a TRUE POSITIVE: the UI gallery
// carries a `sparkline` component on purpose, to demonstrate the
// unknown-component marker. Asserting zero would be asserting the wrong thing —
// it would mean the demonstration had been deleted or the checker had stopped
// descending into ui trees.
func TestExampleBoardWarnsOnlyAboutItsDemonstration(t *testing.T) {
	raw, err := exampleFS.ReadFile(exampleFile)
	if err != nil {
		t.Fatal(err)
	}
	warnings := writeWarnings(WebFS(), raw)
	if len(warnings) != 1 {
		t.Fatalf("the example board produced %d warnings, want exactly the sparkline demonstration:\n%s",
			len(warnings), strings.Join(warnings, "\n"))
	}
	if !strings.Contains(warnings[0], "sparkline") {
		t.Errorf("the one warning is not the demonstration: %s", warnings[0])
	}
	if w := wrongVersion(raw); w != "" {
		t.Errorf("the example board declares a version this board does not write: %s", w)
	}
}

// idNumber pulls the counter out of a bb id. Bare and legacy `n49` ids are still
// read everywhere, so the same tolerant rule is used here.
func idNumber(id string) int {
	n := 0
	digits := strings.TrimLeft(id, "abn")
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
