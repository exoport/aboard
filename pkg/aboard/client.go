// client.go — the commands that talk TO a running board.
//
// Every one of them goes through the instance file to find the URL, so they all
// fail the same recognisable way when nothing is running. None of them exits the
// process: they return errors, and the cli layer decides what a status code is.

package aboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// replyReadLimit caps what a reply body may cost us. Every response this file
// reads is a status line or a short refusal; anything larger is a server that
// has gone wrong, and reading it all would be the second thing to go wrong.
const replyReadLimit = 4 << 10

// ErrNoBase is what Apply returns for a document carrying no compare-and-set
// base at all. A sentinel because the cli layer has to map it to exit 2: it is a
// usage refusal, detected before the board is contacted, and it is the one
// refusal in here that a caller can fix by re-reading rather than by retrying.
var ErrNoBase = errors.New("no compare-and-set base")

// Apply posts a board document to the running board instead of writing the file
// directly. Direct writes have no compare-and-set, so two agents — or an agent
// and the browser — can silently drop each other's changes; going through the
// server means a stale write is refused with 409 instead of winning.
//
// The base for the comparison is the `rev` already inside the submitted
// document: whatever was read before editing is exactly the right base. A
// document with no `rev` and no `updatedAt` has NO base, and that used to be an
// unconditional write — `__base` was set only when a timestamp was present and
// the server skipped the check when it was empty, so a document built from the
// minimal shape in the docs overwrote everything written since it was read, exit
// 0, nothing on stderr. It is refused now, and --force is the way to say you
// meant it.
func Apply(ctx context.Context, root Root, name, by string, force bool, assets fs.FS, in io.Reader, out, errOut io.Writer) error {
	if by == actorHuman {
		// The human acts in the browser, and the guarantees in tabs.go key off
		// this exact string: a write stamped `human` may delete tabs, clear
		// `touched` markers and drop chat acks. An agent that borrowed the name
		// would be handed all three, and the tab dot that tells the human
		// something changed would simply never appear.
		return errors.New(`--by human is refused: "human" means a person acting in the browser, and it carries powers an agent must not have (deleting tabs, clearing change markers). Use agent-1, agent-2 or agent-<role>`)
	}

	body, err := io.ReadAll(io.LimitReader(in, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	// Parsed with encoding/json/v2, which is what the server parses with, and
	// that is the whole point of doing it here: v2 refuses a duplicate object
	// name and invalid UTF-8, where v1 took the last value and replaced the bad
	// bytes. A v1 parse here would have SILENTLY COLLAPSED a duplicate before the
	// server ever saw it — apply would re-marshal the map it decoded, the write
	// would land, and the field the agent thought it set would be the other one.
	// Refusing it in the caller's own terminal is the only place the agent that
	// wrote the document is still listening.
	var doc map[string]any
	if err := jsonv2.Unmarshal(body, &doc); err != nil {
		if errors.Is(err, jsontext.ErrDuplicateName) {
			return fmt.Errorf("stdin json has a duplicate key (%w) — one object sets the same name twice, "+
				"and which one wins is not something to leave to a parser; remove the one you did not mean", err)
		}
		return fmt.Errorf("stdin is not valid json: %w", err)
	}
	if _, ok := doc["tabs"].([]any); !ok {
		return errors.New("stdin json has no tabs array")
	}

	// The base, before anything else is printed. A document with none is refused
	// rather than warned about, so there is nothing to read past — and the caller
	// will see the write warnings on the retry that carries a base.
	base := applyBase(doc)
	switch {
	case force:
		// Deliberate and announced. An unconditional write is occasionally the
		// right thing — repairing a document the browser cannot render, seeding a
		// board from a fixture — and the honest shape for it is a flag that says
		// so on stderr, not a silent consequence of the field being absent. First
		// line, because it is the one that says another writer may be about to
		// lose their work.
		fmt.Fprintf(errOut, "warning: --force: writing without compare-and-set — anything written since you read this document is overwritten\n")
	case base == "":
		return fmt.Errorf("%w: this document has no `rev` (and no `updatedAt`), so the write would overwrite anything since you read it — re-read .aboard/aboard.json (or GET /aboard.json), edit THAT document so it keeps its `rev`, or pass --force to write unconditionally", ErrNoBase)
	default:
		doc["__base"] = base
	}

	// Then the warnings, the version one before the rest: it is the only failure
	// here that blanks the WHOLE board rather than one field, so it is the one
	// worth reading first if a caller reads only the first line.
	//
	// Both are warnings, both print here rather than server-side, and that is
	// deliberate: a CLI warning can only reach the actor who runs the CLI. That is
	// the right audience for these two, because both are mistakes only an agent
	// can make — the browser sends the version it loaded and writes state through
	// the renderers themselves.
	if warning := wrongVersion(body); warning != "" {
		fmt.Fprintf(errOut, "warning: %s\n", warning)
	}

	// The one check no document can perform: does this write set state the
	// renderer never reads? Without it, state.foo is stored, ignored, and
	// reported to the human as done. A warning and not a refusal — a spec can lag
	// its renderer, and refusing writes over stale documentation would be worse
	// than documenting late. Descends into ui trees and stack blocks (caps.go).
	for _, warning := range writeWarnings(assets, body) {
		fmt.Fprintf(errOut, "warning: %s\n", warning)
	}

	inst, err := RunningInstance(root, name)
	if err != nil {
		return err
	}
	doc["__origin"] = "apply"
	doc["__by"] = by

	payload, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.URL+"/aboard.json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting to %s: %w", inst.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(io.LimitReader(resp.Body, replyReadLimit))

	switch resp.StatusCode {
	case http.StatusOK:
		fmt.Fprintf(out, "applied to %s as %q\n", inst.URL, by)
		return nil
	case http.StatusConflict:
		return fmt.Errorf("refused: the board changed since you read it (%s) — re-read the board document, redo the edit, apply again", strings.TrimSpace(string(got)))
	case http.StatusForbidden:
		return fmt.Errorf("refused by the board (%s) — this is the origin/host guard; a client on loopback with no Origin header is what it expects", strings.TrimSpace(string(got)))
	default:
		return fmt.Errorf("board returned %d: %s", resp.StatusCode, strings.TrimSpace(string(got)))
	}
}

// applyBase reads the compare-and-set base out of the document being submitted.
//
// `rev` is the token. `updatedAt` is accepted as a fallback for exactly one
// case — a board whose last write predates the revision counter, whose document
// therefore has no `rev` to send — and the server refuses a timestamp base the
// moment the live document has a `rev` of its own.
func applyBase(doc map[string]any) string {
	switch rev := doc["rev"].(type) {
	case float64:
		return strconv.Itoa(int(rev))
	case string:
		if _, err := strconv.Atoi(strings.TrimSpace(rev)); err == nil {
			return strings.TrimSpace(rev)
		}
	}
	if stamp, ok := doc["updatedAt"].(string); ok {
		return stamp
	}
	return ""
}

/* ---------- status ---------- */

// SkillState is what `status` can say about the committed skill reference.
const (
	SkillCurrent = "current"
	SkillStale   = "stale"
	SkillAbsent  = "absent"
)

// StatusReport is what `aboard status` knows. A struct rather than printed prose
// because --output-format json has to say the same things the human form does,
// and the only way to guarantee that is for both to render the same value.
type StatusReport struct {
	Project string `json:"project"        yaml:"project"`
	Name    string `json:"name,omitempty" yaml:"name,omitempty"`
	// Running is true only when something on the recorded port answered /health.
	Running bool `json:"running" yaml:"running"`
	// Recorded is true when an instance file exists, whether or not it is live.
	// Recorded && !Running is the stale-record case, which needs its own message.
	Recorded     bool   `json:"recorded"          yaml:"recorded"`
	App          string `json:"app,omitempty"     yaml:"app,omitempty"`
	URL          string `json:"url,omitempty"     yaml:"url,omitempty"`
	Port         int    `json:"port,omitempty"    yaml:"port,omitempty"`
	PID          int    `json:"pid,omitempty"     yaml:"pid,omitempty"`
	State        string `json:"state,omitempty"   yaml:"state,omitempty"`
	Started      string `json:"started,omitempty" yaml:"started,omitempty"`
	Version      string `json:"version,omitempty" yaml:"version,omitempty"`
	InstanceFile string `json:"instanceFile"      yaml:"instanceFile"`
	// WouldUsePort is the derived port, reported when nothing is running so the
	// reader knows where to look once it is.
	WouldUsePort int `json:"wouldUsePort,omitempty" yaml:"wouldUsePort,omitempty"`

	CapsHash string `json:"capsHash" yaml:"capsHash"`
	// Skill is SkillCurrent, SkillStale or SkillAbsent. Absent is not drift: a
	// project that never copied the skill has nothing to be out of date.
	Skill         string `json:"skill"                   yaml:"skill"`
	SkillCapsHash string `json:"skillCapsHash,omitempty" yaml:"skillCapsHash,omitempty"`
}

// Status collects everything `aboard status` reports.
func Status(ctx context.Context, root Root, name string, assets fs.FS) StatusReport {
	rep := StatusReport{
		Project:      root.String(),
		Name:         name,
		InstanceFile: root.InstanceFile(name),
		Skill:        SkillAbsent,
	}
	if m, err := buildManifest(assets); err == nil {
		rep.CapsHash = m.Hash
		rep.SkillCapsHash = stampedHash(root)
		switch rep.SkillCapsHash {
		case "":
			rep.Skill = SkillAbsent
		case m.Hash:
			rep.Skill = SkillCurrent
		default:
			rep.Skill = SkillStale
		}
	}

	body, err := os.ReadFile(rep.InstanceFile)
	if err != nil {
		rep.WouldUsePort = DerivePort(root, name)
		return rep
	}
	rep.Recorded = true
	var got Instance
	if json.Unmarshal(body, &got) != nil {
		rep.WouldUsePort = DerivePort(root, name)
		return rep
	}
	rep.Port, rep.PID, rep.URL, rep.State = got.Port, got.PID, got.URL, got.State
	rep.App, rep.Version, rep.Started = got.App, got.Version, got.Started

	live := ProbeBoard(ctx, got.Port, got.Base)
	if live == nil {
		return rep
	}
	rep.Running = true
	rep.App, rep.URL, rep.State = live.App, live.URL, live.State
	rep.PID, rep.Version, rep.Started = live.PID, live.Version, live.Started
	return rep
}

// Human renders the report the way the terminal has always shown it.
func (r StatusReport) Human() string {
	var b strings.Builder
	// A named board says its name in every line that names the project. With two
	// boards in one directory, "no board recorded for /path" is ambiguous exactly
	// when it matters — the reader asked about one of them.
	where := r.Project
	if r.Name != "" {
		where += " [" + r.Name + "]"
	}
	switch {
	case !r.Recorded:
		fmt.Fprintf(&b, "no board recorded for %s\n", where)
		fmt.Fprintf(&b, "it would use port %d\n", r.WouldUsePort)
	case !r.Running:
		fmt.Fprintf(&b, "stale record: %s (pid %d) is not answering\n", r.URL, r.PID)
		fmt.Fprintf(&b, "start a fresh one with `aboard serve`\n")
	default:
		fmt.Fprintf(&b, "aboard running at %s\n", r.URL)
		fmt.Fprintf(&b, "  project %s\n  state   %s\n  pid     %d\n  since   %s\n",
			where, r.State, r.PID, r.Started)
		if r.App != "" && r.App != HostStandalone {
			fmt.Fprintf(&b, "  served  %s\n", r.App)
		}
	}
	// The skill is an open page that loaded a document: same problem the browser
	// has after a rebuild, so it gets the same treatment — a signature, and a
	// warning when what you are reading was generated for a different one.
	switch r.Skill {
	case SkillCurrent:
		fmt.Fprintf(&b, "  caps    %s   (skill reference current)\n", r.CapsHash)
	case SkillStale:
		fmt.Fprintf(&b, "  caps    %s   ⚠ skill reference generated for %s — run `make caps`\n", r.CapsHash, r.SkillCapsHash)
	default:
		fmt.Fprintf(&b, "  caps    %s\n", r.CapsHash)
	}
	return b.String()
}
