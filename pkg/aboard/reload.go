// reload.go — the page reloads itself when its own code changes.
//
// The problem this removes: an open board keeps running the JavaScript it
// loaded. The live-reload stream refreshes DATA, never CODE, so after any change
// to board.html, app.css or views/*.js the page had to be reloaded by hand — and
// in VS Code's Simple Browser that means the "Developer: Reload Webviews"
// command, because a webview never sees Ctrl+R. Every UI change ended with
// telling the user to do that, and forgetting meant debugging a page running
// last build's code.
//
// So the server publishes a signature of the UI it is serving. It goes out on
// the existing SSE stream, and — the part that matters — it is sent to every
// client the moment it connects. That one property covers both cases with no
// extra machinery:
//
//   - restart.sh -force  the stream dies, the browser reconnects on its own, the
//     first frame carries the new signature, the page reloads.
//   - restart.sh -dev    the watcher notices the file on disk changed and pushes
//     the new signature to pages already open.
//
// A restart that changes nothing changes no signature, so it costs no reload.
//
// Three hashes rather than one, because a stylesheet does not need a reload: if
// only the CSS moved, the page re-links app.css and keeps your scroll position,
// your selection and your half-typed sentence.
package aboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// uiSig identifies the code a page is running. Split by what a change to each
// part costs: html or js means reload, css means re-link.
type uiSig struct {
	HTML string `json:"html"`
	CSS  string `json:"css"`
	JS   string `json:"js"`
}

// Vendored bundles (lib/, which used to be vendor/) are sized in megabytes, so they are fingerprinted by size and
// modification time rather than content — enough to catch a swapped library
// without re-hashing 3.5 MB on every poll.
var vendorDirs = []string{"lib"}

type uiWatcher struct {
	mu     sync.Mutex
	cached *uiSig
	frozen bool // embedded assets cannot change under a running process
}

func newUIWatcher(dev bool) *uiWatcher {
	return &uiWatcher{frozen: !dev}
}

// signature returns the current signature, recomputing only when it could have
// changed. In embedded mode that is exactly once.
func (u *uiWatcher) signature(assets fs.FS) uiSig {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cached != nil && u.frozen {
		return *u.cached
	}
	sig := computeUISig(assets)
	u.cached = &sig
	return sig
}

func computeUISig(assets fs.FS) uiSig {
	return uiSig{
		HTML: hashFiles(assets, []string{"board.html"}),
		CSS:  hashFiles(assets, []string{"app.css"}),
		JS:   hashFiles(assets, listJS(assets)) + "-" + stampDirs(assets, vendorDirs),
	}
}

func hashFiles(assets fs.FS, names []string) string {
	sum := sha256.New()
	for _, name := range names {
		body, err := fs.ReadFile(assets, name)
		if err != nil {
			// A missing file is itself a state worth distinguishing from a
			// present one, so fold the error in rather than skipping it.
			fmt.Fprintf(sum, "%s:missing\n", name)
			continue
		}
		fmt.Fprintf(sum, "%s:%d:", name, len(body))
		sum.Write(body)
	}
	return hex.EncodeToString(sum.Sum(nil))[:16]
}

func listJS(assets fs.FS) []string {
	entries, err := fs.ReadDir(assets, "views")
	if err != nil {
		return nil
	}
	out := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
			out = append(out, path.Join("views", e.Name()))
		}
	}
	sort.Strings(out) // ReadDir is sorted, but the hash must not depend on that
	return out
}

func stampDirs(assets fs.FS, dirs []string) string {
	sum := sha256.New()
	for _, dir := range dirs {
		entries, err := fs.ReadDir(assets, dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			fmt.Fprintf(sum, "%s/%s:%d:%d\n", dir, e.Name(), info.Size(), info.ModTime().UnixNano())
		}
	}
	return hex.EncodeToString(sum.Sum(nil))[:8]
}

// uiPayload is what goes down the SSE stream. Keyed under "ui" so the page can
// tell it apart from a state change ("origin") and a waiter count ("waiters").
func (s *server) uiPayload() string {
	sig := s.ui.signature(s.assets)
	b, err := json.Marshal(map[string]uiSig{"ui": sig})
	if err != nil {
		return ""
	}
	return string(b)
}

// watchUI polls for asset changes in dev mode and pushes the new signature. In
// embedded mode there is nothing to poll: the files live inside this process, so
// the only way they change is a restart, and a restart is already covered by the
// signature every reconnecting client is handed.
func (s *server) watchUI() {
	if s.ui.frozen {
		return
	}
	last := s.ui.signature(s.assets)
	for {
		time.Sleep(400 * time.Millisecond)
		s.ui.mu.Lock()
		s.ui.cached = nil // force a recompute
		s.ui.mu.Unlock()
		now := s.ui.signature(s.assets)
		if now != last {
			last = now
			if payload := s.uiPayload(); payload != "" {
				s.fanout(payload)
			}
		}
	}
}
