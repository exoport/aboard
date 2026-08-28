//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/exoport/aboard/pkg/aboard"
)

// The change banner prints a command, and a command in output is a claim. On a
// NAMED board that line has to carry `--name` on both halves: the left one reads
// that board's journal and the right one writes whichever board it was told
// about, so a half-qualified pipeline takes the right document and puts it on
// the wrong board.
//
// The terminal listing gained this (history.go's nameFlag) while the shell — the
// only other place the same pipeline is printed, and the one the human is
// actually looking at when a banner appears — went on printing the unqualified
// form. `GET /history` carries `board` for exactly this consumer; a fact the
// endpoint states and the page ignores is the two halves disagreeing.
//
// A board of its own, because the suite's shared one is the DEFAULT board and
// the whole assertion is about a name.
func TestTheChangeBannersRestoreLineNamesTheBoard(t *testing.T) {
	url := namedBoard(t, "review")

	// Two writes: the one that creates a tab has no previous state, so the
	// journal would hold nothing to offer back.
	pushNamed(t, url, `{"id":"ab1","name":"Side note","type":"notes","state":{"text":"the first thing it said"}}`)
	pushNamed(t, url, `{"id":"ab1","name":"Side note","type":"notes","state":{"text":"the second thing it said"}}`)

	s := openAt(t, url, "tab=ab1")
	s.tab("ab1")

	banner := s.view("ab1").Locator(".banner").First()
	if err := expect.Locator(banner).ToContainText("changed this tab"); err != nil {
		t.Fatalf("no change banner on a tab an agent just wrote to: %v", err)
	}
	link := s.view("ab1").Locator(`button:has-text("What it said before")`)
	if err := link.Click(); err != nil {
		t.Fatalf("pressing the history link: %v", err)
	}
	prev := s.view("ab1").Locator(".history-prev")
	if err := expect.Locator(prev).ToBeVisible(); err != nil {
		t.Fatalf("the previous state never appeared: %v", err)
	}
	want := "aboard history ab1 --at 1 --name review | aboard apply --name review --by agent-1"
	if err := expect.Locator(prev).ToContainText(want); err != nil {
		t.Errorf("the panel's restore line does not name the board: %v", err)
	}
}

// namedBoard seeds an empty board called `name` in its own root, serves it, and
// returns its URL. Its own root as well as its own name: a second server for the
// same (project, name) is refused now, and sharing the suite's root would make
// this test's board a duplicate of nothing in particular.
func namedBoard(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := aboard.Init(aboard.InitConfig{Dir: dir, Name: name}); err != nil {
		t.Fatalf("seeding the %q board: %v", name, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() {
		served <- aboard.Serve(ctx, aboard.Options{
			Host: aboard.HostStandalone, Argv0: "aboard", Logger: log.New(io.Discard, "", 0),
		}, aboard.ServeConfig{Root: aboard.Root(dir), Name: name, Port: port})
	}()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-served:
			t.Fatalf("the %q board stopped: %v", name, err)
		default:
		}
		if aboard.ProbeBoard(ctx, port, "") != nil {
			return "http://127.0.0.1:" + strconv.Itoa(port)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the %q board never answered /health", name)
	return ""
}

// pushNamed writes one tab to a board of its own, as an AGENT: the change banner
// under test only appears for a write the human did not make.
func pushNamed(t *testing.T, base, tab string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/aboard.json", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var got doc
	err = json.NewDecoder(res.Body).Decode(&got)
	_ = res.Body.Close()
	if err != nil {
		t.Fatalf("decoding /aboard.json: %v", err)
	}

	body, err := json.Marshal(doc{
		"version":  got["version"],
		"nextId":   2,
		"tabs":     []any{json.RawMessage(tab)},
		"__base":   revisionToken(t, got["rev"]),
		"__by":     agentActor,
		"__origin": "e2e-" + agentActor,
	})
	if err != nil {
		t.Fatal(err)
	}
	post, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/aboard.json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	post.Header.Set("Content-Type", "application/json")
	out, err := httpClient.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	if out.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(out.Body)
		t.Fatalf("writing to %s: status %d: %s", base, out.StatusCode, strings.TrimSpace(string(msg)))
	}
}
