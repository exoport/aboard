package aboard

import (
	"errors"
	"strings"
	"testing"
)

// The refusal is the half of `boards` that this machine can never produce, so it
// is the half that has to be tested here rather than trusted. It is also the
// only thing a macOS or Windows user of this binary will ever see from the
// command, which makes "does it say the reason AND the alternative" the whole
// acceptance test.
//
// Fails before: noProcessTable did not exist, and the non-Linux path returned a
// bare "not supported" from the stub with nothing to run instead.
func TestTheNoProcessTableRefusalNamesThePlatformAndTheAlternative(t *testing.T) {
	for _, goos := range []string{"darwin", "windows"} {
		err := noProcessTable(goos)
		if !errors.Is(err, ErrNoProcessTable) {
			t.Errorf("%s: refusal does not wrap ErrNoProcessTable, so the cli cannot map it to exit 2", goos)
		}
		msg := err.Error()
		for _, want := range []string{"/proc", goos, "aboard status"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: the refusal does not mention %q: %s", goos, want, msg)
			}
		}
	}
}

// A Linux with no procfs is a different sentence, because the reader's next move
// is different: nothing is wrong with their platform. It still has to end in the
// same alternative, which is why both refusals share msgUseStatusPerProject.
func TestTheMissingProcFSRefusalNamesThePathAndTheAlternative(t *testing.T) {
	err := noProcFS("/proc")
	if !errors.Is(err, ErrNoProcessTable) {
		t.Fatal("the missing-procfs refusal does not wrap ErrNoProcessTable")
	}
	msg := err.Error()
	if !strings.Contains(msg, "/proc") || !strings.Contains(msg, msgUseStatusPerProject) {
		t.Errorf("refusal is missing the path or the alternative: %s", msg)
	}
	if strings.Contains(msg, "Linux only") {
		t.Errorf("this refusal fires ON Linux; telling the reader that /proc is Linux-only is the wrong sentence: %s", msg)
	}
}

// One row per (project, name), sorted by project then name — the default board
// (empty name) before the project's named ones.
func TestRowsSortByProjectThenName(t *testing.T) {
	rows := []BoardRow{
		{Project: "/b", Name: "review"},
		{Project: "/a", Name: "review"},
		{Project: "/b"},
		{Project: "/a", Name: "alpha"},
		{Project: "/a"},
	}
	sortRows(rows)
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, r.Project+"/"+r.displayName())
	}
	want := "/a/default /a/alpha /a/review /b/default /b/review"
	if strings.Join(got, " ") != want {
		t.Errorf("sorted to %q, want %q", strings.Join(got, " "), want)
	}
}

// "No board found" is a claim about the machine, and how much of the machine was
// actually looked at is what makes it one. Three processes inspected and four
// hundred are the same sentence without the count, and they justify opposite
// next moves.
func TestAnEmptyListingSaysHowMuchOfTheMachineItSaw(t *testing.T) {
	out := BoardsReport{Inspected: 412, Unreadable: 3}.Human()
	for _, want := range []string{"no running board found", "412 processes inspected", "3 processes could not be inspected (permission)", "aboard status"} {
		if !strings.Contains(out, want) {
			t.Errorf("the empty listing does not say %q:\n%s", want, out)
		}
	}
}

// A record whose process has gone is information, not noise: it is the reason a
// URL somebody has bookmarked stopped working. Listed, and labelled.
func TestAStaleRecordIsListedRatherThanDropped(t *testing.T) {
	out := BoardsReport{
		Inspected: 9,
		Boards: []BoardRow{{
			Project: "/p", Name: "review", App: "aboard", URL: "http://localhost:41001",
			Port: 41001, PID: 42, Started: "T0", Version: "v1", Recorded: true,
		}},
	}.Human()
	if !strings.Contains(out, "recorded but not answering") {
		t.Errorf("a recorded-but-dead board is not labelled:\n%s", out)
	}
	if !strings.Contains(out, "/p") || !strings.Contains(out, "[review]") {
		t.Errorf("the listing does not name the project and the board:\n%s", out)
	}
}

// A row no instance record named must not be labelled [default]. Its empty name
// is "we do not know which board this is", and the row's own next line says so —
// two lines that contradicted each other, and two such processes in one project
// would have printed the same heading twice.
//
// Fails before: the heading used displayName(), which maps an empty name to
// "default" unconditionally.
func TestAnUnidentifiedRowIsNotLabelledTheDefaultBoard(t *testing.T) {
	out := BoardsReport{
		Inspected: 9,
		Boards: []BoardRow{
			{Project: "/p", PID: 41},
			{Project: "/p", PID: 42},
		},
	}.Human()
	if strings.Contains(out, "[default]") {
		t.Errorf("an unrecorded row claims to be the default board:\n%s", out)
	}
	if strings.Count(out, "no instance record names it") != 2 {
		t.Errorf("both unidentified processes should be listed:\n%s", out)
	}
}

// The full absolute path, and the word "default" for the board that has no name.
// Both are about the reader NOT being in the project the row names, which is the
// only situation this command exists for.
func TestALiveRowNamesTheWholeProjectPathAndTheDefaultBoard(t *testing.T) {
	out := BoardsReport{
		Inspected: 9,
		Boards: []BoardRow{{
			Project: "/home/someone/work/checkout", App: "aboard", URL: "http://localhost:41001",
			Port: 41001, PID: 42, Started: "T0", Version: "v1", Tabs: 15,
			LastEditedBy: "human", UpdatedAt: "T1", Recorded: true, Answering: true,
		}},
	}.Human()
	for _, want := range []string{"/home/someone/work/checkout", "[default]", "15 tabs", "last write by human at T1", "1 board "} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing does not say %q:\n%s", want, out)
		}
	}
}
