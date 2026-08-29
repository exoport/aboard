package aboard

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

func TestInvocationDerivesFromArgv0(t *testing.T) {
	for _, tc := range []struct {
		name, argv0, want string
	}{
		{"unset falls back to the app name", "", AppName},
		{"a bare name is itself", "aboard", "aboard"},
		// os.Args[0] is whatever the shell resolved, and nobody types a path.
		{"a resolved path is reduced", "/usr/local/bin/aboard", "aboard"},
		{"a relative path is reduced", "./aboard", "aboard"},
		// A host passes the command the user typed. It has no separator, so
		// Base leaves it alone — which is the case the whole type exists for.
		{"a host's two words survive", "ape aboard", "ape aboard"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Options{Argv0: tc.argv0}.Invocation().String()
			if got != tc.want {
				t.Errorf("Invocation() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The zero value has to be usable: a function that was never given one still
// prints something a standalone reader can type, which is the safer of the two
// wrong answers.
func TestZeroInvocationIsStandalone(t *testing.T) {
	var i Invocation
	if got := i.String(); got != AppName {
		t.Errorf("zero Invocation = %q, want %q", got, AppName)
	}
	if got := i.Cmd("init"); got != "aboard init" {
		t.Errorf("zero Invocation.Cmd = %q, want %q", got, "aboard init")
	}
	if got := DefaultInvocation.Cmd(""); got != AppName {
		t.Errorf("Cmd(\"\") = %q, want %q", got, AppName)
	}
}

// A hosted board must name the command the reader has.
func TestHostedMessagesNameTheHostedCommand(t *testing.T) {
	hosted := Options{Host: HostApe, Argv0: "ape aboard"}.Invocation()

	if _, err := Uploads(Root(t.TempDir()), hosted); err == nil {
		t.Fatal("a directory with no board document must be an error")
	} else if !strings.Contains(err.Error(), "`ape aboard init`") {
		t.Errorf("hosted message = %q, want it to name `ape aboard init`", err)
	}

	if _, err := RunningInstance(Root(t.TempDir()), "", hosted); err == nil {
		t.Fatal("no instance file must be an error")
	} else if !strings.Contains(err.Error(), "`ape aboard serve`") {
		t.Errorf("hosted message = %q, want it to name `ape aboard serve`", err)
	}
}

// The mirror: standalone output must be unchanged by all of this.
func TestStandaloneMessagesAreUnchanged(t *testing.T) {
	if _, err := Uploads(Root(t.TempDir()), DefaultInvocation); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "`aboard init`") {
		t.Errorf("standalone message = %q, want it to name `aboard init`", err)
	}
}

// buildManifest takes no Invocation, and that is the design: capsHash is
// computed over the whole manifest, Commands included, and the exit meaning
// for `boards` on a non-Linux platform names a command. A host-aware string in
// there would give two hosts two hashes, and an agent reading the manifest
// could tell which one it reached — the one thing
// docs/explanation/why-two-identities.md says must never be true.
//
// The end-to-end version of this assertion, over the rendered output of every
// format, is TestCapabilitiesAreHostIndependent in the cli package.
func TestTheDeclaredTableStillNamesTheBoardsAlternative(t *testing.T) {
	m, err := buildManifest(web.FS)
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}
	var found bool
	for _, c := range m.Commands {
		if c.Name != "boards" {
			continue
		}
		for _, e := range c.Exits {
			if strings.Contains(e.Meaning, "`aboard status`") {
				found = true
			}
		}
	}
	if !found {
		t.Error("the declared `boards` exit meaning must keep naming `aboard status` verbatim: " +
			"it is hashed into capsHash, which both hosts must report identically")
	}
}

// invocationAllowList names every file permitted to hardcode `aboard <cmd>`,
// with the reason. Two kinds, and both are about a string OUTLIVING the
// invocation that produced it:
//
//   - a GENERATED, committed artifact must not depend on which host ran the
//     generator, or `make caps` produces a different file for two developers;
//   - the DECLARED manifest feeds capsHash, which must be identical between
//     hosts by design.
//
// init.go is the third case and the same reasoning: recipesReadme is WRITTEN
// TO DISK into a project that either host may drive afterwards, so it cannot
// be right for both readers and stays in the board's own words.
var invocationAllowList = map[string]string{
	"pkg/aboard/caps.go":       "generated controls module + skill reference headers",
	"pkg/aboard/commands.go":   "the declared command table — feeds capsHash",
	"pkg/aboard/recipes.go":    "RecipeIndexMarkdown, a generated committed file",
	"pkg/aboard/init.go":       "recipesReadme, written into .aboard/recipes/",
	"pkg/aboard/aboard.go":     "the Invocation doc comment's own example",
	"pkg/aboard/cli/boards.go": "prose naming BOTH hosts deliberately",
}

// TestNoNewHardcodedInvocations is the guard that keeps this fix from being
// undone one string at a time.
//
// 55 hardcoded `aboard <cmd>` occurrences accumulated before anybody measured
// them, because each one reads perfectly right in the file it is in — the
// defect is only visible from a host that did not exist yet. A new one would
// be just as invisible, so the tree is checked rather than trusted.
func TestNoNewHardcodedInvocations(t *testing.T) {
	verbs := "serve|status|init|apply|requests|capabilities|journal|history|wait|poke|boards|uploads|rendered|recipes|log|export|watch|version"
	// Inside a Go string literal, not preceded by "ape " (which is prose about
	// the host) and not part of `.aboard/` or `aboard.json`.
	re := regexp.MustCompile(`"[^"]*(^|[^.\w])aboard (` + verbs + `)`)

	root := filepath.Join("..", "..")
	var offenders []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil //nolint:nilerr // an unreadable file is not a verdict
		}
		// Production source only. A test legitimately asserts on STANDALONE
		// output, which is the string this guard is looking for.
		if strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, root+string(filepath.Separator)))
		if _, ok := invocationAllowList[rel]; ok {
			return nil
		}
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil //nolint:nilerr // ditto
		}
		for i, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, "ape aboard") {
				continue
			}
			if re.MatchString(line) {
				offenders = append(offenders, rel+":"+itoa(i+1)+"  "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("hardcoded `aboard <cmd>` in %d place(s) — pass an Invocation instead, "+
			"or add the file to invocationAllowList with the reason:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
