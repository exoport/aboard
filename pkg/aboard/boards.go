// boards.go — every board running on this machine, and the honest answer where
// that question cannot be asked.
//
// The feature was proposed, dropped and then REVERSED, all in 2026-08-26, and
// the reversal came with the design it is built to: a scan of the process table,
// no registry file. The registry half stays rejected — `~/.aboard/known-roots.json`
// would be new user-level state outside `.aboard/`, written on every serve and
// still only a hint. The process table is not a hint: a board is a running
// process or it is not one, and nothing has to be cleaned up when it dies.
//
// The cost is that `/proc` is Linux only, and this binary ships for macOS and
// Windows. That is the reason the scan was written off the first time, and it is
// answered here rather than argued with: the scanner is behind //go:build linux,
// everywhere else the command exists, is declared, and says in one line why it
// cannot answer and what to run instead. A command that is missing on two of
// three platforms is worse than one that is present and honest — the reader who
// types it on a Mac learns something either way, and only one of the two ways
// tells them where the answer actually is.
//
// This file holds everything that is not the walk: the report, its rendering,
// the verification each candidate goes through, and both refusal messages. The
// refusals live here on purpose, so the message path is compiled and tested on
// every platform including the one that can never produce it.

package aboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrNoProcessTable is what Boards returns where there is no process table to
// walk: any OS but Linux, and a Linux without `/proc` mounted. A sentinel so the
// cli layer can map it to exit 2 — a usage refusal, decided before anything was
// contacted — rather than to the exit 1 that means "it ran and found nothing".
// Those two are the same picture on screen and call for opposite next moves.
var ErrNoProcessTable = errors.New("no process table to scan")

// msgUseStatusPerProject is the alternative, and both refusals end with it. One
// string because a reader who cannot have the machine-wide answer must always be
// pointed at the per-project one, in the same words, whichever refusal they hit.
func msgUseStatusPerProject(inv Invocation) string {
	return fmt.Sprintf("run `%s status` inside each project you want the answer for", inv)
}

// noProcessTable is the refusal for a platform that has no /proc at all. It
// takes the OS name rather than reading runtime.GOOS so that it can be tested
// where it can never fire, which is every machine this repo is developed on.
func noProcessTable(goos string, inv Invocation) error {
	return fmt.Errorf("%w: `boards` finds running boards by reading /proc, and /proc exists on Linux only — this is %s. %s",
		ErrNoProcessTable, goos, msgUseStatusPerProject(inv))
}

// noProcFS is the refusal for a Linux with no procfs mounted — a chroot, or a
// container built without one. Different sentence from the one above because the
// reader's next move is different: nothing is wrong with their platform, and
// mounting /proc would fix it.
func noProcFS(procRoot string, inv Invocation) error {
	return fmt.Errorf("%w: %s has no `self` entry, so it is not a process table (a chroot, or a container with no procfs mounted). %s",
		ErrNoProcessTable, procRoot, msgUseStatusPerProject(inv))
}

// BoardRow is one running board: one (project, name) pair, which is why two
// boards in one project are two rows. It carries the FULL project path, because
// the whole point of a machine-wide listing is that the reader is not standing
// in the project it names — a basename would be ambiguous exactly where the
// command is useful.
type BoardRow struct {
	Project string `json:"project" yaml:"project"`
	// Name is empty for the default board, as it is everywhere else in this
	// engine. The human form prints "default"; the structured forms omit it, so
	// a consumer compares against `--name` without translating.
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	App  string `json:"app,omitempty"  yaml:"app,omitempty"`
	URL  string `json:"url,omitempty"  yaml:"url,omitempty"`
	Port int    `json:"port,omitempty" yaml:"port,omitempty"`
	PID  int    `json:"pid"            yaml:"pid"`
	// Started and Version come from the live board where it answered, and from
	// its instance record where it did not.
	Started string `json:"started,omitempty" yaml:"started,omitempty"`
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
	// Tabs, LastEditedBy and UpdatedAt come from GET /aboard.json and are
	// therefore only ever set for a board that answered. Zero tabs on a board
	// that IS answering is a real answer: a board created and never written to.
	Tabs         int    `json:"tabs"                   yaml:"tabs"`
	LastEditedBy string `json:"lastEditedBy,omitempty" yaml:"lastEditedBy,omitempty"`
	UpdatedAt    string `json:"updatedAt,omitempty"    yaml:"updatedAt,omitempty"`
	// Recorded says an instance record names this pid; Answering says /health
	// agreed. Recorded && !Answering is a stale record, and it is LISTED rather
	// than dropped — a process that looks like a board and does not answer is
	// information, and dropping it would report the machine as quieter than it is.
	Recorded  bool `json:"recorded"  yaml:"recorded"`
	Answering bool `json:"answering" yaml:"answering"`

	InstanceFile string `json:"instanceFile,omitempty" yaml:"instanceFile,omitempty"`
}

// BoardsReport is what `aboard boards` knows: the boards, and how much of the
// process table it managed to look at while finding them.
//
// The two counters are not decoration. A listing of running processes is only
// as true as the reader's permissions, and "no board found" after inspecting
// 400 processes means something quite different from "no board found" after
// inspecting 3.
type BoardsReport struct {
	Boards []BoardRow `json:"boards" yaml:"boards"`
	// Inspected is how many process entries the scan looked at.
	Inspected int `json:"inspected" yaml:"inspected"`
	// Unreadable counts processes the scan could not FINISH inspecting: another
	// user's, whose `cwd` link the kernel refuses to resolve for us, and the rare
	// one whose working directory no longer resolves to a project at all. Counted
	// and reported rather than silently skipped, because a board this scan cannot
	// see is exactly what a reader would otherwise conclude is not running.
	Unreadable int `json:"unreadable" yaml:"unreadable"`
}

// Boards lists every board running on this machine.
//
// It needs no project and finds no root of its own: that is the difference
// between it and `status`, and it is why the command works from a directory that
// has never held a board.
func Boards(ctx context.Context, inv Invocation) (BoardsReport, error) {
	return scanBoards(ctx, procDir, inv)
}

// describeBoard turns one candidate process into a row: which of the project's
// boards that pid is serving, and whether it is still answering.
//
// It does exactly what `status` does for one project, in the same order and with
// the same probe — read the instance records, keep the one whose pid matches,
// verify it over /health — with one difference that the machine-wide case forces:
// `status` is TOLD the board name and can name the record directly, while here
// the pid is all there is, so every record in the project is read and matched.
func describeBoard(ctx context.Context, root Root, pid int) BoardRow {
	row := BoardRow{Project: root.String(), PID: pid}

	rec, file, found := instanceForPID(root, pid)
	if !found {
		// A serve process with no record naming it. Rare and real: the record is
		// written at startup, so a board caught in its first milliseconds has none,
		// and a board started with --state has one that may name a different board
		// than its own document. Reported with what is known rather than dropped —
		// the pid and the project are the two facts the reader needs to go look.
		return row
	}
	row.Recorded = true
	row.InstanceFile = file
	row.Name, row.App, row.URL = rec.Name, rec.App, rec.URL
	row.Port, row.Started, row.Version = rec.Port, rec.Started, rec.Version

	live := ProbeBoard(ctx, rec.Port, rec.Base)
	// The project check is the brief's, and it is not paranoia: ports are derived
	// from a root, a derived port can be taken, and a board that probed forward
	// onto the port another project's record names would otherwise be reported as
	// this project's own.
	if live == nil || live.Project != root.String() {
		return row
	}
	row.Answering = true
	row.App, row.URL, row.Version, row.Started = live.App, live.URL, live.Version, live.Started

	if doc, err := boardSummary(ctx, live.URL); err == nil {
		row.Tabs, row.LastEditedBy, row.UpdatedAt = len(doc.Tabs), doc.LastEditedBy, doc.UpdatedAt
	}
	return row
}

// instanceForPID finds which of a project's boards a pid is serving.
//
// Every record in the project is read because the name is what is being looked
// FOR: `.aboard/run/instance.json` and `instance.review.json` are two boards of
// one project and a pid belongs to exactly one of them.
func instanceForPID(root Root, pid int) (inst Instance, file string, ok bool) {
	// The pattern is built in layout.go; a bad pattern is the only error Glob
	// returns, and this one is a constant.
	files, _ := filepath.Glob(root.InstanceGlob())
	sort.Strings(files) // deterministic when two records somehow name one pid
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var got Instance
		if json.Unmarshal(body, &got) != nil || got.PID != pid {
			continue
		}
		return got, path, true
	}
	return inst, "", false
}

// docSummary is the three things a listing wants from the board document, and
// nothing else.
type docSummary struct {
	// Tabs is decoded as a slice of EMPTY structs on purpose: encoding/json walks
	// each tab and stores none of it, so counting a 10 MB board costs the parse
	// and no allocation per tab. A []json.RawMessage here would copy the whole
	// document into the count.
	Tabs         []struct{} `json:"tabs"`
	LastEditedBy string     `json:"lastEditedBy"`
	UpdatedAt    string     `json:"updatedAt"`
}

// boardSummary asks a live board how many tabs it holds and who last wrote.
//
// Failure is not an error the caller acts on: the board answered /health a
// moment ago, so a failure here leaves the three fields unset and the row still
// says the board is running, which is the fact that was actually established.
func boardSummary(ctx context.Context, url string) (docSummary, error) {
	var doc docSummary
	client := &http.Client{Timeout: summaryTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+routeState, http.NoBody)
	if err != nil {
		return doc, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return doc, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return doc, fmt.Errorf("board returned %d for %s", resp.StatusCode, routeState)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&doc); err != nil {
		return doc, err
	}
	return doc, nil
}

// summaryTimeout is what the tab count may cost. Longer than probeTimeout
// because this one parses the whole board document, and a 10 MB board on a busy
// machine is slower than a one-line health reply — but still short: a listing
// that hangs on one board tells the reader nothing about the others.
const summaryTimeout = 2 * time.Second

// sortRows orders the listing by project, then name, then pid: everything about
// one checkout together, with its default board before its named ones (the empty
// name sorts first), and a deterministic order even where the first two are not
// enough to separate two rows.
func sortRows(rows []BoardRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Project != rows[j].Project {
			return rows[i].Project < rows[j].Project
		}
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		// The pair is not unique once an UNRECORDED row is in the listing: it has
		// no name to be told apart by, so two of them in one project tie. Broken
		// on the pid, or the order would be whatever ReadDir returned — which
		// sorts pids as TEXT, so "1000" comes before "999" and the same machine
		// lists the same two boards in a different order after a reboot.
		return rows[i].PID < rows[j].PID
	})
}

// displayName is the name a person reads. The empty name is the default board
// and printing nothing for it would leave a column that is blank exactly when
// there is only one board — the commonest case.
func (b BoardRow) displayName() string {
	if b.Name == "" {
		return "default"
	}
	return b.Name
}

// headingName is displayName everywhere except on a row no instance record
// named, where the empty name means "nothing told us which board this is" and
// NOT "the default board". Printing "default" there asserts the one fact the
// next line of the listing says is unknown — and puts two identical [default]
// headings in one project the moment two unidentified processes are serving it,
// which is the commonest shape of this row: a second board caught in the
// milliseconds before it writes its record.
func (b BoardRow) headingName() string {
	if !b.Recorded {
		return "board not identified"
	}
	return b.displayName()
}

// Human renders the listing.
//
// Two lines per board rather than one row of eleven columns, and that IS the
// judgement: the fields the brief asks for — a full absolute project path, a
// URL, a version, two timestamps — do not fit a terminal on one line, and a
// table that wraps is harder to read than no table. The ORDER is the table's
// (project, then name), and the structured forms carry every field as a row.
func (r BoardsReport) Human(inv Invocation) string {
	var b strings.Builder
	if len(r.Boards) == 0 {
		fmt.Fprintf(&b, "no running board found (%d process%s inspected)\n", r.Inspected, pluralES(r.Inspected))
		b.WriteString(r.limits(inv))
		return b.String()
	}
	fmt.Fprintf(&b, "%d board%s (%d process%s inspected)\n",
		len(r.Boards), plural(len(r.Boards)), r.Inspected, pluralES(r.Inspected))
	for i := range r.Boards {
		row := &r.Boards[i]
		fmt.Fprintf(&b, "\n  %s  [%s]\n", row.Project, row.headingName())
		switch {
		case !row.Recorded:
			fmt.Fprintf(&b, "    pid %d is serving this project, but no instance record names it\n", row.PID)
			fmt.Fprintf(&b, "    (a board caught in its first moments, or one started with --state)\n")
		case !row.Answering:
			fmt.Fprintf(&b, "    recorded but not answering: %s (pid %d)\n", row.URL, row.PID)
			fmt.Fprintf(&b, "    %s\n", strings.TrimSpace(row.App+" "+row.Version+", recorded "+row.Started))
		default:
			fmt.Fprintf(&b, "    %-32s pid %-8d %d tab%s\n", row.URL, row.PID, row.Tabs, plural(row.Tabs))
			fmt.Fprintf(&b, "    %s\n", strings.TrimSpace(row.App+" "+row.Version+", up since "+row.Started))
			if row.LastEditedBy != "" {
				fmt.Fprintf(&b, "    last write by %s at %s\n", row.LastEditedBy, row.UpdatedAt)
			}
		}
	}
	b.WriteString("\n")
	b.WriteString(r.limits(inv))
	return b.String()
}

// limits is what the listing cannot see, printed with it rather than left to the
// docs — the same reason the mount receipts print their own two limits. A
// listing of processes reads as a listing of BOARDS, and the gap between those
// two is where a reader draws a wrong conclusion.
func (r BoardsReport) limits(inv Invocation) string {
	var b strings.Builder
	if r.Unreadable > 0 {
		fmt.Fprintf(&b, "%d process%s could not be inspected (permission)\n", r.Unreadable, pluralES(r.Unreadable))
	}
	b.WriteString("(this is the process table, so a board that is not running does not appear here —\n" +
		" " + msgUseStatusPerProject(inv) + ")\n")
	return b.String()
}

// pluralES is plural's sibling for a word that takes -es. Its own function
// rather than a parameter on plural, which is called in a dozen places and reads
// better with one argument.
func pluralES(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}
