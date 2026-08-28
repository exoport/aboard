package aboard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindRootAtStart(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, DirName))

	got, err := FindRoot(dir)
	if err != nil {
		t.Fatalf("FindRoot(%s): %v", dir, err)
	}
	if got.String() != mustAbs(t, dir) {
		t.Fatalf("root = %q, want %q", got, dir)
	}
}

func TestFindRootTwoLevelsUp(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, DirName))
	deep := filepath.Join(dir, "pkg", "aboard")
	mustMkdir(t, deep)

	got, err := FindRoot(deep)
	if err != nil {
		t.Fatalf("FindRoot(%s): %v", deep, err)
	}
	// The whole point: a command run from a subdirectory resolves the SAME root,
	// so it finds the same board and derives the same port.
	if got.String() != mustAbs(t, dir) {
		t.Fatalf("root = %q, want %q", got, dir)
	}
}

func TestFindRootNotFound(t *testing.T) {
	dir := t.TempDir() // no .aboard anywhere in it

	_, err := FindRoot(dir)
	if !errors.Is(err, ErrNoRoot) {
		t.Fatalf("err = %v, want ErrNoRoot", err)
	}
	// The message has to name where it looked, or the reader has no next step.
	if !strings.Contains(err.Error(), mustAbs(t, dir)) {
		t.Fatalf("error %q does not name the start directory %q", err, dir)
	}
}

func TestFindRootStopsAtAFileNamedLikeTheDir(t *testing.T) {
	// A FILE called .aboard must not be mistaken for the directory: the walk
	// would stop at a project that has no board in it and every path derived
	// from that root would be wrong.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, DirName), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FindRoot(dir); !errors.Is(err, ErrNoRoot) {
		t.Fatalf("err = %v, want ErrNoRoot", err)
	}
}

func TestRootPaths(t *testing.T) {
	root := Root(filepath.FromSlash("/p"))
	j := func(parts ...string) string { return filepath.Join(append([]string{"/p"}, parts...)...) }

	// LogFile answers a question as well as building a path, so it does not fit
	// the table's shape: an id that could escape the logs directory gets no path
	// at all. TestLogFileRefusesAnIDThatCannotBeAFilename covers the refusal.
	logFile, okLogFile := root.LogFile("", "ab42")
	if !okLogFile {
		t.Fatal(`LogFile("ab42") refused a plain tab id`)
	}
	namedLogFile, okNamedLog := root.LogFile("review", "ab42")
	if !okNamedLog {
		t.Fatal(`LogFile("review", "ab42") refused a plain tab id`)
	}

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"Dir", root.Dir(), j(".aboard")},
		{"RunDir", root.RunDir(), j(".aboard", "run")},
		{"StateFile", root.StateFile(""), j(".aboard", "aboard.json")},
		{"StateFile named", root.StateFile("review"), j(".aboard", "aboard.review.json")},
		{"UploadsDir", root.UploadsDir(), j(".aboard", "uploads")},
		{"UploadFile", root.UploadFile("a.png"), j(".aboard", "uploads", "a.png")},
		{"InstanceFile", root.InstanceFile(""), j(".aboard", "run", "instance.json")},
		{"InstanceFile named", root.InstanceFile("review"), j(".aboard", "run", "instance.review.json")},
		{"JournalFile", root.JournalFile(""), j(".aboard", "run", "journal.jsonl")},
		{"JournalFile named", root.JournalFile("review"), j(".aboard", "run", "journal.review.jsonl")},
		{"RenderedFile", root.RenderedFile(""), j(".aboard", "run", "rendered.json")},
		{"RenderedFile named", root.RenderedFile("review"), j(".aboard", "run", "rendered.review.json")},
		{"LogsDir", root.LogsDir(""), j(".aboard", "run", "logs")},
		{"LogsDir named", root.LogsDir("review"), j(".aboard", "run", "logs", "review")},
		{"LogFile", logFile, j(".aboard", "run", "logs", "ab42.log")},
		{"LogFile named", namedLogFile, j(".aboard", "run", "logs", "review", "ab42.log")},
		{"ShotsDir", root.ShotsDir(), j(".aboard", "run", "shots")},
		{"E2EDir", root.E2EDir(), j(".aboard", "run", "e2e")},
		{"E2ECase", root.E2ECase("TestBridge"), j(".aboard", "run", "e2e", "TestBridge")},
		{"DevDir", root.DevDir(), j("pkg", "aboard", "web")},
		{"SkillReference", root.SkillReference(), j(".claude", "skills", "aboard", "references", "reference.generated.md")},
		{"GeneratedControls", root.GeneratedControls(), j("pkg", "aboard", "web", "views", "controls.generated.js")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestRootResolve(t *testing.T) {
	root := Root(filepath.FromSlash("/p"))
	if got, want := root.Resolve("x.json"), filepath.Join(string(filepath.Separator)+"p", "x.json"); got != want {
		t.Errorf("Resolve(relative) = %q, want %q — a relative path means relative to the ROOT, not the cwd", got, want)
	}
	abs := filepath.Join(string(filepath.Separator)+"elsewhere", "x.json")
	if got := root.Resolve(abs); got != abs {
		t.Errorf("Resolve(absolute) = %q, want it untouched", got)
	}
	if got := root.Resolve(""); got != "" {
		t.Errorf("Resolve(\"\") = %q, want \"\" so the caller can fall back", got)
	}
}

// Pinned vectors. The port has to be the SAME number tomorrow, or every URL a
// human bookmarked moves; a test that only checked the range would not notice.
func TestDerivePortIsPinned(t *testing.T) {
	root := Root("/tmp/example-project")
	if got, want := DerivePort(root, ""), 45856; got != want {
		t.Errorf("DerivePort(%q, \"\") = %d, want %d", root, got, want)
	}
	if got, want := DerivePort(root, "review"), 42496; got != want {
		t.Errorf("DerivePort(%q, \"review\") = %d, want %d", root, got, want)
	}
}

func TestDerivePortInRange(t *testing.T) {
	for _, name := range []string{"", "a", "review", "second"} {
		p := DerivePort(Root("/some/where"), name)
		if p < portBase || p >= portBase+portSpan {
			t.Errorf("DerivePort(name=%q) = %d, outside [%d,%d)", name, p, portBase, portBase+portSpan)
		}
	}
}

// The bug this pins: the spike hashed os.Getwd(), so the derived port changed
// with the directory you happened to run from and `aboard status` reported a
// different port than the board it was describing.
func TestDerivePortDependsOnRootNotCwd(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, DirName))
	deep := filepath.Join(dir, "a", "b", "c")
	mustMkdir(t, deep)

	fromTop, err := FindRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	fromDeep, err := FindRoot(deep)
	if err != nil {
		t.Fatal(err)
	}
	if DerivePort(fromTop, "") != DerivePort(fromDeep, "") {
		t.Fatalf("port from %s (%d) != port from %s (%d)",
			dir, DerivePort(fromTop, ""), deep, DerivePort(fromDeep, ""))
	}
}

func TestNormalizeBasePath(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"/":        "",
		"  ":       "",
		"x":        "/x",
		"/x":       "/x",
		"/x/":      "/x",
		"//x//":    "/x",
		"/a/b":     "/a/b",
		"  /a/b  ": "/a/b",
	}
	for in, want := range cases {
		if got := NormalizeBasePath(in); got != want {
			t.Errorf("NormalizeBasePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustAbs(t *testing.T, dir string) string {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	// t.TempDir can hand back a path through a symlink (/tmp -> /private/tmp on
	// macOS); FindRoot does not resolve symlinks, so neither does this.
	return abs
}

// One project, one root, one port. FindRoot never resolved symlinks, so a
// checkout reached through a link derived a DIFFERENT port from the same
// project: two servers on one state file, each with its own instance record, and
// the second one's exit deleting the record the first was found through. Board
// content survives that; discovery does not.
func TestFindRootResolvesSymlinks(t *testing.T) {
	actual := filepath.Join(t.TempDir(), "project")
	mustMkdir(t, actual)
	mustMkdir(t, filepath.Join(actual, DirName))

	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(actual, link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	viaLink, err := FindRoot(link)
	if err != nil {
		t.Fatalf("FindRoot(%s): %v", link, err)
	}
	direct, err := FindRoot(actual)
	if err != nil {
		t.Fatalf("FindRoot(%s): %v", actual, err)
	}
	// Resolved, so both spellings answer with the same string...
	if viaLink != direct {
		t.Fatalf("the same project resolved to two roots: %q via the link, %q direct", viaLink, direct)
	}
	// ...which is what makes them one board on one port.
	if DerivePort(viaLink, "") != DerivePort(direct, "") {
		t.Errorf("two ports for one project: %d and %d", DerivePort(viaLink, ""), DerivePort(direct, ""))
	}
	// And a subdirectory reached through the link resolves there too.
	sub := filepath.Join(actual, "pkg")
	mustMkdir(t, sub)
	viaSub, err := FindRoot(filepath.Join(link, "pkg"))
	if err != nil {
		t.Fatalf("FindRoot(%s): %v", filepath.Join(link, "pkg"), err)
	}
	if viaSub != direct {
		t.Errorf("a subdirectory under the link resolved to %q, want %q", viaSub, direct)
	}
}

// A tab id becomes a filename, so LogFile validates it rather than trusting a
// comment that the caller already did. Before the validation moved into
// layout.go this test could not be written at all: LogFile returned one value
// and joined whatever it was given, so `../../evil` produced a path outside the
// project and the only thing standing between that and a write was a check in a
// different file that gosec — correctly — did not believe in.
func TestLogFileRefusesAnIDThatCannotBeAFilename(t *testing.T) {
	root := Root("/tmp/project")
	for _, bad := range []string{
		"",
		"..",
		"../../etc/passwd",
		"a/b",
		`a\b`,
		"ab42.log",
		"ab 42",
		strings.Repeat("b", 65),
	} {
		if path, ok := root.LogFile("", bad); ok {
			t.Errorf("LogFile(%q) = %q, true — wanted a refusal", bad, path)
		}
	}
	for _, good := range []string{"ab42", "ab126", "a", "A_b-1", strings.Repeat("b", 64)} {
		path, ok := root.LogFile("", good)
		if !ok {
			t.Errorf("LogFile(%q) refused a plain tab id", good)
			continue
		}
		if got := filepath.Dir(path); got != root.LogsDir("") {
			t.Errorf("LogFile(%q) landed in %q, not the logs directory %q", good, got, root.LogsDir(""))
		}
	}
}
