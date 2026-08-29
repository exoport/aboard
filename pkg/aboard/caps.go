// caps.go — the board describes itself, so the skill cannot quietly disagree.
//
//	aboard capabilities                  the whole manifest, as JSON
//	aboard capabilities --format md      the markdown reference (what gets committed)
//	aboard capabilities dag              one type — cheap, for a mid-task lookup
//	aboard capabilities --check          exit 1 if the committed reference is stale
//	GET /capabilities                    the same JSON, for the browser
//
// The problem this exists for: the skill at .claude/skills/aboard/ is a
// hand-maintained copy of the code surface, and it decays every time a renderer
// grows a field. In one day: 9 renderers to 15, 324 to 454 reference lines, all
// written by hand after the fact. A capability the skill has never heard of is
// merely unused; one it describes WRONGLY is expensive — the agent writes
// state.foo, the renderer ignores it, nothing errors, and the agent tells the
// human it did something it did not do.
//
// The precedent is already in the repo, for the other audience: aboard.html keeps
// gestures beside the registry "so the panel cannot drift from what the renderers
// actually do". This extends that principle to the agent-facing skill. One
// declaration, two audiences.
//
// The seam: THE BINARY OWNS FACTS (commands, endpoints, types, state fields,
// gestures), THE SKILL OWNS JUDGMENT (dag vs diagram, ui vs html, tab sprawl,
// multi-session etiquette). Only the first half is generated. The second half is
// the valuable half and no generator can produce it.
//
// Canonical form is one views/<type>.spec.json beside each renderer, because
// emission only removes drift when the declaration lives in the same directory as
// the code it describes — so the change that adds a capability physically touches
// the file that documents it.

package aboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
)

// The control declarations are emitted as a module the renderers import
// (Root.GeneratedControls).
//
// Generated rather than fetched at runtime, deliberately. The shell already pulls
// /capabilities at boot for the help panel, and async is fine there — a help panel
// that fills in 50ms later is invisible. Button LABELS are not: they would render
// from a fallback and then visibly re-label, and a renderer that mounts before the
// fetch resolves would draw the wrong thing. Emitting a module keeps the lookup
// synchronous, keeps it embedded in the binary like every other asset, and reuses
// the drift check this file already has.
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
	// Text names the props carrying this component's DISPLAY text, in reading
	// order. `a|b` is "the first of these that is set", mirroring the renderer's
	// own `value ?? text`; `=p` is "the value in state.data at the path the prop
	// `p` names", which is how a `field`'s answer is reached.
	//
	// Declared rather than tabled in Go because it is a fact about the catalog,
	// and the catalog is declared. `aboard export` is its consumer: checkUITree
	// validates prop NAMES and has no opinion about which prop is the text, so
	// without this the outline would need a second, unchecked copy of the
	// component list living in export.go.
	Text []string `json:"text,omitempty"`
	// Layout marks a component that draws no content of its own — a box its
	// children sit in. It gets no line in an exported outline.
	Layout bool `json:"layout,omitempty"`
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

type routeSpec struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

type manifest struct {
	App    string     `json:"app"`
	Schema int        `json:"schema"`
	Hash   string     `json:"capsHash"`
	Types  []typeSpec `json:"types"`
	// Commands replaces the old `flags` list. It is DECLARED (commands.go), not
	// scraped: under cobra there is no global flag registry to walk, and walking
	// whatever happened to be registered would have made the manifest — and
	// therefore capsHash — depend on which subcommand printed it.
	Commands []Command   `json:"commands"`
	Root     []Flag      `json:"rootFlags,omitempty"`
	Routes   []routeSpec `json:"routes"`
	// Theme is the palette itself: the token names every renderer draws from,
	// and the two variants they are declared in.
	//
	// Reported because it is a VOCABULARY agents write, exactly like the
	// component catalog and the `tones`/`colors` lists — a `.aboard/theme.json`
	// names these, and so does anyone reading a widget's CSS. It is derived from
	// app.css rather than listed here, so the manifest cannot disagree with the
	// stylesheet; `tones` and `colors` stay where they are and are checked
	// AGAINST this set rather than duplicating it (TestEveryDeclaredColourNameIsAToken).
	Theme themeSpec `json:"theme"`
}

// themeSpec is what `aboard capabilities` says about colour.
type themeSpec struct {
	// Tokens are the custom-property names, sorted. The `--` prefix is part of
	// the name: it is what a theme.json and a widget's CSS both write.
	Tokens []string `json:"tokens"`
	// Variants are the theme names, and Default is the one a viewer with nothing
	// stored boots into unless a project's theme.json says otherwise.
	Variants []string `json:"variants"`
	Default  string   `json:"default"`
	// File is where a project puts its own overrides, relative to the root.
	File string `json:"file"`
}

// SchemaVersion is the board-document layout the renderers are written against.
// Kept here rather than only in aboard.html so the manifest can state it without
// parsing JavaScript, and exported because the server stamps it into every write.
//
// It read 3 until 2026-08-28, counting two layout changes made on the spike this
// project was ported from. Nothing was ever released at 1 or 2, so those numbers
// named documents no user could have — a board arriving at "version": 3 invited
// the reader to look for the two earlier shapes, and there is nothing to find.
// The first release is version 1, and the count starts from something shipped.
//
// Reset ONLY because nothing is published. Once a tag exists this number goes up
// and never back: a reader has to be able to tell an old document from a new one,
// and a version that has been two different layouts cannot say which.
const SchemaVersion = 1

// Routes are declared once, here, and reported. The switch in route() is still
// the implementation — this is the description of it, and the smoke test asserts
// every path listed answers.
var declaredRoutes = []routeSpec{
	{http.MethodGet, routeState, "current state"},
	{http.MethodPost, routeState, "write, compare-and-set (__base is the document's rev; __origin, __by, __label). same-origin only"},
	{http.MethodGet, "/events", "SSE: state changes, waiter count, and the UI signature"},
	{http.MethodGet, "/health", "who owns this port, and which binary is serving"},
	{http.MethodGet, "/capabilities", "this manifest"},
	{http.MethodGet, routeTheme, "the project's .aboard/theme.json, validated; 404 when it has none and the built-in palettes apply"},
	{http.MethodGet, "/tab/<id>/html", "one html tab as a standalone sandboxed document"},
	{http.MethodGet, "/wait", "long poll: block until poked or until a predicate matches"},
	{http.MethodPost, "/poke", "release every waiting session"},
	{http.MethodGet, "/waiters", "who is waiting right now"},
	{http.MethodGet, "/journal", "recent accepted writes; `schema` says whether `before` holds each changed tab whole or its bare state"},
	{http.MethodGet, "/history", "one tab's recorded prior versions, newest first (?tab=<id>&limit=N)"},
	{http.MethodGet, "/watch", "those writes as JSON lines, as they happen"},
	{http.MethodPost, "/rendered", "a mount receipt from the browser: control ids drawn, pressed, and any unknown-component markers"},
	{http.MethodPost, routeLog, "append output to a tab's sidecar log"},
	{http.MethodGet, routeLog, "the tail of one"},
	{http.MethodPost, "/upload", "an image pasted or dropped by the human"},
	{http.MethodGet, "/uploads", "list them: url, bytes and mtime"},
	{http.MethodGet, "/uploads/<file>", "serve one, from disk"},
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

func buildManifest(assets fs.FS) (manifest, error) {
	specs, err := loadSpecs(assets)
	if err != nil {
		return manifest{}, err
	}
	m := manifest{
		// AppName, not the host identity: the manifest describes the BOARD, so
		// the same board hosted by ape hashes the same.
		App:      AppName,
		Schema:   SchemaVersion,
		Types:    specs,
		Commands: Commands(),
		Root:     RootFlags(),
		Routes:   declaredRoutes,
		Theme: themeSpec{
			Tokens:   themeTokenNames(assets),
			Variants: []string{ThemeDark, ThemeLight},
			Default:  ThemeDark,
			File:     DirName + "/theme.json",
		},
	}
	m.Hash = capsHash(m)
	return m, nil
}

// unknownHash is what capsHash reports when the manifest will not marshal. It is
// deliberately not "" — an empty hash reads as "not computed yet", and the
// staleness check would then say nothing at all.
const unknownHash = "unknown"

// emDash fills a table cell that would otherwise be empty, so a reader can tell
// "this control has no label" from "this table failed to render".
const emDash = "—"

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
		return unknownHash
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
	b.WriteString("// GENERATED by `aboard capabilities --format js` — do not edit.\n")
	fmt.Fprintf(&b, "// capsHash: %s   schema: v%d\n", m.Hash, m.Schema)
	b.WriteString("//\n")
	b.WriteString("// Every control a renderer draws, declared in views/<type>.spec.json and emitted\n")
	b.WriteString("// here so controls.js can look one up synchronously. Regenerate with `make caps`;\n")
	b.WriteString("// the suite fails if this drifts from the specs.\n\n")
	b.WriteString("export const CONTROLS = {\n")
	for i := range m.Types {
		t := &m.Types[i]
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
	for i := range m.Types {
		t := &m.Types[i]
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

// identRune reports whether r may appear at offset i of a bare JS identifier.
// Digits are legal everywhere but the first position.
func identRune(r rune, i int) bool {
	switch {
	case r == '_' || r == '$':
		return true
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return i > 0
	default:
		return false
	}
}

// jsKey quotes an object key only when it is not a plain identifier, so the
// generated file reads like something a person would have written.
func jsKey(s string) string {
	plain := s != ""
	for i, r := range s {
		if !identRune(r, i) {
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
	fmt.Fprintf(&b, "<!-- GENERATED by `aboard capabilities --format md` — do not edit.\n")
	fmt.Fprintf(&b, "     capsHash: %s   schema: v%d\n", m.Hash, m.Schema)
	fmt.Fprintf(&b, "     Regenerate with `%s capabilities --format md > <this file>`, or `make caps`\n", AppName)
	fmt.Fprintf(&b, "     in aboard's own checkout. The authored files carry the judgment;\n")
	fmt.Fprintf(&b, "     this one carries the facts, which are the half that drifts. -->\n\n")
	fmt.Fprintf(&b, "# aboard capabilities, as the binary reports them\n\n")
	fmt.Fprintf(&b, "`capsHash %s` · schema v%d · %d tab types\n\n", m.Hash, m.Schema, len(m.Types))
	fmt.Fprintf(&b, "If this file disagrees with the board, it is stale and `aboard status` will\n")
	fmt.Fprintf(&b, "have said so. Regenerate it with the binary you have:\n\n")
	fmt.Fprintf(&b, "```bash\n%s capabilities --format md > .claude/skills/%s/references/reference.generated.md\n%s capabilities --check\n```\n\n", AppName, AppName, AppName)
	fmt.Fprintf(&b, "`make caps` does the same and more, but only inside aboard's own checkout —\nmost projects that copied this skill have no such Makefile.\n\n")

	fmt.Fprintf(&b, "## Tab types\n\n")
	for i := range m.Types {
		t := &m.Types[i]
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
					label = emDash
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
					props = emDash
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

	if len(m.Theme.Tokens) > 0 {
		fmt.Fprintf(&b, "## Colour\n\nEvery colour on this board is one of these %d tokens, and nothing else. "+
			"A renderer that takes a colour by NAME (`ui`'s `tone`, `markup`'s `color`) takes the name "+
			"without the `--`; a widget's own CSS writes `var(--name)`.\n\n", len(m.Theme.Tokens))
		fmt.Fprintf(&b, "`%s`\n\n", strings.Join(m.Theme.Tokens, "` · `"))
		fmt.Fprintf(&b, "Two variants — %s — defaulting to `%s`, chosen per viewer and never stored in the board. "+
			"A project may patch either from `%s`; the VALUES are the theme's business, the NAMES are yours.\n\n",
			"`"+strings.Join(m.Theme.Variants, "`, `")+"`", m.Theme.Default, m.Theme.File)
	}

	fmt.Fprintf(&b, "## Endpoints\n\n| route | purpose |\n|---|---|\n")
	for _, r := range m.Routes {
		fmt.Fprintf(&b, "| `%s %s` | %s |\n", r.Method, r.Path, r.Purpose)
	}

	if len(m.Root) > 0 {
		fmt.Fprintf(&b, "\n## Commands\n\nOn the root command, inherited by all of them:\n\n")
		fmt.Fprintf(&b, "| flag | default | meaning |\n|---|---|---|\n")
		for _, f := range m.Root {
			fmt.Fprintf(&b, "| `--%s` | `%s` | %s |\n", f.Name, orDash(f.Def), f.Doc)
		}
		b.WriteString("\n")
	}
	for _, c := range m.Commands {
		writeCommandMarkdown(&b, c, "")
	}

	fmt.Fprintf(&b, "\n---\n\nWhat to DO with all this — when to reach for which type, how to write safely,\n")
	fmt.Fprintf(&b, "multi-session etiquette — is in [SKILL.md](../SKILL.md) and\n")
	fmt.Fprintf(&b, "[capabilities.md](capabilities.md). Those are authored on purpose: judgment does\n")
	fmt.Fprintf(&b, "not drift, facts do.\n")
	return b.String()
}

// writeCommandMarkdown renders one declared command and, recursively, whatever
// it declares beneath it. Depth is carried as a name prefix rather than a
// heading level: `aboard recipes show <name>` is what a reader types, and a
// fourth-level heading for it would say less.
func writeCommandMarkdown(b *strings.Builder, c Command, prefix string) {
	use := strings.TrimSpace(prefix + " " + c.Name)
	if c.Args != "" {
		use += " " + c.Args
	}
	fmt.Fprintf(b, "### `%s %s`\n\n%s\n\n", AppName, use, c.Doc)
	if len(c.Flags) > 0 {
		fmt.Fprintf(b, "| flag | default | meaning |\n|---|---|---|\n")
		for _, f := range c.Flags {
			fmt.Fprintf(b, "| `--%s` | `%s` | %s |\n", f.Name, orDash(f.Def), f.Doc)
		}
		b.WriteString("\n")
	}
	if len(c.Exits) > 0 {
		parts := make([]string, 0, len(c.Exits))
		for _, e := range c.Exits {
			parts = append(parts, fmt.Sprintf("`%d` %s", e.Code, e.Meaning))
		}
		fmt.Fprintf(b, "Exit: %s.\n\n", strings.Join(parts, " · "))
	}
	for _, sub := range c.Subcommands {
		writeCommandMarkdown(b, sub, strings.TrimSpace(prefix+" "+c.Name))
	}
}

/* ---------- CLI ---------- */

// Capabilities needs no running server: it reads the embedded specs and the
// declared command table. A fresh checkout, a server that will not start,
// another session holding the port — the manifest still answers.
func Capabilities(root Root, assets fs.FS, format, only string, check bool, out io.Writer) (int, error) {
	m, err := buildManifest(assets)
	if err != nil {
		return 1, err
	}

	if only != "" {
		for i := range m.Types {
			if m.Types[i].Type == only {
				m.Types = []typeSpec{m.Types[i]}
				m.Commands, m.Root, m.Routes = nil, nil, nil
				break
			}
		}
		if len(m.Types) != 1 {
			return ExitUsage, fmt.Errorf("no such tab type %q", only)
		}
	}

	if check {
		return checkGenerated(root, m, out)
	}

	switch format {
	case "md":
		fmt.Fprint(out, manifestMarkdown(m))
		return ExitOK, nil
	case "js":
		fmt.Fprint(out, controlsModule(m))
		return ExitOK, nil
	case "json", "":
		body, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return ExitFailed, err
		}
		fmt.Fprintln(out, string(body))
		return ExitOK, nil
	default:
		return ExitUsage, fmt.Errorf("unknown --format %q — try json, md or js", format)
	}
}

// orDash renders an empty default as an em dash, so a table column never reads
// as an empty cell that might be a rendering bug.
func orDash(s string) string {
	if s == "" {
		return emDash
	}
	return s
}

// stampedHash is the capsHash the committed reference was generated for, or ""
// when there is no reference to compare against. Never fails: a missing file is
// a normal state, not an error.
func stampedHash(root Root) string {
	body, err := os.ReadFile(root.SkillReference())
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n")[:6] {
		if _, after, found := strings.Cut(line, "capsHash:"); found {
			return strings.TrimSpace(strings.Fields(after)[0])
		}
	}
	return ""
}

func (s *server) handleCapabilities(w http.ResponseWriter, _ *http.Request) {
	m, err := buildManifest(s.assets)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{wireError: "cannot build the manifest"})
		return
	}
	s.writeJSON(w, http.StatusOK, m)
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
	if n, ok := doc.Version.(float64); ok && int(n) == SchemaVersion {
		return ""
	}
	return fmt.Sprintf("document says \"version\": %v, but this board writes version %d — stamped to %d, so it will render. "+
		"Whatever you copied that from is stale; the server owns this field, so do not set it at all",
		doc.Version, SchemaVersion, SchemaVersion)
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
	byType, err := specsByType(assets)
	if err != nil {
		return nil
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
		warningScans.Add(1)
		out = append(out, checkTabState(byType, tab.ID, tab.Type, tab.State, 0)...)
	}
	return out
}

// specsByType is loadSpecs keyed by type, which is the only form either checker
// wants.
func specsByType(assets fs.FS) (map[string]typeSpec, error) {
	specs, err := loadSpecs(assets)
	if err != nil {
		return nil, err
	}
	byType := make(map[string]typeSpec, len(specs))
	for i := range specs {
		byType[specs[i].Type] = specs[i]
	}
	return byType, nil
}

// warningScans counts the TABS whose state the warning checker looked inside.
//
// A counter in production code for the same reason document.go has three: the
// claim being made is about the real write path, and the claim here is the one
// this feature could most easily get wrong. `writeWarnings` walks a whole
// document, which is right for `apply` — the caller submitted the whole thing —
// and wrong on the server, where the write is one edit and the board may be
// thousands of tabs. Worse than the cost: the example board ships a deliberately
// invalid `sparkline` in its gallery, so a whole-document scan would attach that
// warning to EVERY write, on every tab, for ever. A warning that fires
// unconditionally is not a warning, and no suppression mechanism may be added for
// that node (it is a demonstration). Scoping is what keeps it honest, and this
// counter is what proves the scoping.
var warningScans atomic.Int64

// changedTabWarnings runs the same checks over only the tabs a write actually
// touched, keyed by tab id so the browser can put each warning on the tab it is
// about.
//
// The key is the TAB, but the message keeps its own `where` prefix — which for a
// stack block is "ab32/ab37". One wording, in the journal, on stderr and in the
// banner: a warning a reader has seen in a terminal must be recognisable on a
// screen.
func changedTabWarnings(byType map[string]typeSpec, tabs []docTab) map[string][]string {
	var out map[string][]string
	for i := range tabs {
		t := &tabs[i]
		if !t.changed || len(t.State) == 0 {
			continue
		}
		warningScans.Add(1)
		var state map[string]any
		if json.Unmarshal(t.State, &state) != nil {
			// Not an object. The write itself is legal — state is opaque — and a
			// renderer that wanted a bag of fields will show its own emptiness.
			continue
		}
		found := checkTabState(byType, t.ID, t.Type, state, 0)
		if len(found) == 0 {
			continue
		}
		if out == nil {
			out = map[string][]string{}
		}
		out[t.ID] = found
	}
	return out
}

// checkTabState checks one tab — or one stack block, which is a tab in every way
// that matters here. `where` is the id to report, so a block reports as
// "ab32/ab37" exactly as the UI and the html route address it.
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
	if typeName == tabTypeStack && depth < 1 {
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
				case "id", keyType, "title", "state":
				default:
					out = append(out, fmt.Sprintf("%s (stack block): %s is not a block field — a block is { id, type, title, state }",
						blockWhere, key))
				}
			}
			blockType, _ := block[keyType].(string)
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
func checkUITree(spec typeSpec, where string, node any, data map[string]any, nodePath string) []string {
	// A bare string is a valid node: build() renders it as a paragraph.
	if _, isText := node.(string); isText {
		return nil
	}
	obj, ok := node.(map[string]any)
	if !ok {
		return nil
	}

	out := []string{}
	typeName, _ := obj[keyType].(string)
	component, known := spec.Components[typeName]
	if !known {
		return append(out, fmt.Sprintf("%s (ui): %s.type = %q is not in the catalog — it will render as a dashed marker. %s",
			where, nodePath, typeName, catalogHint(spec)))
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
				where, nodePath, typeName, key, typeName, strings.Join(component.Props, ", ")))
			continue
		}
		propPath := nodePath + "." + key
		out = append(out, checkBind(where, propPath, obj[key], data)...)
		if key == "tone" {
			out = append(out, checkPalette(where, propPath, obj[key], spec.Tones, "tone")...)
		}
		out = append(out, checkItemShape(where, obj, key, typeName, propPath, component, data)...)
	}

	for i, child := range childNodes(obj) {
		out = append(out, checkUITree(spec, where, child.node, data, fmt.Sprintf("%s.%s[%d]", nodePath, child.prop, i))...)
	}
	return out
}

// checkItemShape checks the ELEMENT shape of an array prop, where the shape is
// fixed by the declaration. This is the `kv` case that started all of this: the
// prop name was right and every item inside it was wrong, so the component
// rendered its title and an empty list.
func checkItemShape(where string, obj map[string]any, key, typeName, propPath string,
	component componentSpec, data map[string]any,
) []string {
	itemProps, fixed := component.ItemProps[key]
	if !fixed {
		return nil
	}
	out := []string{}
	items, _ := obj[key].([]any)
	for i, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		at := fmt.Sprintf("%s[%d]", propPath, i)
		for _, itemKey := range sortedKeys(item) {
			if !contains(itemProps, itemKey) {
				out = append(out, fmt.Sprintf("%s (ui): %s.%s is not read — a %s %s item is { %s }",
					where, at, itemKey, typeName, key, strings.Join(itemProps, ", ")))
				continue
			}
			out = append(out, checkBind(where, at+"."+itemKey, item[itemKey], data)...)
		}
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
func checkBind(where, at string, value any, data map[string]any) []string {
	dotted, ok := bindPath(value)
	if !ok {
		return nil
	}
	if _, found := lookupBind(dotted, data); found {
		return nil
	}
	return []string{fmt.Sprintf("%s (ui): %s binds to data.%s, which is not in state.data — it will render empty",
		where, at, dotted)}
}

// bindPath returns the path a `{bind:"a.b"}` object names. Only the OBJECT form
// is a read: `field.bind` and a checklist item's `bind` are plain strings naming
// where to WRITE, so they are not required to exist yet.
func bindPath(value any) (string, bool) {
	obj, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	dotted, ok := obj["bind"].(string)
	return dotted, ok
}

// lookupBind walks a dotted path into state.data the way views/ui.js does, and
// reports whether the key was FOUND — never whether its value is non-nil.
//
// Those are different questions and conflating them cost a false positive on the
// first real tree the write checker ran against: `demo.n` is an empty number
// field, initialised to JSON null on purpose, and a checker that calls correct
// state a mistake is worse than no checker — it is the noise that teaches people
// to skip stderr.
//
// Shared with export.go, which asks the same question for a different reason: the
// warning wants "does this resolve", the outline wants "to what".
func lookupBind(dotted string, data map[string]any) (any, bool) {
	var cursor any = data
	found := true
	for key := range strings.SplitSeq(dotted, ".") {
		step, isMap := cursor.(map[string]any)
		if !isMap {
			return nil, false
		}
		if cursor, found = step[key]; !found {
			return nil, false
		}
	}
	return cursor, found
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
	return slices.Contains(list, want)
}

// generatedRecipeIndex is exactly what `recipes index` prints, so the gate and
// the generator cannot disagree. A built-in that does not parse is a build
// defect: report it rather than compare against a table with empty cells.
func generatedRecipeIndex() (string, error) {
	// DefaultInvocation, not the running one: this index is a GENERATED,
	// committed file, so its text must not depend on which host regenerated it.
	built, err := BuiltinRecipes(DefaultInvocation)
	if err != nil {
		return "", err
	}
	for i := range built {
		if !built[i].Valid() {
			return "", fmt.Errorf("built-in recipe %s does not parse: %s", built[i].Path, built[i].Err)
		}
	}
	return RecipeIndexMarkdown(built), nil
}

// checkGenerated is `aboard capabilities --check`: does what is committed still
// match what this binary would emit? THREE files, and they are not the same kind
// of claim, which is why they are checked in this order:
//
//   - the controls module, first and unconditionally, because the renderers
//     IMPORT it: a stale one is a wrong screen, not merely a wrong document;
//   - the recipe index, which is generated from the built-ins compiled into this
//     binary, so it is checkable wherever the file exists;
//   - the skill reference, whose ABSENCE is the "nothing to check" case — a
//     project that copied the binary and not the skill has nothing to be stale.
//
// A file that is present and unreadable is NOT the absent case, and each branch
// says which of the two it hit. Reporting "nothing to check" for a file that
// exists but could not be opened is how a gate stops being one.
func checkGenerated(root Root, m manifest, out io.Writer) (int, error) {
	// The renderers IMPORT the generated controls module, so a stale one means
	// buttons whose labels disagree with their declarations — a wrong screen, not
	// merely wrong documentation.
	// Absent is the normal case everywhere but aboard's own checkout; present and
	// unreadable is not, and it gets the same treatment as the two files below.
	switch got, readErr := os.ReadFile(root.GeneratedControls()); {
	case readErr == nil && !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace([]byte(controlsModule(m)))):
		fmt.Fprintf(out, "stale: %s no longer matches the specs (capsHash %s) — regenerate it with "+
			"`%s capabilities --format js > %s`, or `make caps` in aboard's own checkout\n",
			root.GeneratedControls(), m.Hash, AppName, root.GeneratedControls())
		return ExitFailed, nil
	case readErr != nil && !os.IsNotExist(readErr):
		return ExitFailed, fmt.Errorf("reading %s: %w", root.GeneratedControls(), readErr)
	}

	// The recipe index is generated from the BUILT-IN recipes, so it is checkable
	// wherever the file exists — the third `make caps` artifact, and the one that
	// had no gate at all: renaming a built-in left this table naming a recipe
	// `aboard recipes show` could not find, and nothing anywhere failed.
	//
	// The error is RETURNED, not dropped. It only fires when a built-in recipe
	// does not parse, which is a build defect in this binary — and a gate that
	// answers "current" because it could not work out what current means is the
	// one failure mode a gate must not have.
	want, err := generatedRecipeIndex()
	if err != nil {
		return ExitFailed, err
	}
	switch got, readErr := os.ReadFile(root.SkillRecipes()); {
	case readErr == nil && !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace([]byte(want))):
		fmt.Fprintf(out, "stale: %s no longer matches the built-in recipes — regenerate it with "+
			"`%s recipes index > %s`, or `make caps` in aboard's own checkout\n",
			root.SkillRecipes(), AppName, root.SkillRecipes())
		return ExitFailed, nil
	case readErr != nil && !os.IsNotExist(readErr):
		// Present and unreadable is not the "no skill copied" case.
		return ExitFailed, fmt.Errorf("reading %s: %w", root.SkillRecipes(), readErr)
	}

	got, refErr := os.ReadFile(root.SkillReference())
	if refErr != nil {
		if !os.IsNotExist(refErr) {
			// Present and unreadable is a failure, not "nothing to check".
			return ExitFailed, fmt.Errorf("reading %s: %w", root.SkillReference(), refErr)
		}
		// No reference HERE is not drift — a project that uses the board without
		// copying the skill has nothing to be stale. Only a reference that exists
		// and disagrees is a failure. (Found by running the binary in a bare
		// project: the check failed where there was nothing to check.)
		fmt.Fprintf(out, "no skill reference in this project (capsHash %s) — nothing to check\n", m.Hash)
		return ExitOK, nil
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace([]byte(manifestMarkdown(m)))) {
		// The portable remedy first: this is the message a reader in a project
		// that only COPIED the skill actually sees, and `make caps` is a target
		// in aboard's own checkout — which most such projects do not have. The
		// whole point of the finding was that the file said otherwise.
		fmt.Fprintf(out, "stale: %s no longer matches the binary (capsHash %s) — regenerate it with "+
			"`%s capabilities --format md > %s`, or `make caps` in aboard's own checkout\n",
			root.SkillReference(), m.Hash, AppName, root.SkillReference())
		return ExitFailed, nil
	}
	fmt.Fprintf(out, "current: %s matches capsHash %s\n", root.SkillReference(), m.Hash)
	return ExitOK, nil
}
