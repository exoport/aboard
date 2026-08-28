// Kanban view: nodes grouped by `status`, ordered by `order`.
//
// `parent` and `status` are deliberately independent, so moving a card between
// columns never changes where it sits in the tree. The DAG view reads the same
// nodes through their parent links.
//
// `state.readOnly: true` makes the board agent-owned and the human a reader: the
// agent still writes it, the browser never does. The affordances are REMOVED
// rather than ignored — no drag, no editable title, no reorder or delete buttons —
// because a card that can be dragged and then snaps back reads as a bug, and a
// disabled button invites clicking it to find out why. What stays is everything
// that carries information: titles, notes, id chips, counts.

import { openContextMenu, copyText, referenceFor } from './menu.js';
import { attachHeartbeat } from './heartbeat.js';
import { controlsFor } from './controls.js';

const ctl = controlsFor('kanban');

const STYLE_ID = 'kanban-view-style';

const CSS = `
[data-view="kanban"] .columns {
  display: grid;
  grid-template-columns: repeat(var(--cols, 3), minmax(0, 1fr));
  gap: 12px;
}
@media (max-width: 700px) {
  [data-view="kanban"] .columns { grid-template-columns: minmax(0, 1fr); }
}
[data-view="kanban"] .column {
  background: var(--sunken);
  border: 1px solid var(--line);
  border-radius: 5px;
  padding: 11px;
  display: flex;
  flex-direction: column;
  gap: 9px;
  min-height: 130px;
}
[data-view="kanban"] .column[data-dragover="yes"] {
  background: var(--drop);
  border-color: var(--accent);
}
[data-view="kanban"] .column-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.75rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
}
[data-view="kanban"] .card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 4px;
  padding: 10px 11px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  cursor: grab;
}
[data-view="kanban"] .card[data-dragging="yes"] { opacity: 0.4; }
[data-view="kanban"] .card-title {
  font-weight: 550;
  line-height: 1.35;
  outline-offset: 3px;
  word-break: break-word;
}
[data-view="kanban"] .card-note {
  font-size: 0.84rem;
  color: var(--muted);
  line-height: 1.45;
  word-break: break-word;
}
[data-view="kanban"] .card-foot {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}
[data-view="kanban"] .id-chip {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.75rem;
  color: var(--muted);
  padding: 1px 5px;
  border: 1px solid var(--line);
  border-radius: 2px;
}
[data-view="kanban"] select { max-width: 130px; font-size: 0.79rem; }
[data-view="kanban"] .columns[data-readonly="yes"] .card { cursor: default; }
[data-view="kanban"] .card[data-picked="yes"] {
  border-color: var(--focus);
  box-shadow: inset 3px 0 0 var(--focus);
}
[data-view="kanban"] .card[data-flash="yes"] {
  border-color: var(--accent);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 28%, transparent);
}
[data-view="kanban"] .ro-badge {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.72rem;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--agent);
  border: 1px solid var(--agent);
  border-radius: 2px;
  padding: 2px 6px;
}
`;

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

export function mountKanban(root, ctx) {
  injectStyle();

  // Read at render time, not captured: an agent write can flip it under an open
  // page, and the live reload only re-renders.
  const readOnly = () => ctx.state.readOnly === true;

  // Keyboard selection, per-viewer: dragging is fine once and tiring by the
  // twentieth card, and a drag that misses its column is a silent no-op.
  let picked = null;

  const toolbar = document.createElement('div');
  toolbar.className = 'toolbar';
  const addBtn = ctl('add', { className: 'primary-btn' });
  const badge = document.createElement('span');
  badge.className = 'ro-badge';
  badge.textContent = 'read-only';
  const note = document.createElement('span');
  note.className = 'hint';

  // Is the agent that maintains this board still on duty? On a read-only tab the
  // question matters more, not less: the tab looks equally authoritative whether
  // it was updated a minute ago or abandoned an hour ago.
  const beat = attachHeartbeat(() => ctx.state.heartbeat);

  toolbar.append(addBtn, badge, beat.el, note);

  const columns = document.createElement('div');
  columns.className = 'columns';

  root.append(toolbar, columns);

  const cols = () => ctx.state.columns || ['todo', 'doing', 'done'];
  const cols_ = cols;
  const nodes = () => ctx.state.nodes || [];

  function byId(id) {
    return nodes().find((n) => n.id === id) || null;
  }

  // A stack block's ctx.tab.id is "<tab>/<block>"; a link needs the tab.
  function tabIdOf() {
    return String((ctx.tab && ctx.tab.id) || '').split('/')[0];
  }

  function cardMarkdown(node) {
    const lines = [`- **${node.title}** \`${node.id}\` — _${node.status}_`];
    if (node.note) lines.push(`  ${node.note}`);
    return lines.join('\n');
  }

  function subtreeMarkdown(node, depth = 0) {
    const pad = '  '.repeat(depth);
    const out = [`${pad}- **${node.title}** \`${node.id}\` — _${node.status}_`];
    if (node.note) out.push(`${pad}  ${node.note}`);
    for (const child of childrenOf(node.id)) out.push(subtreeMarkdown(child, depth + 1));
    return out.join('\n');
  }

  function childrenOf(id) {
    return nodes().filter((n) => n.parent === id);
  }

  // Every id at or below `id` — keeps the parent dropdown acyclic.
  function subtreeIds(id) {
    const out = new Set([id]);
    const walk = (pid) => {
      for (const child of childrenOf(pid)) {
        if (out.has(child.id)) continue;
        out.add(child.id);
        walk(child.id);
      }
    };
    walk(id);
    return out;
  }

  function inColumn(status) {
    return nodes()
      .filter((n) => n.status === status)
      .sort((a, b) => a.order - b.order);
  }

  function renormalize() {
    for (const status of cols()) {
      inColumn(status).forEach((node, i) => { node.order = i; });
    }
  }

  // Board-wide monotonic id from the shell: deleting the last node must not let
  // the next one reuse its id, or any instruction referencing it re-points.
  function nextId() {
    if (typeof ctx.nextId === 'function') return ctx.nextId();
    let max = 0;
    for (const n of nodes()) {
      const hit = /^[a-z]*(\d+)$/.exec(n.id);
      if (hit) max = Math.max(max, Number(hit[1]));
    }
    return 'n' + (max + 1);
  }

  async function commit() {
    renormalize();
    await ctx.save();
    render();
    ctx.refreshOthers?.('kanban');
  }

  function reorder(node, delta) {
    const siblings = inColumn(node.status);
    const idx = siblings.findIndex((n) => n.id === node.id);
    const target = idx + delta;
    if (target < 0 || target >= siblings.length) return;
    const [moved] = siblings.splice(idx, 1);
    siblings.splice(target, 0, moved);
    siblings.forEach((n, i) => { n.order = i; });
    commit();
  }

  function makeCard(node) {
    const ro = readOnly();
    const card = document.createElement('article');
    card.className = 'card';
    card.draggable = !ro;
    card.dataset.id = node.id;
    if (node.id === picked) card.dataset.picked = 'yes';
    card.addEventListener('click', () => { picked = node.id; paintPicked(); });

    const title = document.createElement('div');
    title.className = 'card-title';
    title.textContent = node.title;
    if (!ro) {
      title.contentEditable = 'true';
      title.spellcheck = false;
      title.addEventListener('blur', () => {
        const text = title.textContent.trim();
        if (!text) { title.textContent = node.title; return; }
        if (text !== node.title) {
          node.title = text;
          ctx.save().then(() => ctx.refreshOthers?.('kanban'));
        }
      });
      title.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') { e.preventDefault(); title.blur(); }
      });
    }
    card.append(title);

    if (node.note) {
      const noteEl = document.createElement('div');
      noteEl.className = 'card-note';
      noteEl.textContent = node.note;
      card.append(noteEl);
    }

    const foot = document.createElement('div');
    foot.className = 'card-foot';

    const chip = document.createElement('span');
    chip.className = 'id-chip';
    chip.textContent = node.id;
    foot.append(chip);

    // Read-only stops here: the id chip is information, everything below is a
    // control. Nothing is appended, so there is nothing to click and nothing to
    // explain.
    if (ro) {
      card.append(foot);
      return card;
    }

    const parentSelect = document.createElement('select');
    parentSelect.title = 'parent';
    const blocked = subtreeIds(node.id);
    const rootOpt = document.createElement('option');
    rootOpt.value = '';
    rootOpt.textContent = 'no parent';
    parentSelect.append(rootOpt);
    for (const other of nodes()) {
      if (blocked.has(other.id)) continue;
      const opt = document.createElement('option');
      opt.value = other.id;
      opt.textContent = '↳ ' + other.title.slice(0, 22);
      parentSelect.append(opt);
    }
    parentSelect.value = node.parent || '';
    parentSelect.addEventListener('change', () => {
      node.parent = parentSelect.value || null;
      commit();
    });
    foot.append(parentSelect);

    const siblings = inColumn(node.status);
    const idx = siblings.findIndex((n) => n.id === node.id);

    const up = ctl('move-up', { disabled: idx <= 0, onClick: () => reorder(node, -1) });
    foot.append(up);

    const down = ctl('move-down', { disabled: idx === siblings.length - 1, onClick: () => reorder(node, 1) });
    foot.append(down);

    const del = ctl('delete');
    del.addEventListener('click', () => {
      for (const child of childrenOf(node.id)) child.parent = node.parent;
      ctx.state.nodes = nodes().filter((n) => n.id !== node.id);
      commit();
    });
    foot.append(del);

    card.append(foot);

    // Right-click: the id and a link to this exact card, without reading it off
    // the screen and typing it back. Shift+right-click falls through to the
    // browser's own menu.
    card.addEventListener('contextmenu', (e) => {
      if (e.shiftKey) return;
      const ro = readOnly();
      openContextMenu(e, [
        { head: node.id },
        { label: 'Copy id', hint: node.id, run: (ev) => copyText(node.id, ev) },
        { label: 'Copy link to this card', hint: 'tab + node',
          run: (ev) => copyText(referenceFor(tabIdOf(), node.id), ev) },
        { label: 'Copy as markdown', run: (ev) => copyText(cardMarkdown(node), ev) },
        subtreeIds(node.id).size > 1 && { label: 'Copy subtree as markdown',
          hint: subtreeIds(node.id).size - 1 + ' below', run: (ev) => copyText(subtreeMarkdown(node), ev) },
        !ro && 'separator',
        !ro && { label: 'Delete', danger: true, hint: 'children move up', run: () => {
          for (const child of childrenOf(node.id)) child.parent = node.parent;
          ctx.state.nodes = nodes().filter((n) => n.id !== node.id);
          commit();
        } },
      ]);
    });

    card.addEventListener('dragstart', (e) => {
      e.dataTransfer.setData('text/plain', node.id);
      e.dataTransfer.effectAllowed = 'move';
      card.dataset.dragging = 'yes';
    });
    card.addEventListener('dragend', () => { delete card.dataset.dragging; });

    return card;
  }

  function makeColumn(status) {
    const col = document.createElement('section');
    col.className = 'column';
    col.dataset.status = status;

    const head = document.createElement('div');
    head.className = 'column-head';
    const label = document.createElement('span');
    label.textContent = status;
    const items = inColumn(status);
    const count = document.createElement('span');
    count.className = 'mono';
    count.textContent = String(items.length);
    head.append(label, count);
    col.append(head);

    for (const node of items) col.append(makeCard(node));

    if (readOnly()) return col;   // no drop target, so no drag affordance at all

    col.addEventListener('dragover', (e) => {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
      col.dataset.dragover = 'yes';
    });
    col.addEventListener('dragleave', () => { delete col.dataset.dragover; });
    col.addEventListener('drop', (e) => {
      e.preventDefault();
      delete col.dataset.dragover;
      const node = byId(e.dataTransfer.getData('text/plain'));
      if (!node) return;
      if (node.status !== status) {
        node.status = status;
        node.order = inColumn(status).length;
      }
      commit();
    });

    return col;
  }

  // Cheaper than a re-render, and it must not disturb a card being renamed.
  function paintPicked() {
    for (const col of columns.children) {
      for (const card of col.querySelectorAll('.card')) {
        if (card.dataset.id === picked) card.dataset.picked = 'yes';
        else delete card.dataset.picked;
      }
    }
  }

  function pickedNode() {
    return picked ? byId(picked) : null;
  }

  // j/k walk the current column, h/l move the card between columns and the
  // selection FOLLOWS it — the whole point, since otherwise you lose your place
  // on every move. Shift+j/k reorders instead of walking.
  function onKey(e) {
    const cols = cols_();
    if (!cols.length) return false;

    if (e.key === 'j' || e.key === 'k' || e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      const down = e.key === 'j' || e.key === 'ArrowDown';
      const node = pickedNode();
      const status = node ? node.status : cols[0];
      const list = inColumn(status);
      if (!list.length) return false;
      const at = node ? list.findIndex((n) => n.id === node.id) : -1;
      const next = list[Math.min(list.length - 1, Math.max(0, at + (down ? 1 : -1)))] || list[0];
      picked = next.id;
      paintPicked();
      columns.querySelector(`.card[data-id="${CSS.escape(picked)}"]`)
        ?.scrollIntoView({ block: 'nearest' });
      return true;
    }

    if (e.key === 'J' || e.key === 'K') {
      const node = pickedNode();
      if (!node || readOnly()) return false;
      reorder(node, e.key === 'J' ? 1 : -1);
      return true;
    }

    if (e.key === 'h' || e.key === 'l' || e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      const node = pickedNode();
      if (!node) return false;
      if (readOnly()) return true;   // swallow it: this board is not yours to rearrange
      const at = cols.indexOf(node.status);
      const to = cols[at + (e.key === 'l' || e.key === 'ArrowRight' ? 1 : -1)];
      if (!to) return true;
      node.status = to;
      node.order = inColumn(to).length;
      commit().then(() => {
        paintPicked();
        columns.querySelector(`.card[data-id="${CSS.escape(node.id)}"]`)
          ?.scrollIntoView({ block: 'nearest' });
      });
      return true;
    }

    if (e.key === 'Enter') {
      const node = pickedNode();
      if (!node || readOnly()) return false;
      const title = columns.querySelector(`.card[data-id="${CSS.escape(node.id)}"] .card-title`);
      title?.focus();
      return true;
    }

    return false;
  }

  function render() {
    const ro = readOnly();
    beat.refresh();
    addBtn.hidden = ro;
    badge.hidden = !ro;
    note.textContent = ro
      ? 'An agent maintains this board — it is yours to read, not to rearrange.'
      : 'Drag between columns · ▲▼ reorder · parent dropdown restructures the tree';

    const list = cols();
    columns.style.setProperty('--cols', String(list.length));
    if (ro) columns.dataset.readonly = 'yes';
    else delete columns.dataset.readonly;
    columns.replaceChildren(...list.map(makeColumn));
  }

  addBtn.addEventListener('click', () => {
    const status = cols()[0];
    const node = {
      id: nextId(),
      title: 'New node',
      parent: null,
      status,
      order: inColumn(status).length,
      note: '',
    };
    nodes().push(node);
    commit().then(() => {
      const fresh = columns.querySelector(`.card[data-id="${node.id}"] .card-title`);
      if (!fresh) return;
      fresh.focus();
      const range = document.createRange();
      range.selectNodeContents(fresh);
      const sel = getSelection();
      sel.removeAllRanges();
      sel.addRange(range);
    });
  });

  // Deep links: the shell calls this after activating the tab, so
  // #tab=ab71&node=ab57 lands on the card rather than on the tab.
  function focus(id) {
    const card = columns.querySelector(`.card[data-id="${CSS.escape(String(id))}"]`);
    if (!card) return false;
    card.scrollIntoView({ block: 'center', behavior: 'auto' });
    card.dataset.flash = 'yes';
    setTimeout(() => { delete card.dataset.flash; }, 2400);
    return true;
  }

  render();
  return { refresh: render, focus, onKey, destroy: beat.destroy };
}
