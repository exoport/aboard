package aboard

import (
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"
)

// boardJSON builds a document body the way a caller would submit one.
func boardJSON(t *testing.T, tabs ...tab) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"version": SchemaVersion, "nextId": 1, "tabs": tabs})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func tabByID(t *testing.T, tabs []tab, id string) tab {
	t.Helper()
	for i := range tabs {
		if tabs[i].ID == id {
			return tabs[i]
		}
	}
	t.Fatalf("no tab %s in %d", id, len(tabs))
	return tab{}
}

// Guarantee 4, which had NO CALL SITES: mergeSeen was written and never wired
// in, so the guarantee existed in the comment at the top of tabs.go and nowhere
// in the code. An agent write that dropped `seen` — which is most writes, since
// most agents never touch it — erased every other actor's read stamp.
func TestAgentCannotClearAnotherActorsSeen(t *testing.T) {
	current := boardJSON(t, tab{
		ID: "bb1", Name: "Plan", Type: "notes",
		State: json.RawMessage(`{"text":"one"}`),
		Seen:  map[string]string{"human": "T-human", "agent-2": "T-two"},
	})

	// A normal write: the agent edits the state and says nothing about `seen`.
	incoming := boardJSON(t, tab{
		ID: "bb1", Name: "Plan", Type: "notes",
		State: json.RawMessage(`{"text":"two"}`),
	})

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	got := tabByID(t, out, "bb1").Seen
	if got["human"] != "T-human" || got["agent-2"] != "T-two" {
		t.Fatalf("a write that never mentioned seen erased it: %v", got)
	}
}

func TestAgentMaySetOnlyItsOwnSeen(t *testing.T) {
	current := boardJSON(t, tab{
		ID: "bb1", Name: "Plan", Type: "notes",
		State: json.RawMessage(`{"text":"one"}`),
		Seen:  map[string]string{"human": "T-human", "agent-2": "T-two"},
	})
	// The write claims a stamp for itself AND rewrites two it does not own.
	incoming := boardJSON(t, tab{
		ID: "bb1", Name: "Plan", Type: "notes",
		State: json.RawMessage(`{"text":"one"}`),
		Seen:  map[string]string{"agent-1": "T-mine", "human": "FORGED", "agent-2": "FORGED"},
	})

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	got := tabByID(t, out, "bb1").Seen
	if got["agent-1"] != "T-mine" {
		t.Errorf("the writer's own stamp was not accepted: %v", got)
	}
	if got["human"] != "T-human" || got["agent-2"] != "T-two" {
		t.Errorf("another actor's stamp was overwritten: %v", got)
	}
}

// The human acts in the browser and their write is taken as-is: they may clear
// markers, delete tabs, and reset read state. That asymmetry is the reason `by`
// matters at all, so it is asserted rather than assumed.
func TestHumanWriteIsTakenAsIs(t *testing.T) {
	current := boardJSON(
		t,
		tab{ID: "bb1", Name: "Plan", Type: "notes", Seen: map[string]string{"agent-2": "T"}},
		tab{ID: "bb2", Name: "Gone", Type: "notes"},
	)
	incoming := boardJSON(t, tab{ID: "bb1", Name: "Plan", Type: "notes"})

	out, err := reconcileTabs(current, incoming, "human", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("the human's deletion was undone: %d tabs", len(out))
	}
	if out[0].Seen != nil {
		t.Errorf("the human's write did not clear read state: %v", out[0].Seen)
	}
}

// A note is the statement of what the tab is FOR, which the human may have
// written or edited, and an agent can overwrite it in a normal write. It was in neither comparison: no dot
// on the tab, and nothing in the journal, so the sentence could be replaced with
// no trace at all.
func TestNoteOnlyEditIsMarkedAndJournaled(t *testing.T) {
	current := boardJSON(t, tab{
		ID: "bb1", Name: "Plan", Type: "notes",
		State: json.RawMessage(`{"text":"one"}`),
		Note:  "what the human wanted this for",
	})
	incoming := boardJSON(t, tab{
		ID: "bb1", Name: "Plan", Type: "notes",
		State: json.RawMessage(`{"text":"one"}`),
		Note:  "something an agent decided instead",
	})

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if mark := tabByID(t, out, "bb1").Touched; mark == nil {
		t.Error("a note-only edit raised no change marker")
	} else if mark.By != "agent-1" {
		t.Errorf("the marker names %q", mark.By)
	}

	entry := changeSummary(current, out, "agent-1", "apply")
	if len(entry.Tabs) != 1 || entry.Tabs[0] != "bb1" {
		t.Errorf("a note-only edit was not journaled: %v", entry.Tabs)
	}
}

// The two comparisons must agree. When they did not, a write could be journaled
// with no marker on the tab (traceable but invisible) or marked with no journal
// line (visible but untraceable).
func TestMarkerAndJournalAgreeOnWhatChanged(t *testing.T) {
	base := tab{
		ID: "bb1", Name: "Plan", Type: "notes",
		State: json.RawMessage(`{"text":"one"}`), Note: "n", StateFrom: "",
	}
	for _, tc := range []struct {
		what   string
		mutate func(t tab) tab
		want   bool
	}{
		{"state", func(x tab) tab { x.State = json.RawMessage(`{"text":"two"}`); return x }, true},
		{"name", func(x tab) tab { x.Name = "Renamed"; return x }, true},
		{"type", func(x tab) tab { x.Type = "markdown"; return x }, true},
		{"stateFrom", func(x tab) tab { x.StateFrom = "bb9"; return x }, true},
		{"note", func(x tab) tab { x.Note = "rewritten"; return x }, true},
		{"nothing", func(x tab) tab { return x }, false},
	} {
		current := boardJSON(t, base)
		incoming := boardJSON(t, tc.mutate(base))

		out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
		if err != nil {
			t.Fatalf("%s: %v", tc.what, err)
		}
		marked := tabByID(t, out, "bb1").Touched != nil
		journaled := len(changeSummary(current, out, "agent-1", "apply").Tabs) > 0

		if marked != tc.want || journaled != tc.want {
			t.Errorf("%s changed: marked=%v journaled=%v, want both %v", tc.what, marked, journaled, tc.want)
		}
	}
}

// An absent __by is the case this is really about: reconcileTabs must treat
// "unknown" exactly like any other agent, or the default that server.go now
// writes would be a way around every guarantee.
func TestUnknownActorGetsAgentPowersOnly(t *testing.T) {
	current := boardJSON(
		t,
		tab{ID: "bb1", Name: "Plan", Type: "notes", Touched: &touchMark{By: "agent-2", At: "T"}},
		tab{ID: "bb2", Name: "Queue", Type: "notes"},
	)
	// A write that drops one tab and clears the other's marker.
	incoming := boardJSON(t, tab{ID: "bb1", Name: "Plan", Type: "notes"})

	out, err := reconcileTabs(current, incoming, "unknown", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("an unknown actor deleted a tab: %d remain", len(out))
	}
	if gone := tabByID(t, out, "bb2"); gone.PendingRemoval == nil {
		t.Error("the dropped tab came back without a removal request")
	}
	if kept := tabByID(t, out, "bb1"); kept.Touched == nil {
		t.Error("an unknown actor cleared a change marker")
	}
}

// The counterpart to the guarantee: a chat ack cannot be dropped, because the
// human's edit window closed when a session consumed the message.
func TestAgentCannotUnreadAChatMessage(t *testing.T) {
	current := boardJSON(t, tab{
		ID: "bb1", Name: "Coordination", Type: "chat",
		State: json.RawMessage(`{"messages":[{"id":"bb2","text":"hi","ackBy":"agent-2","ackAt":"T"}]}`),
	})
	incoming := boardJSON(t, tab{
		ID: "bb1", Name: "Coordination", Type: "chat",
		State: json.RawMessage(`{"messages":[{"id":"bb2","text":"hi"}]}`),
	})

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tabByID(t, out, "bb1").State), "agent-2") {
		t.Errorf("the ack was dropped: %s", tabByID(t, out, "bb1").State)
	}
}

// Guarantee 1's other half. The restore branch covers a tab an agent DROPPED;
// this covers the far commoner case — an agent carrying the whole document
// through a read-modify-write with the field simply absent, because nothing it
// did was about that tab. `pendingRemoval` was taken verbatim, so a routine write
// by one session cancelled another's removal request and the human's banner
// vanished with nothing to say it had ever been raised.
func TestAgentWriteCannotClearAPendingRemoval(t *testing.T) {
	current := boardJSON(t, tab{
		ID: "bb1", Name: "Plan", Type: "notes",
		State:          json.RawMessage(`{"text":"one"}`),
		PendingRemoval: &removalAsk{By: "agent-2", At: "T0", Reason: "superseded by the new plan"},
	})

	// agent-1 edits the tab and says nothing about the request.
	incoming := boardJSON(t, tab{
		ID: "bb1", Name: "Plan", Type: "notes",
		State: json.RawMessage(`{"text":"two"}`),
	})

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	got := tabByID(t, out, "bb1").PendingRemoval
	if got == nil {
		t.Fatal("an agent write erased another agent's removal request")
	}
	if got.By != "agent-2" || got.Reason != "superseded by the new plan" {
		t.Errorf("the request was rewritten rather than carried: %+v", got)
	}
}

// And the human still answers it — that is what "only the human clears it" means,
// and a carry-forward that applied to them too would make the request permanent.
func TestAHumanWriteAnswersAPendingRemoval(t *testing.T) {
	current := boardJSON(t, tab{
		ID: "bb1", Name: "Plan", Type: "notes",
		PendingRemoval: &removalAsk{By: "agent-2", At: "T0", Reason: "spent"},
	})
	incoming := boardJSON(t, tab{ID: "bb1", Name: "Plan", Type: "notes"})

	out, err := reconcileTabs(current, incoming, actorHuman, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if tabByID(t, out, "bb1").PendingRemoval != nil {
		t.Error("the human declined the removal and the request came back anyway")
	}
}

// testLogger swallows the "a tab was dropped" line these tests deliberately
// provoke. Passing log.Default() would print it into the suite's output on every
// guarantee-1 case, which reads as a failure.
func testLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// Guarantee 4 had a condition on it, and a guarantee with a condition is not
// one. `mergeSeen` short-circuited when the tab carried no previous `seen` map
// and returned the write's own map verbatim — so on a tab that had never been
// stamped, an agent could PLANT a stamp for the human and the "changed since I
// last looked" dot would never light for that tab again.
func TestAnAgentCannotPlantASeenStampOnATabThatHadNone(t *testing.T) {
	current := []byte(`{"tabs":[{"id":"bb1","name":"Plan","type":"dag","state":{}}]}`)
	incoming := []byte(`{"tabs":[{"id":"bb1","name":"Plan","type":"dag","state":{},
		"seen":{"human":"2099-01-01T00:00:00.000Z","agent-1":"2026-08-26T00:00:00.000Z"}}]}`)

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	seen := out[0].Seen
	if _, planted := seen["human"]; planted {
		t.Error("an agent planted a seen stamp for the human on a tab that had none")
	}
	if seen["agent-1"] != "2026-08-26T00:00:00.000Z" {
		t.Errorf("the writer's own stamp was lost: %v", seen)
	}
}

// The same hole from the other direction: tab CREATION skipped the filter
// altogether, so a new tab could arrive with the human's dot already put out by
// the very write that made it.
func TestANewTabCannotArriveWithSomebodyElsesSeenStamp(t *testing.T) {
	current := []byte(`{"tabs":[]}`)
	incoming := []byte(`{"tabs":[{"id":"bb9","name":"Questions","type":"form","state":{},
		"seen":{"human":"2099-01-01T00:00:00.000Z","agent-2":"2099-01-01T00:00:00.000Z","agent-1":"2026-08-26T00:00:00.000Z"}}]}`)

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want one tab, got %d", len(out))
	}
	for _, actor := range []string{"human", "agent-2"} {
		if _, planted := out[0].Seen[actor]; planted {
			t.Errorf("a new tab arrived carrying a seen stamp for %q", actor)
		}
	}
	if out[0].Seen["agent-1"] != "2026-08-26T00:00:00.000Z" {
		t.Errorf("the creator's own stamp was dropped: %v", out[0].Seen)
	}
	// And it is still marked as new, which is the other half of the human
	// noticing it.
	if out[0].Touched == nil {
		t.Error("a new tab arrived with no touch marker")
	}
}

// The common case must not regress: a write that never mentions `seen` at all —
// which is most writes — leaves every stamp exactly as it was.
func TestAWriteThatIgnoresSeenLeavesEveryStampAlone(t *testing.T) {
	current := []byte(`{"tabs":[{"id":"bb1","name":"Plan","type":"dag","state":{},
		"seen":{"human":"2026-08-01T00:00:00.000Z","agent-2":"2026-08-02T00:00:00.000Z"}}]}`)
	incoming := []byte(`{"tabs":[{"id":"bb1","name":"Plan","type":"dag","state":{"nodes":[]}}]}`)

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Seen["human"] != "2026-08-01T00:00:00.000Z" || out[0].Seen["agent-2"] != "2026-08-02T00:00:00.000Z" {
		t.Errorf("a write that never touched seen changed it: %v", out[0].Seen)
	}
}

// And a tab that genuinely has no stamps still writes none, rather than an empty
// object nobody asked for.
func TestATabWithNoSeenStaysWithoutOne(t *testing.T) {
	current := []byte(`{"tabs":[{"id":"bb1","name":"Plan","type":"dag","state":{}}]}`)
	incoming := []byte(`{"tabs":[{"id":"bb1","name":"Plan","type":"dag","state":{"nodes":[]}}]}`)

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(out[0].Seen) != 0 {
		t.Errorf("an empty seen map was invented: %v", out[0].Seen)
	}
}
