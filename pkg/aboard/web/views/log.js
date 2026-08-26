// Log view: output you can watch, with the stream living outside aboard.json.
//
// `chat` is for talk and there was nowhere for OUTPUT — so a ten-minute job was
// either silent on the board or dumped into a notes tab that then had to be
// rewritten whole on every append. The lines come from a sidecar file the server
// owns (logs.go); this tab's state holds a pointer and nothing else:
//
//   state = { source: 'bb90', tail: 400, follow?: true, height?: '60vh' }
//
// An agent feeds it by piping:  go test ./... 2>&1 | aboard log bb90
//
// Polled rather than streamed, deliberately: the SSE channel fires on aboard.json
// changes, and a log write is not a board change — wiring it in would mean every
// log line waking every open page's document reload. A 2s poll while the tab is
// visible costs one small request and stops when you look away.

import { controlsFor } from './controls.js';
import { api } from './api.js';

const ctl = controlsFor('log');

const STYLE_ID = 'log-view-style';
const POLL_MS = 2000;

// SGR colours mapped onto the board's tokens rather than to raw ANSI colours, so
// output looks like it belongs here and survives a retheme. Only the codes that
// actually show up in build and test output are handled; anything else is dropped
// rather than printed as garbage.
const SGR = {
  30: 'dim', 31: 'danger', 32: 'accent', 33: 'mark', 34: 'focus',
  35: 'agent', 36: 'focus', 37: 'text',
  90: 'dim', 91: 'danger', 92: 'accent', 93: 'mark', 94: 'focus',
  95: 'agent', 96: 'focus', 97: 'text',
};

const CSS = `
[data-view="log"] .log-wrap {
  height: var(--log-height, 58vh);
  min-height: 200px;
  overflow: auto;
  background: var(--bg);
  border: 1px solid var(--line);
  border-radius: 4px;
  padding: 10px 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.82rem;
  line-height: 1.5;
}
[data-view="log"] .log-line { white-space: pre-wrap; overflow-wrap: anywhere; }
[data-view="log"] .log-line[data-match="no"] { display: none; }
[data-view="log"] .log-line em { font-style: normal; font-weight: 650; }
[data-view="log"] .c-dim { color: var(--dim); }
[data-view="log"] .c-text { color: var(--text); }
[data-view="log"] .c-danger { color: var(--danger); }
[data-view="log"] .c-accent { color: var(--accent); }
[data-view="log"] .c-mark { color: var(--mark); }
[data-view="log"] .c-focus { color: var(--focus); }
[data-view="log"] .c-agent { color: var(--agent); }
[data-view="log"] .log-empty { color: var(--muted); }
[data-view="log"] .log-filter { min-width: 14ch; }
[data-view="log"] .log-meta { color: var(--dim); font-size: 0.76rem; }
`;

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

/** ANSI SGR → coloured spans. Unknown sequences are consumed, never printed. */
function ansiInto(parent, line) {
  const pattern = /\x1b\[([0-9;]*)m/g;
  let at = 0;
  let cls = null;
  let bold = false;
  let hit;

  const emit = (text) => {
    if (!text) return;
    if (!cls && !bold) { parent.append(document.createTextNode(text)); return; }
    const span = document.createElement(bold ? 'em' : 'span');
    if (cls) span.className = 'c-' + cls;
    span.textContent = text;
    parent.append(span);
  };

  while ((hit = pattern.exec(line)) !== null) {
    emit(line.slice(at, hit.index));
    for (const raw of hit[1].split(';')) {
      const code = Number(raw || '0');
      if (code === 0) { cls = null; bold = false; continue; }
      if (code === 1) { bold = true; continue; }
      if (code === 22) { bold = false; continue; }
      if (code === 39) { cls = null; continue; }
      if (SGR[code]) cls = SGR[code];
    }
    at = hit.index + hit[0].length;
  }
  emit(line.slice(at));
}

export function mountLog(root, ctx) {
  injectStyle();

  let lines = [];
  let filter = '';       // per-viewer
  let follow = true;     // per-viewer, defaults to state.follow on first mount
  let timer = null;
  let lastSize = -1;

  const source = () => String(ctx.state.source || '').trim();

  const toolbar = document.createElement('div');
  toolbar.className = 'toolbar';

  const followBtn = ctl('follow');

  const filterInput = document.createElement('input');
  filterInput.type = 'text';
  filterInput.className = 'log-filter';
  filterInput.placeholder = 'filter…';
  filterInput.setAttribute('aria-label', 'filter lines');

  const meta = document.createElement('span');
  meta.className = 'mono log-meta';

  const copyBtn = ctl('copy');

  toolbar.append(followBtn, filterInput, copyBtn, meta);

  const wrap = document.createElement('div');
  wrap.className = 'log-wrap';

  root.append(toolbar, wrap);

  if (ctx.state.follow === false) follow = false;

  function applyHeight() {
    const h = ctx.state.height;
    if (h === undefined || h === null || h === '') { wrap.style.removeProperty('--log-height'); return; }
    wrap.style.setProperty('--log-height', typeof h === 'number' ? `${h}px` : String(h));
  }

  // One place that dresses the Follow button, because there are two ways to
  // change what it reports and they used to disagree. The scroll handler below
  // set only the LABEL, so after scrolling up the button read "Follow" while
  // aria-pressed still said true and the tooltip still offered to stop
  // following — the visual half moved and the accessible half did not. Found by
  // the browser suite asserting the attribute rather than the text.
  function paintFollowButton() {
    followBtn.textContent = follow ? 'Following' : 'Follow';
    followBtn.setAttribute('aria-pressed', String(follow));
    followBtn.title = follow ? 'Stop pinning to the newest line' : 'Pin to the newest line';
  }

  function paint() {
    paintFollowButton();

    wrap.replaceChildren();
    if (!source()) {
      const p = document.createElement('p');
      p.className = 'log-empty';
      p.textContent = 'No source set — an agent sets state.source to a log id and pipes into aboard log <id>.';
      wrap.append(p);
      return;
    }
    if (!lines.length) {
      const p = document.createElement('p');
      p.className = 'log-empty';
      p.textContent = `Nothing logged yet. Feed it with:  <command> 2>&1 | aboard log ${source()}`;
      wrap.append(p);
      return;
    }

    const needle = filter.toLowerCase();
    let shown = 0;
    for (const line of lines) {
      const div = document.createElement('div');
      div.className = 'log-line';
      ansiInto(div, line);
      if (needle && !line.toLowerCase().includes(needle)) div.dataset.match = 'no';
      else shown += 1;
      wrap.append(div);
    }
    meta.textContent = needle
      ? `${shown} of ${lines.length} lines`
      : `${lines.length} line${lines.length === 1 ? '' : 's'}`;
    if (follow) wrap.scrollTop = wrap.scrollHeight;
  }

  async function fetchTail() {
    const id = source();
    if (!id) return;
    const tail = Number(ctx.state.tail) > 0 ? Number(ctx.state.tail) : 400;
    try {
      const res = await fetch(api(`/log?tab=${encodeURIComponent(id)}&tail=${tail}`), { cache: 'no-store' });
      if (!res.ok) return;
      const body = await res.json();
      // Only repaint when the file actually moved: a poll that redraws every two
      // seconds would fight your scroll position and your text selection.
      if (body.size === lastSize) return;
      lastSize = body.size;
      lines = Array.isArray(body.lines) ? body.lines : [];
      paint();
    } catch {
      // Server gone: keep showing what we have rather than blanking the tab.
    }
  }

  // Polling stops when the tab is not the one on screen — an invisible log has
  // no reason to keep asking.
  function visible() {
    return root.isConnected && root.closest('[data-active="yes"]') !== null && !document.hidden;
  }

  function tick() {
    if (visible()) fetchTail();
  }

  followBtn.addEventListener('click', () => { follow = !follow; paint(); });
  filterInput.addEventListener('input', () => { filter = filterInput.value; paint(); });
  copyBtn.addEventListener('click', async () => {
    try { await navigator.clipboard.writeText(lines.join('\n')); meta.textContent = 'copied'; }
    catch { meta.textContent = 'copy blocked'; }
    setTimeout(() => paint(), 1200);
  });
  // Scrolling up is how you say "stop following"; scrolling back to the bottom
  // resumes it. Buttons are for intent, this is for reflex.
  wrap.addEventListener('scroll', () => {
    const atBottom = wrap.scrollHeight - wrap.scrollTop - wrap.clientHeight < 24;
    if (atBottom !== follow) { follow = atBottom; paintFollowButton(); }
  });

  applyHeight();
  paint();
  fetchTail();
  timer = setInterval(tick, POLL_MS);

  return {
    refresh() {
      applyHeight();
      lastSize = -1;      // state may point at a different log now
      fetchTail();
    },
    destroy() { clearInterval(timer); },
  };
}
