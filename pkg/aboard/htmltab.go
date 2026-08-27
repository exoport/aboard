package aboard

import (
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"strings"
)

// The `html` tab type lets an agent ship arbitrary markup, CSS and script — a
// bespoke widget for one task, which no fixed renderer could cover.
//
// That is a large amount of trust, so the blast radius is closed off two ways
// rather than assumed away:
//
//  1. The page is served from here and rendered in an iframe carrying
//     sandbox="allow-scripts" WITHOUT allow-same-origin. Its origin is opaque,
//     so it cannot reach into the board shell's DOM or storage.
//  2. The response carries a Content-Security-Policy with `connect-src 'none'`.
//     This server has no authentication — anything that can make a request to
//     it can read and rewrite the whole board — so blocking network egress from
//     the frame is what actually contains it. Inline script and style are
//     allowed (that is the point); fetch, XHR, WebSocket and form posts are not.
//  3. That same policy leads with `sandbox allow-scripts`, so the opaque origin
//     holds for a STANDALONE fetch too. Rendered in the board's own iframe the
//     sandbox attribute already provided it; opened directly at /tab/<id>/html —
//     which is how a screenshot is taken and how a human checks a widget — the
//     page ran on the board's real origin, with the board's storage reachable
//     from it. This is HARDENING, not an egress fix: `sandbox allow-scripts`
//     blocks popups and form submission but NOT the page navigating itself, and
//     `connect-src 'none'` was and remains the thing that contains it. The two
//     sandboxes intersect rather than conflict, so the framed case is unchanged
//     (asserted: the header a tab serves and the header a stack block serves are
//     byte-identical, and both carry this directive).
//
// State still round-trips: a small bridge exposes `aboard.get()` / `aboard.set()`
// which postMessage to the parent, and the parent writes the tab's `state.data`
// through the normal compare-and-set path.

const htmlTabCSP = "sandbox allow-scripts; " +
	"default-src 'none'; " +
	"script-src 'unsafe-inline' 'unsafe-eval'; " +
	"style-src 'unsafe-inline'; " +
	"img-src data: blob:; " +
	"font-src data:; " +
	"media-src data: blob:; " +
	"connect-src 'none'; " +
	"form-action 'none'; " +
	"base-uri 'none'; " +
	"frame-ancestors " + htmlTabFrameAncestors

// Who may DISPLAY an html tab. Wider than 'self' on purpose, and the reason is
// not obvious enough to leave uncommented:
//
// frame-ancestors is checked against EVERY ancestor in the chain, not just the
// immediate parent. The board is normally viewed in VS Code's Simple Browser,
// which renders the page inside a vscode-webview:// document, so an html tab's
// chain is
//
//	vscode-webview://<uuid>  →  http://localhost:<port>/  →  /tab/<id>/html
//
// With 'self' alone the non-self GRANDPARENT blocks the frame, even though the
// tab's immediate parent is the board itself — so every html tab came up blank in
// the docked browser while aboard.html (which sends no framing header) loaded
// fine. Confirmed headlessly with a cross-origin wrapper around the board: the
// nested case is refused exactly like the direct one.
//
// This is NOT the containment. What actually contains an html tab is
// connect-src 'none' here plus sandbox="allow-scripts" WITHOUT allow-same-origin
// in views/html.js — an opaque origin with no network egress. frame-ancestors
// only decides who may display the frame, which matters because the bridge
// postMessages to its parent; a page that is allowed to embed the tab receives
// whatever the widget sends. Hence a list, not 'none', and hence do not
// "tighten" this back to 'self'.
//
// If a host still gets a blank tab, its webview console names the origin it
// tried to frame from — add it here rather than widening to *.
const htmlTabFrameAncestors = "'self' vscode-webview: vscode-file: https://*.vscode-cdn.net"

const bridgeScript = `<script>
(function () {
  var current = window.__ABOARD_DATA__ || {};
  var listeners = [];
  function post(msg) { try { parent.postMessage(msg, '*'); } catch (e) {} }
  window.aboard = {
    // The persisted object for this tab. Mutating the return value does nothing
    // until set() is called.
    get: function () { return JSON.parse(JSON.stringify(current)); },
    // Persist a new value. The parent writes it into the tab's state.data.
    set: function (next) {
      current = next;
      post({ __aboard: 'set', data: next });
    },
    // Fires when the value changed elsewhere (another viewer, or an agent).
    onData: function (fn) { if (typeof fn === 'function') listeners.push(fn); },
    // Ask the parent to resize the frame to fit the content.
    fit: function () {
      var h = Math.max(
        document.body ? document.body.scrollHeight : 0,
        document.documentElement ? document.documentElement.scrollHeight : 0
      );
      post({ __aboard: 'height', height: h });
    }
  };
  // The project's theme.json overrides, as inline custom properties. Inline wins
  // over both spliced blocks, so the parent need not say which variant they are
  // for; the previous set is taken off first, so a token dropped from the file
  // goes back to its built-in value instead of sticking.
  //
  // setProperty is CSSOM, not string splicing, so nothing here can leave the
  // property it lands in — which is why this is the one place a value crossing
  // the bridge is not re-validated. The server validated it before it was ever
  // written into a document.
  var themed = [];
  function applyThemeTokens(tokens) {
    var root = document.documentElement;
    for (var i = 0; i < themed.length; i++) root.style.removeProperty(themed[i]);
    themed = [];
    if (!tokens || typeof tokens !== 'object') return;
    for (var name in tokens) {
      if (!Object.prototype.hasOwnProperty.call(tokens, name)) continue;
      if (name.slice(0, 2) !== '--' || typeof tokens[name] !== 'string') continue;
      root.style.setProperty(name, tokens[name]);
      themed.push(name);
    }
  }
  window.addEventListener('message', function (e) {
    if (e.source !== parent || !e.data) return;
    // The board switched theme, or the project's theme.json changed. The frame is
    // a separate document, so it has to be handed both halves: WHICH variant (the
    // stylesheet spliced into this page carries both, exactly as app.css does, so
    // that is one attribute) and, when the project has a house style, the token
    // values — because those were spliced in when this document LOADED, and an
    // edit to theme.json does not reach a document that is already open.
    // Authenticated by e.source, like the data message beside it and like the
    // 'active' message going the other way.
    if (e.data.__aboard === 'theme') {
      var kind = e.data.kind === 'light' ? 'light' : 'dark';
      document.documentElement.setAttribute('data-theme', kind);
      applyThemeTokens(e.data.tokens);
      return;
    }
    if (e.data.__aboard !== 'data') return;
    current = e.data.data || {};
    listeners.forEach(function (fn) { try { fn(window.aboard.get()); } catch (err) {} });
  });
  // Deliberately NOT observing the body: the parent resizes the frame in
  // response to fit(), which resizes the body, which would call fit() again —
  // an infinite loop that starves the widget's own rendering. The frame has a
  // generous default height, so fit() is a refinement a widget opts into.
  window.addEventListener('load', function () { try { window.aboard.fit(); } catch (e) {} });
})();
</script>`

// htmlBlock finds one html block inside a stack tab's state.
//
// It reaches into `state` as an anonymous shape rather than adding a typed block
// to tabs.go on purpose: blocks are the STACK RENDERER's vocabulary, and the four
// server-enforced guarantees in tabs.go are all about tabs. Giving the server a
// real opinion about block structure would invite it to enforce things there too,
// and the renderer is the thing that knows.
func htmlBlock(parent *tab, blockID string) (struct {
	Title string
	State json.RawMessage
}, error,
) {
	var out struct {
		Title string
		State json.RawMessage
	}
	if parent.Type != tabTypeStack {
		return out, fmt.Errorf("%s is not a stack, so it has no blocks", parent.ID)
	}
	var st struct {
		Blocks []struct {
			ID    string          `json:"id"`
			Type  string          `json:"type"`
			Title string          `json:"title"`
			State json.RawMessage `json:"state"`
		} `json:"blocks"`
	}
	if len(parent.State) > 0 {
		_ = json.Unmarshal(parent.State, &st)
	}
	for _, block := range st.Blocks {
		if block.ID != blockID {
			continue
		}
		if block.Type != tabTypeHTML {
			return out, fmt.Errorf("%s/%s is a %s block, not an html one", parent.ID, blockID, block.Type)
		}
		out.Title, out.State = block.Title, block.State
		return out, nil
	}
	return out, fmt.Errorf("%s has no block %s", parent.ID, blockID)
}

// serveTabHTML renders one html-type tab as a standalone document.
//
// `tabID` is either a tab id or a stack block's compound "<tabId>/<blockId>".
// views/html.js asks for `/tab/${ctx.tab.id}/html`, and inside a stack that ctx
// reports the compound form — so an html block used to 404 and render as a blank
// frame, with the static markup absent and no error anywhere. Everything else
// about a block already worked: the bridge writes through the block's own ctx,
// so aboard.set() lands in blocks[].state.data on its own.
func (s *server) serveTabHTML(w http.ResponseWriter, r *http.Request, tabID string) {
	raw, err := os.ReadFile(s.stateFile)
	if err != nil {
		http.Error(w, "cannot read state", http.StatusInternalServerError)
		return
	}
	var b board
	if err := json.Unmarshal(raw, &b); err != nil {
		http.Error(w, "state unreadable", http.StatusInternalServerError)
		return
	}

	tabID, blockID, isBlock := strings.Cut(tabID, "/")

	var found *tab
	for i := range b.Tabs {
		if b.Tabs[i].ID == tabID {
			found = &b.Tabs[i]
			break
		}
	}
	if found == nil {
		http.Error(w, "no such tab", http.StatusNotFound)
		return
	}

	name := found.Name
	state := found.State
	if isBlock {
		block, err := htmlBlock(found, blockID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		// The block's title, not the tab's: a stack of four blocks would otherwise
		// give all of them the same document title.
		name, state = block.Title, block.State
		if name == "" {
			name = found.Name
		}
	} else if found.Type != tabTypeHTML {
		http.Error(w, "tab is not an html tab", http.StatusBadRequest)
		return
	}

	var st struct {
		HTML string          `json:"html"`
		Data json.RawMessage `json:"data"`
	}
	if len(state) > 0 {
		_ = json.Unmarshal(state, &st)
	}
	data := "{}"
	if len(st.Data) > 0 {
		data = string(st.Data)
	}

	body := st.HTML
	if strings.TrimSpace(body) == "" {
		body = `<p style="font:14px ui-sans-serif,system-ui,sans-serif;color:#8a8a8a">` +
			`Empty html tab. An agent sets <code>state.html</code>.</p>`
	}

	// Inherit the board's palette so a widget looks native without the agent
	// having to restate the theme. It can override any of it.
	//
	// BOTH variants are spliced, not just the one in force. The alternative —
	// serving the current theme only — makes a theme switch a frame RELOAD, and a
	// reload throws away whatever the widget was holding in memory: a half-drawn
	// canvas stroke, a scroll position, a simulation mid-run. With both blocks
	// here the parent posts one message and the frame flips an attribute, which
	// is the same cost the board itself pays.
	//
	// The initial value comes from the URL rather than from a message, so a frame
	// on a light board does not paint dark and then correct itself. views/html.js
	// appends it; a frame opened by hand at /tab/<id>/html gets the board's
	// default.
	theme := themeKindOf(r, s.theme())
	page := `<!doctype html>
<html lang="en" data-theme="` + theme + `"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(name) + `</title>
<style>
  :root {
` + rootDeclarations(s.assets, s.theme()) + `
  }
` + lightRootBlock(s.assets, s.theme()) + `  html,body { margin:0; padding:0; }
  body {
    background:var(--bg); color:var(--text);
    font:14px/1.5 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;
    padding:14px;
  }
  a { color:var(--focus); }
  code,pre { font-family:ui-monospace,SFMono-Regular,Menlo,monospace; }
  :focus-visible { outline:2px solid var(--focus); outline-offset:2px; }
</style>
<script>window.__ABOARD_DATA__ = ` + data + `;</script>
` + bridgeScript + `
</head><body>
` + body + `
</body></html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", htmlTabCSP)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = fmt.Fprint(w, page)
}

// ---------- the palette a widget inherits ----------

// fallbackRootDeclarations is the palette an html tab gets when app.css cannot be
// read or parsed. It is the hand-copied subset this file shipped with, kept for
// exactly one purpose: a widget with NO ground and NO ink is unreadable, and a
// stale-but-present palette is better than a blank one. It is not maintained —
// rootDeclarations is what runs — so treat a difference between this and app.css
// as expected rather than as drift to chase.
const fallbackRootDeclarations = `    color-scheme: dark;
    --bg:#000; --sunken:#0a0a0a; --surface:#151515; --raised:#202020;
    --text:#ccd4e0; --muted:#b4b4b4; --dim:#a4a4a4;
    --line:#2a2a2a; --line-strong:#3d3d3d;
    --accent:#a4bd00; --accent-ink:#151515;
    --mark:#fb8c00; --agent:#a7adf4; --edge:#4a4a4a; --focus:#39bae6; --danger:#ff0066;`

// rootDeclarations returns the body of app.css's `:root` block, to be spliced
// into the frame's own `:root`.
//
// Read from the same fs.FS the shell's stylesheet is served from, so `--dev`
// picks up an edit on disk with no rebuild and the embedded tree answers
// everywhere else.
//
// This exists because the frame used to carry a hand-copied duplicate of that
// block, and a duplicate drifts: --accent-dim, --drop and the three --status-*
// tokens were in app.css and simply absent here, so a widget naming one got no
// colour, no fallback and no warning of any kind. The board's colour rule is
// "tokens only, stated once"; this makes the html tab obey it rather than
// re-state it.
func rootDeclarations(assets fs.FS, override *Theme) string {
	dark, _, ok := parseThemeVariants(assets)
	if !ok {
		return fallbackRootDeclarations
	}
	if override == nil {
		return dark.body
	}
	return themeDeclarations(dark, override.Dark)
}

// lightRootBlock is the frame's second palette: the whole
// `:root[data-theme="light"] { … }` rule, ready to splice, or "" when app.css
// has no light variant.
//
// Empty is a real answer and not a failure. A board whose stylesheet declares
// one theme is the board this project had until the switch existed; a frame with
// no light block simply stays dark, which is visible and correct rather than
// blank. What is NOT acceptable is a light block missing tokens the dark one
// has, and that is a stylesheet-level mistake — TestBothThemesDeclareTheSameTokens
// catches it where it is made.
func lightRootBlock(assets fs.FS, override *Theme) string {
	_, light, ok := parseThemeVariants(assets)
	if !ok || len(light.order) == 0 {
		return ""
	}
	return "  " + lightSelector + " {\n" + themeDeclarations(light, override.lightOverride()) + "\n  }\n"
}

// lightOverride is Theme.Light on a value that may be nil, because "no theme
// file" and "a theme file with no light section" are the same instruction here.
func (t *Theme) lightOverride() map[string]string {
	if t == nil {
		return nil
	}
	return t.Light
}

// themeKindOf is which variant a frame should PAINT FIRST: what the parent asked
// for in the URL, and the board's own default when nobody asked.
//
// An unrecognised value falls back to the default rather than being refused —
// the same call `?chrome=` makes, and for the same reason: a typo that renders
// the board in the wrong theme is a nuisance, and a typo that refuses to render
// a widget is a bug report.
func themeKindOf(r *http.Request, theme *Theme) string {
	if r != nil && r.URL.Query().Get("theme") == ThemeLight {
		return ThemeLight
	}
	if r != nil && r.URL.Query().Get("theme") == ThemeDark {
		return ThemeDark
	}
	return theme.DefaultKind()
}

// parseRootBlock returns app.css's dark `:root` body — what the frame splices,
// and what several tests read the palette out of.
//
// A thin wrapper over parseThemeVariants since the light variant arrived: there
// were two hand-rolled block finders in this package for about an hour, which is
// exactly the duplication this file's own comment warns about.
func parseRootBlock(assets fs.FS) (string, bool) {
	dark, _, ok := parseThemeVariants(assets)
	if !ok {
		return "", false
	}
	return dark.body, true
}

// declaresToken reports whether the block assigns the named custom property.
// Matched with its colon and a leading boundary, so `--text` matches neither
// `--text-strong` nor a mention of the name inside another value.
func declaresToken(block, name string) bool {
	for _, field := range strings.FieldsFunc(block, func(r rune) bool {
		return r == ';' || r == '\n' || r == '\r'
	}) {
		if strings.HasPrefix(strings.TrimSpace(field), name+":") {
			return true
		}
	}
	return false
}

// stripCSSComments removes /* … */ so a commented-out `:root {` in the file
// header cannot be mistaken for the real one. app.css opens with a long comment
// describing the palette, which is exactly that hazard.
func stripCSSComments(css string) string {
	var b strings.Builder
	for {
		start := strings.Index(css, "/*")
		if start < 0 {
			b.WriteString(css)
			return b.String()
		}
		b.WriteString(css[:start])
		end := strings.Index(css[start:], "*/")
		if end < 0 {
			return b.String()
		}
		css = css[start+end+2:]
	}
}
