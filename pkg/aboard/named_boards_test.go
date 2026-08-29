package aboard

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// A named board is a whole board, not a view of one. Everything it writes for
// itself — the journal, the mount receipts, the sidecar logs — used to land in
// the DEFAULT board's files, because only StateFile and InstanceFile were
// name-aware. The consequences were not cosmetic: tab ids are allocated per
// board, so both documents have a `ab1`, and one shared journal held two records
// of "ab1" that meant different tabs.
//
// These tests are all one shape: write through a named board, then look at both
// sets of files.

// boardServer is a server for one board of `root`, which may hold several.
// testServer makes its own TempDir and serves the default board, which is
// exactly what these tests cannot use: the point is two boards in ONE project.
// The document is the same for both on purpose — a `ab1` in each, which is what
// a shared journal or a shared sidecar could not tell apart.
func boardServer(t *testing.T, root Root, name string) *server {
	t.Helper()
	if err := os.MkdirAll(root.RunDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	state := root.StateFile(name)
	if err := os.WriteFile(state, []byte(oneTabBoard), 0o644); err != nil {
		t.Fatal(err)
	}
	return &server{
		opts:      Options{Logger: log.New(io.Discard, "", 0)},
		root:      root,
		name:      name,
		assets:    web.FS,
		stateFile: state,
		clients:   map[chan string]struct{}{},
		watchers:  map[chan string]struct{}{},
		waits:     newWaitHub(),
		ui:        newUIWatcher(false),
		journal:   newJournal(root, name),
		receipts:  newReceiptStore(root, name),
	}
}

const oneTabBoard = `{"version":1,"rev":1,"nextId":2,"tabs":[{"id":"ab1","name":"Plan","type":"notes","state":{"text":"first"}}]}`

// The journal is the record of who changed what, and with both boards appending
// to one file the answer was ambiguous by construction: two entries naming ab1,
// with only the tab's NAME to say which document each belonged to.
func TestANamedBoardsWritesLandInItsOwnJournal(t *testing.T) {
	root := Root(t.TempDir())
	def := boardServer(t, root, "")
	review := boardServer(t, root, "review")

	if rec := review.postDocument(t, `{"version":1,"rev":1,"nextId":2,"tabs":[
		{"id":"ab1","name":"Side note","type":"notes","state":{"text":"second"}}],"__by":"agent-2"}`); rec.Code != http.StatusOK {
		t.Fatalf("writing to the review board: %d %s", rec.Code, rec.Body.String())
	}

	mine, err := journalFromDisk(root, "review", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 || mine[0].By != "agent-2" || mine[0].Names["ab1"] != "Side note" {
		t.Fatalf("the named board's own journal does not hold its write: %+v", mine)
	}
	if _, err := os.Stat(root.JournalFile("")); !os.IsNotExist(err) {
		theirs, _ := journalFromDisk(root, "", 10)
		t.Errorf("the write also reached the default board's journal: %+v", theirs)
	}

	// And the other way round, or the split above would be satisfied by a
	// journal that simply never records anything for the default board.
	if rec := def.postDocument(t, `{"version":1,"rev":1,"nextId":2,"tabs":[
		{"id":"ab1","name":"Plan","type":"notes","state":{"text":"edited"}}],"__by":"agent-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("writing to the default board: %d %s", rec.Code, rec.Body.String())
	}
	theirs, err := journalFromDisk(root, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(theirs) != 1 || theirs[0].By != "agent-1" {
		t.Fatalf("the default board's journal: %+v", theirs)
	}
	if again, err := journalFromDisk(root, "review", 10); err != nil || len(again) != 1 {
		t.Errorf("the default board's write reached the named board's journal: %v %+v", err, again)
	}
}

// `aboard history` reads the journal, so it inherited the same mixing: on a
// named board it could hand back the OTHER board's version of an id both boards
// happen to use, and `--at N` turns that into a document somebody applies.
func TestHistoryOnANamedBoardSeesOnlyItsOwnWrites(t *testing.T) {
	root := Root(t.TempDir())
	def := boardServer(t, root, "")
	review := boardServer(t, root, "review")

	def.postDocument(t, `{"version":1,"rev":1,"nextId":2,"tabs":[
		{"id":"ab1","name":"Plan","type":"notes","state":{"text":"edited"}}],"__by":"agent-1"}`)
	review.postDocument(t, `{"version":1,"rev":1,"nextId":2,"tabs":[
		{"id":"ab1","name":"Side note","type":"notes","state":{"text":"second"}}],"__by":"agent-2"}`)

	got, err := History(t.Context(), root, "review", "ab1", 0, DefaultInvocation)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Versions) != 1 {
		t.Fatalf("the review board's history of ab1 has %d versions, want 1: %+v", len(got.Versions), got.Versions)
	}
	if got.Versions[0].By != "agent-2" {
		t.Errorf("the review board's history names %q — that is the other board's write", got.Versions[0].By)
	}
	if !strings.Contains(string(got.Versions[0].State), "first") {
		t.Errorf("the recorded prior state is not the review board's: %s", got.Versions[0].State)
	}
}

// The listing ends with a command, and a command in output is a claim. Without
// `--name` on BOTH halves it reads the default board's journal and writes the
// default board's document — the right version onto the wrong board.
func TestTheRestoreHintNamesTheBoard(t *testing.T) {
	root := Root(t.TempDir())
	review := boardServer(t, root, "review")
	review.postDocument(t, `{"version":1,"rev":1,"nextId":2,"tabs":[
		{"id":"ab1","name":"Side note","type":"notes","state":{"text":"second"}}],"__by":"agent-2"}`)

	got, err := History(t.Context(), root, "review", "ab1", 0, DefaultInvocation)
	if err != nil {
		t.Fatal(err)
	}
	line := got.Human(DefaultInvocation)
	want := "aboard history ab1 --at 1 --name review | aboard apply --name review --by agent-1"
	if !strings.Contains(line, want) {
		t.Errorf("the restore hint does not carry the board name:\nwant %q in\n%s", want, line)
	}

	// The default board's hint is unchanged: a flag nobody needs on every
	// listing is noise, and this is the form the docs print.
	def := boardServer(t, root, "")
	def.postDocument(t, `{"version":1,"rev":1,"nextId":2,"tabs":[
		{"id":"ab1","name":"Plan","type":"notes","state":{"text":"edited"}}],"__by":"agent-1"}`)
	plain, err := History(t.Context(), root, "", "ab1", 0, DefaultInvocation)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.Human(DefaultInvocation), "--name") {
		t.Errorf("the default board's listing grew a --name flag:\n%s", plain.Human(DefaultInvocation))
	}
}

// A mount receipt is per tab, and tab ids repeat across boards — so one shared
// rendered.json meant the review board's ab1 overwriting the default board's,
// and `aboard rendered ab1` answering about whichever browser reported last.
func TestMountReceiptsAreKeptPerBoard(t *testing.T) {
	root := Root(t.TempDir())
	review := boardServer(t, root, "review")

	rec := httptest.NewRecorder()
	review.route(rec, httptest.NewRequest(http.MethodPost, "http://localhost/rendered",
		strings.NewReader(`{"tab":"ab1","type":"notes","mount":true,"controls":["save"]}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /rendered = %d: %s", rec.Code, rec.Body.String())
	}

	mine, err := Rendered(t.Context(), root, "review", "ab1")
	if err != nil || len(mine) != 1 {
		t.Fatalf("the named board's own receipts: %v %+v", err, mine)
	}
	theirs, err := Rendered(t.Context(), root, "", "ab1")
	if err != nil {
		t.Fatal(err)
	}
	if len(theirs) != 0 {
		t.Errorf("the receipt also landed in the default board's sidecar: %+v", theirs)
	}
	if _, err := os.Stat(root.RenderedFile("review")); err != nil {
		t.Errorf("rendered.review.json was not written: %v", err)
	}
}

// A `log` tab's sidecar is keyed by tab id too, so two boards' ab1 appended to
// ONE file and the human read one command's output interleaved with another's.
func TestSidecarLogsAreKeptPerBoard(t *testing.T) {
	root := Root(t.TempDir())
	review := boardServer(t, root, "review")

	rec := httptest.NewRecorder()
	review.route(rec, httptest.NewRequest(http.MethodPost, "http://localhost/log?tab=ab1",
		strings.NewReader("a line from the review board\n")))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /log = %d: %s", rec.Code, rec.Body.String())
	}

	mine, ok := root.LogFile("review", "ab1")
	if !ok {
		t.Fatal(`LogFile("review", "ab1") refused a plain tab id`)
	}
	body, err := os.ReadFile(mine)
	if err != nil {
		t.Fatalf("the named board's log was not written: %v", err)
	}
	if !strings.Contains(string(body), "review board") {
		t.Errorf("%s holds %q", mine, body)
	}
	if filepath.Dir(mine) != root.LogsDir("review") {
		t.Errorf("%s is not in the named board's logs directory %s", mine, root.LogsDir("review"))
	}
	theirs, _ := root.LogFile("", "ab1")
	if _, err := os.Stat(theirs); !os.IsNotExist(err) {
		t.Errorf("the line also reached the default board's log at %s: %v", theirs, err)
	}
}

// StateFiles is what makes a project-wide question answerable: `uploads/` is
// shared by every board, so accounting for it has to see every document.
func TestStateFilesListsEveryBoardInTheProject(t *testing.T) {
	root := Root(t.TempDir())
	if err := os.MkdirAll(root.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "review", "agent-2"} {
		if err := os.WriteFile(root.StateFile(name), []byte(oneTabBoard), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// None of these is a board: a file a human dropped in, a write-in-progress
	// temp file, and `aboard..json` — a base no StateFile call can produce, and
	// the one ValidateBoardName says yes to by accident, since the empty name IS
	// valid and means the default board. A listing that called any of them a
	// board would go on to read it as a board document, and `uploads` refuses to
	// account for anything when one will not parse.
	if err := os.WriteFile(filepath.Join(root.Dir(), "notes.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TempFileBeside(root.StateFile(""), 1), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.Dir(), "aboard..json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := root.StateFiles()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(got))
	for _, b := range got {
		names = append(names, b.Name)
		if want := root.StateFile(b.Name); b.Path != want {
			t.Errorf("%q maps to %s, want %s", b.Name, b.Path, want)
		}
	}
	if strings.Join(names, ",") != ",agent-2,review" {
		t.Errorf("StateFiles = %q, want the default board first then the named ones sorted", names)
	}
}
