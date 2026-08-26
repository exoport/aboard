// logs.go — output that a human can watch, kept OUT of aboard.json.
//
//	POST /log?tab=<id>          append what an agent piped in
//	GET  /log?tab=<id>&tail=n   the last n lines
//	aboard log <tabId>           read stdin and append it, line by line
//
// The design constraint came from the human, and it was right: the board document
// is rewritten whole on every write, so an appending log
// inside a tab's state would mean the entire document rewritten per line and a
// noisy diff in every commit. So the stream lives in a sidecar file and the tab's
// state holds only a pointer.
//
// Bounded by construction: a per-file size cap with one rotation, and a tail-only
// read API. A log nobody prunes is the same bug in a different place.

package aboard

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	logMaxBytes  = 8 << 20
	logTailLines = 500
)

var logMu sync.Mutex

// A PATH-TRAVERSAL NOTE, for the //nolint:gosec below.
//
// `path` here is whatever Root.LogFile returned, and LogFile refuses any tab id
// that is not `^[A-Za-z0-9_-]{1,64}$` before it joins anything — no separator, no
// dot, so no ".." and no absolute path — and then applies filepath.Base to the
// filename it builds anyway. TestLogFileRefusesAnIDThatCannotBeAFilename asserts
// both halves, including that every accepted id lands in the logs directory.
//
// gosec still reports G703 on every use of the result, and the reason is a real
// limitation rather than a disagreement about the code: its taint analysis only
// consults its sanitizer list inside the function holding the sink
// (taint.isTainted). Across a call boundary it uses valueReachableFromParams,
// which asks "is this return value derived from a tainted argument" and never
// looks at sanitizers at all — so filepath.Base is invisible from here no matter
// how the guard is written, unless the join moves into this file. It does not:
// paths are built in layout.go and nowhere else, which is the rule that made the
// guard checkable in the first place.
//
// The alternative rejected: applying filepath.Base to the tab id at the CALL
// site, which does silence the analyser and is a bug — Base("../../bb42") is
// "bb42", so a traversal attempt would stop being refused and start quietly
// succeeding against a different tab.
func (s *server) logPath(tab string) (string, bool) { return s.root.LogFile(s.name, tab) }

func (s *server) handleLogPost(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	path, ok := s.logPath(tab)
	if !ok {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{wireError: msgTabPlainID})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		s.writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{wireError: "chunk too large"})
		return
	}
	if len(body) == 0 {
		s.writeJSON(w, http.StatusOK, map[string]any{wireOK: true, wireBytes: 0})
		return
	}

	logMu.Lock()
	defer logMu.Unlock()

	if err := os.MkdirAll(s.root.LogsDir(s.name), 0o755); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{wireError: "cannot create the log directory"})
		return
	}
	if info, err := os.Stat(path); err == nil && info.Size()+int64(len(body)) > logMaxBytes { //nolint:gosec // see the path-traversal note above
		_ = os.Rename(path, path+".1") //nolint:gosec // see the path-traversal note above
	}
	//nolint:gosec // path: see the path-traversal note above; 0o644: the board's repo-wide file-mode policy, see the note in init.go
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{wireError: "cannot open the log"})
		return
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(body); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{wireError: "cannot append"})
		return
	}
	if body[len(body)-1] != '\n' {
		_, _ = f.Write([]byte{'\n'})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{wireOK: true, wireBytes: len(body)})
}

func (s *server) handleLogGet(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	path, ok := s.logPath(tab)
	if !ok {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{wireError: msgTabPlainID})
		return
	}
	tail := logTailLines
	if raw := r.URL.Query().Get("tail"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 5000 {
			tail = n
		}
	}

	f, err := os.Open(path) //nolint:gosec // see the path-traversal note above
	if err != nil {
		s.writeJSON(w, http.StatusOK, map[string]any{"lines": []string{}, "size": 0, "missing": true})
		return
	}
	defer func() { _ = f.Close() }()

	lines := make([]string, 0, tail)
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scan.Scan() {
		lines = append(lines, scan.Text())
		if len(lines) > tail {
			lines = lines[1:]
		}
	}
	size := int64(0)
	if info, err := f.Stat(); err == nil {
		size = info.Size()
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"lines": lines, "size": size})
}

// Log streams stdin into a tab's log, a line at a time, so a long-running
// command shows up on the board as it happens rather than when it finishes:
//
//	go test ./... 2>&1 | aboard log bb42
func Log(ctx context.Context, root Root, name, tab string, in io.Reader, out io.Writer) error {
	inst, err := RunningInstance(root, name)
	if err != nil {
		return err
	}
	if !validTabFileID(tab) {
		return fmt.Errorf("%q is not a plain tab id", tab)
	}
	url := inst.URL + "/log?tab=" + tab

	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 0, 64<<10), 1<<20)
	client := &http.Client{}
	for scan.Scan() {
		line := scan.Text()
		// Echo it too: piping output to the board should not mean losing it from
		// the terminal you are watching.
		fmt.Fprintln(out, line)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(line+"\n"))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "text/plain")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("appending to %s: %w", inst.URL, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	return scan.Err()
}
