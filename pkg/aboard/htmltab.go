package aboard

import (
	"encoding/json"
	"fmt"
	"html"
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
  window.addEventListener('message', function (e) {
    if (e.source !== parent || !e.data || e.data.__aboard !== 'data') return;
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
func (s *server) serveTabHTML(w http.ResponseWriter, _ *http.Request, tabID string) {
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
	page := `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(name) + `</title>
<style>
  :root {
    color-scheme: dark;
    --bg:#000; --sunken:#0a0a0a; --surface:#151515; --raised:#202020;
    --text:#ccd4e0; --muted:#b4b4b4; --dim:#a4a4a4;
    --line:#2a2a2a; --line-strong:#3d3d3d;
    --accent:#a4bd00; --accent-ink:#151515;
    --mark:#fb8c00; --agent:#a7adf4; --edge:#4a4a4a; --focus:#39bae6; --danger:#ff0066;
  }
  html,body { margin:0; padding:0; }
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
