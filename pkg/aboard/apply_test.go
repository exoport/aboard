package aboard

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// applyTarget stands a board up far enough for Apply to find and post to it, and
// hands back whatever the last write submitted.
type submitted struct{ doc map[string]any }

func applyTarget(t *testing.T) (Root, *submitted) {
	t.Helper()
	dir := t.TempDir()
	root := Root(dir)
	if err := os.MkdirAll(root.RunDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	last := &submitted{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		last.doc = map[string]any{}
		_ = json.Unmarshal(body, &last.doc)
		(&server{}).writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rev": 9})
	}))
	t.Cleanup(srv.Close)

	rec, err := json.Marshal(Instance{App: HostStandalone, URL: srv.URL, Project: root.String()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.InstanceFile(""), rec, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, last
}

func runApply(t *testing.T, root Root, force bool, doc string) (out, errOut string, err error) {
	t.Helper()
	return runApplyWith(t, root, ApplyOptions{By: "agent-1", Force: force}, doc)
}

func runApplyWith(t *testing.T, root Root, options ApplyOptions, doc string) (out, errOut string, err error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err = Apply(t.Context(), root, "", options, web.FS,
		strings.NewReader(doc), &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// `__base` was set only when the document happened to carry a timestamp, and the
// server skipped the check when it was empty — so a write built from the minimal
// shape the docs show overwrote everything since the last read, exit 0, nothing
// on stderr. The whole compare-and-set story was one absent field away from off.
func TestApplyRefusesADocumentWithNoBase(t *testing.T) {
	root, last := applyTarget(t)

	out, errOut, err := runApply(t, root, false, `{"version":3,"nextId":2,"tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}}]}`)
	if err == nil {
		t.Fatalf("a document with no base was applied: %s %s", out, errOut)
	}
	if !errors.Is(err, ErrNoBase) {
		t.Errorf("the refusal is not the typed one, so the cli cannot map it to exit 2: %v", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("the refusal does not name the way to say you meant it: %v", err)
	}
	if len(last.doc) != 0 {
		t.Errorf("the board was written to anyway: %v", last.doc)
	}
}

// --force is the deliberate version of the same write, and it says so where the
// person who typed it will see it.
func TestApplyForceWritesWithoutCompareAndSetAndSaysSo(t *testing.T) {
	root, last := applyTarget(t)

	_, errOut, err := runApply(t, root, true, `{"version":3,"nextId":2,"tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}}]}`)
	if err != nil {
		t.Fatalf("--force was refused: %v (%s)", err, errOut)
	}
	if !strings.Contains(errOut, "--force") || !strings.Contains(errOut, "without compare-and-set") {
		t.Errorf("--force wrote silently; stderr was:\n%s", errOut)
	}
	if _, sent := last.doc["__base"]; sent {
		t.Errorf("--force still sent a base: %v", last.doc["__base"])
	}
}

// The base is the REVISION of the document that was read.
func TestApplySendsTheRevisionAsItsBase(t *testing.T) {
	root, last := applyTarget(t)

	if _, _, err := runApply(t, root, false, `{"version":3,"rev":12,"nextId":2,"updatedAt":"T-old","tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}}]}`); err != nil {
		t.Fatal(err)
	}
	if got := last.doc["__base"]; got != "12" {
		t.Errorf("__base = %v, want the document's rev 12", got)
	}
}

// A document read from a board that has not been written since the counter
// landed has no `rev` to send, and the timestamp is the only base its reader
// could have. Sent, and accepted by the server for exactly that one write.
func TestApplyFallsBackToTheTimestampWhenThereIsNoRev(t *testing.T) {
	root, last := applyTarget(t)

	if _, _, err := runApply(t, root, false, `{"version":3,"nextId":2,"updatedAt":"T-old","tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}}]}`); err != nil {
		t.Fatal(err)
	}
	if got := last.doc["__base"]; got != "T-old" {
		t.Errorf("__base = %v, want the document's updatedAt", got)
	}
}
