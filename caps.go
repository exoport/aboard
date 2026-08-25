// caps.go — the board describes itself, so the skill cannot quietly disagree.
//
//	./board -capabilities                 the whole manifest, as JSON
//	./board -capabilities -format md      the markdown reference (what gets committed)
//	./board -capabilities dag             one type — cheap, for a mid-task lookup
//	./board -capabilities -check          exit 1 if the committed reference is stale
//	GET /capabilities                     the same JSON, for the browser
//
// The problem this exists for: the skill at .claude/skills/board/ is a
// hand-maintained copy of the code surface, and it decays every time a renderer
// grows a field. In one day: 9 renderers to 15, 324 to 454 reference lines, all
// written by hand after the fact. A capability the skill has never heard of is
// merely unused; one it describes WRONGLY is expensive — the agent writes
// state.foo, the renderer ignores it, nothing errors, and the agent tells the
// human it did something it did not do.
//
// The precedent is already in the repo, for the other audience: board.html keeps
// gestures beside the registry "so the panel cannot drift from what the renderers
// actually do". This extends that principle to the agent-facing skill. One
// declaration, two audiences.
//
// The seam: THE BINARY OWNS FACTS (flags, endpoints, types, state fields,
// gestures), THE SKILL OWNS JUDGMENT (dag vs diagram, ui vs html, tab sprawl,
// multi-session etiquette). Only the first half is generated. The second half is
// the valuable half and no generator can produce it.
//
// Canonical form is one views/<type>.spec.json beside each renderer, because
// emission only removes drift when the declaration lives in the same directory as
// the code it describes — so the change that adds a capability physically touches
// the file that documents it.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
)

// Where the generated reference is committed. Read (never written) by -status, so
// it can say when the skill went stale.
const generatedRefPath = ".claude/skills/board/references/reference.generated.md"

// The control declarations, emitted as a module the renderers import.
//
// Generated rather than fetched at runtime, deliberately. The shell already pulls
// /capabilities at boot for the help panel, and async is fine there — a help panel
// that fills in 50ms later is invisible. Button LABELS are not: they would render
// from a fallback and then visibly re-label, and a renderer that mounts before the
// fetch resolves would draw the wrong thing. Emitting a module keeps the lookup
// synchronous, keeps it embedded in the binary like every other asset, and reuses
// the drift check this file already has.
const generatedControlsPath = "views/controls.generated.js"

type stateField struct {
	Type string `json:"type"`
	Doc  string `json:"doc"`
}

// componentSpec describes one entry in a renderer's own inner vocabulary. Only
// `ui` has one — its state is a component TREE, so declaring the tab's state
// fields says almost nothing about whether a write will render. See checkUITree.
type componentSpec struct {
	Doc string `json:"doc,omitempty"`
	// Props this component reads. A prop not listed here is stored and ignored.
	Props []string `json:"props"`
	// ItemProps declares the shape of the objects inside an array prop, for the
	// props whose element shape is fixed: kv pairs, checklist items, tabs panels,
	// table columns. Keyed by the prop name.
	ItemProps map[string][]string `json:"itemProps,omitempty"`
}

// controlSpec is one thing the human can press, declared beside the renderer
// that draws it.
//
// This is the half of the manifest that used to have no consumer. State fields
// never drift far, because -apply READS their declaration — it is load-bearing,
// so a wrong one produces a wrong warning and somebody fixes it. `gestures` only
// fed prose, so nothing broke when it went stale, and `table` shipped a
// delete-row button documented nowhere while SKILL.md advertised the feature.
//
// Declaring controls here gives that half a consumer: views/controls.js renders
// FROM this, so a control with no declaration renders as a visible marker. The
// label on screen and the doc an agent reads are now one edit.
//
// A LIST, not a map, because unlike state fields these have an ORDER and the
// order is meaningful: it is the order they appear in the toolbar, and it is what
// the help panel shows a human. Sorted alphabetically by id instead, markup's
// twelve read "Colour ▾, Clear marks, ✕, Ellipse…" — deterministic and useless. A
// map cannot carry that, so the declaration carries it.
//
// It also means reordering a spec changes capsHash, which is correct: the order
// is part of the described surface now.
type controlSpec struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Title string `json:"title,omitempty"`
	Doc   string `json:"doc"`
}

type typeSpec struct {
	Type  string                `json:"type"`
	Label string                `json:"label"`
	Blurb string                `json:"blurb"`
	Since int                   `json:"since,omitempty"`
	Init  json.RawMessage       `json:"init,omitempty"`
	State map[string]stateField `json:"state,omitempty"`
	// Controls the renderer draws, in the order they appear. Rendered from, not
	// merely described.
	Controls []controlSpec `json:"controls,omitempty"`
	// Gestures is what is LEFT once controls carry themselves: pointer and
	// keyboard behaviour with no element behind it — drag, drop, wheel,
	// double-click, right-click, type-and-it-saves.
	//
	// Deliberately unverifiable, and worth saying out loud so nobody builds a
	// check that pretends otherwise. A control can be asserted three ways (it goes
	// through the helper, its declaration is used, it resolves at runtime) because
	// it is a thing in the DOM. "drag one node ONTO another to reparent" is a
	// behaviour spread across pointer handlers; no sweep can confirm it exists,
	// and none can confirm this sentence still describes it. It stays prose,
	// reviewed by people. That is a smaller surface than it used to be — entries
	// that merely restated a button were pruned when controls gained their own
	// doc — which is the point: the unverifiable half should be as small as the
	// truth allows, not padded with things that could have been checked.
	Gestures []string `json:"gestures,omitempty"`
	// Components and CommonProps describe a renderer whose state is a tree of
	// nodes rather than a flat bag of fields.
	Components  map[string]componentSpec `json:"components,omitempty"`
	CommonProps []string                 `json:"commonProps,omitempty"`
	// Tones and Colors are the palettes these renderers accept BY NAME. Declared
	// because they are a vocabulary agents write, exactly like the component
	// catalog — and because both fail quietly: an unknown `ui` tone draws the
	// default and an unknown `markup` colour resolves to an undefined custom
	// property, so neither renders an error, and the human sees plain text where a
	// colour was intended. Renaming --claude to --agent made that concrete.
	Tones  []string   `json:"tones,omitempty"`
	Colors []string   `json:"colors,omitempty"`
	Keys   [][]string `json:"keys,omitempty"`
	Notes  []string   `json:"notes,omitempty"`
}

type flagSpec struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
	Def  string `json:"default,omitempty"`
}

type routeSpec struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

type manifest struct {
	App    string      `json:"app"`
	Schema int         `json:"schema"`
	Hash   string      `json:"capsHash"`
	Types  []typeSpec  `json:"types"`
	Flags  []flagSpec  `json:"flags"`
	Routes []routeSpec `json:"routes"`
}

// The schema version the renderers are written against. Kept here rather than
// only in board.html so the manifest can state it without parsing JavaScript.
const schemaVersion = 3

// Routes are declared once, here, and reported. The switch in route() is still
// the implementation — this is the description of it, and the smoke test asserts
// every path listed answers.
var declaredRoutes = []routeSpec{
	{"GET", "/board.json", "current state"},
	{"POST", "/board.json", "write, compare-and-set (__base, __origin, __by)"},
	{"GET", "/events", "SSE: state changes, waiter count, and the UI signature"},
	{"GET", "/health", "who owns this port, and which binary is serving"},
	{"GET", "/capabilities", "this manifest"},
	{"GET", "/tab/<id>/html", "one html tab as a standalone sandboxed document"},
	{"GET", "/wait", "long poll: block until poked or until a predicate matches"},
	{"POST", "/poke", "release every waiting session"},
	{"GET", "/waiters", "who is waiting right now"},
	{"GET", "/journal", "recent accepted writes, with the previous state of each changed tab"},
	{"GET", "/watch", "those writes as JSON lines, as they happen"},
	{"POST", "/log", "append output to a tab's sidecar log"},
	{"GET", "/log", "the tail of one"},
	{"POST", "/upload", "an image pasted or dropped by the human"},
	{"GET", "/uploads/<file>", "serve one, from disk"},
}

// loadSpecs reads every views/*.spec.json out of the embedded assets. Embedded,
// not from disk, so the manifest answers on a fresh checkout and from a copied
// binary — the same property that makes -status work anywhere.
func loadSpecs(assets fs.FS) ([]typeSpec, error) {
	entries, err := fs.ReadDir(assets, "views")
	if err != nil {
		return nil, err
	}
	out := []typeSpec{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".spec.json") {
			continue
		}
		body, err := fs.ReadFile(assets, path.Join("views", e.Name()))
		if err != nil {
			return nil, err
		}
		var spec typeSpec
		if err := json.Unmarshal(body, &spec); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if spec.Type == "" {
			return nil, fmt.Errorf("%s: no \"type\"", e.Name())
		}
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out, nil
}

func collectFlags() []flagSpec {
	out := []flagSpec{}
	flag.VisitAll(func(f *flag.Flag) {
		out = append(out, flagSpec{Name: f.Name, Doc: f.Usage, Def: f.DefValue})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func buildManifest(assets fs.FS) (manifest, error) {
	specs, err := loadSpecs(assets)
	if err != nil {
		return manifest{}, err
	}
	m := manifest{App: "board", Schema: schemaVersion, Types: specs, Flags: collectFlags(), Routes: declaredRoutes}
	m.Hash = capsHash(m)
	return m, nil
}

// capsHash fingerprints the DESCRIBED SURFACE, not the source bytes.
//
// Deliberately not reusing uiSig (reload.go): that hashes file contents, so a
// whitespace edit in dag.js would declare the skill stale and train the reader to
// ignore the warning. json.Marshal sorts map keys, so the same surface always
// produces the same hash.
func capsHash(m manifest) string {
	m.Hash = ""
	body, err := json.Marshal(m)
	if err != nil {
		return "unknown"
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:4])
}

/* ---------- generated module ---------- */

// controlsModule emits views/controls.generated.js. Facts only, like the markdown
// reference: the shapes come straight from the specs, so the file has no judgment
// in it and regenerating is never a decision.
func controlsModule(m manifest) string {
	var b strings.Builder
	b.WriteString("// GENERATED by ./board -capabilities — do not edit.\n")
	fmt.Fprintf(&b, "// capsHash: %s   schema: v%d\n", m.Hash, m.Schema)
	b.WriteString("//\n")
	b.WriteString("// Every control a renderer draws, declared in views/<type>.spec.json and emitted\n")
	b.WriteString("// here so controls.js can look one up synchronously. Regenerate with `make caps`;\n")
	b.WriteString("// the suite fails if this drifts from the specs.\n\n")
	b.WriteString("export const CONTROLS = {\n")
	for _, t := range m.Types {
		if len(t.Controls) == 0 {
			continue
		}
		// An object here, keyed by id: this file is only ever a lookup, so it wants
		// O(1) and not order. The ORDERED form reaches the help panel through
		// /capabilities, which serves the list as declared.
		fmt.Fprintf(&b, "  %s: {\n", jsKey(t.Type))
		for _, c := range t.Controls {
			fmt.Fprintf(&b, "    %s: { label: %s, title: %s },\n", jsKey(c.ID), jsString(c.Label), jsString(c.Title))
		}
		b.WriteString("  },\n")
	}
	b.WriteString("};\n\n")

	// The palettes, for the same reason as the controls: the renderer builds its
	// swatches and its tone lookup from the declaration, so the list an agent is
	// told about and the list the code accepts cannot be two lists.
	b.WriteString("export const PALETTES = {\n")
	for _, t := range m.Types {
		if len(t.Tones) == 0 && len(t.Colors) == 0 {
			continue
		}
		names := t.Tones
		if len(names) == 0 {
			names = t.Colors
		}
		quoted := make([]string, 0, len(names))
		for _, n := range names {
			quoted = append(quoted, jsString(n))
		}
		fmt.Fprintf(&b, "  %s: [%s],\n", jsKey(t.Type), strings.Join(quoted, ", "))
	}
	b.WriteString("};\n")
	return b.String()
}

// jsKey quotes an object key only when it is not a plain identifier, so the
// generated file reads like something a person would have written.
func jsKey(s string) string {
	plain := s != ""
	for i, r := range s {
		if !(r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')) {
			plain = false
			break
		}
	}
	if plain {
		return s
	}
	return jsString(s)
}

// jsString emits a JS string literal. json.Marshal is the right tool: it escapes
// quotes, backslashes and control characters, and these labels carry real
// typography (— ▭ ✕ ▾) that must survive verbatim.
func jsString(s string) string {
	out, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(out)
}

/* ---------- markdown ---------- */

// The generated half of the reference. Facts only: what exists, what it accepts,
// what the human can do in it. Judgment stays in the authored files, which is why
// this one ends by pointing at them.
func manifestMarkdown(m manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- GENERATED by ./board -capabilities -format md — do not edit.\n")
	fmt.Fprintf(&b, "     capsHash: %s   schema: v%d\n", m.Hash, m.Schema)
	fmt.Fprintf(&b, "     Regenerate with `make caps`. The authored files carry the judgment;\n")
	fmt.Fprintf(&b, "     this one carries the facts, which are the half that drifts. -->\n\n")
	fmt.Fprintf(&b, "# Board capabilities, as the binary reports them\n\n")
	fmt.Fprintf(&b, "`capsHash %s` · schema v%d · %d tab types\n\n", m.Hash, m.Schema, len(m.Types))
	fmt.Fprintf(&b, "If this file disagrees with the board, it is stale and `./board -status` will\n")
	fmt.Fprintf(&b, "have said so. Run `make caps`.\n\n")

	fmt.Fprintf(&b, "## Tab types\n\n")
	for _, t := range m.Types {
		fmt.Fprintf(&b, "### %s — %s\n\n%s\n\n", t.Type, t.Label, t.Blurb)
		if len(t.State) > 0 {
			fmt.Fprintf(&b, "| state field | type | meaning |\n|---|---|---|\n")
			keys := make([]string, 0, len(t.State))
			for k := range t.State {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&b, "| `%s` | %s | %s |\n", k, t.State[k].Type, t.State[k].Doc)
			}
			b.WriteString("\n")
		}
		if len(t.Controls) > 0 {
			// In declared order, which is the order they appear on screen — a
			// reference the reader can follow along the toolbar with.
			fmt.Fprintf(&b, "| control | label | what pressing it does |\n|---|---|---|\n")
			for _, c := range t.Controls {
				label := c.Label
				if label == "" {
					label = "—"
				}
				fmt.Fprintf(&b, "| `%s` | %s | %s |\n", c.ID, label, c.Doc)
			}
			b.WriteString("\n")
		}
		if len(t.Components) > 0 {
			fmt.Fprintf(&b, "Every node also takes: %s.\n\n", strings.Join(t.CommonProps, ", "))
			fmt.Fprintf(&b, "| component | props | what it draws |\n|---|---|---|\n")
			names := make([]string, 0, len(t.Components))
			for name := range t.Components {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				c := t.Components[name]
				props := strings.Join(c.Props, ", ")
				if props == "" {
					props = "—"
				}
				for _, prop := range c.Props {
					if item, fixed := c.ItemProps[prop]; fixed {
						props = strings.Replace(props, prop,
							fmt.Sprintf("%s[{%s}]", prop, strings.Join(item, ", ")), 1)
					}
				}
				fmt.Fprintf(&b, "| `%s` | %s | %s |\n", name, props, c.Doc)
			}
			b.WriteString("\n")
		}
		if len(t.Init) > 0 {
			fmt.Fprintf(&b, "A fresh tab starts as `%s`.\n\n", strings.TrimSpace(string(t.Init)))
		}
		if len(t.Gestures) > 0 {
			// Worded to say what this list IS, now that the buttons above carry
			// themselves: pointer and keyboard behaviour with no element behind it.
			// Unlike the control table, nothing can verify these — they are prose,
			// reviewed by people.
			fmt.Fprintf(&b, "Beyond the buttons, the human can: %s.\n\n", strings.Join(t.Gestures, "; "))
		}
		if len(t.Keys) > 0 {
			fmt.Fprintf(&b, "Keys: ")
			parts := make([]string, 0, len(t.Keys))
			for _, k := range t.Keys {
				if len(k) == 2 {
					parts = append(parts, fmt.Sprintf("`%s` %s", k[0], k[1]))
				}
			}
			fmt.Fprintf(&b, "%s.\n\n", strings.Join(parts, " · "))
		}
		for _, n := range t.Notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
		if len(t.Notes) > 0 {
			b.WriteString("\n")
		}
	}

	fmt.Fprintf(&b, "## Endpoints\n\n| route | purpose |\n|---|---|\n")
	for _, r := range m.Routes {
		fmt.Fprintf(&b, "| `%s %s` | %s |\n", r.Method, r.Path, r.Purpose)
	}

	fmt.Fprintf(&b, "\n## Flags\n\n| flag | default | meaning |\n|---|---|---|\n")
	for _, f := range m.Flags {
		def := f.Def
		if def == "" {
			def = "—"
		}
		fmt.Fprintf(&b, "| `-%s` | `%s` | %s |\n", f.Name, def, f.Doc)
	}

	fmt.Fprintf(&b, "\n---\n\nWhat to DO with all this — when to reach for which type, how to write safely,\n")
	fmt.Fprintf(&b, "multi-session etiquette — is in [SKILL.md](../SKILL.md) and\n")
	fmt.Fprintf(&b, "[capabilities.md](capabilities.md). Those are authored on purpose: judgment does\n")
	fmt.Fprintf(&b, "not drift, facts do.\n")
	return b.String()
}

/* ---------- CLI ---------- */

// capsCLI needs no running server: it reads the embedded specs and the flag
// registry. A fresh checkout, a server that will not start, another session
// holding the port — the manifest still answers.
func capsCLI(assets fs.FS, format, only string, check bool) (int, error) {
	m, err := buildManifest(assets)
	if err != nil {
		return 1, err
	}

	if only != "" {
		for _, t := range m.Types {
			if t.Type == only {
				m.Types = []typeSpec{t}
				m.Flags, m.Routes = nil, nil
				break
			}
		}
		if len(m.Types) != 1 {
			return 1, fmt.Errorf("no such tab type %q", only)
		}
	}

	if check {
		// The generated MODULE is checked first and unconditionally: unlike the
		// skill reference, it is not optional. The renderers import it, so a stale
		// one means buttons whose labels disagree with their declarations — a
		// wrong screen, not merely wrong documentation.
		if got, err := os.ReadFile(generatedControlsPath); err == nil {
			if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace([]byte(controlsModule(m)))) {
				fmt.Printf("stale: %s no longer matches the specs (capsHash %s) — run `make caps`\n",
					generatedControlsPath, m.Hash)
				return 1, nil
			}
		}

		want := manifestMarkdown(m)
		got, err := os.ReadFile(generatedRefPath)
		if err != nil {
			// No reference HERE is not drift — a project that uses the board
			// without copying the skill has nothing to be stale. Only a reference
			// that exists and disagrees is a failure. (Found by running the binary
			// in a bare project: the check failed where there was nothing to check.)
			fmt.Printf("no skill reference in this project (capsHash %s) — nothing to check\n", m.Hash)
			return 0, nil
		}
		if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace([]byte(want))) {
			fmt.Printf("stale: %s no longer matches the binary (capsHash %s) — run `make caps`\n",
				generatedRefPath, m.Hash)
			return 1, nil
		}
		fmt.Printf("current: %s matches capsHash %s\n", generatedRefPath, m.Hash)
		return 0, nil
	}

	if format == "md" {
		fmt.Print(manifestMarkdown(m))
		return 0, nil
	}
	if format == "js" {
		fmt.Print(controlsModule(m))
		return 0, nil
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return 1, err
	}
	fmt.Println(string(body))
	return 0, nil
}

// stampedHash is the capsHash the committed reference was generated for, or ""
// when there is no reference to compare against. Never fails: a missing file is
// a normal state, not an error.
func stampedHash() string {
	body, err := os.ReadFile(generatedRefPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n")[:6] {
		if i := strings.Index(line, "capsHash:"); i >= 0 {
			return strings.TrimSpace(strings.Fields(line[i+len("capsHash:"):])[0])
		}
	}
	return ""
}

// capsStatusLine is what -status prints. SKILL.md already tells an agent to run
// -status first, so a staleness warning lands in a command it already runs, on
// its first interaction with the board, with no new discipline required.
func capsStatusLine(assets fs.FS) string {
	m, err := buildManifest(assets)
	if err != nil {
		return ""
	}
	stamped := stampedHash()
	if stamped == "" {
		return fmt.Sprintf("  caps    %s", m.Hash)
	}
	if stamped != m.Hash {
		return fmt.Sprintf("  caps    %s   ⚠ skill reference generated for %s — run `make caps`", m.Hash, stamped)
	}
	return fmt.Sprintf("  caps    %s   (skill reference current)", m.Hash)
}

func (s *server) handleCapabilities(w http.ResponseWriter, _ *http.Request) {
	m, err := buildManifest(s.assets)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot build the manifest"})
		return
	}
	writeJSON(w, http.StatusOK, m)
}

/* ---------- write-time validation ---------- */

// The failure mode this whole section exists for: `ui` fails silently and
// successfully. `-apply` prints "applied", exit 0, whatever you wrote — and the
// human finds the empty panel on their screen before the agent hears anything.
// That inverts who finds out first, and the agent is the only one of the two
// still holding the context to fix it. Every check here moves one detection back
// to the write.
//
// All of them warn and none of them refuse, for the reason the original check
// gave: a spec can lag its renderer, and a board that rejects writes because its
// own documentation is behind would be worse than one that documents late.

// wrongVersion catches a document declaring a schema version this board does not
// write. The server stamps the right one regardless (see postState), so this is
// purely so the AGENT hears about it: the stale source it copied from is still
// findable while the context that copied it is still alive.
//
// Absent is not wrong — omitting `version` is the correct thing for a caller to
// do, since the server owns it.
func wrongVersion(raw []byte) string {
	var doc struct {
		Version any `json:"version"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.Version == nil {
		return ""
	}
	if n, ok := doc.Version.(float64); ok && int(n) == schemaVersion {
		return ""
	}
	return fmt.Sprintf("document says \"version\": %v, but this board writes version %d — stamped to %d, so it will render. "+
		"Whatever you copied that from is stale; the server owns this field, so do not set it at all",
		doc.Version, schemaVersion, schemaVersion)
}

// writeWarnings compares what a write actually contains against what the
// renderers declare they read. An agent setting state.foo on a kanban gets told,
// instead of watching nothing happen and reporting success.
//
// It descends, which the first version did not: it read each tab's state as a
// flat map and checked only the top-level keys, so the two surfaces where a
// mistake is most likely — a `ui` component tree and a `stack` block's nested
// state — got no checking at all. Both of the mistakes that prompted this were in
// a ui tree, and both applied cleanly with nothing on stderr.
func writeWarnings(assets fs.FS, raw []byte) []string {
	specs, err := loadSpecs(assets)
	if err != nil {
		return nil
	}
	byType := map[string]typeSpec{}
	for _, spec := range specs {
		byType[spec.Type] = spec
	}

	var doc struct {
		Tabs []struct {
			ID    string         `json:"id"`
			Type  string         `json:"type"`
			State map[string]any `json:"state"`
		} `json:"tabs"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}

	out := []string{}
	for _, tab := range doc.Tabs {
		out = append(out, checkTabState(byType, tab.ID, tab.Type, tab.State, 0)...)
	}
	return out
}

// checkTabState checks one tab — or one stack block, which is a tab in every way
// that matters here. `where` is the id to report, so a block reports as
// "bb32/bb37" exactly as the UI and the html route address it.
func checkTabState(byType map[string]typeSpec, where, typeName string, state map[string]any, depth int) []string {
	spec, ok := byType[typeName]
	if !ok {
		return nil
	}
	out := []string{}

	if len(spec.State) > 0 {
		names := make([]string, 0, len(state))
		for name := range state {
			// `actions` and `intents` are shell-level, not per-renderer, and every
			// renderer may carry them.
			if name == "actions" || name == "intents" {
				continue
			}
			if _, declared := spec.State[name]; !declared {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			out = append(out, fmt.Sprintf("%s (%s): state.%s is not declared by the %s renderer — it will be stored and ignored",
				where, typeName, name, typeName))
		}
	}

	if len(spec.Components) > 0 {
		data, _ := state["data"].(map[string]any)
		out = append(out, checkUITree(spec, where, state["root"], data, "root")...)
	}

	// markup marks name their colour the same way a ui node names its tone, and
	// fail the same silent way — worse, in fact: colorVar() builds
	// var(--<whatever>) with no lookup at all, so a wrong name is an undefined
	// custom property and the mark draws with no colour whatsoever.
	if len(spec.Colors) > 0 {
		images, _ := state["images"].([]any)
		for _, raw := range images {
			image, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			for _, group := range []string{"marks", "strokes"} {
				marks, _ := image[group].([]any)
				for _, m := range marks {
					mark, ok := m.(map[string]any)
					if !ok {
						continue
					}
					id, _ := mark["id"].(string)
					if id == "" {
						id = group
					}
					out = append(out, checkPalette(where, id+".color", mark["color"], spec.Colors, "colour")...)
				}
			}
		}
	}

	// A stack's blocks are a second level of (type, state) pairs, and nothing was
	// looking inside them. Nesting is capped at one level by the renderer, so the
	// recursion is too — a deeper block would not mount, which the renderer says
	// on screen.
	if typeName == "stack" && depth < 1 {
		blocks, _ := state["blocks"].([]any)
		for i, raw := range blocks {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := block["id"].(string)
			if id == "" {
				id = fmt.Sprintf("#%d", i)
			}
			blockWhere := where + "/" + id
			for _, key := range sortedKeys(block) {
				switch key {
				case "id", "type", "title", "state":
				default:
					out = append(out, fmt.Sprintf("%s (stack block): %s is not a block field — a block is { id, type, title, state }",
						blockWhere, key))
				}
			}
			blockType, _ := block["type"].(string)
			blockState, _ := block["state"].(map[string]any)
			out = append(out, checkTabState(byType, blockWhere, blockType, blockState, depth+1)...)
		}
	}

	return out
}

// checkUITree walks a `ui` component tree against the declared catalog. Three
// things go wrong here and only the first is visible on screen:
//
//	unknown component type   → renders a dashed marker naming the catalog
//	unknown prop             → renders NOTHING; the component draws empty
//	a {bind} that resolves nowhere → renders NOTHING, same as an empty string
//
// The second and third are why this exists. `kv` with `items`/`{k,v}` instead of
// `pairs`/`{key,value}` produced a titled card wrapping an empty list, applied
// clean, exit 0, twice in a row.
func checkUITree(spec typeSpec, where string, node any, data map[string]any, path string) []string {
	// A bare string is a valid node: build() renders it as a paragraph.
	if _, isText := node.(string); isText {
		return nil
	}
	obj, ok := node.(map[string]any)
	if !ok {
		return nil
	}

	out := []string{}
	typeName, _ := obj["type"].(string)
	component, known := spec.Components[typeName]
	if !known {
		return append(out, fmt.Sprintf("%s (ui): %s.type = %q is not in the catalog — it will render as a dashed marker. %s",
			where, path, typeName, catalogHint(spec)))
	}

	allowed := map[string]bool{}
	for _, name := range spec.CommonProps {
		allowed[name] = true
	}
	for _, name := range component.Props {
		allowed[name] = true
	}

	for _, key := range sortedKeys(obj) {
		if !allowed[key] {
			out = append(out, fmt.Sprintf("%s (ui): %s is a %s, which does not read %q — it will be stored and draw nothing. %s reads: %s",
				where, path, typeName, key, typeName, strings.Join(component.Props, ", ")))
			continue
		}
		out = append(out, checkBind(where, fmt.Sprintf("%s.%s", path, key), obj[key], data)...)
		if key == "tone" {
			out = append(out, checkPalette(where, fmt.Sprintf("%s.tone", path), obj[key], spec.Tones, "tone")...)
		}

		// The element shape of an array prop, where the shape is fixed.
		if itemProps, fixed := component.ItemProps[key]; fixed {
			items, _ := obj[key].([]any)
			for i, raw := range items {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				at := fmt.Sprintf("%s.%s[%d]", path, key, i)
				for _, itemKey := range sortedKeys(item) {
					if !contains(itemProps, itemKey) {
						out = append(out, fmt.Sprintf("%s (ui): %s.%s is not read — a %s %s item is { %s }",
							where, at, itemKey, typeName, key, strings.Join(itemProps, ", ")))
						continue
					}
					out = append(out, checkBind(where, at+"."+itemKey, item[itemKey], data)...)
				}
			}
		}
	}

	for i, child := range childNodes(obj) {
		out = append(out, checkUITree(spec, where, child.node, data, fmt.Sprintf("%s.%s[%d]", path, child.prop, i))...)
	}
	return out
}

type uiChild struct {
	prop string
	node any
}

// childNodes is where the tree branches: `children` on any node, and the
// `children` inside each panel of a `tabs`. Nothing else in a node is a
// component — the rest is data, and walking it would invent findings.
func childNodes(obj map[string]any) []uiChild {
	out := []uiChild{}
	if kids, ok := obj["children"].([]any); ok {
		for _, kid := range kids {
			out = append(out, uiChild{"children", kid})
		}
	}
	if panels, ok := obj["panels"].([]any); ok {
		for _, raw := range panels {
			panel, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if kids, ok := panel["children"].([]any); ok {
				for _, kid := range kids {
					out = append(out, uiChild{"panels.children", kid})
				}
			}
		}
	}
	return out
}

// checkBind resolves a {bind:"a.b"} READ against state.data, the way the renderer
// will. A path that leads nowhere renders as an empty string, which is
// indistinguishable on screen from a value that is genuinely empty.
//
// Only the OBJECT form is a read. `field.bind` and a checklist item's `bind` are
// plain strings naming where to WRITE, so they are not required to exist yet —
// which is why this checks for the object and not for the prop name.
func checkBind(where, at string, value any, data map[string]any) []string {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	bindPath, ok := obj["bind"].(string)
	if !ok {
		return nil
	}
	// Whether the key was FOUND, never whether its value is non-nil. Those are
	// different questions and conflating them cost a false positive on the first
	// real tree this ran against: `demo.n` is an empty number field, initialised to
	// JSON null on purpose, and a checker that calls correct state a mistake is
	// worse than no checker — it is the noise that teaches people to skip stderr.
	var cursor any = data
	found := true
	for _, key := range strings.Split(bindPath, ".") {
		step, isMap := cursor.(map[string]any)
		if !isMap {
			found = false
			break
		}
		if cursor, found = step[key]; !found {
			break
		}
	}
	if found {
		return nil
	}
	return []string{fmt.Sprintf("%s (ui): %s binds to data.%s, which is not in state.data — it will render empty",
		where, at, bindPath)}
}

// checkPalette catches a colour named by a word the renderer does not know.
//
// Built for the --claude to --agent rename, which is exactly the shape of failure
// worth catching: the word was valid on Tuesday and meaningless on Wednesday, and
// NOTHING on either side says so. A ui tone falls back to the default, a markup
// mark resolves an undefined custom property — both render, both look deliberate,
// and the only person who finds out is whoever notices the colour is missing.
//
// Empty is not wrong: no tone means "the default", which is a normal thing to
// mean.
func checkPalette(where, at string, value any, allowed []string, kind string) []string {
	name, ok := value.(string)
	if !ok || name == "" {
		return nil
	}
	if contains(allowed, name) {
		return nil
	}
	return []string{fmt.Sprintf("%s: %s = %q is not a %s this board has — it will draw the default. Available: %s",
		where, at, name, kind, strings.Join(allowed, ", "))}
}

func catalogHint(spec typeSpec) string {
	names := make([]string, 0, len(spec.Components))
	for name := range spec.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	return "the catalog holds: " + strings.Join(names, ", ")
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, want string) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}
