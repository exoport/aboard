// theme.go — the board's two palettes, and the project's right to patch them.
//
// Three facts hold this file together, and they are all consequences of one
// rule the board has had since it shipped: COLOURS ONLY FROM TOKENS, stated
// once. app.css is where they are stated. This file reads them; it never
// restates them.
//
//  1. The token NAMES are parsed out of app.css, not listed in Go. A list here
//     would be a second declaration of the palette, and a second declaration is
//     a thing that drifts — which is exactly the failure the html tab's
//     hand-copied `:root` block already had once (see htmltab.go), where five
//     tokens were simply missing and a widget naming one got no colour and no
//     warning. So `aboard capabilities` reports what the stylesheet says, and a
//     token added to app.css is a token theme.json accepts on the next build.
//
//  2. There are exactly TWO variants and they must declare the SAME SET. Dark
//     is `:root` and is the default; light is `:root[data-theme="light"]`. A
//     token present in one and forgotten in the other renders as the dark value
//     on a light board — silently, because a custom property that is not
//     redeclared simply inherits. TestBothThemesDeclareTheSameTokens is what
//     stops that shipping.
//
//  3. `.aboard/theme.json` is a PATCH, never a replacement. A project that wants
//     a house style says which handful of tokens it disagrees with; everything
//     it does not mention keeps the built-in value. A replacement would mean a
//     project's theme file silently losing every token added after it was
//     written — the same drift as (1), moved into the user's tree where nothing
//     can check it.
//
// The file is CONTENT, not runtime: it sits at `.aboard/theme.json` beside the
// board document and the uploads, because a house style is meant to be committed
// by the projects that want one. (This repo gitignores `.aboard/` wholesale, so
// its own theme file — if it ever has one — is nobody else's business.)

package aboard

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
)

// ThemeDark and ThemeLight are the two variant names, in the JSON, in the URL
// and on the wire. Named once because five files spell them.
const (
	ThemeDark  = "dark"
	ThemeLight = "light"
)

// themeVersion is the shape `.aboard/theme.json` is written against.
const themeVersion = 1

// lightSelector is the attribute selector the light variant hangs off, in
// app.css and in everything generated from it. `[data-theme]` on the ROOT
// element rather than a `prefers-color-scheme` media query, deliberately: the
// choice belongs to the viewer, who makes it in the page, and to an embedder
// handing the board its host's theme — neither of which a media query can
// express. The default (no attribute at all) is therefore dark.
const lightSelector = `:root[data-theme="light"]`

// Theme is `.aboard/theme.json`: a project's patch over the built-in palettes.
//
// Every field is optional. `Default` decides which variant a viewer with nothing
// stored boots into; the two maps carry token overrides, keyed by the same
// `--token` names app.css declares.
type Theme struct {
	Version int               `json:"version"`
	Default string            `json:"default,omitempty"`
	Dark    map[string]string `json:"dark,omitempty"`
	Light   map[string]string `json:"light,omitempty"`
	// Warnings is what was wrong with the file, in the same voice `apply` uses
	// for a colour name the board does not have. It rides the value rather than
	// being returned beside it because three readers need it — `status`, the
	// serve log and `GET /theme.json` — and a warning only one of them sees is a
	// warning the person who can fix it does not.
	Warnings []string `json:"warnings,omitempty"`
}

// DefaultKind is the variant a viewer with nothing stored starts in: whatever
// the theme file asked for, and dark when it asked for nothing.
func (t *Theme) DefaultKind() string {
	if t != nil && t.Default == ThemeLight {
		return ThemeLight
	}
	return ThemeDark
}

// themeValueRe is what a token value may be.
//
// Validated rather than sanitised, and stricter than CSS, because these strings
// are SPLICED — into the shell's inline `<style>`, into a `<script>` that hands
// the page the same object, and into the html frame's own document. Nothing that
// matches this can close a tag or leave a string: no `<`, no quote, no
// backslash, no brace, no semicolon.
//
// Three forms: a hex colour, a bare CSS colour keyword, or a function call whose
// arguments are numbers, separators and units. That covers `#0b0b0b`,
// `rebeccapurple`, `rgb(10 10 10 / 0.6)` and `oklch(0.7 0.1 250)`, and refuses
// `url(...)`, `var(...)` chains with quotes in them, and anything with markup in
// it. A value CSS would reject anyway (a misspelt keyword) is simply dropped by
// the browser, which is the same outcome as not writing it — that is a mistake
// this file does not need to catch.
var themeValueRe = regexp.MustCompile(`^(#[0-9A-Fa-f]+|[A-Za-z][A-Za-z0-9-]{0,31}|[a-z-]{1,20}\([A-Za-z0-9 ,./%+-]{0,100}\))$`)

// hexLengths are the four hex forms CSS has. The regexp above cannot count, and
// `#12345` is a value a browser drops silently.
var hexLengths = map[int]bool{3: true, 4: true, 6: true, 8: true}

func validThemeValue(v string) bool {
	if v == "" || len(v) > 120 || !themeValueRe.MatchString(v) {
		return false
	}
	if strings.HasPrefix(v, "#") {
		return hexLengths[len(v)-1]
	}
	return true
}

// LoadTheme reads `.aboard/theme.json`, if there is one, and validates it
// against the tokens app.css declares.
//
// It NEVER fails: an absent file is nil with no warnings, and an unreadable or
// unparseable one is nil with a warning saying so. A board that came up with no
// colours because somebody left a trailing comma in a config file would be the
// worst possible trade — the built-in theme is always a correct answer, and the
// warning is what reaches the person who can fix it.
//
// The assets are the same fs.FS everything else reads app.css from, so `--dev`
// validates against the stylesheet on disk and the embedded tree answers
// everywhere else.
func LoadTheme(root Root, assets fs.FS) *Theme {
	path := root.ThemeFile()
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return &Theme{Version: themeVersion, Warnings: []string{
			fmt.Sprintf("theme.json: %s could not be read (%v) — using the built-in theme", path, err),
		}}
	}
	var got Theme
	if err := json.Unmarshal(body, &got); err != nil {
		return &Theme{Version: themeVersion, Warnings: []string{
			fmt.Sprintf("theme.json: %s is not valid JSON (%v) — using the built-in theme", path, err),
		}}
	}
	got.Warnings = nil // never taken from the file
	got.validate(themeTokenNames(assets))
	return &got
}

// validate drops what cannot be used and says why. Dropping rather than
// refusing, for the same reason `apply` warns instead of failing: the rest of
// the file is good, and a house style that half-applies with a named mistake is
// better than a board that ignores the file entirely and says nothing.
func (t *Theme) validate(tokens []string) {
	known := make(map[string]bool, len(tokens))
	for _, name := range tokens {
		known[name] = true
	}
	if t.Version != 0 && t.Version != themeVersion {
		// Warned about, not refused. The content is still a set of token
		// overrides, and failing a good file over a number the caller had no way
		// to check is the worse half of the trade — the same call the write path
		// made when `version` became server-stamped rather than rejected.
		t.Warnings = append(t.Warnings, fmt.Sprintf(
			"theme.json: version = %d is not one this board knows — it understands %d, and is applying the file anyway",
			t.Version, themeVersion))
	}
	if t.Default != "" && t.Default != ThemeDark && t.Default != ThemeLight {
		t.Warnings = append(t.Warnings, fmt.Sprintf(
			"theme.json: default = %q is not a theme this board has — it will boot dark. Available: %s, %s",
			t.Default, ThemeDark, ThemeLight))
		t.Default = ""
	}
	t.Dark = t.checkVariant(ThemeDark, t.Dark, known, tokens)
	t.Light = t.checkVariant(ThemeLight, t.Light, known, tokens)
}

func (t *Theme) checkVariant(kind string, in map[string]string, known map[string]bool, tokens []string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for _, name := range sortedStringKeys(in) {
		value := in[name]
		switch {
		case !known[name]:
			t.Warnings = append(t.Warnings, fmt.Sprintf(
				"theme.json: %s.%s is not a token this board has — it will be ignored. Available: %s",
				kind, name, strings.Join(tokens, ", ")))
		case !validThemeValue(value):
			t.Warnings = append(t.Warnings, fmt.Sprintf(
				"theme.json: %s.%s = %q is not a colour this board will splice — "+
					"give it a hex value, a CSS colour keyword, or a function such as rgb(…)", kind, name, value))
		default:
			out[name] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortedStringKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

/* ---------- the built-in palettes, read out of app.css ---------- */

// themeVariant is one parsed `:root` block: the tokens in declaration order,
// and everything else the block said (`color-scheme`, for one) kept verbatim so
// the html frame inherits it too.
type themeVariant struct {
	body   string
	order  []string
	tokens map[string]string
}

// parseThemeVariants reads both blocks out of app.css. `ok` is false when the
// DARK block cannot be found or does not carry a ground and an ink, which is the
// same fail-closed rule parseRootBlock has always had: a widget with no ground at
// all is worse than one on a stale palette.
//
// A missing LIGHT block is not fatal. It means the board has one theme, which is
// what it had until this file existed — the switch then has nothing to switch to
// and the frame stays dark, rather than the whole page losing its colours.
func parseThemeVariants(assets fs.FS) (dark, light themeVariant, ok bool) {
	body, err := fs.ReadFile(assets, "app.css")
	if err != nil {
		return dark, light, false
	}
	css := stripCSSComments(string(body))

	darkBody, found := findBlock(css, ":root", true)
	if !found || !declaresToken(darkBody, "--bg") || !declaresToken(darkBody, "--text") {
		return dark, light, false
	}
	dark = parseVariant(darkBody)

	if lightBody, found := findBlock(css, lightSelector, false); found {
		light = parseVariant(lightBody)
	}
	return dark, light, true
}

// findBlock returns what is between the braces of the first `selector { … }`.
//
// `bare` is the awkward case and it is why this is not one string search: the
// dark block's selector is `:root`, which is a PREFIX of `:root[data-theme=…]`
// and of `:root:not(…)`, so a plain Index finds the light block first and takes
// its body for the dark one. With `bare` set, a match is only a match when the
// next non-space character is the opening brace.
func findBlock(css, selector string, bare bool) (string, bool) {
	rest := css
	for {
		at := strings.Index(rest, selector)
		if at < 0 {
			return "", false
		}
		after := strings.TrimLeft(rest[at+len(selector):], " \t\r\n")
		if !strings.HasPrefix(after, "{") {
			if !bare {
				return "", false
			}
			rest = rest[at+len(selector):]
			continue
		}
		inner, _, closed := strings.Cut(after[1:], "}")
		if !closed || strings.Contains(inner, "{") {
			return "", false
		}
		return strings.TrimRight(strings.TrimLeft(inner, "\r\n"), " \t\r\n"), true
	}
}

// parseVariant splits a block body into declarations. Crude on purpose: it is
// reading a stylesheet this repo owns, one declaration per line, and anything it
// cannot split is left in `body` — which is what the frame actually splices.
func parseVariant(body string) themeVariant {
	v := themeVariant{body: body, tokens: map[string]string{}}
	for _, field := range strings.FieldsFunc(body, func(r rune) bool { return r == ';' || r == '\n' }) {
		name, value, found := strings.Cut(strings.TrimSpace(field), ":")
		name = strings.TrimSpace(name)
		if !found || !strings.HasPrefix(name, "--") {
			continue
		}
		if _, seen := v.tokens[name]; !seen {
			v.order = append(v.order, name)
		}
		v.tokens[name] = strings.TrimSpace(value)
	}
	return v
}

// themeTokenNames is the palette this board has, as names, sorted.
//
// Sorted rather than in declaration order because this one is a SET being
// reported — the manifest hashes it, `apply`'s sibling warning lists it, and a
// reader looking a name up wants it alphabetical. (Controls are a list in
// toolbar order for the opposite reason: there, the order is the surface.)
func themeTokenNames(assets fs.FS) []string {
	dark, _, ok := parseThemeVariants(assets)
	if !ok {
		return nil
	}
	out := append([]string(nil), dark.order...)
	sort.Strings(out)
	return out
}

/* ---------- what the browser and the frame are handed ---------- */

// themeDeclarations renders one variant's declarations, with the project's
// overrides patched in, as the body of a CSS block.
func themeDeclarations(v themeVariant, override map[string]string) string {
	if len(override) == 0 {
		return v.body
	}
	var b strings.Builder
	b.WriteString(v.body)
	// Appended rather than substituted: a later declaration of the same custom
	// property wins inside one block, so the patch needs no rewriting of the
	// original text — and the original stays readable in the served document,
	// which is what somebody reading the frame's source is trying to check.
	for _, name := range sortedStringKeys(override) {
		fmt.Fprintf(&b, "\n    %s: %s;", name, override[name])
	}
	return b.String()
}
