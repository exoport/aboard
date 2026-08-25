// Trace view: who did what, when — one lane per actor.
//
// The gap the multi-agent literature names out loud: a shared board buys
// flexibility and pays in traceability. Two sessions and a human writing one
// document, and the only record was `lastEditedBy` — the last one wins and the
// rest is gone.
//
// It reads the server's journal (journal.go), not aboard.json. That matters twice:
// the history is not something an agent can quietly rewrite, and this tab costs
// nothing in the state file — its own state is just how much to show.
//
//   state = { limit: 200, height?: '48vh' }
//
// Deliberately not a gantt: writes are instants, not spans, and pretending
// otherwise would invent durations nobody measured.

import { controlsFor } from './controls.js';
import { api } from './api.js';

const ctl = controlsFor('trace');

const STYLE_ID = 'trace-view-style';

const LANE_COLORS = ['--accent', '--agent', '--mark', '--focus', '--edge'];

const CSS = `
[data-view="trace"] .trace-wrap {
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--sunken);
  padding: 12px 14px;
  overflow-x: auto;
}
[data-view="trace"] .lane {
  display: grid;
  grid-template-columns: 12ch 1fr;
  align-items: center;
  gap: 12px;
  min-height: 34px;
}
[data-view="trace"] .lane-name {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.76rem;
  color: var(--lane-color, var(--muted));
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
[data-view="trace"] .lane-track {
  position: relative;
  height: 22px;
  min-width: 320px;
  border-bottom: 1px dashed var(--line);
}
[data-view="trace"] .event {
  position: absolute;
  top: 4px;
  width: 12px;
  height: 12px;
  margin-left: -6px;
  padding: 0;
  border: 1px solid var(--lane-color, var(--line-strong));
  border-radius: 50%;
  background: var(--bg);
  cursor: pointer;
}
[data-view="trace"] .event:hover,
[data-view="trace"] .event[aria-pressed="true"] { background: var(--lane-color, var(--accent)); }
[data-view="trace"] .axis {
  display: grid;
  grid-template-columns: 12ch 1fr;
  gap: 12px;
  margin-top: 6px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.7rem;
  color: var(--dim);
}
[data-view="trace"] .axis-ends { display: flex; justify-content: space-between; }
[data-view="trace"] .detail {
  margin-top: 12px;
  padding: 10px 12px;
  border: 1px solid var(--line);
  border-left: 3px solid var(--accent-dim);
  border-radius: 3px;
  background: var(--surface);
  font-size: 0.85rem;
}
[data-view="trace"] .detail .mono { color: var(--muted); }
[data-view="trace"] .detail ul { margin: 6px 0 0; padding-left: 18px; }
[data-view="trace"] .empty { color: var(--muted); }
[data-view="trace"] .chip {
  font: inherit; font-size: 0.76rem;
  padding: 3px 9px; border-radius: 999px; cursor: pointer;
  border: 1px solid var(--line-strong); background: transparent; color: var(--dim);
}
[data-view="trace"] .chip[aria-pressed="true"] { border-color: var(--accent); color: var(--accent); }
`;

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

export function mountTrace(root, ctx) {
  injectStyle();

  let entries = [];
  let hidden = new Set();   // actors filtered out — per-viewer
  let selected = null;      // index into entries

  const toolbar = document.createElement('div');
  toolbar.className = 'toolbar';
  const reloadBtn = ctl('reload');
  const chips = document.createElement('span');
  chips.className = 'toolbar-inline';
  chips.style.display = 'inline-flex';
  chips.style.gap = '6px';
  chips.style.flexWrap = 'wrap';
  const meta = document.createElement('span');
  meta.className = 'mono hint';
  toolbar.append(reloadBtn, chips, meta);

  const wrap = document.createElement('div');
  wrap.className = 'trace-wrap';
  const detail = document.createElement('div');
  detail.className = 'detail';
  detail.hidden = true;

  root.append(toolbar, wrap, detail);

  const timeOf = (e) => {
    const t = Date.parse(e && e.at);
    return Number.isFinite(t) ? t : 0;
  };

  function actors() {
    const seen = [];
    for (const e of entries) {
      const by = e.by || 'unknown';
      if (!seen.includes(by)) seen.push(by);
    }
    return seen;
  }

  function colorFor(by) {
    const list = actors();
    return `var(${LANE_COLORS[Math.max(0, list.indexOf(by)) % LANE_COLORS.length]})`;
  }

  function paintChips() {
    chips.replaceChildren();
    for (const by of actors()) {
      const chip = ctl('actor', { label: by, className: 'chip', pressed: !hidden.has(by) });
      chip.style.setProperty('--lane-color', colorFor(by));
      chip.addEventListener('click', () => {
        if (hidden.has(by)) hidden.delete(by);
        else hidden.add(by);
        paint();
      });
      chips.append(chip);
    }
  }

  function paintDetail() {
    if (selected === null || !entries[selected]) { detail.hidden = true; return; }
    const e = entries[selected];
    detail.hidden = false;
    detail.replaceChildren();

    const head = document.createElement('div');
    const who = document.createElement('strong');
    who.textContent = e.by || 'unknown';
    const when = document.createElement('span');
    when.className = 'mono';
    when.textContent = '  ' + (e.at || '');
    head.append(who, when);
    detail.append(head);

    const list = document.createElement('ul');
    for (const id of e.tabs || []) {
      const li = document.createElement('li');
      const name = (e.names && e.names[id]) || '';
      li.textContent = name ? `${id} — ${name}` : id;
      // A tab that no longer exists is still worth naming; the link just may not
      // resolve, which is honest.
      const link = document.createElement('a');
      link.href = `#tab=${id}`;
      link.textContent = ' open';
      li.append(link);
      list.append(li);
    }
    if (!(e.tabs || []).length) {
      const li = document.createElement('li');
      li.textContent = 'no tab changed';
      list.append(li);
    }
    detail.append(list);

    if (e.origin) {
      const origin = document.createElement('p');
      origin.className = 'hint';
      origin.textContent = 'origin: ' + e.origin;
      detail.append(origin);
    }
  }

  function paint() {
    paintChips();
    wrap.replaceChildren();

    const visible = entries.filter((e) => !hidden.has(e.by || 'unknown'));
    if (!visible.length) {
      const p = document.createElement('p');
      p.className = 'empty';
      p.textContent = entries.length
        ? 'Every actor is filtered out.'
        : 'No writes recorded yet — the journal fills as the board changes.';
      wrap.append(p);
      meta.textContent = '';
      paintDetail();
      return;
    }

    const times = visible.map(timeOf);
    const first = Math.min(...times);
    const last = Math.max(...times);
    const span = Math.max(1, last - first);

    for (const by of actors()) {
      if (hidden.has(by)) continue;
      const lane = document.createElement('div');
      lane.className = 'lane';
      lane.style.setProperty('--lane-color', colorFor(by));

      const name = document.createElement('div');
      name.className = 'lane-name';
      name.textContent = by;
      name.title = by;

      const track = document.createElement('div');
      track.className = 'lane-track';

      entries.forEach((e, i) => {
        if ((e.by || 'unknown') !== by) return;
        const dot = ctl('event', { label: '', className: 'event' });
        dot.style.left = (((timeOf(e) - first) / span) * 100).toFixed(3) + '%';
        dot.setAttribute('aria-pressed', String(selected === i));
        const tabs = (e.tabs || []).join(', ') || 'no tab';
        dot.title = `${e.at} — ${tabs}`;
        dot.setAttribute('aria-label', `${by} at ${e.at}: ${tabs}`);
        dot.addEventListener('click', () => {
          selected = selected === i ? null : i;
          paint();
        });
        track.append(dot);
      });

      lane.append(name, track);
      wrap.append(lane);
    }

    const axis = document.createElement('div');
    axis.className = 'axis';
    axis.append(document.createElement('span'));
    const ends = document.createElement('div');
    ends.className = 'axis-ends';
    const a = document.createElement('span');
    a.textContent = new Date(first).toLocaleTimeString();
    const b = document.createElement('span');
    b.textContent = new Date(last).toLocaleTimeString();
    ends.append(a, b);
    axis.append(ends);
    wrap.append(axis);

    meta.textContent = `${visible.length} write${visible.length === 1 ? '' : 's'} · ${actors().length} actor${actors().length === 1 ? '' : 's'}`;
    paintDetail();
  }

  async function load() {
    const limit = Number(ctx.state.limit) > 0 ? Number(ctx.state.limit) : 200;
    try {
      const res = await fetch(api(`/journal?limit=${limit}`), { cache: 'no-store' });
      if (!res.ok) return;
      const body = await res.json();
      entries = Array.isArray(body.entries) ? body.entries : [];
      selected = null;
      paint();
    } catch {
      // Leave whatever is on screen: an empty trace is worse than a stale one.
    }
  }

  reloadBtn.addEventListener('click', load);

  paint();
  load();

  return {
    // Every board write is a new journal entry, so the live reload is exactly the
    // right trigger to refetch on.
    refresh: load,
  };
}
