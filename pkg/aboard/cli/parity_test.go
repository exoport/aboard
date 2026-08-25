package cli

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The declared table (aboard/commands.go) feeds the capability manifest, and the
// cobra tree is what a user actually gets. Two things that can disagree, with a
// test that fails when they do — which is the whole reason the table is hand
// written rather than scraped: under cobra there is no global registry to walk,
// and scraping whatever happened to be registered made capsHash depend on which
// subcommand printed it.
//
// `help` is excluded: cobra creates it, nobody declared it, and it is not part
// of the board's surface. `completion` is not excluded — it is switched off in
// NewRootCmd, and if that ever regresses this test says so.
//
// HIDDEN commands are excluded too, and this is the decision made rather than
// inherited: `gen-docs` and `recipes index` maintain THIS repository, they are
// not things a user of a board does, and the manifest describes the user-facing
// surface. Declaring them would put a maintainer command in every agent's
// capability listing and move capsHash whenever one was added. The rule is only
// safe because hiding is asserted separately (TestHiddenCommandsAreDeliberate):
// otherwise a command could disappear from the surface by accident and this test
// would approve of it.
func TestCommandTableMatchesCobraTree(t *testing.T) {
	root := NewRootCmd(Options{Host: aboard.HostStandalone})

	declared := map[string]aboard.Command{}
	for _, c := range aboard.Commands() {
		declared[c.Name] = c
	}

	seen := map[string]bool{}
	for _, cmd := range root.Commands() {
		name := cmd.Name()
		if name == "help" || cmd.Hidden {
			continue
		}
		seen[name] = true
		want, ok := declared[name]
		if !ok {
			t.Errorf("cobra has command %q that commands.go does not declare", name)
			continue
		}
		assertFlagsMatch(t, name, want.Flags, cmd.Flags())
	}
	for name := range declared {
		if !seen[name] {
			t.Errorf("commands.go declares %q but the cobra tree has no such command", name)
		}
	}
}

// The exact set of hidden commands, asserted both ways. A command that becomes
// hidden by accident vanishes from the manifest, from `--help` and from the
// generated CLI reference all at once — and the parity test above would report
// nothing, because it now skips them. So the list is written down here.
func TestHiddenCommandsAreDeliberate(t *testing.T) {
	want := map[string]bool{"gen-docs": true, "recipes index": true}

	got := map[string]bool{}
	var walk func(c *cobra.Command, prefix string)
	walk = func(c *cobra.Command, prefix string) {
		for _, sub := range c.Commands() {
			name := strings.TrimSpace(prefix + " " + sub.Name())
			if sub.Hidden {
				got[name] = true
			}
			walk(sub, name)
		}
	}
	walk(NewRootCmd(Options{Host: aboard.HostStandalone}), "")

	for name := range want {
		if !got[name] {
			t.Errorf("%q is meant to be hidden and is not", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("%q is hidden but nothing says it should be — either declare it in commands.go or add it here", name)
		}
	}
}

// A hidden command is still a command: it must run. `make caps` and
// `make docs-cli` call these two, so a typo in one is a broken build target that
// no other test would notice.
func TestHiddenCommandsResolve(t *testing.T) {
	root := NewRootCmd(Options{Host: aboard.HostStandalone})
	for _, args := range [][]string{{"gen-docs"}, {"recipes", "index"}} {
		cmd, _, err := root.Find(args)
		if err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		if cmd.RunE == nil {
			t.Errorf("%v resolved to %q, which has no RunE", args, cmd.CommandPath())
		}
	}
}

func TestRootFlagsMatchCobraTree(t *testing.T) {
	root := NewRootCmd(Options{Host: aboard.HostStandalone})
	assertFlagsMatch(t, "aboard", aboard.RootFlags(), root.PersistentFlags())
}

// A declared command must also state at least the success and failure codes, or
// the manifest documents a command whose outcome nobody wrote down.
func TestEveryCommandDeclaresExitCodes(t *testing.T) {
	for _, c := range aboard.Commands() {
		if len(c.Exits) == 0 {
			t.Errorf("%q declares no exit codes", c.Name)
			continue
		}
		codes := map[int]bool{}
		for _, e := range c.Exits {
			if e.Meaning == "" {
				t.Errorf("%q: exit %d has no meaning", c.Name, e.Code)
			}
			codes[e.Code] = true
		}
		if !codes[aboard.ExitOK] {
			t.Errorf("%q does not say what exit 0 means", c.Name)
		}
	}
}

// Argument spelling: the table's Args string is what the manifest and the skill
// reference show, so it has to match the cobra Use line the user sees.
func TestDeclaredArgsMatchUse(t *testing.T) {
	root := NewRootCmd(Options{Host: aboard.HostStandalone})
	byName := map[string]*cobra.Command{}
	for _, c := range root.Commands() {
		byName[c.Name()] = c
	}
	for _, c := range aboard.Commands() {
		cmd, ok := byName[c.Name]
		if !ok {
			continue // reported by the parity test above
		}
		want := c.Name
		if c.Args != "" {
			want += " " + c.Args
		}
		if cmd.Use != want {
			t.Errorf("%q: cobra Use is %q, table says %q", c.Name, cmd.Use, want)
		}
	}
}

func assertFlagsMatch(t *testing.T, where string, declared []aboard.Flag, set *pflag.FlagSet) {
	t.Helper()

	want := map[string]aboard.Flag{}
	for _, f := range declared {
		want[f.Name] = f
	}

	got := map[string]aboard.Flag{}
	set.VisitAll(func(f *pflag.Flag) {
		got[f.Name] = aboard.Flag{Name: f.Name, Type: f.Value.Type(), Def: f.DefValue, Doc: f.Usage}
	})

	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("%s: commands.go declares --%s but cobra has no such flag", where, name)
			continue
		}
		if g.Type != w.Type {
			t.Errorf("%s --%s: cobra type %q, table says %q", where, name, g.Type, w.Type)
		}
		// An empty declared default means "no default"; pflag spells that "".
		if g.Def != w.Def {
			t.Errorf("%s --%s: cobra default %q, table says %q", where, name, g.Def, w.Def)
		}
		if g.Doc != w.Doc {
			t.Errorf("%s --%s: cobra help and the table disagree\n  cobra: %s\n  table: %s", where, name, g.Doc, w.Doc)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s: cobra has --%s, which commands.go does not declare", where, name)
		}
	}
}

// The declared table is what a human reads top to bottom in --help and in the
// generated reference, so its order is part of the surface. This does not
// enforce an order; it fails if two commands share a name, which would make the
// map-based comparisons above silently skip one.
func TestCommandNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	var dupes []string
	for _, c := range aboard.Commands() {
		if seen[c.Name] {
			dupes = append(dupes, c.Name)
		}
		seen[c.Name] = true
	}
	sort.Strings(dupes)
	if len(dupes) > 0 {
		t.Fatalf("duplicate command names: %s", strings.Join(dupes, ", "))
	}
}

// Embeddability, asserted rather than assumed: a host mounts this tree by value,
// so building it twice must produce two independent trees. Package-level cobra
// vars would make the second AddCommand attach to the first tree's children.
func TestTreeCanBeBuiltTwice(t *testing.T) {
	a := NewRootCmd(Options{Host: aboard.HostStandalone})
	b := NewRootCmd(Options{Host: aboard.HostApe})
	if a == b {
		t.Fatal("NewRootCmd returned the same command twice")
	}
	if len(a.Commands()) != len(b.Commands()) {
		t.Fatalf("two trees have different command counts: %d and %d", len(a.Commands()), len(b.Commands()))
	}
	if err := fmt.Errorf("%v", a.Commands()[0] == b.Commands()[0]); err.Error() == "true" {
		t.Fatal("two trees share a subcommand instance")
	}
}
