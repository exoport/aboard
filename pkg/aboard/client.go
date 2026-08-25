// client.go — the commands that talk TO a running board.
//
// Every one of them goes through the instance file to find the URL, so they all
// fail the same recognisable way when nothing is running. None of them exits the
// process: they return errors, and the cli layer decides what a status code is.
package aboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
)

// Apply posts a board document to the running board instead of writing the file
// directly. Direct writes have no compare-and-set, so two agents — or an agent
// and the browser — can silently drop each other's changes; going through the
// server means a stale write is refused with 409 instead of winning.
//
// The base for the comparison is the `updatedAt` already inside the submitted
// document: whatever was read before editing is exactly the right base.
func Apply(root Root, name, by string, assets fs.FS, in io.Reader, out, errOut io.Writer) error {
	if by == "human" {
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

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("stdin is not valid json: %w", err)
	}
	if _, ok := doc["tabs"].([]any); !ok {
		return errors.New("stdin json has no tabs array")
	}

	inst, err := RunningInstance(root, name)
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
	resp, err := http.Post(inst.URL+"/aboard.json", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("posting to %s: %w", inst.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	switch resp.StatusCode {
	case http.StatusOK:
		fmt.Fprintf(out, "applied to %s as %q\n", inst.URL, by)
		return nil
	case http.StatusConflict:
		return fmt.Errorf("refused: the board changed since you read it (%s) — re-read the board document, redo the edit, apply again", strings.TrimSpace(string(got)))
	default:
		return fmt.Errorf("board returned %d: %s", resp.StatusCode, strings.TrimSpace(string(got)))
	}
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
	Project string `json:"project"`
	Name    string `json:"name,omitempty"`
	// Running is true only when something on the recorded port answered /health.
	Running bool `json:"running"`
	// Recorded is true when an instance file exists, whether or not it is live.
	// Recorded && !Running is the stale-record case, which needs its own message.
	Recorded     bool   `json:"recorded"`
	App          string `json:"app,omitempty"`
	URL          string `json:"url,omitempty"`
	Port         int    `json:"port,omitempty"`
	PID          int    `json:"pid,omitempty"`
	State        string `json:"state,omitempty"`
	Started      string `json:"started,omitempty"`
	Version      string `json:"version,omitempty"`
	InstanceFile string `json:"instanceFile"`
	// WouldUsePort is the derived port, reported when nothing is running so the
	// reader knows where to look once it is.
	WouldUsePort int `json:"wouldUsePort,omitempty"`

	CapsHash string `json:"capsHash"`
	// Skill is SkillCurrent, SkillStale or SkillAbsent. Absent is not drift: a
	// project that never copied the skill has nothing to be out of date.
	Skill         string `json:"skill"`
	SkillCapsHash string `json:"skillCapsHash,omitempty"`
}

// Status collects everything `aboard status` reports.
func Status(root Root, name string, assets fs.FS) StatusReport {
	rep := StatusReport{
		Project:      root.String(),
		Name:         name,
		InstanceFile: root.InstanceFile(name),
		Skill:        SkillAbsent,
	}
	if m, err := buildManifest(assets); err == nil {
		rep.CapsHash = m.Hash
		rep.SkillCapsHash = stampedHash(root)
		switch {
		case rep.SkillCapsHash == "":
			rep.Skill = SkillAbsent
		case rep.SkillCapsHash == m.Hash:
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

	live := ProbeBoard(got.Port, got.Base)
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
