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

// ErrWarnings is what --strict returns when the document warns. A sentinel so
// the cli layer can tell "your document is wrong" from "the board refused you",
// and so a script can match on it rather than on a sentence.
var ErrWarnings = errors.New("the document warns and --strict was asked for")

// ApplyOptions is everything `aboard apply` decides before it posts.
//
// A struct rather than five more positional parameters: three of the five are
// bools, and `Apply(ctx, root, name, by, false, true, false, …)` is a call
// nobody can read at the site or review in a diff.
type ApplyOptions struct {
	// By is the actor recorded in lastEditedBy and on every tab the write
	// touched. Never "human" — see Apply.
	By string
	// Label is why this write is happening, recorded on the journal entry and
	// nowhere in the board document.
	Label string
	// Force writes with no compare-and-set at all, and says so on stderr.
	Force bool
	// Check runs the write-time checks and stops there: nothing is posted, and no
	// board needs to be running. The cheap habit before a write.
	Check bool
	// Strict turns any warning into a refusal. Opt-in per call, because warning
	// rather than refusing is the default for a good reason — a spec can lag its
	// renderer, and a board that rejects writes over its own stale documentation
	// would be worse than one that documents late.
	Strict bool
}

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
func Apply(ctx context.Context, root Root, name string, options ApplyOptions, assets fs.FS, in io.Reader, out, errOut io.Writer) error {
	by, force := options.By, options.Force
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
	if _, ok := doc[keyTabs].([]any); !ok {
		return errors.New("stdin json has no tabs array")
	}

	// The base, before anything else is printed. A document with none is refused
	// rather than warned about, so there is nothing to read past — and the caller
	// will see the write warnings on the retry that carries a base.
	//
	// --check skips this whole question. It is a question about CONCURRENCY —
	// "has the board moved since you read it" — and a check that posts nothing
	// cannot lose anybody's work. Refusing to check a document because it has no
	// `rev` would make the cheap habit unavailable in exactly the case it is
	// cheapest: a document being built up before it is ever applied.
	base := applyBase(doc)
	switch {
	case options.Check:
		// Nothing to compare against, because nothing is going to be posted.
	case force:
		// Deliberate and announced. An unconditional write is occasionally the
		// right thing — repairing a document the browser cannot render, seeding a
		// board from a fixture — and the honest shape for it is a flag that says
		// so on stderr, not a silent consequence of the field being absent. First
		// line, because it is the one that says another writer may be about to
		// lose their work.
		fmt.Fprintf(errOut, "warning: --force: writing without compare-and-set — anything written since you read this document is overwritten\n")
		// CLEARED, not merely unused. The document still carries the `rev` it was
		// read at, and postDocument puts whatever base it is handed on the wire —
		// so leaving it set here would make --force a compare-and-set write that
		// announced itself as an unconditional one.
		base = ""
	case base == "":
		return fmt.Errorf("%w: this document has no `rev` (and no `updatedAt`), so the write would overwrite anything since you read it — re-read .aboard/aboard.json (or GET /aboard.json), edit THAT document so it keeps its `rev`, or pass --force to write unconditionally", ErrNoBase)
	}

	// Then the warnings, the version one before the rest: it is the only failure
	// here that blanks the WHOLE board rather than one field, so it is the one
	// worth reading first if a caller reads only the first line.
	//
	// Printed HERE as well as recorded server-side, and both halves are needed. A
	// CLI warning can only reach the actor who runs the CLI, which is the right
	// audience for these — a stale `version` and undeclared state are mistakes
	// only an agent can make, since the browser sends the version it loaded and
	// writes state through the renderers themselves. But an agent that pipes
	// stderr to /dev/null used to be the end of it, so the server now puts the
	// same strings on the journal entry and in front of the human (journal.go).
	// This is the copy that reaches the one actor still holding the context to fix
	// it, and it is the only copy that can stop the write.
	//
	// The one check no document can perform comes after it: does this write set
	// state the renderer never reads? Without it, state.foo is stored, ignored,
	// and reported to the human as done. Warnings and not refusals — a spec can
	// lag its renderer, and refusing writes over stale documentation would be
	// worse than documenting late. Descends into ui trees and stack blocks
	// (caps.go).
	warnings := []string{}
	if warning := wrongVersion(body); warning != "" {
		warnings = append(warnings, warning)
	}
	warnings = append(warnings, writeWarnings(assets, body)...)
	for _, warning := range warnings {
		fmt.Fprintf(errOut, "warning: %s\n", warning)
	}

	// --strict is the guard for a loop that must stop rather than ship a wrong
	// tab, and it is opt-in per call: it does not move the default, it declines
	// it. Refused BEFORE the board is contacted, so "nothing was written" needs no
	// qualification.
	if options.Strict && len(warnings) > 0 {
		return fmt.Errorf("%w: %d warning(s), nothing written", ErrWarnings, len(warnings))
	}
	// --check stops here, having contacted nothing. The failure mode the flag is
	// designed against is a session that never runs it, so it says what it did
	// even when it found nothing — a command that prints nothing on success is a
	// command people stop believing they ran.
	if options.Check {
		if len(warnings) == 0 {
			fmt.Fprintf(out, "checked: no warnings — nothing was written (drop --check to apply)\n")
			return nil
		}
		fmt.Fprintf(out, "checked: %d warning(s) — nothing was written\n", len(warnings))
		return nil
	}

	inst, err := RunningInstance(root, name)
	if err != nil {
		return err
	}
	// The label rides beside __by and __base, and it is handed to postDocument
	// rather than written onto the document here: the merged retry below posts a
	// document rebuilt from the board's own, so a key set on ours would not
	// survive it — and the merged write is the SAME write, so it keeps the same
	// reason on the record.
	label := strings.TrimSpace(options.Label)

	code, got, err := postDocument(ctx, inst.URL, doc, base, by, label)
	if err != nil {
		return err
	}

	switch code {
	case http.StatusOK:
		fmt.Fprintf(out, "applied to %s as %q\n", inst.URL, by)
		return nil
	case http.StatusConflict:
		// Not the end of the write any more. See merge.go: somebody else landed
		// between our read and our write, and unless they touched a tab WE touched
		// that is a document we can rebuild rather than a document to throw away.
		return retryMerged(ctx, inst, doc, base, by, label, got, out, errOut)
	case http.StatusForbidden:
		return fmt.Errorf("refused by the board (%s) — this is the origin/host guard; a client on loopback with no Origin header is what it expects", strings.TrimSpace(got))
	default:
		return fmt.Errorf("board returned %d: %s", code, strings.TrimSpace(got))
	}
}

// conflictRefusal is the sentence a 409 got before merging existed, and still
// gets when merging cannot help. One function so the two paths cannot drift into
// two different accounts of the same refusal.
func conflictRefusal(body string) error {
	return fmt.Errorf("refused: the board changed since you read it (%s) — re-read the board document, redo the edit, apply again",
		strings.TrimSpace(body))
}

// retryMerged is the 409 branch: rebuild the write against the document as it is
// now, and post it ONCE more.
//
// Once, and the bound is deliberate. A board being written to continuously would
// otherwise have `apply` retrying against a moving target for as long as the
// writes kept coming, and an agent's command that never returns is worse than one
// that returns a refusal it can act on.
func retryMerged(ctx context.Context, inst Instance, ours map[string]any, base, by, label, firstBody string,
	out, errOut io.Writer,
) error {
	// The control keys are ours, not the document's: they were added for the post
	// that just failed, and the merge compares root fields.
	delete(ours, "__base")
	delete(ours, "__origin")
	delete(ours, "__by")
	delete(ours, "__label")

	merged, err := mergeOnConflict(ctx, inst, ours, base)
	if err != nil {
		if errors.Is(err, ErrCollision) {
			// The cases that are never merged silently, exactly as the browser
			// refuses to merge one: picking a winner is not a decision a retry
			// gets to make, and neither is deciding that a tab whose provenance
			// the journal cannot settle was probably nobody's.
			return err
		}
		// Anything else — a timestamp base, a journal that no longer covers the
		// window — is reported as the plain conflict it always was, with the reason
		// on stderr so the agent knows why the merge did not even run.
		fmt.Fprintf(errOut, "warning: could not merge this conflict: %v\n", err)
		return conflictRefusal(firstBody)
	}

	code, got, err := postDocument(ctx, inst.URL, merged.doc, merged.base, by, label)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		if code == http.StatusConflict {
			return fmt.Errorf("refused twice: the board changed again while the merge was being built (%s) — re-read the board document, redo the edit, apply again",
				strings.TrimSpace(got))
		}
		return fmt.Errorf("board returned %d on the merged write: %s", code, strings.TrimSpace(got))
	}
	fmt.Fprintf(errOut, "note: the board moved while you were writing; merged onto rev %s, keeping the board's version of %s\n",
		merged.base, joinOr(merged.kept, "no tab"))
	fmt.Fprintf(out, "applied to %s as %q (merged)\n", inst.URL, by)
	return nil
}

// postDocument sends one document and reports what the board said.
//
// `base` is passed in rather than re-derived from the document, and that is not
// tidiness: --force deliberately sends NO base while the document it sends still
// carries a `rev`, so a postDocument that read the field would have quietly
// turned every forced write back into a compare-and-set one.
//
// `doc` is mutated with the control keys, which is what every caller wants: they
// are part of the write, not part of the document — and `label` is one of them,
// so the merged retry carries the same reason as the write it is redoing.
func postDocument(ctx context.Context, url string, doc map[string]any, base, by, label string) (status int, body string, err error) {
	if base != "" {
		doc["__base"] = base
	}
	doc["__origin"] = "apply"
	doc["__by"] = by
	// Stripped by the server before the document is stored, exactly like the two
	// above, and recorded on the journal entry instead. Sent only when there is
	// one: an empty label on every entry is a column of nothing.
	if label != "" {
		doc["__label"] = label
	}

	payload, marshalErr := json.Marshal(doc)
	if marshalErr != nil {
		return 0, "", marshalErr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+routeState, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("posting to %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(io.LimitReader(resp.Body, replyReadLimit))
	return resp.StatusCode, string(got), nil
}

// applyBase reads the compare-and-set base out of the document being submitted.
//
// `rev` is the token. `updatedAt` is accepted as a fallback for exactly one
// case — a board whose last write predates the revision counter, whose document
// therefore has no `rev` to send — and the server refuses a timestamp base the
// moment the live document has a `rev` of its own.
func applyBase(doc map[string]any) string {
	switch rev := doc[keyRev].(type) {
	case float64:
		return strconv.Itoa(int(rev))
	case string:
		if _, err := strconv.Atoi(strings.TrimSpace(rev)); err == nil {
			return strings.TrimSpace(rev)
		}
	}
	if stamp, ok := doc[keyUpdatedAt].(string); ok {
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

	// Requests is how many of the human's notes are still waiting on an agent.
	// It is on `status` and not only on `aboard requests` because status is the
	// FIRST command a resuming session runs, and a request nobody discovers is a
	// request that was not made. Zero is omitted: a line saying "0 requests" on
	// every board would be one more thing to read past.
	Requests int `json:"requests,omitempty" yaml:"requests,omitempty"`

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
		Requests:     PendingRequests(root, name),
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
	// What the human has asked for, before the caps beacon, because it is the one
	// line here that is about THEM rather than about the machinery. It prints
	// whether or not a board is running: a note left last week survives a restart
	// and a week away, and the session most likely to have missed it is the one
	// that finds no board running.
	if r.Requests > 0 {
		fmt.Fprintf(&b, "  asked   %d request%s waiting — `aboard requests%s`\n",
			r.Requests, plural(r.Requests), nameFlagFor(r.Name))
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
