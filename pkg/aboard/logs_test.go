package aboard

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sidecar log endpoint is the one place a value out of a URL becomes a
// FILENAME, and the refusal that stops it lives one function away in layout.go.
// That is the right place for it — layout.go is the only file allowed to join a
// path — but it left the ENDPOINT itself untested: with the validation moved,
// reverting Root.LogFile to a bare join made `POST /log?tab=../../etc/passwd`
// write happily outside the logs directory and the only red test was a unit test
// on a path builder. A guard is worth what its caller does with it, so this
// test asks the question at the door: the request is refused with the sentence
// the API documents, and nothing lands anywhere but LogsDir.
func TestTheLogEndpointRefusesATabIDThatCannotBeAFilename(t *testing.T) {
	srv := testServer(t, `{"version":3,"rev":1,"nextId":1,"tabs":[]}`)

	bad := []string{
		"",
		"..",
		"../../etc/passwd",
		"../evil",
		"a/b",
		"bb126.log",
		"bb 126",
		strings.Repeat("b", 65),
	}
	for _, tab := range bad {
		for _, method := range []string{http.MethodPost, http.MethodGet} {
			rec := httptest.NewRecorder()
			target := "http://localhost/log?tab=" + url.QueryEscape(tab)
			req := httptest.NewRequest(method, target, strings.NewReader("a line\n"))
			srv.route(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s /log?tab=%q = %d, want 400", method, tab, rec.Code)
				continue
			}
			var reply map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
				t.Errorf("%s /log?tab=%q: body is not json: %v", method, tab, err)
				continue
			}
			// Spelled out rather than compared to msgTabPlainID: this is the
			// sentence docs/reference/http-api.md promises a caller, and a
			// constant asserted against itself passes however it changes.
			if reply["error"] != "tab must be a plain id" {
				t.Errorf("%s /log?tab=%q: error = %q, want %q", method, tab, reply["error"], "tab must be a plain id")
			}
		}
	}

	// A plain id still works, or the refusal above would be satisfied by an
	// endpoint that refuses everything.
	rec := httptest.NewRecorder()
	srv.route(rec, httptest.NewRequest(http.MethodPost, "http://localhost/log?tab=bb126", strings.NewReader("a line\n")))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /log?tab=bb126 = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	logFile, ok := srv.root.LogFile("", "bb126")
	if !ok {
		t.Fatal(`LogFile("bb126") refused a plain tab id`)
	}
	if _, err := os.Stat(logFile); err != nil {
		t.Fatalf("the accepted write did not land in %s: %v", logFile, err)
	}

	// And nothing at all was written outside the logs directory. Walked rather
	// than stat-ed for each bad id, because the interesting failure is a file
	// somewhere nobody thought to look.
	logsDir := srv.root.LogsDir("")
	err := filepath.WalkDir(string(srv.root), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".log") {
			return nil
		}
		if filepath.Dir(path) != logsDir {
			t.Errorf("a log file landed outside the logs directory: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the project: %v", err)
	}
}
