package aboard

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Rotation exists to bound the file, not to make history vanish. `tail` opened
// journal.jsonl and nothing else, so the kept generation was unreachable the
// moment it was created: `aboard journal --limit 40` on a board that had just
// rotated showed whatever had been written since, and the other entries sat
// readable on disk with nothing willing to open them.
func TestTailReadsTheRotatedGenerationOldestFirst(t *testing.T) {
	root := Root(t.TempDir())
	j := newJournal(root)
	if err := os.MkdirAll(root.RunDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	// Three writes, then a rotation, then two more — the shape a real board
	// reaches at the size cap.
	for i := 1; i <= 3; i++ {
		j.append(JournalEntry{At: fmt.Sprintf("t%d", i), By: "agent-1", Tabs: []string{"bb1"}})
	}
	j.mu.Lock()
	j.rotateLocked()
	j.mu.Unlock()
	for i := 4; i <= 5; i++ {
		j.append(JournalEntry{At: fmt.Sprintf("t%d", i), By: "human", Tabs: []string{"bb1"}})
	}

	if _, err := os.Stat(root.JournalFile() + ".1"); err != nil {
		t.Fatalf("the rotated generation is not where this test thinks it is: %v", err)
	}

	if got := atsOf(t, j.tail(0)); got != "t1 t2 t3 t4 t5" {
		t.Errorf("tail(0) = %q, want every entry across both generations, oldest first", got)
	}
	// The trim still counts across the concatenation rather than per file.
	if got := atsOf(t, j.tail(4)); got != "t2 t3 t4 t5" {
		t.Errorf("tail(4) = %q, want the last four across both generations", got)
	}
	if got := atsOf(t, j.tail(2)); got != "t4 t5" {
		t.Errorf("tail(2) = %q", got)
	}
}

// The normal case for the whole life of most boards: nothing has ever rotated,
// so journal.jsonl.1 does not exist and must not be an error.
func TestTailWithNoRotatedGeneration(t *testing.T) {
	root := Root(t.TempDir())
	j := newJournal(root)
	if err := os.MkdirAll(root.RunDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	j.append(JournalEntry{At: "t1", By: "agent-1"})
	if got := atsOf(t, j.tail(0)); got != "t1" {
		t.Errorf("tail = %q, want t1", got)
	}
}

// And the CLI-facing read goes through the same tail, so `aboard journal` on a
// dead board sees the rotated history too.
func TestJournalFromDiskSpansGenerations(t *testing.T) {
	root := Root(t.TempDir())
	j := newJournal(root)
	if err := os.MkdirAll(root.RunDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	j.append(JournalEntry{At: "t1", By: "agent-1", Tabs: []string{"bb1"}})
	j.mu.Lock()
	j.rotateLocked()
	j.mu.Unlock()
	j.append(JournalEntry{At: "t2", By: "human", Tabs: []string{"bb1"}})

	entries, err := journalFromDisk(root, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].At != "t1" || entries[1].At != "t2" {
		t.Errorf("journalFromDisk = %+v, want t1 then t2", entries)
	}
}

func atsOf(t *testing.T, raw []json.RawMessage) string {
	t.Helper()
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		var e JournalEntry
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("unreadable journal line %s: %v", line, err)
		}
		out = append(out, e.At)
	}
	return strings.Join(out, " ")
}
