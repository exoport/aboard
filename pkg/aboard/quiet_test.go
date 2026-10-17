package aboard

import (
	"encoding/json"
	"strings"
	"testing"
)

// `quiet`: a tab whose STATE an agent rewrites constantly — a status tab kept
// current after every command — and whose dot would therefore mean nothing. The
// flag narrows when the server stamps a NEW marker and nothing else, so each test
// here pins one side of that line: what goes quiet, and what must not.

func quietTab(state string) tab {
	return tab{
		ID: "ab1", Name: "Status", Type: "notes", Quiet: true,
		State: json.RawMessage(state),
	}
}

// The case the flag exists for: an agent rewrites a quiet tab's state, the write
// is accepted and journaled as usual, and no dot goes up.
func TestAnAgentRewritingAQuietTabRaisesNoDotAndIsStillJournaled(t *testing.T) {
	current := boardJSON(t, quietTab(`{"text":"one"}`))
	incoming := boardJSON(t, quietTab(`{"text":"two"}`))

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	got := tabByID(t, out, "ab1")
	if got.Touched != nil {
		t.Errorf("a state change on a quiet tab raised a dot: %+v", got.Touched)
	}
	if !got.Quiet {
		t.Error("the write lost the quiet flag")
	}
	if string(got.State) != `{"text":"two"}` {
		t.Errorf("the write itself did not land: %s", got.State)
	}
	entry := changeSummary(current, out, "agent-1", "apply")
	if len(entry.Tabs) != 1 || entry.Tabs[0] != "ab1" {
		t.Errorf("a quiet write must still be journaled: %v", entry.Tabs)
	}
}

// Guarantee 2 is untouched: quiet decides whether a NEW marker is stamped, never
// whether an existing one survives. A marker raised before the tab went quiet —
// the one saying it went quiet, typically — is the human's to dismiss, and a
// quiet write that omits it has it carried forward exactly as it was, not
// re-stamped with this write's name and time.
func TestAQuietTabKeepsTheMarkerItAlreadyHad(t *testing.T) {
	before := quietTab(`{"text":"one"}`)
	before.Touched = &touchMark{By: "agent-2", At: "T-earlier", Note: noteWentQuiet}
	current := boardJSON(t, before)
	incoming := boardJSON(t, quietTab(`{"text":"two"}`))

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	got := tabByID(t, out, "ab1").Touched
	if got == nil {
		t.Fatal("an agent write cleared a marker on a quiet tab — guarantee 2 has a hole")
	}
	if *got != *before.Touched {
		t.Errorf("the existing marker was re-stamped: got %+v, want %+v unchanged", *got, *before.Touched)
	}
}

// Turning quiet ON is the one write about a quiet tab the human must see, so it
// is stamped — with a note saying what happened — and the writes after it are
// not. Without the first half, quiet would be a way for a write to hide itself:
// flip the flag and change the state in one go.
func TestAnAgentTurningQuietOnIsAnnouncedOnceAndThenSilent(t *testing.T) {
	loud := quietTab(`{"text":"one"}`)
	loud.Quiet = false
	current := boardJSON(t, loud)
	incoming := boardJSON(t, quietTab(`{"text":"two"}`))

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	mark := tabByID(t, out, "ab1").Touched
	if mark == nil {
		t.Fatal("the write that made the tab quiet raised no dot — the human is never told")
	}
	if mark.By != "agent-1" || mark.Note != noteWentQuiet {
		t.Errorf("the marker = %+v, want agent-1 and the went-quiet note", *mark)
	}
	if entry := changeSummary(current, out, "agent-1", "apply"); len(entry.Tabs) != 1 {
		t.Errorf("turning quiet on was not journaled: %v", entry.Tabs)
	}

	// The human reads it and dismisses it; the next agent write is silent.
	dismissed := tabByID(t, out, "ab1")
	dismissed.Touched = nil
	current = boardJSON(t, dismissed)
	incoming = boardJSON(t, quietTab(`{"text":"three"}`))
	out, err = reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if mark := tabByID(t, out, "ab1").Touched; mark != nil {
		t.Errorf("the write after the announcement raised another dot: %+v", *mark)
	}
}

// Turning it back off is a change like any other and is announced: the tab is
// loud again from this write on.
func TestAnAgentTurningQuietOffIsAnnounced(t *testing.T) {
	current := boardJSON(t, quietTab(`{"text":"one"}`))
	loud := quietTab(`{"text":"one"}`)
	loud.Quiet = false
	incoming := boardJSON(t, loud)

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if tabByID(t, out, "ab1").Touched == nil {
		t.Error("an agent turning quiet off raised no dot")
	}
	if entry := changeSummary(current, out, "agent-1", "apply"); len(entry.Tabs) != 1 {
		t.Errorf("turning quiet off was not journaled: %v", entry.Tabs)
	}
}

// Creating a tab quiet IS turning quiet on, and it is announced for the same
// reason: a tab that arrived with no dot is a tab the human may never notice,
// and an agent could add them without limit.
func TestANewQuietTabIsAnnouncedOnce(t *testing.T) {
	other := tab{ID: "ab2", Name: "Plan", Type: "notes", State: json.RawMessage(`{"text":"x"}`)}
	current := boardJSON(t, other)
	incoming := boardJSON(t, other, quietTab(`{"text":"one"}`))

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	got := tabByID(t, out, "ab1")
	if got.Touched == nil {
		t.Fatal("a new quiet tab arrived with no dot")
	}
	if got.Touched.Note != noteNewQuietTab {
		t.Errorf("the new-tab marker does not say the tab is quiet: %q", got.Touched.Note)
	}
	if !got.Quiet {
		t.Error("an agent may create a tab quiet; the flag was dropped")
	}
}

// What quiet silences is the STATE. Every other field the dot announces is still
// announced on a quiet tab — they are rare and they matter, and the `done` stamp
// in particular is the only feedback the human gets that their request was read.
func TestAQuietTabStillAnnouncesEverythingButItsState(t *testing.T) {
	base := quietTab(`{"text":"one"}`)
	base.Note = "n"
	base.Requests = []requestAsk{{ID: "ab5", At: "T0", By: actorHuman, Text: "fix it"}}
	for _, tc := range []struct {
		what   string
		mutate func(x tab) tab
		marked bool
	}{
		{"state", func(x tab) tab { x.State = json.RawMessage(`{"text":"two"}`); return x }, false},
		{"name", func(x tab) tab { x.Name = "Renamed"; return x }, true},
		{"type", func(x tab) tab { x.Type = "markdown"; return x }, true},
		{"stateFrom", func(x tab) tab { x.StateFrom = "ab9"; return x }, true},
		{"note", func(x tab) tab { x.Note = "rewritten"; return x }, true},
		{"done stamp", func(x tab) tab {
			x.Requests = []requestAsk{{ID: "ab5", At: "T0", By: actorHuman, Text: "fix it", Done: &doneStamp{Note: "fixed"}}}
			return x
		}, true},
		{"nothing", func(x tab) tab { return x }, false},
	} {
		current := boardJSON(t, base)
		incoming := boardJSON(t, tc.mutate(base))

		out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
		if err != nil {
			t.Fatalf("%s: %v", tc.what, err)
		}
		marked := tabByID(t, out, "ab1").Touched != nil
		journaled := len(changeSummary(current, out, "agent-1", "apply").Tabs) > 0
		if marked != tc.marked {
			t.Errorf("%s changed on a quiet tab: marked=%v, want %v", tc.what, marked, tc.marked)
		}
		if journaled != (tc.what != "nothing") {
			t.Errorf("%s changed on a quiet tab: journaled=%v", tc.what, journaled)
		}
	}
}

// A human write is taken as is, quiet or not: they may set it, clear it, and
// their own edits never stamp a marker anyway.
func TestTheHumanMayToggleQuietFreely(t *testing.T) {
	loud := quietTab(`{"text":"one"}`)
	loud.Quiet = false
	for _, tc := range []struct {
		what     string
		from, to tab
	}{
		{"on", loud, quietTab(`{"text":"one"}`)},
		{"off", quietTab(`{"text":"one"}`), loud},
	} {
		current := boardJSON(t, tc.from)
		out, err := reconcileTabs(current, boardJSON(t, tc.to), actorHuman, testLogger())
		if err != nil {
			t.Fatal(err)
		}
		got := tabByID(t, out, "ab1")
		if got.Quiet != tc.to.Quiet {
			t.Errorf("the human turning quiet %s was not taken: quiet=%v", tc.what, got.Quiet)
		}
		if got.Touched != nil {
			t.Errorf("the human turning quiet %s stamped a marker: %+v", tc.what, got.Touched)
		}
		if entry := changeSummary(current, out, actorHuman, "browser"); len(entry.Tabs) != 1 {
			t.Errorf("the human turning quiet %s was not journaled: %v", tc.what, entry.Tabs)
		}
	}
}

// Guarantee 1 does not care whether a tab is quiet: an agent dropping one gets
// it back as a removal request, with the dot, because a removal request is a
// question for the human and not an update to the tab's contents.
func TestAQuietTabCannotBeDeletedQuietly(t *testing.T) {
	other := tab{ID: "ab2", Name: "Plan", Type: "notes", State: json.RawMessage(`{"text":"x"}`)}
	current := boardJSON(t, quietTab(`{"text":"one"}`), other)
	incoming := boardJSON(t, other)

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	got := tabByID(t, out, "ab1")
	if got.PendingRemoval == nil {
		t.Error("a quiet tab dropped by an agent was not restored as a removal request")
	}
	if got.Touched == nil {
		t.Error("the removal request on a quiet tab raised no dot")
	}
}

// The whole write path, server side: accepted, rev moved, journaled, no
// warning — `quiet` is a tab field the board knows, not a key it reports as
// unknown — and the document on disk says `"quiet": true` with no marker.
func TestAQuietWriteThroughTheServerIsJournaledAndWarnsNothing(t *testing.T) {
	srv := testServer(t, `{"version":1,"rev":1,"nextId":9,"updatedAt":"T0","tabs":[
	  {"id":"ab1","name":"Status","type":"notes","quiet":true,"state":{"text":"one"}},
	  {"id":"ab2","name":"Plan","type":"notes","state":{"text":"one"}}
	]}`)

	reply := postAs(t, srv, `"__by":"agent-1","__base":"1"`, `{"version":1,"nextId":9,"tabs":[
	  {"id":"ab1","name":"Status","type":"notes","quiet":true,"state":{"text":"two"}},
	  {"id":"ab2","name":"Plan","type":"notes","state":{"text":"one"}}
	]}`)
	if _, warned := reply["warnings"]; warned {
		t.Errorf("a quiet write was warned about: %v", reply["warnings"])
	}
	if rev, _ := reply["rev"].(float64); rev != 2 {
		t.Errorf("rev = %v, want 2", reply["rev"])
	}

	for _, got := range srv.readTabs(t) {
		if got.ID == "ab1" && (got.Touched != nil || !got.Quiet) {
			t.Errorf("ab1 on disk: quiet=%v touched=%+v, want quiet and no marker", got.Quiet, got.Touched)
		}
	}
	entries, err := journalFromDisk(srv.root, srv.name, 10)
	if err != nil {
		t.Fatal(err)
	}
	last := entries[len(entries)-1]
	if len(last.Tabs) != 1 || last.Tabs[0] != "ab1" || last.By != "agent-1" {
		t.Errorf("the journal entry = %+v, want ab1 by agent-1", last)
	}

	disk := string(readFileForTest(t, srv.root.StateFile(srv.name)))
	if !strings.Contains(disk, `"quiet": true`) {
		t.Errorf("the document on disk lost the flag:\n%s", disk)
	}
	// And a tab nobody made quiet does not grow `"quiet": false`: under
	// encoding/json/v2 `omitempty` keeps a false, which would put the key on every
	// tab of every board on its next write.
	if strings.Contains(disk, `"quiet": false`) {
		t.Errorf("a tab that is not quiet was written with the key:\n%s", disk)
	}
}

// `apply --strict` refuses on any warning, so it is the sharpest form of "quiet
// is a recognised key": a document carrying it must pass.
func TestApplyStrictAcceptsAQuietTab(t *testing.T) {
	root, last := applyTarget(t)
	doc := `{"version":1,"rev":1,"nextId":2,"tabs":[
  {"id":"ab1","name":"Status","type":"notes","quiet":true,"state":{"text":"one"}}]}`

	_, errOut, err := runApplyWith(t, root, ApplyOptions{By: "agent-1", Strict: true}, doc)
	if err != nil {
		t.Fatalf("--strict refused a document whose only novelty is quiet: %v\n%s", err, errOut)
	}
	if strings.TrimSpace(errOut) != "" {
		t.Errorf("apply said something about a quiet tab:\n%s", errOut)
	}
	tabs, _ := last.doc["tabs"].([]any)
	if first, _ := tabs[0].(map[string]any); first["quiet"] != true {
		t.Errorf("apply did not send the flag through: %v", last.doc)
	}
}

// The 409 merge treats quiet like the other fields the record carries: a tab
// somebody else made quiet while we wrote to a different one merges, and both
// sides changing it on the same tab is a collision named by the field.
func TestMergeKeepsAForeignQuietAndStopsOnASameTabOne(t *testing.T) {
	t.Run("foreign", func(t *testing.T) {
		root, _, _ := liveBoard(t)
		mine := readBoard(t, root)
		setTabText(t, mine, "ab1", "mine")

		theirs := readBoard(t, root)
		setTabField(t, theirs, "ab2", "quiet", true)
		if _, _, err := applyDoc(t, root, theirs, "agent-2"); err != nil {
			t.Fatal(err)
		}
		if _, errOut, err := applyDoc(t, root, mine, "agent-1"); err != nil {
			t.Fatalf("a foreign quiet must not stop a write to another tab: %v (%s)", err, errOut)
		}
		if got := boardTab(t, root, "ab2")["quiet"]; got != true {
			t.Errorf("the merge dropped the other session's quiet: %v", got)
		}
	})
	t.Run("explicit false is not a change", func(t *testing.T) {
		root, _, _ := liveBoard(t)
		mine := readBoard(t, root)
		setTabText(t, mine, "ab1", "mine")
		// Spelled out on a tab nobody made quiet: the same answer as absent, so
		// when a foreign write moves ab2 this is not our edit colliding with it.
		setTabField(t, mine, "ab2", "quiet", false)

		theirs := readBoard(t, root)
		setTabText(t, theirs, "ab2", "theirs")
		if _, _, err := applyDoc(t, root, theirs, "agent-2"); err != nil {
			t.Fatal(err)
		}
		if _, errOut, err := applyDoc(t, root, mine, "agent-1"); err != nil {
			t.Fatalf("an explicit quiet:false was read as a collision: %v (%s)", err, errOut)
		}
	})
	t.Run("same tab", func(t *testing.T) {
		root, _, _ := liveBoard(t)
		mine := readBoard(t, root)
		setTabField(t, mine, "ab1", "quiet", true)

		theirs := readBoard(t, root)
		setTabField(t, theirs, "ab1", "note", "theirs")
		if _, _, err := applyDoc(t, root, theirs, "agent-2"); err != nil {
			t.Fatal(err)
		}
		// Ours changed only quiet; theirs only the note. The record says ab1
		// was not quiet at our base, so ours DID change it, and the tab moved
		// on the board: a same-tab collision, named by the field.
		_, _, err := applyDoc(t, root, mine, "agent-1")
		if err == nil || !strings.Contains(err.Error(), "quiet") {
			t.Errorf("a same-tab quiet change was not refused by name: %v", err)
		}
	})
}

// A restore puts back a tab's CONTENT, and quiet is not content: it is a
// decision about whose attention the tab's changes are worth, which somebody may
// have made after the version being restored.
func TestARestoreLeavesQuietAsTheBoardHasIt(t *testing.T) {
	live := docTab{tab: quietTab(`{"text":"now"}`)}
	was, err := json.Marshal(tab{ID: "ab1", Name: "Old", Type: "notes", State: json.RawMessage(`{"text":"then"}`)})
	if err != nil {
		t.Fatal(err)
	}
	restoreOnto(&live, HistoryVersion{State: json.RawMessage(`{"text":"then"}`), Tab: was})
	if !live.Quiet {
		t.Error("restoring an older version un-quieted the tab")
	}
	if live.Name != "Old" || string(live.State) != `{"text":"then"}` {
		t.Errorf("the content did not come back: name=%q state=%s", live.Name, live.State)
	}
}

func setTabField(t *testing.T, doc map[string]any, id, field string, value any) {
	t.Helper()
	list, _ := doc["tabs"].([]any)
	for _, raw := range list {
		if tab, ok := raw.(map[string]any); ok && tab["id"] == id {
			tab[field] = value
			return
		}
	}
	t.Fatalf("no tab %s in the document", id)
}

func boardTab(t *testing.T, root Root, id string) map[string]any {
	t.Helper()
	list, _ := readBoard(t, root)["tabs"].([]any)
	for _, raw := range list {
		if tab, ok := raw.(map[string]any); ok && tab["id"] == id {
			return tab
		}
	}
	t.Fatalf("no tab %s on the board", id)
	return nil
}
