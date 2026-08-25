// board — a shared board for a human and Claude Code, as a single binary.
//
//	GET  /             board.html
//	GET  /board.json   current state
//	POST /board.json   write state to disk (compare-and-set)
//	GET  /events        SSE stream; pings whenever board.json changes on disk
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
//	GET  /<file>       static assets (views/, vendor/, assets/, test/, app.css)
//
// The UI is compiled into the binary with //go:embed, so shipping the board is
// copying one file. board.json is deliberately NOT embedded: it is the shared
// state that both the browser and Claude Code read and write on disk.
//
// Stdlib only, no module dependencies — the same property the Node version had.
// That is why the file watcher polls instead of using fsnotify: one small file
// at 200ms costs nothing and keeps the dependency count at zero.
package main

import (
	"crypto/sha256"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
)

// test/ is embedded too, so a shipped binary can still self-check with
// test/smoke.sh against its own embedded UI rather than a working copy.
//
//go:embed board.html app.css views vendor assets test
var embedded embed.FS

const maxBodyBytes = 8 << 20 // 8 MiB, matching the Node server

// Each project gets its own port, derived from its absolute path, so boards in
// different checkouts never collide and each one's URL stays the same between
// runs. Range sits above the crowded 3000-9000 dev band and below the ephemeral
// range the kernel hands out for outbound connections.
const (
	portBase  = 41000
	portSpan  = 8000
	portTries = 24
)

const instanceDir = ".board"

// One record per named board, so a `-name review` instance does not overwrite
// the default board's record and leave restart.sh stopping the wrong process.
func instancePath(name string) string {
	if name == "" {
		return filepath.Join(instanceDir, "instance.json")
	}
	return filepath.Join(instanceDir, "instance."+name+".json")
}

var serveDirs = []string{"views", "vendor", "assets", "test"}
var serveFiles = []string{"app.css", "board.html"}

// instance describes a running board. It is written next to the state file so
// restart.sh can stop exactly this project's board (not every board on the
// machine), and so Claude Code can find the URL without being told.
type instance struct {
	App     string `json:"app"`
	Version string `json:"version"`
	Built   string `json:"built,omitempty"`
	Project string `json:"project"`
	Name    string `json:"name,omitempty"`
	Port    int    `json:"port"`
	URL     string `json:"url"`
	State   string `json:"state"`
	PID     int    `json:"pid"`
	Started string `json:"started"`
}

type server struct {
	stateFile string
	assets    fs.FS
	dev       bool
	port      int
	instance  instance

	mu       sync.Mutex
	clients  map[chan string]struct{}
	watchers map[chan string]struct{} // ./board -watch consumers (journal.go)

	// Sessions blocked on /wait, released by the human's notify button or by
	// another session's -poke (see wait.go).
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

func main() {
	port := flag.Int("port", envInt("PORT", 0), "port to listen on (0 derives one from the project path)")
	state := flag.String("state", "", "path to the state file (default board.json, or board.<name>.json)")
	name := flag.String("name", os.Getenv("BOARD_NAME"), "instance name, for a second isolated board in the same project")
	dev := flag.Bool("dev", false, "serve UI files from disk instead of the embedded copies")
	status := flag.Bool("status", false, "report this project's running board, if any, and exit")
	apply := flag.Bool("apply", false, "read state JSON on stdin and write it through the running board (compare-and-set)")
	by := flag.String("by", "agent-1", "label recorded in lastEditedBy and on touched tabs by -apply")
	wait := flag.Bool("wait", false, "block until the board is poked, print the event, exit (0 poked, 3 timed out)")
	poke := flag.Bool("poke", false, "release every session waiting on this board, as the notify button does")
	forWhat := flag.String("for", "poke", "what -wait waits for: poke | change | \"tab <id>\" | \"answer <id>\" | \"node <id>=<status>\"")
	timeout := flag.Duration("timeout", waitDefault, "how long -wait blocks before giving up")
	note := flag.String("note", "", "with -poke, a message for the waiting sessions; with -wait, why you are waiting (shown on the button)")
	watch := flag.Bool("watch", false, "follow every change as JSON lines until interrupted")
	showJournal := flag.Bool("journal", false, "print recent writes: when, who, which tabs")
	limit := flag.Int("limit", 40, "how many entries -journal prints")
	logTab := flag.String("log", "", "read stdin and append it to this tab's log, line by line")
	caps := flag.Bool("capabilities", false, "print what this board can do: types, state fields, gestures, endpoints, flags")
	capsFormat := flag.String("format", "json", "with -capabilities: json, md, or js (the generated control module)")
	capsCheck := flag.Bool("check", false, "with -capabilities: exit 1 if the committed skill reference is stale")
	export := flag.String("export", "", "print one tab as text for pasting into the project's own documents (a tab id or key; a wrong one lists them)")
	flag.Parse()

	project, err := os.Getwd()
	if err != nil {
		log.Fatalf("cannot determine the project directory: %v", err)
	}

	if *state == "" {
		if *name != "" {
			*state = "board." + *name + ".json"
		} else {
			*state = "board.json"
		}
	}

	// Before anything that needs a port or a state file: the manifest must answer
	// on a fresh checkout, from a copied binary, and while another session holds
	// the port.
	if *caps {
		var assets fs.FS = embedded
		if *dev {
			assets = os.DirFS(".")
		}
		code, err := capsCLI(assets, *capsFormat, flag.Arg(0), *capsCheck)
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(code)
	}

	// Also before the listener: promoting a tab's conclusions into a document must
	// not require a running server, for the same reason -capabilities does not.
	if *export != "" {
		if err := exportCLI(*state, *export, *capsFormat); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *status {
		reportStatus(project, *name)
		return
	}

	if *apply {
		if err := applyStdin(*name, *by); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *wait {
		code, err := waitCLI(*name, *by, *forWhat, *note, *timeout)
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(code)
	}

	if *poke {
		if err := pokeCLI(*name, *by, *note); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *showJournal {
		if err := journalCLI(*name, *limit); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *watch {
		if err := watchCLI(*name); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *logTab != "" {
		if err := logCLI(*name, *logTab); err != nil {
			log.Fatal(err)
		}
		return
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
			log.Printf("mime %s: %v", ext, err)
		}
	}

	var assets fs.FS = embedded
	if *dev {
		assets = os.DirFS(".")
	}

	srv := &server{
		stateFile: *state,
		assets:    assets,
		dev:       *dev,
		clients:   map[chan string]struct{}{},
		watchers:  map[chan string]struct{}{},
		waits:     newWaitHub(),
		ui:        newUIWatcher(*dev),
		journal:   newJournal(*state),
	}

	if _, err := os.Stat(srv.stateFile); err != nil {
		log.Fatalf("state file %s: %v", srv.stateFile, err)
	}

	listener, chosen, err := srv.listen(*port, project, *name)
	if err != nil {
		log.Fatal(err)
	}

	srv.port = chosen
	if err := srv.writeInstance(project, *name); err != nil {
		log.Printf("warning: could not record the instance file: %v", err)
	}
	defer srv.removeInstance()

	go srv.watch()
	go srv.watchUI()

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.route)

	mode := "embedded UI"
	if *dev {
		mode = "UI from disk (dev)"
	}
	label := ""
	if *name != "" {
		label = "  [" + *name + "]"
	}
	log.Printf("board  ->  http://localhost:%d%s   (%s, %s)", chosen, label, mode, buildVersion())
	log.Printf("state  ->  %s", srv.stateFile)
	log.Printf("project->  %s", project)
	log.Printf(`In VS Code: Ctrl/Cmd+Shift+P -> "Simple Browser: Show" -> paste the URL above.`)

	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Remove the instance file on Ctrl-C too, not only on a clean return, so a
	// stale record never points at a dead process.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		srv.removeInstance()
		_ = httpSrv.Close()
	}()

	if err := httpSrv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// listen binds the project's port. An explicit -port is taken literally. Port 0
// means "derive one from the project path", then walk forward if that exact port
// is already busy — but if the occupant turns out to be this project's own
// board, say so and stop instead of starting a duplicate.
func (s *server) listen(want int, project, name string) (net.Listener, int, error) {
	if want != 0 {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", want))
		if err != nil {
			return nil, 0, fmt.Errorf("port %d is busy: %w", want, err)
		}
		return ln, want, nil
	}

	first := derivePort(project, name)
	for i := 0; i < portTries; i++ {
		p := portBase + ((first - portBase + i) % portSpan)
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			return ln, p, nil
		}
		if other := probeBoard(p); other != nil && other.Project == project && other.Name == name {
			return nil, 0, fmt.Errorf("this project's board is already running at %s (pid %d)", other.URL, other.PID)
		}
		log.Printf("port %d busy, trying %d", p, portBase+((first-portBase+i+1)%portSpan))
	}
	return nil, 0, fmt.Errorf("no free port found in %d-%d after %d tries", portBase, portBase+portSpan-1, portTries)
}

// derivePort maps a project path (plus optional instance name) to a stable port,
// so the same checkout always serves on the same URL.
func derivePort(project, name string) int {
	sum := sha256.Sum256([]byte(project + "\x00" + name))
	return portBase + int(binary.BigEndian.Uint32(sum[:4])%portSpan)
}

// probeBoard asks whoever holds a port whether they are a board, and whose.
func probeBoard(port int) *instance {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var got instance
	if json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&got) != nil || got.App != "board" {
		return nil
	}
	return &got
}

func (s *server) writeInstance(project, name string) error {
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		return err
	}
	s.instance = instance{
		App:     "board",
		Version: buildVersion(),
		Built:   buildStamp(),
		Project: project,
		Name:    name,
		Port:    s.port,
		URL:     fmt.Sprintf("http://localhost:%d", s.port),
		State:   s.stateFile,
		PID:     os.Getpid(),
		Started: time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.MarshalIndent(s.instance, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(instancePath(name), append(body, '\n'), 0o644)
}

func (s *server) removeInstance() {
	// Only clear the record if it is still ours, so a restart that already
	// overwrote it is not undone by this process exiting afterwards.
	path := instancePath(s.instance.Name)
	body, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var got instance
	if json.Unmarshal(body, &got) == nil && got.PID != os.Getpid() {
		return
	}
	_ = os.Remove(path)
}

// applyStdin posts new state to the running board instead of writing the file
// directly. Direct writes have no compare-and-set, so two agents — or an agent
// and the browser — can silently drop each other's changes; going through the
// server means a stale write is refused with 409 instead of winning.
//
// The base for the comparison is the `updatedAt` already inside the submitted
// document: whatever was read before editing is exactly the right base.
func applyStdin(name, by string) error {
	body, err := io.ReadAll(io.LimitReader(os.Stdin, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	// Warnings first, and the version one before the rest: it is the only failure
	// here that blanks the WHOLE board rather than one field, so it is the one
	// worth reading first if a caller reads only the first line.
	//
	// Both are warnings, both print here rather than server-side, and that is
	// deliberate: a CLI warning can only reach the actor who runs the CLI. That is
	// the right audience for these two, because both are mistakes only an agent
	// can make — the browser sends the version it loaded and writes state through
	// the renderers themselves.
	if warning := wrongVersion(body); warning != "" {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}

	// The one check no document can perform: does this write set state the
	// renderer never reads? Without it, state.foo is stored, ignored, and
	// reported to the human as done. A warning and not a refusal — a spec can lag
	// its renderer, and refusing writes over stale documentation would be worse
	// than documenting late. Descends into ui trees and stack blocks (caps.go).
	for _, warning := range writeWarnings(embedded, body) {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("stdin is not valid json: %w", err)
	}
	if _, ok := doc["tabs"].([]any); !ok {
		return errors.New("stdin json has no tabs array")
	}

	inst, err := runningInstance(name)
	if err != nil {
		return err
	}

	if base, ok := doc["updatedAt"].(string); ok {
		doc["__base"] = base
	}
	doc["__origin"] = "apply"
	doc["__by"] = by

	payload, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	resp, err := http.Post(inst.URL+"/board.json", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("posting to %s: %w", inst.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	switch resp.StatusCode {
	case http.StatusOK:
		fmt.Printf("applied to %s as %q\n", inst.URL, by)
		return nil
	case http.StatusConflict:
		return fmt.Errorf("refused: the board changed since you read it (%s) — re-read board.json, redo the edit, apply again", strings.TrimSpace(string(out)))
	default:
		return fmt.Errorf("board returned %d: %s", resp.StatusCode, strings.TrimSpace(string(out)))
	}
}

// runningInstance finds the board every client-side flag talks to (-apply,
// -wait, -poke). One lookup, so they all fail the same recognisable way when
// nothing is running.
func runningInstance(name string) (instance, error) {
	var inst instance
	rec, err := os.ReadFile(instancePath(name))
	if err != nil {
		return inst, fmt.Errorf("no running board found (%s); start it with ./restart.sh", instancePath(name))
	}
	if err := json.Unmarshal(rec, &inst); err != nil {
		return inst, fmt.Errorf("unreadable instance file %s", instancePath(name))
	}
	return inst, nil
}

func reportStatus(project, name string) {
	body, err := os.ReadFile(instancePath(name))
	if err != nil {
		fmt.Printf("no board recorded for %s\n", project)
		fmt.Printf("it would use port %d\n", derivePort(project, name))
		return
	}
	var got instance
	if json.Unmarshal(body, &got) != nil {
		fmt.Println("instance file is unreadable; delete " + instancePath(name))
		return
	}
	live := probeBoard(got.Port)
	if live == nil {
		fmt.Printf("stale record: %s (pid %d) is not answering\n", got.URL, got.PID)
		fmt.Println("start a fresh one with ./restart.sh")
		return
	}
	fmt.Printf("board running at %s\n", live.URL)
	fmt.Printf("  project %s\n  state   %s\n  pid     %d\n  since   %s\n",
		live.Project, live.State, live.PID, live.Started)
	// The skill is an open page that loaded a document: same problem the browser
	// has after a rebuild, so it gets the same treatment — a signature, and a
	// warning when what you are reading was generated for a different one.
	if line := capsStatusLine(embedded); line != "" {
		fmt.Println(line)
	}
}

// buildVersion identifies the BINARY that is actually serving, not a constant
// someone has to remember to bump. Go stamps the VCS revision into the build when
// it can, so this is the commit the running board was built from, with "+dirty"
// when the tree had uncommitted changes — which, on this project, it usually does.
//
// The fallback is the binary's own mtime: a version string that lies is worse than
// one that admits it only knows when the file was written.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			rev = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		return rev + "+dirty"
	}
	return rev
}

// buildStamp is when the running binary was written — the answer to "am I looking
// at the code I just built?", which the revision alone cannot give on a dirty tree.
func buildStamp() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	info, err := os.Stat(exe)
	if err != nil {
		return ""
	}
	return info.ModTime().UTC().Format(time.RFC3339)
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func (s *server) route(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/" || r.URL.Path == "/board.html":
		s.serveAsset(w, r, "board.html")
	case r.URL.Path == "/board.json" && r.Method == http.MethodGet:
		s.getState(w)
	case r.URL.Path == "/board.json" && r.Method == http.MethodPost:
		s.postState(w, r)
	case strings.HasPrefix(r.URL.Path, "/tab/") && strings.HasSuffix(r.URL.Path, "/html") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tab/"), "/html")
		s.serveTabHTML(w, r, id)
	case r.URL.Path == "/health" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, s.instance)
	case r.URL.Path == "/events" && r.Method == http.MethodGet:
		s.events(w, r)
	case r.URL.Path == "/wait" && r.Method == http.MethodGet:
		s.handleWait(w, r)
	case r.URL.Path == "/poke" && r.Method == http.MethodPost:
		s.handlePoke(w, r)
	case r.URL.Path == "/waiters" && r.Method == http.MethodGet:
		s.handleWaiters(w, r)
	case r.URL.Path == "/capabilities" && r.Method == http.MethodGet:
		s.handleCapabilities(w, r)
	case r.URL.Path == "/journal" && r.Method == http.MethodGet:
		s.handleJournal(w, r)
	case r.URL.Path == "/watch" && r.Method == http.MethodGet:
		s.handleWatch(w, r)
	case r.URL.Path == "/log" && r.Method == http.MethodPost:
		s.handleLogPost(w, r)
	case r.URL.Path == "/log" && r.Method == http.MethodGet:
		s.handleLogGet(w, r)
	case r.URL.Path == "/upload" && r.Method == http.MethodPost:
		s.handleUpload(w, r)
	case r.URL.Path == "/uploads" && r.Method == http.MethodGet:
		s.handleUploads(w, r)
	case strings.HasPrefix(r.URL.Path, "/"+uploadDir+"/") && r.Method == http.MethodGet:
		s.serveUpload(w, r)
	case r.Method == http.MethodGet:
		s.static(w, r)
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body too large or unreadable"})
		return
	}

	// Decode into an ordered-ish generic map. Field order in the written file is
	// whatever encoding/json produces (alphabetical); the board does not care,
	// and it keeps diffs stable between writes.
	var incoming map[string]any
	if err := json.Unmarshal(raw, &incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if _, ok := incoming["tabs"].([]any); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected a tabs array"})
		return
	}

	origin, _ := incoming["__origin"].(string)
	if origin == "" {
		origin = "browser"
	}
	base, _ := incoming["__base"].(string)
	by, _ := incoming["__by"].(string)
	if by == "" {
		by = "human"
	}
	delete(incoming, "__origin")
	delete(incoming, "__base")
	delete(incoming, "__by")

	currentRaw, _ := os.ReadFile(s.stateFile)

	// Compare-and-set: refuse the write if the file moved on since the browser
	// loaded it, so an agent edit is never silently clobbered.
	if base != "" && len(currentRaw) > 0 {
		var cur map[string]any
		if json.Unmarshal(currentRaw, &cur) == nil {
			if live, ok := cur["updatedAt"].(string); ok && live != base {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "conflict", "live": live})
				return
			}
		}
	}

	// Tab-level guarantees: an agent may not delete a tab or clear a change
	// marker, and every tab it did change gets stamped so the UI can show a dot.
	tabs, err := reconcileTabs(currentRaw, raw, by)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
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
	// board.html refuses to render a document whose version it does not know. The
	// agent found out last, from the human, which is exactly backwards.
	//
	// Stamped rather than refused, for the same reason nextId is reconciled rather
	// than rejected: the CONTENT was fine, and failing a good write over a field
	// the caller should never have set is the worse trade. The agent is still told
	// — applyStdin warns on stderr (see wrongVersion) — so the stale source gets
	// fixed by whoever is still holding the context for it.
	incoming["version"] = schemaVersion

	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	incoming["updatedAt"] = stamp
	incoming["lastEditedBy"] = by

	out, err := json.MarshalIndent(incoming, "", "  ")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encode failed"})
		return
	}
	out = append(out, '\n')

	// One comparison, three consumers: the journal on disk, the -watch stream,
	// and any session blocked on a predicate about this very change.
	entry := changeSummary(currentRaw, tabs, by, origin)

	if err := s.writeAtomic(out); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "write failed"})
		return
	}

	s.mu.Lock()
	s.pendingOrigin = origin
	s.mu.Unlock()

	if len(entry.Tabs) > 0 {
		s.journal.append(entry)
		s.notifyWatchers(entry)
		if released := s.waits.releaseMatching(out, entry); released > 0 {
			log.Printf("released %d waiting session(s) on: %s", released, joinOr(entry.Tabs, "no tab"))
			s.broadcastWaiters()
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updatedAt": stamp})
}

// Write via a temp file in the same directory, then rename. A reader (Claude
// Code, or another browser) therefore never observes a half-written file.
func (s *server) writeAtomic(body []byte) error {
	dir := filepath.Dir(s.stateFile)
	tmp, err := os.CreateTemp(dir, ".board-*.json")
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

// Poll rather than use an OS watcher: it keeps the binary dependency-free, and
// unlike fs.watch it cannot be defeated by an editor replacing the file (a
// rename breaks a single-file watch, which is a real bug in the Node version's
// approach — hence watching the directory there).
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
		b, _ := json.Marshal(map[string]string{"origin": origin})
		payload = string(b)
	}
	s.mu.Unlock()

	s.fanout(payload)
}

// fanout pushes one SSE payload to every open page. Shared by the state watcher
// and by the waiter count (wait.go), which the notify button listens for.
func (s *server) fanout(payload string) {
	s.mu.Lock()
	targets := make([]chan string, 0, len(s.clients))
	for ch := range s.clients {
		targets = append(targets, ch)
	}
	s.mu.Unlock()

	for _, ch := range targets {
		select {
		case ch <- payload:
		default: // a wedged client must not stall the watcher
		}
	}
}

/* ---------- static ---------- */

func (s *server) static(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
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
	s.serveAsset(w, r, name)
}

func (s *server) serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	body, err := fs.ReadFile(s.assets, name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

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

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}
