package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard"
	"gopkg.in/yaml.v3"
)

// exitOf runs the tree and reports the status a process would exit with.
func exitOf(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	out, errOut, err := run(t, args...)
	code, _ = ExitCode(err)
	return code, out, errOut
}

// The declared table promises 2 for "a flag or argument the command cannot act
// on, detected before anything was contacted". Cobra's Args validators return
// untyped errors, so `aboard export` with no argument exited 1 —
// indistinguishable from `aboard export nosuchtab`, which is a board that ran and
// could not find the tab. The manifest advertises 2 for exactly this.
func TestArgumentCountErrorsExitUsage(t *testing.T) {
	dir := t.TempDir()
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir}); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"export"},                      // ExactArgs(1), none given
		{"export", "a", "b"},            // ExactArgs(1), two given
		{"log"},                         // ExactArgs(1)
		{"status", "extra"},             // NoArgs
		{"init", "extra"},               // NoArgs
		{"apply", "extra"},              // NoArgs
		{"journal", "extra"},            // NoArgs
		{"recipes", "list", "extra"},    // NoArgs, on a SUBcommand
		{"recipes", "show"},             // ExactArgs(1), on a subcommand
		{"capabilities", "dag", "more"}, // MaximumNArgs(1)
	} {
		full := append([]string{"--cwd", dir}, args...)
		code, _, _ := exitOf(t, full...)
		if code != aboard.ExitUsage {
			t.Errorf("`aboard %s` exited %d, want %d", strings.Join(args, " "), code, aboard.ExitUsage)
		}
	}
}

// The library refusal is typed (aboard.ErrNoBase) so this layer can map it to the
// status the declared table promises. Nothing asserted the MAPPING, which is the
// half a reader of `aboard apply` actually experiences: delete the errors.Is
// branch and the library test still passes while the command exits 1.
//
// It needs no board: the base is checked before the instance record is read, so
// this is the shape a session hits when it assembles a document from the schema
// instead of from the one it read.
func TestApplyWithNoBaseExitsUsage(t *testing.T) {
	dir := t.TempDir()
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	doc := `{"version":3,"nextId":2,"tabs":[{"id":"bb1","name":"Plan","type":"notes"}]}`

	var out, errOut bytes.Buffer
	root := NewRootCmd(Options{
		Host: aboard.HostStandalone, Stdout: &out, Stderr: &errOut,
		Stdin: strings.NewReader(doc),
	})
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"--cwd", dir, "apply", "--by", "agent-1"})
	code, _ := ExitCode(root.Execute())
	if code != aboard.ExitUsage {
		t.Errorf("apply with a base-less document exited %d, want %d (%s)",
			code, aboard.ExitUsage, errOut.String()+out.String())
	}
}

// A board name becomes a FILENAME. `--name '../../../../evil'` wrote a state file
// and an instance record outside the project and reported success — so the
// refusal has to come before any path is joined, which makes it a usage error.
func TestAnEscapingBoardNameIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	dir := t.TempDir()
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(dir, "..", "escaped")

	code, _, errOut := exitOf(t, "--cwd", dir, "status", "--name", "../escaped")
	if code != aboard.ExitUsage {
		t.Errorf("an escaping --name exited %d, want %d (%s)", code, aboard.ExitUsage, errOut)
	}
	if entries, err := os.ReadDir(filepath.Dir(escape)); err == nil {
		for _, e := range entries {
			if strings.Contains(e.Name(), "escaped") {
				t.Errorf("something named %q was created outside the project", e.Name())
			}
		}
	}

	// And the environment fallback is the same door, so it gets the same lock.
	t.Setenv("ABOARD_NAME", "../escaped")
	if code, _, _ := exitOf(t, "--cwd", dir, "status"); code != aboard.ExitUsage {
		t.Errorf("ABOARD_NAME='../escaped' exited %d, want %d", code, aboard.ExitUsage)
	}
}

// `init` CREATES things, and --output-format was validated inside renderOutput —
// after the board, the directories and the .gitignore line were already written.
// The declared table calls 2 "detected before anything was contacted"; the
// corrected retry then exited 1 with "already exists", so the user's first two
// attempts both failed and the board they were left with came from the failed one.
func TestInitValidatesOutputFormatBeforeWritingAnything(t *testing.T) {
	dir := t.TempDir()

	code, _, _ := exitOf(t, "--cwd", dir, "init", "--output-format", "toml")
	if code != aboard.ExitUsage {
		t.Errorf("init with a bad --output-format exited %d, want %d", code, aboard.ExitUsage)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("the refused init left %v behind (err %v)", entries, err)
	}

	// And the retry is a first run, not a repair.
	if code, _, errOut := exitOf(t, "--cwd", dir, "init"); code != aboard.ExitOK {
		t.Fatalf("the corrected retry exited %d: %s", code, errOut)
	}
}

// The resume protocol is status, capabilities, journal. The journal's disk
// fallback fired only when the instance FILE was unreadable, so the commonest
// real case — a board that crashed, leaving its record behind — dialled a dead
// port and exited 1 while journal.jsonl sat readable beside it.
func TestJournalFallsBackWhenTheRecordedBoardIsDead(t *testing.T) {
	dir := t.TempDir()
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	root, err := aboard.FindRoot(dir)
	if err != nil {
		t.Fatal(err)
	}

	// A port that answered once and does not any more.
	dead := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	url := dead.URL
	dead.Close()

	rec, err := json.Marshal(aboard.Instance{App: aboard.HostStandalone, URL: url, Project: root.String(), PID: 999999})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.InstanceFile(""), rec, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.JournalFile(),
		[]byte(`{"at":"T1","by":"agent-1","tabs":["bb1"]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := exitOf(t, "--cwd", dir, "journal", "--limit", "5")
	if code != aboard.ExitOK {
		t.Fatalf("journal against a stale instance record exited %d: %s", code, errOut)
	}
	if !strings.Contains(out, "agent-1") {
		t.Errorf("the entry on disk was not printed:\n%s", out)
	}
	if !strings.Contains(errOut, "from disk") || !strings.Contains(errOut, root.JournalFile()) {
		t.Errorf("stderr does not say it read the file:\n%s", errOut)
	}
	if !strings.Contains(errOut, "not answering") {
		t.Errorf("stderr does not distinguish a dead board from no board at all:\n%s", errOut)
	}
}

// --output-format yaml has to be the same document as json, and it was not: no
// output struct carried yaml tags, so every key lowercased (capsHash→capshash)
// and omitempty was ignored. `recipes list` was worse — it dropped scope, path,
// shadowedBy and the parse error, which are the three things the command exists
// to report.
//
// One test over every struct that reaches renderOutput, comparing the KEY SETS
// rather than the values: a tag added to one field and forgotten on the next is
// exactly the shape of this defect.
func TestEveryOutputStructAgreesInJSONAndYAML(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"status", aboard.StatusReport{
			Project: "/p", Name: "review", Running: true, Recorded: true, App: "aboard",
			URL: "http://localhost:1", Port: 1, PID: 2, State: "/p/.aboard/aboard.json",
			Started: "T", Version: "v", InstanceFile: "/p/.aboard/run/instance.json",
			WouldUsePort: 3, CapsHash: "abc", Skill: "current", SkillCapsHash: "abc",
		}},
		{"init", aboard.InitResult{
			Root: "/p", Name: "review", StateFile: "/p/.aboard/aboard.json",
			Created: []string{"/p/.aboard"}, Tabs: 15, Seeded: true,
			GitignoreLine: ".aboard/", GitignoreState: "added", GitignoreFile: "/p/.gitignore",
		}},
		{"journal entry", aboard.JournalEntry{
			At: "T", By: "agent-1", Origin: "apply", Tabs: []string{"bb1"},
			Names: map[string]string{"bb1": "Plan"}, NextID: 4,
		}},
		{"version", versionReport{
			App: "aboard", Host: "aboard", Version: "v", BuildDate: "T", GitCommit: "abc", Schema: 3,
		}},
		{"recipe", aboard.RecipeOut{
			Name: "r", Description: "d", WhenToUse: "w", Tags: []string{"t"},
			Requires: aboard.RecipeRequiresOut{MinSchema: 3},
			Scope:    "builtin", Path: "recipes/builtin/r.md", HasTemplate: true,
			ShadowedBy: []string{"/p/.aboard/recipes/r.md"}, Err: "boom",
		}},
	} {
		asJSON := roundTrip(t, tc.name, json.Marshal, json.Unmarshal, tc.value)
		asYAML := roundTrip(t, tc.name, yaml.Marshal, yaml.Unmarshal, tc.value)
		if diff := keyDiff(asJSON, asYAML); diff != "" {
			t.Errorf("%s: json and yaml are different documents: %s", tc.name, diff)
		}
	}
}

// The struct test above proves the SHAPE; this proves the command was wired to
// it. `recipes list` marshalled the parse struct, so the four fields it exists to
// report were dropped from yaml and nothing about the struct definitions would
// have told you.
func TestRecipesListYAMLCarriesWhatTheCommandExistsToReport(t *testing.T) {
	dir := t.TempDir()
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir}); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := exitOf(t, "--cwd", dir, "recipes", "list", "--output-format", "yaml")
	if code != aboard.ExitOK {
		t.Fatalf("recipes list exited %d: %s", code, errOut)
	}
	var got []map[string]any
	if err := yaml.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the yaml does not parse: %v\n%s", err, out)
	}
	if len(got) == 0 {
		t.Fatal("no recipes listed; the built-ins are compiled in and should always be there")
	}
	for _, key := range []string{"name", "description", "whenToUse", "scope", "path", "hasTemplate"} {
		if _, ok := got[0][key]; !ok {
			t.Errorf("yaml output has no %q — keys are %v", key, keysOf(got[0]))
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// And the fields that only appear when they are set must vanish from BOTH.
func TestOmitEmptyIsHonouredInYAMLToo(t *testing.T) {
	empty := aboard.StatusReport{Project: "/p", InstanceFile: "/p/.aboard/run/instance.json", Skill: "absent"}
	asJSON := roundTrip(t, "status", json.Marshal, json.Unmarshal, empty)
	asYAML := roundTrip(t, "status", yaml.Marshal, yaml.Unmarshal, empty)
	for _, key := range []string{"name", "url", "port", "pid", "skillCapsHash"} {
		if _, ok := asYAML[key]; ok {
			t.Errorf("yaml carried empty %q", key)
		}
	}
	if diff := keyDiff(asJSON, asYAML); diff != "" {
		t.Errorf("status with nothing set: %s", diff)
	}
}

func roundTrip(t *testing.T, name string,
	marshal func(any) ([]byte, error), unmarshal func([]byte, any) error, value any,
) map[string]any {
	t.Helper()
	body, err := marshal(value)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	got := map[string]any{}
	if err := unmarshal(body, &got); err != nil {
		t.Fatalf("%s: %v\n%s", name, err, body)
	}
	return got
}

func keyDiff(a, b map[string]any) string {
	var missing, extra []string
	for k := range a {
		if _, ok := b[k]; !ok {
			missing = append(missing, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			extra = append(extra, k)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	return "yaml is missing " + strings.Join(missing, ",") + "; yaml has extra " + strings.Join(extra, ",")
}

// The partial-result change made `init` print what it created when a step fails.
// That is right for the case it was built for — `--gitignore` failing over a
// board that WAS written — and wrong for every earlier step, because
// `res.StateFile` is the path Init intends and is filled in before anything is
// written. Printing "created <it>" unconditionally announced a board that does
// not exist, immediately above an error saying it does not, which is the same
// self-contradiction across two lines instead of across two runs.
func TestInitDoesNotClaimABoardItFailedToCreate(t *testing.T) {
	dir := t.TempDir()
	// A FILE where .aboard/uploads/ has to be a directory: the mkdir fails after
	// .aboard/ itself was made, and before the document is written.
	if err := os.MkdirAll(filepath.Join(dir, ".aboard"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".aboard", "uploads"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, _ := exitOf(t, "--cwd", dir, "init")
	if code == aboard.ExitOK {
		t.Fatal("init succeeded with a file where uploads/ has to be a directory")
	}
	if _, err := os.Stat(filepath.Join(dir, ".aboard", "aboard.json")); err == nil {
		t.Fatal("the board was written after all; this test is asserting the wrong case")
	}
	if strings.Contains(out, "created "+filepath.Join(dir, ".aboard", "aboard.json")) {
		t.Errorf("init announced a board that does not exist:\n%s", out)
	}
	if strings.Contains(out, "start it with") {
		t.Errorf("init told the reader to serve a board that was never created:\n%s", out)
	}
}
