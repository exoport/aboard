// layout.go — where everything lives, decided once.
//
// The spike had three different opinions about where a path starts, and none of
// them agreed: the instance file was joined against the process's working
// directory, while the journal and the sidecar logs were joined against the
// directory of the state file. Run the binary from a subdirectory and it wrote
// its instance record there while reading state from the project root — two
// boards, one project, neither able to find the other. Nothing failed loudly,
// which is why it survived.
//
// So there is now exactly ONE resolved root and this is the only file that joins
// a path underneath it. Everything else asks a Root method. The rule is
// mechanical and therefore checkable: no filepath.Join outside this file.
//
// The split under the root is between CONTENT and MACHINE-LOCAL RUNTIME:
//
//	.aboard/aboard.json         the board itself — the thing a human curates
//	.aboard/uploads/            images they pasted; content too
//	.aboard/run/instance.json   port, pid, url — true only for this machine, now
//	.aboard/run/journal.jsonl   the write log
//	.aboard/run/logs/<tab>.log  sidecar command output
//	.aboard/run/shots/          screenshots from test/shot.sh
//
// A project ignores `.aboard/` wholesale and loses nothing it wanted to keep.

package aboard

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// DirName is the marker directory an aboard project is recognised by, and the
// container for everything the board owns.
const DirName = ".aboard"

// runDirName separates machine-local runtime files from content. Nested inside
// DirName rather than beside it so a project ignores one path, not two.
const runDirName = "run"

// recipeDirName is the leaf of all three on-disk recipe directories, so the three
// tiers differ only in where they hang and never in what they are called.
const recipeDirName = "recipes"

// ErrNoRoot is returned by FindRoot when no ancestor of the starting directory
// contains a DirName directory.
var ErrNoRoot = errors.New("no " + DirName + " directory found")

// Root is a project root: the directory that contains `.aboard/`. It is always
// absolute — FindRoot resolves it — so anything derived from it is stable
// regardless of where the process was started.
type Root string

// FindRoot walks up from start looking for a directory that contains `.aboard/`,
// stopping at the filesystem's fixed point (filepath.Dir is its own fixed point
// at a volume root, on every platform, which is this loop's only exit).
//
// Mirrors apexcfg.Find deliberately: a developer with both tools in the same
// tree should not have to hold two different discovery rules in their head.
func FindRoot(start string) (Root, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", start, err)
	}
	for dir := abs; ; {
		if info, statErr := os.Stat(filepath.Join(dir, DirName)); statErr == nil && info.IsDir() {
			// Resolved, so ONE project is ONE root. The port is derived from this
			// string, so a symlinked path and its target hashed to two different
			// ports: two servers on one state file, each with its own instance
			// record, and the second one's exit deleting the record the first was
			// found through. EvalSymlinks failing is not fatal — the directory was
			// just stat'ed, so a failure here is a race or a permission wall, and
			// the unresolved root is still the right answer.
			if resolved, evalErr := filepath.EvalSymlinks(dir); evalErr == nil {
				return Root(resolved), nil
			}
			return Root(dir), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w at or above %s", ErrNoRoot, abs)
		}
		dir = parent
	}
}

// NewRoot takes a directory as the root without walking. Used where the root is
// already known (a caller that resolved it once) and as the fallback for the
// commands that must answer with no project at all — `capabilities` describes
// the binary, not a board, so it cannot be made to depend on finding one.
func NewRoot(dir string) (Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	return Root(abs), nil
}

// String is the absolute project directory.
func (r Root) String() string { return string(r) }

// Dir is the board's own directory: `<root>/.aboard`.
func (r Root) Dir() string { return filepath.Join(string(r), DirName) }

// RunDir holds everything true only for this machine and this moment.
func (r Root) RunDir() string { return filepath.Join(r.Dir(), runDirName) }

// boardNameRe is what a board name may be. It becomes a FILENAME — the state
// file, the instance record — so it is validated for exactly that, the same way
// logTabRe validates a tab id before it becomes `<tab>.log`.
//
// The rule is a leading alphanumeric and then alphanumerics, dot, dash and
// underscore, up to 64 characters. The leading class is what keeps `.hidden` and
// `-flag` out; the body class is what keeps a separator out. It was unvalidated,
// and `--name '../../../../evil'` wrote a state file and an instance record
// outside the project tree and reported success — uncovered by `.gitignore`,
// invisible to `status`, and impossible to find later.
var boardNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidateBoardName refuses a name that cannot safely become a filename. The
// empty name is the DEFAULT board and is always valid: it means "no suffix".
//
// Called before any path is joined, which is the whole point — a name that has
// already been interpolated into a path has already escaped.
func ValidateBoardName(name string) error {
	if name == "" || boardNameRe.MatchString(name) {
		return nil
	}
	return fmt.Errorf("invalid board name %q: a name becomes a filename, so it must match %s", name, boardNameRe)
}

// StateFile is the board document. A named board gets its own file so two
// boards in one project never share state.
func (r Root) StateFile(name string) string {
	if name == "" {
		return filepath.Join(r.Dir(), "aboard.json")
	}
	return filepath.Join(r.Dir(), "aboard."+name+".json")
}

// UploadsDir holds images the human pasted or dropped. Content, not runtime:
// a markup tab references them by name and would break without them.
func (r Root) UploadsDir() string { return filepath.Join(r.Dir(), uploadDir) }

// UploadFile is one of them, by base name. The caller must have reduced the name
// to a base already; this only joins.
func (r Root) UploadFile(base string) string { return filepath.Join(r.UploadsDir(), base) }

// RecipesDir is this checkout's own recipes: the lowest-precedence directory of
// the three on disk, and the one that ships with `aboard init`. Content, not
// runtime — a recipe is a document somebody wrote.
func (r Root) RecipesDir() string { return filepath.Join(r.Dir(), recipeDirName) }

// ApexRecipesDir and WorkspaceRecipesDir are the two higher-precedence recipe
// directories. They sit BESIDE `.aboard/`, not inside it, because they are meant
// to be committed and shared while `.aboard/` is gitignored wholesale.
//
// Literal strings, deliberately, and not configurable: ape hard-codes
// `_apex/pipelines` exactly the same way. A discovery path that can be
// reconfigured is a discovery path that has to be explained in every error
// message, and "first wins, in this order" stops being a fact anyone can state.
func (r Root) ApexRecipesDir() string {
	return filepath.Join(string(r), "_apex", "aboard", recipeDirName)
}

// WorkspaceRecipesDir is `<root>/_aboard/recipes`.
func (r Root) WorkspaceRecipesDir() string {
	return filepath.Join(string(r), "_aboard", recipeDirName)
}

// GitignoreFile is the project's own ignore file, which `init --gitignore`
// appends `.aboard/` to.
func (r Root) GitignoreFile() string { return filepath.Join(string(r), ".gitignore") }

// InstanceFile records the running board. One record per named board, so a
// `--name review` instance does not overwrite the default board's record and
// leave restart.sh stopping the wrong process.
func (r Root) InstanceFile(name string) string {
	if name == "" {
		return filepath.Join(r.RunDir(), "instance.json")
	}
	return filepath.Join(r.RunDir(), "instance."+name+".json")
}

// InstanceGlob matches EVERY board's instance record in this project — the
// default board's and every named one's. `aboard boards` needs it: it has a pid
// and a root and has to find which of the project's boards that process is
// serving, which InstanceFile cannot answer because it takes the name as input.
//
// A pattern rather than a listing so the join still happens here. The `*` can
// only stand for `.<name>`, because ValidateBoardName is what decides which
// files this directory may ever hold.
func (r Root) InstanceGlob() string { return filepath.Join(r.RunDir(), "instance*.json") }

// JournalFile is the append-only record of accepted writes.
//
// NOT qualified by board name, which is the spike's rule kept deliberately
// rather than reproduced by accident: the journal answers "who changed what in
// this project", and a second named board in the same project is part of the
// same conversation. Rotated generations are this path plus ".1".
func (r Root) JournalFile() string { return filepath.Join(r.RunDir(), "journal.jsonl") }

// LogsDir holds the sidecar log files, one per tab.
func (r Root) LogsDir() string { return filepath.Join(r.RunDir(), "logs") }

// logTabRe is what a tab id may contain when it is about to become a filename.
// It lives here, beside the join it guards, rather than beside the handler that
// used to own it: this file is the only one allowed to build a path, so it is
// the only place where "is this safe to join" is answerable by reading one
// function. Validated rather than sanitised — anything unexpected is refused.
var logTabRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// LogFile is one tab's log, and the second return says whether the id was one a
// path may be built from at all. It used to take the validation on trust from a
// comment ("the caller must have checked logTabRe first"), and a comment is not
// something a reader — or a taint analyser — can check: gosec read the handler,
// saw a query parameter reach filepath.Join, and reported a path traversal on
// all three uses of the result. It was wrong about the outcome and right about
// the shape, because nothing in the joining function established the guarantee.
//
// filepath.Base is belt to the regexp's braces. The pattern already excludes a
// separator and "..", so Base cannot change an accepted string; it is here so
// the construction is safe by INSPECTION rather than by cross-reference.
func (r Root) LogFile(tab string) (string, bool) {
	if !logTabRe.MatchString(tab) {
		return "", false
	}
	return filepath.Join(r.LogsDir(), filepath.Base(tab+".log")), true
}

// validTabFileID reports whether a tab id may become a filename or a key in a
// sidecar record. It sits beside LogFile because two other callers (the mount
// receipts, `aboard log`) need the same answer WITHOUT a path, and both are in
// this package — so it stays unexported. LogFile's second return is the exported
// way to ask, and this engine is a library mounted inside another CLI: every
// exported name is surface a host can come to depend on.
func validTabFileID(tab string) bool { return logTabRe.MatchString(tab) }

// RenderedFile is the mount-receipt sidecar: what the BROWSER reports it drew,
// per tab. Under run/ with the journal and the logs, never in the state
// document — it is per-viewer, machine-local and true only for this moment,
// which is the same rule that keeps selection, zoom and drafts out of the board.
func (r Root) RenderedFile() string { return filepath.Join(r.RunDir(), "rendered.json") }

// ShotsDir is where test/shot.sh writes. Under the run directory because a
// screenshot is a machine-local artefact, and inside the project because a
// snap-confined chromium cannot write outside $HOME.
func (r Root) ShotsDir() string { return filepath.Join(r.RunDir(), "shots") }

// E2EDir holds what the browser suite leaves behind when a test fails: the
// Playwright trace, a screenshot, and the board document as it stood. Beside
// the shots, and for the same reason — a machine-local artefact of a local
// ritual, under a directory the project already gitignores whole.
func (r Root) E2EDir() string { return filepath.Join(r.RunDir(), "e2e") }

// E2ECase is one test's artefacts. The suite writes them TWICE: once under the
// temporary board it drove, which is where the trace's own relative paths make
// sense, and once here under the repo, because the temporary root is deleted
// when the run ends and an artefact the human cannot find is not an artefact.
//
// The caller has already reduced the test name to something a filename can hold;
// this does not sanitise it, exactly like LogFile above.
func (r Root) E2ECase(name string) string { return filepath.Join(r.E2EDir(), name) }

// DevDir is the web tree on disk, served instead of the embedded copy under
// `serve --dev`. Only meaningful inside aboard's own checkout; --dev-dir
// overrides it.
func (r Root) DevDir() string { return filepath.Join(string(r), "pkg", "aboard", "web") }

// SkillReference is the generated half of the committed skill: facts, emitted
// from the manifest, checked for staleness by `capabilities --check`.
func (r Root) SkillReference() string {
	return filepath.Join(string(r), ".claude", "skills", "aboard", "references", "reference.generated.md")
}

// RecipesReadme is the note `aboard init` leaves in the recipes directory saying
// what it is for. Here rather than in init.go because it is a path under
// `.aboard/`, and those are joined in one file.
func (r Root) RecipesReadme() string { return filepath.Join(r.RecipesDir(), "README.md") }

// RecipeFile names one recipe inside a recipe directory. The directory is the
// caller's — three of the four tiers are not under `.aboard/` at all — but the
// join still belongs here, so nothing outside this file has to reason about
// separators.
func RecipeFile(dir, name string) string { return filepath.Join(dir, name) }

// TempFileBeside names a scratch file in the same directory as `path`, for a
// write-then-rename. Same directory because rename is only atomic within one
// filesystem, and named here because it is a path join.
//
// The name carries the pid and a random number: os.CreateTemp would do this too,
// but it also hard-codes mode 0600, which is the wrong mode for the board (see
// writeAtomic).
func TempFileBeside(path string, n uint64) string {
	return filepath.Join(filepath.Dir(path), fmt.Sprintf(".aboard-%d-%d.json", os.Getpid(), n))
}

// procSelfPath and procEntryPath name paths inside a process table. They are
// here for one reason and it is the rule rather than the subject: "no
// filepath.Join outside layout.go" is a rule about the TREE, and a scanner that
// joined its own paths would be the fifth exemption TestNothingOutsideLayoutJoinsAPath
// was written to stop. Nothing about /proc belongs to a project root, which is
// why the proc root is a PARAMETER — the scanner's tests hand it a fake tree
// under t.TempDir() rather than the machine's real one.
func procSelfPath(procRoot string) string { return filepath.Join(procRoot, "self") }

// procEntryPath is one file inside one process's directory: `cmdline`, `cwd`.
func procEntryPath(procRoot, pid, leaf string) string {
	return filepath.Join(procRoot, pid, leaf)
}

// resolveAgainst interprets p relative to base, which is what a `--cwd` read out
// of another process's argv needs: the flag was typed relative to THAT process's
// working directory, not ours, and joining it against ours would resolve a
// perfectly good relative path to the wrong project.
func resolveAgainst(base, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

// DevWebFile names one file inside a --dev web tree. The tree need not be under
// the project root at all — it is whatever --dev-dir points at — so Root has no
// opinion about it, but the join is still a join.
func DevWebFile(dir, name string) string { return filepath.Join(dir, name) }

// LegacyBoardDir is the spike's state directory in a given checkout — `.board/`,
// which aboard never reads. Named here so the cobra tree does not join a path
// either: `no filepath.Join outside layout.go` is a rule about the whole tree,
// and one exemption in another package is how a rule stops being one.
func LegacyBoardDir(start string) string { return filepath.Join(start, legacyDirName) }

// legacyDirName is the spike's directory name. A constant beside DirName so the
// two are read together.
const legacyDirName = ".board"

// SkillRecipes is the skill's generated recipe index — the built-in recipes as a
// markdown table, emitted by `recipes index`. The third generated artifact, and
// the one that had no drift gate: adding or renaming a built-in left this file
// describing a set that no longer existed, with nothing failing anywhere.
func (r Root) SkillRecipes() string {
	return filepath.Join(string(r), ".claude", "skills", "aboard", "references", "recipes.md")
}

// GeneratedControls is the control module the renderers import, emitted from the
// same manifest. A development path: it exists in aboard's own checkout and
// nowhere else, and the staleness check treats its absence as "nothing to check"
// for exactly that reason.
func (r Root) GeneratedControls() string {
	return filepath.Join(string(r), "pkg", "aboard", "web", "views", "controls.generated.js")
}

/* ---------- port derivation ---------- */

// Each project gets its own port, derived from its root, so boards in different
// checkouts never collide and each one's URL stays the same between runs. The
// range sits above the crowded 3000-9000 dev band and below the ephemeral range
// the kernel hands out for outbound connections.
const (
	portBase  = 41000
	portSpan  = 8000
	portTries = 24
)

// DerivePort maps a project root (plus optional board name) to a stable port.
//
// Hashing the DISCOVERED ROOT rather than the working directory is what makes
// the URL the same whichever subdirectory you run the command from — the spike
// hashed os.Getwd(), so `cd views && aboard status` reported a different port
// than the board it was looking at.
func DerivePort(root Root, name string) int {
	sum := sha256.Sum256([]byte(string(root) + "\x00" + name))
	return portBase + int(binary.BigEndian.Uint32(sum[:4])%portSpan)
}

// Resolve turns a caller-supplied path into an absolute one, interpreting a
// relative path against the project root rather than the working directory.
//
// The root is what every other path in this file hangs off, so a `--state
// aboard.json` typed from a subdirectory has to mean the same file it would mean
// from the root. Interpreting it against the process's cwd is exactly the bug
// this file exists to remove.
func (r Root) Resolve(p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(string(r), p)
}
