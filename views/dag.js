// DAG view: the same nodes as the kanban, read through their `parent` links.
//
// Layout is a tidy tree — leaves take sequential slots, parents centre over
// their children — recomputed on every render unless a node carries an explicit
// `pos`, which a human drag writes. Dropping a node ONTO another reparents it
// and clears its `pos`, so the tree re-tidies around the new structure.

import { openContextMenu, copyText, referenceFor } from './menu.js';
import { button, controlsFor } from './controls.js';

const ctl = controlsFor('dag');

const STYLE_ID = 'dag-view-style';

const NODE_W = 168;
const NODE_H = 48;
const GAP_Y = 104;

const CSS = `
[data-view="dag"] .canvas-wrap {
  position: relative;
  background: var(--sunken);
  border: 1px solid var(--line);
  border-radius: 5px;
  overflow: hidden;
  /* How tall the canvas should be is the agent's call, not the renderer's: set
     state.height to any CSS length. This is the fallback. */
  height: var(--canvas-height, calc(100vh - 260px));
  min-height: 260px;
}
[data-view="dag"] svg { display: block; width: 100%; height: 100%; touch-action: none; }
[data-view="dag"] svg.panning { cursor: grabbing; }
[data-view="dag"] .node-box { cursor: grab; }
[data-view="dag"] .node-box rect.body {
  fill: var(--surface);
  stroke: var(--line);
  stroke-width: 1;
}
[data-view="dag"] .node-box[data-selected="yes"] rect.body {
  stroke: var(--accent);
  stroke-width: 2;
}
[data-view="dag"] .node-box[data-drop="yes"] rect.body {
  stroke: var(--accent);
  stroke-width: 2.5;
  fill: var(--drop);
}
[data-view="dag"] .node-box[data-dragging="yes"] { opacity: 0.65; }
[data-view="dag"] .node-title {
  font-size: 13px;
  font-weight: 550;
  fill: var(--text);
  pointer-events: none;
}
[data-view="dag"] .node-id {
  font-size: 11px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  fill: var(--muted);
  pointer-events: none;
}
[data-view="dag"] .edge {
  fill: none;
  stroke: var(--edge);
  stroke-width: 1.5;
}
[data-view="dag"] .detail {
  margin-top: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 5px;
  padding: 10px 12px;
}
[data-view="dag"] .editor-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
[data-view="dag"] .editor-row .title-input { flex: 1 1 160px; min-width: 120px; }
[data-view="dag"] .editor-row select { flex: 0 0 auto; max-width: 170px; }
[data-view="dag"] .id-chip {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.75rem;
  color: var(--muted);
  padding: 1px 5px;
  border: 1px solid var(--line);
  border-radius: 2px;
  flex: 0 0 auto;
}
[data-view="dag"] .note-input {
  width: 100%;
  min-height: 52px;
  font-size: 0.85rem;
}
[data-view="dag"] .editor-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
[data-view="dag"] .rename-input {
  position: absolute;
  box-sizing: border-box;
  font: inherit;
  font-weight: 550;
  padding: 0 6px;
  border: 1px solid var(--accent);
  border-radius: 4px;
  background: var(--surface);
  color: var(--text);
}
[data-view="dag"] .empty {
  display: grid;
  place-items: center;
  height: 100%;
  color: var(--muted);
  font-size: 0.9rem;
}
`;

const STATUS_COLOR = {
  todo: 'var(--status-todo)',
  doing: 'var(--status-doing)',
  done: 'var(--status-done)',
};

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

const svgEl = (name, attrs = {}) => {
  const el = document.createElementNS('http://www.w3.org/2000/svg', name);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, String(v));
  return el;
};

export function mountDag(root, ctx) {
  injectStyle();

  // Per-viewer UI state — deliberately NOT part of board.json.
  let selectedId = null;
  let fitted = false;
  let view = { tx: 40, ty: 40, k: 1 };
  let drag = null;   // { id, moved, pointerId, startLayout, origin }
  let pan = null;    // { startX, startY, tx, ty, moved }
  let editingId = null;     // node id being renamed inline on the canvas
  let renameInput = null;   // the overlay <input> for that rename, if any
  let renamePos = null;     // its node's layout {x,y}, so pan/zoom can reposition it

  const toolbar = document.createElement('div');
  toolbar.className = 'toolbar';
  const addRootBtn = ctl('add-root');
  const relayoutBtn = ctl('relayout');
  const fitBtn = ctl('fit');
  const zoomIn = ctl('zoom-in');
  const zoomOut = ctl('zoom-out');
  const hint = document.createElement('span');
  hint.className = 'hint';
  hint.textContent = 'Drag a node to move it · double-click to rename · drop it ON another node to reparent · drag background to pan · click empty space to deselect · wheel to zoom';
  toolbar.append(addRootBtn, relayoutBtn, fitBtn, zoomIn, zoomOut, hint);

  const wrap = document.createElement('div');
  wrap.className = 'canvas-wrap';
  applyHeight();
  const svg = svgEl('svg');
  const scene = svgEl('g');
  const edgeLayer = svgEl('g');
  const nodeLayer = svgEl('g');
  scene.append(edgeLayer, nodeLayer);
  svg.append(scene);
  wrap.append(svg);

  const detail = document.createElement('div');
  detail.className = 'detail';

  // Confirm-before-delete modal — matches the .sheet-dialog pattern used for
  // the new-tab dialog, so a destructive action never hides behind a native window prompt.
  const deleteDialog = document.createElement('dialog');
  deleteDialog.className = 'sheet-dialog';
  const deleteForm = document.createElement('form');
  const deleteHead = document.createElement('p');
  deleteHead.className = 'panel-head';
  deleteHead.textContent = 'Delete node';
  const deleteMsg = document.createElement('p');
  deleteMsg.className = 'hint';
  const deleteActions = document.createElement('div');
  deleteActions.className = 'dialog-actions';
  const deleteCancel = button('Cancel', 'Keep this node');
  const deleteConfirm = button('Delete', '', { type: 'submit', className: 'icon-btn icon-btn--danger' });
  deleteActions.append(deleteCancel, deleteConfirm);
  deleteForm.append(deleteHead, deleteMsg, deleteActions);
  deleteDialog.append(deleteForm);

  root.append(toolbar, wrap, detail, deleteDialog);

  let pendingDeleteId = null;
  deleteCancel.addEventListener('click', () => deleteDialog.close());
  deleteDialog.addEventListener('close', () => { pendingDeleteId = null; });
  deleteForm.addEventListener('submit', (e) => {
    e.preventDefault();
    const id = pendingDeleteId;
    deleteDialog.close();
    if (id) performDelete(id);
  });

  function confirmDelete(node) {
    const kids = childrenOf(node.id);
    pendingDeleteId = node.id;
    const parentLabel = node.parent ? (byId(node.parent)?.title || node.parent) : 'no parent — they will become roots';
    deleteMsg.textContent = kids.length
      ? `Delete "${node.title}" (${node.id})? ${kids.length} child ${kids.length === 1 ? 'node' : 'nodes'} will be re-parented to ${parentLabel}.`
      : `Delete "${node.title}" (${node.id})? This cannot be undone.`;
    deleteDialog.showModal();
  }

  function performDelete(id) {
    const node = byId(id);
    if (!node) return;
    for (const child of childrenOf(node.id)) child.parent = node.parent;
    ctx.state.nodes = nodes().filter((n) => n.id !== node.id);
    if (selectedId === id) selectedId = null;
    commit();
  }

  const nodes = () => ctx.state.nodes || [];
  const cols = () => ctx.state.columns || ['todo', 'doing', 'done'];
  const byId = (id) => nodes().find((n) => n.id === id) || null;
  const childrenOf = (id) => nodes()
    .filter((n) => n.parent === id)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0) || a.title.localeCompare(b.title));

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

  // Fallback only — a board-wide monotonic allocator (ctx.nextId) is the primary
  // path so ids are never reused after a delete. This just covers ctx not having it yet.
  function localNextId() {
    let max = 0;
    for (const n of nodes()) {
      const m = /^[a-z]*(\d+)$/.exec(n.id);
      if (m) max = Math.max(max, Number(m[1]));
    }
    return 'n' + (max + 1);
  }

  function addNode(parentId) {
    const status = cols()[0];
    const order = nodes().filter((n) => n.status === status).length;
    const node = {
      id: typeof ctx.nextId === 'function' ? ctx.nextId() : localNextId(),
      title: 'New node',
      parent: parentId || null,
      status,
      order,
      note: '',
    };
    nodes().push(node);
    selectedId = node.id;
    commit().then(focusTitleInput);
  }

  function focusTitleInput() {
    const input = detail.querySelector('.title-input');
    if (!input) return;
    input.focus();
    input.select();
  }

  // Spacing lives in this tab's own state now that tabs no longer share a
  // document-wide namespace. Set state.density to widen or tighten the tree.
  // state.height: any CSS length ("70vh", "540px"), or a number read as px.
  function applyHeight() {
    const h = ctx.state.height;
    if (h === undefined || h === null || h === '') {
      wrap.style.removeProperty('--canvas-height');
      return;
    }
    wrap.style.setProperty('--canvas-height', typeof h === 'number' ? `${h}px` : String(h));
  }

  function gapX() {
    const raw = Number(ctx.state.density);
    return Number.isFinite(raw) && raw >= 60 ? raw + NODE_W - 60 : NODE_W + 40;
  }

  // Tidy-tree layout. Leaves consume sequential horizontal slots; a parent sits
  // at the midpoint of its children. `visited` also guards against a cycle in
  // the parent links, which a bad hand-edit of board.json could introduce.
  function layout() {
    const all = nodes();
    const pos = new Map();
    const visited = new Set();
    const stepX = gapX();
    let cursor = 0;

    const walk = (node, depth) => {
      if (visited.has(node.id)) return null;
      visited.add(node.id);
      const kids = childrenOf(node.id);
      let x;
      if (!kids.length) {
        x = cursor * stepX;
        cursor += 1;
      } else {
        const xs = kids.map((k) => walk(k, depth + 1)).filter((v) => v !== null);
        x = xs.length ? (Math.min(...xs) + Math.max(...xs)) / 2 : (cursor++ * stepX);
      }
      pos.set(node.id, { x, y: depth * GAP_Y });
      return x;
    };

    const ids = new Set(all.map((n) => n.id));
    const roots = all.filter((n) => !n.parent || !ids.has(n.parent));
    for (const r of roots) {
      walk(r, 0);
      cursor += 0.6;   // breathing room between separate trees
    }
    // Anything left is inside a cycle or otherwise unreachable — show it anyway.
    for (const n of all) {
      if (!visited.has(n.id)) walk(n, 0);
    }

    // An explicit pos from a human drag wins over the computed slot.
    for (const n of all) {
      if (n.pos && Number.isFinite(n.pos.x) && Number.isFinite(n.pos.y)) {
        pos.set(n.id, { x: n.pos.x, y: n.pos.y });
      }
    }
    return pos;
  }

  function truncate(text, max) {
    return text.length > max ? text.slice(0, max - 1) + '…' : text;
  }

  // A stack block's ctx.tab.id is "<tab>/<block>"; a link needs the tab itself.
  function tabIdOf() {
    return String((ctx.tab && ctx.tab.id) || '').split('/')[0];
  }

  function nodeMarkdown(node) {
    const out = [`- **${node.title}** \`${node.id}\` — _${node.status}_`];
    if (node.note) out.push(`  ${node.note}`);
    return out.join('\n');
  }

  function subtreeMarkdown(node, depth = 0) {
    const pad = '  '.repeat(depth);
    const out = [`${pad}- **${node.title}** \`${node.id}\` — _${node.status}_`];
    if (node.note) out.push(`${pad}  ${node.note}`);
    for (const child of nodes().filter((n) => n.parent === node.id)) {
      out.push(subtreeMarkdown(child, depth + 1));
    }
    return out.join('\n');
  }

  function selectNode(id) {
    selectedId = id;
    for (const g of nodeLayer.children) {
      g.dataset.selected = g.dataset.id === id ? 'yes' : 'no';
    }
    renderDetail();
  }

  function applyView() {
    scene.setAttribute('transform', `translate(${view.tx} ${view.ty}) scale(${view.k})`);
    positionRenameInput();
  }

  function layoutPoint(evt) {
    const rect = svg.getBoundingClientRect();
    return {
      x: (evt.clientX - rect.left - view.tx) / view.k,
      y: (evt.clientY - rect.top - view.ty) / view.k,
    };
  }

  function fit(pos) {
    if (!pos.size) return;
    const xs = [...pos.values()].map((p) => p.x);
    const ys = [...pos.values()].map((p) => p.y);
    const minX = Math.min(...xs) - NODE_W;
    const maxX = Math.max(...xs) + NODE_W;
    const minY = Math.min(...ys) - NODE_H;
    const maxY = Math.max(...ys) + NODE_H;
    const rect = svg.getBoundingClientRect();
    if (!rect.width || !rect.height) return;
    const k = Math.min(rect.width / (maxX - minX), rect.height / (maxY - minY), 1.4);
    view.k = Math.max(0.25, k);
    view.tx = -minX * view.k + (rect.width - (maxX - minX) * view.k) / 2;
    view.ty = -minY * view.k + (rect.height - (maxY - minY) * view.k) / 2;
    applyView();
  }

  async function commit({ rerender = true } = {}) {
    await ctx.save();
    if (rerender) render();
    ctx.refreshOthers?.('dag');
  }

  function makeNode(node, p) {
    const g = svgEl('g', { class: 'node-box', 'data-id': node.id });
    g.dataset.selected = node.id === selectedId ? 'yes' : 'no';

    const x = p.x - NODE_W / 2;
    const y = p.y - NODE_H / 2;

    g.append(svgEl('rect', {
      class: 'body', x, y, width: NODE_W, height: NODE_H, rx: 5,
    }));
    g.append(svgEl('rect', {
      x, y, width: 4, height: NODE_H,
      fill: STATUS_COLOR[node.status] || 'var(--muted)',
      rx: 2,
    }));

    const title = svgEl('text', { class: 'node-title', x: x + 13, y: y + 20 });
    title.textContent = truncate(node.title || '(untitled)', 22);
    g.append(title);

    const sub = svgEl('text', { class: 'node-id', x: x + 13, y: y + 36 });
    sub.textContent = `${node.id} · ${node.status || '—'}`;
    g.append(sub);

    const label = svgEl('title');
    label.textContent = node.title + (node.note ? '\n' + node.note : '');
    g.append(label);

    g.addEventListener('pointerdown', (e) => {
      e.stopPropagation();
      g.setPointerCapture(e.pointerId);
      drag = {
        id: node.id,
        pointerId: e.pointerId,
        moved: false,
        start: layoutPoint(e),
        origin: { x: p.x, y: p.y },
        blocked: subtreeIds(node.id),
        el: g,
      };
    });

    g.addEventListener('dblclick', (e) => {
      e.stopPropagation();
      startInlineRename(node, p);
    });

    // Right-click: the id, and a link that reopens this exact node. Shift falls
    // through to the browser's own menu.
    g.addEventListener('contextmenu', (e) => {
      if (e.shiftKey) return;
      e.stopPropagation();
      selectNode(node.id);
      const kids = subtreeIds(node.id).size - 1;
      openContextMenu(e, [
        { head: node.id },
        { label: 'Copy id', hint: node.id, run: (ev) => copyText(node.id, ev) },
        { label: 'Copy link to this node', hint: 'tab + node',
          run: (ev) => copyText(referenceFor(tabIdOf(), node.id), ev) },
        { label: 'Copy as markdown', run: (ev) => copyText(nodeMarkdown(node), ev) },
        kids > 0 && { label: 'Copy subtree as markdown', hint: kids + ' below',
          run: (ev) => copyText(subtreeMarkdown(node), ev) },
        'separator',
        { label: 'Add child', run: () => addNode(node.id) },
        { label: 'Delete…', danger: true, hint: 'asks first', run: () => confirmDelete(node) },
      ]);
    });

    return g;
  }

  // Renaming happens through a plain HTML input laid over the SVG node, since
  // SVG has no inline text editing of its own. Position tracks view (pan/zoom)
  // so it stays glued to the node while open.
  function positionRenameInput() {
    if (!renameInput || !renamePos) return;
    renameInput.style.left = `${view.tx + (renamePos.x - NODE_W / 2) * view.k}px`;
    renameInput.style.top = `${view.ty + (renamePos.y - NODE_H / 2) * view.k}px`;
    renameInput.style.width = `${NODE_W * view.k}px`;
    renameInput.style.height = `${NODE_H * view.k}px`;
    renameInput.style.fontSize = `${Math.max(10, Math.min(22, 13 * view.k))}px`;
  }

  function startInlineRename(node, p) {
    if (editingId) return;
    editingId = node.id;
    renamePos = p;
    const input = document.createElement('input');
    input.type = 'text';
    input.className = 'rename-input';
    input.value = node.title || '';
    wrap.append(input);
    renameInput = input;
    positionRenameInput();
    input.focus();
    input.select();

    let cancelled = false;
    const cleanup = () => {
      input.remove();
      renameInput = null;
      renamePos = null;
      editingId = null;
    };
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); input.blur(); }
      else if (e.key === 'Escape') { cancelled = true; input.blur(); }
    });
    input.addEventListener('blur', () => {
      const text = input.value.trim();
      cleanup();
      if (!cancelled && text && text !== node.title) {
        node.title = text;
        commit();
      }
    });
  }

  function render() {
    const list = nodes();
    detail.replaceChildren();

    if (!list.length) {
      edgeLayer.replaceChildren();
      nodeLayer.replaceChildren();
      const empty = document.createElement('div');
      empty.className = 'empty';
      empty.textContent = 'No nodes yet — add some in the Kanban tab.';
      wrap.replaceChildren(empty);
      return;
    }
    if (!wrap.contains(svg)) wrap.replaceChildren(svg);

    const pos = layout();

    const edges = [];
    for (const node of list) {
      if (!node.parent) continue;
      const from = pos.get(node.parent);
      const to = pos.get(node.id);
      if (!from || !to) continue;
      const y1 = from.y + NODE_H / 2;
      const y2 = to.y - NODE_H / 2;
      const mid = (y1 + y2) / 2;
      edges.push(svgEl('path', {
        class: 'edge',
        d: `M ${from.x} ${y1} C ${from.x} ${mid}, ${to.x} ${mid}, ${to.x} ${y2}`,
      }));
    }
    edgeLayer.replaceChildren(...edges);
    nodeLayer.replaceChildren(...list.map((n) => makeNode(n, pos.get(n.id) || { x: 0, y: 0 })));

    applyView();
    renderDetail();
    return pos;
  }

  function renderDetail() {
    const node = selectedId ? byId(selectedId) : null;
    if (!node) {
      const p = document.createElement('span');
      p.className = 'hint';
      p.textContent = 'Click a node to select it, or use "Add root" above.';
      detail.replaceChildren(p);
      return;
    }

    const chip = document.createElement('span');
    chip.className = 'id-chip';
    chip.textContent = node.id;

    const titleInput = document.createElement('input');
    titleInput.type = 'text';
    titleInput.className = 'title-input';
    titleInput.value = node.title || '';
    titleInput.setAttribute('aria-label', 'Title');
    titleInput.addEventListener('change', () => {
      const text = titleInput.value.trim();
      if (!text) { titleInput.value = node.title; return; }
      if (text !== node.title) {
        node.title = text;
        commit();
      }
    });

    const statusSelect = document.createElement('select');
    statusSelect.setAttribute('aria-label', 'Status');
    for (const status of cols()) {
      const opt = document.createElement('option');
      opt.value = status;
      opt.textContent = status;
      statusSelect.append(opt);
    }
    statusSelect.value = node.status;
    statusSelect.addEventListener('change', () => {
      node.status = statusSelect.value;
      commit();
    });

    // Excludes the node itself and its own descendants, or a reparent here
    // could wire the tree into a cycle.
    const parentSelect = document.createElement('select');
    parentSelect.setAttribute('aria-label', 'Parent');
    const blocked = subtreeIds(node.id);
    const rootOpt = document.createElement('option');
    rootOpt.value = '';
    rootOpt.textContent = 'no parent';
    parentSelect.append(rootOpt);
    for (const other of nodes()) {
      if (blocked.has(other.id)) continue;
      const opt = document.createElement('option');
      opt.value = other.id;
      opt.textContent = `${other.id} — ${truncate(other.title || '(untitled)', 24)}`;
      parentSelect.append(opt);
    }
    parentSelect.value = node.parent && !blocked.has(node.parent) ? node.parent : '';
    parentSelect.addEventListener('change', () => {
      node.parent = parentSelect.value || null;
      delete node.pos;   // let layout re-tidy under the new parent
      commit();
    });

    const row = document.createElement('div');
    row.className = 'editor-row';
    row.append(chip, titleInput, statusSelect, parentSelect);

    const noteInput = document.createElement('textarea');
    noteInput.className = 'note-input';
    noteInput.placeholder = 'Note…';
    noteInput.value = node.note || '';
    noteInput.setAttribute('aria-label', 'Note');
    let noteTimer = null;
    const saveNote = () => {
      clearTimeout(noteTimer);
      if (node.note !== noteInput.value) {
        node.note = noteInput.value;
        commit({ rerender: false });   // keep focus in the textarea while typing
      }
    };
    noteInput.addEventListener('input', () => {
      clearTimeout(noteTimer);
      noteTimer = setTimeout(saveNote, 500);
    });
    noteInput.addEventListener('blur', saveNote);

    const addChild = ctl('add-child');
    addChild.addEventListener('click', () => addNode(node.id));

    const detach = ctl('detach');
    detach.disabled = !node.parent;
    detach.addEventListener('click', () => {
      node.parent = null;
      delete node.pos;
      commit();
    });

    const clearPos = ctl('auto-place');
    clearPos.disabled = !node.pos;
    clearPos.addEventListener('click', () => {
      delete node.pos;
      commit();
    });

    const del = ctl('delete');
    del.classList.add('icon-btn--danger');
    del.addEventListener('click', () => confirmDelete(node));

    const actions = document.createElement('div');
    actions.className = 'editor-actions';
    actions.append(addChild, detach, clearPos, del);

    detail.replaceChildren(row, noteInput, actions);
  }

  /* ---------- pointer interaction ---------- */

  // A node's own pointerdown stops propagation, so reaching here means the
  // pointer went down on empty canvas.
  svg.addEventListener('pointerdown', (e) => {
    if (drag) return;
    pan = { startX: e.clientX, startY: e.clientY, tx: view.tx, ty: view.ty, moved: false };
    svg.classList.add('panning');
    svg.setPointerCapture(e.pointerId);
  });

  svg.addEventListener('pointermove', (e) => {
    if (drag) {
      const p = layoutPoint(e);
      const dx = p.x - drag.start.x;
      const dy = p.y - drag.start.y;
      if (!drag.moved && Math.hypot(dx * view.k, dy * view.k) > 4) {
        drag.moved = true;
        drag.el.dataset.dragging = 'yes';
      }
      if (!drag.moved) return;

      // Move the dragged node's group visually without a full re-render.
      drag.live = { x: drag.origin.x + dx, y: drag.origin.y + dy };
      drag.el.setAttribute('transform', `translate(${dx} ${dy})`);

      // Highlight a legal reparent target under the pointer.
      const target = nodeUnder(e, drag.id, drag.blocked);
      for (const g of nodeLayer.children) {
        g.dataset.drop = target && g.dataset.id === target.id ? 'yes' : 'no';
      }
      return;
    }
    if (pan) {
      const dx = e.clientX - pan.startX;
      const dy = e.clientY - pan.startY;
      if (!pan.moved && Math.hypot(dx, dy) > 4) pan.moved = true;
      view.tx = pan.tx + dx;
      view.ty = pan.ty + dy;
      applyView();
    }
  });

  function deselect() {
    if (selectedId === null) return;
    selectedId = null;
    for (const g of nodeLayer.children) g.dataset.selected = 'no';
    renderDetail();
  }

  function nodeUnder(evt, exceptId, blocked) {
    const el = document.elementFromPoint(evt.clientX, evt.clientY);
    const g = el && el.closest ? el.closest('.node-box') : null;
    if (!g) return null;
    const id = g.dataset.id;
    if (!id || id === exceptId || blocked.has(id)) return null;
    return byId(id);
  }

  function endDrag(e) {
    if (!drag) return;
    const d = drag;
    drag = null;
    d.el.removeAttribute('transform');
    delete d.el.dataset.dragging;
    for (const g of nodeLayer.children) g.dataset.drop = 'no';

    const node = byId(d.id);
    if (!node) return;

    if (!d.moved) {
      selectedId = d.id;
      for (const g of nodeLayer.children) {
        g.dataset.selected = g.dataset.id === d.id ? 'yes' : 'no';
      }
      renderDetail();
      return;
    }

    const target = nodeUnder(e, d.id, d.blocked);
    if (target) {
      node.parent = target.id;
      delete node.pos;            // let layout re-tidy under the new parent
      selectedId = node.id;
      commit();
      return;
    }
    if (d.live) {
      node.pos = { x: Math.round(d.live.x), y: Math.round(d.live.y) };
      commit();
    }
  }

  svg.addEventListener('pointerup', (e) => {
    if (drag) return endDrag(e);
    // A click on empty canvas — as opposed to a pan drag — clears the selection.
    // Same 4px threshold the node drag uses, so a shaky click still counts.
    if (pan && !pan.moved) deselect();
    pan = null;
    svg.classList.remove('panning');
  });

  svg.addEventListener('pointercancel', (e) => {
    if (drag) endDrag(e);
    pan = null;
    svg.classList.remove('panning');
  });

  svg.addEventListener('wheel', (e) => {
    e.preventDefault();
    const rect = svg.getBoundingClientRect();
    const cx = e.clientX - rect.left;
    const cy = e.clientY - rect.top;
    const factor = e.deltaY < 0 ? 1.12 : 1 / 1.12;
    const next = Math.min(2.5, Math.max(0.2, view.k * factor));
    // Keep the point under the cursor fixed while zooming.
    view.tx = cx - (cx - view.tx) * (next / view.k);
    view.ty = cy - (cy - view.ty) * (next / view.k);
    view.k = next;
    applyView();
  }, { passive: false });

  addRootBtn.addEventListener('click', () => addNode(null));
  relayoutBtn.addEventListener('click', () => {
    for (const n of nodes()) delete n.pos;
    commit();
  });
  fitBtn.addEventListener('click', () => fit(layout()));
  zoomIn.addEventListener('click', () => { view.k = Math.min(2.5, view.k * 1.2); applyView(); });
  zoomOut.addEventListener('click', () => { view.k = Math.max(0.2, view.k / 1.2); applyView(); });

  // Fit once, the first time the canvas actually has a size. On a deep-linked
  // tab that is right now; on a background tab the wrapper is still 0-wide and
  // the first refresh() after it becomes visible is the moment that counts.
  function fitOnce(pos) {
    if (fitted || !pos || !svg.getBoundingClientRect().width) return;
    fitted = true;
    fit(pos);
  }

  fitOnce(render());

  // A canvas mounted inside a stack block (or a hidden tab) has no width yet at
  // mount time, so the first fit would be a no-op. Watch for the first real
  // size and fit then.
  let observer = null;
  if (!fitted && typeof ResizeObserver === 'function') {
    observer = new ResizeObserver(() => {
      if (fitted) { observer.disconnect(); return; }
      if (svg.getBoundingClientRect().width) {
        fitOnce(layout());
        observer.disconnect();
      }
    });
    observer.observe(wrap);
  }

  function detailFocused() {
    const a = document.activeElement;
    return !!a && detail.contains(a);
  }

  return {
    refresh() {
      if (drag || editingId) return;   // never yank the canvas out from under a live drag or rename
      applyHeight();
      if (detailFocused()) return;     // or out from under a title/note edit in progress
      fitOnce(render());
    },

    // f fits, r re-lays out — the two things you do constantly on a graph that
    // has grown. Returning true tells the shell the key was consumed.
    onKey(e) {
      if (e.key === 'f') { fit(render()); return true; }
      if (e.key === 'r') {
        for (const n of nodes()) delete n.pos;
        commit();
        return true;
      }
      return false;
    },

    // Deep links: #tab=bb1&node=bb7 selects the node and centres the canvas on
    // it, rather than dropping you on a tab and leaving you to find it.
    focus(id) {
      const node = byId(String(id));
      if (!node) return false;
      const pos = render();                 // returns undefined for an empty board
      const p = pos && pos.get(node.id);
      selectNode(node.id);
      if (p) {
        const rect = svg.getBoundingClientRect();
        if (rect.width && rect.height) {
          view.k = Math.max(0.6, Math.min(view.k, 1.2));
          view.tx = rect.width / 2 - p.x * view.k;
          view.ty = rect.height / 2 - p.y * view.k;
          applyView();
        }
      }
      return true;
    },
  };
}
