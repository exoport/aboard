// The html tab type: arbitrary agent-authored markup, CSS and script.
//
// The document is NOT injected into this page. It is served from
// /tab/<id>/html and rendered in an iframe with sandbox="allow-scripts" and no
// allow-same-origin, behind a CSP that blocks all network egress (see
// htmltab.go for why that matters — this server has no auth). So a widget can
// do anything HTML can do locally, and nothing to the board it was not handed.
//
// State round-trips through a postMessage bridge: the frame calls aboard.set(),
// we write it into this tab's state.data and save it the normal way.

import { controlsFor } from './controls.js';
import { api } from './api.js';

const ctl = controlsFor('html');

const STYLE_ID = 'html-view-style';

const CSS = `
[data-view="html"] .html-frame-wrap {
  position: relative;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--bg);
  overflow: hidden;
}
[data-view="html"] iframe {
  display: block;
  width: 100%;
  border: 0;
  background: var(--bg);
  /* A generous default rather than a small one grown by aboard.fit(): the fit is
     an async round-trip through postMessage, so relying on it for basic layout
     leaves the frame stubby whenever that is slow or blocked. state.height
     overrides; fit() still adjusts from here. */
  height: var(--frame-height, 62vh);
  min-height: 220px;
}
[data-view="html"] .html-editor {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 12px;
}
[data-view="html"] .html-editor[hidden] { display: none; }
[data-view="html"] .html-editor textarea {
  width: 100%;
  min-height: 240px;
  font-size: 0.85rem;
}
[data-view="html"] .html-data {
  margin: 0;
  padding: 8px 10px;
  border: 1px solid var(--line);
  border-radius: 3px;
  background: var(--sunken);
  color: var(--muted);
  font-size: 0.79rem;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 140px;
  overflow: auto;
}
`;

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

export function mountHtml(root, ctx) {
  injectStyle();

  if (typeof ctx.state.html !== 'string') ctx.state.html = '';
  if (!ctx.state.data || typeof ctx.state.data !== 'object') ctx.state.data = {};

  const tabId = ctx.tab.id;
  let editorOpen = false;
  let lastLoadedHtml = null;
  let saveTimer = null;
  let lastAppliedHeight = 0;

  const panel = document.createElement('div');
  panel.className = 'panel';

  const head = document.createElement('p');
  head.className = 'panel-head';
  head.textContent = 'HTML';
  panel.append(head);

  const toolbar = document.createElement('div');
  toolbar.className = 'toolbar';

  const reload = ctl('reload');
  const toggle = ctl('source');
  const status = document.createElement('span');
  status.className = 'toolbar-label';
  toolbar.append(reload, toggle, status);
  panel.append(toolbar);

  const wrap = document.createElement('div');
  wrap.className = 'html-frame-wrap';
  const frame = document.createElement('iframe');
  // allow-scripts WITHOUT allow-same-origin: the two together would let the
  // frame remove its own sandbox.
  frame.setAttribute('sandbox', 'allow-scripts');
  frame.setAttribute('title', ctx.tab.name || 'HTML tab');
  frame.setAttribute('referrerpolicy', 'no-referrer');
  wrap.append(frame);
  panel.append(wrap);

  const editor = document.createElement('div');
  editor.className = 'html-editor';
  editor.hidden = true;

  const srcLabel = document.createElement('span');
  srcLabel.className = 'toolbar-label';
  srcLabel.textContent = 'source';
  const source = document.createElement('textarea');
  source.className = 'mono';
  source.spellcheck = false;
  source.setAttribute('aria-label', 'HTML source for this tab');
  source.value = ctx.state.html;

  const dataLabel = document.createElement('span');
  dataLabel.className = 'toolbar-label';
  dataLabel.textContent = 'stored data';
  const dataView = document.createElement('pre');
  dataView.className = 'html-data mono';

  const hint = document.createElement('p');
  hint.className = 'hint';
  hint.textContent = 'The frame is sandboxed and cannot reach the network or this page. '
    + 'It persists state by calling aboard.set(value); read it back with aboard.get().';

  editor.append(srcLabel, source, dataLabel, dataView, hint);
  panel.append(editor);
  root.append(panel);

  // state.height: any CSS length ("70vh", "600px") or a number read as px. When
  // set, the frame is that tall and the widget's own fit() requests are ignored.
  function fixedHeight() {
    const h = ctx.state.height;
    if (h === undefined || h === null || h === '') return null;
    return typeof h === 'number' ? `${h}px` : String(h);
  }

  function applyHeight() {
    const h = fixedHeight();
    if (h) frame.style.height = h;
  }

  function flash(text) {
    status.textContent = text;
    setTimeout(() => { if (status.textContent === text) status.textContent = ''; }, 1800);
  }

  function showData() {
    try {
      dataView.textContent = JSON.stringify(ctx.state.data, null, 2);
    } catch {
      dataView.textContent = '(unserialisable)';
    }
  }

  // Which theme the board is showing. The frame is a separate document with its
  // own :root, so it cannot inherit the attribute — it is told, twice: in the
  // URL when it loads (so it never paints the wrong theme and corrects itself)
  // and by postMessage when the human switches (so switching does not reload the
  // frame and throw away whatever the widget was holding).
  function themeKind() {
    return document.documentElement.getAttribute('data-theme') === 'light' ? 'light' : 'dark';
  }

  // The project's own overrides for the variant now in force, when there is a
  // .aboard/theme.json. The two built-in blocks were spliced into the frame when
  // it loaded and a switch only has to name one of them — but an EDIT to
  // theme.json changes VALUES inside those blocks, and the frame is holding the
  // copy the server served it. Without this a house style stops at the widget
  // boundary the moment anybody iterates on it, which is the only time anybody
  // is looking.
  function projectTokens(kind) {
    const theme = window.ABOARD_THEME;
    const tokens = theme && theme[kind];
    return tokens && typeof tokens === 'object' ? tokens : null;
  }

  function pushTheme() {
    if (!frame.contentWindow) return;
    const kind = themeKind();
    frame.contentWindow.postMessage({ __aboard: 'theme', kind, tokens: projectTokens(kind) }, '*');
  }

  // Cache-bust so a reload after an edit actually fetches the new document.
  function loadFrame() {
    lastLoadedHtml = ctx.state.html;
    frame.src = api(`/tab/${encodeURIComponent(tabId)}/html`)
      + `?v=${Date.now()}&theme=${encodeURIComponent(themeKind())}`;
    showData();
  }

  // Messages arrive from an opaque origin (that is what sandboxing gives us),
  // so identity is established by comparing the source window, not the origin.
  function onMessage(e) {
    if (e.source !== frame.contentWindow) return;
    const msg = e.data;
    if (!msg || typeof msg !== 'object') return;

    if (msg.__aboard === 'set') {
      ctx.state.data = msg.data && typeof msg.data === 'object' ? msg.data : {};
      showData();
      clearTimeout(saveTimer);
      saveTimer = setTimeout(() => {
        ctx.save().then((ok) => flash(ok ? 'saved' : 'save failed'));
      }, 250);
      return;
    }
    if (msg.__aboard === 'height') {
      // An explicit state.height wins: the agent asked for a fixed size.
      if (fixedHeight()) return;
      const h = Number(msg.height);
      if (!Number.isFinite(h) || h <= 0) return;
      const next = Math.min(Math.max(h + 8, 220), 4000);
      // Ignore near-identical heights: a widget that calls fit() from its own
      // resize handler would otherwise ping-pong with this assignment forever.
      if (Math.abs(next - lastAppliedHeight) < 12) return;
      lastAppliedHeight = next;
      frame.style.height = next + 'px';
    }
  }

  window.addEventListener('message', onMessage);
  document.addEventListener('aboard:theme', pushTheme);

  reload.addEventListener('click', loadFrame);

  toggle.addEventListener('click', () => {
    editorOpen = !editorOpen;
    editor.hidden = !editorOpen;
    toggle.textContent = editorOpen ? 'Hide source' : 'Show source';
    if (editorOpen) showData();
  });

  source.addEventListener('input', () => {
    ctx.state.html = source.value;
    clearTimeout(saveTimer);
    saveTimer = setTimeout(() => {
      ctx.save().then((ok) => flash(ok ? 'saved' : 'save failed'));
    }, 400);
  });

  // Applying a source edit is explicit: reloading on every keystroke would
  // restart whatever the widget was doing.
  source.addEventListener('change', loadFrame);

  applyHeight();
  loadFrame();

  return {
    refresh() {
      applyHeight();
      if (document.activeElement !== source && source.value !== ctx.state.html) {
        source.value = ctx.state.html;
      }
      // Only rebuild the frame if the document itself changed; otherwise push
      // the new data in so a running widget updates without losing its state.
      if (ctx.state.html !== lastLoadedHtml) {
        loadFrame();
        return;
      }
      showData();
      if (frame.contentWindow) {
        frame.contentWindow.postMessage({ __aboard: 'data', data: ctx.state.data }, '*');
      }
    },
    // Both listeners are on WINDOW and DOCUMENT, so nothing removes them when
    // the view's own root is dropped: a tab mounted and unmounted a dozen times
    // left a dozen handlers posting into frames that no longer exist. The theme
    // one made that visible; the message one had been there all along.
    destroy() {
      window.removeEventListener('message', onMessage);
      document.removeEventListener('aboard:theme', pushTheme);
    },
  };
}
