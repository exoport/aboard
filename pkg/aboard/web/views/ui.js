// UI view: a layout described as data, drawn by trusted components.
//
// Google's A2UI makes the argument well, and it is the argument for this file:
// when an agent emits a COMPONENT TREE instead of code, the trust boundary moves
// from the UI to the data. There is no iframe to sandbox, no CSP to keep honest,
// no script to review — the page can only contain the components this file knows
// how to draw, so it inherits the board's tokens, its type sizes and its contrast
// for free, and a bad tree is a bad layout rather than a security question.
//
// The trade, stated plainly because it is the cost of the whole approach: the
// catalog is a closed vocabulary. Anything not listed here cannot be expressed,
// which is exactly the objection this project raised against prefixed ids — and
// the reason `html` stays. Reach for `ui` when the shape is ordinary (a report, a
// summary, a small form) and for `html` when the interaction IS the thing.
//
//   state = {
//     root: { type: 'col', children: [ … ] },
//     data: { … },        // values that `bind` refs point at, and where fields write
//     intents: [ … ]      // appended when a button is pressed, same as state.actions
//   }
//
// Unknown component types render as a visible marker rather than nothing: a
// silent omission would have an agent believing it showed something it did not.

import { button } from './controls.js';
import { PALETTES } from './controls.generated.js';
import { api } from './api.js';

const STYLE_ID = 'ui-view-style';

const CSS = `
[data-view="ui"] .uic-col { display: flex; flex-direction: column; gap: var(--uic-gap, 10px); }
[data-view="ui"] .uic-row { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--uic-gap, 12px); }
[data-view="ui"] .uic-row > * { min-width: 0; }
[data-view="ui"] .uic-grow { flex: 1 1 0; }
[data-view="ui"] .uic-card {
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  padding: 13px 15px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
[data-view="ui"] .uic-card[data-accent="yes"] { border-left: 3px solid var(--accent); }
[data-view="ui"] .uic-title { font-size: 1.02rem; font-weight: 650; margin: 0; }
[data-view="ui"] .uic-heading {
  font-size: 0.74rem; letter-spacing: 0.09em; text-transform: uppercase;
  color: var(--dim); margin: 0; font-weight: 600;
}
[data-view="ui"] .uic-body { margin: 0; font-size: 0.9rem; line-height: 1.55; }
[data-view="ui"] .uic-caption { margin: 0; font-size: 0.82rem; color: var(--muted); }
[data-view="ui"] .uic-badge {
  display: inline-block;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.72rem; letter-spacing: 0.05em;
  padding: 2px 7px; border-radius: 2px;
  border: 1px solid var(--tone, var(--line-strong));
  color: var(--tone, var(--muted));
}
[data-view="ui"] .uic-divider { border: 0; border-top: 1px solid var(--line); margin: 4px 0; }
[data-view="ui"] .uic-list { margin: 0; padding-left: 18px; font-size: 0.9rem; line-height: 1.6; }
[data-view="ui"] .uic-list li::marker { color: var(--accent); }
[data-view="ui"] .uic-kv { display: grid; grid-template-columns: minmax(8ch, 22ch) 1fr; gap: 5px 14px; font-size: 0.88rem; }
[data-view="ui"] .uic-kv dt { color: var(--dim); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.8rem; }
[data-view="ui"] .uic-kv dd { margin: 0; }
[data-view="ui"] .uic-code {
  margin: 0; padding: 9px 11px;
  background: var(--sunken); border: 1px solid var(--line); border-radius: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.82rem; line-height: 1.5; overflow-x: auto;
}
[data-view="ui"] .uic-meter { height: 6px; border-radius: 3px; background: var(--sunken); overflow: hidden; }
[data-view="ui"] .uic-meter span { display: block; height: 100%; background: var(--tone, var(--accent)); }
[data-view="ui"] .uic-stat { display: flex; flex-direction: column; gap: 2px; }
[data-view="ui"] .uic-stat b { font-size: 1.35rem; font-weight: 650; font-variant-numeric: tabular-nums; color: var(--tone, var(--text)); }
[data-view="ui"] .uic-field { display: flex; flex-direction: column; gap: 4px; font-size: 0.87rem; }
[data-view="ui"] .uic-field > span { color: var(--muted); }
[data-view="ui"] .uic-table { width: 100%; border-collapse: collapse; font-size: 0.86rem; }
[data-view="ui"] .uic-table th {
  text-align: left; padding: 6px 9px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.7rem; letter-spacing: 0.06em; text-transform: uppercase;
  color: var(--dim); border-bottom: 1px solid var(--line-strong);
}
[data-view="ui"] .uic-table td { padding: 6px 9px; border-bottom: 1px solid var(--line); vertical-align: top; }
[data-view="ui"] .uic-table tr:last-child td { border-bottom: 0; }
[data-view="ui"] .uic-table-wrap { overflow-x: auto; }
[data-view="ui"] .uic-tabs { display: flex; gap: 2px; border-bottom: 1px solid var(--line); flex-wrap: wrap; }
[data-view="ui"] .uic-tab {
  font: inherit; font-size: 0.84rem; color: var(--muted);
  background: none; border: 1px solid transparent; border-bottom: none;
  border-radius: 4px 4px 0 0; padding: 6px 12px; margin-bottom: -1px; cursor: pointer;
}
[data-view="ui"] .uic-tab[aria-selected="true"] {
  color: var(--text); background: var(--surface);
  border-color: var(--line); box-shadow: inset 0 2px 0 var(--accent);
}
[data-view="ui"] .uic-panel {
  /* A gapped column, not a bare block. Panel children were appended straight in,
     so cards sat edge to edge with nothing between them: a col component gets its
     gap from a rule, and a panel had none. (No backticks in here — this whole
     block is a JS template literal.) */
  padding-top: 12px;
  display: flex;
  flex-direction: column;
  gap: var(--uic-gap, 10px);
}
[data-view="ui"] .uic-notice {
  display: flex; gap: 10px; align-items: flex-start;
  padding: 9px 12px; border: 1px solid var(--line);
  border-left: 3px solid var(--tone, var(--agent));
  border-radius: 3px; background: var(--sunken); font-size: 0.87rem; line-height: 1.5;
}
[data-view="ui"] .uic-notice b { color: var(--tone, var(--agent)); font-weight: 600; }
[data-view="ui"] .uic-check { display: flex; flex-direction: column; gap: 6px; }
[data-view="ui"] .uic-check label { display: flex; align-items: flex-start; gap: 8px; font-size: 0.88rem; }
[data-view="ui"] .uic-check input { margin-top: 2px; }
[data-view="ui"] .uic-check label[data-done="yes"] > span { color: var(--dim); text-decoration: line-through; }
[data-view="ui"] .uic-img { display: block; max-width: 100%; border: 1px solid var(--line); border-radius: 4px; }
[data-view="ui"] .uic-figure { margin: 0; display: flex; flex-direction: column; gap: 5px; }
[data-view="ui"] .uic-figure figcaption { color: var(--muted); font-size: 0.82rem; }
[data-view="ui"] .uic-quote {
  margin: 0; padding: 4px 0 4px 12px; border-left: 2px solid var(--tone, var(--agent));
  color: var(--muted); font-size: 0.9rem; line-height: 1.55;
}
[data-view="ui"] .uic-quote cite { display: block; margin-top: 4px; color: var(--dim); font-size: 0.8rem; font-style: normal; }
[data-view="ui"] .uic-grid {
  display: grid;
  gap: var(--uic-gap, 12px);
  grid-template-columns: repeat(var(--uic-cols, 2), minmax(0, 1fr));
  align-items: start;
}
@media (max-width: 700px) { [data-view="ui"] .uic-grid { grid-template-columns: minmax(0, 1fr); } }
[data-view="ui"] .uic-spacer { height: var(--uic-size, 12px); }
[data-view="ui"] .uic-link { color: var(--focus); font-size: 0.9rem; }
[data-view="ui"] .uic-unknown {
  border: 1px dashed var(--danger); border-radius: 3px; padding: 6px 9px;
  color: var(--danger); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.78rem;
}
`;

// Colour names an agent may use. A name rather than a hex, for the same reason
// markup's marks take names: it survives a retheme, and nothing outside this list
// can be asked for.
//
// Built FROM views/ui.spec.json rather than restated here, like the controls: the
// list an agent is told about and the list this code accepts must not be two
// lists. Every tone maps to the token of the same name, which is what makes a
// bare array enough. `aboard apply` warns when a write names one that is not here.
const TONES = Object.fromEntries((PALETTES.ui || []).map((name) => [name, `var(--${name})`]));

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

// Which panel of a `tabs` component you had open. Per-viewer state, so it never
// goes near aboard.json — but it must survive a re-render, and render() rebuilds
// the whole tree on every board write. Held here, keyed by tab and component, and
// mirrored into sessionStorage so it also survives the page reloading itself.
const openPanels = new Map();

function panelKey(tabId, node, fallback) {
  const own = node.id || (Array.isArray(node.panels)
    ? node.panels.map((p) => (p && p.label) || '').join('|')
    : String(fallback));
  return `${tabId}::${own}`;
}

function readPanel(key) {
  if (openPanels.has(key)) return openPanels.get(key);
  try {
    const stored = sessionStorage.getItem('aboard.panel.' + key);
    if (stored !== null) {
      const n = Number(stored);
      if (Number.isInteger(n) && n >= 0) { openPanels.set(key, n); return n; }
    }
  } catch {
    // Private windows and blocked site data: fall through to the default.
  }
  return 0;
}

function writePanel(key, index) {
  openPanels.set(key, index);
  try { sessionStorage.setItem('aboard.panel.' + key, String(index)); } catch {}
}

export function mountUi(root, ctx) {
  injectStyle();

  const host = document.createElement('div');
  host.className = 'uic-col';
  root.append(host);

  const data = () => (ctx.state.data && typeof ctx.state.data === 'object' ? ctx.state.data : (ctx.state.data = {}));

  // A bound value: { bind: 'path.to.value' } reads from state.data, so one tree
  // can be re-rendered against changing values without being rewritten.
  function resolve(value) {
    if (value && typeof value === 'object' && typeof value.bind === 'string') {
      return value.bind.split('.').reduce((acc, key) => (acc == null ? acc : acc[key]), data());
    }
    return value;
  }

  const asText = (value) => {
    const v = resolve(value);
    if (v === undefined || v === null) return '';
    return typeof v === 'string' ? v : JSON.stringify(v);
  };

  function toneOf(node, el) {
    const tone = TONES[String(resolve(node.tone) || '')];
    if (tone) el.style.setProperty('--tone', tone);
  }

  function writeBound(path, value) {
    const parts = String(path).split('.');
    const last = parts.pop();
    let target = data();
    for (const key of parts) {
      if (!target[key] || typeof target[key] !== 'object') target[key] = {};
      target = target[key];
    }
    target[last] = value;
    ctx.save();
  }

  function build(node) {
    if (node === null || node === undefined) return null;
    if (typeof node === 'string') {
      const p = document.createElement('p');
      p.className = 'uic-body';
      p.textContent = node;
      return p;
    }
    if (typeof node !== 'object') return null;

    const kids = Array.isArray(node.children) ? node.children : [];
    const el = (() => {
      switch (node.type) {
        case 'col':
        case 'row': {
          const box = document.createElement('div');
          box.className = node.type === 'row' ? 'uic-row' : 'uic-col';
          if (node.gap !== undefined) box.style.setProperty('--uic-gap', String(resolve(node.gap)));
          return box;
        }
        case 'card': {
          const card = document.createElement('section');
          card.className = 'uic-card';
          if (node.accent) card.dataset.accent = 'yes';
          if (node.title) {
            const h = document.createElement('p');
            h.className = 'uic-title';
            h.textContent = asText(node.title);
            card.append(h);
          }
          return card;
        }
        case 'title': case 'heading': case 'text': case 'caption': {
          const map = { title: ['p', 'uic-title'], heading: ['p', 'uic-heading'], text: ['p', 'uic-body'], caption: ['p', 'uic-caption'] };
          const [tag, cls] = map[node.type];
          const p = document.createElement(tag);
          p.className = cls;
          p.textContent = asText(node.value !== undefined ? node.value : node.text);
          toneOf(node, p);
          return p;
        }
        case 'badge': {
          const b = document.createElement('span');
          b.className = 'uic-badge';
          b.textContent = asText(node.value !== undefined ? node.value : node.text);
          toneOf(node, b);
          return b;
        }
        case 'divider':
          return Object.assign(document.createElement('hr'), { className: 'uic-divider' });
        case 'list': {
          const ul = document.createElement('ul');
          ul.className = 'uic-list';
          const items = resolve(node.items);
          for (const item of Array.isArray(items) ? items : []) {
            const li = document.createElement('li');
            li.textContent = typeof item === 'string' ? item : JSON.stringify(item);
            ul.append(li);
          }
          return ul;
        }
        case 'kv': {
          const dl = document.createElement('dl');
          dl.className = 'uic-kv';
          // asText, not String: this resolved the pairs ARRAY but not the values
          // inside it, so every other display component honoured a {bind} and the
          // one component whose entire job is "label: value" rendered
          // "[object Object]". A live summary is a main reason to reach for `ui`,
          // and kv is the obvious component for it.
          const pairs = resolve(node.pairs);
          for (const pair of Array.isArray(pairs) ? pairs : []) {
            const dt = document.createElement('dt');
            dt.textContent = asText(pair && pair.key !== undefined ? pair.key : '');
            const dd = document.createElement('dd');
            dd.textContent = asText(pair && pair.value !== undefined ? pair.value : '');
            dl.append(dt, dd);
          }
          return dl;
        }
        case 'code': {
          const pre = document.createElement('pre');
          pre.className = 'uic-code';
          pre.textContent = asText(node.value !== undefined ? node.value : node.text);
          return pre;
        }
        case 'stat': {
          const box = document.createElement('div');
          box.className = 'uic-stat';
          toneOf(node, box);
          const b = document.createElement('b');
          b.textContent = asText(node.value);
          const label = document.createElement('span');
          label.className = 'uic-caption';
          label.textContent = asText(node.label);
          box.append(b, label);
          return box;
        }
        case 'meter': {
          const wrap = document.createElement('div');
          wrap.className = 'uic-meter';
          toneOf(node, wrap);
          const fill = document.createElement('span');
          const value = Number(resolve(node.value)) || 0;
          const max = Number(resolve(node.max)) || 100;
          fill.style.width = Math.max(0, Math.min(100, (value / max) * 100)) + '%';
          wrap.append(fill);
          return wrap;
        }
        case 'button': {
          // A plain button, not a declared control: the label is the AGENT's,
          // from the component tree, so it is content rather than renderer chrome.
          const btn = button(asText(node.label) || 'Button', '', { className: 'icon-btn action-btn' });
          // Same posture as state.actions: a press records intent, it never acts.
          btn.addEventListener('click', () => {
            if (!Array.isArray(ctx.state.intents)) ctx.state.intents = [];
            ctx.state.intents.push({
              id: typeof ctx.nextId === 'function' ? ctx.nextId() : String(Date.now()),
              action: node.id || node.intent || 'button',
              intent: node.intent || asText(node.label),
              at: new Date().toISOString(),
              by: 'human',
            });
            btn.disabled = true;
            ctx.save({ immediate: true }).then(() => { btn.disabled = false; btn.textContent += ' ✓'; });
          });
          return btn;
        }
        case 'field': {
          const label = document.createElement('label');
          label.className = 'uic-field';
          const span = document.createElement('span');
          span.textContent = asText(node.label);
          label.append(span);
          const path = typeof node.bind === 'string' ? node.bind : null;
          const current = path ? path.split('.').reduce((a, k) => (a == null ? a : a[k]), data()) : '';
          let input;
          if (node.field === 'select') {
            input = document.createElement('select');
            for (const opt of Array.isArray(node.options) ? node.options : []) {
              const o = document.createElement('option');
              o.value = String(opt);
              o.textContent = String(opt);
              input.append(o);
            }
            input.value = current ?? '';
          } else if (node.field === 'checkbox') {
            input = document.createElement('input');
            input.type = 'checkbox';
            input.checked = !!current;
          } else {
            input = document.createElement(node.field === 'longtext' ? 'textarea' : 'input');
            if (node.field === 'number') input.type = 'number';
            input.value = current ?? '';
          }
          input.addEventListener('change', () => {
            if (!path) return;
            writeBound(path, node.field === 'checkbox' ? input.checked
              : node.field === 'number' ? Number(input.value) : input.value);
          });
          label.append(input);
          return label;
        }
        case 'table': {
          // Read-only by design: the `table` TAB type is the editable one. This is
          // for a table that is part of a report, not a table you work in.
          const wrap = document.createElement('div');
          wrap.className = 'uic-table-wrap';
          const table = document.createElement('table');
          table.className = 'uic-table';
          const cols = Array.isArray(node.columns) ? node.columns : [];
          if (cols.length) {
            const thead = document.createElement('thead');
            const tr = document.createElement('tr');
            for (const col of cols) {
              const th = document.createElement('th');
              th.textContent = String(col && col.label !== undefined ? col.label : col);
              tr.append(th);
            }
            thead.append(tr);
            table.append(thead);
          }
          const tbody = document.createElement('tbody');
          const rows = resolve(node.rows);
          for (const row of Array.isArray(rows) ? rows : []) {
            const tr = document.createElement('tr');
            const cells = Array.isArray(row)
              ? row
              : cols.map((col) => row && row[(col && col.id) || col]);
            for (const cell of cells) {
              const td = document.createElement('td');
              td.textContent = cell === undefined || cell === null ? '' : String(cell);
              tr.append(td);
            }
            tbody.append(tr);
          }
          table.append(tbody);
          wrap.append(table);
          return wrap;
        }
        case 'tabs': {
          // Which panel is open is per-viewer: writing it to state would flip the
          // tab under everyone else looking at the same board.
          const box = document.createElement('div');
          const strip = document.createElement('div');
          strip.className = 'uic-tabs';
          const panel = document.createElement('div');
          panel.className = 'uic-panel';
          const panels = Array.isArray(node.panels) ? node.panels : [];
          const key = panelKey(String((ctx.tab && ctx.tab.id) || ''), node, panels.length);
          let open = Math.min(readPanel(key), Math.max(0, panels.length - 1));
          const draw = () => {
            strip.replaceChildren();
            panels.forEach((p, i) => {
              const btn = button(asText(p && p.label) || `panel ${i + 1}`, '',
                { className: 'uic-tab', onClick: () => { open = i; writePanel(key, i); draw(); } });
              btn.setAttribute('aria-selected', String(i === open));
              strip.append(btn);
            });
            panel.replaceChildren();
            const body = panels[open] && panels[open].children;
            for (const child of Array.isArray(body) ? body : []) {
              const built = build(child);
              if (built) panel.append(built);
            }
          };
          draw();
          box.append(strip, panel);
          return box;
        }
        case 'notice': {
          const box = document.createElement('div');
          box.className = 'uic-notice';
          toneOf(node, box);
          if (node.label) {
            const b = document.createElement('b');
            b.textContent = asText(node.label);
            box.append(b);
          }
          const span = document.createElement('span');
          span.textContent = asText(node.value !== undefined ? node.value : node.text);
          box.append(span);
          return box;
        }
        case 'checklist': {
          // Ticks are real state: each item's `bind` says where its boolean lives,
          // so a checklist an agent renders is one the agent can read back.
          const box = document.createElement('div');
          box.className = 'uic-check';
          for (const item of Array.isArray(node.items) ? node.items : []) {
            const label = document.createElement('label');
            const box2 = document.createElement('input');
            box2.type = 'checkbox';
            const path = typeof item.bind === 'string' ? item.bind : null;
            const done = path
              ? !!path.split('.').reduce((a, k) => (a == null ? a : a[k]), data())
              : !!item.done;
            box2.checked = done;
            label.dataset.done = done ? 'yes' : 'no';
            box2.disabled = !path;
            box2.addEventListener('change', () => { if (path) writeBound(path, box2.checked); });
            const text = document.createElement('span');
            text.textContent = asText(item.label !== undefined ? item.label : item);
            label.append(box2, text);
            box.append(label);
          }
          return box;
        }
        case 'image': {
          const fig = document.createElement('figure');
          fig.className = 'uic-figure';
          const img = document.createElement('img');
          img.className = 'uic-img';
          // Same-origin only: assets/ and uploads/ are the board's own files, and
          // a remote src would make a data-only tab fetch across the network.
          const src = String(asText(node.src) || '');
          if (/^(assets|uploads)\//.test(src)) {
            img.src = api('/' + src);
            img.alt = asText(node.alt) || '';
            fig.append(img);
          } else {
            const bad = document.createElement('div');
            bad.className = 'uic-unknown';
            // The marker carries WHAT it is a marker for, so the mount receipt
            // (aboard.html's sweep, POST /rendered) can name it to the agent that
            // wrote the tree instead of only to the human looking at it.
            bad.dataset.unknown = 'image.src';
            bad.textContent = 'image src must be under assets/ or uploads/ — got: ' + src;
            fig.append(bad);
          }
          if (node.caption) {
            const cap = document.createElement('figcaption');
            cap.textContent = asText(node.caption);
            fig.append(cap);
          }
          return fig;
        }
        case 'quote': {
          const q = document.createElement('blockquote');
          q.className = 'uic-quote';
          toneOf(node, q);
          q.append(document.createTextNode(asText(node.value !== undefined ? node.value : node.text)));
          if (node.by) {
            const cite = document.createElement('cite');
            cite.textContent = '— ' + asText(node.by);
            q.append(cite);
          }
          return q;
        }
        case 'grid': {
          const g = document.createElement('div');
          g.className = 'uic-grid';
          const cols = Number(resolve(node.columns));
          g.style.setProperty('--uic-cols', String(Number.isFinite(cols) && cols > 0 ? Math.min(6, cols) : 2));
          if (node.gap !== undefined) g.style.setProperty('--uic-gap', String(resolve(node.gap)));
          return g;
        }
        case 'spacer': {
          const sp = document.createElement('div');
          sp.className = 'uic-spacer';
          if (node.size !== undefined) sp.style.setProperty('--uic-size', String(resolve(node.size)));
          return sp;
        }
        case 'link': {
          const a = document.createElement('a');
          a.className = 'uic-link';
          const href = String(asText(node.href) || '');
          // Board-local links (#tab=…) and http(s) only: a data-only description
          // must not be able to produce a javascript: link.
          if (/^(#|\/|https?:\/\/)/.test(href)) {
            // A ROOT-ABSOLUTE href is a board path and has to carry the base
            // prefix, exactly as `image` above already does. It was assigned
            // verbatim, so under `serve --base-path /brd` every /uploads/… link a
            // ui tab drew went to the server root and 404'd — the one component
            // whose whole job is to point somewhere, pointing nowhere.
            a.href = href.startsWith('/') ? api(href) : href;
            if (/^https?:/.test(href)) { a.target = '_blank'; a.rel = 'noreferrer noopener'; }
          }
          a.textContent = asText(node.label) || href;
          return a;
        }
        default: {
          const bad = document.createElement('div');
          bad.className = 'uic-unknown';
          bad.dataset.unknown = String(node.type);
          bad.textContent = `unknown component "${String(node.type)}" — the catalog holds: ` +
            'col, row, grid, card, tabs, title, heading, text, caption, badge, notice, divider, ' +
            'spacer, list, checklist, kv, table, code, quote, image, link, stat, meter, button, field';
          return bad;
        }
      }
    })();

    if (!el) return null;
    if (node.grow) el.classList.add('uic-grow');
    for (const child of kids) {
      const built = build(child);
      if (built) el.append(built);
    }
    return el;
  }

  function render() {
    const tree = ctx.state.root;
    host.replaceChildren();
    if (!tree) {
      const p = document.createElement('p');
      p.className = 'uic-caption';
      p.textContent = 'Nothing described yet — an agent sets state.root to a component tree.';
      host.append(p);
      return;
    }
    const built = build(tree);
    if (built) host.append(built);
  }

  render();
  return {
    refresh() {
      if (root.contains(document.activeElement) &&
          /INPUT|TEXTAREA|SELECT/.test(document.activeElement.tagName)) return;
      render();
    },
  };
}
