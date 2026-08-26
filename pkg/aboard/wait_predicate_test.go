package aboard

import (
	"strings"
	"testing"
)

// The predicate vocabulary is a closed set on purpose: every form here can fail
// LOUDLY at request time, which a grammar cannot. That property is the whole
// argument against growing this into an expression language, and nothing in Go
// asserted it — the only coverage was one curl in the shell suite checking that
// an unknown predicate returns 400.
func TestParsePredicateAcceptsTheWholeVocabulary(t *testing.T) {
	cases := []struct {
		raw  string
		want predicate
	}{
		{"", predicate{kind: eventPoke}},
		{"   ", predicate{kind: eventPoke}},
		{"poke", predicate{kind: eventPoke}},
		{"change", predicate{kind: "change"}},
		{"tab bb71", predicate{kind: "tab", id: "bb71"}},
		{"answer bb128", predicate{kind: "answer", id: "bb128"}},
		{"node bb58=done", predicate{kind: "node", id: "bb58", value: "done"}},
		// Whitespace is the caller's shell, not their meaning.
		{"  tab   bb71  ", predicate{kind: "tab", id: "bb71"}},
		// A status containing '=' keeps the whole tail: SplitN(…, 2).
		{"node bb58=a=b", predicate{kind: "node", id: "bb58", value: "a=b"}},
		// An empty status is a legal ask ("has no status yet"), not a parse error.
		{"node bb58=", predicate{kind: "node", id: "bb58", value: ""}},
	}
	for _, tc := range cases {
		got, err := parsePredicate(tc.raw)
		if err != nil {
			t.Errorf("parsePredicate(%q): %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parsePredicate(%q) = %+v, want %+v", tc.raw, got, tc.want)
		}
	}
}

// Refused UP FRONT, and the message names the vocabulary — because the
// alternative is a session that blocks for ten minutes on something that will
// never fire and then reports a timeout, which reads as "nobody came" rather
// than "you asked for something that does not exist".
func TestParsePredicateRefusesEverythingElse(t *testing.T) {
	cases := []struct {
		raw      string
		mentions string
	}{
		{"form 15 answered", "unknown predicate"},
		{"answered bb15", "unknown predicate"},
		{"TAB bb71", "unknown predicate"}, // the vocabulary is lower-case
		{"poke bb1", "takes no argument"},
		{"change now", "takes no argument"},
		{"tab", "needs one id"},
		{"tab bb1 bb2", "needs one id"},
		{"answer", "needs one id"},
		{"node", "id=status"},
		{"node bb58", "id=status"},
		{"node bb58 done", "id=status"},
	}
	for _, tc := range cases {
		_, err := parsePredicate(tc.raw)
		if err == nil {
			t.Errorf("parsePredicate(%q) was accepted; it can never fire", tc.raw)
			continue
		}
		if !strings.Contains(err.Error(), tc.mentions) {
			t.Errorf("parsePredicate(%q) error %q does not mention %q", tc.raw, err, tc.mentions)
		}
	}
}

// What each predicate means when a write lands. `poke` is the one that returns
// false for every write: only an explicit poke releases those, and a predicate
// that fired on any change would make the notify button meaningless.
func TestPredicateMatching(t *testing.T) {
	doc := []byte(`{"tabs":[
		{"id":"bb1","type":"dag","state":{"nodes":[{"id":"bb58","status":"done"},{"id":"bb59","status":"todo"}]}},
		{"id":"bb32","type":"stack","state":{"blocks":[{"id":"bb33","type":"kanban","state":{"nodes":[{"id":"bb90","status":"working"}]}}]}}
	]}`)

	agentWrite := JournalEntry{By: "agent-1", Tabs: []string{"bb1"}}
	humanWrite := JournalEntry{By: actorHuman, Tabs: []string{"bb128"}}
	noTabs := JournalEntry{By: "agent-1"}

	cases := []struct {
		raw   string
		entry JournalEntry
		want  bool
		why   string
	}{
		{"poke", agentWrite, false, "only an explicit poke releases a poke waiter"},
		{"poke", humanWrite, false, "not even a human write"},
		{"change", agentWrite, true, "any accepted write with a tab in it"},
		{"change", noTabs, false, "a write that touched no tab is not a change"},
		{"tab bb1", agentWrite, true, "that tab changed"},
		{"tab bb2", agentWrite, false, "a different tab changed"},
		{"answer bb128", humanWrite, true, "a human answered it"},
		{
			"answer bb128",
			JournalEntry{By: "agent-1", Tabs: []string{"bb128"}},
			false,
			"an agent rewriting the tab is not an answer to anything",
		},
		{"answer bb1", humanWrite, false, "the human answered a different tab"},
		{"node bb58=done", agentWrite, true, "the node reached that status"},
		{"node bb59=done", agentWrite, false, "that node is still todo"},
		{"node bb58=todo", agentWrite, false, "the wrong status"},
		{"node bb90=working", agentWrite, true, "a node inside a stack block counts — a waiter should not have to say where it lives"},
		{"node bb999=done", agentWrite, false, "no such node"},
	}

	for _, tc := range cases {
		p, err := parsePredicate(tc.raw)
		if err != nil {
			t.Fatalf("parsePredicate(%q): %v", tc.raw, err)
		}
		if got := p.matches(doc, tc.entry); got != tc.want {
			t.Errorf("%q against %+v = %v, want %v — %s", tc.raw, tc.entry, got, tc.want, tc.why)
		}
	}
}

// The declared `--for` help string is what a user copies, so every form it names
// must parse. A vocabulary that drifts from its own help is the failure this
// whole file exists to prevent.
func TestTheDeclaredForFlagListsOnlyFormsThatParse(t *testing.T) {
	var doc string
	for _, c := range Commands() {
		if c.Name != "wait" {
			continue
		}
		for _, f := range c.Flags {
			if f.Name == "for" {
				doc = f.Doc
			}
		}
	}
	if doc == "" {
		t.Fatal("the wait command does not declare a --for flag")
	}
	for _, form := range []string{"poke", "change", "tab <id>", "answer <id>", "node <id>=<status>"} {
		if !strings.Contains(doc, form) {
			t.Errorf("--for's help does not mention %q: %s", form, doc)
		}
		// The placeholder forms, with a real id substituted.
		concrete := strings.NewReplacer("<id>", "bb71", "<status>", "done").Replace(form)
		if _, err := parsePredicate(concrete); err != nil {
			t.Errorf("--for advertises %q but %q does not parse: %v", form, concrete, err)
		}
	}
}
